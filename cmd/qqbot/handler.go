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
	inbox  *fileInbox

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
		inbox:    newFileInbox(),
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
	// A rulebook upload arrives as a file attachment (content_type=file),
	// usually with no command text. Stash it for the sender's next
	// `rule add` and reply with the next step. Group file messages only
	// reach the bot when the全量消息 permission (GROUP_MESSAGE_CREATE) is
	// enabled — otherwise masters upload in a private chat.
	if att, ok := yamlFileAttachment(ev); ok {
		h.logger.Printf("收到规则书文件 scope=%s msg=%s file=%q size=%d",
			scope, ev.ID, att.Filename, att.Size)
		target := ev.GroupOpenid
		if scope == botScopeC2C {
			target = ev.Author.UserOpenid
		}
		h.postText(ctx, scope, target, ev.ID, 1, h.stashUpload(ctx, scope, ev, att))
		return
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

	// Hidden commands in a private chat are just private rolls/checks: the
	// channel is already private.
	if isHiddenCommand(content) {
		content = h.hiddenLine(ctx, bot.ScopeC2C, target, content)
	}
	// rule add / rule del are gated and consume inbox files; they bypass the
	// generic executor path.
	if mutation, _ := ruleMutation(content); mutation {
		h.runRuleMutation(ctx, botScopeC2C, target, ev, content,
			func(line string) (string, int, bool) {
				return h.executeWith(ctx, bot.ScopeC2C, target, line,
					bot.WithAutoApply(), bot.WithActor(target, "私聊用户", false))
			})
		return
	}
	var logExport string
	out, code, ok := h.executeWith(ctx, bot.ScopeC2C, target, content,
		bot.WithAutoApply(), bot.WithActor(target, "私聊用户", false),
		bot.WithLogFileHook(func(path string) { logExport = path }))
	if !ok {
		h.maybeReplyUnknown(ctx, botScopeC2C, target, ev.ID)
		return
	}
	if logExport == "" {
		h.replyChunks(ctx, botScopeC2C, target, ev.ID, out, code, maxPassiveC2C)
		return
	}
	next := 1
	if err := h.sendLogFile(ctx, botScopeC2C, target, ev.ID, logExport, next); err != nil {
		h.postText(ctx, botScopeC2C, target, ev.ID, next,
			"日志文件发送失败："+err.Error())
		next++
	}
	h.replyChunksFrom(ctx, botScopeC2C, target, ev.ID, out, code, next, maxPassiveC2C-next+1)
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

	// rule add / rule del: master-gated, inbox-consuming, executed with a
	// minimal option set (no card binding matters for rulebook management).
	if mutation, _ := ruleMutation(content); mutation {
		member := ev.Author.MemberOpenid
		tid := h.scopeTableID(ctx, t)
		h.runRuleMutation(ctx, botScopeGroup, ev.GroupOpenid, ev, content,
			func(line string) (string, int, bool) {
				return h.executeWith(ctx, bot.ScopeGroup, ev.GroupOpenid, line,
					bot.WithAutoApply(),
					bot.WithActor(member, memberLabel(st, member, tid), false))
			})
		return
	}

	// Ordinary chat is part of a play log. Commands are recorded by the
	// shared executor; adapter-only commands (ob/bind/pc tag) stay outside
	// the log grammar just like before.
	if !isCommandLine(t.App, content) {
		member := ev.Author.MemberOpenid
		if err := t.App.LogSay(ctx, member, memberLabel(st, member, h.scopeTableID(ctx, t)), content); err != nil {
			h.logger.Printf("记录群聊失败 group=%s member=%s：%v", ev.GroupOpenid, member, err)
		}
	}

	switch action, sub := groupDispatch(content); action {
	case groupActionHidden:
		h.runHiddenInGroup(ctx, t, st, ev, content)
	case groupActionObserver:
		h.runObserver(ctx, t, st, ev, fields[1:], func(text string) {
			h.postText(ctx, botScopeGroup, ev.GroupOpenid, ev.ID, 1, text)
		})
	case groupActionPCBinding:
		h.runPCBinding(ctx, t, st, ev, sub, fields[2:], func(text string) {
			h.postText(ctx, botScopeGroup, ev.GroupOpenid, ev.ID, 1, text)
		})
	case groupActionPCRejected:
		// Only state-changing card commands are refused here: the group's
		// card store is shared by every member, so pc new/nn/use/del/clr/...
		// must never reach the general executor. Read-only subcommands
		// (show/list/active/val/stat) stay on the normal path above.
		h.postText(ctx, botScopeGroup, ev.GroupOpenid, ev.ID, 1,
			"群里不能新建、改名、删除或切换角色卡；这类操作请在私聊进行（查看可用 pc show / pc list）")
	default:
		h.runNormal(ctx, t, st, ev, content)
	}
}

