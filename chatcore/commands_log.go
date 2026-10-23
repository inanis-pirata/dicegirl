package chatcore

// Play-session logs (`.log`). The lifecycle is identical on the CLI and the
// chat adapters: Dice!Next's new/on/off/end/halt/list/get/del/stat/export/
// type/time surface. File delivery stays an adapter concern: the command
// writes the export and hands the path to the adapter's hook (or prints the
// path on the CLI).

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"dicegirl/internal/app"
)

const logUsage = "用法：log｜log new [名称]｜log on｜log off｜log end｜log halt｜log list [页码]｜log get [名称]｜log del <名称>｜log stat [名称]｜log export [名称]｜log type txt|html｜log timer [on|off]｜log time"

// cmdLog implements the shared `.log` command.
func cmdLog(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	sub := ""
	var rest []string
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
		rest = args[1:]
	}
	ref := strings.TrimSpace(strings.Join(rest, " "))
	actorID, actorName, _ := currentActor(ctx)

	switch sub {
	case "", "status":
		if sub == "status" {
			// `log status` was never part of Dice!Next's surface.
			fmt.Fprintln(stderr, logUsage)
			return 2
		}
		view, err := a.LogStatus(ctx)
		if err != nil {
			return PrintError(stderr, err)
		}
		return renderLogStatus(r, stderr, view)

	case "new":
		view, err := a.LogNew(ctx, ref, actorID, actorName)
		if err != nil {
			return PrintError(stderr, err)
		}
		if view.Current != nil {
			return renderLogAction(r, stderr, view,
				fmt.Sprintf("跑团记录《%s》已开始；用 log off 暂停、log end 结束", view.Current.Name))
		}
		return renderLogStatus(r, stderr, view)

	case "on":
		if len(rest) != 0 {
			fmt.Fprintln(stderr, logUsage)
			return 2
		}
		view, err := a.LogOn(ctx)
		if err != nil {
			return PrintError(stderr, err)
		}
		if view.Current != nil {
			return renderLogAction(r, stderr, view,
				fmt.Sprintf("已继续记录《%s》（%s，%d 条）",
					view.Current.Name, view.Current.Duration, view.Current.Entries))
		}
		return renderLogStatus(r, stderr, view)

	case "off":
		if len(rest) != 0 {
			fmt.Fprintln(stderr, logUsage)
			return 2
		}
		view, err := a.LogOff(ctx)
		if err != nil {
			return PrintError(stderr, err)
		}
		if view.Current != nil {
			return renderLogAction(r, stderr, view,
				fmt.Sprintf("已暂停《%s》（累计 %s，%d 条）；log on 继续，log end 结束",
					view.Current.Name, view.Current.Duration, view.Current.Entries))
		}
		return renderLogStatus(r, stderr, view)

	case "end":
		if len(rest) != 0 {
			fmt.Fprintln(stderr, logUsage)
			return 2
		}
		summary, err := a.LogEnd(ctx, false)
		if err != nil {
			return PrintError(stderr, err)
		}
		export, err := a.LogExport(ctx, summary.ID)
		if err != nil {
			return PrintError(stderr, err)
		}
		return renderLogExport(ctx, r, stderr, export,
			fmt.Sprintf("跑团记录《%s》已结束（%s，%d 条）", summary.Name, summary.Duration, summary.Entries))

	case "halt":
		if len(rest) != 0 {
			fmt.Fprintln(stderr, logUsage)
			return 2
		}
		summary, err := a.LogEnd(ctx, true)
		if err != nil {
			return PrintError(stderr, err)
		}
		return PrintError(stderr, r.SimpleMessage(
			fmt.Sprintf("已中止《%s》（%s，%d 条），未导出文件", summary.Name, summary.Duration, summary.Entries)))

	case "list":
		page := 1
		if len(rest) > 1 {
			fmt.Fprintln(stderr, logUsage)
			return 2
		}
		if len(rest) == 1 {
			n, err := strconv.Atoi(rest[0])
			if err != nil || n <= 0 {
				fmt.Fprintln(stderr, "页码必须是正整数")
				return 2
			}
			page = n
		}
		list, err := a.LogList(ctx, page)
		if err != nil {
			return PrintError(stderr, err)
		}
		return renderLogList(r, stderr, list)

	case "get", "export":
		export, err := a.LogExport(ctx, ref)
		if err != nil {
			return PrintError(stderr, err)
		}
		if sub == "get" {
			return renderLogExport(ctx, r, stderr, export,
				fmt.Sprintf("跑团记录《%s》已重新导出（%s，%d 条）",
					export.Log.Name, export.Format, export.Log.Entries))
		}
		return renderLogExport(ctx, r, stderr, export,
			fmt.Sprintf("跑团记录《%s》已导出（%s，%d 条）",
				export.Log.Name, export.Format, export.Log.Entries))

	case "del":
		if ref == "" {
			fmt.Fprintln(stderr, "用法：log del <名称|id>")
			return 2
		}
		summary, err := a.LogDelete(ctx, ref)
		if err != nil {
			return PrintError(stderr, err)
		}
		return PrintError(stderr, r.SimpleMessage(
			fmt.Sprintf("已删除跑团记录《%s》（%d 条）", summary.Name, summary.Entries)))

	case "stat":
		stat, err := a.LogStat(ctx, ref)
		if err != nil {
			return PrintError(stderr, err)
		}
		return renderLogStat(r, stderr, stat)

	case "type":
		if len(rest) != 1 {
			fmt.Fprintln(stderr, "用法：log type txt|html")
			return 2
		}
		format := strings.ToLower(rest[0])
		if format != "txt" && format != "html" {
			fmt.Fprintln(stderr, "日志格式只能是 txt 或 html")
			return 2
		}
		if !requireLogManager(ctx, a, stderr) {
			return 1
		}
		settings, err := a.LogSetType(ctx, format)
		if err != nil {
			return PrintError(stderr, err)
		}
		return PrintError(stderr, r.SimpleMessage(
			fmt.Sprintf("日志导出格式已设为 %s", settings.Format)))

	case "timer":
		if len(rest) > 1 {
			fmt.Fprintln(stderr, "用法：log timer on|off")
			return 2
		}
		if len(rest) == 0 {
			view, err := a.LogStatus(ctx)
			if err != nil {
				return PrintError(stderr, err)
			}
			state := "关闭"
			if view.Timer {
				state = "开启"
			}
			return PrintError(stderr, r.SimpleMessage("跑团计时："+state))
		}
		on := false
		switch strings.ToLower(rest[0]) {
		case "on", "开", "开启":
			on = true
		case "off", "关", "关闭":
			on = false
		default:
			fmt.Fprintln(stderr, "用法：log timer on|off")
			return 2
		}
		if !requireLogManager(ctx, a, stderr) {
			return 1
		}
		if _, err := a.LogSetTimer(ctx, on); err != nil {
			return PrintError(stderr, err)
		}
		state := "关闭"
		if on {
			state = "开启"
		}
		return PrintError(stderr, r.SimpleMessage("跑团计时已"+state))

	case "time":
		if len(rest) != 0 {
			fmt.Fprintln(stderr, logUsage)
			return 2
		}
		view, err := a.LogStatus(ctx)
		if err != nil {
			return PrintError(stderr, err)
		}
		return renderLogTime(r, stderr, view)
	}
	fmt.Fprintln(stderr, logUsage)
	return 2
}

