package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitTextCJK(t *testing.T) {
	// The help text that triggered the original panic: multi-byte runes,
	// more than one chunk long.
	s := strings.Repeat("🎲 掷骰检定帮助文本\n", 200)
	chunks := splitText(s, maxChunkRunes)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if n := utf8.RuneCountInString(c); n > maxChunkRunes {
			t.Fatalf("chunk %d has %d runes, max %d", i, n, maxChunkRunes)
		}
	}
	if joined := strings.Join(chunks, ""); joined != s {
		t.Fatal("chunks do not reassemble the input")
	}
}

func TestSplitTextPrefersNewline(t *testing.T) {
	s := strings.Repeat("a", 1100) + "\n" + strings.Repeat("b", 200)
	chunks := splitText(s, maxChunkRunes)
	if len(chunks) != 2 {
		t.Fatalf("want 2 chunks, got %d", len(chunks))
	}
	if strings.ContainsAny(chunks[0], "b") {
		t.Fatalf("first chunk should end at the newline: %q…", chunks[0][:20])
	}
}

func TestSplitHalf(t *testing.T) {
	s := strings.Repeat("帮助文本\n", 300) + "末尾"
	head, tail := splitHalf(s)
	if head+tail != s {
		t.Fatal("halves do not reassemble the input")
	}
	if !strings.HasSuffix(head, "\n") {
		t.Fatalf("head should end at a newline: %q…", tail[:0])
	}
	if utf8.RuneCountInString(head) < len([]rune(s))/3 {
		t.Fatalf("split too early: %d of %d runes", utf8.RuneCountInString(head), utf8.RuneCountInString(s))
	}
}

func TestSplitTextShortAndEmpty(t *testing.T) {
	if got := splitText("", 10); len(got) != 0 {
		t.Fatalf("empty input: %v", got)
	}
	if got := splitText("短文本", 10); len(got) != 1 || got[0] != "短文本" {
		t.Fatalf("short input: %v", got)
	}
}
