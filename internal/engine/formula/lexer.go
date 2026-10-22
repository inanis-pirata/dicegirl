package formula

import (
	"fmt"

	"dicegirl/internal/apperr"
)

type tokenKind int

const (
	tEOF tokenKind = iota
	tNumber
	tIdent
	tLParen
	tRParen
	tComma
	tPlus
	tMinus
	tStar
	tSlash
	tLT
	tLE
	tGT
	tGE
	tEQ
	tNE
	tAnd
	tOr
	tNot
)

type token struct {
	kind tokenKind
	text string
	pos  int // 0-based byte offset in the source
}

func lex(src string) ([]token, error) {
	toks := make([]token, 0, 16)
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c >= '0' && c <= '9':
			start := i
			for i < len(src) && src[i] >= '0' && src[i] <= '9' {
				i++
			}
			toks = append(toks, token{tNumber, src[start:i], start})
		case isIdentStart(c):
			start := i
			for i < len(src) && isIdentPart(src[i]) {
				i++
			}
			toks = append(toks, token{tIdent, src[start:i], start})
		default:
			tk, n, ok := lexOp(src, i)
			if !ok {
				return nil, apperr.New("formula.lex", apperr.ParseError,
					fmt.Sprintf("formula %q column %d: unexpected character %q", src, i+1, string(c)))
			}
			toks = append(toks, token{tk, src[i : i+n], i})
			i += n
		}
	}
	toks = append(toks, token{tEOF, "", i})
	return toks, nil
}

func lexOp(src string, i int) (tokenKind, int, bool) {
	if i+1 < len(src) {
		two := src[i : i+2]
		switch two {
		case "<=":
			return tLE, 2, true
		case ">=":
			return tGE, 2, true
		case "==":
			return tEQ, 2, true
		case "!=":
			return tNE, 2, true
		case "&&":
			return tAnd, 2, true
		case "||":
			return tOr, 2, true
		}
	}
	switch src[i] {
	case '(':
		return tLParen, 1, true
	case ')':
		return tRParen, 1, true
	case ',':
		return tComma, 1, true
	case '+':
		return tPlus, 1, true
	case '-':
		return tMinus, 1, true
	case '*':
		return tStar, 1, true
	case '/':
		return tSlash, 1, true
	case '<':
		return tLT, 1, true
	case '>':
		return tGT, 1, true
	case '!':
		return tNot, 1, true
	}
	return 0, 0, false
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}
