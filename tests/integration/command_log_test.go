package integration

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestLogLifecycle covers the shared Dice!Next `.log` surface: start, pause,
// resume, statistics, export, end and delete. Commands executed while the log
// is active are recorded together with their rendered output.
func TestLogLifecycle(t *testing.T) {
	root := t.TempDir()

	code, out, errs := runCLI(t, root, ".log")
	if code != 0 || !strings.Contains(out, "还没有跑团记录") {
		t.Fatalf("empty log code=%d out=%q err=%s", code, out, errs)
	}
	// Old dicegirl lifecycle names are not kept as aliases.
	for _, old := range []string{"start", "status", "show"} {
		if code, _, _ := runCLI(t, root, ".log", old); code == 0 {
			t.Fatalf("old .log subcommand %q should be rejected", old)
		}
	}

	code, out, errs = runCLI(t, root, ".log", "new", "第一章")
	if code != 0 || !strings.Contains(out, "第一章") || !strings.Contains(out, "已开始") {
		t.Fatalf("log new code=%d out=%q err=%s", code, out, errs)
	}

	// Only one session may be in progress.
	code, _, errs = runCLI(t, root, ".log", "new", "第二章")
	if code == 0 || !strings.Contains(errs, "CONFLICT") {
		t.Fatalf("second new code=%d err=%q", code, errs)
	}

	// A command executed while recording is stored with its output.
	code, out, errs = runCLI(t, root, ".r", "1d6")
	if code != 0 {
		t.Fatalf("roll code=%d err=%s", code, errs)
	}
	roll := strings.TrimSpace(out)

	code, out, errs = runCLI(t, root, ".log", "off")
	if code != 0 || !strings.Contains(out, "已暂停") {
		t.Fatalf("log off code=%d out=%q err=%s", code, out, errs)
	}

	// A paused log records nothing.
	if code, _, errs := runCLI(t, root, ".r", "1d100"); code != 0 {
		t.Fatalf("paused roll code=%d err=%s", code, errs)
	}

	code, out, _ = runCLI(t, root, ".log", "stat")
	if code != 0 || !strings.Contains(out, "条目：1") || !strings.Contains(out, "本地用户") {
		t.Fatalf("stat while paused code=%d out=%q", code, out)
	}

	code, out, errs = runCLI(t, root, ".log", "on")
	if code != 0 || !strings.Contains(out, "已继续记录") {
		t.Fatalf("log on code=%d out=%q err=%s", code, out, errs)
	}
	if code, _, errs := runCLI(t, root, ".r", "1d6"); code != 0 {
		t.Fatalf("resumed roll code=%d err=%s", code, errs)
	}

	code, out, _ = runCLI(t, root, ".log", "stat")
	if code != 0 || !strings.Contains(out, "条目：2") || !strings.Contains(out, "参与者：1 人") {
		t.Fatalf("stat after resume code=%d out=%q", code, out)
	}

	code, out, _ = runCLI(t, root, ".log", "list")
	if code != 0 || !strings.Contains(out, "第一章") || !strings.Contains(out, "记录中") {
		t.Fatalf("list code=%d out=%q", code, out)
	}

	// JSON export exposes the file path; the file contains the recorded
	// command and its rendered result.
	code, out, errs = runCLI(t, root, "--json", ".log", "export")
	if code != 0 {
		t.Fatalf("export code=%d err=%s", code, errs)
	}
	var exported struct {
		Path     string `json:"path"`
		Filename string `json:"filename"`
		Format   string `json:"format"`
	}
	if err := json.Unmarshal([]byte(out), &exported); err != nil {
		t.Fatalf("export json %q: %v", out, err)
	}
	if exported.Format != "txt" || !strings.HasSuffix(exported.Filename, ".txt") {
		t.Fatalf("export payload = %+v", exported)
	}
	raw, err := os.ReadFile(exported.Path)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	text := string(raw)
	for _, want := range []string{"第一章", "本地用户", ".r 1d6", roll, "条目：2"} {
		if !strings.Contains(text, want) {
			t.Fatalf("export missing %q:\n%s", want, text)
		}
	}

	code, out, errs = runCLI(t, root, ".log", "end")
	if code != 0 || !strings.Contains(out, "已结束") {
		t.Fatalf("end code=%d out=%q err=%s", code, out, errs)
	}
	code, out, _ = runCLI(t, root, ".log", "list")
	if code != 0 || !strings.Contains(out, "第一章") || !strings.Contains(out, "已结束") {
		t.Fatalf("list after end code=%d out=%q", code, out)
	}
	code, out, _ = runCLI(t, root, ".log")
	if code != 0 || !strings.Contains(out, "当前没有进行中的跑团记录") ||
		!strings.Contains(out, "最近一份《第一章》已结束") {
		t.Fatalf("status after end code=%d out=%q", code, out)
	}

	code, out, errs = runCLI(t, root, ".log", "del", "第一章")
	if code != 0 || !strings.Contains(out, "已删除") {
		t.Fatalf("del code=%d out=%q err=%s", code, out, errs)
	}
	if _, err := os.Stat(exported.Path); !os.IsNotExist(err) {
		t.Fatalf("export file should be deleted with the log: %v", err)
	}
	code, out, _ = runCLI(t, root, ".log", "list")
	if code != 0 || !strings.Contains(out, "还没有跑团记录") {
		t.Fatalf("list after del code=%d out=%q", code, out)
	}
}

