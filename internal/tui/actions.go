package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/project"
)

// Actions: the list's keys. One table drives the key handling, the help
// and the footer, so they can't disagree.

// An action is a key on the list and what it does.
type action struct {
	// id is its stable name (keybind.go): what a keymap binds and the
	// action binding names. One id is one behaviour, whatever its keys.
	id string
	// keys are its default keys; the first is the one it runs with, as
	// the remap layer hands it the default key whichever key was typed
	// (keybind.go). fixed actions can't be rebound.
	keys  []string
	fixed bool
	// label and help are its line in the help; an empty label leaves it
	// out, or joins its keys to the line above when help is "" too
	// (keyLabel).
	label, help string
	// menu is its entry in the ≡ menu ("" for none): the mouse's way to
	// every action. mouse is its mouse path instead, for those that
	// aren't in the menu.
	menu  string
	mouse string
	// foot is its word in the footer for the selected row (ok is false
	// without one), "" to leave it out there.
	foot func(m *dash, r row, ok bool) string
	run  func(m *dash, key string) tea.Cmd
}

// always shows an action in the footer as word.
func always(word string) func(*dash, row, bool) string {
	return func(*dash, row, bool) string { return word }
}

// withProject shows an action in the footer as word when there is a
// project to apply it to.
func withProject(word string) func(*dash, row, bool) string {
	return func(m *dash, _ row, _ bool) string {
		if m.projectHere() == "" {
			return ""
		}
		return word
	}
}

// actions is set in init: the ? action opens the help, which lists the
// actions (keymap.go).
var actions []action

