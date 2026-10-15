package tui

import (
	"cmp"
	"errors"
	"os"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/view"
)

// The dashboard (docs/SPEC.md §4): the current project's NEEDS YOU, then
// the project with its coordinator and threads, then the other
// sessions; the sidebar's PROJECTS header shows every project's NEEDS
// YOU instead (allProjects). It
// is a client with no state of its own: it polls the server and the
// project files every second. Attaching leaves the dashboard; the caller
// runs the attach view and opens the dashboard again afterwards.
//
// The list's rows are built in rows.go, its keys are the actions in
// actions.go, and the views opened on top of it are the overlays in
// overlays.go.

// refresh is how often the dashboard polls; an open task list reloads
// every boardRefresh (it asks git where reviews stand), so no key
// refreshes.
const (
	refresh      = time.Second
	boardRefresh = 5 * time.Second
)

// DashState is what survives an attach: the selected row, the project
// last attached to (for ] and [), and a message for the footer.
type DashState struct {
	Selected string
	Current  string
	Message  string
	// Then is a key to run once the first poll is in: the key typed
	// after the prefix in a session (p, ] or [).
	Then string
	// SideFocus starts the dashboard with the keyboard in the sidebar,
	// where the session left it.
	SideFocus bool
}

// DashOptions configure Dashboard.
type DashOptions struct {
	Source Source
	In     *os.File
	Out    *os.File
	// Cwd is the default directory for adding a repository.
	Cwd   string
	State DashState
	// Width and Height size the first frame before the terminal reports
	// its size (tests).
	Width, Height int
	// UIFile is ui.json, where the layout is kept; empty keeps it in
	// memory only (tests).
	UIFile string
	// Prefix is the prefix key ("ctrl+b"); empty is the default.
	Prefix string
	// View is the server-owned view the dashboard is a screen of
	// (docs/SPEC.md §3.3): its selection, current project and sidebar are
	// the view's, and the dashboard ends when the view shows sessions,
	// from this console or another. Nil keeps them here (tests).
	View *ViewConn
	// Over runs the dashboard as a popup over a session instead: see
	// Over.
	Over *Over
	// Handover: an attach handed this console over with the alternate
	// screen on, showing its last frame (Options.Back): the dashboard
	// draws over it once it has its data (dashOut). Whatever the
	// handover, a dashboard that ends attaching leaves the screen on for
	// the attach to draw over (Options.Handover).
	Handover bool
}

// DashResult says why the dashboard ended: Attach names a session to
// attach to (with a View: the view shows sessions now); empty means the
// user quit.
type DashResult struct {
	Attach string
	State  DashState
	// Message is the footer's message when a popup over a session closed
	// (Over), for the session's status bar: a key that couldn't open its
	// popup says why there.
	Message string
	// SideFocus: the sidebar has the keyboard, and keeps it in the
	// session attached (a click on a sidebar row attached it, say).
	SideFocus bool
	// Command is a prefix command for the session to run once attached
	// again: prefix then r or tab over it, which are the session's own.
	Command string
}

// Dashboard runs the dashboard until the user quits or picks a session.
func Dashboard(opts DashOptions) (DashResult, error) {
	err := loadLook()
	spec, lerr := configLayout()
	if n := lookNews(errors.Join(err, lerr)); n != "" && opts.State.Message == "" {
		opts.State.Message = n
	}
	m := newDash(opts)
	// The sidebar's sections the attached console showed, from its
	// last mod.views, show here at once (handViews).
	m.mv.list = handedViews()
	// Safe mode puts its own sidebar on the view, whatever a layout
	// left there.
	m.useLayout(layoutValue(), spec, m.safe)
	cols := layoutColumns(spec)
	m.wantCols = &cols // a layout that doesn't load: the built-in order
	defer close(m.done)
	if o := opts.Over; o != nil && o.Session != "" && opts.View != nil {
		// The session keeps drawing under the popup.
		if f, err := followPane(opts.View.paths, o.Session); err == nil {
			m.feed = f
			defer func() {
				if m.feed != nil {
					m.feed.close()
				}
			}()
		}
	}
	if m.stopWatch != nil {
		defer m.stopWatch()
	}
	probeTerminal(opts.In, opts.Out, false)
	m.out = newDashOut(opts.Out, opts.Handover)
	popts := []tea.ProgramOption{tea.WithInput(opts.In), tea.WithOutput(m.out)}
	if probedTruecolor.Load() {
		// Bubble Tea reads the colour profile from the environment only.
		popts = append(popts, tea.WithColorProfile(colorprofile.TrueColor))
	}
	if opts.Width > 0 && opts.Height > 0 {
		popts = append(popts, tea.WithWindowSize(opts.Width, opts.Height))
	}
	final, err := tea.NewProgram(m, popts...).Run()
	m.out.flush()
	if err != nil {
		opts.Out.WriteString(leaveScreen)
		return DashResult{}, err
	}
	d := final.(*dash)
	if d.result.Attach == "" {
		opts.Out.WriteString(leaveScreen) // quit: dashOut kept the screen
	}
	if d.mv.sent != "" {
		// The dashboard and its popups are gone: their mod views close,
		// but the sidebar's sections while a session attaches next,
		// whose sidebar shows them on.
		var keep []modview.Shown
		if d.result.Attach != "" {
			keep = sidebarShown(d.shownViews())
		}
		_, _ = d.src.ModViews(keep)
	}
	d.result.State = DashState{Selected: d.sel, Current: d.current}
	d.result.SideFocus = d.result.Attach != "" && d.focus == areaSide
	return d.result, nil
}

