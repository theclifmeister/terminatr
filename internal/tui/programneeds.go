package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// The program needs dialog (the programs design §7.2, §7.3): what waits
// on the user in a program, from its rolled-up Needs you
// (program.needs), page by page. First the program's own questions, up
// to project.MaxDialogQuestions a page, each with the members it covers
// in its chip (api+web): the user picks an option of each and submits,
// and each answer goes to every member question it covers
// (program.Answer, the user's own). Then every other need one by one,
// its member in the chip, with the human acts it takes: a thread's
// question menu or permission dialog opens its pane; a task in review
// is accepted or sent back with a note (the member coordinator's
// items, as the Tasks tab asks them); the coordinator's questions open
// in its pane. The director never fills it: it only opens it (tm
// program needs --open, its band's Answer).
//
// Enter on a need of the program dashboard opens it at that need; the
// director pane's band and tm program needs --open at its first page.

// programNeedRef is a program need on a dashboard row: the program and
// the need as program.needs sent it.
type programNeedRef struct {
	program string
	need    proto.ProgramNeed
}

// pnPage is one page of the dialog: up to MaxDialogQuestions program
// questions, or one other need.
type pnPage struct {
	questions []proto.ProgramNeed
	need      *proto.ProgramNeed
}

// pnItem is one line of a page that the keys pick: a question's option
// (q its question's index on the page, opt the option's), the submit
// line, or one of a need's acts.
type pnItem struct {
	label  string
	q, opt int
	submit bool
	run    func(m *dash) tea.Cmd
}

// programNeedsView is the program needs dialog.
type programNeedsView struct {
	slug  string
	pages []pnPage
	page  int
	sel   int
	// picks are the options picked of each program question, by its id
	// (Q2), by option index.
	picks map[string]map[int]bool
	// at is the need to open at once the needs came; "" for the first
	// page.
	at     string
	loaded bool
}

// openProgramNeeds opens program slug's needs dialog, at need at ("" for
// its first page): from the needs the dashboard has, else once tm's
// server sent them.
func (m *dash) openProgramNeeds(slug, at string) tea.Cmd {
	v := &programNeedsView{slug: slug, at: at, picks: map[string]map[int]bool{}}
	m.push(v)
	if m.pneeds.slug == slug && m.pneeds.res != nil {
		v.refresh(m, m.pneeds.res)
	}
	src := m.src
	return func() tea.Msg {
		res, err := src.ProgramNeeds(slug)
		return programNeedsMsg{slug: slug, res: res, err: err, dialog: true}
	}
}

// programPages are res's needs as the dialog's pages: the program's
// questions first, MaxDialogQuestions to a page, then each other need.
func programPages(res *proto.ProgramNeedsResult) []pnPage {
	var pages []pnPage
	var qs []proto.ProgramNeed
	for _, n := range res.Needs {
		if programQuestion(n) {
			qs = append(qs, n)
		}
	}
	for len(qs) > 0 {
		k := min(len(qs), project.MaxDialogQuestions)
		pages = append(pages, pnPage{questions: qs[:k]})
		qs = qs[k:]
	}
	for i := range res.Needs {
		if n := res.Needs[i]; !programQuestion(n) {
			pages = append(pages, pnPage{need: &n})
		}
	}
	return pages
}

// programQuestion says need n is one of the program's own questions as
// program.needs sends it: program, with its question, and the answer
// act; the user answers it here once for the member questions it
// covers (merged).
func programQuestion(n proto.ProgramNeed) bool {
	return n.Program && n.Question != nil && len(n.Question.Questions) > 0 && slices.Contains(n.Acts, proto.ActAnswer)
}

// pageHas says page p shows need id.
func (p pnPage) has(id string) bool {
	if p.need != nil {
		return p.need.ID == id
	}
	for _, q := range p.questions {
		if q.ID == id {
			return true
		}
	}
	return false
}

