package dice

import (
	"testing"
)

func TestCryptoSourceRange(t *testing.T) {
	src := NewCryptoSource()
	for i := 0; i < 200; i++ {
		v, err := src.Intn(10)
		if err != nil {
			t.Fatalf("Intn: %v", err)
		}
		if v < 0 || v >= 10 {
			t.Fatalf("value %d out of range", v)
		}
	}
}

func TestSourceRejectsNonPositiveN(t *testing.T) {
	sources := []Source{NewCryptoSource(), NewSeededSource(1), NewScriptedSource(0)}
	for _, src := range sources {
		if _, err := src.Intn(0); err == nil {
			t.Fatalf("%T: expected error for n=0", src)
		}
	}
}

func TestSeededSourceDeterministic(t *testing.T) {
	a := NewSeededSource(42)
	b := NewSeededSource(42)
	for i := 0; i < 100; i++ {
		va, _ := a.Intn(100)
		vb, _ := b.Intn(100)
		if va != vb {
			t.Fatalf("seeded sources diverged at %d: %d vs %d", i, va, vb)
		}
	}
}

func TestScriptedSource(t *testing.T) {
	src := NewScriptedSource(0, 5, 9)
	want := []int{0, 5, 9}
	for i, w := range want {
		v, err := src.Intn(10)
		if err != nil || v != w {
			t.Fatalf("call %d: got %d, %v", i, v, err)
		}
	}
	if _, err := src.Intn(10); err == nil {
		t.Fatal("exhausted scripted source must error")
	}
}

func TestScriptedSourceRangeCheck(t *testing.T) {
	src := NewScriptedSource(10)
	if _, err := src.Intn(5); err == nil {
		t.Fatal("out-of-range scripted value must error")
	}
}
