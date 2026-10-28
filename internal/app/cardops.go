package app

import (
	"context"
	"strings"

	"dicegirl/internal/apperr"
	"dicegirl/internal/character"
)

// Card operations that go beyond "create, read, update, delete": renaming,
// copying one card's values onto another and destroying every card. They are
// the Dice! `.pc nn` / `.pc cpy` / `.pc clr` commands.

// RenameCharacter renames a card (the active sheet when id is empty). The
// card's identity (its id) and its rulebook stay untouched; the name must
// stay unique, as in the Dice! manual.
func (a *App) RenameCharacter(ctx context.Context, id, name string) (*character.Character, error) {
	const op = "app.RenameCharacter"
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, apperr.New(op, apperr.InvalidArgument, "character name must not be empty")
	}
	c, err := a.GetCharacter(ctx, id)
	if err != nil {
		return nil, err
	}
	chars, err := a.ListCharacters(ctx)
	if err != nil {
		return nil, err
	}
	for _, other := range chars {
		if other.ID != c.ID && other.Name == name {
			return nil, apperr.New(op, apperr.InvalidArgument,
				"character name already in use: "+name)
		}
	}
	c.Name = name
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if err := a.chars.Update(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// CopyCharacterProperties copies the values of one card onto another
// (the Dice! `.pc cpy A=B` command): the attributes, skills, resource
// tracks and macros are taken over, while the identity (id, name and
// rulebook) stays with the target card.
func (a *App) CopyCharacterProperties(ctx context.Context, targetID, sourceID string) (*character.Character, error) {
	const op = "app.CopyCharacterProperties"
	target, err := a.GetCharacter(ctx, targetID)
	if err != nil {
		return nil, err
	}
	source, err := a.GetCharacter(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	if target.ID == source.ID {
		return nil, apperr.New(op, apperr.InvalidArgument, "cannot copy a card onto itself")
	}
	cp := source.Clone()
	target.Attributes = cp.Attributes
	target.Skills = cp.Skills
	target.Resources = cp.Resources
	target.Macros = cp.Macros
	target.SkillPoints = cp.SkillPoints
	if err := target.Validate(); err != nil {
		return nil, err
	}
	sys, err := a.registry.MustGet(target.SystemID)
	if err != nil {
		return nil, err
	}
	if err := a.validateBySystem(ctx, sys, target); err != nil {
		return nil, err
	}
	if err := a.chars.Update(ctx, target); err != nil {
		return nil, err
	}
	return target, nil
}

// ClearCharacters deletes every character card and returns how many were
// removed. The active character pointer is cleared as well.
func (a *App) ClearCharacters(ctx context.Context) (int, error) {
	chars, err := a.ListCharacters(ctx)
	if err != nil {
		return 0, err
	}
	for _, c := range chars {
		if err := a.DeleteCharacter(ctx, c.ID); err != nil {
			return 0, err
		}
	}
	return len(chars), nil
}

// SetCharacterMeta sets a metadata entry of a card; an empty value removes
// the entry. Metadata is the documented extension point of the card (for
// example the per-card default die size).
func (a *App) SetCharacterMeta(ctx context.Context, id, key, value string) (*character.Character, error) {
	const op = "app.SetCharacterMeta"
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, apperr.New(op, apperr.InvalidArgument, "metadata key must not be empty")
	}
	c, err := a.GetCharacter(ctx, id)
	if err != nil {
		return nil, err
	}
	if c.Metadata == nil {
		c.Metadata = map[string]string{}
	}
	if value == "" {
		delete(c.Metadata, key)
	} else {
		c.Metadata[key] = value
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if err := a.chars.Update(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}
