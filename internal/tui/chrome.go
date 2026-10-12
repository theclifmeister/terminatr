package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// The dashboard's header and footer by the layout's style (the layout v2
// design §2.2, §2.4.3): the header a row or none; the footer a rule, the
// keys and the message, one row (the message while there is one, else the
// keys), or none. What a hidden one carries still shows, over the body:
// the server's alarms and the needs-you badge over its first row at the
// right, a message over its last row at the left, while they last. The
// geometry never changes with them, so nothing jumps.

// headRight is the header's right part: the server's state, after the
// needs-you badge while the sidebar is hidden. alarm is what of it shows
// with the header hidden: the badge and the server's state unless it is
// fine; badge is the badge, "" for none.
func (m *dash) headRight() (right, alarm, badge string) {
	switch {
	case !m.loaded:
		alarm = th().Faint.Render("◌ loading…")
	case !m.data.ServerOK:
		alarm = th().Bad.Render("▲ server down: " + oneLine(m.data.Err))
	case m.data.NoAgent:
		alarm = th().Bad.Render("▲ no agent installed: nothing can start (tm doctor)")
	default:
		n := len(m.data.Sessions)
		right = th().Good.Render(ic().working) + " server ok" + th().Faint.Render(fmt.Sprintf(" · %d session%s", n, map[bool]string{true: "s"}[n != 1]))
	}
	if alarm != "" {
		right = alarm
	}
	// With the sidebar hidden, what needs you is counted before the
	// server's state: a click on it goes there.
	if m.sideW() == 0 {
		badge = needsBadge(m.needsCount())
	}
	if badge != "" {
		right = badge + " " + right
		alarm = strings.TrimSuffix(badge+" "+alarm, " ")
	}
	return right, alarm, badge
}

// footMsg is the footer's message, styled: working…, an error, a note;
// "" for none.
func (m *dash) footMsg() string {
	switch {
	case m.busy:
		return th().Faint.Render(" working…")
	case m.msg == "":
		return ""
	case m.msg == m.errMsg:
		return th().Bad.Render(" " + oneLine(m.msg))
	}
	return " " + oneLine(m.msg)
}

// footLines are the footer's rows with keys as its key hints, by its
// style, and records the hints drawn (shownKeys: their buttons).
func (m *dash) footLines(g dashGeo, keys string) []string {
	msg := m.footMsg()
	m.shownKeys = keys
	switch g.footRows() {
	case 0:
		m.shownKeys = ""
		return nil
	case 1:
		// The prefix's keys win: they are what the next key does.
		if msg != "" && !m.prefixed {
			m.shownKeys = ""
			return []string{fit(msg, m.w) + reset}
		}
		return []string{fit(" "+keysLine(keys), m.w) + reset}
	}
	return []string{m.rule(""), fit(" "+keysLine(keys), m.w) + reset, fit(msg, m.w) + reset}
}

// overRight is line, w cells wide, with s over its right end.
func overRight(line, s string, w int) string {
	sw := ansi.StringWidth(s)
	if sw >= w {
		return fit(s, w) + reset
	}
	return fit(line, w-sw-1) + reset + " " + s + reset
}

// overLeft is line, w cells wide, with s over its left end.
func overLeft(line, s string, w int) string {
	sw := ansi.StringWidth(s)
	if sw >= w {
		return fit(s, w) + reset
	}
	return fit(s+" ", sw+1) + reset + ansi.Cut(fit(line, w), sw+1, w) + reset
}
