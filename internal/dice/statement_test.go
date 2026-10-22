package dice

import (
	"context"
	"testing"

	"dicegirl/internal/apperr"
)

func TestParseStatement(t *testing.T) {
	l := DefaultLimits()
	ok := []struct {
		in     string
		repeat int
		expr   string
		cmp    *CompareSpec
	}{
		{"3#1d20", 3, "1d20", nil},
		{"#1d6", 1, "1d6", nil}, // omitted count => 1 (Dice! convention)
		{" 2# 1d20>=10 ", 2, "1d20", &CompareSpec{CmpGE, 10}},
		{"2d6+1>=7", 1, "2d6+1", &CompareSpec{CmpGE, 7}},
		{"1d20<=5", 1, "1d20", &CompareSpec{CmpLE, 5}},
		{"1d4<4", 1, "1d4", &CompareSpec{CmpLT, 4}},
		{"1d4>2", 1, "1d4", &CompareSpec{CmpGT, 2}},
		{"1d4>=-1", 1, "1d4", &CompareSpec{CmpGE, -1}},
		{"6", 1, "6", nil}, // plain integer literal is a valid expression
	}
	for _, tc := range ok {
		st, err := ParseStatement(tc.in, l)
		if err != nil {
			t.Errorf("ParseStatement(%q): %v", tc.in, err)
			continue
		}
		if st.Repeat != tc.repeat || st.Expr != tc.expr {
			t.Errorf("ParseStatement(%q) = repeat %d expr %q, want repeat %d expr %q",
				tc.in, st.Repeat, st.Expr, tc.repeat, tc.expr)
		}
		switch {
		case tc.cmp == nil && st.Compare != nil:
			t.Errorf("ParseStatement(%q): unexpected comparison %v", tc.in, st.Compare)
		case tc.cmp != nil && st.Compare == nil:
			t.Errorf("ParseStatement(%q): missing comparison", tc.in)
		case tc.cmp != nil && (st.Compare.Op != tc.cmp.Op || st.Compare.Target != tc.cmp.Target):
			t.Errorf("ParseStatement(%q): comparison = %s%d, want %s%d",
				tc.in, st.Compare.Op, st.Compare.Target, tc.cmp.Op, tc.cmp.Target)
		}
	}
}

func TestStatementParseErrors(t *testing.T) {
	bad := []struct {
		in   string
		code apperr.Code
	}{
		{"", apperr.ParseError},
		{"   ", apperr.ParseError},
		{"#", apperr.ParseError},
		{"3#", apperr.ParseError},
		{"1d6#", apperr.ParseError},    // '#' is only legal as a prefix
		{"1d6#1d4", apperr.ParseError}, // prefix must be a number
		{"2#1d6#3", apperr.ParseError}, // '#' may appear only once
		{"0#1d6", apperr.InvalidArgument},
		{"11#1d6", apperr.LimitExceeded}, // default MaxRepeat is 10
		{">=5", apperr.ParseError},       // nothing to compare
		{"1d6>=", apperr.ParseError},     // missing target
		{"1d6>x", apperr.ParseError},     // non-integer target
		{"2#1d", apperr.ParseError},      // invalid inner expression
	}
	for _, tc := range bad {
		_, err := ParseStatement(tc.in, DefaultLimits())
		if err == nil {
			t.Errorf("ParseStatement(%q): expected error, got nil", tc.in)
			continue
		}
		if got := apperr.CodeOf(err); got != tc.code {
			t.Errorf("ParseStatement(%q): code = %q, want %q (%v)", tc.in, got, tc.code, err)
		}
	}

	// Custom repeat limit.
	custom := DefaultLimits()
	custom.MaxRepeat = 2
	if _, err := ParseStatement("3#1d6", custom); !apperr.IsCode(err, apperr.LimitExceeded) {
		t.Errorf("custom limit: code = %v, want LIMIT_EXCEEDED", err)
	}
}

