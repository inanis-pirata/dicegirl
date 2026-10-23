package app

// Play-session logs (`.log`). One data root (one group, or one CLI data
// directory) keeps at most one in-progress session; finished sessions stay
// available for listing, statistics, export and deletion. The lifecycle is
// shared by the CLI and the chat adapters; adapters only add the transport
// details (plain-message capture, file delivery).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"dicegirl/internal/apperr"
	"dicegirl/internal/storage"
)

const (
	// LogPageSize is the number of sessions shown by one `log list` page.
	LogPageSize = 10
	// logFilenameLimit bounds the human-readable part of an export filename.
	logFilenameLimit = 40
)

// LogSummary is the user-facing view of one session.
type LogSummary struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Creator    string    `json:"creator,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	EndedAt    time.Time `json:"ended_at,omitempty"`
	Entries    int       `json:"entries"`
	Duration   string    `json:"duration"`
	DurationMS int64     `json:"duration_ms"`
}

// LogParticipant is one speaker in a session's statistics.
type LogParticipant struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// LogStatusView is the result of `log`, `log on/off/time` and `log new`.
type LogStatusView struct {
	Kind    string      `json:"kind"`
	Current *LogSummary `json:"current,omitempty"`
	Recent  *LogSummary `json:"recent,omitempty"`
	Format  string      `json:"format"`
	Timer   bool        `json:"timer"`
}

// LogList is one page of `log list`.
type LogList struct {
	Kind  string       `json:"kind"`
	Logs  []LogSummary `json:"logs"`
	Page  int          `json:"page"`
	Pages int          `json:"pages"`
	Total int          `json:"total"`
}

// LogStat is the result of `log stat`.
type LogStat struct {
	Kind         string           `json:"kind"`
	Log          LogSummary       `json:"log"`
	Participants []LogParticipant `json:"participants"`
	FirstAt      time.Time        `json:"first_at,omitempty"`
	LastAt       time.Time        `json:"last_at,omitempty"`
	TopSpeaker   string           `json:"top_speaker,omitempty"`
	TopCount     int              `json:"top_count,omitempty"`
}

// LogExport is the result of `log export` / `log get` / `log end`.
type LogExport struct {
	Kind     string     `json:"kind"`
	Log      LogSummary `json:"log"`
	Format   string     `json:"format"`
	Filename string     `json:"filename"`
	Path     string     `json:"path"`
	Size     int64      `json:"size"`
}

// logElapsed returns the timed duration of a session, including the current
// span when timing is running.
func logElapsed(l *storage.LogSession, now time.Time) time.Duration {
	d := l.Accrued
	if !l.TimerSince.IsZero() {
		d += now.Sub(l.TimerSince)
	}
	if d < 0 {
		d = 0
	}
	return d
}

// formatLogDuration renders a duration compactly for chat output.
func formatLogDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d / time.Hour)
	m := int(d/time.Minute) % 60
	s := int(d/time.Second) % 60
	switch {
	case h > 0:
		return fmt.Sprintf("%d小时%d分", h, m)
	case m > 0:
		return fmt.Sprintf("%d分%d秒", m, s)
	default:
		return fmt.Sprintf("%d秒", s)
	}
}

// summarize converts storage metadata to the public view.
func logSummary(l *storage.LogSession, now time.Time) LogSummary {
	d := logElapsed(l, now)
	out := LogSummary{
		ID:         l.ID,
		Name:       l.Name,
		Status:     l.Status,
		Creator:    l.CreatorName,
		CreatedAt:  l.CreatedAt,
		EndedAt:    l.EndedAt,
		Entries:    l.Entries,
		Duration:   formatLogDuration(d),
		DurationMS: d.Milliseconds(),
	}
	if out.Creator == "" {
		out.Creator = l.CreatorID
	}
	return out
}

// LogStatus returns the current session (if any), the most recent finished
// session (for a helpful empty-state message) and the data root's settings.
func (a *App) LogStatus(ctx context.Context) (*LogStatusView, error) {
	settings, err := a.logs.LogSettings(ctx)
	if err != nil {
		return nil, err
	}
	cur, err := a.logs.CurrentLog(ctx)
	if err != nil {
		return nil, err
	}
	view := &LogStatusView{Kind: "log", Format: settings.Format, Timer: settings.Timer}
	now := time.Now().UTC()
	if cur != nil {
		s := logSummary(cur, now)
		view.Current = &s
		return view, nil
	}
	list, err := a.logs.Logs(ctx)
	if err != nil {
		return nil, err
	}
	if len(list) > 0 {
		s := logSummary(list[0], now)
		view.Recent = &s
	}
	return view, nil
}

// LogNew starts a new session. Only one session may be in progress at a time.
func (a *App) LogNew(ctx context.Context, name, creatorID, creatorName string) (*LogStatusView, error) {
	const op = "app.LogNew"
	if cur, err := a.logs.CurrentLog(ctx); err != nil {
		return nil, err
	} else if cur != nil {
		return nil, apperr.New(op, apperr.Conflict,
			"已有进行中的跑团记录《"+cur.Name+"》；先 log end 或 log halt 结束它")
	}
	now := time.Now().UTC()
	name = strings.TrimSpace(name)
	if name == "" {
		name = "跑团记录 " + now.Local().Format("2006-01-02")
	}
	name, err := a.uniqueLogName(ctx, name)
	if err != nil {
		return nil, err
	}
	settings, err := a.logs.LogSettings(ctx)
	if err != nil {
		return nil, err
	}
	id, err := newLogID()
	if err != nil {
		return nil, err
	}
	l := &storage.LogSession{
		ID:          id,
		Name:        name,
		CreatorID:   creatorID,
		CreatorName: creatorName,
		Status:      storage.LogStatusActive,
		CreatedAt:   now,
		ActiveSince: now,
	}
	if settings.Timer {
		l.TimerSince = now
	}
	if err := a.logs.CreateLog(ctx, l); err != nil {
		return nil, err
	}
	return a.LogStatus(ctx)
}

// uniqueLogName appends a numeric suffix when a name already exists, so
// `log get <名称>` is never ambiguous.
func (a *App) uniqueLogName(ctx context.Context, name string) (string, error) {
	list, err := a.logs.Logs(ctx)
	if err != nil {
		return "", err
	}
	used := make(map[string]bool, len(list))
	for _, l := range list {
		used[strings.ToLower(l.Name)] = true
	}
	candidate := name
	for i := 2; used[strings.ToLower(candidate)]; i++ {
		candidate = fmt.Sprintf("%s %d", name, i)
	}
	return candidate, nil
}

// LogOn resumes the current paused session. An already active session is
// returned unchanged so the command is idempotent.
func (a *App) LogOn(ctx context.Context) (*LogStatusView, error) {
	const op = "app.LogOn"
	cur, err := a.logs.CurrentLog(ctx)
	if err != nil {
		return nil, err
	}
	if cur == nil {
		return nil, apperr.New(op, apperr.NotFound,
			"当前没有暂停的跑团记录（log new [名称] 开始）")
	}
	if cur.Status == storage.LogStatusActive {
		return a.LogStatus(ctx)
	}
	settings, err := a.logs.LogSettings(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	cur.Status = storage.LogStatusActive
	cur.ActiveSince = now
	if settings.Timer {
		cur.TimerSince = now
	} else {
		cur.TimerSince = time.Time{}
	}
	if err := a.logs.UpdateLog(ctx, cur); err != nil {
		return nil, err
	}
	return a.LogStatus(ctx)
}

// LogOff pauses the current session. An already paused session is returned
// unchanged.
func (a *App) LogOff(ctx context.Context) (*LogStatusView, error) {
	const op = "app.LogOff"
	cur, err := a.logs.CurrentLog(ctx)
	if err != nil {
		return nil, err
	}
	if cur == nil {
		return nil, apperr.New(op, apperr.NotFound,
			"当前没有进行中的跑团记录（log new [名称] 开始）")
	}
	if cur.Status == storage.LogStatusPaused {
		return a.LogStatus(ctx)
	}
	now := time.Now().UTC()
	if !cur.TimerSince.IsZero() {
		cur.Accrued += now.Sub(cur.TimerSince)
		cur.TimerSince = time.Time{}
	}
	cur.Status = storage.LogStatusPaused
	cur.ActiveSince = time.Time{}
	if err := a.logs.UpdateLog(ctx, cur); err != nil {
		return nil, err
	}
	return a.LogStatus(ctx)
}

// LogEnd stops the current session. halt marks it as aborted, which skips the
// final export in the command layer.
func (a *App) LogEnd(ctx context.Context, halt bool) (*LogSummary, error) {
	const op = "app.LogEnd"
	cur, err := a.logs.CurrentLog(ctx)
	if err != nil {
		return nil, err
	}
	if cur == nil {
		return nil, apperr.New(op, apperr.NotFound,
			"当前没有进行中的跑团记录（log new [名称] 开始）")
	}
	now := time.Now().UTC()
	if !cur.TimerSince.IsZero() {
		cur.Accrued += now.Sub(cur.TimerSince)
		cur.TimerSince = time.Time{}
	}
	if halt {
		cur.Status = storage.LogStatusHalted
	} else {
		cur.Status = storage.LogStatusEnded
	}
	cur.ActiveSince = time.Time{}
	cur.EndedAt = now
	if err := a.logs.UpdateLog(ctx, cur); err != nil {
		return nil, err
	}
	s := logSummary(cur, now)
	return &s, nil
}

// LogList returns one page of sessions, newest first.
func (a *App) LogList(ctx context.Context, page int) (*LogList, error) {
	if page <= 0 {
		page = 1
	}
	rows, err := a.logs.Logs(ctx)
	if err != nil {
		return nil, err
	}
	pages := (len(rows) + LogPageSize - 1) / LogPageSize
	if pages == 0 {
		pages = 1
	}
	if page > pages {
		page = pages
	}
	start := (page - 1) * LogPageSize
	end := start + LogPageSize
	if end > len(rows) {
		end = len(rows)
	}
	out := &LogList{
		Kind:  "log_list",
		Logs:  make([]LogSummary, 0, end-start),
		Page:  page,
		Pages: pages,
		Total: len(rows),
	}
	now := time.Now().UTC()
	for _, l := range rows[start:end] {
		out.Logs = append(out.Logs, logSummary(l, now))
	}
	return out, nil
}

// resolveLog resolves an empty ref to the current session, then the newest
// session, and otherwise an ID or exact/case-insensitive name.
func (a *App) resolveLog(ctx context.Context, ref string) (*storage.LogSession, error) {
	const op = "app.resolveLog"
	ref = strings.TrimSpace(ref)
	if ref == "" {
		cur, err := a.logs.CurrentLog(ctx)
		if err != nil {
			return nil, err
		}
		if cur != nil {
			return cur, nil
		}
		list, err := a.logs.Logs(ctx)
		if err != nil {
			return nil, err
		}
		if len(list) > 0 {
			return list[0], nil
		}
		return nil, apperr.New(op, apperr.NotFound,
			"还没有跑团记录（log new [名称] 开始）")
	}
	if l, err := a.logs.LogByID(ctx, ref); err != nil {
		return nil, err
	} else if l != nil {
		return l, nil
	}
	list, err := a.logs.Logs(ctx)
	if err != nil {
		return nil, err
	}
	var exact, folded []*storage.LogSession
	for _, l := range list {
		switch {
		case l.Name == ref:
			exact = append(exact, l)
		case strings.EqualFold(l.Name, ref):
			folded = append(folded, l)
		}
	}
	for _, matches := range [][]*storage.LogSession{exact, folded} {
		switch len(matches) {
		case 0:
			continue
		case 1:
			return matches[0], nil
		default:
			return nil, apperr.New(op, apperr.InvalidArgument,
				"日志名 "+ref+" 对应多份记录，请改用 ID")
		}
	}
	return nil, apperr.New(op, apperr.NotFound,
		"找不到跑团记录："+ref+"（可用 log list 查看）")
}

// LogStat returns entry, participant and timing statistics of one session.
func (a *App) LogStat(ctx context.Context, ref string) (*LogStat, error) {
	l, err := a.resolveLog(ctx, ref)
	if err != nil {
		return nil, err
	}
	entries, err := a.logs.LogEntries(ctx, l.ID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := &LogStat{Kind: "log_stat", Log: logSummary(l, now)}
	counts := map[string]int{}
	for _, e := range entries {
		name := strings.TrimSpace(e.Speaker)
		if name == "" {
			name = "系统"
		}
		counts[name]++
		if out.FirstAt.IsZero() || e.At.Before(out.FirstAt) {
			out.FirstAt = e.At
		}
		if e.At.After(out.LastAt) {
			out.LastAt = e.At
		}
	}
	if out.FirstAt.IsZero() {
		out.FirstAt = l.CreatedAt
	}
	if out.LastAt.IsZero() {
		out.LastAt = l.EndedAt
	}
	for name, n := range counts {
		out.Participants = append(out.Participants, LogParticipant{Name: name, Count: n})
	}
	sort.SliceStable(out.Participants, func(i, j int) bool {
		if out.Participants[i].Count != out.Participants[j].Count {
			return out.Participants[i].Count > out.Participants[j].Count
		}
		return out.Participants[i].Name < out.Participants[j].Name
	})
	if len(out.Participants) > 0 {
		out.TopSpeaker = out.Participants[0].Name
		out.TopCount = out.Participants[0].Count
	}
	return out, nil
}

// LogExport writes one session to the data root's logs directory using the
// configured txt/html format and returns its path.
func (a *App) LogExport(ctx context.Context, ref string) (*LogExport, error) {
	const op = "app.LogExport"
	l, err := a.resolveLog(ctx, ref)
	if err != nil {
		return nil, err
	}
	settings, err := a.logs.LogSettings(ctx)
	if err != nil {
		return nil, err
	}
	format := settings.Format
	if format != storage.LogFormatHTML {
		format = storage.LogFormatTXT
	}
	entries, err := a.logs.LogEntries(ctx, l.ID)
	if err != nil {
		return nil, err
	}
	var content string
	if format == storage.LogFormatHTML {
		content = renderLogHTML(l, entries)
	} else {
		content = renderLogText(l, entries)
	}
	dir := filepath.Join(a.cfg.DataRoot, "logs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, apperr.Wrap(op, apperr.StorageWriteFailed, "cannot create log directory", err)
	}
	filename := logFilename(l, format)
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return nil, apperr.Wrap(op, apperr.StorageWriteFailed, "cannot write log export", err)
	}
	info, _ := os.Stat(path)
	size := int64(0)
	if info != nil {
		size = info.Size()
	}
	return &LogExport{
		Kind:     "log_export",
		Log:      logSummary(l, time.Now().UTC()),
		Format:   format,
		Filename: filename,
		Path:     path,
		Size:     size,
	}, nil
}

// LogDelete removes one session and any exported files belonging to it.
func (a *App) LogDelete(ctx context.Context, ref string) (*LogSummary, error) {
	l, err := a.resolveLog(ctx, ref)
	if err != nil {
		return nil, err
	}
	if err := a.logs.DeleteLog(ctx, l.ID); err != nil {
		return nil, err
	}
	a.removeLogExports(l.ID)
	s := logSummary(l, time.Now().UTC())
	return &s, nil
}

// removeLogExports deletes generated files for a session. Failures are
// deliberately ignored: the database record is already gone.
func (a *App) removeLogExports(id string) {
	dir := filepath.Join(a.cfg.DataRoot, "logs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	marker := "-" + id + "."
	for _, e := range entries {
		if !e.IsDir() && strings.Contains(e.Name(), marker) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// LogSetType changes the export format of the data root.
func (a *App) LogSetType(ctx context.Context, format string) (*storage.LogSettings, error) {
	const op = "app.LogSetType"
	format = strings.ToLower(strings.TrimSpace(format))
	if format != storage.LogFormatTXT && format != storage.LogFormatHTML {
		return nil, apperr.New(op, apperr.InvalidArgument,
			"日志格式只能是 txt 或 html")
	}
	settings, err := a.logs.LogSettings(ctx)
	if err != nil {
		return nil, err
	}
	settings.Format = format
	if err := a.logs.SetLogSettings(ctx, settings); err != nil {
		return nil, err
	}
	return settings, nil
}

// LogSetTimer switches the group timer on or off. Turning it on mid-session
// starts counting now; turning it off banks the current span.
func (a *App) LogSetTimer(ctx context.Context, on bool) (*storage.LogSettings, error) {
	settings, err := a.logs.LogSettings(ctx)
	if err != nil {
		return nil, err
	}
	settings.Timer = on
	if err := a.logs.SetLogSettings(ctx, settings); err != nil {
		return nil, err
	}
	cur, err := a.logs.CurrentLog(ctx)
	if err != nil {
		return nil, err
	}
	if cur == nil || cur.Status != storage.LogStatusActive {
		return settings, nil
	}
	now := time.Now().UTC()
	switch {
	case on && cur.TimerSince.IsZero():
		cur.TimerSince = now
	case !on && !cur.TimerSince.IsZero():
		cur.Accrued += now.Sub(cur.TimerSince)
		cur.TimerSince = time.Time{}
	default:
		return settings, nil
	}
	if err := a.logs.UpdateLog(ctx, cur); err != nil {
		return nil, err
	}
	return settings, nil
}

// LogRecording reports whether commands and messages are currently recorded.
func (a *App) LogRecording(ctx context.Context) (bool, error) {
	cur, err := a.logs.CurrentLog(ctx)
	if err != nil {
		return false, err
	}
	return cur != nil && cur.Status == storage.LogStatusActive, nil
}

// LogSay records one ordinary chat message. It is a no-op unless a session is
// actively recording.
func (a *App) LogSay(ctx context.Context, speakerID, speaker, text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return a.appendLog(ctx, &storage.LogEntry{
		Kind:      storage.LogEntrySay,
		SpeakerID: speakerID,
		Speaker:   speaker,
		Text:      text,
	})
}

// LogRecordCommand records one executed command and its rendered output.
func (a *App) LogRecordCommand(ctx context.Context, speakerID, speaker, line, out string) error {
	return a.appendLog(ctx, &storage.LogEntry{
		Kind:      storage.LogEntryCommand,
		SpeakerID: speakerID,
		Speaker:   speaker,
		Text:      line,
		Result:    strings.TrimRight(out, "\n"),
	})
}

// appendLog writes an entry only while a session is actively recording.
func (a *App) appendLog(ctx context.Context, entry *storage.LogEntry) error {
	cur, err := a.logs.CurrentLog(ctx)
	if err != nil {
		return err
	}
	if cur == nil || cur.Status != storage.LogStatusActive {
		return nil
	}
	entry.At = time.Now().UTC()
	return a.logs.AppendLogEntry(ctx, cur.ID, entry)
}

// IsCurrentGameGM reports whether actorID is on the current game's GM roster.
// The QQ adapter uses this fallback for group-management actions because its
// events carry no role flag.
func (a *App) IsCurrentGameGM(ctx context.Context, actorID string) (bool, error) {
	if actorID == "" {
		return false, nil
	}
	t, err := a.CurrentTable(ctx)
	if err != nil {
		return false, err
	}
	meta, err := a.games.GameMeta(storage.WithTable(ctx, t.ID), t.ID)
	if err != nil {
		return false, err
	}
	for _, gm := range meta.GMs {
		if gm.ID == actorID {
			return true, nil
		}
	}
	return false, nil
}

// renderLogText builds the plain-text export.
func renderLogText(l *storage.LogSession, entries []storage.LogEntry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", l.Name)
	fmt.Fprintf(&b, "创建：%s", l.CreatedAt.Local().Format("2006-01-02 15:04:05"))
	if l.CreatorName != "" {
		fmt.Fprintf(&b, "  记录人：%s", l.CreatorName)
	}
	if !l.EndedAt.IsZero() {
		fmt.Fprintf(&b, "  结束：%s", l.EndedAt.Local().Format("2006-01-02 15:04:05"))
	}
	fmt.Fprintf(&b, "  条目：%d\n\n", len(entries))
	for _, e := range entries {
		speaker := e.Speaker
		if speaker == "" {
			speaker = "系统"
		}
		fmt.Fprintf(&b, "[%s] %s", e.At.Local().Format("15:04:05"), speaker)
		if e.Kind == storage.LogEntryCommand {
			fmt.Fprintf(&b, "（%s）", e.Text)
		} else {
			fmt.Fprintf(&b, "：%s", e.Text)
		}
		b.WriteString("\n")
		if e.ImageURL != "" {
			fmt.Fprintf(&b, "    [图片] %s\n", e.ImageURL)
		}
		if e.Result != "" {
			for _, line := range strings.Split(e.Result, "\n") {
				fmt.Fprintf(&b, "    %s\n", line)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderLogHTML builds a self-contained HTML export. The layout is plain on
// purpose: it opens offline without external stylesheets or scripts.
func renderLogHTML(l *storage.LogSession, entries []storage.LogEntry) string {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html lang=\"zh-CN\"><head><meta charset=\"utf-8\">\n")
	fmt.Fprintf(&b, "<title>%s</title>\n", html.EscapeString(l.Name))
	b.WriteString(`<style>
