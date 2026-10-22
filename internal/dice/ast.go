package dice

// Limits bound resource consumption of parsed expressions. They are
// deliberately conservative; a Roller can be constructed with custom Limits.
type Limits struct {
	// MaxDicePerGroup bounds the count N in a single NdS group.
	MaxDicePerGroup int
	// MaxTotalDice bounds the number of dice across the whole expression.
	MaxTotalDice int
	// MaxSides bounds the die size S.
	MaxSides int
	// MaxDepth bounds parenthesis/operator nesting.
	MaxDepth int
	// MaxRepeat bounds the n in the n# repeat prefix of a roll statement.
	MaxRepeat int
	// MaxExplodeDice bounds the extra dice one group may spawn through
	// the "!" modifier (safety limit against runaway chains).
	MaxExplodeDice int
	// MaxRerolls bounds how often one die may be rolled again through
	// the "r" modifier.
	MaxRerolls int
}

// DefaultLimits are the built-in safety limits (documented in README).
func DefaultLimits() Limits {
	return Limits{
		MaxDicePerGroup: 100,
		MaxTotalDice:    1000,
		MaxSides:        1000,
		MaxDepth:        40,
		MaxRepeat:       10,
		MaxExplodeDice:  100,
		MaxRerolls:      10,
	}
}

// Op is a binary/unary arithmetic operator.
type Op int

const (
	OpAdd Op = iota + 1
	OpSub
	OpMul
	OpDiv
)

// String renders an operator for result explanations.
func (o Op) String() string {
	switch o {
	case OpAdd:
		return "+"
	case OpSub:
		return "-"
	case OpMul:
		return "*"
	case OpDiv:
		return "/"
	default:
		return "?"
	}
}

// KeepKind selects a keep/drop modifier.
type KeepKind int

const (
	KeepHighest KeepKind = iota + 1
	KeepLowest
	DropHighest
	DropLowest
)

// KeepSpec is a parsed kh/kl/dh/dl modifier.
type KeepSpec struct {
	Kind   KeepKind `json:"kind"`
	Amount int      `json:"amount"`
}

// ExplodeSpec is a parsed "!" modifier: every die that reaches the
// threshold spawns another die of the same size (the chain continues until
// no die explodes or the safety limit is hit).
type ExplodeSpec struct {
	// Threshold is the value a die must reach to explode. Zero means the
	// die's maximum face.
	Threshold int `json:"threshold"`
	// Max bounds the extra dice one group may spawn.
	Max int `json:"max"`
}

// RerollSpec is a parsed "r" modifier: dice below the threshold are rolled
// again (up to the safety limit).
type RerollSpec struct {
	// Threshold is the value a die must reach to keep its roll.
	Threshold int `json:"threshold"`
	// Max bounds the rerolls of one die.
	Max int `json:"max"`
}

// CountSpec is a parsed "c" modifier: the group's value is the number of
// kept dice matching the comparison, not their sum (dice-pool semantics).
type CountSpec struct {
	Op     CmpOp `json:"op"`
	Target int   `json:"target"`
}

// Node is any evaluated expression node. Evaluated numeric values live on
// the concrete nodes so a full expression with several dice groups and
// operators never collapses into one total + modifier.
type Node interface {
	node()
}

// NumberNode is an integer literal.
type NumberNode struct {
	Value int `json:"value"`
}

// DieRoll is a single die result. Kept is false when a keep/drop modifier
// discarded it.
type DieRoll struct {
	Value int  `json:"value"`
	Kept  bool `json:"kept"`
	// Exploded marks a die that spawned another die ("!" modifier).
	Exploded bool `json:"exploded,omitempty"`
	// Rerolls counts how often the die was rolled again ("r" modifier).
	Rerolls int `json:"rerolls,omitempty"`
}

// DiceNode is one NdS group with optional keep/drop modifier and its results.
type DiceNode struct {
	Count int       `json:"count"`
	Sides int       `json:"sides"`
	Keep  *KeepSpec `json:"keep,omitempty"`
	// Explode, Reroll and CountSpec are the generic group modifiers.
	Explode   *ExplodeSpec `json:"explode,omitempty"`
	Reroll    *RerollSpec  `json:"reroll,omitempty"`
	CountSpec *CountSpec   `json:"count_spec,omitempty"`
	Rolls     []DieRoll    `json:"rolls"`
	// Value is the subtotal: sum of kept rolls, or the count of the
	// matching dice when a "c" modifier is present.
	Value int `json:"value"`
}

// BinaryNode is an arithmetic combination of two evaluated nodes.
type BinaryNode struct {
	Op    Op   `json:"op"`
	Left  Node `json:"left"`
	Right Node `json:"right"`
	Value int  `json:"value"`
}

// UnaryNode is a sign applied to one node.
type UnaryNode struct {
	Op    Op   `json:"op"`
	Child Node `json:"child"`
	Value int  `json:"value"`
}

// MarshalText renders operators inside JSON trees.
func (o Op) MarshalText() ([]byte, error) { return []byte(o.String()), nil }

// MarshalText renders keep kinds inside JSON trees.
func (k KeepKind) MarshalText() ([]byte, error) {
	switch k {
	case KeepHighest:
		return []byte("keep_highest"), nil
	case KeepLowest:
		return []byte("keep_lowest"), nil
	case DropHighest:
		return []byte("drop_highest"), nil
	case DropLowest:
		return []byte("drop_lowest"), nil
	default:
		return []byte("unknown"), nil
	}
}

func (*NumberNode) node() {}
func (*DiceNode) node()   {}
func (*BinaryNode) node() {}
func (*UnaryNode) node()  {}

// CmpOp is the whole-expression comparison suffix operator (BCDice style),
// e.g. the ">= " in "2d6+1>=7". It is unrelated to arithmetic operators.
type CmpOp int

const (
	CmpLT CmpOp = iota + 1 // <
	CmpLE                  // <=
	CmpGT                  // >
	CmpGE                  // >=
)

// String renders a comparison operator.
func (c CmpOp) String() string {
	switch c {
	case CmpLT:
		return "<"
	case CmpLE:
		return "<="
	case CmpGT:
		return ">"
	case CmpGE:
		return ">="
	default:
		return "?"
	}
}

// MarshalText renders comparison operators inside JSON results.
func (c CmpOp) MarshalText() ([]byte, error) { return []byte(c.String()), nil }

// CompareSpec is a parsed comparison suffix: judge the roll total against
// Target using Op.
type CompareSpec struct {
	Op     CmpOp
	Target int
}

// Judge evaluates the comparison against a total.
func (c CompareSpec) Judge(total int) *Comparison {
	return &Comparison{Op: c.Op, Target: c.Target, Success: matches(total, c.Op, c.Target)}
}

// matches evaluates a comparison of one value against a target.
func matches(value int, op CmpOp, target int) bool {
	switch op {
	case CmpLT:
		return value < target
	case CmpLE:
		return value <= target
	case CmpGT:
		return value > target
	case CmpGE:
		return value >= target
	}
	return false
}

// Comparison is a judged comparison suffix on a rolled total.
type Comparison struct {
	Op      CmpOp `json:"op"`
	Target  int   `json:"target"`
	Success bool  `json:"success"`
}

// RollResult is the structured result of rolling an expression.
type RollResult struct {
	// Expression is the normalized input expression (as typed).
	Expression string
	// Root is the fully evaluated AST.
	Root Node
	// Total is the final integer value.
	Total int
	// Groups lists every dice group in evaluation order.
	Groups []*DiceNode
	// Compare is the judged whole-expression comparison suffix, if any.
	Compare *Comparison `json:"comparison,omitempty"`
}
