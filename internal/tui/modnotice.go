package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/modnotify"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// Mod notifications (mods design §7, notify): the server keeps what user
// mods sent with $.ui.notify (session.list's ModNotices). The console
// draws each as an OS notification, once over all consoles (notify.claim),
// titled "terminatr · id", and N lists them. A button is a core
// action id on a task, pressed as an action binding (pressNotice): the
// core's reach check, precondition check on the live board and
// confirmation come first. The mod hears of nothing, and its words never
// name an action or a label.

// noticeStable is how long a notification stays unchanged before a button
// press counts (mods design §11, "no bait and switch").
const noticeStable = 300 * time.Millisecond

// noticeSource is a Source that can claim and dismiss notifications (the
// server's); the tests' fakes need not.
type noticeSource interface {
	ClaimNotice(id string) bool
	DismissNotice(id string)
}

func (s *ServerSource) ClaimNotice(id string) bool {
	var res proto.NoticeResult
	return s.call(proto.MethodNotifyClaim, proto.NoticeParams{ID: id}, &res) == nil && res.OK
}

func (s *ServerSource) DismissNotice(id string) {
	var res proto.NoticeResult
	_ = s.call(proto.MethodNotifyDismiss, proto.NoticeParams{ID: id}, &res)
}

// OSNotice is the escape sequence that has the terminal show an OS
// notification for n: OSC 9 in iTerm2, OSC 777 in the others that have
// one (Ghostty, WezTerm, kitty, foot). The title is always "terminatr ·
// id" and the text n's title and text, without anything the terminal
// would read as more than text.
func OSNotice(termProgram string, n modnotify.Notice) string {
	clean := func(s string) string {
		return strings.ReplaceAll(modnotify.Line(s, modnotify.MaxText+modnotify.MaxTitle), ";", ",")
	}
	title := "terminatr · " + clean(n.Mod)
	body := clean(n.Title)
	if n.Text != "" {
		body += ": " + clean(n.Text)
	}
	if termProgram == "iTerm.app" {
		return "\x1b]9;" + title + " — " + body + "\a"
	}
	return "\x1b]777;notify;" + title + ";" + body + "\a"
}

// drawNotices claims the notifications this console has not seen and
// the server has not had drawn, and draws the ones it wins; the footer
// says the first new one.
func (m *dash) drawNotices() []tea.Cmd {
	src, _ := m.src.(noticeSource)
	var cmds []tea.Cmd
	var fresh []modnotify.Notice
	live := map[string]bool{}
	for _, n := range m.data.ModNotices {
		live[n.ID] = true
		if m.noticed[n.ID] {
			continue
		}
		if m.noticed == nil {
			m.noticed = map[string]bool{}
		}
		m.noticed[n.ID] = true
		fresh = append(fresh, n)
		if src != nil && !n.Drawn {
			prog := os.Getenv("TERM_PROGRAM")
			cmds = append(cmds, func() tea.Msg {
				if src.ClaimNotice(n.ID) {
					return tea.Raw(OSNotice(prog, n))()
				}
				return nil
			})
		}
	}
	for id := range m.noticed {
		if !live[id] {
			delete(m.noticed, id)
		}
	}
	if len(fresh) > 0 && m.seen {
		n := fresh[0]
		m.msg = "mod " + n.Mod + ": " + oneLine(n.Title) + " · N to see"
		if len(fresh) > 1 {
			m.msg = fmt.Sprintf("%d notifications from mods · N to see", len(fresh))
		}
	}
	return cmds
}

// pressNotice presses button b of mod's notification: a binding like a
// mod's row's (binding.go), so the server checks it is in the mod's reach,
// the console re-checks it on the live board, the core asks, and the mod
// hears action.done. The notices view has kept the press waiting
// noticeStable already.
func (m *dash) pressNotice(mod string, b modnotify.Button) tea.Cmd {
	return m.pressBinding(mod, &widget.Event{Kind: widget.EventAction, Action: &widget.Action{ID: b.ID, Target: b.Target()}, Stable: true})
}

// noticesView lists the notifications of mods.
type noticesView struct {
	sel int
	// ids are the notifications as last drawn, and stableAt when that
	// list or the selection last changed: a press before noticeStable
	// after it is ignored, so a list that changes under the cursor can't
	// take a press meant for another.
	ids      string
	stableAt time.Time
	now      func() time.Time
}