// firstID is the first need page p shows.
func (p pnPage) firstID() string {
	if p.need != nil {
		return p.need.ID
	}
	if len(p.questions) > 0 {
		return p.questions[0].ID
	}
	return ""
}

// refresh lays the pages out again from res, on the page that shows the
// need it showed (or at), the picks kept.
func (v *programNeedsView) refresh(m *dash, res *proto.ProgramNeedsResult) {
	keep := v.at
	if v.loaded && v.page < len(v.pages) {
		keep = v.pages[v.page].firstID()
	}
	v.pages, v.loaded, v.at = programPages(res), true, ""
	page := 0
	for i, p := range v.pages {
		if keep != "" && p.has(keep) {
			page = i
			break
		}
	}
	if page != v.page {
		v.page, v.sel = page, 0
	}
	v.sel = min(v.sel, max(len(v.items(m))-1, 0))
}

// questionID is program question need n's id: Q2.
func questionID(n proto.ProgramNeed) string { return strings.TrimPrefix(n.ID, "ask:") }

// items are the current page's lines that the keys pick.
func (v *programNeedsView) items(m *dash) []pnItem {
	if v.page >= len(v.pages) {
		return nil
	}
	p := v.pages[v.page]
	if p.need == nil {
		var out []pnItem
		for qi, n := range p.questions {
			for oi, o := range n.Question.Questions[0].Options {
				out = append(out, pnItem{label: oneLine(o.Label), q: qi, opt: oi})
			}
		}
		return append(out, pnItem{label: "submit the answers picked", submit: true})
	}
	return needActs(m, v.slug, *p.need)
}

// needActs are the human acts the dialog offers on need n of program
// slug, as its row's Acts list them.
func needActs(m *dash, slug string, n proto.ProgramNeed) []pnItem {
	var out []pnItem
	add := func(label string, run func(m *dash) tea.Cmd) {
		out = append(out, pnItem{label: label, run: run})
	}
	show := func(m *dash) tea.Cmd { return m.showProject(n.Project) }
	if n.Server != "" {
		// A paired member's need: acted on from here while its server
		// takes this one's acts (its Acts: needs answer), through
		// peer.need-act in the paired need dialog; else answered there.
		where := peerShown(m.data.Peers, n.Server)
		ref := peerNeedRef{alias: n.Server, server: where, project: n.Project, need: n.PeerNeed, answer: len(n.Acts) > 0}
		if _, choices := peerNeedChoices(n.Project, n.PeerNeed); ref.answer && len(choices) > 0 && !n.Escape {
			add("act on it here: it goes to "+where+" as your answer", func(m *dash) tea.Cmd { return m.openPeerNeed(ref) })
			add("show its dashboard on "+where, func(m *dash) tea.Cmd { return m.showProject(peerFold(n.Server, n.Project)) })
			return out
		}
		add("show its dashboard on "+where+" (answered there)", func(m *dash) tea.Cmd { return m.showProject(peerFold(n.Server, n.Project)) })
		return out
	}
	if n.Kind == proto.NeedUmbrella {
		for _, a := range n.Acts {
			switch a {
			case proto.ActAccept:
				add("accept: marks "+slug+"/"+n.Task+" done", func(m *dash) tea.Cmd { return m.acceptUmbrellaNeed(slug, n) })
			case proto.ActSendBack:
				add("send back, with a note: reopens it", func(m *dash) tea.Cmd { return m.sendBackUmbrellaNeed(slug, n) })
			}
		}
		return out
	}
	if n.Program {
		return out // a program question the answer act doesn't offer: nothing here
	}
	if n.Escape {
		if n.Session != "" {
			add("open its pane: only you answer it, there", attachNeed(n))
		}
		return out
	}
	for _, a := range n.Acts {
		switch a {
		case proto.ActAnswer:
			if n.Session != "" {
				add("open its pane to answer its question", attachNeed(n))
			}
		case proto.ActApprove:
			if n.Session != "" {
				add("open its pane to answer the dialog", attachNeed(n))
			}
		case proto.ActAccept:
			add("accept: "+n.Project+"'s coordinator marks it done", func(m *dash) tea.Cmd { return m.acceptNeed(n) })
		case proto.ActSendBack:
			add("send back, with a note", func(m *dash) tea.Cmd { return m.sendBackNeed(n) })
		case proto.ActQuestions:
			add("open "+n.Project+"'s coordinator questions in its pane", func(m *dash) tea.Cmd { return m.answerQuestions(n.Project) })
		}
	}
	add("show "+n.Project+"'s dashboard", show)
	return out
}

