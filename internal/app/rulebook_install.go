package app

// User rulebook installation (`rule add` / `rule del`): a master uploads a
// rulebook YAML (in chat via a file attachment, in the CLI via a path), the
// book is compiled under the engine's strict KnownFields validation and only
// then written to <data-root>/rulebooks/ and registered — a bad file never
// touches the disk. Built-in (embedded) books can be neither overwritten nor
// deleted; user books can be replaced with force.

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"dicegirl/internal/apperr"
	"dicegirl/internal/engine"
)

// MaxRulebookBytes caps one uploaded rulebook. The built-in books are tens of
// kilobytes; anything beyond half a megabyte is not a rulebook, it is abuse
// (or a fat finger) and rejecting it early keeps the inbox download cheap.
const MaxRulebookBytes = 512 << 10

// InstallRulebook validates and registers an uploaded rulebook YAML and
// persists it under the user rulebook directory. The id is taken from the
// YAML's top-level `id`, not from the filename. With force, an existing user
// book with the same id is replaced (built-in books are refused regardless).
// Returns the installed id.
func (a *App) InstallRulebook(ctx context.Context, data []byte, force bool) (string, error) {
	const op = "app.InstallRulebook"
	if len(data) == 0 {
		return "", apperr.New(op, apperr.InvalidArgument, "规则书内容为空")
	}
	if len(data) > MaxRulebookBytes {
		return "", apperr.New(op, apperr.InvalidArgument,
			"规则书超过大小上限（"+itoa(MaxRulebookBytes>>10)+" KiB）")
	}

	// The id comes from the document itself.
	id := topLevelString(data, "id")
	if id == "" {
		return "", apperr.New(op, apperr.ConfigInvalid, "规则书缺少顶层 id 字段")
	}
	if len(id) > 64 || !rulebookIDPattern.MatchString(id) {
		return "", apperr.New(op, apperr.InvalidArgument,
			"规则书 id 必须是小写字母、数字与 ._- 分隔符（当前："+id+"）")
	}

	dir := a.cfg.RulebookDir()
	target := filepath.Join(dir, id+".yaml")

	// Classify the collision: built-in books are untouchable, existing user
	// books need force, a fresh id is a plain install.
	if existing, ok := a.bookOf(id); ok {
		if !isWritableRulebookSource(existing.Source(), dir) {
			return "", apperr.New(op, apperr.Conflict,
				"内置规则书 "+id+" 不允许覆盖；请改用其他 id")
		}
		if !force {
			return "", apperr.New(op, apperr.Conflict,
				"规则书 "+id+" 已存在（用户规则书）；覆盖请加 --force")
		}
	} else if _, err := os.Stat(target); err == nil && !force {
		// A file exists but no book is registered (e.g. a previous crash
		// left a broken file): treat as user data, still ask for force so
		// an explicit yes precedes clobbering anything on disk.
		return "", apperr.New(op, apperr.Conflict,
			"已存在同名规则书文件 "+target+"；覆盖请加 --force")
	}

	// Compile under the strict parser BEFORE anything touches the disk.
	book, err := engine.ParseBook(data, target, a.roller)
	if err != nil {
		return "", apperr.Wrap(op, apperr.CodeOf(err), "规则书校验失败（未写入磁盘）", err)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", apperr.Wrap(op, apperr.StorageWriteFailed, "cannot create rulebook directory", err)
	}
	// Keep the previous bytes so a failed hot-swap can restore them.
	var previous []byte
	if old, err := os.ReadFile(target); err == nil {
		previous = old
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return "", apperr.Wrap(op, apperr.StorageWriteFailed, "cannot write rulebook "+target, err)
	}

	if force {
		err = a.registry.Replace(ctx, book)
	} else {
		err = a.registry.Register(ctx, book)
	}
	if err != nil {
		if previous != nil {
			_ = os.WriteFile(target, previous, 0o644)
		} else {
			_ = os.Remove(target)
		}
		return "", apperr.Wrap(op, apperr.CodeOf(err), "规则书写入成功但热更新失败，已回滚", err)
	}
	return id, nil
}

// UninstallRulebook removes a user rulebook: the file is deleted and the
// system unregistered. Built-in books are refused; a book that still has
// characters on it is refused (delete or move the cards first); the current
// default book is refused (switch the default first), because every
// no-system command would lose its rulebook.
func (a *App) UninstallRulebook(ctx context.Context, id string) error {
	const op = "app.UninstallRulebook"
	book, ok := a.bookOf(id)
	if !ok {
		return apperr.New(op, apperr.RuleNotFound, "rule system not registered: "+id)
	}
	if book == nil {
		return apperr.New(op, apperr.Conflict, "规则书 "+id+" 不是可删除的文件型规则书")
	}
	source := book.Source()
	if !isWritableRulebookSource(source, a.cfg.RulebookDir()) {
		return apperr.New(op, apperr.Conflict, "内置规则书 "+id+" 不能删除")
	}
	cards, err := a.chars.List(ctx)
	if err != nil {
		return err
	}
	inUse := 0
	for _, c := range cards {
		if c != nil && c.SystemID == id {
			inUse++
		}
	}
	if inUse > 0 {
		return apperr.New(op, apperr.Conflict,
			"规则书 "+id+" 还有 "+itoa(inUse)+" 张角色卡在使用；请先删除或迁移这些卡")
	}
	if a.registry.DefaultID() == id {
		return apperr.New(op, apperr.Conflict,
			"规则书 "+id+" 是当前默认规则书；请先安装或指定其他默认规则书")
	}
	if err := os.Remove(source); err != nil && !os.IsNotExist(err) {
		return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot remove rulebook "+source, err)
	}
	if err := a.registry.Unregister(id); err != nil {
		return apperr.Wrap(op, apperr.CodeOf(err), "cannot unregister rulebook "+id, err)
	}
	return nil
}

// topLevelString reads one top-level scalar mapping value from a YAML
// document without full-deserializing it (the strict decode happens later,
// in ParseBook).
func topLevelString(data []byte, key string) string {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return ""
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return ""
	}
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key && root.Content[i+1].Kind == yaml.ScalarNode {
			return strings.TrimSpace(root.Content[i+1].Value)
		}
	}
	return ""
}

// itoa is a tiny local helper avoiding a strconv import in every caller.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
