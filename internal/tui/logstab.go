package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/plat/open"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// The Logs tab of the , popup (docs/SPEC.md §4): the logs on the
// server's host, read through it (log.list, log.read), so a console of
// another host's server shows that server's. [ and ] pick the log:
// the server's, launchd's service log, the consoles' and each mod's.
// It shows the log's end and follows it as it grows; scrolling up reads
// the chunk before what it holds, one at a time, never the whole file.
// / searches what it holds (n older, N newer), v keeps the lines of a
// level when they carry one, o opens the file in $PAGER when it is on
// this host and c copies its path.
//
//	" Server   Service   Console   mod linear "
//	"3.8 MB · following · ~/.terminatr/logs/server.log"
//	"2026/10/18 01:08:50.075414 session s-693: claude working/"
//	…

// logsTab is the Logs tab: the logs as last listed, the one shown and
// the lines read of it.
type logsTab struct {
	logs   []proto.LogInfo
	err    error
	loaded bool
	cur    int
	// lines are the log's lines read so far, the file's bytes [start,
	// end); size is its size when last read.
	lines            []logLine
	start, end, size int64
	readErr          error
	// first is the index in lines of the first line shown, as the last
	// drawing found it; atEnd says the last line showed. With follow the
	// end shows, and each tick reads what was added.
	first  int
	atEnd  bool
	follow bool
	// level is the filter: "" all, "warn" warnings and errors, "error"
	// errors only.
	level string
	// query is the search; typing while the line is open, hit the line
	// it found.
	query  string
	typing bool
	hit    int
	note   string
	// reading is set while a follow read is on its way.
	reading bool
	// rows is how many lines the log had the last drawing: a page.
	rows int
}

// logLine is one line of a log, made safe to show, with its level if it
// carries one.
type logLine struct {
	text  string
	level string
}

// The Logs tab's sizes: the chunk each read asks for, and the most lines
// it holds (following drops the oldest; scrolling up drops none).
const (
	logChunk    = 64 << 10
	logMaxLines = 20000
)

// logsKeys are the Logs tab's action row.
const logsKeys = "[ ] log · ↑ ↓ pgup pgdn scroll · f follow · / search · n N next · v level · o pager · c copy path · ← → tabs · esc close"

// logMsg is a follow read's answer.
type logMsg struct {
	id  string
	res proto.LogReadResult
	err error
}

// levelRE finds a line's level: level=warn, [ERROR], a WARN word, or
// error: and the like after the time.
var levelRE = regexp.MustCompile(`(?i)(?:\blevel=|\[)(error|err|warn|warning|info|debug)\b|(?:^|\s)(ERROR|WARN|WARNING|INFO|DEBUG)\b|(?:^|\s)(error|warning|warn|panic|fatal):`)

func lineLevel(s string) string {
	m := levelRE.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	switch strings.ToLower(m[1] + m[2] + m[3]) {
	case "error", "err", "panic", "fatal":
		return "error"
	case "warn", "warning":
		return "warn"
	case "info":
		return "info"
	}
	return "debug"
}

// splitLog is text's lines, made safe to show.
func splitLog(text string) []logLine {
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil
	}
	parts := strings.Split(text, "\n")
	out := make([]logLine, len(parts))
	for i, p := range parts {
		p = textsafe.Clean(strings.ReplaceAll(p, "\t", "    "), false)
		out[i] = logLine{text: p, level: lineLevel(p)}
	}
	return out
}

// load lists the logs and reads the one shown, its end.
func (lt *logsTab) load(m *dash) {
	id := lt.id()
	lt.logs, lt.err = m.src.Logs()
	lt.loaded = true
	lt.cur = 0
	for i, l := range lt.logs {
		if l.ID == id {
			lt.cur = i
		}
	}
	lt.open(m)
}

// id is the shown log's id, "" before the list.
func (lt *logsTab) id() string {
	if lt.cur < len(lt.logs) {
		return lt.logs[lt.cur].ID
	}
	return ""
}

