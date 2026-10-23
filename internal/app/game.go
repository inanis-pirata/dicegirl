package app

// Game affairs (Dice!Next `.game`): one roster of GMs and players, arbitrary
// GM-owned variables, an open/closed flag and an optional roulette die. The
// data hangs off the current game table, so parallel games in one group stay
// isolated.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"dicegirl/internal/apperr"
	"dicegirl/internal/storage"
)

// GameMember is one GM or player of a game.
type GameMember = storage.GameMember

// GameRoulette is the anti-reroll bag of one die size.
type GameRoulette = storage.GameRoulette

// MaxRouletteTotal bounds sides × copies (the Dice!Next rule).
const MaxRouletteTotal = 100

// GameMeta returns the current game's metadata. A fresh game has empty
// rosters and variables and is open.
func (a *App) GameMeta(ctx context.Context) (*storage.GameMeta, error) {
	t, err := a.CurrentTable(ctx)
	if err != nil {
		return nil, err
	}
	return a.games.GameMeta(ctx, t.ID)
}

// GameNew creates a game and appoints the creator its first GM. An empty
// name gets an automatic one. The new game becomes current.
func (a *App) GameNew(ctx context.Context, name, systemID, actorID, actorName string) (*TableView, error) {
	const op = "app.GameNew"
	if strings.TrimSpace(actorID) == "" {
		return nil, apperr.New(op, apperr.InvalidArgument, "缺少操作者身份")
	}
	if strings.TrimSpace(name) == "" {
		name = a.newGameName(ctx)
	}
	view, err := a.CreateTable(ctx, name, systemID)
	if err != nil {
		return nil, err
	}
	meta := &storage.GameMeta{GMs: []GameMember{newGameMember(actorID, actorName)}}
	if err := a.games.SetGameMeta(ctx, view.ID, meta); err != nil {
		return nil, err
	}
	return a.GameView(ctx, view.ID)
}

// GameJoin puts the actor on the current game's player roster.
func (a *App) GameJoin(ctx context.Context, actorID, actorName string) (*TableView, error) {
	const op = "app.GameJoin"
	t, meta, err := a.currentGameMeta(ctx)
	if err != nil {
		return nil, err
	}
	if meta.Closed {
		return nil, apperr.New(op, apperr.InvalidArgument, "游戏领域已关闭，先让 GM 用 game open 展开")
	}
	// A GM may also sit in the player roster (the GM is a participant too);
	// only a duplicate player entry is rejected.
	if gameMemberIndex(meta.Players, actorID) >= 0 {
		return nil, apperr.New(op, apperr.Conflict, "你已经在本团玩家名单中")
	}
	meta.Players = append(meta.Players, newGameMember(actorID, actorName))
	if err := a.games.SetGameMeta(ctx, t.ID, meta); err != nil {
		return nil, err
	}
	return a.tableView(ctx, t)
}

// GameExit removes the actor from the current game's player and GM rosters.
func (a *App) GameExit(ctx context.Context, actorID string) (*TableView, error) {
	const op = "app.GameExit"
	t, meta, err := a.currentGameMeta(ctx)
	if err != nil {
		return nil, err
	}
	players := removeGameMember(meta.Players, actorID)
	gms := removeGameMember(meta.GMs, actorID)
	if len(players) == len(meta.Players) && len(gms) == len(meta.GMs) {
		return nil, apperr.New(op, apperr.NotFound, "你不在本团名单中")
	}
	meta.Players, meta.GMs = players, gms
	if err := a.games.SetGameMeta(ctx, t.ID, meta); err != nil {
		return nil, err
	}
	return a.tableView(ctx, t)
}

// GameMaster appoints the actor GM when they are not one already. The second
// return value reports whether a new GM was added; when false the caller
// should show the existing GM roster.
func (a *App) GameMaster(ctx context.Context, actorID, actorName string) (*TableView, bool, error) {
	t, meta, err := a.currentGameMeta(ctx)
	if err != nil {
		return nil, false, err
	}
	if gameMemberIndex(meta.GMs, actorID) >= 0 {
		view, err := a.tableView(ctx, t)
		return view, false, err
	}
	meta.GMs = append(meta.GMs, newGameMember(actorID, actorName))
	meta.Players = removeGameMember(meta.Players, actorID)
	if err := a.games.SetGameMeta(ctx, t.ID, meta); err != nil {
		return nil, false, err
	}
	view, err := a.tableView(ctx, t)
	return view, true, err
}

