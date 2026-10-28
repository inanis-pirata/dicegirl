package main

import (
	"strings"
	"testing"
	"time"
)

// ruleMutation decides the master-gated path; the gate and the inbox
// handoff both depend on it, so every recognised and non-recognised shape
// is pinned here.
func TestRuleMutation(t *testing.T) {
	cases := []struct {
		line  string
		isMut bool
		isAdd bool
	}{
		{"rule add", true, true},
		{".rule add", true, true},
		{"rule add --force", true, true},
		{"rule install", true, true},
		{"rule del myrpg", true, false},
		{"rule remove myrpg", true, false},
		{"rule uninstall myrpg", true, false},
		{"rule list", false, false},
		{"rule validate --system myrpg", false, false},
		{"rule", false, false},
		{"r 3d6", false, false},
		{"ra 心理学", false, false},
		{"", false, false},
	}
	for _, tc := range cases {
		isMut, isAdd := ruleMutation(tc.line)
		if isMut != tc.isMut || isAdd != tc.isAdd {
			t.Fatalf("ruleMutation(%q) = (%v,%v), want (%v,%v)",
				tc.line, isMut, isAdd, tc.isMut, tc.isAdd)
		}
	}
}

func TestYAMLFileAttachment(t *testing.T) {
	// A rulebook upload: content_type=file with a yaml name.
	hit := messageEvent{Attachments: []messageAttachment{
		{ContentType: "image/jpeg", Filename: "photo.jpg", URL: "https://x/1"},
		{ContentType: "file", Filename: "coc-house.yaml", URL: "https://x/2"},
	}}
	if att, ok := yamlFileAttachment(hit); !ok || att.Filename != "coc-house.yaml" {
		t.Fatalf("yaml attachment not selected: %+v ok=%v", att, ok)
	}

	// Everything else is ignored: images, non-yaml files, no attachments.
	for name, ev := range map[string]messageEvent{
		"image only":   {Attachments: []messageAttachment{{ContentType: "image/png", Filename: "a.png"}}},
		"file not yml": {Attachments: []messageAttachment{{ContentType: "file", Filename: "notes.txt"}}},
		"yaml not file": {Attachments: []messageAttachment{
			{ContentType: "image/jpeg", Filename: "rule.yaml"},
		}},
		"empty": {},
	} {
		if _, ok := yamlFileAttachment(ev); ok {
			t.Fatalf("%s must not be treated as a rulebook upload", name)
		}
	}
}

func TestFileInboxTakeOnceAndExpire(t *testing.T) {
	inbox := newFileInbox()
	if _, ok := inbox.take("u1"); ok {
		t.Fatal("empty inbox must miss")
	}
	inbox.put("u1", pendingFile{Path: "/tmp/a.yaml", Name: "a.yaml", At: time.Now()})
	p, ok := inbox.take("u1")
	if !ok || p.Path != "/tmp/a.yaml" {
		t.Fatalf("take = %+v ok=%v", p, ok)
	}
	if _, ok := inbox.take("u1"); ok {
		t.Fatal("take must consume the entry")
	}
	// Expired entries are dropped instead of returned.
	inbox.put("u2", pendingFile{Path: "/tmp/old.yaml", At: time.Now().Add(-2 * inboxTTL)})
	if _, ok := inbox.take("u2"); ok {
		t.Fatal("expired entry must not be returned")
	}
	// A fresh upload replaces the sender's previous one.
	inbox.put("u3", pendingFile{Path: "/tmp/1.yaml", At: time.Now()})
	inbox.put("u3", pendingFile{Path: "/tmp/2.yaml", At: time.Now()})
	if p, _ := inbox.take("u3"); p.Path != "/tmp/2.yaml" {
		t.Fatalf("latest upload must win, got %s", p.Path)
	}
}

func TestSanitizeFilename(t *testing.T) {
	if got := sanitizeFilename(`..\evil.yaml`); strings.ContainsAny(got, `\/`) {
		t.Fatalf("path separators must be stripped: %q", got)
	}
	if got := sanitizeFilename(""); got != "rulebook.yaml" {
		t.Fatalf("empty name: %q", got)
	}
	if got := sanitizeFilename("my book.yaml"); strings.Contains(got, " ") {
		t.Fatalf("spaces must be replaced: %q", got)
	}
}

func TestSenderKeyPrefersUnionOpenid(t *testing.T) {
	ev := messageEvent{Author: author{
		UnionOpenid: "U1", UserOpenid: "P1", MemberOpenid: "M1",
	}}
	if got := senderKey(ev); got != "U1" {
		t.Fatalf("senderKey = %q, want U1", got)
	}
	if got := senderKey(messageEvent{Author: author{UserOpenid: "P1"}}); got != "P1" {
		t.Fatalf("c2c fallback = %q", got)
	}
	if got := senderKey(messageEvent{Author: author{MemberOpenid: "M1"}}); got != "M1" {
		t.Fatalf("group fallback = %q", got)
	}
}
