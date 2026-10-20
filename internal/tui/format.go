package tui

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// Shared text for the dashboard rows and the attach status bar.

// stateWord is a session's state, "running" for a plain program.
func stateWord(s proto.SessionInfo) string {
	switch {
	case s.State != "":
		return s.State
	case s.Agent != "":
		return "starting"
	}
	return "running"
}

// blockReason is why a blocked session is blocked; "question" says
// "question open" while the server holds the menu (SessionInfo.Question).
func blockReason(s proto.SessionInfo) string {
	if s.Reason == "question" && s.Question != nil {
		return "question open"
	}
	return s.Reason
}

// sessionState is a session's state as its panels head it: its word
// (stateWord), and why it is blocked when it says.
func sessionState(s proto.SessionInfo) string {
	state := stateWord(s)
	if s.State == "blocked" && s.Reason != "" {
		state += " " + blockReason(s)
	}
	return state
}

// nowWords are what a thread's STATUS.md says it does now: its current
// step (▸), else its activity, quoted; "" for neither.
func nowWords(st *thread.Status) string {
	switch {
	case st.Current != "":
		return "▸ " + oneLine(st.Current)
	case st.Activity != "":
		return `"` + oneLine(st.Activity) + `"`
	}
	return ""
}

// stepLine is a task's plan step as the panels and the rows list it:
// its box, number and text.
func stepLine(s tasks.Step) string {
	return fmt.Sprintf("%s %d %s", todoGlyph(stepStatus(s.Done)), s.N, oneLine(s.Text))
}

// stepsProgress is a task's steps as a progress bar (progressLine).
func stepsProgress(t *tasks.Task) string {
	return progressLine(thread.Progress{Percent: pctOf(t.StepsDone(), len(t.Steps)), Done: t.StepsDone(), Total: len(t.Steps)})
}

// questionLines are a session's open question menu, as tm thread show
// prints it: each question with its header, then its options by number,
// the last one the menu's free text. Each line is plain; indent says how
// far the options sit in.
func questionLines(q *proto.Question) []string {
	var out []string
	for i, it := range q.Questions {
		l := fmt.Sprintf("%d. %s", i+1, oneLine(it.Question))
		if it.Header != "" {
			l = fmt.Sprintf("%d. [%s] %s", i+1, oneLine(it.Header), oneLine(it.Question))
		}
		if it.MultiSelect {
			l += " (several)"
		}
		if it.Answered {
			out = append(out, l+" → answered: "+oneLine(it.Answer))
			continue
		}
		out = append(out, l)
		for j, o := range it.Options {
			opt := fmt.Sprintf("   %d. %s", j+1, oneLine(o.Label))
			if o.Description != "" {
				opt += " — " + oneLine(o.Description)
			}
			out = append(out, opt)
		}
		out = append(out, fmt.Sprintf("   %d. (the user's own words)", len(it.Options)+1))
	}
	return out
}

// progressOnly is progress without the block's reason, which rows show
// on their own.
func progressOnly(s proto.SessionInfo) string {
	s.Reason = ""
	return progress(s, nil)
}

// progress is the derived percent, done/total and the current todo
// (docs/SPEC.md §7.3): "40% 2/5 ▸ Write SPEC §8", or the block reason.
// For a thread's session, st is its STATUS.md, which the dashboard shows
// too; without it, the session's own todos count.
func progress(s proto.SessionInfo, st *thread.Status) string {
	var parts []string
	if s.State == "blocked" && s.Reason != "" {
		parts = append(parts, s.Reason)
	}
	switch {
	case st != nil:
		if p := threadProgress(st); p != "" {
			parts = append(parts, p)
		}
	default:
		if p := sessionProgress(s).String(); p != "" {
			parts = append(parts, p)
		}
		if s.Current != "" {
			parts = append(parts, "▸ "+oneLine(s.Current))
		}
	}
	if s.Queued > 0 {
		q := fmt.Sprintf("%d queued", s.Queued)
		if s.QueueNote(time.Now()) != "" {
			q += " (held)"
		}
		parts = append(parts, q)
	}
	return strings.Join(parts, " ")
}

// sessionName is what a row calls a session: its role in its project,
// the agent, or the command.
func sessionName(s proto.SessionInfo) string {
	switch {
	case s.Role == proto.RoleCoordinator:
		return "coordinator"
	case s.Role == proto.RoleThread && s.Thread != "":
		return threadName(s.Task, s.Thread)
	case s.Agent != "":
		return s.Agent
	case s.Title != "":
		return oneLine(s.Title)
	case len(s.Argv) > 0:
		return strings.Join(s.Argv, " ")
	}
	return "shell"
}

// threadName is how the UI names a thread (docs/SPEC.md §4): by its
// task's id ("T12"), which is what the user and the coordinator talk
// in; a thread without a task (ad hoc, adopted) by its own id. The
// thread id shows in the details: the info panel, tm thread show.
func threadName(task, id string) string {
	if task != "" {
		return task
	}
	return id
}

