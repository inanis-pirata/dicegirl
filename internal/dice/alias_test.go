package dice

import "testing"

// The Dice! convention writes the multiplication sign as "X"; a lone "x"
// between operands means the same as "*".
func TestMultiplicationAliasX(t *testing.T) {
	res := rollScripted(t, "3d6X5", 0, 1, 2)
	if res.Total != 30 {
		t.Fatalf("3d6X5 = %d, want 30 (1+2+3 times 5)", res.Total)
	}
	if got := rollScripted(t, "2d6x2", 0, 5).Total; got != 14 {
		t.Fatalf("2d6x2 = %d, want 14", got)
	}
	// The alias must not break the dice syntax itself.
	if got := rollScripted(t, "1d6X2+1", 5).Total; got != 13 {
		t.Fatalf("1d6X2+1 = %d, want 13", got)
	}
	// A word that merely contains an x is still an unknown word.
	if _, _, err := Parse("dx", DefaultLimits()); err == nil {
		t.Fatal("unknown word 'dx' accepted")
	}
}