// open reads the shown log's end, following it.
func (lt *logsTab) open(m *dash) {
	lt.lines, lt.start, lt.end, lt.size, lt.readErr = nil, 0, 0, 0, nil
	lt.first, lt.follow, lt.hit, lt.note, lt.reading = 0, true, -1, "", false
	id := lt.id()
	if id == "" {
		return
	}
	res, err := m.src.ReadLog(proto.LogReadParams{ID: id, Max: logChunk})
	if err != nil {
		lt.readErr = err
		return
	}
	lt.lines = splitLog(res.Text)
	lt.start, lt.end, lt.size = res.Start, res.End, res.Size
	lt.first = max(len(lt.lines)-1, 0)
}

// older reads the chunk before what the tab holds; false when it holds
// the file's start.
func (lt *logsTab) older(m *dash) bool {
	if lt.start <= 0 || lt.id() == "" {
		return false
	}
	res, err := m.src.ReadLog(proto.LogReadParams{ID: lt.id(), Before: lt.start, Max: logChunk})
	if err != nil {
		lt.note = "can't read more: " + oneLine(err.Error())
		return false
	}
	if res.Start >= lt.start {
		return false
	}
	more := splitLog(res.Text)
	lt.lines = append(more, lt.lines...)
	lt.first += len(more)
	if lt.hit >= 0 {
		lt.hit += len(more)
	}
	lt.start = res.Start
	return true
}

// poll asks for what follows the end, while the tab follows the log.
func (lt *logsTab) poll(m *dash) tea.Cmd {
	if !lt.follow || lt.reading || lt.id() == "" || lt.readErr != nil {
		return nil
	}
	lt.reading = true
	src, id, after := m.src, lt.id(), lt.end
	return func() tea.Msg {
		res, err := src.ReadLog(proto.LogReadParams{ID: id, After: &after, Max: logChunk})
		return logMsg{id: id, res: res, err: err}
	}
}

// took takes a follow read: the lines added, or the log anew when it
// was rotated.
func (lt *logsTab) took(msg logMsg) {
	lt.reading = false
	if msg.id != lt.id() {
		return
	}
	if msg.err != nil {
		lt.note = "can't follow it: " + oneLine(msg.err.Error())
		return
	}
	res := msg.res
	if res.Reset {
		lt.lines, lt.start, lt.hit = nil, res.Start, -1
		lt.note = "the log was rotated: it starts anew"
	}
	lt.lines = append(lt.lines, splitLog(res.Text)...)
	lt.end, lt.size = res.End, res.Size
	if n := len(lt.lines) - logMaxLines; n > 0 {
		// The oldest go; scrolling up reads them again.
		for _, l := range lt.lines[:n] {
			lt.start += int64(len(l.text)) + 1
		}
		lt.lines = lt.lines[n:]
		lt.first = max(lt.first-n, 0)
		if lt.hit >= 0 {
			lt.hit -= n
		}
	}
}

// hasLevels says whether any line held carries a level.
func (lt *logsTab) hasLevels() bool {
	for _, l := range lt.lines {
		if l.level != "" {
			return true
		}
	}
	return false
}

// shows says whether line i passes the level filter.
func (lt *logsTab) shows(i int) bool {
	switch lt.level {
	case "error":
		return lt.lines[i].level == "error"
	case "warn":
		return lt.lines[i].level == "error" || lt.lines[i].level == "warn"
	}
	return true
}

// step is the next shown line from i in direction d, -1 for none.
func (lt *logsTab) step(i, d int) int {
	for i += d; i >= 0 && i < len(lt.lines); i += d {
		if lt.shows(i) {
			return i
		}
	}
	return -1
}

// scroll moves the first line shown by n shown lines (up when n < 0),
// reading older chunks at the top.
func (lt *logsTab) scroll(m *dash, n int) {
	lt.follow = false
	for ; n < 0; n++ {
		j := lt.step(lt.first, -1)
		for j < 0 && lt.older(m) {
			j = lt.step(lt.first, -1)
		}
		if j < 0 {
			break
		}
		lt.first = j
	}
	for ; n > 0 && !lt.atEnd; n-- {
		j := lt.step(lt.first, 1)
		if j < 0 {
			break
		}
		lt.first = j
	}
}