// age is a short duration: "4s", "3m", "2h", "5d".
func age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// oneLine is s without what textsafe drops (control and format
// characters), so text from agents and peers can't break a row or reach
// the terminal as anything but text.
func oneLine(s string) string { return textsafe.Clean(s, false) }

// sgrRE is a colour or style sequence, the one escape sequence a row
// may carry.
var sgrRE = regexp.MustCompile(`^\x1b\[[0-9;:]*m$`)

// plainStyled is s as text and the colour and style sequences lipgloss
// writes, nothing else: any other escape sequence goes whole (its
// payload too), and so do the characters textsafe drops.
func plainStyled(s string) string {
	if textsafe.Safe(s) {
		return s
	}
	var b strings.Builder
	var state byte
	for len(s) > 0 {
		seq, width, n, next := ansi.DecodeSequence(s, state, nil)
		if n == 0 {
			break
		}
		switch {
		case width > 0:
			b.WriteString(textsafe.Clean(seq, false))
		case sgrRE.MatchString(seq):
			b.WriteString(seq)
		case seq == "\t" || seq == "\n" || seq == "\r":
			b.WriteByte(' ')
		}
		s, state = s[n:], next
	}
	return b.String()
}

// fit truncates or pads text to exactly w cells; whatever isn't text or
// a style (plainStyled) goes, so a row can't carry it to the terminal.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(plainStyled(s), w, "…")
	if n := ansi.StringWidth(s); n < w {
		s += strings.Repeat(" ", w-n)
	}
	return s
}

// pendingHead starts the status bar's list of commands after the prefix.
const pendingHead = " prefix ▸ "

// pendingList are the prefix commands, as the status bar lists them
// after the prefix, in the bound keys (keybind.go).
func pendingList() string {
	return "d dashboard · q quit · " + boundKey("a") + " project · " + boundKey("n") + " new · p " + boundKeys("]", "[") +
		" projects · " + boundKeys("i", "t", ",", "?") + " · " + boundKeys("{", "}", "b") + " sidebar · " + boundKey("|") +
		" info · tab sidebar keys · " + boundKey(";") + " last place · r remote control · " + boundKey("R") + " repaint · C save+clear"
}

// The status bar's notes while the sidebar's cursor previews a row:
// waiting for it to rest (previewDelay), then showing it; previewShort
// is what previewNote shrinks to.
const (
	previewWait  = "preview…"
	previewNote  = "preview · ⏎ to type here"
	previewShort = "preview"
)

// pathMin is what the status bar keeps of project › pane before it cuts
// the note after it.
const pathMin = 20

// statusRight is the status bar's right end: the global keys.
func statusRight() string {
	return "prefix+" + boundKey("?") + " help · prefix+d dashboard · " + menuButton + " menu "
}

