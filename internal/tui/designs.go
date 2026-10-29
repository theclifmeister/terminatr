package tui

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// The project popup's Designs tab (docs/SPEC.md §4): each design that
// tm design set recorded or a task names (project.DesignBuilds, the
// board's and the archive's tasks), with its status from its build, its
// design task, how much of its build is done, its open questions, its
// answers and its page. enter opens a design: its answers, its design
// task's open questions (enter has the coordinator open them, as the
// sidebar's questions row does) and its build in after order (enter
// shows a task). Nothing here changes a design: the coordinator records
// them (tm design set).

// designLook is how a design's status is drawn.
func designLook(status string) lipgloss.Style {
	switch status {
	case project.DesignBuilding:
		return th().Good
	case project.DesignDone:
		return th().Good.Bold(true)
	case project.DesignDesigning:
		return th().Info
	}
	return th().Plain
}

// designTitle is d's title, its name when it has none.
func designTitle(d project.DesignBuild) string { return cmp.Or(oneLine(d.Title), d.Name) }

// designTaskWords is d's design task and its status: "T714 review",
// "T714 (not on the board)", "" for none.
func designTaskWords(d project.DesignBuild) string {
	switch {
	case d.Task == "":
		return ""
	case d.DesignTask == nil:
		return d.Task + " (not on the board)"
	}
	return d.Task + " " + string(d.DesignTask.Status)
}

// designFacts are d's design task, open questions, answers and page in
// words, for its second line in the list.
func designFacts(d project.DesignBuild) []string {
	var out []string
	if t := designTaskWords(d); t != "" {
		out = append(out, "design task "+t)
	}
	if n := len(d.Questions); n > 0 {
		out = append(out, counted(n, "open question"))
	}
	out = append(out, counted(len(d.Answers), "answer"))
	if d.Artifact != "" {
		out = append(out, oneLine(d.Artifact))
	}
	return out
}

// buildWords is "3/5 built", "no build tasks".
func buildWords(d project.DesignBuild) string {
	if len(d.Build) == 0 {
		return "no build tasks"
	}
	return fmt.Sprintf("%d/%d built", d.Done(), len(d.Build))
}

// designLines are the tab's lines, two to a design: its status, title
// and build, then its facts faint; sel is the selected design's first
// line, and each line's hit its design.
func (pv *projectView) designLines(w int) ([]string, int, []int) {
	switch {
	case pv.desErr != nil:
		return []string{th().Bad.Render("error: " + oneLine(pv.desErr.Error()))}, -1, nil
	case !pv.desOK:
		return []string{th().Faint.Render("loading…")}, -1, nil
	case len(pv.designs) == 0:
		return faintLines("no designs: a task names the design it builds (tm task edit --design), and the coordinator records one (tm design set)", w), -1, nil
	}
	var out []string
	var hits []int
	sel := -1
	for i, d := range pv.designs {
		status := fmt.Sprintf("%-9s", d.Status())
		build := buildWords(d)
		title := fit(designTitle(d), max(w-len(status)-len(build)-6, 4))
		facts := fit("           "+strings.Join(designFacts(d), " · "), w)
		if i == pv.sel[tabDesigns] {
			sel = len(out)
			out = append(out, th().Sel.Render(fit(status+"  "+padTo(title, w-len(status)-len(build)-4)+"  "+build, w)))
		} else {
			out = append(out, fit(designLook(d.Status()).Render(status)+"  "+padTo(title, w-len(status)-len(build)-4)+"  "+th().Faint.Render(build), w)+reset)
		}
		out = append(out, th().Faint.Render(facts))
		hits = append(hits, i, i)
	}
	return out, sel, hits
}

// padTo pads s with spaces to w cells.
func padTo(s string, w int) string {
	s = fit(s, w)
	return s + strings.Repeat(" ", max(w-ansi.StringWidth(s), 0))
}

// designsKey: enter opens the selected design.
func (pv *projectView) designsKey(m *dash, s string) tea.Cmd {
	if i := pv.sel[tabDesigns]; (s == "enter" || s == "space") && i < len(pv.designs) {
		m.push(&designView{pv: pv, name: pv.designs[i].Name})
	}
	return nil
}

// designView is one design of a project popup's Designs tab, in full.
// It follows the popup's designs, which reload while it is open. Its
// rows are the design task, its open questions and its build tasks.
type designView struct {
	pv   *projectView
	name string
	sel  int
}

func (dv *designView) design() (project.DesignBuild, bool) {
	for _, d := range dv.pv.designs {
		if d.Name == dv.name {
			return d, true
		}
	}
	return project.DesignBuild{}, false
}

// A designRow is what a selectable row of the design view stands for:
// a task to show, or an open question.
type designRow struct {
	task     *tasks.Task
	question *project.Question
}

// rows are d's selectable rows, in the order they are drawn.
func designRows(d project.DesignBuild) []designRow {
	var out []designRow
	if d.DesignTask != nil {
		out = append(out, designRow{task: d.DesignTask})
	}
	for i := range d.Questions {
		out = append(out, designRow{question: &d.Questions[i]})
	}
	for _, t := range d.Ordered() {
		out = append(out, designRow{task: t})
	}
	return out
}

