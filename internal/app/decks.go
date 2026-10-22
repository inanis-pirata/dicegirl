package app

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"dicegirl/internal/apperr"
	"dicegirl/internal/storage"
)

// Card decks (the Dice! `.deck` / `.draw` commands): named decks of cards.
// A deck keeps its full definition (Cards), a draw pile (Remaining) and a
// discard pile (Discard); Remaining + Discard is always the same multiset as
// Cards. Cards are normally drawn without replacement and move to the
// discard pile; a replacement draw leaves both piles untouched. `deck put`
// moves named cards back from the discard pile, and `deck reset` merges the
// piles and reshuffles. The group-only features (member decks, secret draws)
// are delivered by the chat adapter, not here.

// MaxDeckCards bounds one deck's definition.
const MaxDeckCards = 500

// Deck is one card deck with its definition and both piles.
type Deck struct {
	Name      string   `json:"name"`
	Cards     []string `json:"cards"`
	Remaining []string `json:"remaining"`
	Discard   []string `json:"discard,omitempty"`
}

// DeckList is the collection of decks.
type DeckList struct {
	Kind  string `json:"kind"`
	Decks []Deck `json:"decks"`
}

// DrawOptions parameterizes a draw.
type DrawOptions struct {
	// Count is the number of cards to draw.
	Count int
	// Replace draws with replacement: the cards stay in the draw pile and
	// nothing moves to the discard pile.
	Replace bool
}

// DrawResult is the outcome of a draw.
type DrawResult struct {
	Kind    string   `json:"kind"`
	Deck    string   `json:"deck"`
	Cards   []string `json:"cards"`
	Left    int      `json:"left"`
	Discard int      `json:"discard"`
	Replace bool     `json:"replace,omitempty"`
}

// NewDeck defines a deck from a card specification: cards separated by "|",
// with "::n::card" repeating a card n times.
func (a *App) NewDeck(ctx context.Context, name, spec string) (*Deck, error) {
	const op = "app.NewDeck"
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, apperr.New(op, apperr.InvalidArgument, "deck name must not be empty")
	}
	cards, err := parseDeckSpec(spec)
	if err != nil {
		return nil, err
	}
	tid := tableIDFromContext(ctx)
	set, err := a.decks.Decks(ctx, tid)
	if err != nil {
		return nil, err
	}
	if _, exists := set[name]; exists {
		return nil, apperr.New(op, apperr.Conflict, "deck already exists: "+name)
	}
	deck := &storage.Deck{Cards: cards, Remaining: append([]string(nil), cards...)}
	if err := a.shuffleDeck(ctx, deck.Remaining); err != nil {
		return nil, err
	}
	set[name] = deck
	if err := a.decks.SetDecks(ctx, tid, set); err != nil {
		return nil, err
	}
	return deckView(name, deck), nil
}

// SetDeck creates a deck from another deck ("实例=公共名") or from an
// arithmetic range ("实例=range 下限 上限").
func (a *App) SetDeck(ctx context.Context, name, source string) (*Deck, error) {
	const op = "app.SetDeck"
	name = strings.TrimSpace(name)
	source = strings.TrimSpace(source)
	if name == "" {
		return nil, apperr.New(op, apperr.InvalidArgument, "deck name must not be empty")
	}
	tid := tableIDFromContext(ctx)
	set, err := a.decks.Decks(ctx, tid)
	if err != nil {
		return nil, err
	}
	if _, exists := set[name]; exists {
		return nil, apperr.New(op, apperr.Conflict, "deck already exists: "+name)
	}
	var cards []string
	if strings.HasPrefix(source, "range ") {
		cards, err = parseDeckRange(strings.TrimSpace(strings.TrimPrefix(source, "range ")))
	} else {
		src, ok := set[source]
		if !ok {
			return nil, apperr.New(op, apperr.InvalidArgument, "unknown deck: "+source)
		}
		cards = append(cards, src.Cards...)
	}
	if len(cards) == 0 {
		return nil, apperr.New(op, apperr.InvalidArgument, "deck has no cards")
	}
	deck := &storage.Deck{Cards: cards, Remaining: append([]string(nil), cards...)}
	if err := a.shuffleDeck(ctx, deck.Remaining); err != nil {
		return nil, err
	}
	set[name] = deck
	if err := a.decks.SetDecks(ctx, tid, set); err != nil {
		return nil, err
	}
	return deckView(name, deck), nil
}

// ShowDecks lists every deck with its pile sizes.
func (a *App) ShowDecks(ctx context.Context) (*DeckList, error) {
	tid := tableIDFromContext(ctx)
	set, err := a.decks.Decks(ctx, tid)
	if err != nil {
		return nil, err
	}
	out := &DeckList{Kind: "decks", Decks: make([]Deck, 0, len(set))}
	for name, deck := range set {
		out.Decks = append(out.Decks, *deckView(name, deck))
	}
	sort.Slice(out.Decks, func(i, j int) bool { return out.Decks[i].Name < out.Decks[j].Name })
	return out, nil
}

