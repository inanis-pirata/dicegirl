package main

// Inbound event handling: deduplicate platform redeliveries, translate the
// event into one scoped chatcore command line (bot.Manager), and post the
// rendered text back as passive replies. Platform-only behavior lives here:
// hidden rolls (.rh) with observers (.ob), per-member card binding
// (.pc tag/grp) and session logs (.log).

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"dicegirl/bot"
	"dicegirl/chatcore"
	"dicegirl/internal/apperr"
)

const (
	// Platform-recommended reply window is 5 minutes and at most a handful
	// of passive messages per inbound event.
	maxPassiveGroup = 5
	maxPassiveC2C   = 4
	// Conservative per-message size: the documented limit is not published,
	// the server rejects overlong content with code 40054007.
	maxChunkRunes = 1200
	// maxMessageRunes is a safety pre-split only; the real platform limit is
	// unpublished, so sendChunks starts with one message and splits on the
	// server's MESSAGE_TOO_LONG response instead of guessing.
	maxMessageRunes = 4000
	minSplitRunes   = 400
	dedupeMaxSize   = 2048
	dedupeTTL       = 10 * time.Minute
)

type botHandler struct {
	cfg      *botConfig
	manager  *bot.Manager
	api      *apiClient
	contacts *contactStore
	logger   interface{ Printf(string, ...any) }

	stores *groupStores
	binds  *bindManager

	dedupeMu sync.Mutex
	seen     map[string]time.Time
}

func newBotHandler(cfg *botConfig, m *bot.Manager, api *apiClient,
	contacts *contactStore, logger interface{ Printf(string, ...any) }) *botHandler {
	return &botHandler{
		cfg:      cfg,
		manager:  m,
		api:      api,
		contacts: contacts,
		logger:   logger,
		stores:   newGroupStores(),
		binds:    newBindManager(),
		seen:     map[string]time.Time{},
	}
}

// handle is the gateway event callback. It must return quickly-ish; the
// gateway already bounds concurrency with a worker pool.
func (h *botHandler) handle(ctx context.Context, scope string, ev messageEvent) {
	if ev.ID != "" && !h.markSeen(ev.ID) {
		return // duplicate redelivery
	}
	// Only plain text events are commands for now (voice attachments carry
	// asr_refer_text but ignoring them keeps the contract explicit).
	if ev.MessageType != 0 {
		return
	}
	content := strings.TrimSpace(ev.Content)
	h.logger.Printf("事件 scope=%s msg=%s group=%s member=%s user=%s type=%d content=%q",
		scope, ev.ID, ev.GroupOpenid, ev.Author.MemberOpenid,
		ev.Author.UserOpenid, ev.MessageType, content)
	if content == "" {
		return
	}

	switch scope {
	case botScopeC2C:
		if err := h.contacts.remember(ev.Author.UnionOpenid, ev.Author.UserOpenid); err != nil {
			h.logger.Printf("保存联系人映射失败：%v", err)
		}
		h.runC2C(ctx, ev, content)
	case botScopeGroup:
		h.runGroup(ctx, ev, content)
	}
}

// --- private chat ----------------------------------------------------------

func (h *botHandler) runC2C(ctx context.Context, ev messageEvent, content string) {
	target := ev.Author.UserOpenid

	// Binding-code completion takes priority over the chat grammar.
	if code, ok := parseBindCommand(content); ok {
		h.completeBind(ctx, target, code)
		return
	}

	// ".rh" in a private chat is just a private roll: the channel is already
	// private.
	if isHiddenCommand(content) {
		content = h.hiddenLine(ctx, bot.ScopeC2C, target, content)
	}
	out, code, ok := h.execute(ctx, bot.ScopeC2C, target, content)
	if !ok {
		h.maybeReplyUnknown(ctx, botScopeC2C, target, ev.ID)
		return
	}
	h.replyChunks(ctx, botScopeC2C, target, ev.ID, out, code, maxPassiveC2C)
}

