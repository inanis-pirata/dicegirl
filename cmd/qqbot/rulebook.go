package main

// Rulebook upload over chat (`rule add`): a master sends a .yaml file, then
// runs `rule add`; the adapter downloads the attachment into a local inbox
// file and hands its path to the shared executor. Every platform detail —
// the attachment download, the master allowlist, the pending-file handoff —
// lives here so chatcore stays platform-neutral.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"dicegirl/chatcore"
	"dicegirl/internal/app"
	"dicegirl/internal/apperr"
)

const (
	inboxTTL      = 15 * time.Minute
	maxDownload   = app.MaxRulebookBytes + 1024 // download slightly past the cap to give a real error
	inboxFileMode = 0o644
)

// pendingFile is one downloaded attachment waiting for its `rule add`.
type pendingFile struct {
	Path  string
	Name  string
	Size  int64
	At    time.Time
	Scope string // botScopeGroup / botScopeC2C, for the log only
}

// fileInbox remembers the most recent downloadable file per sender. Entries
// expire lazily; a new file from the same sender replaces the old one.
type fileInbox struct {
	mu    sync.Mutex
	byKey map[string]pendingFile
}

func newFileInbox() *fileInbox {
	return &fileInbox{byKey: map[string]pendingFile{}}
}

func (f *fileInbox) put(sender string, p pendingFile) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byKey[sender] = p
}

// take removes and returns the sender's pending file, or false when none is
// fresh. Expired entries are deleted on the way through.
func (f *fileInbox) take(sender string) (pendingFile, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.byKey[sender]
	if !ok {
		return p, false
	}
	delete(f.byKey, sender)
	if time.Since(p.At) > inboxTTL {
		return pendingFile{}, false
	}
	return p, true
}

// senderKey collapses the three openid flavours into one inbox key: the
// union openid when present, otherwise the scene openid. The same person
// uploading from a group and from a private chat then shares one slot.
func senderKey(ev messageEvent) string {
	if ev.Author.UnionOpenid != "" {
		return ev.Author.UnionOpenid
	}
	if ev.Author.UserOpenid != "" {
		return ev.Author.UserOpenid
	}
	return ev.Author.MemberOpenid
}

// isMaster reports whether the sender may run rule add / rule del: its
// union/user/member openid must be listed in DICEGIRL_BOT_MASTERS.
func (h *botHandler) isMaster(ev messageEvent) bool {
	for _, id := range []string{ev.Author.UnionOpenid, ev.Author.UserOpenid, ev.Author.MemberOpenid} {
		if id != "" && h.cfg.Masters[id] {
			return true
		}
	}
	return false
}

// ruleMutation reports whether the line installs or uninstalls a rulebook
// (the two commands gated behind the master allowlist). Pure, so the gate is
// testable without a handler.
func ruleMutation(line string) (isMutation, isAdd bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return false, false
	}
	name, _, _, _ := chatcore.SplitCommandToken(fields[0])
	if name != "rule" {
		return false, false
	}
	switch fields[1] {
	case "add", "install", "upload":
		return true, true
	case "del", "remove", "uninstall":
		return true, false
	}
	return false, false
}

// yamlFileAttachment returns the first file attachment that looks like a
// rulebook upload: content_type "file" with a .yaml/.yml filename.
func yamlFileAttachment(ev messageEvent) (messageAttachment, bool) {
	for _, a := range ev.Attachments {
		name := strings.ToLower(strings.TrimSpace(a.Filename))
		if a.ContentType == "file" && (strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")) {
			return a, true
		}
	}
	return messageAttachment{}, false
}