// attachNeed attaches need n's session, where its menu or dialog is
// answered.
func attachNeed(n proto.ProgramNeed) func(m *dash) tea.Cmd {
	return func(m *dash) tea.Cmd {
		return m.act(func() actionMsg { return actionMsg{attach: n.Session, current: n.Project} })
	}
}

// needTask is need n's task's number, 0 for none.
func needTask(n proto.ProgramNeed) int {
	id, err := strconv.Atoi(strings.TrimPrefix(n.Task, "T"))
	if err != nil {
		return 0
	}
	return id
}

// acceptNeed asks first, then has need n's member coordinator accept its
// task in review, as the Tasks tab's A does.
func (m *dash) acceptNeed(n proto.ProgramNeed) tea.Cmd {
	id := needTask(n)
	if id == 0 {
		return nil
	}
	ref := n.Project + " " + n.Task
	m.confirmNo("Accept "+ref, "Accept "+ref+" "+oneLine(n.Title)+"? "+n.Project+"'s coordinator marks it done.", ref+" not accepted", func() tea.Cmd {
		return m.askNeed(n.Project, id, project.KindAccept, "", "asked "+n.Project+"'s coordinator to accept "+n.Task)
	})
	return nil
}

// acceptUmbrellaNeed asks first, then marks umbrella need n of program
// slug done: the user's own accept, as the dashboard's UMBRELLAS pane.
func (m *dash) acceptUmbrellaNeed(slug string, n proto.ProgramNeed) tea.Cmd {
	id := needTask(n)
	if id == 0 {
		return nil
	}
	return m.openUmbrella(umbrellaRef{program: slug, id: id, status: tasks.Review, title: n.Title})
}

// sendBackUmbrellaNeed asks for a note, then reopens umbrella need n of
// program slug with it.
func (m *dash) sendBackUmbrellaNeed(slug string, n proto.ProgramNeed) tea.Cmd {
	id := needTask(n)
	if id == 0 {
		return nil
	}
	ref := slug + "/" + n.Task
	m.promptNo("Send "+ref+" back", "What should change? The note stays on the task; it reopens.", "", ref+" not sent back", project.MaxSendBackNote, func(note string) tea.Cmd {
		src := m.src
		return m.act(func() actionMsg {
			if err := src.SendBackUmbrella(slug, id, note); err != nil {
				return actionMsg{err: err}
			}
			return actionMsg{msg: "sent " + ref + " back"}
		})
	})
	return nil
}

// sendBackNeed asks for a note, then has need n's member coordinator
// send its task back with it.
func (m *dash) sendBackNeed(n proto.ProgramNeed) tea.Cmd {
	id := needTask(n)
	if id == 0 {
		return nil
	}
	ref := n.Project + " " + n.Task
	m.promptNo("Send "+ref+" back", "What should change? The coordinator passes your note on.", "", ref+" not sent back", project.MaxSendBackNote, func(note string) tea.Cmd {
		return m.askNeed(n.Project, id, project.KindSendBack, note, "asked "+n.Project+"'s coordinator to send "+n.Task+" back")
	})
	return nil
}