type dash struct {
	treeMemo []treeRow // tree's rows while only the mouse hovers (treeHold)
	treeHold bool      // the last message was a hover: tree may be reused
	lastView tea.View  // the last View, reused while hold says nothing changed
	lastOK   bool      // lastView is set
	hold     bool      // the last message was a hover that changed nothing
	src      Source
	out      *dashOut // nil in tests
	cwd      string
	w, h     int // the dashboard's own area: the window less the sidebar
	winW     int // the window's width

	data    Data
	loaded  bool
	rows    []row
	sel     string // key of the selected row
	current string // the current project: the one listed (listProject), or a paired server's shown (peerdash.go)
	// remoteTop is the remote dashboard's first line shown.
	remoteTop int
	// focus is the area with this console's keyboard: the list, the
	// sidebar or the details panel (tab). sideSel is the sidebar's
	// keyboard row, the view's (viewSync.sideSent).
	focus   area
	sideSel string
	// previewSeq counts the sidebar cursor's moves: only the last one's
	// preview shows (previewSoon).
	previewSeq uint64
	// folds are what the sidebar's tree folds: the view's, changed here
	// at once (refold).
	folds     view.Tree
	msg       string
	errMsg    string                  // msg when it reports a failure, drawn as one
	toastSeq  uint64                  // the last mod toast shown (showToasts)
	toasted   bool                    // a poll set toastSeq
	busy      bool                    // an action is running
	acts      int                     // actions started, so a poll knows whether one began since it did
	queued    []func(m *dash) tea.Cmd // settings keys pressed while it runs, in order
	catalogs  []Catalog               // the agents' models, read when the , popup opens (catalog.go)
	agents    []string                // the installed agents, read by agentList; nil until then or after a poll
	agentsOK  bool                    // agents is read
	stack     []overlay               // views open on top of the list, topmost last
	mv        modViews                // the views user mods draw (modviews.go)
	under     overlay                 // the popup the topmost draws over, while it draws
	geo       *boxGeo                 // where the topmost was drawn, for the mouse
	lastClick click                   // the last left click, for double-clicks
	shownKeys string                  // the footer's hints as drawn: its buttons
	badgeAt   [2]int                  // the header's needs-you badge as drawn: its columns, [0 0] for none

	detailsState
	layoutState

	prefix   string    // the prefix key, as tea names it
	prefixed bool      // the prefix was typed: the next key is a command
	quitting bool      // prefix+q: this console quits, popup or not
	then     string    // a key to run after the first poll
	boardAt  time.Time // when the open task list last reloaded on its own

	alerts uint64
	seen   bool // the first poll arrived (no bell for old alerts)
	// noticed are the mod notifications the console has seen, so each is
	// drawn once (modnotice.go).
	noticed map[string]bool
	// modsFailed are the mods whose failure the footer has said
	// (noteModFailures).
	modsFailed map[string]bool

	viewSync
	overState
	sized bool // the first window size came
	done  chan struct{}

	result DashResult
}

