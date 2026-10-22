package main

// Group-facing adapter commands that only exist on a chat platform:
//
//	ob join | ob exit | ob list | ob on | ob off   hidden-roll observers
//	pc tag [名字|id] | pc untag | pc grp            per-member card binding
//	log start [标题] | log stop | log status | log show [n] | log list
//
// They are handled before chatcore because they need platform state; all
// game commands still go through the shared chat grammar.

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
)

// memberLabel is a human-readable stand-in for a group member (the platform
// gives no nickname in the event): the bound card's name when known.
func memberLabel(st *groupStore, memberOpenid string) string {
	var label string
	st.read(func(gs *groupState) {
		label = labelOf(gs, memberOpenid)
	})
	return label
}

// labelOf is memberLabel for callers that already hold the state lock.
func labelOf(gs *groupState, memberOpenid string) string {
	if b, ok := gs.Bindings[memberOpenid]; ok && b.CardName != "" {
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
	name, _ := chatcore.ShortToken(fields[0])
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
	name, _ := chatcore.ShortToken(fields[0])
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
				label := memberLabel(st, member)
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
	switch sub {
	case "tag":
		ref := ""
		if len(args) > 0 {
			ref = args[0]
		}
		var id string
		var err error
		if ref == "" {
			id, err = tenant.App.ActiveCharacterID(ctx)
			if err != nil {
				reply("请先指定角色卡：pc tag <名字>，或先用 pc use 选一张卡")
				return
			}
		} else {
			id, err = chatcore.ResolveCardRef(ctx, tenant.App, ref)
			if err != nil {
				reply(errMessage(err))
				return
			}
		}
		c, err := tenant.App.GetCharacter(ctx, id)
		if err != nil {
			reply(errMessage(err))
			return
		}
		err = st.update(func(gs *groupState) error {
			gs.Bindings[member] = cardBinding{CardID: c.ID, CardName: c.Name, UpdatedAt: time.Now()}
			return nil
		})
		if err != nil {
			reply("保存绑卡失败：" + err.Error())
			return
		}
		reply(fmt.Sprintf("已在本群绑定角色「%s」；之后的 ra/st/sc 等指令默认使用这张卡", c.Name))
	case "untag":
		err := st.update(func(gs *groupState) error {
			delete(gs.Bindings, member)
			return nil
		})
		if err != nil {
			reply("保存失败：" + err.Error())
			return
		}
		reply("已解除本群绑卡")
	case "grp":
		var lines []string
		st.read(func(gs *groupState) {
			for m, b := range gs.Bindings {
				suffix := ""
				if _, linked := gs.Links[m]; !linked {
					suffix = "（未绑定私信）"
				}
				lines = append(lines, fmt.Sprintf("%s → %s%s", labelOf(gs, m), b.CardName, suffix))
			}
		})
		sort.Strings(lines)
		if len(lines) == 0 {
			reply("本群还没有人绑卡（pc tag <名字>）")
			return
		}
		reply("本群绑卡：\n" + strings.Join(lines, "\n"))
	}
}

func errMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// --- session log -----------------------------------------------------------

// runLog implements `.log`. Entries are appended by runNormal whenever a
// session is active.
func (h *botHandler) runLog(ctx context.Context, tenant *bot.Tenant, st *groupStore,
	ev messageEvent, args []string, reply func(string)) {
	if len(args) == 0 {
		reply("用法：log start [标题]｜log stop｜log status｜log show [行数]｜log list")
		return
	}
	switch args[0] {
	case "start":
		title := strings.TrimSpace(strings.Join(args[1:], " "))
		if title == "" {
			title = "跑团记录 " + time.Now().Format("2006-01-02")
		}
		now := time.Now()
		rel := filepath.Join("logs", now.Format("20060102-150405")+".md")
		path := filepath.Join(tenant.Root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			reply("创建战报失败：" + err.Error())
			return
		}
		header := fmt.Sprintf("# %s\n\n开始：%s\n\n", title, now.Format("2006-01-02 15:04:05"))
		if err := os.WriteFile(path, []byte(header), 0o600); err != nil {
			reply("创建战报失败：" + err.Error())
			return
		}
		err := st.update(func(gs *groupState) error {
			gs.Log = logState{Active: true, Title: title, File: rel, StartedAt: now}
			return nil
		})
		if err != nil {
			reply("保存战报状态失败：" + err.Error())
			return
		}
		reply(fmt.Sprintf("战报已开始记录：《%s》。用 log stop 结束，log show 查看", title))
	case "stop":
		var title string
		err := st.update(func(gs *groupState) error {
			if !gs.Log.Active {
				return fmt.Errorf("当前没有进行中的战报")
			}
			title = gs.Log.Title
			gs.Log.Active = false
			return nil
		})
		if err != nil {
			reply(err.Error())
			return
		}
		// The file itself is the result; on failure fall back to a notice.
		if ok, msg := h.sendLogFile(ctx, ev, tenant, st, ""); !ok {
			reply(fmt.Sprintf("战报《%s》已停止，但文件发送失败：%s", title, msg))
		}
	case "status":
		var out string
		st.read(func(gs *groupState) {
			if !gs.Log.Active {
				out = "当前没有进行中的战报（log start [标题] 开始）"
				return
			}
			out = fmt.Sprintf("战报《%s》记录中：%d 条，开始于 %s",
				gs.Log.Title, gs.Log.Entries, gs.Log.StartedAt.Format("15:04"))
		})
		reply(out)
	case "show", "file", "export":
		// A file keeps any length in one place; text is only the fallback.
		name := ""
		if len(args) > 1 && args[0] != "show" {
			name = args[1]
		}
		if ok, msg := h.sendLogFile(ctx, ev, tenant, st, name); ok {
			return
		} else if name != "" {
			reply(msg)
			return
		}
		lines := 30
		if len(args) > 1 {
			if n, err := parseInt(args[1]); err == nil && n > 0 {
				lines = n
			}
		}
		content, path := currentLog(st, tenant.Root)
		if path == "" {
			reply("当前没有战报文件（log start 开始记录）")
			return
		}
		reply(tailLines(content, lines) + "\n\n（文件发送失败，以上为末尾预览）")
	case "list":
		dir := filepath.Join(tenant.Root, "logs")
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) == 0 {
			reply("还没有战报文件")
			return
		}
		var names []string
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				names = append(names, e.Name())
			}
		}
		sort.Sort(sort.Reverse(sort.StringSlice(names)))
		reply("战报文件（服务器路径 " + dir + "）：\n" + strings.Join(names, "\n"))
	default:
		reply("用法：log start [标题]｜log stop｜log status｜log show [行数]｜log list")
	}
}