// statusBar is the attach status bar (docs/SPEC.md §4): under a faint
// rule (the caller's), with no fill and muted. Left, where you are: the
// session's state glyph, project › pane (a thread's task and title, the
// coordinator, else the session's id and command), its progress (the
// percent, a bar, the step) and the rest worth a glance (why it is
// blocked, queued prompts, remote control, a model on next start), then
// where: what the keyboard does away from the pane (the sidebar's
// preview note, the info panel's keys) or a note in passing; then, when
// room is left, the mods' status segments ($.ui.status.set). Right, the
// global keys: prefix+? help · prefix+d dashboard · ≡ menu. After the
// prefix it lists the commands instead, and "prefix again sends it".
//
// When the left side doesn't fit, the step and the rest give way first,
// then the bar, then the preview note shrinks to "preview", then the
// title is cut; the percent stays. Hints never show the prefix's key,
// which is configurable: only the help and the settings do. It returns
// the line, cols cells wide, and its buttons, by column: after the
// prefix each command; else ?, d and the ≡ menu.
func statusBar(s proto.SessionInfo, ts *thread.Status, title string, pending bool, cols int, where string, mods *modCells) (string, []hint) {
	if pending {
		right := "prefix again sends it "
		w := cols - ansi.StringWidth(right) - 1
		left := th().Faint.Render(pendingHead) + keysLine(pendingList())
		if w < 1 {
			return fit(left, cols), nil
		}
		hs := hints(pendingList(), ansi.StringWidth(pendingHead))
		hs = slices.DeleteFunc(hs, func(h hint) bool { return h.x1 > w-1 })
		hs = append(hs, hint{w + 1, w + 1 + 21, "prefix"})
		return fit(left, w) + reset + " " + th().Faint.Render(right), hs
	}
	right := keysLine(strings.TrimSuffix(statusRight(), " ")) + " "
	rw := ansi.StringWidth(statusRight())
	g, gst := stateLook(stateWord(s))
	if g == "" {
		g, gst = ic().none, th().Faint
	}
	path := s.ID + " › " + sessionName(s)
	if s.Project != "" {
		pane := sessionName(s)
		if s.Role == proto.RoleThread && title != "" {
			pane += " " + oneLine(title)
		}
		path = s.Project + " › " + pane
	}
	if s.Server != "" {
		// A paired server's pane (docs/SPEC.md §16.12).
		path = s.Server + " › " + path
	}
	path = oneLine(path) // names a paired server may choose
	// The progress, from the most to the least: the percent always
	// stays.
	pr := sessionProgress(s)
	cur := s.Current
	if ts != nil {
		pr, cur = ts.Progress(), ts.Current
	}
	var pct, step string
	var rest []string
	if pr.Percent >= 0 {
		pct = fmt.Sprintf("%d%%", pr.Percent)
	} else if c := s.ContextPercent(); s.Role == proto.RoleCoordinator && c >= 0 {
		pct = fmt.Sprintf("context %d%%", c)
	}
	if pr.Total > 0 {
		step = fmt.Sprintf("%d/%d", pr.Done, pr.Total)
	}
	if cur != "" {
		step = strings.TrimSpace(step + " ▸ " + oneLine(cur))
	}
	if s.State == "blocked" && s.Reason != "" {
		rest = append(rest, oneLine(blockReason(s)))
	}
	if s.Queued > 0 {
		q := fmt.Sprintf("%d queued", s.Queued)
		if s.QueueNote(time.Now()) != "" {
			q += " (held)"
		}
		rest = append(rest, q)
	}
	if s.RemoteControl {
		rest = append(rest, "remote control on")
	}
	if s.ModelPending {
		// The coordinator's agent can't switch models live (§8.2).
		rest = append(rest, modelWord(s.ModelNext)+" on next start")
	}
	if s.HooksSkipped != "" {
		// Its context comes through the command hooks instead (§7.8).
		rest = append(rest, "terminatr hooks skipped by "+s.HooksSkipped)
	}
	join := func(parts ...string) string {
		var out []string
		for _, p := range parts {
			if p != "" {
				out = append(out, p)
			}
		}
		return strings.Join(out, " ")
	}
	barW := ""
	if pr.Percent >= 0 {
		barW = bar(pr.Percent)
	}
	progs := []string{join(pct, barW, step, strings.Join(rest, " · ")), join(pct, barW), pct}
	wheres := []string{where}
	if where == previewNote {
		wheres = append(wheres, previewShort)
	}
	tail := func(pg, wh string) string {
		switch {
		case pg == "":
			return wh
		case wh == "":
			return pg
		}
		return pg + " · " + wh
	}
	room := cols - rw - 2 - 3 // a gap of two, the margin and the glyph
	t := ""
	fits := false
	for _, wh := range wheres {
		for _, pg := range progs {
			t = tail(pg, wh)
			if ansi.StringWidth(path)+2+ansi.StringWidth(t) <= room {
				fits = true
				break
			}
		}
		if fits {
			break
		}
	}
	if !fits {
		// The title gives way, down to pathMin cells; then the tail is
		// cut (a long note in passing).
		t = tail(pct, wheres[len(wheres)-1])
		pw := max(room-2-ansi.StringWidth(t), min(ansi.StringWidth(path), pathMin))
		path = strings.TrimRight(fit(path, max(min(pw, room), 0)), " ")
		t = strings.TrimRight(fit(t, max(room-2-ansi.StringWidth(path), 0)), " ")
	}
	left := " " + gst.Render(g) + " " + th().Head.Render(path)
	if t != "" {
		left += "  " + th().Faint.Render(t)
	}
	w := cols - rw
	if w < 1 {
		return fit(left, cols), nil
	}
	// The mods' segments (statusMods) show where there is room left,
	// after everything else; they give way first.
	if seg, sw := statusMods(mods); sw > 0 && ansi.StringWidth(left)+2+sw <= w-2 {
		left += "  " + seg
	}
	// The buttons follow the text, whatever help.open is bound to.
	help := ansi.StringWidth("prefix+" + boundKey("?") + " help")
	dash := ansi.StringWidth("prefix+d dashboard")
	menu := ansi.StringWidth(menuButton + " menu")
	hs := []hint{{w, w + help, "?"}, {w + help + 3, w + help + 3 + dash, "d"}, {w + help + dash + 6, w + help + dash + 6 + menu, "menu"}}
	return fit(left, w) + reset + right, hs
}

// sessionProgress is a session's progress from its own todos.
func sessionProgress(s proto.SessionInfo) thread.Progress {
	return thread.SessionProgress(s.TodosDone, s.TodosTotal)
}

// threadProgress is a thread's line from STATUS.md (docs/SPEC.md §7.3):
// "60% 3/5 ▸ current", the self-reported activity only without steps or
// todos.
func threadProgress(st *thread.Status) string {
	if st == nil {
		return ""
	}
	var parts []string
	if p := st.Progress().String(); p != "" {
		parts = append(parts, p)
	}
	if now := nowWords(st); now != "" {
		parts = append(parts, now)
	}
	return strings.Join(parts, " ")
}

// modelWord is a model's name, or the agent's own default.
func modelWord(m string) string {
	if m == "" {
		return "agent's default model"
	}
	return "model " + m
}
