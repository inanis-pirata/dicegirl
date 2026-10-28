package app

import (
	"context"
	"strings"
	"time"

	"dicegirl/internal/apperr"
	"dicegirl/internal/character"
	"dicegirl/internal/engine"
	"dicegirl/internal/storage"
)

// CheckTarget describes how a check target is resolved.
type CheckTarget struct {
	SystemID    string
	CharacterID string
	// Skill, Attr or Resource is the skill/characteristic/resource-track
	// name (ID or localized label); Resource addresses a track such as san.
	Skill    string
	Attr     string
	Resource string
	// Value is an explicit target; Explicit marks that --target was given
	// (target 0 is a legal value in some rulebooks).
	Value    int
	Explicit bool
	Bonus    int
	Penalty  int
	// Difficulty is an optional checks.difficulties id or localized name
	// (explicit difficulty modifier such as 困难/极难/自动成功).
	Difficulty string
	// Adjust optionally rewrites the resolved target value (the Dice!
	// `体质*5` convention and the explicit `--mod` correction). It is
	// applied by the engine before any difficulty modifier.
	Adjust engine.TargetAdjust
	// Repeat runs the check independently Repeat times (1..engine.MaxCheckRepeat).
	Repeat int
}

// RunCheck resolves the target against a rulebook and runs its check.
func (a *App) RunCheck(ctx context.Context, t CheckTarget) (*engine.CheckResult, error) {
	book, in, cardID, err := a.resolveCheck(ctx, t)
	if err != nil {
		return nil, err
	}
	res, err := book.Check(ctx, in)
	if err != nil {
		return nil, err
	}
	if err := a.recordCheck(ctx, cardID, book, res, in); err != nil {
		return nil, err
	}
	return res, nil
}

// RunCheckBatch resolves the target once and runs the check Repeat times,
// each trial rolling its own dice independently.
func (a *App) RunCheckBatch(ctx context.Context, t CheckTarget) (*engine.CheckBatchResult, error) {
	book, in, cardID, err := a.resolveCheck(ctx, t)
	if err != nil {
		return nil, err
	}
	repeat := t.Repeat
	if repeat == 0 {
		repeat = 1
	}
	batch, err := book.CheckMany(ctx, engine.CheckBatchInput{CheckInput: in, Repeat: repeat})
	if err != nil {
		return nil, err
	}
	for _, res := range batch.Checks {
		if err := a.recordCheck(ctx, cardID, book, res, in); err != nil {
			return nil, err
		}
	}
	return batch, nil
}

// recordCheck adds one check to the card's roll statistics. Checks without
// a card (a bare --target check) and checks without a subject (no skill or
// characteristic) are not recorded.
func (a *App) recordCheck(ctx context.Context, cardID string, book *engine.Book,
	res *engine.CheckResult, in engine.CheckInput) error {
	if cardID == "" {
		return nil
	}
	key, kind, name := in.SkillID, storage.StatKindSkill, in.SkillName
	switch {
	case key != "":
	case in.AttrID != "":
		key, kind, name = in.AttrID, storage.StatKindAttribute, in.AttrName
	case in.ResourceID != "":
		key, kind, name = in.ResourceID, storage.StatKindResource, in.ResourceName
	default:
		// A target-only check has no subject to aggregate by.
		return nil
	}
	return a.stats.Record(ctx, storage.StatRecord{
		CardID:    cardID,
		Key:       key,
		Kind:      kind,
		Name:      name,
		Target:    res.Target,
		Roll:      res.RawRoll,
		LevelCode: res.LevelCode,
		Success:   book.LevelSucceeds(res),
		Fumble:    res.LevelCode == book.FumbleLevel(),
		At:        time.Now().UTC(),
	})
}

