package integration

import (
	"strings"
	"testing"
)

// The rulebook ↔ command contract: resource tracks declared in a rulebook
// (vitals names, resource checks, command bindings) are reachable from
// every name-taking command — the sheet-vocabulary ladder is rulebook
// data, and the verbs address bound targets only.
func TestResourceVocabularyCommands(t *testing.T) {
	root := t.TempDir()
	createInvestigator(t, root, "调查员") // POW 55 → san track 55/99

	// ra understands the resource rung: the track's current value is the
	// target, difficulties apply, and the raw track id resolves too.
	code, out, errs := runCLI(t, root, "ra", "理智")
	if code != 0 || !strings.Contains(out, "[理智] 目标 55") {
		t.Fatalf("ra 理智 code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, ".ra", "困难理智")
	if code != 0 || !strings.Contains(out, "理智（困难）") || !strings.Contains(out, "基础 55") {
		t.Fatalf("ra 困难理智 code=%d out=%q err=%s", code, out, errs)
	}
	code, _, errs = runCLI(t, root, "ra", "san")
	if code != 0 {
		t.Fatalf("ra san code=%d err=%s", code, errs)
	}

	// st show / st ± address the track through its declared names.
	code, out, errs = runCLI(t, root, "st", "show", "理智")
	if code != 0 || !strings.Contains(out, "san 55/99") {
		t.Fatalf("st show 理智 code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "st", "理智-1")
	if code != 0 || !strings.Contains(out, "san 55 → 54") {
		t.Fatalf("st 理智-1 code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "st", "san=40")
	if code != 0 || !strings.Contains(out, "san 40/99") {
		t.Fatalf("st san=40 code=%d out=%q err=%s", code, out, errs)
	}

	// A hand-built card without tracks falls back to the declared formula
	// chain (san: POW) instead of failing to resolve.
	code, _, errs = runCLI(t, root, "pc", "new", "无轨", "POW=50", "STR=60")
	if code != 0 {
		t.Fatalf("pc new 无轨 code=%d err=%s", code, errs)
	}
	code, out, errs = runCLI(t, root, "ra", "理智", "--character", "无轨")
	if code != 0 || !strings.Contains(out, "[理智] 目标 50") {
		t.Fatalf("ra 理智 fallback code=%d out=%q err=%s", code, out, errs)
	}

	// The sc/ti/li verbs address their rulebook bindings (command_bindings).
	code, out, errs = runCLI(t, root, "sc", "0/1")
	if code != 0 || !strings.Contains(out, "[理智] 目标") {
		t.Fatalf("sc code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "ti")
	if code != 0 || !strings.Contains(out, "临时疯狂症状") {
		t.Fatalf("ti code=%d out=%q err=%s", code, out, errs)
	}
	// A rulebook without the binding answers 未声明 instead of guessing.
	code, _, errs = runCLI(t, root, "ti", "--system", "dnd5e")
	if code != 2 || !strings.Contains(errs, "未声明") {
		t.Fatalf("ti dnd5e code=%d err=%s", code, errs)
	}
	code, _, errs = runCLI(t, root, "sc", "0/1", "--system", "dnd5e")
	if code != 2 || !strings.Contains(errs, "未声明") {
		t.Fatalf("sc dnd5e code=%d err=%s", code, errs)
	}

	// tb resolves tables by id and by display name.
	code, out, errs = runCLI(t, root, "tb", "恐惧症")
	if code != 0 || !strings.Contains(out, "恐惧症") {
		t.Fatalf("tb by name code=%d out=%q err=%s", code, out, errs)
	}

	// rav accepts sheet-value names alongside explicit values.
	code, out, errs = runCLI(t, root, "rav", "理智", "60")
	if code != 0 || !strings.Contains(out, "对抗检定") {
		t.Fatalf("rav by name code=%d out=%q err=%s", code, out, errs)
	}
}