func newDash(o DashOptions) *dash {
	w, h := o.Width, o.Height
	if w <= 0 || h <= 0 {
		w, h = 80, 24
	}
	m := &dash{src: o.Source, cwd: o.Cwd, h: h,
		sel: o.State.Selected, current: o.State.Current, msg: o.State.Message,
		layoutState: layoutState{ui: LoadLayout(o.UIFile), uiFile: o.UIFile, safe: safeMode()},
		prefix:      cmp.Or(o.Prefix, DefaultPrefixKey), then: o.State.Then, done: make(chan struct{}),
		overState: overState{over: o.Over}}
	if o.Over != nil {
		m.then = o.Over.Key
		if o.Over.View {
			// Wait a little for the mods' views to show the one opened.
			m.mv.seen, m.awaitView = o.Over.After, time.Now().Add(awaitView)
		}
	}
	if o.State.SideFocus {
		m.focus = areaSide
	}
	m.layout = effectiveLayout(m.spec, m.ui, m.layoutID)
	m.folds = m.layout.Tree
	if vc := o.View; vc != nil {
		v := vc.View()
		m.view, m.viewSeq = vc, v.Seq
		if v.Selected != "p:"+allProjects {
			m.sel = cmp.Or(v.Selected, m.sel)
		}
		m.current = v.Current
		m.sideSel, m.sideSent = v.SideSel, v.SideSel
		m.folds, m.layout.Tree = v.Tree, v.Tree
		m.layout.Sidebar, m.viewSide = v.Sidebar, v.Sidebar
		m.sideCols = v.Columns
		m.layout.Info = v.Info
		m.watch, m.stopWatch = vc.Watch()
	}
	m.setWidth(w)
	return m
}

// setWidth takes the window's width; the dashboard gets what the sidebar
// leaves.
func (m *dash) setWidth(w int) {
	m.winW = w
	m.w = m.geom().main().w
}

// sideW is the sidebar's width in this window.
func (m *dash) sideW() int { return m.layout.Sidebar.Cols(m.winW) }

// dataMsg is a poll's result. acts is m.acts when the poll began, and
// busy whether an action ran then: a poll that began before an action or
// during one may have read the settings before its save landed.
type dataMsg struct {
	data Data
	acts int
	busy bool
}
type tickMsg struct{}

type boardMsg struct {
	slug  string
	board *tasks.Board
	// reviews are the board's tasks in review, by id.
	reviews map[int]Review
	err     error
	// library is the project's library files (the Library tab); libErr
	// is why they didn't load.
	library []thread.LibFile
	libOK   bool
	libErr  error
}

// actionMsg is the outcome of a key's action: attach to a session, or a
// message for the footer.
type actionMsg struct {
	attach  string
	current string
	sel     string
	msg     string
	err     error
	// catalogs: the agents' models changed (asked again, a refusal
	// forgotten); the Models page reads them again.
	catalogs bool
	// pick: the project that waits for the user to choose an agent.
	pick string
	// then runs on the dashboard once the action is done, before the
	// reload (a mod's update that asks first opens its question).
	then func(m *dash)
}

func (m *dash) load() tea.Cmd {
	src, acts, busy := m.src, m.acts, m.busy
	return func() tea.Msg { return dataMsg{data: src.Load(), acts: acts, busy: busy} }
}

func (m *dash) loadBoard(slug string) tea.Cmd {
	src := m.src
	return func() tea.Msg {
		b, err := src.Board(slug)
		msg := boardMsg{slug: slug, board: b, err: err, reviews: map[int]Review{}}
		if b != nil {
			for _, t := range b.Tasks {
				if t.Status == tasks.Review {
					msg.reviews[t.ID] = src.Review(slug, t)
				}
			}
		}
		return msg
	}
}

// loadPopup loads what slug's project popup shows from the project
// folder: its board, as loadBoard, and its library.
func (m *dash) loadPopup(slug string) tea.Cmd {
	board, src := m.loadBoard(slug), m.src
	return func() tea.Msg {
		msg := board().(boardMsg)
		msg.library, msg.libErr = src.Library(slug)
		msg.libOK = true
		return msg
	}
}

// reloadBoards reloads the open task lists, every boardRefresh.
func (m *dash) reloadBoards() tea.Cmd {
	if time.Since(m.boardAt) < boardRefresh {
		return nil
	}
	var cmds []tea.Cmd
	if pv := m.projectPopupView(); pv != nil {
		cmds = append(cmds, m.loadPopup(pv.slug))
	}
	if len(cmds) > 0 {
		m.boardAt = time.Now()
	}
	return tea.Batch(cmds...)
}

func (m *dash) Init() tea.Cmd {
	cmds := []tea.Cmd{m.load(), m.waitView(), m.waitFeed()}
	if themed() {
		// A mod's theme may differ light and dark: ask the terminal.
		cmds = append(cmds, tea.RequestBackgroundColor)
	}
	return tea.Batch(cmds...)
}