// find puts the next match of the query in direction d (-1 older) from
// the hit, or from the lines shown, at the top; older chunks are read
// one at a time when none is held.
func (lt *logsTab) find(m *dash, d int) {
	if lt.query == "" {
		return
	}
	from := lt.hit
	if from < 0 {
		from = len(lt.lines)
		if d > 0 {
			from = lt.first - 1
		}
	}
	q := strings.ToLower(lt.query)
	for {
		for i := from + d; i >= 0 && i < len(lt.lines); i += d {
			if lt.shows(i) && strings.Contains(strings.ToLower(lt.lines[i].text), q) {
				lt.hit, lt.first, lt.follow, lt.note = i, i, false, ""
				return
			}
		}
		if d > 0 {
			lt.note = "no newer match"
			return
		}
		before := len(lt.lines)
		if !lt.older(m) {
			lt.note = "no older match: " + lt.query
			return
		}
		from = len(lt.lines) - before // the first line the older chunk didn't add
	}
}

// key handles the tab's keys; ok false for one it leaves to the popup.
func (lt *logsTab) key(m *dash, k tea.KeyPressMsg) (tea.Cmd, bool) {
	if lt.typing {
		switch k.String() {
		case "esc":
			lt.typing, lt.query = false, ""
		case "enter":
			lt.typing, lt.hit = false, -1
			lt.find(m, -1)
		case "backspace":
			if r := []rune(lt.query); len(r) > 0 {
				lt.query = string(r[:len(r)-1])
			}
		default:
			if t := k.Key().Text; t != "" {
				lt.query += textsafe.Clean(t, false)
			}
		}
		return nil, true
	}
	rows := max(lt.rows-1, 1)
	switch k.String() {
	case "[", "]":
		if n := len(lt.logs); n > 0 {
			d := map[string]int{"[": n - 1, "]": 1}[k.String()]
			lt.cur = (lt.cur + d) % n
			lt.open(m)
		}
	case "up":
		lt.scroll(m, -1)
	case "down":
		lt.scroll(m, 1)
	case "pgup":
		lt.scroll(m, -rows)
	case "pgdown", "space":
		lt.scroll(m, rows)
	case "home", "g":
		for lt.older(m) {
			if lt.start > 0 && len(lt.lines) >= logMaxLines {
				lt.note = "shows its last " + fmt.Sprint(len(lt.lines)) + " lines read; o opens the whole file"
				break
			}
		}
		lt.follow, lt.first = false, max(lt.step(-1, 1), 0)
	case "end", "G":
		lt.follow = true
	case "f":
		lt.follow = !lt.follow
		if lt.follow {
			return lt.poll(m), true
		}
	case "/":
		lt.typing, lt.query, lt.hit = true, "", -1
	case "n":
		lt.find(m, -1)
	case "N":
		lt.find(m, 1)
	case "v":
		if lt.hasLevels() || lt.level != "" {
			lt.level = map[string]string{"": "warn", "warn": "error", "error": ""}[lt.level]
			if lt.first < len(lt.lines) && !lt.shows(lt.first) {
				lt.follow = true
			}
		} else {
			lt.note = "its lines carry no level"
		}
	case "o":
		return lt.pager(m), true
	case "c":
		if l, ok := lt.shown(); ok {
			lt.note = "copied " + l.Path
			return tea.SetClipboard(l.Path), true
		}
	case "r":
		lt.load(m)
	default:
		return nil, false
	}
	return nil, true
}

// shown is the log shown; ok false with none.
func (lt *logsTab) shown() (proto.LogInfo, bool) {
	if lt.cur < len(lt.logs) {
		return lt.logs[lt.cur], true
	}
	return proto.LogInfo{}, false
}

// pager opens the shown log in $PAGER, when its file is on this host.
func (lt *logsTab) pager(m *dash) tea.Cmd {
	l, ok := lt.shown()
	if !ok {
		return nil
	}
	// Only an absolute path: one the pager could take for an option (less
	// runs a +!command) never reaches it.
	if fi, err := os.Stat(l.Path); err != nil || !fi.Mode().IsRegular() || !filepath.IsAbs(l.Path) {
		lt.note = "its file isn't on this host: c copies its path"
		return nil
	}
	if m.out == nil || m.out.File == nil {
		lt.note = "not available: the pager needs this console's terminal"
		return nil
	}
	argv := open.Pager(os.Getenv, l.Path)
	cmd := exec.Command(argv[0], argv[1:]...)
	m.out.flush()
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return actionMsg{err: fmt.Errorf("the pager (%s): %w", argv[0], err)}
		}
		return actionMsg{}
	})
}