func init() {
	actions = []action{
		{id: "list.up", keys: []string{"up"}, mouse: "the wheel, or a click on a row", run: func(m *dash, _ string) tea.Cmd { m.move(-1); return nil }},
		{id: "list.down", keys: []string{"down"}, mouse: "the wheel, or a click on a row", run: func(m *dash, _ string) tea.Cmd { m.move(1); return nil }},
		{id: "list.page-up", keys: []string{"pgup"}, mouse: "the wheel", run: (*dash).page},
		{id: "list.page-down", keys: []string{"pgdown"}, mouse: "the wheel", run: (*dash).page},
		{id: "focus.next", keys: []string{"tab"}, fixed: true, label: "tab shift+tab", help: "the keyboard to the next / previous area: the list, the details panel, the projects sidebar (its keys below); esc back to the list",
			mouse: "a click on the area", run: (*dash).cycleFocus},
		{id: "focus.prev", keys: []string{"shift+tab"}, fixed: true, mouse: "a click on the area", run: (*dash).cycleFocus},
		{id: "place.last", keys: []string{";"}, label: ";", help: "back to the last place: the area, sidebar row or session the keyboard was in before (prefix+; anywhere)",
			menu: "back to the last place", run: (*dash).goLastPlace},
		{id: "row.open", keys: []string{"enter"}, label: "enter", help: "attach to the selected session; on a project, open its coordinator; a task in NEEDS YOU shows in the project popup; questions waiting there open in the coordinator's question dialog; a held message or a file offered opens its dialog, a link request, a paused link or a paired server's share request the Network popup at its row; a paired server's need in the all-projects NEEDS YOU shows its read-only dashboard",
			menu: "open the selected row",
			foot: func(_ *dash, r row, ok bool) string {
				switch {
				case !ok:
					return ""
				case r.task != nil, r.remote != "":
					return "show"
				case r.card:
					return "open"
				case r.questions > 0:
					return "answer"
				case r.links > 0:
					return "open"
				case r.session != "":
					return "attach"
				case r.thread == nil && r.project != "":
					return "open"
				}
				return ""
			},
			run: (*dash).enter},
		{id: "session.adopt", keys: []string{"O"}, label: "O", help: "on an agent session outside the projects (one you started yourself): ask the current project's coordinator to adopt it as a thread (asks first)",
			menu: "adopt as a thread",
			foot: func(m *dash, r row, ok bool) string {
				if _, why := m.adoptable(r, ok); why == "" {
					return "adopt"
				}
				return ""
			},
			run: (*dash).adopt},
		{id: "project.activate", keys: []string{"S"}, label: "S", help: "activate the project shown (named in the header of its section, its state at the right), or deactivate it: its coordinator and threads stop, to resume when you activate it; asks first while they run",
			menu: "activate / deactivate the project",
			foot: func(m *dash, _ row, _ bool) string {
				if p := m.projectData(m.projectHere()); p != nil && p.Safety != nil {
					return activeWord(!p.inactive())
				}
				return ""
			},
			run: (*dash).toggleHere},
		{id: "card.fold", keys: []string{"Z"}, label: "Z", help: "on a mod's card (below the sections): fold it to its header, or unfold it",
			menu: "fold / unfold the mod card",
			foot: func(m *dash, r row, ok bool) string {
				if !ok || !r.card {
					return ""
				}
				if m.mv.folded[r.key] {
					return "unfold"
				}
				return "fold"
			},
			run: (*dash).foldCard},
		{id: "project.new", keys: []string{"+"}, label: "+", help: "new project (n too while there is none)", menu: "new project",
			foot: func(m *dash, _ row, _ bool) string {
				if len(m.data.Projects) == 0 {
					return "new project"
				}
				return ""
			},
			run: (*dash).newProject},
		{id: "project.popup", keys: []string{"A"}, label: "A", help: "the project popup: overview, inbox, tasks, settings and keys", menu: "project popup",
			foot: withProject("project"), run: (*dash).projectPopup},
		{id: "project.tasks", keys: []string{"T"}, label: "T", help: "the project popup on its Tasks tab; enter shows a task; D delegates, A accepts, X sends back (asking the coordinator); c opens it", menu: "tasks",
			foot: withProject("tasks"), run: (*dash).taskTab},
		{id: "project.inbox", keys: []string{"I"}, label: "I", help: "the Inbox tab of the project popup: what the coordinator is told about", menu: "inbox",
			foot: withProject("inbox"), run: (*dash).inbox},
		{id: "access.show", keys: []string{"E"}, label: "E", help: "effective access: on a thread, what it launched with (RESTART TO APPLY marks what the settings changed since); elsewhere, what a new thread of the project gets: every sandbox, prompt, guard and secret rule with where it comes from, read-only (tm access show)",
			menu: "effective access", run: (*dash).accessHere},
		{id: "project.next", keys: []string{"]"}, label: "] [", help: "next / previous project's coordinator", menu: "next project",
			run: func(m *dash, _ string) tea.Cmd { return m.cycleProject(true) }},
		{id: "project.prev", keys: []string{"["}, menu: "previous project",
			run: func(m *dash, _ string) tea.Cmd { return m.cycleProject(false) }},
		{id: "list.narrower", keys: []string{"<"}, label: "< >", help: "shrink / grow the list beside or above the details panel (or drag the divider)", menu: "smaller list",
			run: (*dash).resize},
		{id: "list.wider", keys: []string{">"}, menu: "larger list", run: (*dash).resize},
		{id: "sidebar.narrower", keys: []string{"{"}, label: "{ }", help: "narrow / widen the projects sidebar (or drag its border)", menu: "narrower sidebar",
			run: (*dash).sideKey},
		{id: "sidebar.wider", keys: []string{"}"}, menu: "wider sidebar", run: (*dash).sideKey},
		{id: "sidebar.slim", keys: []string{"B"}, label: "B", help: "the sidebar: full, a slim strip (glyphs and short names), hidden, full again", menu: "sidebar: slim / hide / full",
			run: (*dash).sideKey},
		{id: "details.toggle", keys: []string{"|", "D"}, label: "| D", help: "show or hide the details panel, where the layout puts it: right of the list (dashboards 120 columns or wider) or under it", menu: "details panel on / off",
			run: (*dash).toggleDetails},
		{id: "settings.open", keys: []string{","}, label: ",", help: "settings for every project: the prefix key, the default agent, the layout (a project's own are under A)", menu: "settings",
			foot: always("settings"), run: func(m *dash, _ string) tea.Cmd { m.openSettings(); return nil }},
		{id: "network.open", keys: []string{"N"}, label: "N", help: "the Network popup: this server and the paired ones, with their connection; the projects you share and those they share with you; pair, name, panes, relay and unpair (a click on a server's header in the sidebar opens it on that server)", menu: "network",
			run: func(m *dash, _ string) tea.Cmd { m.openNetwork(""); return nil }},
		{id: "pane.repaint", keys: []string{"R"}, help: "a paired server's pane: repaint from its server (prefix+R; listed with the prefix commands)", mouse: "the session's " + menuButton + " menu in a paired server's pane: repaint from its server",
			run: func(m *dash, _ string) tea.Cmd {
				m.msg = "prefix+" + keyShown(boundKey("R")) + " repaints a paired server's pane: open one first"
				return nil
			}},
		{id: "notices.open", keys: []string{"M"}, label: "M", help: "the notifications from mods, while there are any: D dismisses one, 1 2 3 press its buttons (asks first)", menu: "notifications from mods",
			run: func(m *dash, _ string) tea.Cmd {
				if len(m.data.ModNotices) == 0 {
					m.msg = "no notifications from mods"
					return nil
				}
				m.openNotices()
				return nil
			}},
		{id: "help.open", keys: []string{"?"}, label: "?", help: "help", menu: "help: keys and mouse",
			foot: always("help"), run: func(m *dash, _ string) tea.Cmd { m.push(&helpView{}); return nil }},
		{id: "console.quit", keys: []string{"prefix+q"}, fixed: true, label: "prefix+q", help: "quit this console, from anywhere (the server, the sessions and other consoles keep running)", menu: "quit",
			foot: always("quit"), run: func(m *dash, _ string) tea.Cmd { m.quitting = true; return tea.Quit }},
	}
}