func TestRollStatementRepeat(t *testing.T) {
	// Scripted 0-based draws are consumed in order across trials:
	// trial 1 draws face 11 (10+1), trial 2 face 3 (2+1).
	r := New(NewScriptedSource(10, 2))
	batch, err := r.RollStatement(context.Background(), "2#1d20>=10")
	if err != nil {
		t.Fatalf("RollStatement: %v", err)
	}
	if batch.Expression != "2#1d20>=10" || batch.Repeat != 2 || len(batch.Rolls) != 2 {
		t.Fatalf("batch shape wrong: %+v", batch)
	}
	want := []struct {
		total   int
		success bool
	}{
		{11, true},
		{3, false},
	}
	for i, w := range want {
		rr := batch.Rolls[i]
		if rr.Expression != "1d20" {
			t.Errorf("trial %d expression = %q, want inner expression", i, rr.Expression)
		}
		if rr.Total != w.total {
			t.Errorf("trial %d total = %d, want %d", i, rr.Total, w.total)
		}
		if rr.Compare == nil || rr.Compare.Op != CmpGE || rr.Compare.Target != 10 {
			t.Errorf("trial %d comparison missing/wrong: %+v", i, rr.Compare)
		} else if rr.Compare.Success != w.success {
			t.Errorf("trial %d success = %v, want %v", i, rr.Compare.Success, w.success)
		}
	}

	// Multi-group expressions re-roll independently per trial:
	// trial 1: 1+2+1 = 4; trial 2: 3+4+1 = 8.
	batch = rollBatchScripted(t, "2#2d6+1", 0, 1, 2, 3)
	if batch.Rolls[0].Total != 4 || batch.Rolls[1].Total != 8 {
		t.Fatalf("independent trials = %d, %d, want 4, 8",
			batch.Rolls[0].Total, batch.Rolls[1].Total)
	}
	if len(batch.Rolls[0].Groups) != 1 || len(batch.Rolls[0].Groups[0].Rolls) != 2 {
		t.Fatalf("trial groups not independent: %+v", batch.Rolls[0].Groups)
	}
}

func TestRollStatementNoDice(t *testing.T) {
	// A repeated integer literal consumes no randomness at all.
	batch, err := New(NewScriptedSource()).RollStatement(context.Background(), "3#6")
	if err != nil {
		t.Fatalf("RollStatement: %v", err)
	}
	if batch.Repeat != 3 || len(batch.Rolls) != 3 {
		t.Fatalf("batch shape wrong: %+v", batch)
	}
	for i, rr := range batch.Rolls {
		if rr.Total != 6 {
			t.Errorf("trial %d total = %d, want 6", i, rr.Total)
		}
	}
}

func TestPureRollRejectsStatementSyntax(t *testing.T) {
	// Roll/RollStatement separation: the pure expression entry point must
	// never accept repeat/comparison syntax (rulebook-authored expressions
	// rely on that grammar staying pure).
	r := New(NewScriptedSource(0))
	for _, in := range []string{"2#1d6", "2d6>=7", "1d6<"} {
		if _, err := r.Roll(context.Background(), in); !apperr.IsCode(err, apperr.ParseError) {
			t.Errorf("Roll(%q): code = %v, want PARSE_ERROR", in, err)
		}
	}
}

func TestCmpOpString(t *testing.T) {
	cases := map[CmpOp]string{
		CmpLT: "<", CmpLE: "<=", CmpGT: ">", CmpGE: ">=",
	}
	for op, want := range cases {
		if op.String() != want {
			t.Errorf("%v.String() = %q, want %q", op, op.String(), want)
		}
	}
}

func rollBatchScripted(t *testing.T, input string, values ...int) *BatchResult {
	t.Helper()
	res, err := New(NewScriptedSource(values...)).RollStatement(context.Background(), input)
	if err != nil {
		t.Fatalf("RollStatement(%q): %v", input, err)
	}
	return res
}