// head is the tab's lines above the log: the logs to pick from, then
// the shown one's file, size and state, or the search line.
func (lt *logsTab) head(m *dash, w int) []string {
	if !lt.loaded {
		lt.load(m)
	}
	if lt.err != nil {
		return []string{th().Bad.Render("can't list the logs: " + oneLine(lt.err.Error()))}
	}
	var names []string
	for i, l := range lt.logs {
		name := oneLine(l.Name)
		if i == lt.cur {
			names = append(names, th().Sel.Render(" "+name+" "))
		} else {
			names = append(names, th().Faint.Render(" "+name+" "))
		}
	}
	out := wrapStyled(names, w)
	l, ok := lt.shown()
	if !ok {
		return out
	}
	var info []string
	switch {
	case l.Missing && len(lt.lines) == 0:
		info = append(info, "no file yet")
	default:
		info = append(info, sizeWords(max(lt.size, l.Size)))
	}
	if lt.follow {
		info = append(info, "following")
	} else {
		info = append(info, "paused (f follows)")
	}
	if lt.level != "" {
		info = append(info, map[string]string{"warn": "warnings and errors", "error": "errors only"}[lt.level])
	}
	if lt.query != "" && !lt.typing {
		info = append(info, "/"+lt.query)
	}
	// The path last, its start cut when it doesn't fit: its end names it.
	state := strings.Join(info, " · ") + " · "
	path := tildePath(l.Path)
	if room := w - ansi.StringWidth(state); ansi.StringWidth(path) > room {
		path = "…" + ansi.TruncateLeft(path, ansi.StringWidth(path)-max(room-1, 0), "")
	}
	out = append(out, th().Faint.Render(ansi.Truncate(state+path, w, "…")))
	switch {
	case lt.typing:
		out = append(out, "/"+lt.query+"▏")
	case lt.note != "":
		out = append(out, th().Warn.Render(ansi.Truncate(lt.note, w, "…")))
	default:
		out = append(out, "")
	}
	return out
}

// wrapStyled joins parts with a space into lines at most w wide.
func wrapStyled(parts []string, w int) []string {
	var out []string
	line, lw := "", 0
	for _, p := range parts {
		pw := ansi.StringWidth(p)
		if lw > 0 && lw+1+pw > w {
			out, line, lw = append(out, line), "", 0
		}
		if lw > 0 {
			line, lw = line+" ", lw+1
		}
		line, lw = line+p, lw+pw
	}
	return append(out, line)
}

// tildePath is path with the home folder written ~.
func tildePath(path string) string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		if rest, ok := strings.CutPrefix(path, h+string(os.PathSeparator)); ok {
			return "~" + string(os.PathSeparator) + rest
		}
	}
	return path
}

