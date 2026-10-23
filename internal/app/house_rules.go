package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"dicegirl/internal/apperr"
	"dicegirl/internal/engine"
)

var rulebookIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)

// RuleShow returns the pretty-printed YAML subtree of one rulebook section,
// used by `rule show`.
func (a *App) RuleShow(ctx context.Context, systemID, section, entryID string) ([]byte, error) {
	const op = "app.RuleShow"
	sysID, err := a.defaultSystemID(systemID)
	if err != nil {
		return nil, err
	}
	book, err := a.book(sysID)
	if err != nil {
		return nil, err
	}
	out, err := engine.ExtractYAML(book.Raw(), section, entryID)
	if err != nil {
		return nil, apperr.Wrap(op, apperr.CodeOf(err), "cannot extract "+section, err)
	}
	return out, nil
}

// ApplyRulePatch applies a batch of fine-grained house-rule edits to one
// rulebook, validates the result by recompiling it, persists it to the
// user rulebook directory (copying a built-in book on the first edit) and
// hot-swaps the registered system so the change is live immediately.
func (a *App) ApplyRulePatch(ctx context.Context, systemID string, ops []engine.PatchOp) error {
	const op = "app.ApplyRulePatch"
	id, err := a.defaultSystemID(systemID)
	if err != nil {
		return err
	}
	book, err := a.book(id)
	if err != nil {
		return err
	}

	patched, err := engine.ApplyPatch(book.Raw(), ops)
	if err != nil {
		return apperr.Wrap(op, apperr.CodeOf(err), "cannot apply rule patch", err)
	}

	// Resolve the destination file: user books are edited in place; built-in
	// (or otherwise non-file) books are copied into the writable user
	// rulebook directory on the first edit.
	target := book.Source()
	if !isWritableRulebookSource(target, a.cfg.RulebookDir()) {
		if err := os.MkdirAll(a.cfg.RulebookDir(), 0o755); err != nil {
			return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot create rulebook directory", err)
		}
		target = filepath.Join(a.cfg.RulebookDir(), id+".yaml")
	}

	newBook, err := engine.ParseBook(patched, target, a.roller)
	if err != nil {
		return apperr.Wrap(op, apperr.CodeOf(err), "patched rulebook does not compile", err)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot create rulebook directory", err)
	}
	if err := os.WriteFile(target, patched, 0o644); err != nil {
		return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot write rulebook "+target, err)
	}

	if err := a.registry.Replace(ctx, newBook); err != nil {
		return apperr.Wrap(op, apperr.CodeOf(err), "cannot activate patched rulebook", err)
	}
	return nil
}

// isWritableRulebookSource reports whether source points at an existing file
// inside the user rulebook directory.
func isWritableRulebookSource(source, dir string) bool {
	if source == "" || source == "builtin" {
		return false
	}
	if strings.HasPrefix(source, "builtin:") {
		return false
	}
	abs, err := filepath.Abs(source)
	if err != nil {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absDir, abs)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, "..")
}

// RuleShowAll returns the full raw YAML of one rulebook.
func (a *App) RuleShowAll(ctx context.Context, systemID string) ([]byte, error) {
	id, err := a.defaultSystemID(systemID)
	if err != nil {
		return nil, err
	}
	book, err := a.book(id)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(book.Raw()))
	copy(out, book.Raw())
	return out, nil
}

// Aliases for full export.
func (a *App) ShowRulebook(ctx context.Context, systemID string) ([]byte, error) {
	return a.RuleShowAll(ctx, systemID)
}
func (a *App) ExportRulebook(ctx context.Context, systemID string) ([]byte, error) {
	return a.RuleShowAll(ctx, systemID)
}

// RuleSections returns the entry ids grouped by section for one rulebook.
func (a *App) RuleSections(ctx context.Context, systemID string) (map[string][]string, error) {
	id, err := a.defaultSystemID(systemID)
	if err != nil {
		return nil, err
	}
	book, err := a.book(id)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(book.Raw(), &doc); err != nil {
		return nil, apperr.Wrap("app.RuleSections", apperr.ConfigInvalid, "cannot parse rulebook", err)
	}
	root := doc.Content[0]
	out := map[string][]string{}
	// Mapping sections use the key as the entry id.
	for _, key := range []string{"skills", "limit"} {
		yamlKey := key
		if key == "limit" {
			yamlKey = "limits"
		}
		ids := []string{}
		if n := mapNode(root, yamlKey); n != nil && n.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(n.Content); i += 2 {
				ids = append(ids, n.Content[i].Value)
			}
		}
		sort.Strings(ids)
		out[key] = ids
	}
	// Sequence sections use the element's `id` field.
	seqSections := []struct {
		section string
		root    *yaml.Node
	}{
		{"attributes", mapNode(root, "attributes")},
		{"rules", mapNode(root, "rules")},
		{"tables", mapNode(root, "tables")},
	}
	if checks := mapNode(root, "checks"); checks != nil {
		seqSections = append(seqSections, struct {
			section string
			root    *yaml.Node
		}{"levels", mapNode(checks, "levels")})
	}
	for _, s := range seqSections {
		ids := []string{}
		if s.root != nil && s.root.Kind == yaml.SequenceNode {
			for _, el := range s.root.Content {
				if idn := mapNode(el, "id"); idn != nil {
					ids = append(ids, idn.Value)
				}
			}
		}
		sort.Strings(ids)
		out[s.section] = ids
	}
	return out, nil
}

