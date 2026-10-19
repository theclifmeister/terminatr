package tui

import (
	"os"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/modaction"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// Asking the coordinator (docs/SPEC.md §4): keys on a task in the Tasks
// tab ask the project's coordinator to act on it. The
// dashboard still changes no task: it drops an inbox item, which the
// coordinator takes as the user's word. D delegates an open, ready or
// blocked task, A accepts a task in review, x sends one back with a
// note (a done one too, which the coordinator reopens); c opens the
// coordinator, to answer what a task is blocked on.

// notAskable is why kind can't be asked for task t, "" when it can: the
// action binding's check (modaction), so a mod's bound D, A or x and the
// Tasks tab's agree.
func notAskable(t *tasks.Task, kind string) string { return modaction.NotAskable(t, kind) }

// delegateWaiting is what a task's row says while an item asks the
// coordinator something about it.
const delegateWaiting = "waiting on the coordinator"

// waitingFor says what the coordinator is asked, for kind's item.
func waitingFor(kind string) string {
	switch kind {
	case project.KindAccept:
		return delegateWaiting + " to accept it"
	case project.KindSendBack:
		return delegateWaiting + " to send it back"
	}
	return delegateWaiting + " to delegate it"
}

// asked is the kind of the item that asks slug's coordinator about task
// t, "" for none: while there is one, the task waits on the coordinator.
func (m *dash) asked(slug string, t *tasks.Task) string {
	if p := m.projectData(slug); p != nil {
		return project.TaskAsked(p.Items, t.Ref())
	}
	return ""
}

// loaded tells whether t is a task from a loaded board.
func loaded(t *tasks.Task) bool { return t != nil && (t.Title != "" || t.Status != "") }

// canAsk checks before asking: the footer says why when kind can't be
// asked for t (with what the key does, hint), or when an item already
// asks something of it.
func (m *dash) canAsk(slug string, t *tasks.Task, kind, hint string) bool {
	if !loaded(t) {
		return false // the board isn't loaded yet
	}
	if why := notAskable(t, kind); why != "" {
		m.msg = why + "; " + hint
		return false
	}
	if k := m.asked(slug, t); k != "" {
		m.msg = t.Ref() + " is already " + waitingFor(k)
		return false
	}
	return true
}

// askCoordinator drops kind's item for slug's task t; the footer says
// done. b is the mod binding pressed, nil for the core's own key.
func (m *dash) askCoordinator(slug string, t *tasks.Task, kind, note, done string, b *binding) tea.Cmd {
	src, ref, id := m.src, t.Ref(), t.ID
	return m.actVia(b, func() (actionMsg, bool) {
		asked, err := src.Ask(slug, id, kind, note)
		if err != nil {
			return actionMsg{err: err}, false
		}
		if !asked {
			return actionMsg{msg: ref + " is already " + delegateWaiting}, false
		}
		return actionMsg{msg: done}, true
	})
}

// askAfterConfirm asks the user to confirm, then has askCoordinator drop
// kind's item for slug's task t. hint is canAsk's refusal key hint; title
// and question word the confirmation, no the footer after a no, done the
// footer after the ask. b is the mod binding pressed, nil for the core's
// own key.
func (m *dash) askAfterConfirm(slug string, t *tasks.Task, b *binding, kind, hint, title, question, no, done string) tea.Cmd {
	if !m.canAsk(slug, t, kind, hint) {
		return nil
	}
	m.confirmNo(title, question, no, func() tea.Cmd {
		return m.askCoordinator(slug, t, kind, "", done, b)
	})
	return nil
}

// delegate asks, then has the coordinator delegate slug's task t. b is
// the mod binding pressed, nil for the core's own D (and so for accept
// and sendBack).
func (m *dash) delegate(slug string, t *tasks.Task, b *binding) tea.Cmd {
	ref := t.Ref()
	var repos []string
	if p := m.projectData(slug); p != nil {
		repos = p.Repos
	}
	return m.askAfterConfirm(slug, t, b, project.KindDelegate, "D delegates open, ready and blocked tasks",
		"Delegate "+ref, "Delegate "+ref+" to the coordinator? It starts a thread for "+ref+" "+oneLine(t.Title)+delegateWhere(t, repos)+".",
		ref+" not delegated", "asked the coordinator to delegate "+ref+"; it starts a thread for it")
}

// delegateWhere says where the thread for t works, for D's question:
// the task's repo, a folder without git, or, in a project of several
// repos when t names none, that the coordinator asks which (tm task
// delegate refuses to guess).
func delegateWhere(t *tasks.Task, repos []string) string {
	switch {
	case t.Repo == tasks.NoRepo:
		return " in a folder without git"
	case t.Repo != "":
		return " in " + oneLine(t.Repo)
	case len(repos) > 1:
		return "; it names none of the project's repos, so the coordinator asks you which"
	}
	return ""
}

// accept asks, then tells the coordinator the user accepts slug's task
// t in review: it marks the task done.
func (m *dash) accept(slug string, t *tasks.Task, b *binding) tea.Cmd {
	ref := t.Ref()
	return m.askAfterConfirm(slug, t, b, project.KindAccept, "A accepts tasks in review",
		"Accept "+ref, "Accept "+ref+"? The coordinator marks "+ref+" "+oneLine(t.Title)+" done.",
		ref+" not accepted", "told the coordinator you accept "+ref+"; it marks it done")
}

// sendBack asks for a note, then has the coordinator send slug's task t
// back to work with it: one in review, or a done one, which it reopens.
func (m *dash) sendBack(slug string, t *tasks.Task, b *binding) tea.Cmd {
	if !m.canAsk(slug, t, project.KindSendBack, "x sends back tasks in review or done") {
		return nil
	}
	ref := t.Ref()
	m.promptNo("Send "+ref+" back", "What should change? The coordinator passes your note on.", ref+" not sent back", project.MaxSendBackNote, func(note string) tea.Cmd {
		return m.askCoordinator(slug, t, project.KindSendBack, note, "sent "+ref+" back with your note; the coordinator passes it on", b)
	})
	return nil
}

// taskKey runs a task key on slug's task t: D, A, x or c (none of
// them a prefix command, docs/SPEC.md §4).
func (m *dash) taskKey(slug string, t *tasks.Task, key string) tea.Cmd {
	switch key {
	case "D":
		return m.delegate(slug, t, nil)
	case "A":
		return m.accept(slug, t, nil)
	case "x":
		return m.sendBack(slug, t, nil)
	case "c":
		return m.openProject(slug)
	}
	return nil
}

// taskKeys is the footer's key list for task t, before the view's own
// keys: the one table of which task keys work in which status, for the
// Tasks tab and a shown task alike (each key only where it
// works, as notAskable says). Each list, with the view's keys, fits the
// 60 columns a full sidebar leaves at the least (view.SideRoom).
func taskKeys(t *tasks.Task) string {
	if t == nil {
		return ""
	}
	switch t.Status {
	case tasks.Review:
		return "A accept · x send back"
	case tasks.Done:
		return "x send back"
	case tasks.Blocked:
		return "c coordinator · D delegate"
	case tasks.Open, tasks.Ready:
		return "D delegate"
	}
	return ""
}

// blockedOn is what task t is blocked on: its latest "blocked (date):"
// note, "" when it has none.
func blockedOn(t *tasks.Task) string {
	paras := strings.Split(strings.TrimSpace(t.Notes), "\n\n")
	for i := len(paras) - 1; i >= 0; i-- {
		p := strings.TrimSpace(paras[i])
		if rest, ok := strings.CutPrefix(p, string(tasks.Blocked)+" ("); ok {
			if _, note, ok := strings.Cut(rest, "): "); ok {
				return oneLine(note)
			}
		}
	}
	return ""
}

// statusNoteRE is a status note's start: "review (2026-10-05): ".
var statusNoteRE = regexp.MustCompile(`^[a-z]+ \([0-9-]+\): `)

// noteChecks are the task notes that say how to check it: a paragraph
// starting "Check:", "To check:" or "How to check:" (after a status
// note's "review (date): ").
func noteChecks(t *tasks.Task) []string {
	var out []string
	for _, p := range strings.Split(strings.TrimSpace(t.Notes), "\n\n") {
		p = strings.TrimSpace(p)
		p = statusNoteRE.ReplaceAllString(p, "")
		low := strings.ToLower(p)
		for _, pre := range []string{"check:", "to check:", "how to check:"} {
			if strings.HasPrefix(low, pre) {
				if s := oneLine(strings.TrimSpace(p[len(pre):])); s != "" {
					out = append(out, s)
				}
				break
			}
		}
	}
	return out
}

// adoptable is the session of row r that T can ask to adopt, or why
// not (docs/SPEC.md §4, Adopt): an agent session outside the projects,
// with a current project to adopt it into.
func (m *dash) adoptable(r row, ok bool) (proto.SessionInfo, string) {
	if !ok || r.session == "" {
		return proto.SessionInfo{}, "T adopts an agent session outside the projects as a thread; select one"
	}
	s, found := m.session(r.session)
	switch {
	case !found:
		return s, r.session + " is gone"
	case s.Project != "" || s.Role != proto.RoleShell:
		return s, r.session + " is a project's session already"
	case s.Agent == "" || s.State == "exited":
		return s, "no agent runs in " + r.session + "; only an agent session becomes a thread"
	case m.projectHere() == "":
		return s, "no project to adopt " + r.session + " into; n creates one"
	}
	return s, ""
}

// adoptAsked reports whether an inbox item of slug's asks its
// coordinator to adopt session id.
func (m *dash) adoptAsked(slug, id string) bool {
	if p := m.projectData(slug); p != nil {
		for _, it := range p.Items {
			if it.Kind == project.KindAdopt && it.Subject == id {
				return true
			}
		}
	}
	return false
}

// adopt asks, then has the current project's coordinator adopt the
// selected agent session as a thread.
func (m *dash) adopt(string) tea.Cmd {
	r, ok := m.selected()
	s, why := m.adoptable(r, ok)
	if why != "" {
		m.msg = why
		return nil
	}
	slug, src := m.projectHere(), m.src
	if m.adoptAsked(slug, s.ID) {
		// T again takes the go-ahead back (T335): without it the
		// coordinator can't adopt the session.
		question := "Withdraw your go-ahead to adopt " + s.ID + " as a thread of " + slug + "? The coordinator can't adopt it then."
		m.confirmNo("Withdraw "+s.ID, question, s.ID+" still asked", func() tea.Cmd {
			return m.act(func() actionMsg {
				if err := src.WithdrawAdopt(slug, s.ID); err != nil {
					return actionMsg{err: err}
				}
				return actionMsg{msg: "withdrew the go-ahead to adopt " + s.ID}
			})
		})
		return nil
	}
	home, _ := os.UserHomeDir()
	question := "Adopt " + s.ID + " as a thread of " + slug + "? The coordinator links it to a task and briefs it.\n\n" + adoptWhere(s, home)
	m.confirmNo("Adopt "+s.ID, question, s.ID+" not adopted", func() tea.Cmd {
		return m.act(func() actionMsg {
			asked, err := src.AskAdopt(slug, s)
			if err != nil {
				return actionMsg{err: err}
			}
			if !asked {
				return actionMsg{msg: s.ID + " is already " + delegateWaiting + " to adopt it"}
			}
			return actionMsg{msg: "asked the coordinator to adopt " + s.ID}
		})
	})
	return nil
}