// body is the log's lines rows high and w wide, wrapped, from the first
// line shown (or, following, ending with the last); above and below
// say there are lines out of view.
func (lt *logsTab) body(w, rows int) (out []string, above, below bool) {
	lt.rows = rows
	if lt.readErr != nil {
		return []string{th().Bad.Render("can't read it: " + oneLine(lt.readErr.Error()))}, false, false
	}
	if len(lt.lines) == 0 {
		msg := "It is empty."
		if l, ok := lt.shown(); ok && l.Missing {
			msg = "No file yet: nothing was logged here."
			if l.ID == proto.LogServer {
				msg = "No file yet: a server run in the foreground (tm server run, as the systemd service runs it) logs to its terminal or the journal (journalctl --user -u terminatr)."
			}
		}
		return faintLines(msg, w), false, false
	}
	if lt.follow {
		// From the end up, as many as fit.
		n, i := 0, len(lt.lines)
		for {
			j := lt.step(i, -1)
			if j < 0 {
				break
			}
			h := len(lt.draw(j, w))
			if n+h > rows && n > 0 {
				break
			}
			n, i = n+h, j
		}
		lt.first = min(i, len(lt.lines)-1)
	}
	if !lt.shows(lt.first) {
		if j := lt.step(lt.first, 1); j >= 0 {
			lt.first = j
		} else if j := lt.step(lt.first, -1); j >= 0 {
			lt.first = j
		}
	}
	above = lt.step(lt.first, -1) >= 0 || lt.start > 0
	i := lt.first
	for i >= 0 && i < len(lt.lines) && len(out) < rows {
		if lt.shows(i) {
			out = append(out, lt.draw(i, w)...)
		}
		i++
	}
	if len(out) > rows {
		out = out[:rows]
	}
	below = lt.step(i-1, 1) >= 0
	lt.atEnd = !below
	if len(out) == 0 {
		out = faintLines("No line of this level in what is read: v shows all.", w)
	}
	return out, above, below
}

// draw is line i, wrapped w wide, in its level's colour, the search's
// matches marked.
func (lt *logsTab) draw(i, w int) []string {
	l := lt.lines[i]
	parts := strings.Split(ansi.Hardwrap(l.text, max(w, 10), false), "\n")
	style := th().Plain
	switch l.level {
	case "error":
		style = th().Bad
	case "warn":
		style = th().Warn
	case "debug":
		style = th().Faint
	}
	for j, p := range parts {
		parts[j] = lt.mark(p, style, i == lt.hit)
	}
	return parts
}

// mark styles p, its matches of the query reversed (bold on the hit).
func (lt *logsTab) mark(p string, style lipgloss.Style, hit bool) string {
	q := lt.query
	if q == "" || lt.typing {
		return style.Render(p)
	}
	low, lq := strings.ToLower(p), strings.ToLower(q)
	if len(low) != len(p) || len(lq) != len(q) {
		return style.Render(p) // a case change that moves bytes: unmarked
	}
	var b strings.Builder
	at := 0
	for {
		k := strings.Index(low[at:], lq)
		if k < 0 {
			break
		}
		b.WriteString(style.Render(p[at : at+k]))
		m := th().Sel
		if hit {
			m = m.Bold(true)
		}
		b.WriteString(m.Render(p[at+k : at+k+len(q)]))
		at += k + len(q)
	}
	b.WriteString(style.Render(p[at:]))
	return b.String()
}

// logsTick reads what the shown log added, on the dashboard's tick,
// while the , popup is on its Logs tab and follows it.
func (m *dash) logsTick() tea.Cmd {
	if len(m.stack) == 0 {
		return nil
	}
	sv, ok := m.stack[len(m.stack)-1].(*settingsView)
	if !ok || sv.tab != tabLogs {
		return nil
	}
	return sv.logs.poll(m)
}

// logsTook takes a follow read into the , popup's Logs tab, if open.
func (m *dash) logsTook(msg logMsg) {
	for _, o := range m.stack {
		if sv, ok := o.(*settingsView); ok {
			sv.logs.took(msg)
		}
	}
}

// renderLogs draws the , popup on its Logs tab: the tab bar and the
// About line, the tab's head, and the log as high as the popup has room.
func (sv *settingsView) renderLogs(m *dash, w int) string {
	lt := &sv.logs
	head := append([]string{sv.tabBar()}, aboutLines(m, w)...)
	head = append(head, "")
	head = append(head, lt.head(m, w)...)
	hits := []int{tabHit}
	for range len(head) - 1 {
		hits = append(hits, noHit)
	}
	b := box{title: "Settings", head: head, sel: -1, hits: hits, keys: logsKeys}
	if lt.typing {
		b.keys = "enter search · esc cancel"
	}
	rows := max(m.boxRows(b)-len(head), 1)
	body, above, below := lt.body(w, rows)
	if above {
		body, b.scroll = append([]string{""}, body...), 1
	}
	if below {
		for len(body)-b.scroll < rows {
			body = append(body, "")
		}
		body = append(body, "")
	}
	b.body = body
	return m.popup(b)
}
