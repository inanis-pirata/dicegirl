package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

// game is the game-management command: one group data root can run several
// games, each with its own rulebook, initiative, decks and bindings.
func TestGMCommands(t *testing.T) {
	root := t.TempDir()

	// With no arguments game shows the implicit default game.
	code, out, errs := runCLI(t, root, ".game")
	if code != 0 {
		t.Fatalf("game code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "默认游戏") || !strings.Contains(out, "当前") {
		t.Fatalf("game should show the default game: %q", out)
	}

	// game new creates and switches to the new game.
	code, out, errs = runCLI(t, root, ".game", "new", "恐怖团", "--system", "coc7")
	if code != 0 {
		t.Fatalf("game new code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "恐怖团") || !strings.Contains(out, "coc7") {
		t.Fatalf("game new output: %q", out)
	}

	code, out, _ = runCLI(t, root, ".game", "list")
	if code != 0 || !strings.Contains(out, "恐怖团") || !strings.Contains(out, "默认游戏") {
		t.Fatalf("game list code=%d out=%q", code, out)
	}

	// game show accepts a display name and leaves the current game alone.
	code, out, errs = runCLI(t, root, ".game", "show", "默认游戏")
	if code != 0 || !strings.Contains(out, "默认游戏") {
		t.Fatalf("game show code=%d out=%q err=%s", code, out, errs)
	}

	// game system changes the current game's default rulebook.
	code, out, errs = runCLI(t, root, ".game", "system", "dnd5e")
	if code != 0 || !strings.Contains(out, "dnd5e") {
		t.Fatalf("game system code=%d out=%q err=%s", code, out, errs)
	}
	code, out, _ = runCLI(t, root, ".game", "show", "恐怖团")
	if code != 0 || !strings.Contains(out, "dnd5e") {
		t.Fatalf("game show after system code=%d out=%q", code, out)
	}

	// game use switches back; the current game cannot be deleted.
	if code, _, errs := runCLI(t, root, ".game", "use", "默认游戏"); code != 0 {
		t.Fatalf("game use code=%d err=%s", code, errs)
	}
	code, _, errs = runCLI(t, root, ".game", "del", "默认游戏")
	if code == 0 || !strings.Contains(errs, "不能删除当前游戏") {
		t.Fatalf("deleting the current game code=%d err=%q", code, errs)
	}

	// A different game can be deleted.
	if code, _, errs := runCLI(t, root, ".game", "del", "恐怖团"); code != 0 {
		t.Fatalf("game del code=%d err=%s", code, errs)
	}
	code, out, _ = runCLI(t, root, ".game", "list")
	if code != 0 || strings.Contains(out, "恐怖团") {
		t.Fatalf("game list after del code=%d out=%q", code, out)
	}

	// Unknown games are a NOT_FOUND error.
	code, _, errs = runCLI(t, root, ".game", "show", "不存在")
	if code != 1 || !strings.Contains(errs, "NOT_FOUND") {
		t.Fatalf("unknown game code=%d err=%q", code, errs)
	}
}

// TestGameAffairs covers the Dice!Next roster, variables, open/closed state
// and roulette actions. The CLI acts as one local member; the GM is appointed
// by `game new`.
func TestGameAffairs(t *testing.T) {
	root := t.TempDir()

	code, out, errs := runCLI(t, root, ".game", "new", "迷雾之城", "--system", "coc7")
	if code != 0 {
		t.Fatalf("new game code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "GM：本地用户") {
		t.Fatalf("creator should become GM: %q", out)
	}

	// A player joins, and can also be the GM (the GM is a participant).
	code, out, errs = runCLI(t, root, ".game", "join")
	if code != 0 || !strings.Contains(out, "玩家：本地用户") {
		t.Fatalf("join code=%d out=%q err=%s", code, out, errs)
	}
	code, out, _ = runCLI(t, root, ".game", "call")
	if code != 0 || !strings.Contains(out, "@本地用户") {
		t.Fatalf("call code=%d out=%q", code, out)
	}
	code, out, _ = runCLI(t, root, ".game", "kick", "本地用户")
	if code != 0 || !strings.Contains(out, "踢下桌") {
		t.Fatalf("kick code=%d out=%q", code, out)
	}
	if code, _, errs := runCLI(t, root, ".game", "kick", "本地用户"); code == 0 || !strings.Contains(errs, "NOT_FOUND") {
		t.Fatalf("kick missing code=%d err=%q", code, errs)
	}

	// Variables accept both "key value" and "key=value", and a bare key reads.
	code, out, _ = runCLI(t, root, ".game", "set", "章节=第三章")
	if code != 0 || !strings.Contains(out, "章节=第三章") {
		t.Fatalf("set var code=%d out=%q", code, out)
	}
	code, out, _ = runCLI(t, root, ".game", "set", "天气", "雨")
	if code != 0 || !strings.Contains(out, "天气=雨") {
		t.Fatalf("set var words code=%d out=%q", code, out)
	}
	code, out, _ = runCLI(t, root, ".game", "set", "章节")
	if code != 0 || !strings.Contains(out, "章节 = 第三章") {
		t.Fatalf("get var code=%d out=%q", code, out)
	}

	// Game affairs are isolated per game.
	code, _, errs = runCLI(t, root, ".game", "new", "第二团", "--system", "coc7")
	if code != 0 {
		t.Fatalf("second game code=%d err=%s", code, errs)
	}
	code, out, _ = runCLI(t, root, ".game", "state")
	if code != 0 || strings.Contains(out, "章节=第三章") {
		t.Fatalf("second game leaked variables: %q", out)
	}
	code, out, _ = runCLI(t, root, ".game", "show", "迷雾之城")
	if code != 0 || !strings.Contains(out, "章节=第三章") {
		t.Fatalf("first game lost variables: %q", out)
	}

	// Close keeps the roster and variables but marks the game closed; open
	// restores it.
	code, out, _ = runCLI(t, root, ".game", "close")
	if code != 0 || !strings.Contains(out, "已关闭") {
		t.Fatalf("close code=%d out=%q", code, out)
	}
	code, out, _ = runCLI(t, root, ".game", "open")
	if code != 0 || !strings.Contains(out, "进行中") {
		t.Fatalf("open code=%d out=%q", code, out)
	}

	// Roulette: a d6 bag of one copy draws every face exactly once.
	code, out, errs = runCLI(t, root, ".game", "rou", "6")
	if code != 0 || !strings.Contains(out, "已为 d6 启用轮盘骰") {
		t.Fatalf("rou enable code=%d out=%q err=%s", code, out, errs)
	}
	seen := map[int]bool{}
	for i := 0; i < 6; i++ {
		code, out, errs = runCLI(t, root, "--json", ".r", "1d6")
		if code != 0 {
			t.Fatalf("roulette roll %d code=%d err=%s", i, code, errs)
		}
		var res struct{ Total int }
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("roulette json: %v (%q)", err, out)
		}
		if res.Total < 1 || res.Total > 6 || seen[res.Total] {
			t.Fatalf("roulette draw %d repeated/out of range: %d (seen=%v)", i, res.Total, seen)
		}
		seen[res.Total] = true
	}
	code, out, _ = runCLI(t, root, ".game", "rou", "hist")
	if code != 0 || !strings.Contains(out, "6/6") {
		t.Fatalf("rou hist code=%d out=%q", code, out)
	}

	// Non-single rolls bypass the bag: after a reset, 1d6+1 leaves no history.
	if code, _, errs := runCLI(t, root, ".game", "rou", "reset"); code != 0 {
		t.Fatalf("rou reset code=%d err=%s", code, errs)
	}
	if code, _, errs := runCLI(t, root, ".r", "1d6+1"); code != 0 {
		t.Fatalf("compound roll code=%d err=%s", code, errs)
	}
	code, out, _ = runCLI(t, root, ".game", "rou", "hist")
	if code != 0 || !strings.Contains(out, "本轮还没有抽过点数") {
		t.Fatalf("compound roll touched roulette: %q", out)
	}

	// A closed game suspends the roulette but keeps it configured.
	if code, _, errs := runCLI(t, root, ".game", "close"); code != 0 {
		t.Fatalf("close before suspend code=%d err=%s", code, errs)
	}
	if code, _, errs := runCLI(t, root, ".r", "1d6"); code != 0 {
		t.Fatalf("closed roulette roll code=%d err=%s", code, errs)
	}
	code, out, _ = runCLI(t, root, ".game", "rou", "hist")
	if code != 0 || !strings.Contains(out, "本轮还没有抽过点数") {
		t.Fatalf("closed game used roulette: %q", out)
	}
	if code, _, errs := runCLI(t, root, ".game", "open"); code != 0 {
		t.Fatalf("reopen code=%d err=%s", code, errs)
	}
	if code, out, _ = runCLI(t, root, ".game", "rou", "clr"); code != 0 || !strings.Contains(out, "恢复普通掷骰") {
		t.Fatalf("rou clr code=%d out=%q", code, out)
	}

	// The bag size limit follows sides × copies ≤ 100.
	if code, _, errs := runCLI(t, root, ".game", "rou", "3*50"); code != 2 || !strings.Contains(errs, "100") {
		t.Fatalf("rou limit code=%d err=%q", code, errs)
	}

	// Ending a game clears its roster, variables and roulette.
	if code, _, errs := runCLI(t, root, ".game", "set", "章节", "终章"); code != 0 {
		t.Fatalf("set before over code=%d err=%s", code, errs)
	}
	code, out, errs = runCLI(t, root, ".game", "over")
	if code != 0 || !strings.Contains(out, "已结团") {
		t.Fatalf("over code=%d out=%q err=%s", code, out, errs)
	}
	code, out, _ = runCLI(t, root, ".game", "state")
	if code != 0 || strings.Contains(out, "终章") || strings.Contains(out, "GM：本地用户") {
		t.Fatalf("over left game affairs behind: %q", out)
	}
}
