package main

// Group-facing adapter commands that only exist on a chat platform:
//
//	ob join | ob exit | ob list | ob on | ob off   hidden-roll observers
//	pc tag [名字|id] | pc untag | pc grp            per-member card binding
//
// They are handled before chatcore because they need platform state; all
// game and log commands go through the shared chat grammar. The only log
// responsibility left here is uploading a generated export file.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"dicegirl/bot"
	"dicegirl/chatcore"
	"dicegirl/internal/app"
	"dicegirl/internal/apperr"
	"dicegirl/internal/storage"
)

// scopeTableID resolves the scope's current table id; any failure falls back
// to the implicit default table so a command can still run.
func (h *botHandler) scopeTableID(ctx context.Context, t *bot.Tenant) string {
	id, err := t.App.ActiveTableID(ctx)
	if err != nil {
		h.logger.Printf("解析当前游戏失败：%v", err)
		return storage.DefaultTableID
	}
	if id == "" {
		return storage.DefaultTableID
	}
	return id
}

// boundCardOption validates a member's binding and returns the chatcore
// option that selects the card. A deleted card prunes the binding; a renamed
// card refreshes the stored name; other errors leave the binding untouched
// and simply skip the default for this message.
func (h *botHandler) boundCardOption(ctx context.Context, t *bot.Tenant, st *groupStore,
	member, tableID string, b cardBinding) (bot.RunOption, bool) {
	c, err := t.App.GetCharacter(ctx, b.CardID)
	if err != nil {
		if apperr.IsCode(err, apperr.NotFound) {
			_ = st.update(func(gs *groupState) error {
				if per, ok := gs.Bindings[member]; ok {
					delete(per, tableID)
				}
				return nil
			})
		} else {
			h.logger.Printf("校验绑卡失败 member=%s card=%s：%v", member, b.CardID, err)
		}
		return nil, false
	}
	if c.Name != b.CardName {
		_ = st.update(func(gs *groupState) error {
			if per, ok := gs.Bindings[member]; ok {
				if cur, ok := per[tableID]; ok && cur.CardID == b.CardID {
					cur.CardName = c.Name
					cur.UpdatedAt = time.Now()
					per[tableID] = cur
				}
			}
			return nil
		})
	}
	return bot.WithDefaultCharacter(b.CardID), true
}

// memberLabel is a human-readable stand-in for a group member (the platform
// gives no nickname in the event): the card name bound on the current table
// when known, otherwise any of the member's bindings.
func memberLabel(st *groupStore, memberOpenid, tableID string) string {
	var label string
	st.read(func(gs *groupState) {
		label = labelOf(gs, memberOpenid, tableID)
	})
	return label
}

// labelOf is memberLabel for callers that already hold the state lock.
func labelOf(gs *groupState, memberOpenid, tableID string) string {
	if b, ok := anyBinding(gs, memberOpenid, tableID); ok && b.CardName != "" {
		return b.CardName
	}
	if len(memberOpenid) > 6 {
		return "成员" + memberOpenid[len(memberOpenid)-4:]
	}
	return "成员"
}

// isGroupPCCommand reports whether a pc invocation is one of the adapter
// subcommands handled here (tag/untag/grp) rather than chatcore's.
func isGroupPCCommand(fields []string) (string, bool) {
	if len(fields) < 2 {
		return "", false
	}
	name, _ := chatcore.StripCommandPrefix(fields[0])
	if name != "pc" {
		return "", false
	}
	switch fields[1] {
	case "tag", "untag", "grp":
		return fields[1], true
	}
	return "", false
}

// groupCommandName returns the normalized first token.
func groupCommandName(line string) string {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	name, _ := chatcore.StripCommandPrefix(fields[0])
	return name
}

// --- observers -------------------------------------------------------------

