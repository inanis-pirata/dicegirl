package app

// Import of the legacy QQ play logs. Before the shared `.log` lifecycle,
// the QQ adapter kept one Markdown file per session under
// <data-root>/logs/*.md and only tracked the current file in adapter.json.
// The importer converts those files into ordinary LogSession rows so old
// campaigns stay visible to `log list/get/stat/export`. The original
// Markdown files are left untouched.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"dicegirl/internal/apperr"
	"dicegirl/internal/storage"
)

// LegacyLogSource describes the legacy adapter's current session. File is
// relative to the data root; StartedAt is the timestamp recorded in
// adapter.json. When Active is true and the database has no in-progress
// session yet, the imported file keeps recording status.
type LegacyLogSource struct {
	Active    bool
	File      string
	StartedAt time.Time
}

var legacyLogHeadingRE = regexp.MustCompile(`^###\s+(\d{2}:\d{2}:\d{2})\s*[·•]\s*(.*)$`)

// ImportLegacyLogs converts <root>/logs/*.md into database sessions. It is
// idempotent: each source file maps to a deterministic log id, so running it
// again (or after a partial failure) never duplicates a session.
func (a *App) ImportLegacyLogs(ctx context.Context, root string, source LegacyLogSource) (int, error) {
	const op = "app.ImportLegacyLogs"
	dir := filepath.Join(root, "logs")
	dirEntries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read legacy log directory", err)
	}

	names := make([]string, 0, len(dirEntries))
	for _, e := range dirEntries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	current, err := a.logs.CurrentLog(ctx)
	if err != nil {
		return 0, err
	}
	activeRel := legacyRelativePath(root, source.File)
	imported := 0
	for _, name := range names {
		path := filepath.Join(dir, name)
		rel := legacyRelativePath(root, path)
		id := legacyLogID(rel)
		existing, err := a.logs.LogByID(ctx, id)
		if err != nil {
			return imported, err
		}
		if existing != nil {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return imported, apperr.Wrap(op, apperr.StorageReadFailed,
				"cannot read legacy log "+rel, err)
		}
		info, _ := os.Stat(path)
		fallback := time.Now()
		if info != nil {
			fallback = info.ModTime()
		}
		parsed, ok := parseLegacyLog(string(raw), strings.TrimSuffix(name, filepath.Ext(name)), fallback)
		if !ok {
			continue
		}

		isActive := activeRel != "" && activeRel == rel
		created := parsed.Start
		if isActive && !source.StartedAt.IsZero() {
			created = source.StartedAt
		}
		if created.IsZero() {
			created = fallback
		}
		created = created.UTC()

		status := storage.LogStatusEnded
		ended := parsed.Last
		if isActive && source.Active && current == nil {
			status = storage.LogStatusActive
			ended = time.Time{}
		} else if ended.IsZero() {
			ended = created
		}
		ended = ended.UTC()

		name, err := a.uniqueLogName(ctx, parsed.Title)
		if err != nil {
			return imported, err
		}
		session := &storage.LogSession{
			ID:        id,
			Name:      name,
			Status:    status,
			CreatedAt: created,
			EndedAt:   ended,
		}
		if status == storage.LogStatusActive {
			session.ActiveSince = created
		}
		if err := a.logs.CreateLog(ctx, session); err != nil {
			return imported, err
		}
		for i := range parsed.Entries {
			if err := a.logs.AppendLogEntry(ctx, id, &parsed.Entries[i]); err != nil {
				// Do not leave a half-imported session behind: a retry
				// would otherwise skip it because the id now exists.
				_ = a.logs.DeleteLog(ctx, id)
				return imported, err
			}
		}
		imported++
	}
	return imported, nil
}

// legacyRelativePath normalizes a path recorded by the old adapter to a
// slash-separated path relative to the data root.
func legacyRelativePath(root, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	// adapter.json may have been written on Windows and copied to Linux
	// (or vice versa), so accept either separator.
	path = strings.ReplaceAll(path, "\\", "/")
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(path))
	}
	return filepath.ToSlash(filepath.Clean(rel))
}

