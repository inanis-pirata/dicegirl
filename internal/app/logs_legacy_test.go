package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dicegirl/internal/config"
	"dicegirl/internal/storage"
)

func newTestApp(t *testing.T, root string) *App {
	t.Helper()
	cfg := config.Default()
	cfg.DataRoot = root
	if err := cfg.Resolve(""); err != nil {
		t.Fatal(err)
	}
	a, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func writeLegacyLog(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "logs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestImportLegacyLogs(t *testing.T) {
	root := t.TempDir()
	a := newTestApp(t, root)

	writeLegacyLog(t, root, "20260102-150405.md", `# 第一章

开始：2026-01-02 15:04:05

### 15:04:05 · 张三

> .ra 心理学

    目标 60 → 成功

### 15:05:00 · 李四

> .r 1d6

    4

`)
	writeLegacyLog(t, root, "20260103-090000.md", `# 第二章

开始：2026-01-03 09:00:00

### 09:00:05 · 王五

> .r 1d20

    17

`)
	started := time.Date(2026, 1, 3, 9, 0, 0, 0, time.Local)
	imported, err := a.ImportLegacyLogs(context.Background(), root, LegacyLogSource{
		Active:    true,
		File:      filepath.Join("logs", "20260103-090000.md"),
		StartedAt: started,
	})
	if err != nil {
		t.Fatal(err)
	}
	if imported != 2 {
		t.Fatalf("imported = %d, want 2", imported)
	}

	logs, err := a.logs.Logs(context.Background())
	if err != nil || len(logs) != 2 {
		t.Fatalf("logs = %d, err = %v", len(logs), err)
	}
	byName := map[string]*storage.LogSession{}
	for _, l := range logs {
		byName[l.Name] = l
	}
	first := byName["第一章"]
	second := byName["第二章"]
	if first == nil || second == nil {
		t.Fatalf("imported names = %+v", byName)
	}
	if first.Status != storage.LogStatusEnded || first.Entries != 2 {
		t.Fatalf("first = %+v", first)
	}
	if second.Status != storage.LogStatusActive || second.Entries != 1 {
		t.Fatalf("second = %+v", second)
	}
	if !second.EndedAt.IsZero() {
		t.Fatalf("active legacy log has an end time: %+v", second)
	}
	if !second.CreatedAt.Equal(started.UTC()) {
		t.Fatalf("second created = %s, want %s", second.CreatedAt, started.UTC())
	}

	entries, err := a.logs.LogEntries(context.Background(), first.ID)
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries = %d, err = %v", len(entries), err)
	}
	if entries[0].Speaker != "张三" || entries[0].Text != ".ra 心理学" ||
		!strings.Contains(entries[0].Result, "目标 60 → 成功") {
		t.Fatalf("first entry = %+v", entries[0])
	}

	export, err := a.LogExport(context.Background(), "第一章")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(export.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"第一章", "张三", ".ra 心理学", "目标 60 → 成功"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("export missing %q:\n%s", want, raw)
		}
	}

	// The source files (including the directory entry) are preserved and a
	// second import is a no-op.
	if _, err := os.Stat(filepath.Join(root, "logs", "20260102-150405.md")); err != nil {
		t.Fatalf("legacy source removed: %v", err)
	}
	again, err := a.ImportLegacyLogs(context.Background(), root, LegacyLogSource{
		Active: true,
		File:   "logs/20260103-090000.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if again != 0 {
		t.Fatalf("second import = %d, want 0", again)
	}
}

func TestImportLegacyLogsKeepsExistingCurrent(t *testing.T) {
	root := t.TempDir()
	a := newTestApp(t, root)
	writeLegacyLog(t, root, "20260102-150405.md", `# 旧战报

开始：2026-01-02 15:04:05

### 15:04:05 · 张三

> .r 1d6

    4
`)
	if _, err := a.LogNew(context.Background(), "新日志", "u1", "用户"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ImportLegacyLogs(context.Background(), root, LegacyLogSource{
		Active: true,
		File:   "logs/20260102-150405.md",
	}); err != nil {
		t.Fatal(err)
	}
	status, err := a.LogStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Current == nil || status.Current.Name != "新日志" {
		t.Fatalf("current = %+v", status.Current)
	}
	logs, err := a.logs.Logs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range logs {
		if l.Name == "旧战报" && l.Status != storage.LogStatusEnded {
			t.Fatalf("legacy active should become ended: %+v", l)
		}
	}
}

func TestParseLegacyLogCrossesMidnight(t *testing.T) {
	parsed, ok := parseLegacyLog(`# 通宵团

开始：2026-01-02 23:55:00

### 23:58:00 · GM

> .r 1d6

    3

### 00:02:00 · GM

> .r 1d6

    5
`, "fallback", time.Now())
	if !ok {
		t.Fatal("legacy log not recognized")
	}
	if len(parsed.Entries) != 2 {
		t.Fatalf("entries = %d", len(parsed.Entries))
	}
	if got := parsed.Entries[1].At.Day(); got != 3 {
		t.Fatalf("second entry day = %d, want 3", got)
	}
}