func (h *botHandler) runObserver(ctx context.Context, tenant *bot.Tenant, st *groupStore,
	ev messageEvent, args []string, reply func(string)) {
	if len(args) == 0 {
		reply("用法：ob join｜ob exit｜ob list｜ob on｜ob off")
		return
	}
	switch args[0] {
	case "join":
		if _, linked := st.link(ev.Author.MemberOpenid); !linked {
			code := h.binds.issue(pendingBind{GroupID: ev.GroupOpenid, Member: ev.Author.MemberOpenid, Observer: true})
			reply(fmt.Sprintf("围观需要先绑定私信：请私聊机器人发送「绑定 %s」（10 分钟内有效）", code))
			return
		}
		err := st.update(func(gs *groupState) error {
			gs.ObEnabled = true
			gs.Observers[ev.Author.MemberOpenid] = observerEntry{Enabled: true, UpdatedAt: time.Now()}
			return nil
		})
		if err != nil {
			reply("保存围观列表失败：" + err.Error())
			return
		}
		reply("已加入暗骰围观列表")
	case "exit":
		err := st.update(func(gs *groupState) error {
			delete(gs.Observers, ev.Author.MemberOpenid)
			return nil
		})
		if err != nil {
			reply("保存围观列表失败：" + err.Error())
			return
		}
		reply("已退出暗骰围观列表")
	case "list":
		var names []string
		st.read(func(gs *groupState) {
			for member, ob := range gs.Observers {
				if !ob.Enabled {
					continue
				}
				// The state lock is already held here, so use the
				// no-lock label helper (memberLabel would deadlock).
				label := labelOf(gs, member, storage.DefaultTableID)
				if _, linked := gs.Links[member]; !linked {
					label += "（未绑定私信）"
				}
				names = append(names, label)
			}
		})
		sort.Strings(names)
		if len(names) == 0 {
			reply("当前没有围观者")
			return
		}
		reply("围观者：" + strings.Join(names, "、") + fmt.Sprintf("（总开关 %s）", onOff(st)))
	case "on", "off":
		enabled := args[0] == "on"
		err := st.update(func(gs *groupState) error {
			gs.ObEnabled = enabled
			return nil
		})
		if err != nil {
			reply("保存失败：" + err.Error())
			return
		}
		reply("暗骰围观已" + map[bool]string{true: "开启", false: "关闭"}[enabled])
	default:
		reply("用法：ob join｜ob exit｜ob list｜ob on｜ob off")
	}
}

func onOff(st *groupStore) string {
	out := "关闭"
	st.read(func(gs *groupState) {
		if gs.ObEnabled {
			out = "开启"
		}
	})
	return out
}

// --- card binding ----------------------------------------------------------

func (h *botHandler) runPCBinding(ctx context.Context, tenant *bot.Tenant, st *groupStore,
	ev messageEvent, sub string, args []string, reply func(string)) {
	member := ev.Author.MemberOpenid
	tid := h.scopeTableID(ctx, tenant)
	sctx := storage.WithTable(ctx, tid)
	tableName := h.tableName(sctx, tenant, tid)
	switch sub {
	case "tag":
		ref := ""
		if len(args) > 0 {
			ref = args[0]
		}
		var id string
		var err error
		if ref == "" {
			id, err = tenant.App.ActiveCharacterID(sctx)
			if err != nil || id == "" {
				reply("请先指定角色卡：pc tag <名字>，或先用 pc use 选一张卡")
				return
			}
		} else {
			id, err = chatcore.ResolveCardRef(sctx, tenant.App, ref)
			if err != nil {
				reply(errMessage(err))
				return
			}
		}
		c, err := tenant.App.GetCharacter(sctx, id)
		if err != nil {
			reply(errMessage(err))
			return
		}
		err = st.update(func(gs *groupState) error {
			per := gs.Bindings[member]
			if per == nil {
				per = map[string]cardBinding{}
				gs.Bindings[member] = per
			}
			per[tid] = cardBinding{CardID: c.ID, CardName: c.Name, UpdatedAt: time.Now()}
			return nil
		})
		if err != nil {
			reply("保存绑卡失败：" + err.Error())
			return
		}
		reply(fmt.Sprintf("已在游戏「%s」绑定角色「%s」；该游戏上的 ra/st/sc 等指令默认使用这张卡", tableName, c.Name))
	case "untag":
		all := len(args) > 0 && args[0] == "all"
		err := st.update(func(gs *groupState) error {
			if all {
				delete(gs.Bindings, member)
				return nil
			}
			if per, ok := gs.Bindings[member]; ok {
				delete(per, tid)
				if len(per) == 0 {
					delete(gs.Bindings, member)
				}
			}
			return nil
		})
		if err != nil {
			reply("保存失败：" + err.Error())
			return
		}
		if all {
			reply("已解除所有游戏上的绑卡")
		} else {
			reply(fmt.Sprintf("已解除游戏「%s」的绑卡（其他游戏不受影响；pc untag all 解除全部）", tableName))
		}
	case "grp":
		list, err := tenant.App.ListGameTables(sctx)
		if err != nil {
			reply("读取游戏列表失败：" + err.Error())
			return
		}
		names := map[string]string{}
		for _, t := range list.Tables {
			names[t.ID] = t.Name
		}
		var lines []string
		st.read(func(gs *groupState) {
			for m, per := range gs.Bindings {
				for tableID, b := range per {
					name := names[tableID]
					if name == "" {
						name = tableID
					}
					suffix := ""
					if _, linked := gs.Links[m]; !linked {
						suffix = "（未绑定私信）"
					}
					lines = append(lines, fmt.Sprintf("[%s] %s → %s%s",
						name, labelOf(gs, m, tableID), b.CardName, suffix))
				}
			}
		})
		sort.Strings(lines)
		if len(lines) == 0 {
			reply("本群还没有人绑卡（pc tag <名字>；绑卡按游戏隔离）")
			return
		}
		reply("本群绑卡（按游戏）：\n" + strings.Join(lines, "\n"))
	}
}