// completeBind finishes a group member's private-channel binding: it links
// the member to this user_openid in the group's adapter state, then runs the
// pending action carried by the code (deliver a hidden-roll result, or join
// the observer list).
func (h *botHandler) completeBind(ctx context.Context, userOpenid, code string) {
	pending, ok := h.binds.take(code)
	if !ok {
		h.postText(ctx, botScopeC2C, userOpenid, "", 1,
			"绑定码无效或已过期。请在群里重新发送该指令以获取新绑定码")
		return
	}
	t, err := h.manager.Get(ctx, bot.ScopeGroup, pending.GroupID)
	if err != nil {
		h.postText(ctx, botScopeC2C, userOpenid, "", 1, "绑定失败："+err.Error())
		return
	}
	st, err := h.stores.get(t.Root)
	if err != nil {
		h.postText(ctx, botScopeC2C, userOpenid, "", 1, "绑定失败："+err.Error())
		return
	}
	err = st.update(func(gs *groupState) error {
		gs.Links[pending.Member] = memberLink{UserOpenid: userOpenid, UpdatedAt: time.Now()}
		if pending.Observer {
			gs.ObEnabled = true
			gs.Observers[pending.Member] = observerEntry{Enabled: true, UpdatedAt: time.Now()}
		}
		return nil
	})
	if err != nil {
		h.postText(ctx, botScopeC2C, userOpenid, "", 1, "绑定保存失败："+err.Error())
		return
	}
	h.postText(ctx, botScopeC2C, userOpenid, "", 1, "✅ 私信绑定成功。之后本群的暗骰结果会私发给你")
	if pending.Text != "" {
		if err := h.sendChunks(ctx, botScopeC2C, userOpenid, "", "🎲 暗骰结果（仅你可见）\n"+pending.Text, maxPassiveGroup); err != nil {
			h.logger.Printf("暗骰结果补发失败：%v", err)
		}
	}
}

// --- group chat ------------------------------------------------------------

func (h *botHandler) runGroup(ctx context.Context, ev messageEvent, content string) {
	t, st, ok := h.groupContext(ctx, ev.GroupOpenid)
	if !ok {
		return
	}
	fields := strings.Fields(content)
	name := groupCommandName(content)

	switch {
	case isHiddenCommand(content):
		h.runHiddenInGroup(ctx, t, st, ev, content)
	case name == "ob":
		h.runObserver(ctx, t, st, ev, fields[1:], func(text string) {
			h.postText(ctx, botScopeGroup, ev.GroupOpenid, ev.ID, 1, text)
		})
	case name == "log":
		h.runLog(ctx, t, st, ev, fields[1:], func(text string) {
			h.postText(ctx, botScopeGroup, ev.GroupOpenid, ev.ID, 1, text)
		})
	case name == "pc" && h.isGroupPC(fields):
		sub, _ := isGroupPCCommand(fields)
		h.runPCBinding(ctx, t, st, ev, sub, fields[2:], func(text string) {
			h.postText(ctx, botScopeGroup, ev.GroupOpenid, ev.ID, 1, text)
		})
	default:
		h.runNormal(ctx, t, st, ev, content)
	}
}

// groupContext opens the group's application and adapter state.
func (h *botHandler) groupContext(ctx context.Context, groupOpenid string) (*bot.Tenant, *groupStore, bool) {
	t, err := h.manager.Get(ctx, bot.ScopeGroup, groupOpenid)
	if err != nil {
		h.logger.Printf("打开群数据失败 group=%s：%v", groupOpenid, err)
		h.postText(ctx, botScopeGroup, groupOpenid, "", 1, "内部错误：无法打开本群数据")
		return nil, nil, false
	}
	st, err := h.stores.get(t.Root)
	if err != nil {
		h.logger.Printf("读取群状态失败 group=%s：%v", groupOpenid, err)
		h.postText(ctx, botScopeGroup, groupOpenid, "", 1, "内部错误：无法读取本群状态")
		return nil, nil, false
	}
	return t, st, true
}

func (h *botHandler) isGroupPC(fields []string) bool {
	_, ok := isGroupPCCommand(fields)
	return ok
}

// runNormal executes an ordinary command in the group, using the sender's
// bound card and recording it into an active session log.
func (h *botHandler) runNormal(ctx context.Context, t *bot.Tenant, st *groupStore,
	ev messageEvent, content string) {
	opts := []bot.RunOption{bot.WithAutoApply()}
	if b, ok := st.binding(ev.Author.MemberOpenid); ok {
		opts = append(opts, bot.WithDefaultCharacter(b.CardID))
	}
	out, code, recognized := h.executeWith(ctx, bot.ScopeGroup, ev.GroupOpenid, content, opts...)
	if !recognized {
		h.maybeReplyUnknown(ctx, botScopeGroup, ev.GroupOpenid, ev.ID)
		return
	}
	appendLogEntry(st, t.Root, memberLabel(st, ev.Author.MemberOpenid), content, out)
	h.replyChunks(ctx, botScopeGroup, ev.GroupOpenid, ev.ID, out, code, maxPassiveGroup)
}

