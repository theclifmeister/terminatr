package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Overlays: the views opened on top of the list (help, a prompt, the
// project switcher, the settings, the project popup, a task).
// Each keeps its own state and draws as a popup (popup.go); keys go to the topmost,
// and closing it returns to the one below, or to the list.
type overlay interface {
	key(m *dash, k tea.KeyPressMsg) tea.Cmd
	render(m *dash) string
}

func (m *dash) push(o overlay) { m.stack = append(m.stack, o) }

// pop closes the topmost overlay.
func (m *dash) pop() {
	if len(m.stack) > 0 {
		m.stack = m.stack[:len(m.stack)-1]
	}
}

// close removes o wherever it is in the stack.
func (m *dash) close(o overlay) {
	for i := len(m.stack) - 1; i >= 0; i-- {
		if m.stack[i] == o {
			m.stack = append(m.stack[:i], m.stack[i+1:]...)
			return
		}
	}
}

func (m *dash) top() overlay {
	if len(m.stack) == 0 {
		return nil
	}
	return m.stack[len(m.stack)-1]
}

// moveSel moves a list selection by d within n items.
func moveSel(sel, d, n int) int { return min(max(sel+d, 0), max(n-1, 0)) }

// helpView lists the keys (keymap.go) as wide as the window; the arrows
// scroll, esc closes it.
type helpView struct{ scroll int }

func (h *helpView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	if scrollKey(&h.scroll, k, len(m.helpBody())) {
		return nil
	}
	if k.String() == "esc" {
		m.pop()
	}
	return nil
}

func (h *helpView) box(m *dash) box {
	return box{title: "Help", body: m.helpBody(), sel: -1, scroll: h.scroll,
		keys: "↑ ↓ scroll · esc close"}
}

func (h *helpView) render(m *dash) string { return m.popup(h.box(m)) }

// helpBody is the help's lines: the version tm and its server run first
// (versionTitle), then the keys and symbols.
func (m *dash) helpBody() []string {
	lines := helpLines(m.prefix, m.inner(viewWidth), m.commandHelp())
	if v := versionTitle(m.data.Server); v != "" {
		lines = append([]string{th().Faint.Render(v), ""}, lines...)
	}
	return lines
}

func (h *helpView) wheel(m *dash, d int) { h.key(m, arrow(d)) }

// scrollKeys scroll a long popup, or move a list's selection: by a line,
// or by a page.
var scrollKeys = map[string]int{"up": -1, "down": 1, "pgup": -10, "pgdown": 10}

func clampScroll(s, n int) int { return min(max(s, 0), max(n-1, 0)) }

// inputView reads a line of text: its title names what for, its label
// asks for it. Its text starts as what it edits, the cursor at its end,
// and the line-editing keys (lineEdit) work in it.
type inputView struct {
	title, label string
	lineEdit
	// placeholder shows faint while the text is empty, an example or the
	// default an empty one means; it is never the text.
	placeholder string
	submit      func(string) tea.Cmd
	// cancel is the footer message on esc or an empty line; max is the
	// most runes it takes, 0 for no limit.
	cancel string
	max    int
	// keep submits the text as typed, an empty one too (a text
	// setting), instead of trimmed with empty as a cancel.
	keep bool
}

func (m *dash) prompt(title, label, initial string, submit func(string) tea.Cmd) {
	m.push(&inputView{title: title, label: label, lineEdit: newLine(initial), submit: submit})
}

// promptNo is prompt with an empty start, saying cancel in the footer
// when it is cancelled, and taking at most max runes; placeholder shows
// while it is empty.
func (m *dash) promptNo(title, label, placeholder, cancel string, max int, submit func(string) tea.Cmd) {
	m.push(&inputView{title: title, label: label, placeholder: placeholder, submit: submit, cancel: cancel, max: max})
}

func (in *inputView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.pop()
		in.cancelled(m)
	case "enter":
		m.pop()
		if in.keep {
			return in.submit(in.value())
		}
		if t := strings.TrimSpace(in.value()); t != "" {
			return in.submit(t)
		}
		in.cancelled(m)
	case "ctrl+u":
		// It clears the line, as it always has, wherever the cursor is.
		in.lineEdit = lineEdit{}
	default:
		if !in.edit(k.String()) {
			in.paste(m, k.Text)
		}
	}
	return nil
}

// paste puts text typed or pasted in at the cursor, on one line, as far
// as max allows.
func (in *inputView) paste(_ *dash, text string) {
	r := []rune(oneLine(strings.TrimRight(text, "\r\n")))
	if in.max > 0 {
		r = r[:min(len(r), max(in.max-len(in.text), 0))]
	}
	in.insert(r)
}

func (in *inputView) cancelled(m *dash) {
	if in.cancel != "" {
		m.msg = in.cancel
	}
}

func (in *inputView) render(m *dash) string {
	// The label above the field; a long text wraps onto more lines, so
	// all of it shows, the popup keeping the cursor's line in view.
	w := m.inner(dialogWidth)
	body := wrapLines(in.label, w)
	text, cur := in.lines(w)
	if len(in.text) == 0 && in.placeholder != "" {
		text = []string{"█" + th().Faint.Render(ansi.Truncate(in.placeholder, max(w-1, 0), "…"))}
	}
	sel := len(body) + cur
	body = append(body, text...)
	if n := len(in.text); in.max > 0 && n*10 >= in.max*8 {
		// Near the limit: how much is left.
		body = append(body, th().Faint.Render(fmt.Sprintf("%d of at most %d characters", n, in.max)))
	}
	return m.popup(box{title: in.title, body: body, sel: sel, keys: "enter ok · ← → move · ctrl+u clear · esc cancel", dialog: true})
}

// wrapInput lays out a prompt's label and text in lines of w cells,
// breaking anywhere (a path has no spaces to break at); the label is in
// the accent colour.
func wrapInput(label, text string, w int) []string {
	r := []rune(label + text)
	var lines []string
	for len(r) > 0 {
		n := min(len(r), max(w, 1))
		lines = append(lines, string(r[:n]))
		r = r[n:]
	}
	if l := len([]rune(label)); l <= len([]rune(lines[0])) {
		first := []rune(lines[0])
		lines[0] = th().Accent.Render(label) + string(first[l:])
	}
	return lines
}

// joinKeys joins key lists, skipping empty ones.
func joinKeys(keys ...string) string {
	var out []string
	for _, k := range keys {
		if k != "" {
			out = append(out, k)
		}
	}
	return strings.Join(out, " · ")
}
