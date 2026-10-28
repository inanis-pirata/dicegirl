package chatcore

// Command table for target resolution: how each command treats a target token.
//
// 🔴 The rule behind the strip list (the T16b-1 boundary): only commands that
// never change card state may take a target in this batch. Everything that
// writes a card gets the capability in the same batch as the T08 permission
// check. The reason is that "@名字" is far easier to trigger by accident than
// "--character" (picking a member from a phone's mention list is one tap), so
// opening a write path before any ownership check turns "an obscure old hole"
// into "the group's everyday operation".
//
// This is not the final state: T01 opens `st` (after the eight-form regression
// exists) and splits `pc` into subcommands; T08 opens `sc` / `rc` / `rch` / `en`
// together with the permission check.

// mentionMode declares how "@名字" tokens are treated.
type mentionMode uint8

const (
	// mentionModeNone means the command does not take a target: a token that
	// resolves to a card is an explicit error (layer 3). It must never be
	// swallowed into free text, because ".st hp-5 @别人" silently falling back
	// to the operator's own card is the failure this whole mechanism exists to
	// prevent.
	mentionModeNone mentionMode = iota
	// mentionModeStrip removes the target token from anywhere in the arguments
	// and passes the rest to the command unchanged (layer 4).
	mentionModeStrip
	// mentionModeInline keeps the token in place and only marks it as a target
	// (the phase-2 opposed check). Reserved; a target it cannot place is
	// reported, never guessed.
	mentionModeInline
)

// barePlacement records where a bare (un-@-ed) card name may appear as a
// target. It is data only: nothing reads it yet, so bare names are currently
// never treated as targets and filling the field in does not switch anything
// on. Enabling bare-name fallback means adding the reader — do not assume this
// field already has an effect.
//
// (`ri 李四` / `init del 王五` do accept a bare name, but they get it from
// App.ParseInitiativeArgs' own name probe, not from this field.)
type barePlacement uint8

const (
	bareNone barePlacement = iota
	bareLeading
	bareTrailing
	barePositional
)

// cmdMeta is one command's target policy.
type cmdMeta struct {
	// name is the command as dispatched, used in error messages.
	name string
	// mentionMode decides whether "@名字" is scanned, stripped or rejected.
	mentionMode mentionMode
	// barePlacement is where a bare name may stand as a target. No reader
	// exists yet (see the type), so it cannot change behaviour today.
	barePlacement barePlacement
	// noTargetHint tells the player which command to use instead; it is
	// appended to the "does not take a target" error.
	//
	// 🔴 It is only for DELEGATION ("roll for someone else with .ra"). Every
	// ".命令名" it mentions must therefore have mentionMode = strip: pointing at
	// a command that would reject the same input sends the player through two
	// dead ends. Query hints ("可用 pc list 查看本团角色") belong to the layer-2
	// not-found message, not here — they answer a different question ("how do I
	// look a name up") and may name a command that takes no target at all.
	noTargetHint string
	// multi allows more than one target (the phase-2 opposed check).
	multi bool
}

// writePathHint is the hint for commands that change card state: they wait for
// the ownership check, not for a syntax decision.
const writePathHint = "代骰目标请等待 T08 权限上线"