// GameVar returns one game variable. The second result reports whether it is
// set.
func (a *App) GameVar(ctx context.Context, key string) (string, bool, error) {
	meta, err := a.GameMeta(ctx)
	if err != nil {
		return "", false, err
	}
	value, ok := meta.Vars[key]
	return value, ok, nil
}

// GameSetVar writes one GM-owned game variable.
func (a *App) GameSetVar(ctx context.Context, actorID, key, value string) (*TableView, error) {
	const op = "app.GameSetVar"
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, apperr.New(op, apperr.InvalidArgument, "团变量名不能为空")
	}
	t, meta, err := a.currentGameMeta(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireGM(meta, actorID); err != nil {
		return nil, err
	}
	if meta.Vars == nil {
		meta.Vars = map[string]string{}
	}
	meta.Vars[key] = value
	if err := a.games.SetGameMeta(ctx, t.ID, meta); err != nil {
		return nil, err
	}
	return a.tableView(ctx, t)
}

// GameCall returns the current player roster for a GM's roll call.
func (a *App) GameCall(ctx context.Context, actorID string) ([]GameMember, error) {
	meta, err := a.GameMeta(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireGM(meta, actorID); err != nil {
		return nil, err
	}
	return append([]GameMember(nil), meta.Players...), nil
}

// GameKick removes one player from the current game. The target may be a
// member ID or a display name.
func (a *App) GameKick(ctx context.Context, actorID, target string) (*TableView, GameMember, error) {
	const op = "app.GameKick"
	t, meta, err := a.currentGameMeta(ctx)
	if err != nil {
		return nil, GameMember{}, err
	}
	if err := requireGM(meta, actorID); err != nil {
		return nil, GameMember{}, err
	}
	idx := findGameMember(meta.Players, target)
	if idx < 0 {
		return nil, GameMember{}, apperr.New(op, apperr.NotFound, "玩家名单中没有 "+target)
	}
	kicked := meta.Players[idx]
	meta.Players = append(meta.Players[:idx], meta.Players[idx+1:]...)
	if err := a.games.SetGameMeta(ctx, t.ID, meta); err != nil {
		return nil, GameMember{}, err
	}
	view, err := a.tableView(ctx, t)
	return view, kicked, err
}

// GameSetOpen opens or temporarily closes the current game's domain.
func (a *App) GameSetOpen(ctx context.Context, actorID string, open bool) (*TableView, error) {
	const op = "app.GameSetOpen"
	t, meta, err := a.currentGameMeta(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireGM(meta, actorID); err != nil {
		return nil, err
	}
	if meta.Closed == !open {
		state := "展开"
		if !open {
			state = "关闭"
		}
		return nil, apperr.New(op, apperr.Conflict, "游戏领域已经是"+state+"状态")
	}
	meta.Closed = !open
	if err := a.games.SetGameMeta(ctx, t.ID, meta); err != nil {
		return nil, err
	}
	return a.tableView(ctx, t)
}

// GameOver ends the current game: rosters, variables, roulette, initiative
// and decks are all cleared. The table itself stays so the group can open a
// new game or reuse the name.
func (a *App) GameOver(ctx context.Context, actorID string) (*TableView, error) {
	t, meta, err := a.currentGameMeta(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireGM(meta, actorID); err != nil {
		return nil, err
	}
	if err := a.games.DeleteGameMeta(ctx, t.ID); err != nil {
		return nil, err
	}
	if err := a.encounter.SetInitiative(ctx, t.ID, nil); err != nil {
		return nil, err
	}
	if err := a.decks.SetDecks(ctx, t.ID, map[string]*storage.Deck{}); err != nil {
		return nil, err
	}
	// The active-card pointer is per game too; clearing it prevents a new
	// game that reuses this table from inheriting the old table's card.
	if err := a.state.SetActive(storage.WithTable(ctx, t.ID), ""); err != nil {
		return nil, err
	}
	return a.tableView(ctx, t)
}

// GameRouletteHistory returns the values drawn from the current roulette
// this round.
func (a *App) GameRouletteHistory(ctx context.Context) ([]int, *GameRoulette, error) {
	meta, err := a.GameMeta(ctx)
	if err != nil {
		return nil, nil, err
	}
	if meta.Roulette == nil {
		return nil, nil, nil
	}
	return append([]int(nil), meta.Roulette.Drawn...), meta.Roulette, nil
}

// GameRouletteEnable installs the roulette for one die size. Copies repeats
// each face in the bag; sides × copies is capped at MaxRouletteTotal.
func (a *App) GameRouletteEnable(ctx context.Context, actorID string, sides, copies int) (*TableView, error) {
	const op = "app.GameRouletteEnable"
	if sides < 1 || sides > MaxDefaultDieSides {
		return nil, apperr.New(op, apperr.InvalidArgument,
			fmt.Sprintf("轮盘骰面数必须在 1–%d 之间", MaxDefaultDieSides))
	}
	if copies < 1 {
		return nil, apperr.New(op, apperr.InvalidArgument, "轮盘份数必须 ≥1")
	}
	if sides*copies > MaxRouletteTotal {
		return nil, apperr.New(op, apperr.InvalidArgument,
			fmt.Sprintf("轮盘面数×份数不能超过 %d", MaxRouletteTotal))
	}
	t, meta, err := a.currentGameMeta(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireGM(meta, actorID); err != nil {
		return nil, err
	}
	meta.Roulette = newRoulette(sides, copies)
	if err := a.games.SetGameMeta(ctx, t.ID, meta); err != nil {
		return nil, err
	}
	return a.tableView(ctx, t)
}

// GameRouletteReset refills the current roulette and clears its history.
func (a *App) GameRouletteReset(ctx context.Context, actorID string) (*TableView, error) {
	const op = "app.GameRouletteReset"
	t, meta, err := a.currentGameMeta(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireGM(meta, actorID); err != nil {
		return nil, err
	}
	if meta.Roulette == nil {
		return nil, apperr.New(op, apperr.NotFound, "尚未启用轮盘骰")
	}
	meta.Roulette = newRoulette(meta.Roulette.Sides, meta.Roulette.Copies)
	if err := a.games.SetGameMeta(ctx, t.ID, meta); err != nil {
		return nil, err
	}
	return a.tableView(ctx, t)
}

// GameRouletteClear removes the current roulette, restoring ordinary rolls.
func (a *App) GameRouletteClear(ctx context.Context, actorID string) (*TableView, error) {
	const op = "app.GameRouletteClear"
	t, meta, err := a.currentGameMeta(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireGM(meta, actorID); err != nil {
		return nil, err
	}
	if meta.Roulette == nil {
		return nil, apperr.New(op, apperr.NotFound, "尚未启用轮盘骰")
	}
	meta.Roulette = nil
	if err := a.games.SetGameMeta(ctx, t.ID, meta); err != nil {
		return nil, err
	}
	return a.tableView(ctx, t)
}

// drawRoulette takes one value for a bare dN roll. It reports false when the
// current game has no matching, active roulette; the caller then rolls
// normally.
func (a *App) drawRoulette(ctx context.Context, sides int) (int, bool, error) {
	tid := tableIDFromContext(ctx)
	meta, err := a.games.GameMeta(ctx, tid)
	if err != nil {
		return 0, false, err
	}
	if meta.Closed || meta.Roulette == nil || meta.Roulette.Sides != sides {
		return 0, false, nil
	}
	r := meta.Roulette
	if r.Copies < 1 {
		r.Copies = 1
	}
	if len(r.Remaining) == 0 {
		// A new round: refill the bag and start the history over.
		r.Remaining = rouletteBag(r.Sides, r.Copies)
		r.Drawn = nil
	}
	idx, err := a.roller.Intn(ctx, len(r.Remaining))
	if err != nil {
		return 0, false, err
	}
	value := r.Remaining[idx]
	r.Remaining = append(r.Remaining[:idx], r.Remaining[idx+1:]...)
	r.Drawn = append(r.Drawn, value)
	if err := a.games.SetGameMeta(ctx, tid, meta); err != nil {
		return 0, false, err
	}
	return value, true, nil
}

// currentGameMeta loads the current table and its metadata together.
func (a *App) currentGameMeta(ctx context.Context) (*storage.GameTable, *storage.GameMeta, error) {
	t, err := a.CurrentTable(ctx)
	if err != nil {
		return nil, nil, err
	}
	meta, err := a.games.GameMeta(ctx, t.ID)
	if err != nil {
		return nil, nil, err
	}
	return t, meta, nil
}

// newGameName generates the automatic name for `.game new` without a title.
func (a *App) newGameName(ctx context.Context) string {
	base := "新团 " + time.Now().Format("2006-01-02 15:04")
	rows, err := a.tables.Tables(ctx)
	if err != nil {
		return base
	}
	names := map[string]bool{}
	for _, t := range rows {
		names[strings.ToLower(t.Name)] = true
	}
	name := base
	for i := 2; names[strings.ToLower(name)]; i++ {
		name = fmt.Sprintf("%s (%d)", base, i)
	}
	return name
}

// requireGM enforces the GM-only mutations.
func requireGM(meta *storage.GameMeta, actorID string) error {
	if gameMemberIndex(meta.GMs, actorID) < 0 {
		return apperr.New("app.game", apperr.InvalidArgument, "该操作需要 GM 权限")
	}
	return nil
}

// newGameMember builds one roster entry, defaulting its label to its id.
func newGameMember(id, name string) GameMember {
	name = strings.TrimSpace(name)
	if name == "" {
		name = id
	}
	return GameMember{ID: id, Name: name, Since: time.Now().UTC()}
}

// gameMemberIndex finds a member by exact id.
func gameMemberIndex(list []GameMember, id string) int {
	for i, m := range list {
		if m.ID == id {
			return i
		}
	}
	return -1
}

// findGameMember finds a roster entry by id, exact name, then
// case-insensitive name.
func findGameMember(list []GameMember, ref string) int {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return -1
	}
	for i, m := range list {
		if m.ID == ref {
			return i
		}
	}
	for i, m := range list {
		if m.Name == ref {
			return i
		}
	}
	for i, m := range list {
		if strings.EqualFold(m.Name, ref) {
			return i
		}
	}
	return -1
}

// removeGameMember returns list without the entry with that id.
func removeGameMember(list []GameMember, id string) []GameMember {
	idx := gameMemberIndex(list, id)
	if idx < 0 {
		return list
	}
	return append(list[:idx], list[idx+1:]...)
}

// newRoulette builds a full bag and an empty history.
func newRoulette(sides, copies int) *GameRoulette {
	return &GameRoulette{Sides: sides, Copies: copies, Remaining: rouletteBag(sides, copies)}
}

// rouletteBag returns 1..sides repeated copies times.
func rouletteBag(sides, copies int) []int {
	bag := make([]int, 0, sides*copies)
	for copy := 0; copy < copies; copy++ {
		for value := 1; value <= sides; value++ {
			bag = append(bag, value)
		}
	}
	return bag
}

// rouletteSpec parses the `sides[*copies]` argument of `.game rou`.
func ParseRouletteSpec(text string) (sides, copies int, err error) {
	text = strings.ReplaceAll(strings.TrimSpace(text), "＊", "*")
	copies = 1
	if i := strings.IndexByte(text, '*'); i >= 0 {
		parts := strings.Split(text, "*")
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return 0, 0, fmt.Errorf("轮盘参数必须形如 面数[*份数]，例如 20 或 6*3")
		}
		sides, err = strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			return 0, 0, fmt.Errorf("轮盘面数必须是整数，得到 %q", parts[0])
		}
		copies, err = strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return 0, 0, fmt.Errorf("轮盘份数必须是整数，得到 %q", parts[1])
		}
		return sides, copies, nil
	}
	sides, err = strconv.Atoi(text)
	if err != nil {
		return 0, 0, fmt.Errorf("轮盘面数必须是整数，得到 %q", text)
	}
	return sides, copies, nil
}