// sendLogFile uploads the current (or named) log file and attaches it to the
// triggering message as a passive file reply. ok=false carries a reason for
// the caller's text fallback.
func (h *botHandler) sendLogFile(ctx context.Context, ev messageEvent, tenant *bot.Tenant,
	st *groupStore, name string) (bool, string) {
	path, err := logFilePath(tenant.Root, st, name)
	if err != nil {
		return false, err.Error()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, "读取战报失败：" + err.Error()
	}
	fileInfo, err := h.api.uploadFile(ctx, botScopeGroup, ev.GroupOpenid, filepath.Base(path), data)
	if err != nil {
		h.logger.Printf("战报上传失败：%v", err)
		return false, "上传失败：" + err.Error()
	}
	if err := h.api.sendFileReply(ctx, botScopeGroup, ev.GroupOpenid, ev.ID, 1, fileInfo); err != nil {
		h.logger.Printf("战报发送失败：%v", err)
		return false, "发送失败：" + err.Error()
	}
	return true, ""
}

// logFilePath resolves the log file to send: an explicit name must be a
// plain .md file inside the group's logs directory; an empty name selects
// the current (or most recent) session log.
func logFilePath(root string, st *groupStore, name string) (string, error) {
	logsDir := filepath.Join(root, "logs")
	if name == "" {
		var rel string
		st.read(func(gs *groupState) { rel = gs.Log.File })
		if rel == "" {
			return "", fmt.Errorf("当前没有战报文件（log start 开始记录）")
		}
		return filepath.Join(root, rel), nil
	}
	if name != filepath.Base(name) || !strings.HasSuffix(name, ".md") {
		return "", fmt.Errorf("无效的战报文件名：%s", name)
	}
	return filepath.Join(logsDir, name), nil
}

// appendLogEntry records one executed command while a session is active.
func appendLogEntry(st *groupStore, root, label, line, out string) {
	_ = st.update(func(gs *groupState) error {
		if !gs.Log.Active {
			return nil
		}
		path := filepath.Join(root, gs.Log.File)
		var b strings.Builder
		fmt.Fprintf(&b, "### %s · %s\n\n", time.Now().Format("15:04:05"), label)
		fmt.Fprintf(&b, "> %s\n\n", line)
		for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			b.WriteString("    ")
			b.WriteString(l)
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		if _, err := f.WriteString(b.String()); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		gs.Log.Entries++
		return nil
	})
}

func currentLog(st *groupStore, root string) (string, string) {
	var rel string
	st.read(func(gs *groupState) {
		if gs.Log.Active {
			rel = gs.Log.File
		}
	})
	if rel == "" {
		return "", ""
	}
	path := filepath.Join(root, rel)
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	return string(raw), path
}

// tailLines returns the last n lines of s.
func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscan(s, &n)
	return n, err
}