// legacyLogID derives a stable id from the source file's data-root-relative
// path, keeping reruns idempotent.
func legacyLogID(rel string) string {
	sum := sha256.Sum256([]byte("dicegirl-legacy-log:" + strings.ToLower(filepath.ToSlash(rel))))
	return "l-" + hex.EncodeToString(sum[:4])
}

// legacyParsedLog is one converted Markdown session.
type legacyParsedLog struct {
	Title   string
	Start   time.Time
	Last    time.Time
	Entries []storage.LogEntry
}

// parseLegacyLog reads the old adapter's Markdown shape:
//
//	# 标题
//
//	开始：2006-01-02 15:04:05
//
//	### 15:04:05 · 说话人
//
//	> 指令
//
//	    result line
//
// Files that do not contain any legacy marker are skipped so unrelated
// Markdown in the directory is not imported.
func parseLegacyLog(raw, fallbackName string, fallback time.Time) (*legacyParsedLog, bool) {
	raw = strings.TrimPrefix(raw, "\ufeff")
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	lines := strings.Split(raw, "\n")
	out := &legacyParsedLog{}
	saw := false
	seenContent := false
	for _, line := range lines {
		if !seenContent {
			if strings.TrimSpace(line) == "" {
				continue
			}
			seenContent = true
			if strings.HasPrefix(line, "# ") {
				out.Title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
				saw = true
				continue
			}
		}
		var stamp string
		switch {
		case strings.HasPrefix(line, "开始："):
			stamp = strings.TrimSpace(strings.TrimPrefix(line, "开始："))
		case strings.HasPrefix(line, "开始:"):
			stamp = strings.TrimSpace(strings.TrimPrefix(line, "开始:"))
		default:
			continue
		}
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", stamp, time.Local); err == nil {
			out.Start = t
			saw = true
		}
	}
	if out.Title == "" {
		out.Title = strings.TrimSpace(fallbackName)
	}
	if out.Title == "" {
		out.Title = "旧版战报"
	}

	base := out.Start
	if base.IsZero() {
		base = fallback
	}
	if base.IsZero() {
		base = time.Now()
	}
	base = time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, time.Local)

	var prev time.Time
	for i := 0; i < len(lines); i++ {
		m := legacyLogHeadingRE.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		clock, err := time.Parse("15:04:05", m[1])
		if err != nil {
			continue
		}
		at := time.Date(base.Year(), base.Month(), base.Day(),
			clock.Hour(), clock.Minute(), clock.Second(), 0, time.Local)
		for !prev.IsZero() && at.Before(prev) && prev.Sub(at) > 12*time.Hour {
			at = at.AddDate(0, 0, 1)
		}

		j := i + 1
		for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
			j++
		}
		if j >= len(lines) {
			break
		}
		if !strings.HasPrefix(strings.TrimLeft(lines[j], " \t"), ">") {
			i = j - 1
			continue
		}
		command := strings.TrimSpace(strings.TrimPrefix(strings.TrimLeft(lines[j], " \t"), ">"))
		j++
		// The old writer separates the quoted command from its indented
		// result with one blank line.
		for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
			j++
		}

		var result []string
		for j < len(lines) {
			if !strings.HasPrefix(lines[j], "    ") {
				break
			}
			result = append(result, strings.TrimPrefix(lines[j], "    "))
			j++
		}
		for len(result) > 0 && strings.TrimSpace(result[len(result)-1]) == "" {
			result = result[:len(result)-1]
		}

		speaker := strings.TrimSpace(m[2])
		out.Entries = append(out.Entries, storage.LogEntry{
			At:      at,
			Kind:    storage.LogEntryCommand,
			Speaker: speaker,
			Text:    command,
			Result:  strings.Join(result, "\n"),
		})
		if at.After(out.Last) {
			out.Last = at
		}
		if prev.IsZero() || at.After(prev) {
			prev = at
		}
		saw = true
		i = j - 1
	}
	return out, saw
}
