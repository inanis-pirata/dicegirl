package engine_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"dicegirl/internal/apperr"
	"dicegirl/internal/engine"
	"dicegirl/rulebooks"
)

// TestExplicitDifficultyRewritesTarget covers hard/extreme formula
// difficulties against the embedded coc7 book with scripted d100 rolls.
func TestExplicitDifficultyRewritesTarget(t *testing.T) {
	ctx := context.Background()

	// 53: against adjusted target 30 (hard of 60) this fails.
	b := scriptedBook(t, 52)
	res, err := b.Check(ctx, engine.CheckInput{Target: 60, Difficulty: "hard", SkillName: "心理学"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Target != 30 || res.BaseTarget != 60 || res.LevelCode != "fail" {
		t.Fatalf("hard fail: target=%d base=%d level=%s", res.Target, res.BaseTarget, res.LevelCode)
	}
	if res.DifficultyID != "hard" || res.DifficultyName != "困难" {
		t.Fatalf("difficulty fields = %q/%q", res.DifficultyID, res.DifficultyName)
	}
	if !strings.Contains(res.Explanation, "心理学（困难）") || !strings.Contains(res.Explanation, "（基础 60）") {
		t.Fatalf("explanation: %q", res.Explanation)
	}

	// 13: against adjusted target 30 the ladder puts it in (6,15],
	// i.e. the hard band.
	b = scriptedBook(t, 12)
	res, err = b.Check(ctx, engine.CheckInput{Target: 60, Difficulty: "hard"})
	if err != nil || res.LevelCode != "hard" {
		t.Fatalf("hard success: level=%s err=%v", res.LevelCode, err)
	}

	// 12: extreme of 60 = 12, boundary included.
	b = scriptedBook(t, 11)
	res, err = b.Check(ctx, engine.CheckInput{Target: 60, Difficulty: "extreme"})
	if err != nil || res.Target != 12 || res.LevelCode != "extreme" {
		t.Fatalf("extreme: target=%d level=%s err=%v", res.Target, res.LevelCode, err)
	}
}

func TestDifficultyFumbleUsesAdjustedTarget(t *testing.T) {
	ctx := context.Background()
	// 96. Base target 99 fumbles only on 100, but hard adjusts to 49,
	// so 96 fumbles: fumble evaluation follows the rewritten target
	// (BCDice convention).
	b := scriptedBook(t, 95)
	res, err := b.Check(ctx, engine.CheckInput{Target: 99, Difficulty: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	if res.LevelCode != "fumble" {
		t.Fatalf("adjusted-target fumble = %s, want fumble", res.LevelCode)
	}
}

func TestAutoSuccessDifficulty(t *testing.T) {
	ctx := context.Background()

	// 53 against base 60: an ordinary fail maps to regular success.
	b := scriptedBook(t, 52)
	res, err := b.Check(ctx, engine.CheckInput{Target: 60, Difficulty: "auto", SkillName: "爆破"})
	if err != nil {
		t.Fatal(err)
	}
	if res.LevelCode != "regular" || res.Target != 60 || res.BaseTarget != 60 {
		t.Fatalf("auto success: level=%s target=%d base=%d", res.LevelCode, res.Target, res.BaseTarget)
	}
	if !strings.Contains(res.Explanation, "非大失败即成功") {
		t.Fatalf("auto explanation: %q", res.Explanation)
	}

	// 96 against base 60 is not a fumble (fumble only on 100).
	b = scriptedBook(t, 95)
	res, err = b.Check(ctx, engine.CheckInput{Target: 60, Difficulty: "auto"})
	if err != nil || res.LevelCode != "regular" {
		t.Fatalf("96 vs 60: level=%s err=%v", res.LevelCode, err)
	}

	// 96 against base 40 IS a fumble and stays a fumble.
	b = scriptedBook(t, 95)
	res, err = b.Check(ctx, engine.CheckInput{Target: 40, Difficulty: "auto"})
	if err != nil || res.LevelCode != "fumble" {
		t.Fatalf("fumble under auto: level=%s err=%v", res.LevelCode, err)
	}

	// 100 is always a fumble.
	b = scriptedBook(t, 99)
	res, err = b.Check(ctx, engine.CheckInput{Target: 60, Difficulty: "auto"})
	if err != nil || res.LevelCode != "fumble" {
		t.Fatalf("100 under auto: level=%s err=%v", res.LevelCode, err)
	}
}

func TestUnknownDifficultyRejected(t *testing.T) {
	b := coc7Book(t, nil)
	_, err := b.Check(context.Background(), engine.CheckInput{Target: 60, Difficulty: "nope"})
	if !apperr.IsCode(err, apperr.InvalidArgument) {
		t.Fatalf("unknown difficulty code = %v, want INVALID_ARGUMENT", err)
	}
}

func TestDifficultyAccessors(t *testing.T) {
	b := coc7Book(t, nil)
	diffs := b.Difficulties()
	if len(diffs) != 3 {
		t.Fatalf("difficulties = %v", diffs)
	}
	for _, want := range []struct{ id, name string }{
		{"extreme", "极难"}, {"hard", "困难"}, {"auto", "自动成功"},
	} {
		if d, ok := b.FindDifficulty(want.id); !ok || d.Name != want.name {
			t.Errorf("FindDifficulty(%q) = %+v,%v", want.id, d, ok)
		}
		if _, ok := b.FindDifficulty(want.name); !ok {
			t.Errorf("FindDifficulty name %q not found", want.name)
		}
	}
	if d, ok := b.FindDifficulty("HARD"); !ok || d.ID != "hard" {
		t.Fatalf("id matching should be case-insensitive: %+v,%v", d, ok)
	}
	if _, ok := b.FindDifficulty("不存在"); ok {
		t.Fatal("unknown difficulty must not resolve")
	}
}

// difficultyVariant replaces one embedded-YAML line, compiles, and returns
// the error (nil when the variant is accepted).
func difficultyVariant(t *testing.T, marker, replacement string) error {
	t.Helper()
	data, err := rulebooks.FS.ReadFile("coc7.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(marker)) {
		t.Fatalf("marker missing: %q", marker)
	}
	variant := bytes.Replace(data, []byte(marker), []byte(replacement), 1)
	_, err = engine.ParseBook(variant, "variant.yaml", nil)
	return err
}

const (
	extremeDiffLine = `    - { id: extreme, name: "极难",     expr: "target/5" }`
	hardDiffLine    = `    - { id: hard,    name: "困难",     expr: "target/2" }`
	autoDiffLine    = `    - { id: auto,    name: "自动成功", auto: true, success_level: regular }`
)

func TestDifficultyConfigValidation(t *testing.T) {
	cases := []struct {
		name        string
		marker      string
		replacement string
	}{
		{"duplicate id", hardDiffLine, `    - { id: extreme, name: "困难B",    expr: "target/2" }`},
		{"duplicate name", hardDiffLine, `    - { id: hardb,   name: "极难",     expr: "target/2" }`},
		{"both expr and auto", hardDiffLine, `    - { id: hard,    name: "困难",     expr: "target/2", auto: true }`},
		{"neither expr nor auto", hardDiffLine, `    - { id: hard,    name: "困难" }`},
		{"unknown var", hardDiffLine, `    - { id: hard,    name: "困难",     expr: "roll/2" }`},
		{"auto unknown level", autoDiffLine, `    - { id: auto,    name: "自动成功", auto: true, success_level: nope }`},
		{"auto fumble level", autoDiffLine, `    - { id: auto,    name: "自动成功", auto: true, success_level: fumble }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := difficultyVariant(t, tc.marker, tc.replacement)
			if !apperr.IsCode(err, apperr.ConfigInvalid) {
				t.Fatalf("code = %v, want CONFIG_INVALID", err)
			}
		})
	}

	// Books may omit the section entirely.
	err := difficultyVariant(t,
		`  difficulties:
`+extremeDiffLine+"\n"+hardDiffLine+"\n"+autoDiffLine+"\n", "")
	if err != nil {
		t.Fatalf("omitting difficulties must be valid: %v", err)
	}
}

func TestDifficultyEffectiveTargetOutOfRange(t *testing.T) {
	// A formula pushing the target outside the declared computed-target
	// range is an execution-time INVALID_ARGUMENT, not a config error
	// (base targets vary).
	data, _ := rulebooks.FS.ReadFile("coc7.yaml")
	huge := `    - { id: huge,    name: "巨难",     expr: "target*50" }`
	variant := bytes.Replace(data, []byte(hardDiffLine), []byte(huge), 1)
	b, perr := engine.ParseBook(variant, "variant.yaml", nil)
	if perr != nil {
		t.Fatal(perr)
	}
	// 60*50 = 3000 exceeds coc7's computed_target (0..999).
	_, err := b.Check(context.Background(), engine.CheckInput{Target: 60, Difficulty: "huge"})
	if !apperr.IsCode(err, apperr.InvalidArgument) {
		t.Fatalf("effective target 3000: code = %v, want INVALID_ARGUMENT", err)
	}

	// Without limits.computed_target the range defaults to the target
	// range, so 60*5 = 300 is out of range as well.
	plain := bytes.Replace(variant, []byte("  computed_target: { min: 0, max: 999 }\n"), nil, 1)
	plain = bytes.Replace(plain, []byte(huge),
		[]byte(`    - { id: huge,    name: "巨难",     expr: "target*5" }`), 1)
	b, perr = engine.ParseBook(plain, "plain.yaml", nil)
	if perr != nil {
		t.Fatal(perr)
	}
	_, err = b.Check(context.Background(), engine.CheckInput{Target: 60, Difficulty: "huge"})
	if !apperr.IsCode(err, apperr.InvalidArgument) {
		t.Fatalf("effective target 300: code = %v, want INVALID_ARGUMENT", err)
	}
}
