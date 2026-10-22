package app

import (
	"context"
	"fmt"
	"strconv"

	"dicegirl/internal/apperr"
	"dicegirl/internal/character"
	"dicegirl/internal/dice"
	"dicegirl/internal/engine"
)

// GenerateOptions parameterizes a character generation request.
type GenerateOptions struct {
	SystemID string
	// VariantID selects the generation variant ("6", "7", ...); empty
	// means the rulebook's default variant.
	VariantID string
	// Count is the number of characters to generate.
	Count int
	// Detailed switches to the point-buy method (the "detailed creation").
	Detailed bool
	// Name is the base name of the generated characters; empty means the
	// names come from the data pack's default name set.
	Name string
	// MakeActive sets the first generated character as the active one.
	MakeActive bool
}

// GeneratedSheet is one generated character together with its stored card.
type GeneratedSheet struct {
	Name       string                 `json:"name"`
	ID         string                 `json:"id"`
	Attributes map[string]int         `json:"attributes"`
	Rolls      []*dice.RollResult     `json:"rolls,omitempty"`
	PointBuy   *engine.PointBuyReport `json:"point_buy,omitempty"`
	// Vitals are the rulebook-derived resource tracks recorded on the card
	// during creation (initial and maximum values, declaration order).
	Vitals      []engine.VitalsValue `json:"vitals,omitempty"`
	Explanation string               `json:"explanation"`
}

// GenerateResult is the application-level result of a generation request.
type GenerateResult struct {
	Kind        string            `json:"kind"`
	SystemID    string            `json:"system_id"`
	VariantID   string            `json:"variant_id"`
	VariantName string            `json:"variant_name"`
	Detailed    bool              `json:"detailed"`
	Characters  []*GeneratedSheet `json:"characters"`
}

// GenerateCharacters generates characters according to the rulebook's
// generation rules and stores them as ordinary character cards (so every
// later command works on them unchanged).
func (a *App) GenerateCharacters(ctx context.Context, o GenerateOptions) (*GenerateResult, error) {
	const op = "app.GenerateCharacters"
	count := o.Count
	if count < 1 {
		count = 1
	}
	if o.Detailed && count > 1 {
		return nil, apperr.New(op, apperr.InvalidArgument,
			"详细作成一次只能生成一个人物")
	}
	book, err := a.selectSystem(ctx, o.SystemID, nil)
	if err != nil {
		return nil, err
	}
	if book.Generation() == nil {
		return nil, apperr.New(op, apperr.InvalidArgument,
			"rulebook "+book.Manifest().ID+" declares no character generation")
	}
	gen, err := book.Generate(ctx, engine.GenerationInput{
		VariantID: o.VariantID,
		Count:     count,
		Detailed:  o.Detailed,
	})
	if err != nil {
		return nil, err
	}
	names, err := a.generatedNames(ctx, count, o.Name)
	if err != nil {
		return nil, err
	}
	res := &GenerateResult{
		Kind:        "generation",
		SystemID:    gen.SystemID,
		VariantID:   gen.VariantID,
		VariantName: gen.VariantName,
		Detailed:    gen.Detailed,
	}
	for i, c := range gen.Characters {
		sets := make([]Assignment, 0, len(c.Attributes))
		for attr, v := range c.Attributes {
			sets = append(sets, Assignment{Key: attr, Value: v})
		}
		makeActive := o.MakeActive && i == 0
		// CreateCharacter finishes the sheet with the rulebook's character
		// rules too: the generated attributes let the derived initial
		// resource tracks (CoC HP/MP/SAN, D&D HP, ...) initialize right
		// away, so a generated card is playable as-is.
		card, derived, err := a.CreateCharacter(ctx, o.SystemID, names[i], sets, makeActive)
		if err != nil {
			return nil, err
		}
		var vitals []engine.VitalsValue
		if derived != nil {
			vitals = derived.Vitals
		}
		res.Characters = append(res.Characters, &GeneratedSheet{
			Name:        card.Name,
			ID:          card.ID,
			Attributes:  c.Attributes,
			Rolls:       c.Rolls,
			PointBuy:    c.PointBuy,
			Vitals:      vitals,
			Explanation: c.Explanation,
		})
	}
	return res, nil
}

// generatedNames builds the names of the generated characters: the given
// base name (with a number suffix) or random names from the data pack.
func (a *App) generatedNames(ctx context.Context, count int, base string) ([]string, error) {
	if base != "" {
		out := make([]string, 0, count)
		for i := 0; i < count; i++ {
			if count == 1 {
				out = append(out, base)
				continue
			}
			out = append(out, fmt.Sprintf("%s %d", base, i+1))
		}
		return out, nil
	}
	if res, err := a.RandomNames(ctx, "", count); err == nil {
		return res.Names, nil
	}
	// No name set available: fall back to a generic name.
	out := make([]string, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, "调查员 "+strconv.Itoa(i+1))
	}
	return out, nil
}

// RedoCharacter regenerates the characteristics of an existing card (the
// Dice! `.pc redo` / `.pc build` commands): the old values are cleared and
// replaced by a freshly generated set, the derived tracks are re-initialized
// and the card is saved through the usual backup/atomic-update path.
//
// The card keeps its identity (id, name and rulebook); variantID selects the
// generation variant (empty = the rulebook's default).
func (a *App) RedoCharacter(ctx context.Context, characterID, variantID string) (*GeneratedSheet, *character.Character, error) {
	const op = "app.RedoCharacter"
	c, err := a.GetCharacter(ctx, characterID)
	if err != nil {
		return nil, nil, err
	}
	book, err := a.selectSystem(ctx, "", c)
	if err != nil {
		return nil, nil, err
	}
	if book.Generation() == nil {
		return nil, nil, apperr.New(op, apperr.InvalidArgument,
			"rulebook "+book.Manifest().ID+" declares no character generation")
	}
	gen, err := book.Generate(ctx, engine.GenerationInput{VariantID: variantID, Count: 1})
	if err != nil {
		return nil, nil, err
	}
	gc := gen.Characters[0]
	if c.Attributes == nil {
		c.Attributes = map[string]int{}
	}
	for attr := range c.Attributes {
		delete(c.Attributes, attr)
	}
	for attr, v := range gc.Attributes {
		c.Attributes[attr] = v
	}
	c.Skills = map[string]int{}
	if err := c.Validate(); err != nil {
		return nil, nil, err
	}
	if err := a.validateBySystem(ctx, book, c); err != nil {
		return nil, nil, err
	}
	if err := a.chars.Update(ctx, c); err != nil {
		return nil, nil, err
	}
	// The derived tracks depend on the new characteristics.
	d, _, err := a.RunDerived(ctx, DerivedOptions{
		CharacterID: c.ID,
		Initialize:  true,
		Overwrite:   true,
	})
	if err != nil {
		return nil, nil, err
	}
	updated, err := a.GetCharacter(ctx, c.ID)
	if err != nil {
		return nil, nil, err
	}
	return &GeneratedSheet{
		Name:        updated.Name,
		ID:          updated.ID,
		Attributes:  gc.Attributes,
		Rolls:       gc.Rolls,
		PointBuy:    gc.PointBuy,
		Vitals:      d.Vitals,
		Explanation: gc.Explanation,
	}, updated, nil
}
