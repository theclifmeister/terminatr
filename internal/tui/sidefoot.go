package tui

import (
	"cmp"
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// The sidebar's footer: the selected row's details and the key bar.

// sideFooter is the full sidebar's footer, below its tree (docs/SPEC.md
// §4): a faint rule, two lines about the highlighted row (the cursor's
// while the sidebar has the keyboard, else the one you are on; details),
// its key bar (sideKeyBar), and the hint that an update is out when
// there is one. session: drawn beside a session, where prefix+tab gives
// the sidebar the keyboard (tab on the dashboard). None in the slim
// strip. compact (rows.density) keeps the key bar alone, where a
// glyph's tooltip shows instead. Each line is w-1-sideGap cells wide,
// styled.
func sideFooter(all []treeRow, w int, head sideHeadIn, s ServerInfo, session, compact bool) []string {
	if !footWide(w) {
		return nil
	}
	cw := w - 1 - sideGap
	var r *treeRow
	focused := false
	if head.cursor || head.here {
		hr := headRow(head.here)
		r, focused = &hr, head.cursor
	}
	for i := range all {
		if focused {
			break
		}
		if all[i].cursor {
			r, focused = &all[i], true
			break
		}
		if all[i].here && r == nil {
			r = &all[i]
		}
	}
	d1, d2 := "", ""
	tip := sideHoverTip()
	switch {
	case tip != "":
		// The glyph under the mouse, instead of the row (sideTip).
		d1, d2 = tip, "(hover)"
	case r != nil:
		d1, d2 = r.details()
	}
	out := []string{
		th().Faint.Render(" " + strings.Repeat("─", cw-1)),
		th().Head.Render(fit(" "+d1, cw)),
		th().Faint.Render(fit(" "+d2, cw)),
		sideKeyBar(r, focused, session, cw),
	}
	if compact {
		out = out[3:]
		if tip != "" {
			out[0] = th().Head.Render(fit(" "+tip, cw))
		}
	}
	if h := versionHint(s); h != "" {
		if ansi.StringWidth(h) > cw-1 {
			h = versionHintShort(s)
		}
		st := th().Faint
		if consoleOlder(s) {
			st = th().Warn
		}
		out = append(out, st.Render(fit(" "+h, cw)))
	}
	return out
}

// details are the footer's two lines about r: its full name and state,
// then what goes with it (the coordinator's context, a thread's
// progress, its PR's number and checks).
func (r treeRow) details() (string, string) {
	running := func(state string) string { return cmp.Or(state, "not running") }
	if r.kind == treeServer || r.isPeer() {
		return peerDetails(r)
	}
	switch r.kind {
	case treeHead:
		return "all projects", "what needs you in every project, together"
	case treeSection:
		return sectionDetails(r.sec)
	case treeDivider:
		return counted(r.threads, "inactive project"), "nothing of them runs; folded by default"
	case treeProject:
		bits := []string{r.slug, "active"}
		if r.inactive {
			bits[1] = "inactive"
		}
		if r.paused {
			bits = append(bits, "paused")
		}
		if r.pinned {
			bits = append(bits, "pinned")
		}
		if r.inactive {
			return strings.Join(bits, " · "), "previewing it starts nothing"
		}
		if r.prFails > 0 {
			return r.slug + ": PR host failing", prHostWords(r.prFails)
		}
		return strings.Join(bits, " · "), fmt.Sprintf("%s · coordinator %s", counted(r.threads, "thread"), running(r.state))
	case treeCoordinator:
		if r.dormant {
			return r.slug + " coordinator · dormant", "enter starts it and activates " + r.slug
		}
		var bits []string
		if r.ctx >= 0 {
			bits = append(bits, fmt.Sprintf("context %d%%", r.ctx))
		}
		if r.remote {
			bits = append(bits, "remote control on")
		}
		if r.asks > 0 {
			bits = append(bits, counted(r.asks, "question")+" waiting")
		}
		if r.state == "" {
			bits = append(bits, "enter starts it")
		}
		return r.slug + " coordinator · " + running(r.state), strings.Join(bits, " · ")
	}
	l1 := r.thread + " · ad hoc"
	switch {
	case r.name != r.thread:
		l1 = strings.TrimSpace(r.name+" "+r.title) + " (" + r.thread + ")"
	case r.title != "":
		l1 = r.thread + " " + r.title
	}
	bits := []string{running(r.state)}
	if r.dormant {
		bits[0] = "dormant"
	}
	if r.pct >= 0 {
		bits = append(bits, fmt.Sprintf("%d%%", r.pct))
	}
	if r.pr > 0 {
		bits = append(bits, fmt.Sprintf("PR #%d", r.pr))
		if w := ciVerb(r.ci); w != "" {
			bits = append(bits, "CI "+w)
		}
	}
	return l1, strings.Join(bits, " · ")
}

// counted is "1 thread", "2 threads".
func counted(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// sideKeyBar is the footer's key bar, cw cells wide: the keys that do
// something on r while the sidebar has the keyboard (focused), else the
// way to give it the keyboard. "? help" gives way first when they don't
// fit.
func sideKeyBar(r *treeRow, focused, session bool, cw int) string {
	var keys []string
	// The keys as the bar writes them, or as a keymap binds them.
	enter, space, left := barKey("side.enter", "⏎"), barKey("side.activate", "␣"), barKey("side.fold", "←")
	switch {
	case focused && sideFilterNow().typing:
		keys = []string{"type filter", "⏎ keep", "esc clear"}
	case !focused && session:
		keys = []string{"prefix+tab sidebar", "prefix+" + barKey("help.open", "?") + " help"}
	case !focused:
		keys = []string{"tab sidebar", barKey("help.open", "?") + " help"}
	case r == nil:
	case r.kind == treeHead:
		keys = []string{enter + " show all"}
	case r.kind == treeSection:
		keys = sectionKeys(r.sec)
	case r.kind == treeServer, r.isPeer():
		keys = peerKeyBar(r)
	case r.kind == treeDivider && r.fold:
		keys = []string{enter + " unfold"}
	case r.kind == treeDivider:
		keys = []string{enter + " fold"}
	case r.kind == treeProject:
		fold := barKey("side.fold", "←") + " fold"
		if r.fold {
			fold = barKey("side.unfold", "→") + " unfold"
		}
		act, pin := space+" deactivate", barKey("side.pin", "p")+" pin"
		if r.inactive {
			act = space + " activate"
		}
		if r.pinned {
			pin = barKey("side.pin", "p") + " unpin"
		}
		keys = []string{enter + " open"}
		if r.fold || r.inactive {
			keys = append(keys, fold)
		}
		keys = append(keys, act)
		if !r.inactive {
			keys = append(keys, pin)
		}
	case r.kind == treeCoordinator && r.dormant:
		keys = []string{enter + " start", left + " project"}
	case r.kind == treeCoordinator && r.state == "":
		keys = []string{enter + " start", left + " project"}
	case r.kind == treeCoordinator, r.session != "":
		keys = []string{enter + " attach", left + " project"}
		if r.kind == treeCoordinator && r.ctxHint {
			keys = append(keys, barKey("side.save-clear", "c")+" save and clear")
		}
		if r.hint {
			keys = append(keys, barKey("side.needs-next", "w")+" next "+ic().hint)
		}
	default:
		keys = []string{left + " project"}
	}
	if focused && !sideFilterNow().typing {
		keys = append(keys, barKey("side.help", "?")+" help")
	}
	for len(keys) > 1 && ansi.StringWidth(" "+strings.Join(keys, " · ")) > cw {
		keys = keys[:len(keys)-1]
	}
	return fit(" "+keysLine(strings.Join(keys, " · ")), cw)
}

// ciVerb is a PR's CI state as the footer says it.
func ciVerb(ci string) string {
	switch ci {
	case "pass":
		return "passes"
	case "fail":
		return "fails"
	case "pending":
		return "running"
	}
	return ""
}
