package formula

import (
	"testing"

	"dicegirl/internal/apperr"
)

func TestEvalInt(t *testing.T) {
	cases := []struct {
		src  string
		env  Env
		want int
	}{
		{"1+2*3", nil, 7},
		{"(1+2)*3", nil, 9},
		{"-7/2", nil, -4}, // floor division, not truncation
		{"7/2", nil, 3},
		{"target/5", Env{"target": 76}, 15},
		{"max(1, 2, 3)+min(3, 2, 1)", nil, 4},
		{"abs(-5)+abs(5)", nil, 10},
		{"STR+SIZ", Env{"STR": 50, "SIZ": 70}, 120},
		{"2*-3", nil, -6},
		{"100-(4-2)*3", nil, 94},
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			e, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got, err := e.EvalInt(tc.env)
			if err != nil {
				t.Fatalf("eval: %v", err)
			}
			if got != tc.want {
				t.Fatalf("%s = %d, want %d", tc.src, got, tc.want)
			}
		})
	}
}

func TestEvalBool(t *testing.T) {
	cases := []struct {
		src  string
		env  Env
		want bool
	}{
		{"roll <= target/5", Env{"roll": 15, "target": 76}, true},
		{"roll <= target/5", Env{"roll": 16, "target": 76}, false},
		{"target < 50", Env{"target": 49}, true},
		{"!(1 == 2) && 3 >= 3", nil, true},
		{"1 < 2 || 99 < 0", nil, true},
		{"1 < 2 && 99 < 0", nil, false},
		{"STR<SIZ && DEX<SIZ", Env{"STR": 40, "SIZ": 70, "DEX": 55}, true},
		{"roll >= 96", Env{"roll": 100}, true},
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			e, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if e.Kind() != KindBool {
				t.Fatalf("%s has kind int, want bool", tc.src)
			}
			got, err := e.EvalBool(tc.env)
			if err != nil {
				t.Fatalf("eval: %v", err)
			}
			if got != tc.want {
				t.Fatalf("%s = %v, want %v", tc.src, got, tc.want)
			}
		})
	}
}

func TestShortCircuit(t *testing.T) {
	// Right side would divide by zero; short-circuit must not evaluate it.
	e, err := Parse("1 < 0 || 2 == 2")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := e.EvalBool(nil); err != nil || !ok {
		t.Fatalf("got %v, %v", ok, err)
	}
}

func TestCheckVars(t *testing.T) {
	e, err := Parse("roll <= target/5")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Check(map[string]bool{"roll": true, "target": true}, KindBool); err != nil {
		t.Fatalf("check with allowed vars: %v", err)
	}
	err = e.Check(map[string]bool{"roll": true}, KindBool)
	if err == nil || !apperr.IsCode(err, apperr.ConfigInvalid) {
		t.Fatalf("unknown target should be ConfigInvalid, got %v", err)
	}

	ie, err := Parse("STR+1")
	if err != nil {
		t.Fatal(err)
	}
	if err := ie.Check(map[string]bool{"STR": true}, KindInt); err != nil {
		t.Fatalf("int check: %v", err)
	}
	if err := ie.Check(map[string]bool{"STR": true}, KindBool); err == nil {
		t.Fatal("int formula must not pass a boolean check")
	}
}

func TestParseErrors(t *testing.T) {
	bad := []string{
		"",
		"1 +",
		"(1+2",
		"1 2",
		"1 @ 2",
		"@",
		"1 + * 2",
		"(1+2))( ",
	}
	for _, src := range bad {
		if _, err := Parse(src); err == nil {
			t.Errorf("Parse(%q) succeeded, want error", src)
		} else if !apperr.IsCode(err, apperr.ParseError) {
			t.Errorf("Parse(%q) code = %v, want PARSE_ERROR", src, apperr.CodeOf(err))
		}
	}
}

func TestTypeErrors(t *testing.T) {
	bad := []string{
		"1 < 2 < 3",   // chained comparison: bool vs int
		"!1",          // ! on integer
		"- (1 == 1)",  // unary minus on boolean
		"(1 < 2) + 1", // arithmetic on boolean
		"1 && 2",      // logic on integers
		"unknownf(1)", // unknown function
		"abs(1, 2)",   // wrong arity
		"min()",       // no args
		"abs(1 < 2)",  // boolean argument
		"1 == 2 == 3", // chained equality
	}
	for _, src := range bad {
		if _, err := Parse(src); err == nil {
			t.Errorf("Parse(%q) succeeded, want type error", src)
		} else if !apperr.IsCode(err, apperr.ConfigInvalid) {
			t.Errorf("Parse(%q) code = %v, want CONFIG_INVALID", src, apperr.CodeOf(err))
		}
	}
}

func TestRuntimeErrors(t *testing.T) {
	var err error

	e := MustParse("STR+1")
	_, err = e.EvalInt(Env{})
	if err == nil || !apperr.IsCode(err, apperr.InvalidArgument) {
		t.Fatalf("missing var = %v, want INVALID_ARGUMENT", err)
	}

	e = MustParse("1/0")
	_, err = e.EvalInt(nil)
	if err == nil || !apperr.IsCode(err, apperr.DivideByZero) {
		t.Fatalf("divide by zero = %v, want DIVIDE_BY_ZERO", err)
	}

	e = MustParse("1")
	if _, err = e.EvalBool(nil); err == nil {
		t.Fatal("EvalBool on int formula should fail")
	}
	if err := e.Check(map[string]bool{}, KindInt); err != nil {
		t.Fatalf("constant should check: %v", err)
	}
}

func TestNilCheck(t *testing.T) {
	if err := (*Expr)(nil).Check(nil, KindInt); err == nil {
		t.Fatal("nil expr should error")
	}
}
