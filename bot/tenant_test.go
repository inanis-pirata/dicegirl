package bot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	m := NewManager(t.TempDir())
	m.maxOpen = 2 // tiny limit to exercise eviction
	t.Cleanup(m.CloseAll)
	return m
}

func TestRunLineAndScopeIsolation(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()

	// A command in group A creates a character there.
	res, err := m.RunLine(ctx, ScopeGroup, "group-openid-aaaa1111", ".pc new 张三")
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != 0 {
		t.Fatalf("create code=%d err=%s", res.Code, res.Err)
	}

	// Group A sees exactly that card.
	res, err = m.RunLine(ctx, ScopeGroup, "group-openid-aaaa1111", ".pc list")
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != 0 || !strings.Contains(res.Out, "张三") {
		t.Fatalf("list A = (%d, %q, %q)", res.Code, res.Out, res.Err)
	}

	// Group B and a C2C scope have empty rosters: independent data roots.
	for _, scope := range [][2]string{
		{ScopeGroup, "group-openid-bbbb2222"},
		{ScopeC2C, "user-openid-cccc3333"},
	} {
		res, err = m.RunLine(ctx, scope[0], scope[1], ".pc list")
		if err != nil {
			t.Fatal(err)
		}
		if res.Code != 0 {
			t.Fatalf("list %v code=%d err=%s", scope, res.Code, res.Err)
		}
		if strings.Contains(res.Out, "张三") {
			t.Fatalf("card leaked into scope %v: %q", scope, res.Out)
		}
	}

	// Each scope owns a database under the base root.
	if _, err = m.Get(ctx, ScopeGroup, "group-openid-aaaa1111"); err != nil {
		t.Fatal(err)
	}
	if path := filepath.Join(m.baseRoot, "groups", "group-openid-aaaa1111", "dicegirl.db"); !fileExists(path) {
		t.Fatalf("scope database missing at %s", path)
	}
}

func TestRunLineUnrecognized(t *testing.T) {
	m := newTestManager(t)
	res, err := m.RunLine(context.Background(), ScopeGroup, "group-openid-aaaa1111", "大家好啊")
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != -1 {
		t.Fatalf("want Unrecognized(-1), got %d", res.Code)
	}
}

func TestStateIsolationBetweenGroups(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()

	// Active character is per-scope local state.
	if _, err := m.RunLine(ctx, ScopeGroup, "group-openid-aaaa1111", ".pc new 卡卡 --active"); err != nil {
		t.Fatal(err)
	}
	resA, err := m.RunLine(ctx, ScopeGroup, "group-openid-aaaa1111", ".pc active")
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(resA.Out)
	if !strings.HasPrefix(id, "c-") {
		t.Fatalf("active in A = %q, want card id", resA.Out)
	}

	res, err := m.RunLine(ctx, ScopeGroup, "group-openid-bbbb2222", ".pc active")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Out, id) {
		t.Fatalf("active card leaked into group B: %q", res.Out)
	}
}

func TestUnsafeIDIsHashed(t *testing.T) {
	got := dirName(ScopeGroup, "../escape")
	if strings.Contains(got, "..") {
		t.Fatalf("unsafe id not hashed: %s", got)
	}
	if !strings.HasPrefix(got, filepath.Join(ScopeGroup, "sha256-")) {
		t.Fatalf("unexpected dir name %s", got)
	}
}

func TestAutoApplyOption(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()
	scope := [2]string{ScopeC2C, "user-openid-cccc3333"}

	if _, err := m.RunLine(ctx, scope[0], scope[1], ".coc --active"); err != nil {
		t.Fatal(err)
	}
	// Target 0: any roll fails, so the failure loss (1) always applies.
	line := ".sc 0/1 0 暗骰联调"

	res, err := m.RunLine(ctx, scope[0], scope[1], line) // CLI semantics
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != 0 || !strings.Contains(res.Out, "未写回") {
		t.Fatalf("default should not write back: code=%d out=%q err=%q", res.Code, res.Out, res.Err)
	}

	res, err = m.RunLine(ctx, scope[0], scope[1], line, WithAutoApply()) // bot semantics
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != 0 || !strings.Contains(res.Out, "已保存") {
		t.Fatalf("WithAutoApply should write back: code=%d out=%q err=%q", res.Code, res.Out, res.Err)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
