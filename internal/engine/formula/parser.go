package formula

import (
	"fmt"
	"strconv"

	"dicegirl/internal/apperr"
)

type parser struct {
	tokens []token
	pos    int
}

func (p *parser) peek() token { return p.tokens[p.pos] }

func (p *parser) next() token {
	t := p.tokens[p.pos]
	if p.pos < len(p.tokens)-1 {
		p.pos++
	}
	return t
}

func (p *parser) errorf(t token, format string, args ...any) error {
	prefix := fmt.Sprintf("column %d: ", t.pos+1)
	return apperr.New("formula.parse", apperr.ParseError, prefix+fmt.Sprintf(format, args...))
}

// Grammar (see package doc):
//
//	or    := and { "||" and }
//	and   := not { "&&" not }
//	not   := "!" not | cmp
//	cmp   := add { cmpOp add }
//	add   := mul { ("+"|"-") mul }
//	mul   := unary { ("*"|"/") unary }
//	unary := "-" unary | primary
//	primary := number | ident [ "(" args ")" ] | "(" or ")"
func (p *parser) parseOr() (node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tOr {
		op := p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &binaryNode{op: op.text, left: left, right: right}
	}
	return left, nil
}

func (p *parser) parseAnd() (node, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tAnd {
		op := p.next()
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = &binaryNode{op: op.text, left: left, right: right}
	}
	return left, nil
}

func (p *parser) parseNot() (node, error) {
	if p.peek().kind == tNot {
		op := p.next()
		operand, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &unaryNode{op: op.text, operand: operand}, nil
	}
	return p.parseCmp()
}

func (p *parser) parseCmp() (node, error) {
	left, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	for isCmp(p.peek().kind) {
		op := p.next()
		right, err := p.parseAdd()
		if err != nil {
			return nil, err
		}
		left = &binaryNode{op: op.text, left: left, right: right}
	}
	return left, nil
}

func isCmp(k tokenKind) bool {
	switch k {
	case tLT, tLE, tGT, tGE, tEQ, tNE:
		return true
	}
	return false
}

func (p *parser) parseAdd() (node, error) {
	left, err := p.parseMul()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tPlus || p.peek().kind == tMinus {
		op := p.next()
		right, err := p.parseMul()
		if err != nil {
			return nil, err
		}
		left = &binaryNode{op: op.text, left: left, right: right}
	}
	return left, nil
}

func (p *parser) parseMul() (node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tStar || p.peek().kind == tSlash {
		op := p.next()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &binaryNode{op: op.text, left: left, right: right}
	}
	return left, nil
}

func (p *parser) parseUnary() (node, error) {
	if p.peek().kind == tMinus {
		op := p.next()
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &unaryNode{op: op.text, operand: operand}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (node, error) {
	t := p.peek()
	switch t.kind {
	case tNumber:
		p.next()
		v, err := strconv.Atoi(t.text)
		if err != nil {
			return nil, p.errorf(t, "invalid number %q", t.text)
		}
		return &numberNode{value: v}, nil
	case tIdent:
		p.next()
		if p.peek().kind != tLParen {
			return &varNode{name: t.text}, nil
		}
		p.next() // consume "("
		call := &callNode{name: t.text}
		if p.peek().kind == tRParen {
			p.next()
			return call, nil
		}
		for {
			arg, err := p.parseOr()
			if err != nil {
				return nil, err
			}
			call.args = append(call.args, arg)
			switch p.peek().kind {
			case tComma:
				p.next()
			case tRParen:
				p.next()
				return call, nil
			default:
				return nil, p.errorf(p.peek(), "expected \",\" or \")\" in function call")
			}
		}
	case tLParen:
		p.next()
		inner, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tRParen {
			return nil, p.errorf(p.peek(), "missing closing \")\"")
		}
		p.next()
		return inner, nil
	default:
		return nil, p.errorf(t, "expected a number, variable or \"(\"")
	}
}