// ShowDeck returns one deck.
func (a *App) ShowDeck(ctx context.Context, name string) (*Deck, error) {
	const op = "app.ShowDeck"
	tid := tableIDFromContext(ctx)
	set, err := a.decks.Decks(ctx, tid)
	if err != nil {
		return nil, err
	}
	deck, ok := set[strings.TrimSpace(name)]
	if !ok {
		return nil, apperr.New(op, apperr.InvalidArgument, "unknown deck: "+name)
	}
	return deckView(strings.TrimSpace(name), deck), nil
}

// ResetDeck merges the discard pile back and reshuffles the deck.
func (a *App) ResetDeck(ctx context.Context, name string) (*Deck, error) {
	const op = "app.ResetDeck"
	tid := tableIDFromContext(ctx)
	set, err := a.decks.Decks(ctx, tid)
	if err != nil {
		return nil, err
	}
	deck, ok := set[strings.TrimSpace(name)]
	if !ok {
		return nil, apperr.New(op, apperr.InvalidArgument, "unknown deck: "+name)
	}
	deck.Remaining = append([]string(nil), deck.Cards...)
	deck.Discard = nil
	if err := a.shuffleDeck(ctx, deck.Remaining); err != nil {
		return nil, err
	}
	if err := a.decks.SetDecks(ctx, tid, set); err != nil {
		return nil, err
	}
	return deckView(strings.TrimSpace(name), deck), nil
}

// ClearDecks removes every deck of the current table.
func (a *App) ClearDecks(ctx context.Context) error {
	tid := tableIDFromContext(ctx)
	return a.decks.SetDecks(ctx, tid, map[string]*storage.Deck{})
}

// DrawCards draws cards from a deck. Without replacement (the default) the
// drawn cards move from the draw pile to the discard pile; with replacement
// both piles are left untouched and repeated faces are possible.
func (a *App) DrawCards(ctx context.Context, name string, o DrawOptions) (*DrawResult, error) {
	const op = "app.DrawCards"
	count := o.Count
	if count < 1 {
		return nil, apperr.New(op, apperr.InvalidArgument, "draw count must be at least 1")
	}
	tid := tableIDFromContext(ctx)
	set, err := a.decks.Decks(ctx, tid)
	if err != nil {
		return nil, err
	}
	deck, ok := set[strings.TrimSpace(name)]
	if !ok {
		return nil, apperr.New(op, apperr.InvalidArgument, "unknown deck: "+name)
	}
	if len(deck.Remaining) == 0 {
		return nil, apperr.New(op, apperr.InvalidArgument,
			"牌堆「"+name+"」已经抽空（可用 deck reset 重洗或 deck put 放回）")
	}
	drawn := make([]string, 0, count)
	if o.Replace {
		for i := 0; i < count; i++ {
			idx, err := a.roller.Intn(ctx, len(deck.Remaining))
			if err != nil {
				return nil, err
			}
			drawn = append(drawn, deck.Remaining[idx])
		}
	} else {
		if count > len(deck.Remaining) {
			return nil, apperr.New(op, apperr.InvalidArgument,
				"deck "+name+" has only "+strconv.Itoa(len(deck.Remaining))+" cards left")
		}
		drawn = append(drawn, deck.Remaining[:count]...)
		deck.Discard = append(deck.Discard, drawn...)
		deck.Remaining = deck.Remaining[count:]
	}
	if err := a.decks.SetDecks(ctx, tid, set); err != nil {
		return nil, err
	}
	return &DrawResult{
		Kind:    "draw",
		Deck:    strings.TrimSpace(name),
		Cards:   drawn,
		Left:    len(deck.Remaining),
		Discard: len(deck.Discard),
		Replace: o.Replace,
	}, nil
}

// ReturnCards moves named cards from the discard pile back to the draw pile
// and reshuffles. Every requested face must be present in the discard pile
// (with multiplicity); nothing is changed when one is missing.
func (a *App) ReturnCards(ctx context.Context, name string, cards []string) (*Deck, error) {
	const op = "app.ReturnCards"
	wanted := make([]string, 0, len(cards))
	for _, c := range cards {
		if c = strings.TrimSpace(c); c != "" {
			wanted = append(wanted, c)
		}
	}
	if len(wanted) == 0 {
		return nil, apperr.New(op, apperr.InvalidArgument, "缺少要放回的卡面")
	}
	tid := tableIDFromContext(ctx)
	set, err := a.decks.Decks(ctx, tid)
	if err != nil {
		return nil, err
	}
	deck, ok := set[strings.TrimSpace(name)]
	if !ok {
		return nil, apperr.New(op, apperr.InvalidArgument, "unknown deck: "+name)
	}
	discard := append([]string(nil), deck.Discard...)
	returned := make([]string, 0, len(wanted))
	for _, face := range wanted {
		idx := indexOf(discard, face)
		if idx < 0 {
			return nil, apperr.New(op, apperr.InvalidArgument,
				"弃牌堆中没有卡面："+face+"（可用 deck show 查看弃牌堆）")
		}
		returned = append(returned, discard[idx])
		discard = append(discard[:idx], discard[idx+1:]...)
	}
	deck.Discard = discard
	deck.Remaining = append(deck.Remaining, returned...)
	if err := a.shuffleDeck(ctx, deck.Remaining); err != nil {
		return nil, err
	}
	if err := a.decks.SetDecks(ctx, tid, set); err != nil {
		return nil, err
	}
	return deckView(strings.TrimSpace(name), deck), nil
}

