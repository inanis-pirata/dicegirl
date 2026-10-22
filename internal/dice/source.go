package dice

import (
	"crypto/rand"
	"fmt"
	"math/big"
	mrand "math/rand/v2"
)

// Source is the injectable randomness source. Intn returns a value in
// [0, n). Production code uses CryptoSource; tests use SeededSource or
// ScriptedSource so results are fully deterministic.
type Source interface {
	Intn(n int) (int, error)
}

// CryptoSource draws from crypto/rand. It is suitable for game use: it needs
// no seed and cannot be predicted or reproduced.
type CryptoSource struct{}

// NewCryptoSource returns a source backed by crypto/rand.
func NewCryptoSource() *CryptoSource { return &CryptoSource{} }

// Intn implements Source.
func (CryptoSource) Intn(n int) (int, error) {
	if n <= 0 {
		return 0, fmt.Errorf("dice: Intn requires n > 0, got %d", n)
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, err
	}
	return int(v.Int64()), nil
}

// SeededSource is a deterministic PCG-based source for reproducible games and
// tests.
type SeededSource struct {
	rng *mrand.Rand
}

// NewSeededSource builds a deterministic source from seed. The same seed
// always produces the same sequence.
func NewSeededSource(seed uint64) *SeededSource {
	return &SeededSource{rng: mrand.New(mrand.NewPCG(seed, seed^0x9E3779B97F4A7C15))}
}

// Intn implements Source.
func (s *SeededSource) Intn(n int) (int, error) {
	if n <= 0 {
		return 0, fmt.Errorf("dice: Intn requires n > 0, got %d", n)
	}
	return s.rng.IntN(n), nil
}

// ScriptedSource returns a fixed sequence of values, each in [0, n). It is
// intended for unit tests: running out of values is an error, so tests can
// never silently depend on an unscripted random value.
type ScriptedSource struct {
	values []int
	pos    int
}

// NewScriptedSource builds a source yielding values in order.
func NewScriptedSource(values ...int) *ScriptedSource {
	return &ScriptedSource{values: values}
}

// Intn implements Source.
func (s *ScriptedSource) Intn(n int) (int, error) {
	if n <= 0 {
		return 0, fmt.Errorf("dice: Intn requires n > 0, got %d", n)
	}
	if s.pos >= len(s.values) {
		return 0, fmt.Errorf("dice: scripted source exhausted at call %d", s.pos+1)
	}
	v := s.values[s.pos]
	s.pos++
	if v < 0 || v >= n {
		return 0, fmt.Errorf("dice: scripted value %d out of range [0,%d)", v, n)
	}
	return v, nil
}
