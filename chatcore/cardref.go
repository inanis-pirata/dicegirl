package chatcore

import (
	"context"
	"strings"

	"dicegirl/internal/app"
	"dicegirl/internal/apperr"
)

// cardRef resolves a card reference typed by a player. Players normally use
// the character name (the program-generated id is not something they see or
// remember), so a reference is accepted as:
//
//  1. an exact card id ("c-1a2b3c4d");
//  2. an exact card name (names are unique: rename/create refuse duplicates);
//  3. a case-insensitive card name.
//
// An empty reference means the active card and resolves to "", letting the
// app layer apply its own active-card rule.
// ResolveCardRef resolves a player-typed card reference (id, exact name or
// case-insensitive name) to a card id. Adapters use it to validate a card
// before remembering a member's binding.
func ResolveCardRef(ctx context.Context, a *app.App, ref string) (string, error) {
	return cardRef(ctx, a, ref)
}

func cardRef(ctx context.Context, a *app.App, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil
	}
	chars, err := a.ListCharacters(ctx)
	if err != nil {
		return "", err
	}
	for _, c := range chars {
		if c.ID == ref {
			return c.ID, nil
		}
	}
	var exact, folded []string
	for _, c := range chars {
		switch {
		case c.Name == ref:
			exact = append(exact, c.ID)
		case strings.EqualFold(c.Name, ref):
			folded = append(folded, c.ID)
		}
	}
	for _, ids := range [][]string{exact, folded} {
		switch len(ids) {
		case 0:
			continue
		case 1:
			return ids[0], nil
		default:
			return "", apperr.New("cli.cardRef", apperr.InvalidArgument,
				"角色名 "+ref+" 对应多张卡，请改用 ID")
		}
	}
	return "", apperr.New("cli.cardRef", apperr.NotFound,
		"找不到角色："+ref+"（可用 pc list 查看）")
}
