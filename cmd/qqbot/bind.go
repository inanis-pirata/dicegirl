package main

// Private-channel binding. QQ group events expose only member_openid while
// the C2C send API needs user_openid, and the two are not correlated by the
// platform (union_openid arrives empty in practice). So a group member
// completes a one-time code in a private chat; the bot then remembers
// member_openid → user_openid for that group. Pending work (a hidden-roll
// result, an observer join) is carried with the code and completed on bind.

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

const bindCodeTTL = 10 * time.Minute

type pendingBind struct {
	GroupID  string
	Member   string
	Text     string // private text to deliver once bound (hidden-roll result)
	Observer bool   // enroll as an observer once bound
	Expires  time.Time
}

type bindManager struct {
	mu sync.Mutex
	m  map[string]pendingBind
}

func newBindManager() *bindManager {
	return &bindManager{m: map[string]pendingBind{}}
}

// issue stores a pending action and returns its one-time code.
func (b *bindManager) issue(p pendingBind) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.expireLocked()
	p.Expires = time.Now().Add(bindCodeTTL)
	var code string
	for {
		buf := make([]byte, 4)
		_, _ = rand.Read(buf)
		code = strings.ToUpper(hex.EncodeToString(buf))
		if _, exists := b.m[code]; !exists {
			break
		}
	}
	b.m[code] = p
	return code
}

// take consumes a code, returning the pending action when valid.
func (b *bindManager) take(code string) (pendingBind, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.expireLocked()
	p, ok := b.m[strings.ToUpper(strings.TrimSpace(code))]
	if !ok {
		return pendingBind{}, false
	}
	delete(b.m, strings.ToUpper(strings.TrimSpace(code)))
	if time.Now().After(p.Expires) {
		return pendingBind{}, false
	}
	return p, true
}

func (b *bindManager) expireLocked() {
	now := time.Now()
	for k, p := range b.m {
		if now.After(p.Expires) {
			delete(b.m, k)
		}
	}
}

// parseBindCommand recognizes the private-chat binding message:
//
//	绑定 1A2B3C4D
//	.绑定 1A2B3C4D
//	bind 1A2B3C4D
func parseBindCommand(content string) (string, bool) {
	fields := strings.Fields(content)
	if len(fields) != 2 {
		return "", false
	}
	name := strings.TrimLeft(fields[0], ".。!！")
	switch name {
	case "绑定", "bind", "绑定码":
		return fields[1], true
	}
	return "", false
}