body{font-family:system-ui,-apple-system,"Segoe UI",sans-serif;max-width:860px;margin:2em auto;padding:0 1em;line-height:1.6;color:#222}
h1{font-size:1.5em;border-bottom:2px solid #ddd;padding-bottom:.4em}
.meta{color:#666;font-size:.9em;margin-bottom:1.5em}
.entry{margin:0 0 1em}
.who{font-weight:600}
.cmd{color:#555;font-family:ui-monospace,Consolas,monospace}
.result{white-space:pre-wrap;background:#f6f8fa;border-left:3px solid #bbb;margin:.3em 0 0;padding:.5em .8em;border-radius:4px}
.say{white-space:pre-wrap}
.image{max-width:100%;border-radius:4px;margin-top:.4em}
</style></head><body>
`)
	fmt.Fprintf(&b, "<h1>%s</h1>\n", html.EscapeString(l.Name))
	b.WriteString("<div class=\"meta\">")
	fmt.Fprintf(&b, "创建：%s", l.CreatedAt.Local().Format("2006-01-02 15:04:05"))
	if l.CreatorName != "" {
		fmt.Fprintf(&b, "　记录人：%s", html.EscapeString(l.CreatorName))
	}
	if !l.EndedAt.IsZero() {
		fmt.Fprintf(&b, "　结束：%s", l.EndedAt.Local().Format("2006-01-02 15:04:05"))
	}
	fmt.Fprintf(&b, "　条目：%d</div>\n", len(entries))
	for _, e := range entries {
		speaker := e.Speaker
		if speaker == "" {
			speaker = "系统"
		}
		b.WriteString("<div class=\"entry\">")
		fmt.Fprintf(&b, "<span class=\"who\">%s</span> <span class=\"meta\">%s</span>",
			html.EscapeString(speaker), e.At.Local().Format("15:04:05"))
		if e.Kind == storage.LogEntryCommand {
			fmt.Fprintf(&b, "<div class=\"cmd\">%s</div>", html.EscapeString(e.Text))
		} else {
			fmt.Fprintf(&b, "<div class=\"say\">%s</div>", html.EscapeString(e.Text))
		}
		if e.ImageURL != "" {
			fmt.Fprintf(&b, "<div><img class=\"image\" src=\"%s\" alt=\"图片\"></div>",
				html.EscapeString(e.ImageURL))
		}
		if e.Result != "" {
			fmt.Fprintf(&b, "<pre class=\"result\">%s</pre>", html.EscapeString(e.Result))
		}
		b.WriteString("</div>\n")
	}
	b.WriteString("</body></html>\n")
	return b.String()
}

// logFilename builds a filesystem-safe export name that still includes the
// session id, so two sessions with the same display name never collide.
func logFilename(l *storage.LogSession, format string) string {
	name := sanitizeLogName(l.Name)
	if name == "" {
		name = "log"
	}
	return fmt.Sprintf("%s-%s.%s", name, l.ID, format)
}

// sanitizeLogName removes path separators and characters Windows rejects.
func sanitizeLogName(name string) string {
	var b strings.Builder
	count := 0
	for _, r := range strings.TrimSpace(name) {
		if count >= logFilenameLimit {
			break
		}
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', '\r', '\n', '\t':
			b.WriteByte('_')
		default:
			if r < 0x20 {
				b.WriteByte('_')
			} else {
				b.WriteRune(r)
			}
		}
		count++
	}
	return strings.Trim(strings.TrimSpace(b.String()), ".")
}

// newLogID generates a short, URL-safe session id.
func newLogID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", apperr.Wrap("app.newLogID", apperr.Internal, "cannot generate log id", err)
	}
	return "l-" + hex.EncodeToString(b), nil
}