// TestLogSettingsAndHalt covers format selection, the play timer and the
// no-export halt path.
func TestLogSettingsAndHalt(t *testing.T) {
	root := t.TempDir()

	code, out, errs := runCLI(t, root, ".log", "type", "html")
	if code != 0 || !strings.Contains(out, "html") {
		t.Fatalf("type html code=%d out=%q err=%s", code, out, errs)
	}
	if code, _, errs := runCLI(t, root, ".log", "type", "pdf"); code == 0 ||
		!strings.Contains(errs, "日志格式只能是") {
		t.Fatalf("type pdf code=%d err=%q", code, errs)
	}
	code, out, _ = runCLI(t, root, ".log", "timer", "on")
	if code != 0 || !strings.Contains(out, "开启") {
		t.Fatalf("timer on code=%d out=%q", code, out)
	}

	if code, _, errs := runCLI(t, root, ".log", "new", "网页团"); code != 0 {
		t.Fatalf("new html log code=%d err=%s", code, errs)
	}
	code, out, _ = runCLI(t, root, "--json", ".log", "export")
	if code != 0 {
		t.Fatalf("html export code=%d", code)
	}
	var exported struct {
		Path   string `json:"path"`
		Format string `json:"format"`
	}
	if err := json.Unmarshal([]byte(out), &exported); err != nil {
		t.Fatalf("export json: %v", err)
	}
	if exported.Format != "html" || !strings.HasSuffix(exported.Path, ".html") {
		t.Fatalf("html export = %+v", exported)
	}
	raw, err := os.ReadFile(exported.Path)
	if err != nil || !strings.Contains(string(raw), "<!DOCTYPE html>") {
		t.Fatalf("html content: %v %q", err, string(raw))
	}

	code, out, _ = runCLI(t, root, ".log", "time")
	if code != 0 || !strings.Contains(out, "累计跑团时长") {
		t.Fatalf("time code=%d out=%q", code, out)
	}
	code, out, errs = runCLI(t, root, ".log", "halt")
	if code != 0 || !strings.Contains(out, "已中止") || !strings.Contains(out, "未导出") {
		t.Fatalf("halt code=%d out=%q err=%s", code, out, errs)
	}
	code, out, _ = runCLI(t, root, ".log", "list")
	if code != 0 || !strings.Contains(out, "已中止") {
		t.Fatalf("list after halt code=%d out=%q", code, out)
	}
}