// runHiddenInGroup executes a .rh line in the group scope and privately
// delivers its result to the roller and to observers. Nothing about the roll
// result is ever posted in the group; on delivery failure the group only
// learns that the hidden roll failed, not what it was.
func (h *botHandler) runHiddenInGroup(ctx context.Context, t *bot.Tenant, st *groupStore,
	ev messageEvent, content string) {
	line := h.hiddenLine(ctx, bot.ScopeGroup, ev.GroupOpenid, content)
	opts := []bot.RunOption{bot.WithAutoApply()}
	if b, ok := st.binding(ev.Author.MemberOpenid); ok {
		opts = append(opts, bot.WithDefaultCharacter(b.CardID))
	}
	out, _, recognized := h.executeWith(ctx, bot.ScopeGroup, ev.GroupOpenid, line, opts...)
	if !recognized {
		h.postText(ctx, botScopeGroup, ev.GroupOpenid, ev.ID, 1, "暗骰：无法识别的指令")
		return
	}
	appendLogEntry(st, t.Root, memberLabel(st, ev.Author.MemberOpenid), "(暗骰) "+line, out)

	userOpenid, linked := st.link(ev.Author.MemberOpenid)
	if !linked {
		code := h.binds.issue(pendingBind{
			GroupID: ev.GroupOpenid,
			Member:  ev.Author.MemberOpenid,
			Text:    out,
		})
		h.postText(ctx, botScopeGroup, ev.GroupOpenid, ev.ID, 1, fmt.Sprintf(
			"暗骰结果已生成。为保护隐私，请先私聊机器人发送「绑定 %s」（10 分钟内有效），绑定后结果会立刻私发给你",
			code))
		return
	}
	if err := h.sendChunks(ctx, botScopeC2C, userOpenid, "",
		"🎲 群暗骰结果（仅你可见）\n"+out, maxPassiveGroup); err != nil {
		h.logger.Printf("暗骰私发失败：%v", err)
		h.postText(ctx, botScopeGroup, ev.GroupOpenid, ev.ID, 1,
			"暗骰未送达：私信失败（需已是机器人好友且未关闭消息）")
		return
	}
	// Observers see the same result, marked as an observation.
	for _, oid := range st.observerUserOpenids(ev.Author.MemberOpenid) {
		if err := h.sendChunks(ctx, botScopeC2C, oid, "",
			"🎲 群暗骰围观\n"+out, maxPassiveGroup); err != nil {
			h.logger.Printf("暗骰围观推送失败：%v", err)
		}
	}
	h.postText(ctx, botScopeGroup, ev.GroupOpenid, ev.ID, 1, "🎲 暗骰结果已私发给你")
}

// --- execution helpers -----------------------------------------------------

// execute runs one line against a scope with the adapter's default policy.
func (h *botHandler) execute(ctx context.Context, kind, id, line string) (string, int, bool) {
	return h.executeWith(ctx, kind, id, line, bot.WithAutoApply())
}

func (h *botHandler) executeWith(ctx context.Context, kind, id, line string,
	opts ...bot.RunOption) (string, int, bool) {
	res, err := h.manager.RunLine(ctx, kind, id, line, opts...)
	if err != nil {
		return "内部错误：" + err.Error(), 1, true
	}
	if res.Code == chatcore.Unrecognized {
		return "", 0, false
	}
	text := res.Out
	if res.Err != "" {
		text = strings.TrimRight(res.Err, "\n")
	}
	h.logger.Printf("执行 kind=%s id=%s line=%q code=%d out=%q", kind, id, line, res.Code, text)
	return text, res.Code, true
}

func (h *botHandler) maybeReplyUnknown(ctx context.Context, scope, target, msgID string) {
	if !h.cfg.ReplyUnknown {
		return
	}
	h.postText(ctx, scope, target, msgID, 1,
		"没有识别到骰子指令。发送 help 可查看可用指令（如 .r 1d100、.ra 心理学）")
}

// replyChunks splits text and posts the pieces as sequential passive
// replies (msg_seq 1..N).
func (h *botHandler) replyChunks(ctx context.Context, scope, target, msgID, text string, code, maxParts int) {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		text = "（无输出）"
	}
	if err := h.sendChunks(ctx, scope, target, msgID, text, maxParts); err != nil {
		h.logger.Printf("回复失败 scope=%s target=%s code=%d：%v", scope, target, code, err)
	}
}

func (h *botHandler) postText(ctx context.Context, scope, target, msgID string, seq int, text string) {
	if err := h.api.sendReply(ctx, scope, target, msgID, text, seq); err != nil {
		h.logger.Printf("发送失败 scope=%s：%v", scope, err)
	}
}