func (dv *designView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	d, ok := dv.design()
	rows := designRows(d)
	switch s := k.String(); s {
	case "esc":
		m.pop()
	case "enter", "space":
		if ok && dv.sel < len(rows) {
			return dv.open(m, rows[dv.sel])
		}
	default:
		if n, ok := scrollKeys[s]; ok {
			dv.sel = moveSel(dv.sel, n, len(rows))
		}
	}
	return nil
}

// open shows a row's task over the design, or has the coordinator open
// its questions, as the sidebar's questions row does.
func (dv *designView) open(m *dash, r designRow) tea.Cmd {
	if r.question != nil {
		return m.answerQuestions(dv.pv.slug)
	}
	if dv.pv.board != nil {
		for _, t := range dv.pv.board.Tasks {
			if t.ID == r.task.ID {
				return m.openTask(dv.pv, t)
			}
		}
	}
	m.msg = r.task.Ref() + " is archived: tm task show " + r.task.Ref() + " shows it"
	return nil
}

func (dv *designView) click(m *dash, item, _ int, double bool) tea.Cmd {
	d, _ := dv.design()
	rows := designRows(d)
	if item < 0 || item >= len(rows) {
		return nil
	}
	dv.sel = item
	if double {
		return dv.open(m, rows[item])
	}
	return nil
}

func (dv *designView) wheel(m *dash, d int) { dv.key(m, arrow(d)) }

func (dv *designView) render(m *dash) string {
	title := "Design · " + dv.pv.slug
	d, ok := dv.design()
	if !ok {
		body := th().Faint.Render("loading…")
		if dv.pv.desOK {
			body = th().Faint.Render("this design is gone")
		}
		return m.popup(box{title: title, body: []string{body}, sel: -1, keys: "esc back"})
	}
	w := m.inner(viewWidth)
	rows := designRows(d)
	dv.sel = min(dv.sel, max(len(rows)-1, 0))
	var body []string
	var hits []int
	sel := -1
	add := func(l string, hit int) {
		body, hits = append(body, l), append(hits, hit)
	}
	field := func(label, value string) {
		if value != "" {
			add(th().Faint.Render(fmt.Sprintf("%-12s", label))+value, noHit)
		}
	}
	section := func(name string) {
		add("", noHit)
		add(th().Head.Render(name), noHit)
	}
	// row is selectable row i: plain when selected, else styled.
	row := func(i int, plain, styled string) {
		if i == dv.sel {
			sel = len(body)
			add(th().Sel.Render(fit(plain, w)), i)
			return
		}
		add(fit(styled, w)+reset, i)
	}
	add(th().Title.Render(fit(designTitle(d), w)), noHit)
	add(designLook(d.Status()).Render(d.Status())+th().Faint.Render(" · "+d.Name), noHit)
	add("", noHit)
	i := 0
	if d.Task != "" {
		if d.DesignTask != nil {
			label, rest := fmt.Sprintf("%-12s", "design task"), d.Task+" "+oneLine(d.DesignTask.Title)
			row(i, label+rest+" · "+string(d.DesignTask.Status), th().Faint.Render(label)+rest+th().Faint.Render(" · "+string(d.DesignTask.Status)))
			i++
		} else {
			field("design task", d.Task+th().Faint.Render(" (not on the board)"))
		}
	}
	field("page", oneLine(d.Artifact))
	field("build", buildWords(d))
	section(fmt.Sprintf("ANSWERS %d", len(d.Answers)))
	if len(d.Answers) == 0 {
		add(th().Faint.Render("none recorded yet (tm design set --answer)"), noHit)
	}
	for j, a := range d.Answers {
		for k, l := range wrapLines(oneLine(a), max(w-4, 10)) {
			lead := "    "
			if k == 0 {
				lead = fmt.Sprintf("%2d. ", j+1)
			}
			add(lead+l, noHit)
		}
	}
	if len(d.Questions) > 0 {
		section(fmt.Sprintf("OPEN QUESTIONS %d", len(d.Questions)))
		for _, q := range d.Questions {
			head := oneLine(q.Header) + ": "
			row(i, q.ID+" "+head+oneLine(q.Question), th().Warn.Render(q.ID)+" "+th().Faint.Render(head)+oneLine(q.Question))
			i++
		}
	}
	section(fmt.Sprintf("BUILD %s", buildWords(d)))
	if len(d.Build) == 0 {
		add(th().Faint.Render("no task builds it yet: a task names it with tm task edit --design "+d.Name), noHit)
	}
	for _, t := range d.Ordered() {
		g, st := stateLook(string(t.Status))
		lead, state := fmt.Sprintf("%-5s ", t.Ref())+oneLine(t.Title)+" ", strings.TrimSpace(g+" "+string(t.Status))
		after := ""
		if len(t.After) > 0 {
			after = " · after " + taskRefList(t.After)
		}
		row(i, lead+state+after, lead+st.Render(state)+th().Faint.Render(after))
		i++
	}
	keys := "↑ ↓ move · esc back"
	if dv.sel < len(rows) {
		if rows[dv.sel].question != nil {
			keys = "enter answer in the coordinator · " + keys
		} else {
			keys = "enter show the task · " + keys
		}
	}
	b := box{title: title, body: body, sel: sel, hits: hits, keys: keys}
	return m.popup(b)
}

// taskRefList is "T10, T11".
func taskRefList(ids []int) string {
	refs := make([]string, len(ids))
	for i, id := range ids {
		refs[i] = "T" + strconv.Itoa(id)
	}
	return strings.Join(refs, ", ")
}
