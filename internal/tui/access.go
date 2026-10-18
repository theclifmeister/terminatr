package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// The effective access view (T574 §4.4, phase 1): what a thread of a
// project gets, as rules with their sources, from tm access show's
// access.show. On a thread it is what the thread launched with, each
// rule config.toml added or removed since marked RESTART TO APPLY; the
// agent's own settings files' rules are listed after, shown only.
// Read-only: e on the dashboard (a thread's row, or the project) and on
// a project's Sandbox tab opens it.

// accessView shows one result; ↑ ↓ move through its rules.
type accessView struct {
	res proto.AccessShowResult
	sel int
}

// showAccess opens the effective access of thread id of project slug
// (id "" for a new thread of it).
func (m *dash) showAccess(slug, id string) tea.Cmd {
	res, err := m.src.AccessShow(slug, id)
	if err != nil {
		m.fail(err)
		return nil
	}
	m.push(&accessView{res: res})
	return nil
}

// accessHere is the e action: the selected thread's effective access,
// else what a new thread of the project here gets.
func (m *dash) accessHere(string) tea.Cmd {
	if r, ok := m.selected(); ok && r.thread != nil && r.thread.Record != nil && r.project != "" {
		return m.showAccess(r.project, r.thread.ID)
	}
	if slug := m.needProject(); slug != "" {
		return m.showAccess(slug, "")
	}
	return nil
}

func (v *accessView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc", "q":
		m.pop()
	case "up", "k", "down", "j", "pgup", "pgdown":
		v.sel = moveSel(v.sel, scrollKeys[k.String()], len(v.res.Rules))
	}
	return nil
}

// accessViewTitle is the view's title: whose access, its agent and
// platform, and when it launched.
func accessViewTitle(res proto.AccessShowResult) string {
	who := "a new thread"
	if res.Thread != "" {
		who = res.Thread
	}
	on := config.OSName(res.OS)
	if res.Agent != "" {
		on = res.Agent + ", " + on
	}
	t := "Effective access · " + res.Project + " · " + who + " (" + on + ")"
	if res.Recorded && !res.Launched.IsZero() {
		t += " · launched " + res.Launched.Local().Format("Jan 2 15:04")
	}
	return t
}

func (v *accessView) render(m *dash) string {
	w := m.inner(viewWidth)
	res := v.res
	var head []string
	switch {
	case res.Thread != "" && !res.Recorded:
		head = faintLines(res.Thread+" has no launch record (it launched before tm kept one, or hasn't launched): this is what it would get now.", w)
	case res.Thread != "":
		head = faintLines("What "+res.Thread+" launched with. Guard rules are read live; the agent's own files are shown only.", w)
	default:
		head = faintLines("What a new thread of "+res.Project+" gets. Guard rules are read live; the agent's own files are shown only.", w)
	}
	if res.SandboxError != "" {
		head = append(head, warnLines("Sandbox lists and presets left out: "+res.SandboxError, w)...)
	}
	const whereW = 9
	srcW := min(max(w/3, 12), 30)
	ruleW := max(w-whereW-srcW-2, 10)
	lines := []string{th().Faint.Render(fit("WHERE", whereW) + " " + fit("RULE", ruleW) + " " + "FROM")}
	sel := -1
	for i, r := range res.Rules {
		where := r.Where
		gone := r.Change == proto.AccessRemoved || r.Skipped != ""
		if gone {
			where += " ─"
		}
		value := strings.Join(strings.Fields(r.Value), " ")
		if r.Note == proto.AccessSettingsNote {
			value = "(your settings file)"
		}
		rule := textsafe.Line(r.Kind+" "+value, ruleW)
		from := r.Source
		if r.Note != "" {
			from += " · " + r.Note
		}
		text := fit(where, whereW) + " " + fit(rule, ruleW) + " " + textsafe.Line(from, srcW)
		switch {
		case i == v.sel:
			lines = append(lines, th().Sel.Render(fit(text, w)))
			sel = len(lines) - 1
		case gone || r.Where == proto.AccessAgent:
			lines = append(lines, th().Faint.Render(text))
		default:
			lines = append(lines, text)
		}
		for _, rm := range r.Remarks() {
			style := th().Faint
			if strings.HasPrefix(rm, "RESTART TO APPLY") {
				style = th().Warn
			}
			for _, l := range wrapLines(rm, w-whereW-1) {
				lines = append(lines, strings.Repeat(" ", whereW+1)+style.Render(l))
			}
		}
	}
	switch {
	case res.Changes == 1:
		lines = append(lines, "", th().Warn.Render(fmt.Sprintf("1 change since launch: tm thread restart %s applies it", res.Thread)))
	case res.Changes > 1:
		lines = append(lines, "", th().Warn.Render(fmt.Sprintf("%d changes since launch: tm thread restart %s applies them", res.Changes, res.Thread)))
	}
	return m.popup(box{title: accessViewTitle(res), head: append(head, ""), body: lines, sel: sel, keys: "↑ ↓ move · esc back"})
}