// Update runs update, then tells the view what this console changed in
// it: the selection and the sidebar.
func (m *dash) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.update(msg)
	if m.quitting {
		m.result.Attach = ""
		return m, tea.Quit
	}
	if m.overDone() {
		// The popup over the session closed: back to the session, with
		// what the footer said.
		m.result.Attach, m.result.Message = cmp.Or(m.over.Session, "view"), m.msg
		return m, tea.Quit
	}
	m.notePlace()
	if c := m.modViewsChanged(); c != nil {
		cmd = tea.Batch(cmd, c)
	}
	if m.view == nil {
		return m, cmd
	}
	cmds := []tea.Cmd{cmd}
	if m.userSel {
		m.userSel = false
		cmds = append(cmds, m.call(proto.MethodViewSelect, proto.ViewParams{Key: m.sel}))
	}
	if w := m.wantCols; w != nil {
		m.wantCols = nil
		if !slices.Equal(view.Columns(*w), view.Columns(m.view.View().Columns)) {
			cmds = append(cmds, m.call(proto.MethodViewColumns, proto.ViewParams{Columns: *w}))
		}
	}
	if c := m.wantCompact; c != nil {
		m.wantCompact = nil
		if *c != m.view.View().StatusCompact {
			cmds = append(cmds, m.call(proto.MethodViewStatus, proto.ViewParams{Compact: *c}))
		}
	}
	if l := m.layout.Sidebar; !m.sideDrag && l != m.viewSide {
		m.viewSide = l
		cmds = append(cmds, m.call(proto.MethodViewSidebar, proto.ViewParams{Sidebar: &l}))
	}
	return m, tea.Batch(cmds...)
}