// requireLogManager checks the group-management permission for `log type`
// and `log timer`. CLI callers are always administrators; chat adapters fall
// back to the current game's GM roster because QQ events carry no role flag.
func requireLogManager(ctx context.Context, a *app.App, stderr io.Writer) bool {
	actorID, _, admin := currentActor(ctx)
	if admin {
		return true
	}
	gm, err := a.IsCurrentGameGM(ctx, actorID)
	if err != nil {
		_ = PrintError(stderr, err)
		return false
	}
	if !gm {
		fmt.Fprintln(stderr, "需要当前游戏 GM（CLI 管理员可直接修改）才能修改日志设置")
		return false
	}
	return true
}

func renderLogStatus(r *Renderer, stderr io.Writer, view *app.LogStatusView) int {
	if r.JSON {
		if err := r.render(view, nil); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	}
	message := logStatusText(view)
	return PrintError(stderr, r.SimpleMessage(message))
}

// renderLogAction emits the structured status in JSON mode and a short
// acknowledgement in text mode.
func renderLogAction(r *Renderer, stderr io.Writer, view *app.LogStatusView, message string) int {
	if r.JSON {
		return renderLogStatus(r, stderr, view)
	}
	return PrintError(stderr, r.SimpleMessage(message))
}

func renderLogTime(r *Renderer, stderr io.Writer, view *app.LogStatusView) int {
	if r.JSON {
		if err := r.render(view, nil); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	}
	if view.Current == nil {
		return PrintError(stderr, r.SimpleMessage("当前没有进行中的跑团记录"))
	}
	return PrintError(stderr, r.SimpleMessage(
		fmt.Sprintf("《%s》累计跑团时长：%s", view.Current.Name, view.Current.Duration)))
}

func renderLogExport(ctx context.Context, r *Renderer, stderr io.Writer, export *app.LogExport, plain string) int {
	if r.JSON {
		if err := r.render(export, nil); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	}
	if notifyLogFile(ctx, export.Path) {
		return PrintError(stderr, r.SimpleMessage(
			fmt.Sprintf("%s，已导出：%s（%s）", plain, export.Filename, export.Format)))
	}
	return PrintError(stderr, r.SimpleMessage(
		fmt.Sprintf("%s，已导出到：%s", plain, export.Path)))
}

func renderLogList(r *Renderer, stderr io.Writer, list *app.LogList) int {
	if r.JSON {
		if err := r.render(list, nil); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	}
	return PrintError(stderr, r.SimpleMessage(logListText(list)))
}

func renderLogStat(r *Renderer, stderr io.Writer, stat *app.LogStat) int {
	if r.JSON {
		if err := r.render(stat, nil); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	}
	return PrintError(stderr, r.SimpleMessage(logStatText(stat)))
}