func (m *dash) openNotices() {
	nv := &noticesView{now: time.Now}
	nv.stableAt = nv.now()
	nv.ids = noticeIDs(m.data.ModNotices)
	m.push(nv)
}

func noticeIDs(ns []modnotify.Notice) string {
	var b strings.Builder
	for _, n := range ns {
		b.WriteString(n.ID + ",")
	}
	return b.String()
}

func (nv *noticesView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	ns := m.data.ModNotices
	if ids := noticeIDs(ns); ids != nv.ids {
		nv.ids, nv.stableAt = ids, nv.now()
	}
	if len(ns) == 0 {
		m.pop()
		return nil
	}
	nv.sel = min(max(nv.sel, 0), len(ns)-1)
	n := ns[nv.sel]
	switch s := k.String(); s {
	case "esc", "q":
		m.pop()
	case "up", "k":
		nv.sel, nv.stableAt = max(nv.sel-1, 0), nv.now()
	case "down", "j":
		nv.sel, nv.stableAt = min(nv.sel+1, len(ns)-1), nv.now()
	case "d":
		return m.dismissNotice(n)
	case "1", "2", "3":
		i := int(s[0] - '1')
		if i >= len(n.Buttons) {
			return nil
		}
		if nv.now().Sub(nv.stableAt) < noticeStable {
			m.msg = "the notification just changed; press again"
			return nil
		}
		m.pop()
		cmd := m.dismissNotice(n)
		return tea.Batch(cmd, m.pressNotice(n.Mod, n.Buttons[i]))
	}
	return nil
}

// dismissNotice takes n out of the tray now and on the server.
func (m *dash) dismissNotice(n modnotify.Notice) tea.Cmd {
	for i, x := range m.data.ModNotices {
		if x.ID == n.ID {
			m.data.ModNotices = append(m.data.ModNotices[:i:i], m.data.ModNotices[i+1:]...)
			break
		}
	}
	src, _ := m.src.(noticeSource)
	if src == nil {
		return nil
	}
	return func() tea.Msg { src.DismissNotice(n.ID); return nil }
}

func (nv *noticesView) render(m *dash) string {
	ns := m.data.ModNotices
	w := m.inner(viewWidth)
	var body []string
	sel := -1
	for i, n := range ns {
		if i == nv.sel {
			sel = len(body)
		}
		mark := th().Faint.Render(n.Mod)
		title := oneLine(n.Title)
		switch n.Level {
		case "warn":
			title = th().Warn.Render(title)
		case "error":
			title = th().Bad.Render(title)
		}
		body = append(body, fit(" ", 1)+title+"  "+mark)
		if n.Text != "" {
			for _, l := range wrapLines(oneLine(n.Text), w-2) {
				body = append(body, "  "+l)
			}
		}
		var bs []string
		for j, b := range n.Buttons {
			if b.Session != "" {
				bs = append(bs, fmt.Sprintf("%d %s %q", j+1, b.Label, oneLine(b.Option)))
				continue
			}
			bs = append(bs, fmt.Sprintf("%d %s T%d", j+1, b.Label, b.Task))
		}
		if len(bs) > 0 {
			body = append(body, "  "+th().Accent.Render(strings.Join(bs, " · ")))
		}
		body = append(body, "")
	}
	if len(ns) == 0 {
		body = []string{th().Faint.Render("no notifications")}
	}
	keys := "↑↓ select · d dismiss · esc close"
	if len(ns) > 0 && len(ns[min(nv.sel, len(ns)-1)].Buttons) > 0 {
		keys = "1 2 3 button (asks first) · " + keys
	}
	return m.popup(box{title: "Notifications from mods", body: body, sel: sel, keys: keys})
}

// claimNotices claims the notifications in the tray that this console has
// not seen and no console has drawn, and returns the sequences that draw
// the ones it won; noticed is its memory, pruned to the tray.
func claimNotices(ctl *server.Client, ns []modnotify.Notice, noticed map[string]bool) []byte {
	var out []byte
	live := map[string]bool{}
	for _, n := range ns {
		live[n.ID] = true
		if noticed[n.ID] {
			continue
		}
		noticed[n.ID] = true
		var res proto.NoticeResult
		if n.Drawn || ctl.Call(proto.MethodNotifyClaim, proto.NoticeParams{ID: n.ID}, &res) != nil || !res.OK {
			continue
		}
		out = append(out, OSNotice(os.Getenv("TERM_PROGRAM"), n)...)
	}
	for id := range noticed {
		if !live[id] {
			delete(noticed, id)
		}
	}
	return out
}