func (m *dash) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Hovering changes nothing the tree is built from: the motions that
	// only hover, and the frames between them, share one tree (PERF-03).
	_, motion := msg.(tea.MouseMotionMsg)
	m.treeHold = motion && !m.sideDrag && !m.dragging
	m.hold = false
	m.treeMemo = nil
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		setScheme(msg.IsDark())
		return m, nil
	case tea.WindowSizeMsg:
		changed := msg.Width != m.winW || msg.Height != m.h
		m.h = msg.Height
		m.setWidth(msg.Width)
		if m.view != nil {
			// The first size is the window as it was; later ones are the
			// user resizing it (docs/SPEC.md §3.3). The same size again
			// is none: the terminal's report after the size the
			// dashboard started at (DashOptions.Width) would make this
			// console the view's latest.
			resize := m.sized && changed
			m.sized = true
			return m, m.call(proto.MethodViewSize, proto.ViewParams{Cols: uint16(msg.Width), Rows: uint16(msg.Height), Resize: resize})
		}
		return m, nil
	case viewMsg:
		return m, m.fromView()
	case feedMsg:
		return m, m.fromFeed()
	case previewMsg:
		return m, m.preview(msg.seq)
	case viewDoneMsg:
		if msg.sel {
			m.selPending--
		}
		if msg.tree {
			// The view has this console's change now (ViewConn.Do).
			if m.treePending--; m.treePending == 0 {
				t := m.view.View().Tree
				m.folds, m.layout.Tree = t, t
			}
		}
		if msg.side {
			m.sideBusy = false
			return m, m.sendSideSel()
		}
		if msg.err != nil && !errors.Is(msg.err, ErrViewDown) {
			m.fail(msg.err)
		}
		return m, nil
	case dataMsg:
		if msg.busy || msg.acts != m.acts {
			// It may be older than what the settings show, which they
			// set before its save lands: taken, it would undo a change
			// that keys waiting for the save then build on. The action's
			// end loads again; till then the polls go on.
			return m, tea.Tick(refresh, func(time.Time) tea.Msg { return tickMsg{} })
		}
		return m, m.setData(msg.data)
	case tickMsg:
		return m, tea.Batch(m.load(), m.reloadBoards(), m.pollModViews())
	case bindingMsg:
		return m, m.runBinding(msg)
	case modViewsMsg:
		return m, m.setModViews(msg)
	case modInputMsg:
		if msg.err != nil {
			m.fail(msg.err)
		}
		return m, nil
	case modCursorMsg:
		return m, m.cursorSent(msg)
	case boardMsg:
		if pv := m.projectPopupView(); pv != nil && pv.slug == msg.slug {
			if msg.err == nil {
				pv.setBoard(msg.board)
				pv.reviews = msg.reviews
			}
			if msg.libOK {
				pv.library, pv.libErr, pv.libOK = msg.library, msg.libErr, true
				pv.sel[tabLibrary] = min(pv.sel[tabLibrary], max(len(pv.library)-1, 0))
			}
		}
		return m, nil
	case pairMsg:
		return m, msg.pv.step(m, msg)
	case actionMsg:
		m.busy = false
		m.agentsOK = false // an action may have installed or removed one
		if msg.pick != "" {
			m.push(&agentPickView{slug: msg.pick, agents: m.src.Agents()})
			return m, nil
		}
		if msg.catalogs {
			m.catalogs = m.src.Catalogs()
			for _, o := range m.stack {
				if v, ok := o.(*catalogView); ok {
					v.reload(m)
				}
			}
		}
		if m.leaving && msg.err == nil && msg.attach == "" {
			// Over a session, the view shows what the popup opened.
			m.result.Attach = "view"
			return m, tea.Quit
		}
		m.leaving = false
		if msg.err != nil {
			m.queued = nil
			m.fail(msg.err)
			return m, m.load()
		}
		if msg.then != nil {
			msg.then(m)
		}
		if msg.sel != "" {
			m.sel, m.userSel = msg.sel, true
		}
		// An attach is never dropped for a key that waited: the action
		// asked to leave for it (the keys stay queued, and run if the
		// dashboard is still there when the view answers).
		if msg.attach == "" {
			if cmd := m.runQueued(); cmd != nil {
				// Keys waited for this save: they build on it, and one
				// reload follows the last.
				m.msg = msg.msg
				return m, cmd
			}
		}
		m.msg = msg.msg
		if msg.attach != "" {
			if msg.current != "" && msg.current != m.current {
				m.current = msg.current
				m.rebuild()
			}
			if m.view != nil {
				// The view shows it, on every console; the dashboard ends
				// when that version comes.
				id, project := msg.attach, msg.current
				vc := m.view
				m.busy, m.leaving = true, m.over != nil
				return m, m.inTurn(func() tea.Msg {
					_, err := vc.Do(proto.MethodViewAttach, proto.ViewParams{Session: id, Project: project})
					return actionMsg{err: err}
				})
			}
			m.result.Attach = msg.attach
			return m, tea.Quit
		}
		cmds := []tea.Cmd{m.load()}
		if pv := m.projectPopupView(); pv != nil {
			cmds = append(cmds, m.loadPopup(pv.slug))
		}
		return m, tea.Batch(cmds...)
	case tea.KeyPressMsg:
		return m, m.key(msg)
	case tea.PasteMsg:
		// Into a prompt: a path or a URL is often pasted.
		if p, ok := m.top().(interface{ paste(*dash, string) }); ok {
			p.paste(m, msg.Content)
		}
		return m, nil
	case tea.MouseClickMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg, tea.MouseWheelMsg:
		return m, m.mouse(msg)
	}
	return m, nil
}

// openTarget opens a sidebar row's target, closing any popup: a thread's
// session is attached, a coordinator opened, else the project's dashboard
// shown.
func (m *dash) openTarget(t Target) tea.Cmd {
	m.stack = nil
	switch {
	case t.Session != "":
		return m.act(func() actionMsg { return actionMsg{attach: t.Session, current: t.Project} })
	case t.Coordinator && t.Activate:
		// Starting an inactive project's coordinator from its row
		// activates the project first (§4, §5.1).
		src, slug := m.src, t.Project
		cols, rows := m.paneSize()
		return m.act(func() actionMsg {
			if _, err := src.Lifecycle(slug, "activate"); err != nil {
				return actionMsg{err: err}
			}
			id, err := src.OpenProject(slug, cols, rows)
			return actionMsg{attach: id, current: slug, sel: "p:" + slug, err: err}
		})
	case t.Coordinator:
		return m.openProject(t.Project)
	}
	return m.showProject(t.Project)
}

// targetActs reports whether openTarget runs t as an action (m.act): a
// session or a coordinator; a project's dashboard only moves the view.
func targetActs(t Target) bool { return t.Session != "" || t.Coordinator }

