package app

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"dicegirl/internal/apperr"
	"dicegirl/internal/storage"
)

// Card decks (the Dice! `.deck` / `.draw` commands): named decks of cards
// that are drawn without replacement. The group-only features (member decks,
// secret draws) are out of scope for the local CLI.

// MaxDeckCards bounds one deck's definition.
const MaxDeckCards = 500

// Deck is one card deck with its definition and the remaining queue.
type Deck struct {
	Name      string   `json:"name"`
	Cards     []string `json:"cards"`
	Remaining []string `json:"remaining"`
}

// DeckList is the collection of decks.
type DeckList struct {
	Kind  string `json:"kind"`
	Decks []Deck `json:"decks"`
}

// DrawResult is the outcome of a draw.
type DrawResult struct {
	Kind  string   `json:"kind"`
	Deck  string   `json:"deck"`
	Cards []string `json:"cards"`
	Left  int      `json:"left"`
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
	set, err := a.decks.Decks(ctx)
	if err != nil {
		return nil, err
	}
	if _, exists := set[name]; exists {
		return nil, apperr.New(op, apperr.Conflict, "deck already exists: "+name)
	}
	deck := &storage.Deck{Cards: cards, Remaining: cards}
	if err := a.shuffleDeck(ctx, deck); err != nil {
		return nil, err
	}
	set[name] = deck
	if err := a.decks.SetDecks(ctx, set); err != nil {
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
	set, err := a.decks.Decks(ctx)
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
	deck := &storage.Deck{Cards: cards, Remaining: cards}
	if err := a.shuffleDeck(ctx, deck); err != nil {
		return nil, err
	}
	set[name] = deck
	if err := a.decks.SetDecks(ctx, set); err != nil {
		return nil, err
	}
	return deckView(name, deck), nil
}

// ShowDecks lists every deck with its remaining size.
func (a *App) ShowDecks(ctx context.Context) (*DeckList, error) {
	set, err := a.decks.Decks(ctx)
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
	set, err := a.decks.Decks(ctx)
	if err != nil {
		return nil, err
	}
	deck, ok := set[strings.TrimSpace(name)]
	if !ok {
		return nil, apperr.New("app.ShowDeck", apperr.InvalidArgument, "unknown deck: "+name)
	}
	return deckView(strings.TrimSpace(name), deck), nil
}

// ResetDeck restores a deck's remaining queue to its definition.
func (a *App) ResetDeck(ctx context.Context, name string) (*Deck, error) {
	const op = "app.ResetDeck"
	set, err := a.decks.Decks(ctx)
	if err != nil {
		return nil, err
	}
	deck, ok := set[strings.TrimSpace(name)]
	if !ok {
		return nil, apperr.New(op, apperr.InvalidArgument, "unknown deck: "+name)
	}
	deck.Remaining = append([]string(nil), deck.Cards...)
	if err := a.shuffleDeck(ctx, deck); err != nil {
		return nil, err
	}
	if err := a.decks.SetDecks(ctx, set); err != nil {
		return nil, err
	}
	return deckView(strings.TrimSpace(name), deck), nil
}

// ClearDecks removes every deck.
func (a *App) ClearDecks(ctx context.Context) error {
	set, err := a.decks.Decks(ctx)
	if err != nil {
		return err
	}
	set = map[string]*storage.Deck{}
	return a.decks.SetDecks(ctx, set)
}

// DrawCards draws count cards from a deck without replacement.
func (a *App) DrawCards(ctx context.Context, name string, count int) (*DrawResult, error) {
	const op = "app.DrawCards"
	if count < 1 {
		return nil, apperr.New(op, apperr.InvalidArgument, "draw count must be at least 1")
	}
	set, err := a.decks.Decks(ctx)
	if err != nil {
		return nil, err
	}
	deck, ok := set[strings.TrimSpace(name)]
	if !ok {
		return nil, apperr.New(op, apperr.InvalidArgument, "unknown deck: "+name)
	}
	if count > len(deck.Remaining) {
		return nil, apperr.New(op, apperr.InvalidArgument,
			"deck "+name+" has only "+strconv.Itoa(len(deck.Remaining))+" cards left")
	}
	drawn := append([]string(nil), deck.Remaining[:count]...)
	deck.Remaining = deck.Remaining[count:]
	if err := a.decks.SetDecks(ctx, set); err != nil {
		return nil, err
	}
	return &DrawResult{
		Kind:  "draw",
		Deck:  strings.TrimSpace(name),
		Cards: drawn,
		Left:  len(deck.Remaining),
	}, nil
}

// shuffleDeck randomizes the remaining queue of a deck.
func (a *App) shuffleDeck(ctx context.Context, deck *storage.Deck) error {
	for i := len(deck.Remaining) - 1; i > 0; i-- {
		j, err := a.roller.Intn(ctx, i+1)
		if err != nil {
			return err
		}
		deck.Remaining[i], deck.Remaining[j] = deck.Remaining[j], deck.Remaining[i]
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
		a, err1 := strconv.Atoi(fields[0])
		b, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			return nil, apperr.New(op, apperr.InvalidArgument, "invalid range: "+spec)
		}
		lo, hi = a, b
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
	}
}