// askNeed drops kind's item for slug's task id; the footer says done.
func (m *dash) askNeed(slug string, id int, kind, note, done string) tea.Cmd {
	src := m.src
	return m.act(func() actionMsg {
		asked, err := src.Ask(slug, id, kind, note)
		if err == nil && !asked {
			return actionMsg{msg: fmt.Sprintf("T%d is already %s", id, delegateWaiting)}
		}
		return actionMsg{msg: done, err: err}
	})
}

func (v *programNeedsView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	items := v.items(m)
	switch s := k.String(); s {
	case "esc", "q":
		m.pop()
	case "up", "down":
		v.sel = moveSel(v.sel, scrollKeys[s], len(items))
	case "left", "pgup", "shift+tab":
		v.turn(-1)
	case "right", "pgdown", "tab":
		v.turn(1)
	case "enter", "space":
		if v.sel < len(items) {
			return v.pick(m, items[v.sel], s == "space")
		}
	default:
		if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= len(items) {
			v.sel = n - 1
			return v.pick(m, items[n-1], false)
		}
	}
	return nil
}

// turn moves d pages on, within the pages.
func (v *programNeedsView) turn(d int) {
	if p := min(max(v.page+d, 0), max(len(v.pages)-1, 0)); p != v.page {
		v.page, v.sel = p, 0
	}
}

// pick runs item it: an option is picked (a multi-select's ticked),
// submit sends the answers, an act runs; tickOnly: space never runs
// an act or submits.
func (v *programNeedsView) pick(m *dash, it pnItem, tickOnly bool) tea.Cmd {
	switch {
	case it.run != nil && !tickOnly:
		m.pop()
		return it.run(m)
	case it.submit && !tickOnly:
		return v.submit(m)
	case it.run == nil && !it.submit:
		n := v.pages[v.page].questions[it.q]
		id, multi := questionID(n), n.Question.Questions[0].MultiSelect
		// One option of a question, any of a multi-select's; picking
		// a picked one again unpicks it.
		was := v.picks[id][it.opt]
		if !multi || v.picks[id] == nil {
			v.picks[id] = map[int]bool{}
		}
		v.picks[id][it.opt] = !was
	}
	return nil
}

// answers are the page's questions with an option picked: each one's
// id and labels, in the options' order.
func (v *programNeedsView) answers() (ids []string, labels [][]string) {
	for _, n := range v.pages[v.page].questions {
		id := questionID(n)
		var ls []string
		for oi, o := range n.Question.Questions[0].Options {
			if v.picks[id][oi] {
				ls = append(ls, o.Label)
			}
		}
		if len(ls) > 0 {
			ids, labels = append(ids, id), append(labels, ls)
		}
	}
	return ids, labels
}

// submit asks first, then sends the page's answers as the user's, each
// to every member question its program question covers.
func (v *programNeedsView) submit(m *dash) tea.Cmd {
	ids, labels := v.answers()
	if len(ids) == 0 {
		m.msg = "pick an option of a question first: up, down and enter"
		return nil
	}
	var words []string
	for i, id := range ids {
		words = append(words, id+": "+strings.Join(labels[i], ", "))
	}
	slug := v.slug
	m.pop()
	m.confirmNo("Answer in "+slug, "Answer "+strings.Join(words, "; ")+"? Each answer goes to every member question it covers, as yours.", "not answered", func() tea.Cmd {
		src := m.src
		return m.act(func() actionMsg {
			var done, failed []string
			for i, id := range ids {
				res, err := src.AnswerProgram(slug, id, labels[i], "")
				if err != nil && len(res.Fanned) == 0 {
					return actionMsg{err: fmt.Errorf("%s/%s: %w", slug, id, err)}
				}
				var to []string
				for _, f := range res.Fanned {
					to = append(to, f.Project+"/"+f.Question+" "+f.Result)
					if f.Error != "" {
						failed = append(failed, f.Project+"/"+f.Question+": "+f.Error)
					}
				}
				done = append(done, id+" → "+strings.Join(to, ", "))
			}
			msg := "answered " + strings.Join(done, "; ")
			if len(failed) > 0 {
				msg += "; not sent to " + strings.Join(failed, "; ")
			}
			return actionMsg{msg: msg}
		})
	})
	return nil
}