// showProject shows project's dashboard: it becomes current, with its
// coordinator's row selected, on every console of the view.
func (m *dash) showProject(slug string) tea.Cmd {
	if m.current != slug {
		m.current, m.remoteTop = slug, 0
		m.rebuild()
	}
	m.sel = "p:" + slug
	if slug == allProjects {
		m.sel = ""
		m.rebuild() // NEEDS YOU's first row
	}
	if m.view == nil {
		return nil
	}
	m.selPending++ // the view's selection follows with the answer
	vc := m.view
	return m.inTurn(func() tea.Msg {
		_, err := vc.Do(proto.MethodViewProject, proto.ViewParams{Project: slug})
		return viewDoneMsg{sel: true, err: err}
	})
}

// agentList is the installed agents (Source.Agents), read once per poll:
// the settings and the project's overview ask for them every frame, and
// reading them lists the registry and probes the PATH (PERF-04).
func (m *dash) agentList() []string {
	if !m.agentsOK {
		m.agents, m.agentsOK = m.src.Agents(), true
	}
	return m.agents
}

// catalogList is the agents' models, read on the first ask and again
// when a refresh changes them (m.catalogs).
func (m *dash) catalogList() []Catalog {
	if m.catalogs == nil {
		m.catalogs = m.src.Catalogs()
	}
	return m.catalogs
}

// area is the area with the keyboard: the list when the details panel
// has it but doesn't show (the window got narrower), or the sidebar has
// it while hidden.
func (m *dash) area() area {
	if m.focus == areaDetails && m.panel() == "" || m.focus == areaSide && m.sideW() == 0 {
		return areaMain
	}
	return m.focus
}

// cycleFocus moves the keyboard to the next area (tab), or the previous
// (shift+tab): the list, the details panel when it shows, the sidebar
// unless it is hidden.
func (m *dash) cycleFocus(key string) tea.Cmd {
	areas := []area{areaMain}
	if m.panel() != "" {
		areas = append(areas, areaDetails)
	}
	if m.sideW() > 0 {
		areas = append(areas, areaSide)
	}
	m.focus = cycle(areas, m.area(), key == "shift+tab")
	if m.focus == areaSide {
		// The keyboard's row starts on the row you are on: the header
		// while every project's NEEDS YOU shows.
		m.sideSel = hereKey(m.tree())
		if m.sideHeadIn().here {
			m.sideSel = headKey
		}
		return m.sendSideSel()
	}
	return nil
}

// setData takes a poll's result, rebuilds the rows and rings the bell
// when the server sent a notification (a session blocked, a thread
// reported) since the last poll.
func (m *dash) setData(d Data) tea.Cmd {
	first := !m.loaded
	m.agentsOK = false // read again, once, by the next frame that needs them
	m.data, m.loaded = d, true
	if m.out != nil {
		m.out.loaded()
	}
	if n := oldConsoleNews(d.Server); n != "" && m.msg == "" {
		m.msg = n
	}
	m.rebuild()
	m.noteModFailures()
	m.showToasts(d.Toasts)
	noticeCmds := m.drawNotices()
	ring := false
	if d.ServerOK {
		ring = m.seen && d.Alerts > m.alerts
		m.alerts, m.seen = d.Alerts, true
	}
	cmds := []tea.Cmd{tea.Tick(refresh, func(time.Time) tea.Msg { return tickMsg{} })}
	if ring {
		cmds = append(cmds, tea.Raw("\a"))
	}
	cmds = append(cmds, noticeCmds...)
	switch {
	case first && m.then == "t" && m.over != nil && m.over.Task > 0:
		// The info panel's task, over its session: in the popup's
		// Tasks tab, shown.
		cmds = append(cmds, m.showTask(m.over.Project, m.over.Task))
		if pv := m.projectPopupView(); pv != nil {
			m.push(&taskView{pv: pv, id: m.over.Task})
		}
		m.then = ""
	case first && m.then == thenNeeds:
		// The needs-you badge clicked in a session.
		cmds = append(cmds, m.goNeeds())
		m.then = ""
	case first && m.over != nil && m.over.View:
		// The view a mod opened after a command in the session.
		cmds = append(cmds, m.pollModViews())
	case first && m.then != "":
		// The key typed after the prefix in a session, now that the
		// projects are known.
		cmds = append(cmds, m.listKey(m.then))
		m.then = ""
	}
	if len(cmds) == 1 {
		return cmds[0]
	}
	return tea.Batch(cmds...)
}

// rebuild lays the rows out again from the last poll, for the listed
// project; a selection that went away moves to the first row.
func (m *dash) rebuild() {
	m.rows = buildRows(m.data, shownProject(m.data, m.current), m.layout.order(), m.cardRows())
	if m.layout.compact() {
		m.rows = compactRows(m.rows)
	}
	if m.selIndex() < 0 {
		m.sel = ""
		for _, r := range m.rows {
			if r.selectable() {
				m.sel = r.key
				break
			}
		}
	}
}

