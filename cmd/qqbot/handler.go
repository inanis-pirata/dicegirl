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

// debugf logs only when DICEGIRL_BOT_DEBUG is on.
func (h *botHandler) debugf(format string, args ...any) {
	if h.cfg.Debug {
		h.logger.Printf(format, args...)
	}
}

// handle is the gateway event callback. It must return quickly-ish; the
// gateway already bounds concurrency with a worker pool.
func (h *botHandler) handle(ctx context.Context, scope string, ev messageEvent) {
	if ev.ID != "" && !h.markSeen(ev.ID) {
		h.debugf("丢弃重复投递 scope=%s msg=%s", scope, ev.ID)
		return // duplicate redelivery
	}
	// Only plain text events are commands for now (voice attachments carry
	// asr_refer_text but ignoring them keeps the contract explicit).
	if ev.MessageType != 0 {
		h.logger.Printf("忽略非文本消息 scope=%s msg=%s type=%d content=%q",
			scope, ev.ID, ev.MessageType, truncate(strings.TrimSpace(ev.Content), 200))
		return
	}
	content := strings.TrimSpace(ev.Content)
	h.logger.Printf("事件 scope=%s msg=%s group=%s member=%s user=%s type=%d content=%q",
		scope, ev.ID, ev.GroupOpenid, ev.Author.MemberOpenid,
		ev.Author.UserOpenid, ev.MessageType, content)
	if content == "" {
		h.logger.Printf("忽略空内容消息 scope=%s msg=%s（可能只@了机器人）", scope, ev.ID)
		return
	}

	// ".r3d6" is as good as ".r 3d6": split a command name glued to its
	// first argument before any platform-specific routing.
	content = normalizeCommandLine(content, "ob", "log", "绑定", "bind", "绑定码")
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
	noun := hiddenNoun(pending.Kind)
	h.postText(ctx, botScopeC2C, userOpenid, "", 1, "✅ 私信绑定成功。之后本群的"+noun+"结果会私发给你")
	if pending.Text != "" {
		if err := h.sendChunks(ctx, botScopeC2C, userOpenid, "", "🎲 群"+noun+"结果（仅你可见）\n"+pending.Text, maxPassiveGroup); err != nil {
			h.logger.Printf(noun+"结果补发失败：%v", err)
		}
	}
}

// --- group chat ------------------------------------------------------------