// page moves the selection ten rows up (pgup) or down.
func (m *dash) page(key string) tea.Cmd {
	for range 10 {
		m.move(scrollKeys[key] / 10)
	}
	return nil
}

// listKey runs the list's action for key, a default key (keyLayer.canon
// turns a typed key into one), if it has one.
func (m *dash) listKey(key string) tea.Cmd {
	for _, a := range actions {
		if runsAs(a.id, a.keys, key) {
			return a.run(m, runKey(a.id, a.keys))
		}
	}
	if key == "n" && len(m.data.Projects) == 0 && !m.remoteHere() {
		// The first run: n still makes the first project.
		return m.newProject(key)
	}
	return nil
}

// footKeys is the footer's key list: the ≡ menu, then the actions that
// apply to the selected row. Each is a button for the mouse too.
func (m *dash) footKeys() string {
	if m.remoteHere() {
		return m.remoteFootKeys()
	}
	r, ok := m.selected()
	out := []string{menuButton + " menu"}
	_, _, through := m.typeThroughRow()
	if through {
		// Typing writes to the coordinator: the list's keys are the
		// prefix's there.
		out = append(out, typeThroughHint)
	}
	for _, a := range actions {
		if a.foot == nil {
			continue
		}
		if word := a.foot(m, r, ok); word != "" {
			label := footLabel(a)
			if through && utf8.RuneCountInString(label) == 1 {
				label = "prefix+" + label
			}
			out = append(out, label+" "+word)
		}
	}
	return strings.Join(out, " · ")
}

func (m *dash) enter(string) tea.Cmd {
	r, ok := m.selected()
	switch {
	case !ok:
	case r.task != nil:
		return m.showTask(r.project, r.task.ID)
	case r.card:
		return m.openCard(r.key)
	case r.remote != "":
		return m.showProject(r.remote)
	case r.questions > 0:
		return m.answerQuestions(r.project)
	case r.links > 0:
		return m.openLinkNeed(r.project, r.other, r.linkNeed)
	case r.netAt != "":
		m.openNetwork(r.netAt)
	case r.session != "":
		return m.act(func() actionMsg { return actionMsg{attach: r.session, current: r.project} })
	case r.thread != nil:
		name := threadName(r.thread.Task, r.thread.ID)
		m.msg = name + " has no running session; the coordinator restarts it (tm thread restart " + name + ")"
	case r.project != "":
		return m.openProject(r.project)
	}
	return nil
}

func (m *dash) newProject(string) tea.Cmd {
	m.promptNo("New project", "Its name (becomes a lower-case slug):", "demo-app", "", 0, func(slug string) tea.Cmd {
		src := m.src
		return m.act(func() actionMsg {
			slug, err := src.NewProject(slug)
			msg := "created project " + slug + "; enter starts its coordinator"
			if len(src.Agents()) == 0 {
				msg = "created project " + slug + "; no agent is installed, so it can't run a coordinator or threads until one is (tm doctor)"
			}
			return actionMsg{sel: "p:" + slug, msg: msg, err: err}
		})
	})
	return nil
}

