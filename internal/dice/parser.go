package dice

import (
	"context"
	"strconv"
	"strings"

	"dicegirl/internal/apperr"
)

// Parser/evaluator contract (also documented in README "妤犳澘鐡欑悰銊ㄦ彧瀵繗顕㈠▔?):
//
//	expr   = term { ('+' | '-') term }
//	term   = unary { ('*' | '/') unary }
//	unary  = ('-' | '+') unary | primary
//	primary= number | dice | '(' expr ')'
//	dice   = [number] ('d'|'D') (number | '%') [ keep ] [ modifier ]...
//	keep   = ('kh'|'kl'|'dh'|'dl') [number]
//	modifier = ('!' [number])          # explode: dice reaching the threshold spawn a die
//	         | ('r' number)            # reroll: dice below the threshold are rolled again
//	         | ('c' [cmp] number)      # count: the group value is the number of matching dice
//
// Only one keep/drop modifier per dice group. Division is integer
// truncation toward zero; division by zero is an error.

type parser struct {
	tokens    []token
	pos       int
	limits    Limits
	totalDice int
	groups    []*DiceNode
}

// Parse builds and statically validates the AST without rolling any dice.
func Parse(expr string, limits Limits) (Node, []*DiceNode, error) {
	tokens, err := lex(expr)
	if err != nil {
		return nil, nil, apperr.Wrap("dice.Parse", apperr.ParseError, err.Error(), err)
	}
	if strings.TrimSpace(expr) == "" {
		return nil, nil, apperr.New("dice.Parse", apperr.ParseError, "empty expression")
	}
	p := &parser{tokens: tokens, limits: limits}
	root, perr := p.parseExpr(0)
	if perr != nil {
		return nil, nil, perr
	}
	if p.peek().kind != tEOF {
		return nil, nil, p.tokenError(p.peek(), "unexpected trailing token")
	}
	return root, p.groups, nil
}