// groupAction is the group-chat counterpart of a dispatch decision: which
// handler a message goes to. It exists so the decision can be tested without
// opening a tenant, a store or an API client.
type groupAction int

const (
	// groupActionNormal is an ordinary command: the shared executor runs it
	// with the sender's bound card.
	groupActionNormal groupAction = iota
	groupActionHidden
	groupActionObserver
	// groupActionPCBinding is one of the card-binding subcommands the
	// adapter handles itself (pc tag/untag/grp).
	groupActionPCBinding
	// groupActionPCRejected is any pc line that would change the group's
	// persistent state. Those subcommands (new/nn/use/del/clr/copy/import/
	// meta/export/...) are private-chat only and must not fall through to
	// the executor: the group store is shared, so a fall-through would let
	// any member rename or delete another member's card, clear the table, or
	// repoint the table's active card at someone else's sheet.
	groupActionPCRejected
)

// groupDispatch decides which group handler a message goes to. It is pure so
// the pc boundary can be tested for every subcommand; runGroup only executes
// the decision.
func groupDispatch(content string) (groupAction, string) {
	name := groupCommandName(content)
	fields := strings.Fields(content)
	switch {
	case isHiddenCommand(content):
		return groupActionHidden, ""
	case name == "ob":
		return groupActionObserver, ""
	case name == "pc":
		// Three outcomes, in order: the adapter's own binding subcommands,
		// the read-only subcommands (which the shared executor renders
		// normally), and everything else, which is refused.
		if sub, ok := isGroupPCCommand(fields); ok {
			return groupActionPCBinding, sub
		}
		if isGroupPCReadOnly(fields) {
			return groupActionNormal, ""
		}
		return groupActionPCRejected, ""
	}
	return groupActionNormal, ""
}

// isGroupPCReadOnly reports whether a pc line only reads the card store. The
// line is "read-only" when the subcommand changes no persistent state — not
// when it merely looks like a query: `pc stat clr` resets a card's dice
// statistics, so it is excluded even though `pc stat` itself only reads.
func isGroupPCReadOnly(fields []string) bool {
	if len(fields) < 2 {
		return false
	}
	switch fields[1] {
	case "show", "list", "active", "val":
		return true
	case "stat":
		return len(fields) < 3 || fields[2] != "clr"
	}
	return false
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
	if err := h.migrateLegacyLogs(ctx, t, st); err != nil {
		// A failed import is retried on the next message; the command
		// itself still runs so a broken legacy file cannot lock the group.
		h.logger.Printf("迁移旧版战报失败 group=%s：%v", groupOpenid, err)
	}
	return t, st, true
}

// runNormal executes an ordinary command in the group, using the sender's
// card bound on the current table and recording it into an active session
// log. A newly created card is auto-bound to the sender on the current table.
func (h *botHandler) runNormal(ctx context.Context, t *bot.Tenant, st *groupStore,
	ev messageEvent, content string) {
	member := ev.Author.MemberOpenid
	tid := h.scopeTableID(ctx, t)
	h.debugf("群命令分派 group=%s member=%s table=%s line=%q", ev.GroupOpenid, member, tid, content)
	opts := []bot.RunOption{
		bot.WithAutoApply(),
		// QQ group events carry no role flag; the group's own GM roster
		// is the authority for game affairs, so the adapter labels the
		// sender as an ordinary member and lets `.game` enforce GM rules.
		bot.WithActor(member, memberLabel(st, member, tid), false),
	}
	if b, ok := st.binding(member, tid); ok {
		if opt, fresh := h.boundCardOption(ctx, t, st, member, tid, b); fresh {
			h.debugf("使用绑卡 table=%s card=%s(%s)", tid, b.CardName, b.CardID)
			opts = append(opts, opt)
		}
	} else {
		h.debugf("该成员在当前游戏无绑卡 member=%s table=%s", member, tid)
	}
	// `.game over` clears the shared game data; the adapter must also drop
	// the member→card bindings that belong to that game.
	opts = append(opts, bot.WithGameOverHook(func(tableID string) {
		err := st.update(func(gs *groupState) error {
			for memberID, per := range gs.Bindings {
				delete(per, tableID)
				if len(per) == 0 {
					delete(gs.Bindings, memberID)
				}
			}
			return nil
		})
		if err != nil {
			h.logger.Printf("结团清理绑卡失败 table=%s：%v", tableID, err)
		}
	}))
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
	// `.log end/get/export` writes a file; capture its path and upload it
	// after the command releases the tenant lock.
	var logExport string
	opts = append(opts, bot.WithLogFileHook(func(path string) { logExport = path }))
	out, code, recognized := h.executeWith(ctx, bot.ScopeGroup, ev.GroupOpenid, content, opts...)
	if !recognized {
		h.maybeReplyUnknown(ctx, botScopeGroup, ev.GroupOpenid, ev.ID)
		return
	}
	if logExport == "" {
		h.replyChunks(ctx, botScopeGroup, ev.GroupOpenid, ev.ID, out, code, maxPassiveGroup)
		return
	}
	// Send the file first (seq 1) and the acknowledgement after it, so a
	// multi-chunk text reply cannot collide with the file's sequence number.
	next := 1
	if err := h.sendLogFile(ctx, botScopeGroup, ev.GroupOpenid, ev.ID, logExport, next); err != nil {
		h.postText(ctx, botScopeGroup, ev.GroupOpenid, ev.ID, next,
			"日志文件发送失败："+err.Error())
		next++
	}
	h.replyChunksFrom(ctx, botScopeGroup, ev.GroupOpenid, ev.ID, out, code, next, maxPassiveGroup-next+1)
}