// commandMetas is the complete phase-1 policy, keyed by command name. Every
// name in commandNames must have a row: a missing row silently means "no target
// support, no scanning", which is the state T16b removes — that is why the
// spec-lock test walks commandNames instead of this table.
//
// Hints may only point at commands that exist: ".rx" is phase 2 (P1-13), so no
// hint mentions it.
var commandMetas = map[string]cmdMeta{
	"help": {name: "help", mentionMode: mentionModeNone, barePlacement: bareNone},
	"帮助":   {name: "帮助", mentionMode: mentionModeNone, barePlacement: bareNone},

	// --- strip: read-only checks -------------------------------------------
	//
	// These families all end in cmdCheck, which only reads the sheet.
	//
	// rah/rch are NOT here, although they are hidden rolls of the same family:
	// in a group the adapter rewrites them to "ra"/"rc" before dispatch
	// (cmd/qqbot runHiddenInGroup returns "ra "+body), so the key is never
	// consulted there — while on the CLI they are rejected before any check
	// runs ("暗骰检定需要聊天平台适配器"). Giving them a strip entry buys
	// nothing on QQ and loses the accurate CLI error: ".rah 侦查 @小王" would
	// resolve the name first and report "no such character" (exit 1) instead of
	// "this command needs an adapter" (exit 2).
	"ra": {name: "ra", mentionMode: mentionModeStrip, barePlacement: bareNone},
	"rc": {name: "rc", mentionMode: mentionModeStrip, barePlacement: bareNone},
	"rb": {name: "rb", mentionMode: mentionModeStrip, barePlacement: bareNone},
	"rp": {name: "rp", mentionMode: mentionModeStrip, barePlacement: bareNone},

	// --- none: rolls that take free text -----------------------------------
	//
	// The trailing "@名字" used to be swallowed as the roll's label, so a target
	// here is rejected with a pointer to the commands that do accept one.
	"r": {name: "r", mentionMode: mentionModeNone, barePlacement: bareNone,
		noTargetHint: "代骰请用 .ra"},
	"rs": {name: "rs", mentionMode: mentionModeNone, barePlacement: bareNone,
		noTargetHint: "代骰请用 .ra"},
	"w": {name: "w", mentionMode: mentionModeNone, barePlacement: bareNone,
		noTargetHint: "代骰请用 .ra"},
	"ww": {name: "ww", mentionMode: mentionModeNone, barePlacement: bareNone,
		noTargetHint: "代骰请用 .ra"},
	"h":  {name: "h", mentionMode: mentionModeNone, barePlacement: bareNone},
	"rh": {name: "rh", mentionMode: mentionModeNone, barePlacement: bareNone},
	"rd": {name: "rd", mentionMode: mentionModeNone, barePlacement: bareNone},

	// --- none: write paths waiting for the permission check ----------------
	"sc": {name: "sc", mentionMode: mentionModeNone, barePlacement: bareNone,
		noTargetHint: writePathHint},
	"st": {name: "st", mentionMode: mentionModeNone, barePlacement: bareNone,
		noTargetHint: writePathHint},
	"pc": {name: "pc", mentionMode: mentionModeNone, barePlacement: bareNone,
		noTargetHint: writePathHint},
	"en": {name: "en", mentionMode: mentionModeNone, barePlacement: bareNone,
		noTargetHint: writePathHint},

	// Opposed checks share one card across both sides, so a single "@X" cannot
	// say "only for side A": it is rejected rather than half-applied.
	"rav": {name: "rav", mentionMode: mentionModeNone, barePlacement: bareNone},

	// Hidden rolls: the group adapter rewrites them to ra/rc before dispatch
	// (cmd/qqbot/handler.go hiddenLine), so on QQ this entry is never reached
	// as "rah" — the rewrite already turned it into "ra". On the CLI there is
	// no adapter, and the order matters: ResolveTarget runs before
	// dispatchCommand (chatcore/dispatch.go:351 vs :361), so a line carrying
	// "@X" is refused here first ("rah 不支持指定目标"), and only the same line
	// without "@X" reaches the adapter check ("需要聊天平台适配器"). Both fail
	// and both exit 2, but they are different messages — do not write either
	// off as unreachable. See the strip block.
	"rah": {name: "rah", mentionMode: mentionModeNone, barePlacement: bareNone,
		noTargetHint: "暗骰不支持指定目标"},
	"rch": {name: "rch", mentionMode: mentionModeNone, barePlacement: bareNone,
		noTargetHint: "暗骰不支持指定目标"},

	// --- none: no card involved --------------------------------------------
	"ak": {name: "ak", mentionMode: mentionModeNone, barePlacement: bareNone,
		noTargetHint: "抽签不支持指定目标"},

	// Death save (5e death_save, T17b): one plain roll, counters live on the
	// card. It touches card state but never takes a target — you roll YOUR
	// character's saves.
	"ds": {name: "ds", mentionMode: mentionModeNone, barePlacement: bareNone},
	"deck": {name: "deck", mentionMode: mentionModeNone, barePlacement: bareNone,
		noTargetHint: "牌堆不支持指定目标"},
	"draw": {name: "draw", mentionMode: mentionModeNone, barePlacement: bareNone,
		noTargetHint: "牌堆不支持指定目标"},
	"drawh": {name: "drawh", mentionMode: mentionModeNone, barePlacement: bareNone,
		noTargetHint: "暗抽不支持指定目标"},
	"log": {name: "log", mentionMode: mentionModeNone, barePlacement: bareNone,
		noTargetHint: "记录不支持指定目标"},
	"tb":   {name: "tb", mentionMode: mentionModeNone, barePlacement: bareNone},
	"rule": {name: "rule", mentionMode: mentionModeNone, barePlacement: bareNone},
	"game": {name: "game", mentionMode: mentionModeNone, barePlacement: bareNone},
	"set":  {name: "set", mentionMode: mentionModeNone, barePlacement: bareNone},
	"ti":   {name: "ti", mentionMode: mentionModeNone, barePlacement: bareNone},
	"li":   {name: "li", mentionMode: mentionModeNone, barePlacement: bareNone},
	"name": {name: "name", mentionMode: mentionModeNone, barePlacement: bareNone},
	"ri":   {name: "ri", mentionMode: mentionModeNone, barePlacement: bareNone},
	"init": {name: "init", mentionMode: mentionModeNone, barePlacement: bareNone},
}

// commandMeta returns the command's target policy. Unknown names get the zero
// policy (no target support), which keeps an unrecognized command from ever
// taking a target it does not understand.
func commandMeta(name string) cmdMeta {
	if m, ok := commandMetas[name]; ok {
		return m
	}
	return cmdMeta{name: name}
}