// indexOf returns the first index of v in xs, or -1.
func indexOf(xs []string, v string) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
	return -1
}

// shuffleDeck randomizes a pile in place (Fisher-Yates).
func (a *App) shuffleDeck(ctx context.Context, pile []string) error {
	for i := len(pile) - 1; i > 0; i-- {
		j, err := a.roller.Intn(ctx, i+1)
		if err != nil {
			return err
		}
		pile[i], pile[j] = pile[j], pile[i]
	}
	return nil
}

// parseDeckSpec parses a card specification: cards separated by "|", with
// "::n::card" repeating a card n times.
func parseDeckSpec(spec string) ([]string, error) {
	const op = "app.parseDeckSpec"
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, apperr.New(op, apperr.InvalidArgument, "deck has no cards")
	}
	var cards []string
	for _, part := range strings.Split(spec, "|") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, apperr.New(op, apperr.InvalidArgument, "empty card in deck definition")
		}
		if strings.HasPrefix(part, "::") {
			rest := strings.TrimPrefix(part, "::")
			idx := strings.Index(rest, "::")
			if idx <= 0 {
				return nil, apperr.New(op, apperr.InvalidArgument,
					"invalid multiplicity syntax: "+part)
			}
			n, err := strconv.Atoi(rest[:idx])
			if err != nil || n < 1 {
				return nil, apperr.New(op, apperr.InvalidArgument,
					"invalid multiplicity in: "+part)
			}
			card := strings.TrimSpace(rest[idx+2:])
			if card == "" {
				return nil, apperr.New(op, apperr.InvalidArgument, "empty card in deck definition")
			}
			for i := 0; i < n; i++ {
				cards = append(cards, card)
			}
			continue
		}
		cards = append(cards, part)
	}
	if len(cards) == 0 {
		return nil, apperr.New(op, apperr.InvalidArgument, "deck has no cards")
	}
	if len(cards) > MaxDeckCards {
		return nil, apperr.New(op, apperr.InvalidArgument,
			"deck has too many cards (limit "+strconv.Itoa(MaxDeckCards)+")")
	}
	return cards, nil
}

// parseDeckRange parses an arithmetic range ("下限 上限" or "下限-上限").
func parseDeckRange(spec string) ([]string, error) {
	const op = "app.parseDeckRange"
	fields := strings.Fields(spec)
	var lo, hi int
	switch len(fields) {
	case 1:
		parts := strings.SplitN(fields[0], "-", 2)
		if len(parts) != 2 {
			return nil, apperr.New(op, apperr.InvalidArgument, "invalid range: "+spec)
		}
		a, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		b, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 != nil || err2 != nil {
			return nil, apperr.New(op, apperr.InvalidArgument, "invalid range: "+spec)
		}
		lo, hi = a, b
	case 2:
		x, err1 := strconv.Atoi(fields[0])
		y, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			return nil, apperr.New(op, apperr.InvalidArgument, "invalid range: "+spec)
		}
		lo, hi = x, y
	default:
		return nil, apperr.New(op, apperr.InvalidArgument, "invalid range: "+spec)
	}
	if hi < lo {
		return nil, apperr.New(op, apperr.InvalidArgument, "range upper bound is below its lower bound")
	}
	cards := make([]string, 0, hi-lo+1)
	for v := lo; v <= hi; v++ {
		cards = append(cards, strconv.Itoa(v))
	}
	if len(cards) > MaxDeckCards {
		return nil, apperr.New(op, apperr.InvalidArgument,
			"range has too many cards (limit "+strconv.Itoa(MaxDeckCards)+")")
	}
	return cards, nil
}

// deckView converts a stored deck into its public view.
func deckView(name string, deck *storage.Deck) *Deck {
	return &Deck{
		Name:      name,
		Cards:     append([]string(nil), deck.Cards...),
		Remaining: append([]string(nil), deck.Remaining...),
		Discard:   append([]string(nil), deck.Discard...),
	}
}