func (p *parser) parseExpr(depth int) (Node, error) {
	if depth > p.limits.MaxDepth {
		return nil, apperr.New("dice.Parse", apperr.LimitExceeded, "expression nesting too deep")
	}
	left, err := p.parseTerm(depth)
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tPlus || p.peek().kind == tMinus {
		opTok := p.next()
		op := OpAdd
		if opTok.kind == tMinus {
			op = OpSub
		}
		right, err := p.parseTerm(depth)
		if err != nil {
			return nil, err
		}
		left = &BinaryNode{Op: op, Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseTerm(depth int) (Node, error) {
	left, err := p.parseUnary(depth)
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tStar || p.peek().kind == tSlash {
		opTok := p.next()
		op := OpMul
		if opTok.kind == tSlash {
			op = OpDiv
		}
		right, err := p.parseUnary(depth)
		if err != nil {
			return nil, err
		}
		left = &BinaryNode{Op: op, Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseUnary(depth int) (Node, error) {
	tok := p.peek()
	if tok.kind == tMinus || tok.kind == tPlus {
		p.next()
		child, err := p.parseUnary(depth + 1)
		if err != nil {
			return nil, err
		}
		op := OpAdd
		if tok.kind == tMinus {
			op = OpSub
		}
		return &UnaryNode{Op: op, Child: child}, nil
	}
	return p.parsePrimary(depth)
}

func (p *parser) parsePrimary(depth int) (Node, error) {
	tok := p.peek()
	switch tok.kind {
	case tLParen:
		p.next()
		inner, err := p.parseExpr(depth + 1)
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tRParen {
			return nil, p.tokenError(p.peek(), "missing closing ')'")
		}
		p.next()
		return inner, nil
	case tNumber:
		// NdS dice group, or a plain integer literal.
		if p.peekAt(1).kind == tWord && p.peekAt(1).text == "d" {
			count, err := strconv.Atoi(tok.text)
			if err != nil {
				return nil, apperr.Wrap("dice.Parse", apperr.ParseError,
					"invalid number at position "+itoa(tok.pos), err)
			}
			p.next() // consume number
			p.next() // consume 'd'
			return p.parseDiceBody(count)
		}
		return p.parseLiteral(tok)
	case tWord:
		if tok.text == "d" {
			p.next()
			return p.parseDiceBody(1)
		}
		return nil, p.tokenError(tok, "unknown word")
	case tEOF:
		return nil, p.tokenError(tok, "expected a number or dice expression")
	default:
		return nil, p.tokenError(tok, "unexpected token")
	}
}

func (p *parser) parseLiteral(tok token) (Node, error) {
	p.next()
	v, err := strconv.Atoi(tok.text)
	if err != nil {
		return nil, apperr.Wrap("dice.Parse", apperr.ParseError,
			"invalid number at position "+itoa(tok.pos), err)
	}
	return &NumberNode{Value: v}, nil
}

func (p *parser) parseDiceBody(count int) (Node, error) {
	if count < 1 {
		return nil, apperr.New("dice.Parse", apperr.InvalidArgument,
			"dice count must be at least 1")
	}
	if count > p.limits.MaxDicePerGroup {
		return nil, apperr.New("dice.Parse", apperr.LimitExceeded,
			"dice count "+itoa(count)+" exceeds limit "+itoa(p.limits.MaxDicePerGroup))
	}

	sidesTok := p.peek()
	var sides int
	switch sidesTok.kind {
	case tNumber:
		v, err := strconv.Atoi(sidesTok.text)
		if err != nil {
			return nil, apperr.Wrap("dice.Parse", apperr.ParseError,
				"invalid die size", err)
		}
		sides = v
		p.next()
	case tPercent:
		sides = 100
		p.next()
	default:
		return nil, p.tokenError(sidesTok, "expected die size after 'd'")
	}
	if sides < 1 {
		return nil, apperr.New("dice.Parse", apperr.InvalidArgument,
			"die size must be at least 1")
	}
	if sides > p.limits.MaxSides {
		return nil, apperr.New("dice.Parse", apperr.LimitExceeded,
			"die size d"+itoa(sides)+" exceeds limit d"+itoa(p.limits.MaxSides))
	}

	node := &DiceNode{Count: count, Sides: sides}
	p.groups = append(p.groups, node)
	p.totalDice += count
	if p.totalDice > p.limits.MaxTotalDice {
		return nil, apperr.New("dice.Parse", apperr.LimitExceeded,
			"total dice count exceeds limit "+itoa(p.limits.MaxTotalDice))
	}

	// Keep/drop and the generic group modifiers may appear in any order
	// after the group, but each kind at most once.
	for {
		tok := p.peek()
		switch {
		case tok.kind == tWord && isKeepWord(tok.text):
			if node.Keep != nil {
				return nil, apperr.New("dice.Parse", apperr.InvalidArgument,
					"only one keep/drop modifier per dice group")
			}
			keep, err := p.parseKeep(count)
			if err != nil {
				return nil, err
			}
			node.Keep = keep
			continue
		case tok.kind == tBang:
			p.next()
			if node.Explode != nil {
				return nil, apperr.New("dice.Parse", apperr.InvalidArgument,
					"only one explode modifier per dice group")
			}
			spec := &ExplodeSpec{Max: p.limits.MaxExplodeDice}
			if p.peek().kind == tNumber {
				v, err := strconv.Atoi(p.next().text)
				if err != nil {
					return nil, apperr.Wrap("dice.Parse", apperr.ParseError,
						"invalid explode threshold", err)
				}
				spec.Threshold = v
			}
			if spec.Threshold < 0 {
				return nil, apperr.New("dice.Parse", apperr.InvalidArgument,
					"explode threshold must not be negative")
			}
			node.Explode = spec
			continue
		case tok.kind == tWord && tok.text == "r":
			p.next()
			if node.Reroll != nil {
				return nil, apperr.New("dice.Parse", apperr.InvalidArgument,
					"only one reroll modifier per dice group")
			}
			if p.peek().kind != tNumber {
				return nil, p.tokenError(p.peek(), "reroll modifier needs a threshold")
			}
			v, err := strconv.Atoi(p.next().text)
			if err != nil {
				return nil, apperr.Wrap("dice.Parse", apperr.ParseError,
					"invalid reroll threshold", err)
			}
			node.Reroll = &RerollSpec{Threshold: v, Max: p.limits.MaxRerolls}
			continue
		case tok.kind == tWord && tok.text == "c":
			p.next()
			if node.CountSpec != nil {
				return nil, apperr.New("dice.Parse", apperr.InvalidArgument,
					"only one count modifier per dice group")
			}
			spec := &CountSpec{Op: CmpGE}
			switch p.peek().kind {
			case tGreaterEqual:
				spec.Op = CmpGE
				p.next()
			case tGreater:
				spec.Op = CmpGT
				p.next()
			case tLessEqual:
				spec.Op = CmpLE
				p.next()
			case tLess:
				spec.Op = CmpLT
				p.next()
			}
			if p.peek().kind != tNumber {
				return nil, p.tokenError(p.peek(), "count modifier needs a threshold")
			}
			v, err := strconv.Atoi(p.next().text)
			if err != nil {
				return nil, apperr.Wrap("dice.Parse", apperr.ParseError,
					"invalid count threshold", err)
			}
			spec.Target = v
			node.CountSpec = spec
			continue
		default:
			return node, nil
		}
	}
}

func (p *parser) parseKeep(count int) (*KeepSpec, error) {
	tok := p.next()
	amount := 1
	if p.peek().kind == tNumber {
		v, err := strconv.Atoi(p.next().text)
		if err != nil {
			return nil, apperr.Wrap("dice.Parse", apperr.ParseError, "invalid keep amount", err)
		}
		amount = v
	}
	if amount < 1 {
		return nil, apperr.New("dice.Parse", apperr.InvalidArgument,
			"keep/drop amount must be at least 1")
	}
	if amount > count {
		return nil, apperr.New("dice.Parse", apperr.InvalidArgument,
			"keep/drop amount exceeds dice count")
	}
	kind := mapKeepKind(tok.text)
	if p.peek().kind == tWord && isKeepWord(p.peek().text) {
		return nil, p.tokenError(p.peek(), "only one keep/drop modifier per dice group")
	}
	return &KeepSpec{Kind: kind, Amount: amount}, nil
}

func isKeepWord(w string) bool { return mapKeepKind(w) != 0 }

func mapKeepKind(w string) KeepKind {
	switch w {
	case "kh":
		return KeepHighest
	case "kl":
		return KeepLowest
	case "dh":
		return DropHighest
	case "dl":
		return DropLowest
	default:
		return 0
	}
}

func (p *parser) peek() token { return p.tokens[p.pos] }
func (p *parser) peekAt(n int) token {
	if p.pos+n >= len(p.tokens) {
		return p.tokens[len(p.tokens)-1]
	}
	return p.tokens[p.pos+n]
}
func (p *parser) next() token {
	t := p.tokens[p.pos]
	if p.pos < len(p.tokens)-1 {
		p.pos++
	}
	return t
}

func (p *parser) tokenError(tok token, msg string) error {
	switch tok.kind {
	case tEOF:
		return apperr.New("dice.Parse", apperr.ParseError, msg+" (end of expression)")
	default:
		return apperr.New("dice.Parse", apperr.ParseError,
			msg+" "+quote(tok.text)+" at position "+itoa(tok.pos))
	}
}

// Eval runs a parsed AST against src, filling per-node values and die rolls.
// The context is checked between dice groups so callers can cancel huge
// rolls.
func Eval(ctx context.Context, root Node, src Source) error {
	_, err := eval(ctx, root, src)
	return err
}

func eval(ctx context.Context, n Node, src Source) (int, error) {
	switch nn := n.(type) {
	case *NumberNode:
		return nn.Value, nil
	case *UnaryNode:
		v, err := eval(ctx, nn.Child, src)
		if err != nil {
			return 0, err
		}
		if nn.Op == OpSub {
			v = -v
		}
		nn.Value = v
		return v, nil
	case *BinaryNode:
		lv, err := eval(ctx, nn.Left, src)
		if err != nil {
			return 0, err
		}
		rv, err := eval(ctx, nn.Right, src)
		if err != nil {
			return 0, err
		}
		v, err := applyOp(nn.Op, lv, rv)
		if err != nil {
			return 0, err
		}
		nn.Value = v
		return v, nil
	case *DiceNode:
		if err := ctx.Err(); err != nil {
			return 0, apperr.Wrap("dice.Eval", apperr.RollError, "roll cancelled", err)
		}
		nn.Rolls = make([]DieRoll, 0, nn.Count)
		for i := 0; i < nn.Count; i++ {
			dr, err := nn.rollOne(src)
			if err != nil {
				return 0, apperr.Wrap("dice.Eval", apperr.RollError, "random source failed", err)
			}
			nn.Rolls = append(nn.Rolls, dr)
		}
		if err := nn.applyExplode(src); err != nil {
			return 0, apperr.Wrap("dice.Eval", apperr.RollError, "random source failed", err)
		}
		applyKeep(nn)
		nn.Value = nn.total()
		return nn.Value, nil
	default:
		return 0, apperr.New("dice.Eval", apperr.Internal, "unknown AST node")
	}
}

// rollOne rolls a single die, applying the reroll modifier if present.
func (nn *DiceNode) rollOne(src Source) (DieRoll, error) {
	v, err := src.Intn(nn.Sides)
	if err != nil {
		return DieRoll{}, err
	}
	dr := DieRoll{Value: v + 1, Kept: true}
	if nn.Reroll == nil {
		return dr, nil
	}
	for i := 0; i < nn.Reroll.Max && dr.Value < nn.Reroll.Threshold; i++ {
		v, err = src.Intn(nn.Sides)
		if err != nil {
			return DieRoll{}, err
		}
		dr.Value = v + 1
		dr.Rerolls++
	}
	return dr, nil
}

// applyExplode spawns extra dice for every die that reached the explode
// threshold, until no die explodes any more or the safety limit is hit.
func (nn *DiceNode) applyExplode(src Source) error {
	if nn.Explode == nil {
		return nil
	}
	threshold := nn.Explode.Threshold
	if threshold <= 0 {
		threshold = nn.Sides
	}
	added := 0
	for added < nn.Explode.Max {
		idx := -1
		for i := range nn.Rolls {
			if !nn.Rolls[i].Exploded && nn.Rolls[i].Value >= threshold {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil
		}
		nn.Rolls[idx].Exploded = true
		dr, err := nn.rollOne(src)
		if err != nil {
			return err
		}
		nn.Rolls = append(nn.Rolls, dr)
		added++
	}
	return nil
}

// total is the group's value: the sum of the kept dice, or the number of
// matching dice when a count modifier is present.
func (nn *DiceNode) total() int {
	if nn.CountSpec != nil {
		n := 0
		for _, dr := range nn.Rolls {
			if !dr.Kept {
				continue
			}
			if matches(dr.Value, nn.CountSpec.Op, nn.CountSpec.Target) {
				n++
			}
		}
		return n
	}
	sum := 0
	for _, dr := range nn.Rolls {
		if dr.Kept {
			sum += dr.Value
		}
	}
	return sum
}

// applyKeep marks rolls kept/dropped per the modifier. Ties keep the earlier
// die so output ordering is stable.
func applyKeep(nn *DiceNode) {
	if nn.Keep == nil {
		return
	}
	keepAmount := nn.AmountForKeep()
	order := make([]int, nn.Count)
	for i := range order {
		order[i] = i
	}
	// Small-count insertion sort by (value, index), no allocation of sort
	// interface and deterministic tie handling.
	less := func(a, b int) bool {
		ra, rb := nn.Rolls[a].Value, nn.Rolls[b].Value
		if ra != rb {
			switch nn.Keep.Kind {
			case KeepHighest, DropLowest:
				return ra > rb
			default:
				return ra < rb
			}
		}
		return a < b
	}
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && less(order[j], order[j-1]); j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	for i := range nn.Rolls {
		nn.Rolls[i].Kept = false
	}
	for i := 0; i < keepAmount; i++ {
		nn.Rolls[order[i]].Kept = true
	}
}

// AmountForKeep converts a drop modifier into the equivalent keep amount.
func (nn *DiceNode) AmountForKeep() int {
	if nn.Keep == nil {
		return nn.Count
	}
	switch nn.Keep.Kind {
	case DropHighest, DropLowest:
		return nn.Count - nn.Keep.Amount
	default:
		return nn.Keep.Amount
	}
}
