package tui

import (
	"cmp"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// The program dashboard (the programs design §11.2): enter on a
// program's row in the sidebar shows it, as enter on a project's shows
// the project's. The view's current project is then "g:<slug>"
// (programKey), which no project slug can be, so every console of the
// view shows it. It lists the program's rolled-up NEEDS YOU, as tm's
// server ranks it (program.needs), then its CHILDREN, the members in
// the program's order:
//
//	" tm dashboard · acme · program · propose · cap 6"
//	" NEEDS YOU 3"
//	"   api       T12 review: OIDC login    review   must · enter answers"
//	"   api+web   Q2 Session length?        blocked  program question"
//	" CHILDREN 3"
//	"   acme-api  coordinator ● working     2 threads · 1 working · 2 need you"
//	"   mini/acme-ios                       invited · its user accepts there"
//
// Enter on a need opens the program needs dialog at it (programneeds.go),
// where the human acts are; on a child, its dashboard (a paired one's
// read-only one).

// programPlace is the program a view's current project names,
// "g:<slug>"; ok is false for anything else.
func programPlace(cur string) (string, bool) {
	slug, ok := strings.CutPrefix(cur, "g:")
	return slug, ok && slug != "" && !strings.Contains(slug, "/")
}

// shownProgram is the program the dashboard shows for current: one of
// d's programs, false when current names none (programs off, or gone).
func shownProgram(d Data, current string) (ProgramData, bool) {
	slug, ok := programPlace(current)
	if !ok {
		return ProgramData{}, false
	}
	for _, g := range d.Programs {
		if g.Slug == slug {
			return g, true
		}
	}
	return ProgramData{}, false
}

// programShown is the program the dashboard shows, if it shows one.
func (m *dash) programShown() (ProgramData, bool) { return shownProgram(m.data, m.current) }

// programCrumb is the header's title on the program dashboard: "acme ·
// program · propose · cap 6".
func programCrumb(g ProgramData) string {
	parts := []string{g.Slug, "program", cmp.Or(g.Authority, config.AuthorityPropose)}
	if g.ParallelThreads > 0 {
		parts = append(parts, fmt.Sprintf("cap %d", g.ParallelThreads))
	}
	if g.Paused {
		parts = append(parts, "paused")
	}
	return strings.Join(parts, " · ")
}

// programNeeds is the shown program's rolled-up Needs you as tm's
// server last sent it (program.needs): res nil until it came; err why
// the last read failed.
type programNeeds struct {
	slug string
	res  *proto.ProgramNeedsResult
	err  error
	at   time.Time
	busy bool
}

// programNeedsMsg is a program.needs answer; dialog: the needs dialog
// asked for it, not the dashboard's poll.
type programNeedsMsg struct {
	slug   string
	res    proto.ProgramNeedsResult
	err    error
	dialog bool
}

// programNeedsEvery is how often the program dashboard reads its needs
// again while it shows: the server builds them from every member.
const programNeedsEvery = 2 * time.Second

// loadProgramNeeds reads the shown program's needs, when they are older
// than programNeedsEvery (now: whatever their age); nil when no program
// shows or a read runs.
func (m *dash) loadProgramNeeds(now bool) tea.Cmd {
	g, ok := m.programShown()
	if !ok || m.pneeds.busy && m.pneeds.slug == g.Slug {
		return nil
	}
	if m.pneeds.slug != g.Slug {
		m.pneeds = programNeeds{slug: g.Slug}
	}
	if !now && time.Since(m.pneeds.at) < programNeedsEvery {
		return nil
	}
	m.pneeds.busy = true
	src, slug := m.src, g.Slug
	return func() tea.Msg {
		res, err := src.ProgramNeeds(slug)
		return programNeedsMsg{slug: slug, res: res, err: err}
	}
}

// tookProgramNeeds keeps a program.needs answer and lays the rows out
// again with it.
func (m *dash) tookProgramNeeds(msg programNeedsMsg) {
	if v, ok := m.top().(*programNeedsView); ok && v.slug == msg.slug {
		if msg.err != nil {
			m.fail(msg.err)
		} else {
			v.refresh(m, &msg.res)
		}
	}
	if msg.slug != m.pneeds.slug {
		return
	}
	if !msg.dialog {
		m.pneeds.busy, m.pneeds.at, m.pneeds.err = false, time.Now(), msg.err
	}
	if msg.err == nil {
		res := msg.res
		m.pneeds.res, m.pneeds.err = &res, nil
	}
	m.rebuild()
}

// programDashRows are the program dashboard's rows: its NEEDS YOU, then
// its CHILDREN.
func programDashRows(d Data, g ProgramData, pn programNeeds) []row {
	var rows []row
	var res *proto.ProgramNeedsResult
	if pn.slug == g.Slug {
		res = pn.res
	}
	switch {
	case res == nil && pn.err != nil:
		rows = append(rows, row{head: "NEEDS YOU"}, row{note: "  not known now: " + oneLine(errText(pn.err))})
	case res == nil:
		rows = append(rows, row{head: "NEEDS YOU"}, row{note: "  reading…"})
	default:
		needs := programNeedRows(d, g.Slug, res)
		rows = append(rows, row{head: "NEEDS YOU", count: len(needs)})
		rows = append(rows, needs...)
		if len(needs) == 0 {
			rows = append(rows, row{note: "  nothing waits on you in " + g.Slug})
		}
		for _, mb := range res.Members {
			switch {
			case mb.Error != "":
				rows = append(rows, row{note: "  " + memberName(d.Peers, mb.Server, mb.Project) + ": needs unknown (" + oneLine(mb.Error) + ")"})
			case mb.Server != "" && mb.Count > 0:
				rows = append(rows, row{note: fmt.Sprintf("  %s: %d waiting on %s (answered there)", memberName(d.Peers, mb.Server, mb.Project), mb.Count, peerShown(d.Peers, mb.Server))})
			}
		}
		if res.Quiet > 0 && len(needs) > 0 {
			rows = append(rows, row{note: "  " + countedAs(res.Quiet, "other project: nothing waiting", "other projects: nothing waiting")})
		}
	}
	kids := childRows(d, g, res)
	rows = append(rows, row{head: "CHILDREN", count: len(kids)})
	if len(kids) == 0 {
		rows = append(rows, row{note: "  no members yet: the program's settings (" + keyShown(boundKey("A")) + ") pick them"})
	}
	rows = append(rows, kids...)
	rows = append(rows, umbrellaRows(d, g)...)
	return append(rows, waitRows(d, g)...)
}

// umbrellaRows are the UMBRELLAS: each open umbrella task of g's board
// (§5.3) with how far its parts are, its bar their share met, and its
// parts under it as a checklist ("[x] acme-api/T12 OIDC login  merged"),
// with their states as the ticker last wrote them. A sweep is an
// umbrella with one part per member, so its checklist reads down the
// members. Enter on an umbrella in review accepts it (asks first); on a
// part, shows that task in its project.
//
//	" UMBRELLAS 2"
//	"   T4  SSO with Okta        ◐ started   ▰▰▱▱▱  2/4 parts · must"
//	"         [x] acme-infra/T21 Terraform OIDC    merged"
//	"         [ ] acme-api/T12 OIDC login          ◔ review"
func umbrellaRows(d Data, g ProgramData) []row {
	if len(g.Umbrellas) == 0 {
		return nil
	}
	out := []row{{head: "UMBRELLAS", count: len(g.Umbrellas)}}
	for _, t := range g.Umbrellas {
		u := umbrellaRef{program: g.Slug, id: t.ID, status: t.Status, title: t.Title}
		r := row{key: "pu:" + t.Ref(), mark: markTop, who: t.Ref(), whoW: max(len(t.Ref()), 4), what: oneLine(t.Title),
			state: string(t.Status), pct: -1, umbrella: &u}
		if t.Status == tasks.Review {
			r.mark = markNeeds
		}
		var rest []string
		parts := program.Parts(t, g.Deps)
		if len(parts) > 0 {
			met, of := program.Progress(parts)
			r.pct = pctOf(met, of)
			rest = append(rest, fmt.Sprintf("%d/%d %s", met, of, map[bool]string{true: "part", false: "parts"}[of == 1]))
		} else {
			rest = append(rest, "no parts yet")
		}
		if t.Priority != tasks.NoPriority {
			rest = append(rest, string(t.Priority))
		}
		if t.Status == tasks.Review {
			rest = append(rest, "every part is in: enter accepts")
		}
		r.rest = strings.Join(rest, " · ")
		out = append(out, r)
		for _, ps := range parts {
			box := "[ ]"
			if ps.Checked() {
				box = "[x]"
			}
			if ps.State == program.DepGone {
				box = "[-]"
			}
			what := box + " " + ps.Ref
			if ps.Title != "" {
				what += " " + oneLine(ps.Title)
			}
			pr := row{key: "pu:" + t.Ref() + ":" + ps.Ref, mark: markTop + "    ", what: what, state: ps.Word(), pct: -1}
			pr.whatMin = len([]rune(what))
			linkTaskRow(&pr, g, d, ps.Ref)
			out = append(out, pr)
		}
	}
	return out
}

// waitRows are the DEPENDENCIES: each member task that comes after
// another member's, as the ticker last saw it, its own status, then
// what it waits on and that one's state:
// "web/T40 Login button  ● started  ← api/T12 (review)".
// Enter shows the waiting task in its project.
func waitRows(d Data, g ProgramData) []row {
	if len(g.Waits) == 0 {
		return nil
	}
	out := []row{{head: "DEPENDENCIES", count: len(g.Waits)}}
	for _, w := range g.Waits {
		dep, ok := g.Deps[w.After]
		word := "not known yet"
		if ok {
			word = dep.Word()
		}
		rest := "← " + w.After + " (" + word + ")"
		switch {
		case ok && dep.State == program.DepMet:
			rest += " · met"
		case ok && dep.State == program.DepGone:
			rest += " · gone: the user decides whether it still waits"
		}
		what := w.Task
		if w.Title != "" {
			what += " " + oneLine(w.Title)
		}
		r := row{key: "pw:" + w.Task + ":" + w.After, mark: markTop, what: what, state: cmp.Or(w.Status, "—"), pct: -1, rest: rest}
		linkTaskRow(&r, g, d, w.Task)
		out = append(out, r)
	}
	return out
}

// linkTaskRow points r at member task ref: a local member's task, which
// enter shows in its project; a paired member's project, whose
// read-only dashboard enter shows.
func linkTaskRow(r *row, g ProgramData, d Data, ref string) {
	alias, slug, _, ok := tasks.CrossRef(ref)
	switch {
	case !ok:
	case alias != "":
		r.remote = peerFold(alias, slug)
	default:
		if _, here := findProject(d.Projects, slug); here {
			r.project = slug
			r.task = g.Tasks[ref]
		}
	}
}

// umbrellaRef is an umbrella task's row on the program dashboard.
type umbrellaRef struct {
	program string
	id      int
	status  tasks.Status
	title   string
}

// openUmbrella is enter on an umbrella's row: one in review is the
// user's to accept (asks first); else it says what its status follows.
func (m *dash) openUmbrella(u umbrellaRef) tea.Cmd {
	ref := fmt.Sprintf("%s/T%d", u.program, u.id)
	if u.status != tasks.Review {
		m.msg = ref + " is " + string(u.status) + ": its status follows its parts, and it waits for your accept once every part is in"
		return nil
	}
	m.confirmNo("Accept "+ref, "Every part of "+ref+" ("+oneLine(u.title)+") is in. Accept it as done?", "not accepted", func() tea.Cmd {
		src := m.src
		return m.act(func() actionMsg {
			if err := src.AcceptUmbrella(u.program, u.id); err != nil {
				return actionMsg{err: err}
			}
			return actionMsg{msg: "accepted " + ref}
		})
	})
	return nil
}

// errText is err's message; a refusal's own words.
func errText(err error) string {
	if perr, ok := err.(*proto.Error); ok {
		return perr.Message
	}
	return err.Error()
}

// memberName is a member as the program dashboard names it: its slug,
// or server/slug on a paired server.
func memberName(peers []proto.PeerMirror, alias, slug string) string {
	if alias == "" {
		return slug
	}
	return peerShown(peers, alias) + "/" + slug
}

// needChip is the member chip of need n on the program dashboard and in
// the needs dialog: the member, or a program question's covered
// members joined with + (api+web).
func needChip(peers []proto.PeerMirror, n proto.ProgramNeed) string {
	if n.Program && len(n.Merged) > 0 {
		var ps []string
		for _, ref := range n.Merged {
			p, _, _ := strings.Cut(ref, "/")
			if len(ps) == 0 || ps[len(ps)-1] != p {
				ps = append(ps, p)
			}
		}
		return strings.Join(ps, "+")
	}
	return memberName(peers, n.Server, n.Project)
}

// programNeedRows are the rows of res's needs, in its order: each
// names its member and what waits, its task's priority after.
func programNeedRows(d Data, slug string, res *proto.ProgramNeedsResult) []row {
	var out []row
	for i := range res.Needs {
		n := &res.Needs[i]
		r := row{key: "pn:" + n.Server + "/" + n.Project + ":" + n.ID, mark: markNeeds, who: needChip(d.Peers, *n),
			state: needState(*n), pct: -1, pneed: &programNeedRef{program: slug, need: *n}}
		if n.Server == "" && !n.Program {
			r.project = n.Project
		}
		what := needWhatShort(*n)
		r.what = strings.TrimSpace(needRefShort(*n) + " " + what)
		var rest []string
		if n.Priority != "" {
			rest = append(rest, n.Priority)
		}
		rest = append(rest, needKindWords(*n))
		if len(n.Similar) > 0 {
			rest = append(rest, "alike: "+strings.Join(n.Similar, ", "))
		}
		r.rest = strings.Join(rest, " · ")
		out = append(out, r)
	}
	return out
}

// needRefShort is what need n is on, short: its task, its thread, a
// question's id.
func needRefShort(n proto.ProgramNeed) string {
	switch {
	case n.Task != "":
		return n.Task
	case n.Thread != "":
		return n.Thread
	case n.Kind == proto.NeedAsk:
		return strings.TrimPrefix(n.ID, "ask:")
	}
	return ""
}

// needWhatShort is need n's own words on one line: its question, its
// dialog's, its text, else its title.
func needWhatShort(n proto.ProgramNeed) string {
	if n.Escape {
		return "asks to run a command outside the sandbox"
	}
	if q := n.Question; q != nil {
		if i := openQuestionAt(q); i >= 0 {
			return oneLine(q.Questions[i].Question)
		}
	}
	if a := n.Approval; a != nil && a.Question != "" {
		return oneLine(a.Question)
	}
	return oneLine(cmp.Or(n.Text, n.Title))
}

// needState is the state word whose glyph need n's row gets.
func needState(n proto.ProgramNeed) string {
	if n.Kind == proto.NeedReview {
		return "review"
	}
	return "blocked"
}

// needKindWords say what kind of need n is, in tm's fixed words.
func needKindWords(n proto.ProgramNeed) string {
	switch {
	case n.Escape:
		return "only in its pane"
	case n.Program:
		return "program question"
	}
	switch n.Kind {
	case proto.NeedApproval:
		return "permission dialog"
	case proto.NeedQuestion:
		if n.Role == proto.RoleCoordinator {
			return "coordinator asks"
		}
		return "thread asks"
	case proto.NeedReview:
		return "in review"
	case proto.NeedAsk:
		return "coordinator question"
	case proto.NeedCI:
		return "PR red"
	case proto.NeedHost:
		return "PR host failing"
	case proto.NeedBlocked:
		return "blocked"
	case proto.NeedQueue:
		return "prompts held"
	case proto.NeedLinkHeld:
		return "message to " + n.Peer + " held"
	case proto.NeedLinkFile:
		return "file offered by " + n.Peer
	case proto.NeedLinkRequest:
		return "link request from " + n.Peer
	}
	return n.Kind
}

// childRows are the CHILDREN rows: each member of g in its order, a
// local one with its coordinator's state and its threads, a paired one
// with its link and membership ("invited", "dormant on mini").
func childRows(d Data, g ProgramData, res *proto.ProgramNeedsResult) []row {
	counts := map[string]int{}
	if res != nil {
		for _, mb := range res.Members {
			counts[mb.Server+"/"+mb.Project] = mb.Needs + mb.Count
		}
	}
	byID := map[string]proto.SessionInfo{}
	for _, s := range d.Sessions {
		byID[s.ID] = s
	}
	var out []row
	for _, m := range g.Projects {
		alias, slug, ok := config.SplitMember(m)
		if !ok {
			continue
		}
		if alias != "" {
			out = append(out, pairedChildRow(d, g, alias, slug, counts[alias+"/"+slug]))
			continue
		}
		p, found := findProject(d.Projects, slug)
		r := row{key: "pc:" + slug, project: slug, mark: markTop, who: slug, pct: -1}
		r.whoW = max(colWho, len([]rune(slug)))
		if !found {
			r.project, r.what, r.state, r.rest = "", "not a project here", "—", "the program's settings ("+keyShown(boundKey("A"))+") drop it"
			out = append(out, r)
			continue
		}
		r.what, r.state = "coordinator", "—"
		for _, s := range d.Sessions {
			if s.Role == proto.RoleCoordinator && s.Project == slug {
				r.session, r.state = s.ID, stateWord(s)
				break
			}
		}
		working, red := 0, 0
		for i := range p.Threads {
			st, _, _ := threadState(&p.Threads[i], byID)
			if st == "working" {
				working++
			}
			if p.Threads[i].Checks == "fail" {
				red++
			}
		}
		var rest []string
		if p.inactive() {
			rest = append(rest, "inactive")
		}
		if n := len(p.Threads); n > 0 {
			rest = append(rest, countedAs(n, "thread", "threads"))
			if working > 0 {
				rest = append(rest, fmt.Sprintf("%d working", working))
			}
		} else if !p.inactive() {
			rest = append(rest, "idle")
		}
		needs, known := counts["/"+slug]
		if !known {
			needs = len(p.NeedsYou)
			if p.Questions > 0 {
				needs++
			}
		}
		if needs > 0 {
			rest = append(rest, needsYouWords(needs))
		}
		if red > 0 {
			rest = append(rest, countedAs(red, "PR red", "PRs red"))
		}
		if p.PRHostFails > 0 {
			rest = append(rest, "PR host failing")
		}
		r.rest = strings.Join(rest, " · ")
		out = append(out, r)
	}
	return out
}

// pairedChildRow is paired member alias/slug's CHILDREN row: its link,
// the program membership's state, what waits there. Enter shows its
// read-only dashboard.
func pairedChildRow(d Data, g ProgramData, alias, slug string, needs int) row {
	name := memberName(d.Peers, alias, slug)
	r := row{key: "pc:" + alias + "/" + slug, remote: peerFold(alias, slug), mark: markTop, who: name, what: "", state: "—", pct: -1}
	r.whoW = max(colWho, len([]rune(name)))
	r.what = "on " + peerShown(d.Peers, alias)
	r.rest = strings.Join(pairedMemberWords(d.Peers, g.Invites[alias+"/"+slug], alias, slug, needs), " · ")
	return r
}

// pairedMemberWords say how paired member alias/slug of a program
// stands, as its CHILDREN row says it: "invited" while its user hasn't
// answered, "dormant on mini" while that server has programs off,
// "offline" while the link is down, else linked with its round trip;
// then what waits there.
func pairedMemberWords(peers []proto.PeerMirror, inv link.Invite, alias, slug string, needs int) []string {
	server := peerShown(peers, alias)
	var out []string
	switch {
	case inv.ID != "" && inv.Pending():
		out = append(out, "invited", "its user accepts there")
		return out
	case inv.ID != "" && inv.State != link.ReqAccepted:
		out = append(out, inv.Words())
		return out
	}
	pm, mp := findPeer(peers, alias, slug)
	switch {
	case pm == nil:
		out = append(out, "not paired any more")
	case !pm.Program:
		out = append(out, "dormant on "+server)
	case pm.State != proto.LinkUp:
		out = append(out, "offline")
	case pm.RTT > 0:
		out = append(out, fmt.Sprintf("linked %.0f ms", pm.RTT))
	default:
		out = append(out, "linked")
	}
	if pm != nil && mp == nil {
		out = append(out, "not shared with this server")
	}
	if needs > 0 {
		out = append(out, fmt.Sprintf("%d waiting there", needs))
	}
	return out
}

// isChildRow says r is a CHILDREN row of the program dashboard: enter
// shows that member's dashboard.
func isChildRow(r row) bool { return strings.HasPrefix(r.key, "pc:") }

// newProgram asks for a new program's name, then its goal (empty for
// none), and makes it as tm program new does, its clash checks with
// it (a project, a program or a paired server of that name refuses
// it); then it shows the program's dashboard and opens its settings,
// where its members are picked. The PROJECTS header's new item, while
// programs are on (the programs design §11.1).
func (m *dash) newProgram() {
	m.promptNo("New program", "Its name (a lower-case slug no project or paired server has):", "acme", "no program made", 0, func(slug string) tea.Cmd {
		in := &inputView{title: "New program: " + slug, label: "Its goal, one line (enter with none leaves it empty):", keep: true,
			submit: func(goal string) tea.Cmd {
				src := m.src
				return whenLoaded(func() func(m *dash) tea.Cmd {
					made, err := src.NewProgram(slug, strings.TrimSpace(goal))
					var d Data
					if err == nil {
						d = src.Load() // with the program in it
					}
					return func(m *dash) tea.Cmd {
						if err != nil {
							m.fail(err)
							return nil
						}
						// The program in the data now, for its settings;
						// the polls go on as they were (setData's tick
						// is theirs).
						_ = m.setData(d)
						m.msg = "created program " + made + "; pick its members in its settings"
						show := m.showProject(programKey(made))
						m.openProgramSettings(made)
						return show
					}
				})
			}}
		m.push(in)
		return nil
	})
}
