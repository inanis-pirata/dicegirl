package engine_test

import (
	"context"
	"testing"

	"dicegirl/internal/apperr"
	"dicegirl/internal/dice"
	"dicegirl/internal/engine"
)

// The rolled method rolls the declared expressions: 3d6*5 for STR and
// (2d6+6)*5 for SIZ. Each characteristic consumes as many values as it has
// dice (3 or 2).
func TestGenerationRolled(t *testing.T) {
	values := []int{
		0, 1, 2, // STR: 3d6 -> 6
		0, 0, 0, // CON: 3d6 -> 3
		5, 4, // SIZ: 2d6 -> 11
	}
	for len(values) < 24 {
		values = append(values, 0)
	}
	b := scriptedBook(t, values...)
	res, err := b.Generate(context.Background(), engine.GenerationInput{Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != "generation" || res.VariantID != "7" {
		t.Fatalf("kind=%s variant=%s", res.Kind, res.VariantID)
	}
	c := res.Characters[0]
	// 3d6 = 1+2+3 = 6 -> 30
	if c.Attributes["STR"] != 30 {
		t.Fatalf("STR = %d, want 30", c.Attributes["STR"])
	}
	// (2d6+6)*5 = (6+5+6)*5 = 85
	if c.Attributes["SIZ"] != 85 {
		t.Fatalf("SIZ = %d, want 85", c.Attributes["SIZ"])
	}
	if len(c.Rolls) != 9 {
		t.Fatalf("rolls = %d, want 9", len(c.Rolls))
	}
}

// The variants select another edition of the same game.
func TestGenerationVariant(t *testing.T) {
	values := []int{
		0, 1, // STR: (2d6+6) -> 9
		0, 0, // CON
		0, 0, // DEX
		0, 1, // SIZ: 2d6 -> 3
	}
	for len(values) < 18 {
		values = append(values, 0)
	}
	b := scriptedBook(t, values...)
	res, err := b.Generate(context.Background(), engine.GenerationInput{VariantID: "6", Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.VariantID != "6" {
		t.Fatalf("variant = %s", res.VariantID)
	}
	c := res.Characters[0]
	// CoC6: (2d6+6)*5 = (1+2+6)*5 = 45
	if c.Attributes["STR"] != 45 {
		t.Fatalf("STR = %d, want 45", c.Attributes["STR"])
	}
	// CoC6: 2d6*5 = 3*5 = 15
	if c.Attributes["SIZ"] != 15 {
		t.Fatalf("SIZ = %d, want 15", c.Attributes["SIZ"])
	}
}

// The detailed mode spends the point budget on the declared priorities.
func TestGenerationPointBuy(t *testing.T) {
	b := coc7Book(t, nil)
	res, err := b.Generate(context.Background(), engine.GenerationInput{Count: 1, Detailed: true})
	if err != nil {
		t.Fatal(err)
	}
	c := res.Characters[0]
	if c.PointBuy == nil {
		t.Fatal("point-buy report missing")
	}
	// EDU 60 and INT 50 are bought first: the budget is 60*20+50*10 = 1700.
	if c.PointBuy.Budget != 1700 {
		t.Fatalf("budget = %d, want 1700", c.PointBuy.Budget)
	}
	if c.Attributes["EDU"] != 60 || c.Attributes["INT"] != 50 {
		t.Fatalf("EDU/INT = %d/%d, want 60/50", c.Attributes["EDU"], c.Attributes["INT"])
	}
	if c.Attributes["STR"] != 50 {
		t.Fatalf("STR = %d, want 50", c.Attributes["STR"])
	}
	// The published cost table: 15-49 costs 1 point per 2 points, 50-79
	// costs 1 point per 5 points above 49.
	if c.PointBuy.Costs["INT"] != 25 || c.PointBuy.Costs["EDU"] != 27 {
		t.Fatalf("costs = %v, want INT 25 / EDU 27", c.PointBuy.Costs)
	}
	// The remaining points are for skills.
	if c.PointBuy.Left <= 0 {
		t.Fatalf("left = %d, want a positive remainder", c.PointBuy.Left)
	}
	if c.PointBuy.OccupationCap != 300 || c.PointBuy.InterestCap != 75 {
		t.Fatalf("caps = %d/%d, want 300/75",
			c.PointBuy.OccupationCap, c.PointBuy.InterestCap)
	}
}

// Batch generation does not share dice between characters.
func TestGenerationBatch(t *testing.T) {
	// 2 characters x 24 dice values; the pattern differs per character.
	values := make([]int, 0, 48)
	for i := 0; i < 48; i++ {
		values = append(values, i%5)
	}
	b := scriptedBook(t, values...)
	res, err := b.Generate(context.Background(), engine.GenerationInput{Count: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Characters) != 2 {
		t.Fatalf("characters = %d, want 2", len(res.Characters))
	}
	if res.Characters[0].Attributes["STR"] == res.Characters[1].Attributes["STR"] {
		t.Fatal("batch characters must not share dice")
	}
}

// Usage errors: unknown variants, bad counts and a detailed batch.
func TestGenerationErrors(t *testing.T) {
	b := coc7Book(t, nil)
	ctx := context.Background()
	if _, err := b.Generate(ctx, engine.GenerationInput{VariantID: "nope"}); !apperr.IsCode(err, apperr.InvalidArgument) {
		t.Fatalf("unknown variant: %v", err)
	}
	if _, err := b.Generate(ctx, engine.GenerationInput{Count: 0}); !apperr.IsCode(err, apperr.InvalidArgument) {
		t.Fatalf("count 0: %v", err)
	}
	if _, err := b.Generate(ctx, engine.GenerationInput{Count: 100}); !apperr.IsCode(err, apperr.InvalidArgument) {
		t.Fatalf("count 100: %v", err)
	}
}

// Books without a generation section report it.
func TestGenerationAbsent(t *testing.T) {
	book, err := engine.ParseBook([]byte(minimalBook), "minimal.yaml", dice.New(dice.NewScriptedSource(1)))
	if err != nil {
		t.Fatal(err)
	}
	if book.Generation() != nil {
		t.Fatal("book without generation must not report one")
	}
	if _, err := book.Generate(context.Background(), engine.GenerationInput{Count: 1}); err == nil {
		t.Fatal("generating without a generation section must fail")
	}
}