// runHiddenInGroup executes a .h/.rh/.rah/.rch/.drawh line in the group scope and
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
	opts := []bot.RunOption{
		bot.WithAutoApply(),
		bot.WithActor(member, memberLabel(st, member, tid), false),
		bot.WithLogPrefix("（" + noun + "）"),
	}
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
	h.replyChunksFrom(ctx, scope, target, msgID, text, code, 1, maxParts)
}

// replyChunksFrom is replyChunks with an explicit first msg_seq. Log file
// replies use it so the file and the text do not reuse the same sequence
// number.
func (h *botHandler) replyChunksFrom(ctx context.Context, scope, target, msgID, text string,
	code, startSeq, maxParts int) {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		text = "（无输出）"
	}
	if err := h.sendChunksFrom(ctx, scope, target, msgID, text, startSeq, maxParts); err != nil {
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

// sendChunks posts text as few messages as possible (msg_seq starts at 1).
func (h *botHandler) sendChunks(ctx context.Context, scope, target, msgID, text string, maxParts int) error {
	return h.sendChunksFrom(ctx, scope, target, msgID, text, 1, maxParts)
}

// sendChunksFrom posts text with an explicit first msg_seq: it first tries one
// message and only splits when the platform answers MESSAGE_TOO_LONG, since
// the real length limit is not documented. msgID empty means a proactive
// message. Parts beyond maxParts are dropped with a truncation notice.
func (h *botHandler) sendChunksFrom(ctx context.Context, scope, target, msgID, text string,
	startSeq, maxParts int) error {
	chunks := splitText(text, maxMessageRunes)
	if len(chunks) > maxParts {
		chunks = chunks[:maxParts]
		chunks[maxParts-1] = strings.TrimRight(chunks[maxParts-1], "\n") +
			"\n…（输出过长已截断）"
	}

	seq, sent := startSeq-1, 0
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
// (".h"/".rh" hidden roll, ".rah"/".rch" hidden checks, or ".drawh" hidden
// draw) with an optional alias prefix.
func isHiddenCommand(line string) bool {
	name, _, _, _ := chatcore.SplitCommandToken(firstField(line))
	switch name {
	case "h", "rh", "rah", "rch", "drawh":
		return true
	default:
		return false
	}
}

// hiddenKind returns the hidden command name ("h"/"rh"/"rah"/"rch"/"drawh").
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
	case "h", "rh":
		if body == "" || h.looksLikeCommand(ctx, kind, id, body) {
			return body
		}
		return "r " + body
	case "rah":
		if body == "" || h.looksLikeCommand(ctx, kind, id, body) {
			return body
		}
		return "ra " + body
	case "rch":
		if body == "" || h.looksLikeCommand(ctx, kind, id, body) {
			return body
		}
		return "rc " + body
	}
	return body
}

func (h *botHandler) looksLikeCommand(ctx context.Context, kind, id, line string) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false
	}
	name, prefixed := chatcore.StripCommandPrefix(fields[0])
	if prefixed || chatcore.IsCommandName(name) {
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
// ("h"/"rh"/"rah"/"rch"/"drawh") and the body after the command, keeping any
// argument glued to the first token (".rh3d6" -> "rh", "3d6").
func hiddenParts(line string) (name, body string) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", ""
	}
	n, glued, _, _ := chatcore.SplitCommandToken(fields[0])
	switch n {
	case "h", "rh", "rah", "rch", "drawh":
	default:
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