// stashUpload downloads a rulebook attachment into the inbox directory and
// remembers it for the sender. The reply text tells the master what to do
// next; the download URL is never echoed.
func (h *botHandler) stashUpload(ctx context.Context, scope string, ev messageEvent, att messageAttachment) string {
	sender := senderKey(ev)
	dir := filepath.Join(h.cfg.DataRoot, "inbox")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		h.logger.Printf("创建收件目录失败：%v", err)
		return "保存文件失败：无法创建收件目录"
	}
	if att.Size > app.MaxRulebookBytes {
		return "文件过大（上限 " + itoa64(app.MaxRulebookBytes>>10) + " KiB），未接收"
	}
	name := filepath.Base(strings.TrimSpace(att.Filename))
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "rulebook.yaml"
	}
	path := filepath.Join(dir, fmt.Sprintf("%d-%s", time.Now().UnixNano(), sanitizeFilename(name)))
	if err := downloadFile(ctx, h.api.http, att.URL, path); err != nil {
		h.logger.Printf("下载规则书附件失败 sender=%s：%v", sender, err)
		return "文件下载失败：" + err.Error()
	}
	st, err := os.Stat(path)
	if err == nil && st.Size() > app.MaxRulebookBytes {
		_ = os.Remove(path)
		return "文件过大（上限 " + itoa64(app.MaxRulebookBytes>>10) + " KiB），未接收"
	}
	h.inbox.put(sender, pendingFile{Path: path, Name: name, Size: stSize(st), At: time.Now(), Scope: scope})
	h.logger.Printf("规则书附件已入箱 sender=%s file=%s size=%d", sender, name, stSize(st))
	return "已收到规则书文件 " + name + "。请继续发送 rule add 安装（覆盖同名规则书用 rule add --force）"
}

// runRuleMutation executes a gated `rule add` / `rule del` line. The master
// gate runs BEFORE anything touches the executor; `rule add` without --file
// consumes the sender's pending inbox file and always cleans it up.
func (h *botHandler) runRuleMutation(ctx context.Context, scope, chatID string, ev messageEvent, content string, execute func(line string) (string, int, bool)) {
	mutation, isAdd := ruleMutation(content)
	if !mutation {
		return
	}
	if !h.isMaster(ev) {
		h.postText(ctx, scope, chatID, ev.ID, 1,
			"rule add / rule del 仅限 bot master 使用（服务端配置 DICEGIRL_BOT_MASTERS）")
		return
	}
	fields := strings.Fields(content)
	if isAdd && !strings.Contains(content, "--file") {
		pending, ok := h.inbox.take(senderKey(ev))
		if !ok {
			h.postText(ctx, scope, chatID, ev.ID, 1,
				"没有待安装的规则书文件。请先发送 .yaml 规则书文件，再发送 rule add")
			return
		}
		defer func() { _ = os.Remove(pending.Path) }()
		content = "rule add --file " + pending.Path
		if fields[1] == "add" {
			// preserve an explicit --force written after the command
			for _, f := range fields[2:] {
				if f == "--force" {
					content += " --force"
				}
			}
		}
	}
	out, code, _ := execute(content)
	h.replyChunks(ctx, scope, chatID, ev.ID, out, code, maxPassiveGroup)
}

// downloadFile streams url to path with a hard byte cap.
func downloadFile(ctx context.Context, client *http.Client, url, path string) error {
	if url == "" {
		return errors.New("附件缺少下载地址")
	}
	if client == nil {
		client = &http.Client{Timeout: httpTimeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return apperr.New("qqbot.downloadFile", apperr.UpstreamFailed,
			"下载返回 "+resp.Status)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, inboxFileMode)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(f, io.LimitReader(resp.Body, maxDownload))
	if err != nil {
		return err
	}
	return nil
}

// sanitizeFilename strips anything path-like from a client-supplied name.
func sanitizeFilename(name string) string {
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', ' ', '\t', '\n', '\r':
			return '_'
		}
		return r
	}, name)
	if name == "" {
		return "rulebook.yaml"
	}
	return name
}

func stSize(st os.FileInfo) int64 {
	if st == nil {
		return 0
	}
	return st.Size()
}

func itoa64(n int64) string {
	return strconv.FormatInt(n, 10)
}