// resolveCheck maps a CheckTarget to the rulebook and a fully resolved engine
// input (target value, labels, bonus/penalty and difficulty all finalized).
func (a *App) resolveCheck(ctx context.Context, t CheckTarget) (*engine.Book, engine.CheckInput, string, error) {
	switch {
	case t.Explicit:
		// The rulebook comes from the same context as the other
		// branches: an explicit --system wins, then the active card's
		// system, then the only loaded book.
		book, c, err := a.skillContext(ctx, t.SystemID, t.CharacterID)
		if err != nil {
			return nil, engine.CheckInput{}, "", err
		}
		diff, err := resolveDifficulty(book, t.Difficulty)
		if err != nil {
			return nil, engine.CheckInput{}, "", err
		}
		return book, engine.CheckInput{
			Target:     t.Value,
			Bonus:      t.Bonus,
			Penalty:    t.Penalty,
			SkillName:  t.Skill, // label only when an explicit value is given
			Difficulty: diff,
			Adjust:     t.Adjust,
		}, cardIDOf(c), nil
	case t.Skill != "":
		book, c, err := a.skillContext(ctx, t.SystemID, t.CharacterID)
		if err != nil {
			return nil, engine.CheckInput{}, "", err
		}
		diff, err := resolveDifficulty(book, t.Difficulty)
		if err != nil {
			return nil, engine.CheckInput{}, "", err
		}
		def, ok := book.FindSkill(t.Skill)
		if !ok {
			return nil, engine.CheckInput{}, "", apperr.New("app.RunCheck", apperr.InvalidArgument,
				"unknown skill: "+t.Skill)
		}
		in := engine.CheckInput{Bonus: t.Bonus, Penalty: t.Penalty, SkillName: def.Name,
			SkillID: def.ID, Difficulty: diff, Adjust: t.Adjust}
		if c != nil {
			if v, ok := c.Skills[def.ID]; ok {
				in.Target = v
			} else {
				base, err := book.SkillBase(def, c.Attributes)
				if err != nil {
					return nil, engine.CheckInput{}, "", err
				}
				in.Target = base
				in.SkillName = def.Name + "（基础）"
			}
		} else {
			base, err := book.SkillBase(def, nil)
			if err != nil {
				return nil, engine.CheckInput{}, "", err
			}
			in.Target = base
		}
		return book, in, cardIDOf(c), nil
	case t.Attr != "":
		c, err := a.GetCharacter(ctx, t.CharacterID)
		if err != nil {
			return nil, engine.CheckInput{}, "", err
		}
		book, err := a.selectSystem(ctx, t.SystemID, c)
		if err != nil {
			return nil, engine.CheckInput{}, "", err
		}
		diff, err := resolveDifficulty(book, t.Difficulty)
		if err != nil {
			return nil, engine.CheckInput{}, "", err
		}
		def, ok := book.FindAttribute(t.Attr)
		if !ok {
			return nil, engine.CheckInput{}, "", apperr.New("app.RunCheck", apperr.InvalidArgument,
				"unknown characteristic: "+t.Attr)
		}
		v, ok := c.Attributes[def.ID]
		if !ok {
			return nil, engine.CheckInput{}, "", apperr.New("app.RunCheck", apperr.InvalidArgument,
				"character is missing characteristic "+def.ID)
		}
		return book, engine.CheckInput{
			Target:     v,
			Bonus:      t.Bonus,
			Penalty:    t.Penalty,
			AttrName:   def.Name,
			AttrID:     def.ID,
			Difficulty: diff,
			Adjust:     t.Adjust,
		}, c.ID, nil
	case t.Resource != "":
		// A resource-track check (ra 理智): the target is the track's
		// current value, or the rulebook's fallback formula when the sheet
		// has no track yet (the same chain the sc path uses).
		c, err := a.GetCharacter(ctx, t.CharacterID)
		if err != nil {
			return nil, engine.CheckInput{}, "", err
		}
		book, err := a.selectSystem(ctx, t.SystemID, c)
		if err != nil {
			return nil, engine.CheckInput{}, "", err
		}
		diff, err := resolveDifficulty(book, t.Difficulty)
		if err != nil {
			return nil, engine.CheckInput{}, "", err
		}
		track, ok := book.ResolveResource(t.Resource)
		if !ok {
			return nil, engine.CheckInput{}, "", apperr.New("app.RunCheck", apperr.InvalidArgument,
				"unknown resource: "+t.Resource)
		}
		target := 0
		if r, has := c.Resources[track]; has {
			target = r.Current
		} else {
			target, err = book.ResourceFallbackTarget(c, track)
			if err != nil {
				return nil, engine.CheckInput{}, "", err
			}
		}
		return book, engine.CheckInput{
			Target:       target,
			Bonus:        t.Bonus,
			Penalty:      t.Penalty,
			ResourceName: book.ResourceDisplayName(track),
			ResourceID:   track,
			Difficulty:   diff,
			Adjust:       t.Adjust,
		}, c.ID, nil
	default:
		return nil, engine.CheckInput{}, "", apperr.New("app.RunCheck", apperr.InvalidArgument,
			"check requires --skill, --attr or --target")
	}
}