func (h *botHandler) runGroup(ctx context.Context, ev messageEvent, content string) {
	t, st, ok := h.groupContext(ctx, ev)
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

// groupContext opens the group's application and adapter state. The failure
// notice is sent as a passive reply to the inbound event (msg_id attached):
// a proactive send needs a separate quota/permission and is rejected with
// 40034105.
func (h *botHandler) groupContext(ctx context.Context, ev messageEvent) (*bot.Tenant, *groupStore, bool) {
	groupOpenid := ev.GroupOpenid
	t, err := h.manager.Get(ctx, bot.ScopeGroup, groupOpenid)
	if err != nil {
		h.logger.Printf("打开群数据失败 group=%s：%v", groupOpenid, err)
		h.postText(ctx, botScopeGroup, groupOpenid, ev.ID, 1, "内部错误：无法打开本群数据")
		return nil, nil, false
	}
	st, err := h.stores.get(t.Root)
	if err != nil {
		h.logger.Printf("读取群状态失败 group=%s：%v", groupOpenid, err)
		h.postText(ctx, botScopeGroup, groupOpenid, ev.ID, 1, "内部错误：无法读取本群状态")
		return nil, nil, false
	}
	return t, st, true
}

func (h *botHandler) isGroupPC(fields []string) bool {
	_, ok := isGroupPCCommand(fields)
	return ok
}

// runNormal executes an ordinary command in the group, using the sender's
// card bound on the current table and recording it into an active session
// log. A newly created card is auto-bound to the sender on the current table.
func (h *botHandler) runNormal(ctx context.Context, t *bot.Tenant, st *groupStore,
	ev messageEvent, content string) {
	member := ev.Author.MemberOpenid
	tid := h.scopeTableID(ctx, t)
	h.debugf("群命令分派 group=%s member=%s table=%s line=%q", ev.GroupOpenid, member, tid, content)
	opts := []bot.RunOption{bot.WithAutoApply()}
	if b, ok := st.binding(member, tid); ok {
		if opt, fresh := h.boundCardOption(ctx, t, st, member, tid, b); fresh {
			h.debugf("使用绑卡 table=%s card=%s(%s)", tid, b.CardName, b.CardID)
			opts = append(opts, opt)
		}
	} else {
		h.debugf("该成员在当前桌无绑卡 member=%s table=%s", member, tid)
	}
	opts = append(opts, bot.WithCardCreatedHook(func(id, name string) {
		err := st.update(func(gs *groupState) error {
			per := gs.Bindings[member]
			if per == nil {
				per = map[string]cardBinding{}
				gs.Bindings[member] = per
			}
			per[tid] = cardBinding{CardID: id, CardName: name, UpdatedAt: time.Now()}
			return nil
		})
		if err != nil {
			h.logger.Printf("新卡自动绑卡失败 member=%s table=%s：%v", member, tid, err)
		}
	}))
	out, code, recognized := h.executeWith(ctx, bot.ScopeGroup, ev.GroupOpenid, content, opts...)
	if !recognized {
		h.maybeReplyUnknown(ctx, botScopeGroup, ev.GroupOpenid, ev.ID)
		return
	}
	appendLogEntry(st, t.Root, memberLabel(st, member, tid), content, out)
	h.replyChunks(ctx, botScopeGroup, ev.GroupOpenid, ev.ID, out, code, maxPassiveGroup)
}

// runHiddenInGroup executes a .rh/.drawh line in the group scope and
// privately delivers its result to the sender and to observers. Nothing
// about the result is ever posted in the group; on delivery failure the
// group only learns that the hidden command failed, not what it was.
func (h *botHandler) runHiddenInGroup(ctx context.Context, t *bot.Tenant, st *groupStore,
	ev messageEvent, content string) {
	kind := hiddenKind(content)
	noun := hiddenNoun(kind)
	line := h.hiddenLine(ctx, bot.ScopeGroup, ev.GroupOpenid, content)
	member := ev.Author.MemberOpenid
	tid := h.scopeTableID(ctx, t)
	opts := []bot.RunOption{bot.WithAutoApply()}
	if b, ok := st.binding(member, tid); ok {
		if opt, fresh := h.boundCardOption(ctx, t, st, member, tid, b); fresh {
			opts = append(opts, opt)
		}
	}
	out, _, recognized := h.executeWith(ctx, bot.ScopeGroup, ev.GroupOpenid, line, opts...)
	if !recognized {
		h.postText(ctx, botScopeGroup, ev.GroupOpenid, ev.ID, 1, noun+"：无法识别的指令")
		return
	}
	appendLogEntry(st, t.Root, memberLabel(st, member, tid), "（"+noun+"）"+line, out)

	userOpenid, linked := st.link(member)
	if !linked {
		code := h.binds.issue(pendingBind{
			GroupID: ev.GroupOpenid,
			Member:  member,
			Text:    out,
			Kind:    kind,
		})
		h.postText(ctx, botScopeGroup, ev.GroupOpenid, ev.ID, 1, fmt.Sprintf(
			"%s结果已生成。为保护隐私，请先私聊机器人发送「绑定 %s」（10 分钟内有效），绑定后结果会立刻私发给你",
			noun, code))
		return
	}
	if err := h.sendChunks(ctx, botScopeC2C, userOpenid, "",
		"🎲 群"+noun+"结果（仅你可见）\n"+out, maxPassiveGroup); err != nil {
		h.logger.Printf(noun+"私发失败：%v", err)
		h.postText(ctx, botScopeGroup, ev.GroupOpenid, ev.ID, 1,
			noun+"未送达：私信失败（需已是机器人好友且未关闭消息）")
		return
	}
	// Observers see the same result, marked as an observation.
	for _, oid := range st.observerUserOpenids(member) {
		if err := h.sendChunks(ctx, botScopeC2C, oid, "",
			"🎲 群"+noun+"围观\n"+out, maxPassiveGroup); err != nil {
			h.logger.Printf(noun+"围观推送失败：%v", err)
		}
	}
	h.postText(ctx, botScopeGroup, ev.GroupOpenid, ev.ID, 1, "🎲 "+noun+"结果已私发给你")
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
		h.debugf("未识别指令，静默忽略 kind=%s id=%s line=%q", kind, id, line)
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
		return
	}
	h.debugf("回复成功 scope=%s target=%s runes=%d", scope, target, utf8.RuneCountInString(text))
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

// normalizeCommandLine repairs a first token that glues the command to its
// first argument (".r3d6" -> "r 3d6", ".objoin" -> "ob join"). Unknown lines
// pass through untouched.
func normalizeCommandLine(line string, extra ...string) string {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return line
	}
	name, glued, _, ok := chatcore.SplitCommandToken(fields[0], extra...)
	if !ok || glued == "" {
		return line
	}
	out := make([]string, 0, len(fields)+1)
	out = append(out, name, glued)
	out = append(out, fields[1:]...)
	return strings.Join(out, " ")
}

// isHiddenCommand reports whether the first token is a hidden-result command
// ("rh" hidden roll or "drawh" hidden draw) with an optional alias prefix.
func isHiddenCommand(line string) bool {
	name, _, _, _ := chatcore.SplitCommandToken(firstField(line))
	return name == "rh" || name == "drawh"
}

// hiddenKind returns the hidden command name ("rh"/"drawh").
func hiddenKind(line string) string {
	name, _, _, _ := chatcore.SplitCommandToken(firstField(line))
	return name
}

// hiddenLine turns the body of a hidden-result command into an executable
// line. Dice! writes ".rh 3d6" (a hidden ordinary roll) as well as
// ".rh .ra 心理学" (a hidden check), so an rh expression body is routed
// through the shared roll command while an explicit command is left
// untouched. A ".drawh" body always runs through "draw".
func (h *botHandler) hiddenLine(ctx context.Context, kind, id, content string) string {
	name, body := hiddenParts(content)
	switch name {
	case "drawh":
		body = strings.TrimSpace(body)
		if body == "" {
			return ""
		}
		return "draw " + body
	case "rh":
		if body == "" || h.looksLikeCommand(ctx, kind, id, body) {
			return body
		}
		return "r " + body
	}
	return body
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

// hiddenParts splits a hidden-command line into its normalized command name
// ("rh"/"drawh") and the body after the command, keeping any argument glued
// to the first token (".rh3d6" -> "rh", "3d6").
func hiddenParts(line string) (name, body string) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", ""
	}
	n, glued, _, _ := chatcore.SplitCommandToken(fields[0])
	if n != "rh" && n != "drawh" {
		return "", line
	}
	parts := make([]string, 0, len(fields))
	if glued != "" {
		parts = append(parts, glued)
	}
	parts = append(parts, fields[1:]...)
	return n, strings.Join(parts, " ")
}

func stripHiddenPrefix(line string) string {
	_, body := hiddenParts(line)
	return body
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