func (v *programNeedsView) render(m *dash) string {
	w := m.inner(dialogWidth)
	var body []string
	var hits []int
	line := func(s string, hit int) {
		body = append(body, s)
		hits = append(hits, hit)
	}
	title := v.slug + " · needs you"
	keys := "esc close"
	switch {
	case !v.loaded:
		line(th().Faint.Render("Reading what waits on you in "+v.slug+"…"), noHit)
	case len(v.pages) == 0:
		line(th().Faint.Render("Nothing waits on you in "+v.slug+"."), noHit)
	default:
		p := v.pages[v.page]
		title = fmt.Sprintf("%s · needs you (%d of %d)", v.slug, v.page+1, len(v.pages))
		items := v.items(m)
		at := 0
		if p.need == nil {
			for qi, n := range p.questions {
				it := n.Question.Questions[0]
				if qi > 0 {
					line("", noHit)
				}
				head := "[" + needChip(m.data.Peers, n) + "] " + questionID(n)
				if it.Header != "" {
					head += " " + oneLine(it.Header)
				}
				line(th().Head.Render(fit(head, w)), noHit)
				for _, l := range wrapLines(it.Question, w) {
					line(l, noHit)
				}
				if len(n.Merged) > 0 {
					line(th().Faint.Render(fit("covers "+strings.Join(n.Merged, ", "), w)), noHit)
				}
				for oi, o := range it.Options {
					mark := optionMark(it.MultiSelect, v.picks[questionID(n)][oi])
					label := o.Label
					if o.Description != "" {
						label += " — " + o.Description
					}
					line(fit(fmt.Sprintf("  %d  %s%s", at+1, mark, oneLine(label)), w), at)
					at++
				}
			}
			line("", noHit)
			line("     "+items[len(items)-1].label, at)
			line("", noHit)
			for _, l := range wrapLines("Each answer goes to every member question it covers, as yours: tm answers there for you.", w) {
				line(th().Faint.Render(l), noHit)
			}
			keys = "↑ ↓ · enter picks · submit sends · ← → pages · esc close"
		} else {
			n := *p.need
			head := "[" + needChip(m.data.Peers, n) + "] " + strings.TrimSpace(needRefShort(n)+" "+needKindWords(n))
			if n.Priority != "" {
				head += " · " + n.Priority
			}
			line(th().Head.Render(fit(head, w)), noHit)
			if n.Title != "" && n.Task != "" {
				for _, l := range wrapLines(n.Task+" "+oneLine(n.Title), w) {
					line(l, noHit)
				}
			}
			if what := needWhatShort(n); what != "" && what != oneLine(n.Title) {
				for _, l := range wrapLines(what, w) {
					line(th().Warn.Render(l), noHit)
				}
			}
			if n.PRURL != "" {
				line(th().Faint.Render(fit("PR "+prRef(n.PRURL)+" "+oneLine(n.PR), w)), noHit)
			}
			line("", noHit)
			for i, it := range items {
				line(fit(fmt.Sprintf("  %d  %s", i+1, it.label), w), i)
			}
			if len(items) == 0 {
				line(th().Faint.Render("Nothing here acts on it."), noHit)
			}
			keys = "1-9 or enter pick · ← → pages · esc close"
		}
	}
	sel := -1
	for i, h := range hits {
		if h == v.sel && h != noHit {
			sel = i
		}
	}
	return m.popup(box{title: title, body: body, sel: sel, hits: hits, keys: keys, dialog: true})
}

// optionMark is an option's box: ( ) or (x) for one of a question,
// [ ] or [x] for a multi-select's.
func optionMark(multi, picked bool) string {
	switch {
	case multi && picked:
		return "[x] "
	case multi:
		return "[ ] "
	case picked:
		return "(x) "
	}
	return "( ) "
}