// cardIDOf returns the id of a character card (empty when there is none).
func cardIDOf(c *character.Character) string {
	if c == nil {
		return ""
	}
	return c.ID
}

// resolveDifficulty maps a difficulty id or localized label to its
// canonical id; empty input means no modifier.
func resolveDifficulty(book *engine.Book, name string) (string, error) {
	if name == "" {
		return "", nil
	}
	d, ok := book.FindDifficulty(name)
	if !ok {
		return "", apperr.New("app.RunCheck", apperr.InvalidArgument,
			"unknown difficulty: "+name)
	}
	return d.ID, nil
}

// Difficulties lists the explicit-difficulty modifiers declared by the
// rulebook applicable to the (active) character context. The UI layer uses
// it to recognize free-form keywords such as 困难.
func (a *App) Difficulties(ctx context.Context, systemID, characterID string) ([]engine.DifficultyInfo, error) {
	book, _, err := a.skillContext(ctx, systemID, characterID)
	if err != nil {
		return nil, err
	}
	return book.Difficulties(), nil
}

// ResourceCheckBinding returns the resource-check id a chat verb (sc) is
// bound to in the rulebook applicable to the (active) character context.
func (a *App) ResourceCheckBinding(ctx context.Context, systemID, characterID, verb string) (string, error) {
	book, _, err := a.skillContext(ctx, systemID, characterID)
	if err != nil {
		return "", err
	}
	b, ok := book.Binding(verb)
	if !ok || b.Kind != engine.BindingKindResourceCheck {
		return "", apperr.New("app.ResourceCheckBinding", apperr.InvalidArgument,
			"当前规则书未声明 "+verb+" 绑定")
	}
	return b.Target, nil
}

// TableBinding returns the table id a chat verb (ti/li) is bound to in the
// applicable rulebook.
func (a *App) TableBinding(ctx context.Context, systemID, verb string) (string, error) {
	book, err := a.selectSystem(ctx, systemID, nil)
	if err != nil {
		return "", err
	}
	b, ok := book.Binding(verb)
	if !ok || b.Kind != engine.BindingKindTable {
		return "", apperr.New("app.TableBinding", apperr.InvalidArgument,
			"当前规则书未声明 "+verb+" 绑定")
	}
	return b.Target, nil
}

// ResolveTargetValue resolves a free-form check name to its sheet value
// (the opposed-check shorthand `rav 侦查 60`): skill, characteristic or
// resource track, through the same resolution as a named check.
func (a *App) ResolveTargetValue(ctx context.Context, systemID, characterID, name string) (int, error) {
	skillID, attrID, resourceID, err := a.ResolveCheckName(ctx, systemID, characterID, name)
	if err != nil {
		return 0, err
	}
	t := CheckTarget{SystemID: systemID, CharacterID: characterID, Skill: skillID, Attr: attrID, Resource: resourceID}
	_, in, _, err := a.resolveCheck(ctx, t)
	if err != nil {
		return 0, err
	}
	return in.Target, nil
}

// ResolveCheckName disambiguates a free-form check name (command
// syntax has no --skill/--attr split): it returns the canonical skill id,
// attribute id or resource-track id the name refers to in the rulebook
// applicable to the (active) character context. Exactly one return id is
// non-empty.
func (a *App) ResolveCheckName(ctx context.Context, systemID, characterID, name string) (skillID, attrID, resourceID string, err error) {
	book, _, err := a.skillContext(ctx, systemID, characterID)
	if err != nil {
		return "", "", "", err
	}
	if def, ok := book.FindSkill(name); ok {
		return def.ID, "", "", nil
	}
	if def, ok := book.FindAttribute(name); ok {
		return "", def.ID, "", nil
	}
	if track, ok := book.ResolveResource(name); ok {
		return "", "", track, nil
	}
	return "", "", "", apperr.New("app.ResolveCheckName", apperr.InvalidArgument,
		"unknown skill, characteristic or resource: "+name)
}

