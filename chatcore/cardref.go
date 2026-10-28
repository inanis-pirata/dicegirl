package chatcore

import (
	"context"

	"dicegirl/internal/app"
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
//
// The tiers themselves live in resolveTargetRef (chatcore/target.go) so the
// "@名字" path and the "--character" path share one vocabulary; this wrapper
// keeps the id-only signature used across the command layer and by adapters.
//
// 🔴 It must never consult the message's platform mentions (C1). Every caller
// of cardRef is a positional name: `.pc show 张三`, `.pc tag 张三`, `.pc del`,
// `.st show/del 张三`. If cardRef resolved through a mention hint first, then in
// a group where two cards share the name 张三, `.pc show 张三` would stop
// reporting "角色名 张三 对应多张卡，请改用 ID" and silently show whichever of
// them was @-mentioned — note that app.CreateCharacter does not refuse
// duplicate names, so this is reachable, not theoretical. Mention resolution
// belongs to resolveTargetWith, where the token shape says it is a mention.
func cardRef(ctx context.Context, a *app.App, ref string) (string, error) {
	t, err := resolveTargetRef(ctx, a, ref)
	if err != nil {
		return "", err
	}
	return t.CardID, nil
}

// ResolveCardRef resolves a player-typed card reference (id, exact name or
// case-insensitive name) to a card id. Adapters use it to validate a card
// before remembering a member's binding.
func ResolveCardRef(ctx context.Context, a *app.App, ref string) (string, error) {
	return cardRef(ctx, a, ref)
}