// tableName returns the display name of a table, falling back to the id.
func (h *botHandler) tableName(ctx context.Context, tenant *bot.Tenant, tableID string) string {
	t, err := tenant.App.Table(ctx, tableID)
	if err != nil || t == nil {
		return tableID
	}
	return t.Name
}

func errMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// migrateLegacyLogs imports the Markdown sessions written by the old
// adapter into the shared log store. It runs once per group root; the
// adapter.json marker is written only after the whole import succeeds.
func (h *botHandler) migrateLegacyLogs(ctx context.Context, t *bot.Tenant, st *groupStore) error {
	return st.withLegacyLogMigration(func(legacy legacyLogState) error {
		n, err := t.App.ImportLegacyLogs(ctx, t.Root, app.LegacyLogSource{
			Active:    legacy.Active,
			File:      legacy.File,
			StartedAt: legacy.StartedAt,
		})
		if err == nil && n > 0 {
			h.logger.Printf("已导入旧版战报 %d 份 root=%s", n, t.Root)
		}
		return err
	})
}

// isCommandLine reports whether a group message starts a command, including
// adapter-only names (ob/bind), glued forms such as r3d6, and rulebook-declared
// generation aliases such as coc6d. Non-command chat is what a play log
// records as ordinary speech.
func isCommandLine(a *app.App, line string) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false
	}
	if _, _, _, ok := chatcore.SplitCommandToken(fields[0], "ob", "bind", "绑定", "绑定码"); ok {
		return true
	}
	name, _ := chatcore.StripCommandPrefix(fields[0])
	_, _, _, ok := chatcore.BookCommand(a, name)
	return ok
}

// sendLogFile uploads one exported log file and attaches it to the triggering
// message in either a group or a private chat. seq is the passive-reply
// sequence number.
func (h *botHandler) sendLogFile(ctx context.Context, scope, target, msgID, path string, seq int) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取日志失败：%w", err)
	}
	fileInfo, err := h.api.uploadFile(ctx, scope, target, filepath.Base(path), data)
	if err != nil {
		h.logger.Printf("日志上传失败 scope=%s target=%s：%v", scope, target, err)
		return fmt.Errorf("上传失败：%w", err)
	}
	if err := h.api.sendFileReply(ctx, scope, target, msgID, seq, fileInfo); err != nil {
		h.logger.Printf("日志发送失败 scope=%s target=%s：%v", scope, target, err)
		return fmt.Errorf("发送失败：%w", err)
	}
	return nil
}