// skillContext returns the rulebook and (optionally) the character that
// supplies a skill value: explicit --character, the active character, or
// no character (rulebook base values only).
func (a *App) skillContext(ctx context.Context, systemID, charID string) (*engine.Book, *character.Character, error) {
	if charID == "" {
		active, err := a.state.ActiveID(ctx)
		if err != nil {
			return nil, nil, err
		}
		charID = active
	}
	if charID == "" {
		book, err := a.selectSystem(ctx, systemID, nil)
		return book, nil, err
	}
	c, err := a.chars.Get(ctx, charID)
	if err != nil {
		return nil, nil, err
	}
	book, err := a.selectSystem(ctx, systemID, c)
	return book, c, err
}

// RunOpposed runs two target-only checks and resolves them per the
// rulebook's opposed policy.
func (a *App) RunOpposed(ctx context.Context, systemID string, targetA, targetB int) (*engine.OpposedResult, error) {
	book, err := a.selectSystem(ctx, systemID, nil)
	if err != nil {
		return nil, err
	}
	ca, err := book.Check(ctx, engine.CheckInput{Target: targetA, SkillName: "A"})
	if err != nil {
		return nil, err
	}
	cb, err := book.Check(ctx, engine.CheckInput{Target: targetB, SkillName: "B"})
	if err != nil {
		return nil, err
	}
	return book.Opposed(ca, cb)
}

// ResourceOptions parameterizes a rulebook-declared resource check.
type ResourceOptions struct {
	SystemID    string
	CharacterID string
	CheckID     string
	SuccessLoss string
	FailureLoss string
	Bonus       int
	Penalty     int
	Apply       bool
	// CurrentOverride explicitly sets the value the check runs against
	// (the Dice! `.sc 0/1 70` convention). It never rewrites the track:
	// the proposed change is still computed from the track's own value.
	CurrentOverride *int
}