func mapNode(m *yaml.Node, key string) *yaml.Node {
	if m == nil {
		return nil
	}
	if m.Kind == yaml.DocumentNode && len(m.Content) > 0 {
		m = m.Content[0]
	}
	if m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// CreateRulebookFrom copies an existing rulebook into a new user rulebook
// with a new id, registers it, and returns after persisting.
func (a *App) CreateRulebookFrom(ctx context.Context, baseSystemID, newID string) error {
	const op = "app.CreateRulebookFrom"
	newID = strings.TrimSpace(newID)
	if newID == "" {
		return apperr.New(op, apperr.InvalidArgument, "new rulebook id must not be empty")
	}
	if len(newID) > 64 || !rulebookIDPattern.MatchString(newID) {
		return apperr.New(op, apperr.InvalidArgument,
			"new rulebook id must be lowercase letters, digits and ._- separators")
	}
	if _, ok := a.registry.Get(newID); ok {
		return apperr.New(op, apperr.Conflict, "rule system already registered: "+newID)
	}
	baseID, err := a.defaultSystemID(baseSystemID)
	if err != nil {
		return err
	}
	baseBook, err := a.book(baseID)
	if err != nil {
		return err
	}

	// Rewrite the manifest id (and name) in the copied YAML.
	var doc yaml.Node
	if err := yaml.Unmarshal(baseBook.Raw(), &doc); err != nil {
		return apperr.Wrap(op, apperr.ConfigInvalid, "cannot parse base rulebook", err)
	}
	root := doc.Content[0]
	setTopLevelScalar(root, "id", newID)
	if n := mapNode(root, "name"); n != nil {
		setNodeValue(n, newID)
	}
	// A derived book must not inherit the base book's chat command aliases
	// (they are globally unique) or its default-book flag.
	removeMapKeyNode(root, "commands")
	removeMapKeyNode(root, "default")

	w := &bytes.Buffer{}
	encoder := yaml.NewEncoder(w)
	encoder.SetIndent(2)
	if err := encoder.Encode(&doc); err != nil {
		return apperr.Wrap(op, apperr.ConfigInvalid, "cannot encode new rulebook", err)
	}
	_ = encoder.Close()
	patched := w.Bytes()

	target := filepath.Join(a.cfg.RulebookDir(), newID+".yaml")
	if err := os.MkdirAll(a.cfg.RulebookDir(), 0o755); err != nil {
		return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot create rulebook directory", err)
	}
	if _, err := os.Stat(target); err == nil {
		return apperr.New(op, apperr.Conflict, "rulebook file already exists: "+target)
	}
	newBook, err := engine.ParseBook(patched, target, a.roller)
	if err != nil {
		return apperr.Wrap(op, apperr.CodeOf(err), "new rulebook does not compile", err)
	}
	if err := os.WriteFile(target, patched, 0o644); err != nil {
		return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot write rulebook "+target, err)
	}
	if err := a.registry.Register(ctx, newBook); err != nil {
		_ = os.Remove(target)
		return apperr.Wrap(op, apperr.CodeOf(err), "cannot register new rulebook", err)
	}
	return nil
}

// Aliases for creation.
func (a *App) CreateRulebook(ctx context.Context, baseSystemID, newID string) error {
	return a.CreateRulebookFrom(ctx, baseSystemID, newID)
}
func (a *App) NewRulebook(ctx context.Context, baseSystemID, newID string) error {
	return a.CreateRulebookFrom(ctx, baseSystemID, newID)
}
func (a *App) CopyRulebook(ctx context.Context, baseSystemID, newID string) error {
	return a.CreateRulebookFrom(ctx, baseSystemID, newID)
}

func setTopLevelScalar(root *yaml.Node, key, value string) {
	if root == nil || root.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			setNodeValue(root.Content[i+1], value)
			return
		}
	}
	// append if missing
	root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}

func setNodeValue(n *yaml.Node, value string) {
	if n == nil {
		return
	}
	n.Kind = yaml.ScalarNode
	n.Tag = "!!str"
	n.Value = value
}

func removeMapKeyNode(root *yaml.Node, key string) {
	if root == nil || root.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			root.Content = append(root.Content[:i], root.Content[i+2:]...)
			return
		}
	}
}

// Convenience aliases so callers can use either naming.
func (a *App) PatchRulebook(ctx context.Context, systemID string, ops []engine.PatchOp) error {
	return a.ApplyRulePatch(ctx, systemID, ops)
}

func (a *App) EditRulebook(ctx context.Context, systemID string, ops []engine.PatchOp) error {
	return a.ApplyRulePatch(ctx, systemID, ops)
}

func (a *App) EditRule(ctx context.Context, systemID string, ops []engine.PatchOp) error {
	return a.ApplyRulePatch(ctx, systemID, ops)
}