// needsCount is what waits on the human in every project of this
// server, as the all-projects NEEDS YOU counts it (needsYouCount): the
// header's ⚑ N.
func (m *dash) needsCount() int {
	return needsYouCount(m.data.Projects, m.data.Sessions)
}

// thenNeeds is the then (DashState.Then) of the needs-you badge clicked
// in a session: NEEDS YOU's first row selected.
const thenNeeds = "needs-you"

// goNeeds shows every project's NEEDS YOU and selects its first row: a
// click on the needs-you badge, which counts them all.
func (m *dash) goNeeds() tea.Cmd {
	var cmd tea.Cmd
	if !m.allShown() && len(m.data.Projects) > 0 {
		cmd = m.showProject(allProjects)
	}
	m.selNeeds()
	return cmd
}

// selNeeds selects NEEDS YOU's first row, with the list's keyboard.
func (m *dash) selNeeds() {
	in := false
	for _, r := range m.rows {
		switch {
		case r.head == "NEEDS YOU":
			in = true
		case r.head != "":
			in = false
		case in && r.selectable():
			m.sel, m.userSel, m.focus = r.key, true, areaMain
			return
		}
	}
}

func (m *dash) selIndex() int {
	for i, r := range m.rows {
		if r.key != "" && r.key == m.sel {
			return i
		}
	}
	return -1
}

func (m *dash) selected() (row, bool) {
	if i := m.selIndex(); i >= 0 {
		return m.rows[i], true
	}
	return row{}, false
}

func (m *dash) move(d int) {
	if m.remoteHere() {
		m.remoteScroll(d)
		return
	}
	i := m.selIndex()
	for j := i + d; j >= 0 && j < len(m.rows); j += d {
		if m.rows[j].selectable() {
			m.sel, m.userSel = m.rows[j].key, true
			return
		}
	}
}

// paneSize is the size new sessions get: the window less the status bar
// and the rule above it (none when compact).
func (m *dash) paneSize() (int, int) {
	if m.layout.StatusCompact() {
		return m.w, max(m.h-1, 1)
	}
	return m.w, max(m.h-2, 1)
}

// key sends a key to the topmost overlay, else to the list's actions.
// The prefix works here as in a session (docs/SPEC.md §4, the key
// table): prefix then a key is that prefix command, the same everywhere,
// and never reaches a popup as a plain key.
func (m *dash) key(k tea.KeyPressMsg) tea.Cmd {
	if k.String() == "ctrl+c" {
		return tea.Quit
	}
	// A message is about the last key: the next one clears it
	// (docs/STYLE.md D5).
	if !m.prefixed {
		m.msg, m.errMsg = "", ""
	}
	if cv, ok := m.top().(*captureView); ok {
		// Any key, the prefix too, is the new prefix.
		m.prefixed = false
		return cv.key(m, k)
	}
	if m.prefixed {
		m.prefixed = false
		return m.prefixCommand(k.String())
	} else if k.String() == m.prefix {
		m.prefixed = true
		return nil
	}
	if o := m.top(); o != nil {
		return o.key(m, k)
	}
	if m.area() == areaSide {
		// The sidebar's keys go on while an action runs: its moves at
		// once, what they start after it (sideKeyboard).
		if cmd, ok := m.sideKeyboard(k.String()); ok {
			return cmd
		}
	}
	if m.busy {
		return nil
	}
	if k.String() == "N" && len(m.data.ModNotices) > 0 {
		m.openNotices()
		return nil
	}
	switch m.area() {
	case areaDetails:
		if cmd, ok := m.detailsKey(k.String()); ok {
			return cmd
		}
	}
	if cmd, ok := m.remoteKey(k.String()); ok {
		return cmd
	}
	if c, ok := m.commandFor(k.String()); ok {
		return m.runCommand(c)
	}
	return m.listKey(binds.list.canon(k.String()))
}

// fail shows err in the footer, as a failure.
func (m *dash) fail(err error) { m.msg = err.Error(); m.errMsg = m.msg }

// act runs an action in the background; one at a time.
func (m *dash) act(fn func() actionMsg) tea.Cmd {
	m.busy = true
	m.acts++
	return func() tea.Msg { return fn() }
}