// RunResourceCheck runs a declared resource check (e.g. sanity). The check
// target is the track's current value, falling back to the rulebook formula
// when the track does not exist yet. With Apply the proposed change is
// written back; otherwise the result is advisory only.
func (a *App) RunResourceCheck(ctx context.Context, o ResourceOptions) (*engine.ResourceCheckResult, error) {
	c, err := a.GetCharacter(ctx, o.CharacterID)
	if err != nil {
		return nil, err
	}
	book, err := a.selectSystem(ctx, o.SystemID, c)
	if err != nil {
		return nil, err
	}
	rc, ok := book.ResourceCheck(o.CheckID)
	if !ok {
		return nil, apperr.New("app.RunResourceCheck", apperr.InvalidArgument,
			"rulebook declares no resource check "+o.CheckID)
	}

	before := 0
	if r, ok := c.Resources[rc.Resource()]; ok {
		before = r.Current
	} else {
		before, err = book.ResourceFallbackTarget(c, rc.Resource())
		if err != nil {
			return nil, err
		}
	}
	succ := rc.DefaultSuccessLoss()
	fail := rc.DefaultFailureLoss()
	if strings.TrimSpace(o.SuccessLoss) != "" {
		succ, err = engine.ParseLossSpec(o.SuccessLoss)
		if err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(o.FailureLoss) != "" {
		fail, err = engine.ParseLossSpec(o.FailureLoss)
		if err != nil {
			return nil, err
		}
	}

	res, err := book.RunResourceCheck(ctx, rc, engine.ResourceCheckInput{
		Before:      before,
		Target:      o.CurrentOverride,
		SuccessLoss: succ,
		FailureLoss: fail,
		Bonus:       o.Bonus,
		Penalty:     o.Penalty,
	})
	if err != nil {
		return nil, err
	}
	// The resource check counts towards the card's statistics under its
	// resource track id (san), like a resource-targeted check would.
	if res.Check != nil {
		if err := a.stats.Record(ctx, storage.StatRecord{
			CardID:    c.ID,
			Key:       rc.Resource(),
			Kind:      storage.StatKindResource,
			Name:      book.ResourceDisplayName(rc.Resource()),
			Target:    res.Check.Target,
			Roll:      res.Check.RawRoll,
			LevelCode: res.Check.LevelCode,
			Success:   book.LevelSucceeds(res.Check),
			Fumble:    res.Check.LevelCode == book.FumbleLevel(),
			At:        time.Now().UTC(),
		}); err != nil {
			return nil, err
		}
	}
	if o.Apply {
		if err := a.applyResource(ctx, book, c, res.Change); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// DamageOptions parameterizes a weapon-damage roll.
type DamageOptions struct {
	SystemID    string
	Expr        string
	CharacterID string
}

// RunDamage rolls weapon damage, adding the rulebook lookup's damage bonus
// when a character is available.
func (a *App) RunDamage(ctx context.Context, o DamageOptions) (*engine.DamageResult, error) {
	var c *character.Character
	if o.CharacterID != "" {
		var err error
		c, err = a.chars.Get(ctx, o.CharacterID)
		if err != nil {
			return nil, err
		}
	} else if id, err := a.state.ActiveID(ctx); err != nil {
		return nil, err
	} else if id != "" {
		c, _ = a.chars.Get(ctx, id)
	}
	book, err := a.selectSystem(ctx, o.SystemID, c)
	if err != nil {
		return nil, err
	}
	return book.Damage(ctx, engine.DamageInput{Expr: o.Expr, Character: c})
}

// DerivedOptions parameterizes derived-statistic display/initialization.
type DerivedOptions struct {
	SystemID    string
	CharacterID string
	Initialize  bool
	Overwrite   bool
}

// RunDerived reports derived statistics. With Initialize it also creates
// missing resource tracks and initial attributes (Overwrite resets existing
// tracks), validates and persists the sheet.
func (a *App) RunDerived(ctx context.Context, o DerivedOptions) (*engine.DerivedResult, *character.Character, error) {
	c, err := a.GetCharacter(ctx, o.CharacterID)
	if err != nil {
		return nil, nil, err
	}
	book, err := a.selectSystem(ctx, o.SystemID, c)
	if err != nil {
		return nil, nil, err
	}
	d, err := book.Derive(c)
	if err != nil {
		return nil, nil, err
	}
	if o.Initialize {
		if err := a.applyDerived(ctx, book, c, d, o.Overwrite); err != nil {
			return nil, nil, err
		}
	}
	return d, c, nil
}

// applyDerived records the rulebook-derived initial resource tracks and
// initial attributes on a sheet, validates and persists it. With overwrite,
// tracks that already exist are reset; otherwise an existing track keeps its
// current value but its maximum is refreshed from the formulas (contract
// §9.4: a level-up changes max_hp, not the HP you still have). Current is
// clamped into the new range. Initial attributes are always only filled in
// when missing.
func (a *App) applyDerived(ctx context.Context, book *engine.Book, c *character.Character, d *engine.DerivedResult, overwrite bool) error {
	if c.Resources == nil {
		c.Resources = map[string]character.Resource{}
	}
	for _, v := range d.Vitals {
		if !overwrite {
			if existing, exists := c.Resources[v.Resource]; exists {
				refreshed := existing
				refreshed.Max = v.Max
				if refreshed.Current > refreshed.Max {
					refreshed.Current = refreshed.Max
				}
				c.Resources[v.Resource] = refreshed
				continue
			}
		}
		c.Resources[v.Resource] = character.Resource{Current: v.Current, Max: v.Max}
	}
	for _, ia := range d.InitAttributes {
		if _, exists := c.Attributes[ia.Attribute]; !exists {
			if c.Attributes == nil {
				c.Attributes = map[string]int{}
			}
			c.Attributes[ia.Attribute] = ia.Value
		}
	}
	if err := c.Validate(); err != nil {
		return err
	}
	if err := a.validateBySystem(ctx, book, c); err != nil {
		return err
	}
	return a.chars.Update(ctx, c)
}

// applyResource writes one proposed resource change, deriving the track
// maximum from the rulebook when the track is first seen. The written value
// is clamped to the track maximum, so a recovery can never exceed it.
func (a *App) applyResource(ctx context.Context, book *engine.Book, c *character.Character, ch engine.ResourceChange) error {
	if c.Resources == nil {
		c.Resources = map[string]character.Resource{}
	}
	r := c.Resources[ch.Resource]
	if r.Max == 0 {
		if max, found, err := book.ResourceMax(c, ch.Resource); err != nil {
			return err
		} else if found {
			r.Max = max
		}
	}
	after := ch.After
	if r.Max > 0 && after > r.Max {
		after = r.Max
	}
	if after < 0 {
		after = 0
	}
	r.Current = after
	c.Resources[ch.Resource] = r
	if err := c.Validate(); err != nil {
		return err
	}
	if err := a.validateBySystem(ctx, book, c); err != nil {
		return err
	}
	return a.chars.Update(ctx, c)
}
