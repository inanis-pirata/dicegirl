package dice

import (
	"strings"
	"unicode"
)

type tokenKind int

const (
	tEOF tokenKind = iota
	tNumber
	tWord    // bare letters, e.g. d, kh, kl
	tPercent // %
	tPlus
	tMinus
	tStar
	tSlash
	tLParen
	tRParen
	tBang         // !  (explode modifier)
	tGreater      // >
	tLess         // <
	tGreaterEqual // >=
	tLessEqual    // <=
)

type token struct {
	kind tokenKind
	text string
	pos  int
}

type lexer struct {
	input  string
	tokens []token
}

func lex(input string) ([]token, error) {
	l := &lexer{input: input}
	for i := 0; i < len(input); {
		r := rune(input[i])
		switch {
		case unicode.IsSpace(r):
			i++
		case r >= '0' && r <= '9':
			start := i
			for i < len(input) && input[i] >= '0' && input[i] <= '9' {
				i++
			}
			l.tokens = append(l.tokens, token{tNumber, input[start:i], start})
		case isLetterByte(input[i]):
			start := i
			for i < len(input) && isLetterByte(input[i]) {
				i++
			}
			word := strings.ToLower(input[start:i])
			if word == "x" {
				// Dice! writes the multiplication sign as "X"; a lone
				// word "x" between operands is the same as "*".
				l.tokens = append(l.tokens, token{tStar, "*", start})
				continue
			}
			l.tokens = append(l.tokens, token{tWord, word, start})
		case input[i] == '%':
			l.tokens = append(l.tokens, token{tPercent, "%", i})
			i++
		case input[i] == '+':
			l.tokens = append(l.tokens, token{tPlus, "+", i})
			i++
		case input[i] == '-':
			l.tokens = append(l.tokens, token{tMinus, "-", i})
			i++
		case input[i] == '*':
			l.tokens = append(l.tokens, token{tStar, "*", i})
			i++
		case input[i] == '/':
			l.tokens = append(l.tokens, token{tSlash, "/", i})
			i++
		case input[i] == '(':
			l.tokens = append(l.tokens, token{tLParen, "(", i})
			i++
		case input[i] == ')':
			l.tokens = append(l.tokens, token{tRParen, ")", i})
			i++
		case input[i] == '!':
			l.tokens = append(l.tokens, token{tBang, "!", i})
			i++
		case input[i] == '>':
			if i+1 < len(input) && input[i+1] == '=' {
				l.tokens = append(l.tokens, token{tGreaterEqual, ">=", i})
				i += 2
				continue
			}
			l.tokens = append(l.tokens, token{tGreater, ">", i})
			i++
		case input[i] == '<':
			if i+1 < len(input) && input[i+1] == '=' {
				l.tokens = append(l.tokens, token{tLessEqual, "<=", i})
				i += 2
				continue
			}
			l.tokens = append(l.tokens, token{tLess, "<", i})
			i++
		default:
			return nil, lexError{i: i, ch: string(input[i])}
		}
	}
	l.tokens = append(l.tokens, token{tEOF, "", len(input)})
	return l.tokens, nil
}

type lexError struct {
	i  int
	ch string
}

func (e lexError) Error() string {
	return "unexpected character " + quote(e.ch) + " at position " + itoa(e.i)
}

// isLetterByte accepts ASCII letters only; tokens stay ASCII so expressions
// remain portable across platforms and adapters.
func isLetterByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

func quote(s string) string { return "'" + s + "'" }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}