func (m *dash) openProject(slug string) tea.Cmd {
	cols, rows := m.paneSize()
	return m.act(func() actionMsg {
		id, err := m.src.OpenProject(slug, cols, rows)
		if notChosen(err) {
			// Several agents and none chosen: ask (agentpick.go).
			return actionMsg{pick: slug, sel: "p:" + slug}
		}
		return actionMsg{attach: id, current: slug, sel: "p:" + slug, err: err}
	})
}

// projectHere is the project of the selected row, else the listed one;
// over a session, the popup's.
func (m *dash) projectHere() string {
	if m.over != nil && m.over.Project != "" {
		return m.over.Project
	}
	if r, ok := m.selected(); ok && r.project != "" {
		return r.project
	}
	if m.allShown() {
		return "" // every project's NEEDS YOU: none of its own
	}
	return listProject(m.data, m.current)
}

// allShown says whether the list shows every project's NEEDS YOU (the
// sidebar's PROJECTS header) rather than one project.
func (m *dash) allShown() bool { return shownProject(m.data, m.current) == allProjects }

// cycleProject opens the coordinator of the next (or previous) project
// after the current one.
func (m *dash) cycleProject(next bool) tea.Cmd {
	ps := m.data.Projects
	if len(ps) == 0 {
		m.msg = "no projects; n creates one"
		return nil
	}
	i := -1
	cur := m.current
	if cur == "" {
		cur = m.projectHere()
	}
	for j, p := range ps {
		if p.Slug == cur {
			i = j
		}
	}
	// The k-th project tried; inactive ones are skipped, since opening
	// never activates.
	nth := func(k int) int {
		switch {
		case i < 0:
			return k
		case next:
			return (i + 1 + k) % len(ps)
		}
		return (i - 1 - k + 2*len(ps)) % len(ps)
	}
	for k := range len(ps) {
		if p := ps[nth(k)]; !p.inactive() {
			return m.openProject(p.Slug)
		}
	}
	m.msg = "no project is active; space on one in the sidebar activates it"
	return nil
}

// View.

func (m *dash) View() tea.View {
	if m.hold && m.lastOK {
		return m.lastView
	}
	m.lastView, m.lastOK = m.draw(), true
	return m.lastView
}

// draw is View's frame.
func (m *dash) draw() tea.View {
	// With a pane palette, every cell in the palette's colours (theme.go).
	v := tea.NewView(th().paintWindow(m.render(), m.w, m.h))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	if m.sideW() > sideSlim {
		v.MouseMode = tea.MouseModeAllMotion // the sidebar's tooltip
	}
	v.WindowTitle = "terminatr"
	return v
}

// render draws the sidebar and, beside it, the list; a popup draws the
// whole window itself, centred on it.
func (m *dash) render() string {
	m.geo = nil
	if o := m.top(); o != nil {
		// The popup under it, if any, is what it draws over.
		m.under = nil
		if n := len(m.stack); n > 1 {
			m.under = m.stack[n-2]
		}
		return o.render(m)
	}
	return m.withSide(m.renderList())
}

// withSide puts the sidebar beside the dashboard's lines s, on its
// side.
func (m *dash) withSide(s string) string {
	lines := strings.Split(s, "\n")
	rows := m.tree()
	right := m.layout.Sidebar.Right()
	head := m.sideHeadIn()
	side := sideLines(rows, m.sideW(), m.h, head, sideFooter(rows, m.sideW(), head, m.data.Server, m.over != nil, m.layout.compact()), right)
	w := m.geom().main().w
	for i := range side {
		var l string
		if i < len(lines) {
			l = lines[i]
		}
		if right {
			side[i] = fit(l, w) + reset + side[i]
		} else {
			side[i] += fit(l, w) + reset
		}
	}
	return strings.Join(side, "\n")
}

// baseWindow is the window a popup draws over: the popup it was opened
// from, else the list or a session's screen with the sidebar.
func (m *dash) baseWindow() []string {
	if u := m.under; u != nil {
		m.under = nil
		return strings.Split(u.render(m), "\n")
	}
	title := ""
	if m.over == nil {
		title = m.shownCrumb()
	}
	return strings.Split(m.withSide(m.frame(title, m.base(), -1, "")), "\n")
}

// base is what a popup draws over, as the body's rows: the list, or the
// session's screen under the header.
func (m *dash) base() []string {
	if m.over == nil {
		return m.listBody()
	}
	b := m.geom().body()
	body := make([]string, b.h)
	for i := range body {
		if i+b.y < len(m.over.Screen) {
			body[i] = m.over.Screen[i+b.y]
		}
	}
	return body
}