// sendChunks posts text as few messages as possible: it first tries one
// message and only splits when the platform answers MESSAGE_TOO_LONG, since
// the real length limit is not documented. msgID empty means a proactive
// message. Parts beyond maxParts are dropped with a truncation notice.
func (h *botHandler) sendChunks(ctx context.Context, scope, target, msgID, text string, maxParts int) error {
	chunks := splitText(text, maxMessageRunes)
	if len(chunks) > maxParts {
		chunks = chunks[:maxParts]
		chunks[maxParts-1] = strings.TrimRight(chunks[maxParts-1], "\n") +
			"\n…（输出过长已截断）"
	}

	seq, sent := 0, 0
	var send func(part string) error
	send = func(part string) error {
		if sent >= maxParts {
			return nil
		}
		err := h.api.sendReply(ctx, scope, target, msgID, part, seq+1)
		if err == nil {
			seq++
			sent++
			return nil
		}
		if !apperr.IsCode(err, apperr.MessageTooLong) || utf8.RuneCountInString(part) <= minSplitRunes {
			return err
		}
		head, tail := splitHalf(part)
		if err := send(head); err != nil {
			return err
		}
		return send(tail)
	}
	for _, part := range chunks {
		if err := send(part); err != nil {
			return err
		}
	}
	return nil
}

// splitHalf divides s near the middle, preferring a newline boundary.
func splitHalf(s string) (string, string) {
	runes := []rune(s)
	mid := len(runes) / 2
	cut := mid
	for i := mid; i < len(runes) && i < mid+200; i++ {
		if runes[i] == '\n' {
			cut = i + 1
			break
		}
	}
	for i := mid; i > 0 && i > mid-200; i-- {
		if runes[i-1] == '\n' {
			cut = i
			break
		}
	}
	return string(runes[:cut]), string(runes[cut:])
}

// markSeen returns false when id was already seen. The table is bounded and
// expired lazily on insert.
func (h *botHandler) markSeen(id string) bool {
	h.dedupeMu.Lock()
	defer h.dedupeMu.Unlock()
	now := time.Now()
	if _, dup := h.seen[id]; dup {
		return false
	}
	if len(h.seen) >= dedupeMaxSize {
		for k, t := range h.seen {
			if now.Sub(t) > dedupeTTL {
				delete(h.seen, k)
			}
		}
	}
	h.seen[id] = now
	return true
}

// isHiddenCommand reports whether the first token is "rh" with an optional
// alias prefix.
func isHiddenCommand(line string) bool {
	name, _ := chatcore.ShortToken(firstField(line))
	return name == "rh"
}

// hiddenLine turns the body of a hidden-roll command into an executable
// line. Dice! writes ".rh 3d6" (a hidden ordinary roll) as well as
// ".rh .ra 心理学" (a hidden check), so an expression body is routed through
// the shared roll command and an explicit command is left untouched.
func (h *botHandler) hiddenLine(ctx context.Context, kind, id, content string) string {
	line := stripHiddenPrefix(content)
	if line == "" || h.looksLikeCommand(ctx, kind, id, line) {
		return line
	}
	return "r " + line
}

func (h *botHandler) looksLikeCommand(ctx context.Context, kind, id, line string) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false
	}
	name, prefixed := chatcore.ShortToken(fields[0])
	if prefixed || chatcore.IsShortCommand(name) {
		return true
	}
	t, err := h.manager.Get(ctx, kind, id)
	if err != nil {
		return false
	}
	if _, _, _, ok := chatcore.BookCommand(t.App, name); ok {
		return true
	}
	return false
}

func stripHiddenPrefix(line string) string {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return line
	}
	name, _ := chatcore.ShortToken(fields[0])
	if name != "rh" {
		return line
	}
	return strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
}

func firstField(s string) string {
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i]
	}
	return s
}

// splitText breaks s into chunks of at most max runes, preferring line
// boundaries and never splitting inside a rune.
func splitText(s string, max int) []string {
	if max <= 0 {
		max = maxChunkRunes
	}
	runes := []rune(s)
	var chunks []string
	for len(runes) > 0 {
		n := len(runes)
		if n > max {
			n = max
			// Prefer the last newline inside the window, but never so far
			// back that a part becomes unreasonably small.
			for i := n - 1; i >= n*3/4; i-- {
				if runes[i] == '\n' {
					n = i
					break
				}
			}
		}
		chunks = append(chunks, string(runes[:n]))
		runes = runes[n:]
	}
	return chunks
}