// needProject is the project the key is about, or "" with a hint in the
// footer when there is none.
func (m *dash) needProject() string {
	slug := m.projectHere()
	if alias, peerSlug, ok := m.remote(); ok && slug == "" {
		m.msg = remoteNothing(peerShown(m.data.Peers, alias), peerSlug)
	} else if slug == "" {
		m.msg = "no project; " + keyShown(boundKey("+")) + " creates one"
	}
	return slug
}

// taskTab is the project popup with the Tasks tab selected.
func (m *dash) taskTab(string) tea.Cmd {
	if slug := m.needProject(); slug != "" {
		return m.showTask(slug, 0)
	}
	return nil
}

func (m *dash) inbox(string) tea.Cmd {
	if slug := m.needProject(); slug != "" {
		return m.showTab(slug, tabInbox)
	}
	return nil
}

// resize steps the list's share beside the details panel (right) or
// above it (bottom): < makes the list smaller, > larger.
func (m *dash) resize(key string) tea.Cmd {
	g := m.geom()
	l, d := m.layout, splitStep
	if key == "<" {
		d = -d
	}
	switch g.panel() {
	case placeRight:
		if l.detCells != 0 {
			// A panel the layout gives in cells steps from its share now.
			l.Split, l.detCells = float64(g.list().w)/float64(max(g.body().w, 1)), 0
		}
		l.Split += d
	case placeBottom:
		l.DetailsRows = l.rows()
		if l.rowCells != 0 {
			// As for cells: rows step from the panel's share now.
			l.DetailsRows, l.rowCells = 1-float64(g.list().h)/float64(max(g.body().h, 1)), 0
		}
		l.DetailsRows -= d
	default:
		m.msg = m.noPanel()
		return nil
	}
	m.setLayout(l)
	return nil
}

// toggleDetails turns the details panel off, or on at its place; the
// footer says why it doesn't show when the window has no room for it.
func (m *dash) toggleDetails(string) tea.Cmd {
	l := m.layout
	l.Details = !l.Details
	m.setLayout(l)
	if l.Details && m.panel() == "" {
		m.msg = m.noPanel()
	}
	return nil
}

// noPanel says why the details panel doesn't show.
func (m *dash) noPanel() string {
	switch m.layout.place() {
	case placeOff:
		return "the details panel is off; " + boundKey("|") + " turns it on"
	case placeBottom:
		return fmt.Sprintf("the details panel shows under the list when the dashboard has %d rows or more between its header and footer; here a thread's details show under its row", bottomMin)
	}
	return fmt.Sprintf("the details panel shows right of the list when the dashboard (the window less the sidebar) is %d columns or wider; here a thread's details show under its row", splitMin)
}

func (m *dash) sideKey(key string) tea.Cmd {
	l := m.layout
	var msg string
	l.Sidebar, msg = l.Sidebar.Key(key, m.winW)
	m.setLayout(l)
	if msg != "" {
		m.msg = msg
	}
	return nil
}

// questionsWaiting is "2 questions waiting" (project.QuestionsWaiting),
// for where a project variable hides the package.
func questionsWaiting(n int) string { return project.QuestionsWaiting(n) }

// answerQuestions has slug's coordinator open its questions in its
// question dialog; the footer says what came of it.
func (m *dash) answerQuestions(slug string) tea.Cmd {
	src := m.src
	return m.act(func() actionMsg {
		res, err := src.OpenQuestions(slug)
		if err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{msg: fmt.Sprintf("asked %s's coordinator to open its %s; the dialog shows in its pane once it is free",
			slug, strings.TrimSuffix(project.QuestionsWaiting(res.Open), " waiting"))}
	})
}

// saveClear has slug's coordinator save what it learned, then clears its
// conversation (the context hint's key); the footer says what came of it.
func (m *dash) saveClear(slug string) tea.Cmd {
	src := m.src
	return m.act(func() actionMsg {
		res, err := src.SaveClear(slug)
		return actionMsg{msg: saveClearWords(slug, res, err)}
	})
}

// activeWord is what space does to a project that is on (active) or not.
func activeWord(active bool) string {
	if active {
		return "deactivate"
	}
	return "activate"
}

// toggleHere activates or deactivates the project shown, as space does
// on its row in the sidebar.
func (m *dash) toggleHere(string) tea.Cmd {
	p := m.projectData(m.projectHere())
	if p == nil || p.Safety == nil {
		m.msg = "no project; " + keyShown(boundKey("+")) + " creates one"
		return nil
	}
	return m.toggleActive(p.Slug, p.inactive())
}
