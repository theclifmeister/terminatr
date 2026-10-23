package tui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"reflect"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	tmclient "github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/emu"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/plat/term"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/view"
)

// The attach client (docs/SPEC.md §3.3). It draws a server-owned view's
// pane: the session the view shows, with its own attach connection and a
// mirror of the session's emulator, restored
// from the server's snapshot, then fed exactly the bytes and resizes the
// server's emulator gets, in the same order. It draws the outer terminal
// from the mirror and encodes input against the mirror's modes,
// so neither needs a round trip.
//
// The view is the server's, not the client's: showing a session, the
// sidebar's width and tree are view.* calls, and every console joined to
// the view redraws from the version that comes back. The pane's
// rectangle is laid out at the view's size, the window of its latest
// client; a smaller window shows the same frame cropped, a larger one
// padded. What stays here is this console's own: its window, the outer
// terminal's modes, the local scrollback and the prefix.
//
// A thread's pane takes input like any other. The user talks to
// coordinators, and threads to their coordinator (docs/SPEC.md §4), so
// the first text this console types into a thread's prompt box during
// an attach tells the thread's coordinator, without asking: a takeover,
// which the server raises (session.Session.HumanInput).

const (
	frameInterval = time.Second / 120 // render cap
	maxHold       = time.Second       // an app that never ends a 2026 hold is drawn anyway
	// endWait is how long the last pane's session may be gone before the
	// view says so; then the attach ends anyway.
	endWait = 3 * time.Second
	// closeWait is how long the view may say the last pane's session is
	// gone before its stream says why: the two come on separate
	// connections, and the stream's words are what the console prints.
	closeWait = 2 * time.Second
)

// enterScreen switches to the alternate screen.
const enterScreen = "\x1b[?1049h"

// Outer terminal setup: alternate screen, bracketed paste, kitty keyboard
// "disambiguate" (so Shift+Enter and the prefix are unambiguous), colour
// scheme updates (2031) plus a query for the current scheme, and queries
// for its colours (askColors): the panes' programs get them, and the
// theme takes its light or dark roles from the background (OSC 11), as
// the dashboard's does (T319).
var outerSetup = enterScreen + "\x1b[?2004h\x1b[>1u\x1b[?2031h\x1b[?996n" + askColors

const (
	outerRestore = outerHandover + leaveScreen
	// outerHandover restores the modes the attach set but keeps its
	// screen, for the dashboard to draw over (Options.Back).
	outerHandover = "\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1004l\x1b[?1006l\x1b[?2031l" +
		"\x1b[<u\x1b[?2004l\x1b[0m\x1b[0 q"
	// leaveScreen shows the cursor and leaves the alternate screen.
	leaveScreen = "\x1b[?25h\x1b[?1049l"
)

// Key is a key from the outer terminal, as the dashboard hands typed
// keys on to the attach (DashResult.Type).
type Key = uv.Key

// Options configure Attach.
type Options struct {
	Paths tmclient.Paths
	// View is the view drawn, in its layout mode. A bare view (tm attach,
	// tm project open) has no dashboard to go back to: detaching leaves.
	View *ViewConn
	// In and Out are the outer terminal; In must be a TTY.
	In, Out *os.File
	// Log receives diagnostics (digest checks, key encodings); nil
	// discards them.
	Log *log.Logger
	// Sidebar configures the projects sidebar, which every view shows
	// (docs/SPEC.md §4); nil is the defaults.
	Sidebar *SidebarOptions
	// Flash is a note for the status bar until the first key: what a
	// popup over the session last said.
	Flash string
	// Command is a prefix command to run once attached (r or tab):
	// typed after the prefix over a popup on this session, which closed
	// it (DashResult.Command).
	Command string
	// Type are keys typed through a coordinator's row on the dashboard
	// (DashResult.Type): they run first, as typed here, before any key
	// the terminal sends, so none is lost or reordered; TypeFor is the
	// coordinator's session they were typed to, the only pane they reach.
	Type    []Key
	TypeFor string
	// Handover: the dashboard handed this console over with the
	// alternate screen on, showing its last frame (DashOptions.Handover):
	// the attach draws its first frame over it rather than switch
	// screens, so the switch is one frame (T372).
	Handover bool
	// Back: the dashboard follows an attach that doesn't quit (the
	// console's loop). The attach then leaves the alternate screen on,
	// its last frame showing and the cursor hidden, for the dashboard to
	// draw over (DashOptions.Handover).
	Back bool
}

// SidebarOptions configure the attach view's projects sidebar.
type SidebarOptions struct {
	// UIFile is ui.json, where its width is kept as the default of new
	// views; empty keeps it nowhere.
	UIFile string
	// Focus starts the attach with the keyboard in the sidebar: it had it
	// on the dashboard, a click on one of its rows, say (docs/SPEC.md §4).
	Focus bool
}

// Result says how an attach ended.
type Result struct {
	// Reason is shown to the user: "detached", "session exited: …".
	Reason string
	// Detached is true when the session is still running.
	Detached bool
	// Session is the session that had the focus.
	Session string
	// Then is the dashboard key to run once back on the dashboard: the
	// key typed after the prefix (p, ] or [), or "".
	Then string
	// Over is a popup to open over the session (prefix then a, i, t, ,
	// or ?): the caller runs the dashboard over it, which attaches again
	// once the popup closes.
	Over *Over
	// GoTo is a sidebar row clicked in a bare view, which has no
	// dashboard: the caller hands the console over to view main, which
	// opens it (OpenTarget).
	GoTo *Target
	// Quit is set when the console itself went away (its terminal closed,
	// a signal): the client exits rather than show the dashboard.
	Quit bool
	// SideFocus: the sidebar had the keyboard when the attach ended; the
	// dashboard starts with it there.
	SideFocus bool
}

// ErrNotTTY is returned when stdin is not a terminal.
var ErrNotTTY = errors.New("tm attach needs a terminal")

// Attach draws the view until it leaves its layout mode (detached, the
// last pane closed or ended), the user detaches from a bare view or the
// outer terminal goes away. The outer terminal is restored on every path
// out, panics included.
func Attach(opts Options) (res Result, err error) {
	if opts.Log == nil {
		opts.Log = log.New(io.Discard, "", 0)
	}
	if !term.IsTerminal(opts.In) {
		return res, ErrNotTTY
	}
	if n := lookNews(loadLook()); n != "" {
		fmt.Fprintf(os.Stderr, "tm attach: %s\n", n)
	}
	prefix, kerr := prefixKey()
	if kerr != nil {
		fmt.Fprintf(os.Stderr, "tm attach: %v; using %s\n", kerr, DefaultPrefixKey)
	}
	cols, rows, ok := term.Size(opts.In)
	if !ok {
		cols, rows = 80, 24
	}
	// Handed over, the screen is the dashboard's until the setup below:
	// leave it when no dashboard draws it again.
	handed := opts.Handover
	defer func() {
		if handed && (err != nil || res.Quit || !opts.Back) {
			opts.Out.WriteString(leaveScreen)
		}
	}()
	c, err := newClient(opts.Paths, opts.Log)
	if err != nil {
		return res, err
	}
	defer c.close()
	vc := opts.View
	v := vc.View()
	c.vc, c.me = vc, vc.Client()
	c.prefix = prefix
	c.bare, c.dashboard = v.Bare, !v.Bare
	c.flash = opts.Flash
	c.typeAhead, c.typeAheadFor = opts.Type, opts.TypeFor
	so := opts.Sidebar
	if so == nil {
		so = &SidebarOptions{}
	}
	lay := ConsoleLayout(so.UIFile)
	feed, projects := watchSide(c.paths, false)
	defer feed.close() // the watch, if pollState never took it
	c.sideFeed = feed
	c.side = &sidebar{uiFile: so.UIFile, projects: projects, compact: lay.compact(), order: lay.sideSections, projOrder: lay.projSections}
	c.info = &infoPanel{coordOrder: lay.coordSections, detOrder: lay.detSections}
	c.views = handedViews() // the dashboard's, for the sections it showed
	c.setWindow(cols, rows)
	watch, stopWatch := vc.Watch()
	defer stopWatch()
	if err := c.sync(v); err != nil {
		return res, err
	}
	if c.empty() {
		// Why the pane didn't open, when the server said: a paired
		// server's pane it doesn't share, say (docs/SPEC.md §16.12).
		return Result{Reason: cmp.Or(c.flash, "no session to show"), Detached: true}, nil
	}
	if so.Focus && c.sideW > 0 {
		// The sidebar keeps the keyboard it had on the dashboard; its
		// width is known once the view is laid out (sync).
		c.kb = areaSide
		c.status()
	}

	unraw, err := term.MakeRaw(opts.In)
	if err != nil {
		return res, err
	}
	var restoreOnce sync.Once
	restore := func(back bool) {
		restoreOnce.Do(func() {
			if back {
				opts.Out.WriteString(outerHandover)
			} else {
				opts.Out.WriteString(outerRestore)
			}
			unraw()
		})
	}
	defer func() { restore(opts.Back && err == nil && !res.Quit) }()
	defer func() {
		if p := recover(); p != nil {
			restore(false)
			clog("attach: panic: %v\n%s", p, debug.Stack())
			panic(p)
		}
	}()
	// Before the colour queries: the panes' palette, sent once they
	// answered, needs the terminal's truecolor known (truecolor.go).
	probeTerminal(opts.In, opts.Out, true)
	setup := outerSetup
	if handed {
		setup = strings.TrimPrefix(setup, enterScreen) // on it already
	}
	handed = false // restore has the screen from here
	if _, err := opts.Out.WriteString(setup); err != nil {
		return res, err
	}

	// The window closing (on Unix SIGHUP), SIGTERM and SIGINT detach. The
	// server lives in its own session and never sees these.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	digests, stopDigests := vc.Digests()
	defer stopDigests()
	// Read before viewLoop starts: its relayout writes statusBar.
	poll := c.statusBar || c.side != nil
	go c.events(ctx, term.Events(ctx), digests, opts.In)
	go c.viewLoop(watch)
	stopInput := c.inputLoop(ctx, opts.In)
	if poll {
		go c.pollState(ctx)
	}
	c.poke() // paint the snapshot now, even if the pane is idle
	switch opts.Command {
	case "r":
		go c.askRemote()
	case "R":
		go c.repaint()
	case "C":
		go c.saveClearFocus()
	case "tab", "|":
		go c.paneCommand(opts.Command)
	case "":
	default:
		go c.keyCommand(opts.Command)
	}
	res = c.renderLoop(opts.Out)
	cancel()
	stopInput()
	restore(opts.Back && !res.Quit)
	return res, nil
}

// pane is one session in the window.
type pane struct {
	conn *tmclient.Client
	wmu  sync.Mutex // serialises frames to the server

	// Guarded by client.mu.
	mirror   *emu.Terminal
	r        *emu.Renderer
	held     bool // inside the program's 2026 hold
	heldAt   time.Time
	scrolled bool // the local viewport is scrolled back
	info     proto.SessionInfo
	status   *thread.Status // a thread's STATUS.md, for the status bar
	rect     view.Rect      // where it is in this window; empty when cropped away
	gone     bool           // closed or ended: its goroutines stop
	// restarting: the server relaunches its session under the same id
	// (proto.ClosedRestarting); a failing write isn't a lost server then.
	restarting bool
}

// client is the attached window: its panes and everything they share.
type client struct {
	paths tmclient.Paths
	// typeAhead are the keys typed through a coordinator's row on the
	// dashboard (Options.Type), run before the terminal's (replay).
	typeAhead    []Key
	typeAheadFor string
	// typeFor is the coordinator a key typed on its sidebar row is going
	// to, typeKeys that key and those since, held until its pane has the
	// focus, typeBy when they are dropped instead (typethrough.go). keyQueue
	// is inputLoop's queue, which typedReady joins. Guarded by mu.
	typeFor  string
	typeKeys []Key
	typeBy   time.Time
	keyQueue chan uv.Event
	// cells are the mods' cells and status segments from the last poll
	// (pollState). Guarded by mu.
	cells *modCells
	// views are the mods' views as mod.views last listed them (pollState):
	// the info panel's coordinator sections and the sidebar's band. Guarded
	// by mu.
	views []proto.ModView
	log   *log.Logger
	enc   *emu.Encoder
	vc    *ViewConn
	me    string // this console's id in the view
	// syncMu serialises sync: the view loop, and actions drawing their
	// answer at once.
	syncMu sync.Mutex

	mu     sync.Mutex // guards everything below, and the panes' state
	v      view.View  // the version drawn
	geo    view.Geometry
	panes  map[string]*pane
	focus  *pane // the view's pane: keys, paste and the cursor go here; nil without one
	single bool  // the pane draws alone: its renderer has the window to itself
	full   bool  // the next frame repaints the whole window
	closed bool
	// claimSeq is the view's version when this console last claimed the
	// size by typing: it claims again only once the view changed.
	claimSeq uint64
	claimed  bool
	// lastReason is why the last pane's session ended, for the result.
	lastReason string

	prefix  chord
	pending bool   // the prefix was typed: the next key is a command
	flash   string // a note for the status bar until the next key
	// confirm answers the yes/no question the dialog asks (whether to
	// turn a coordinator's remote control around, to deactivate a
	// project), nil while none asks. It is called with c.mu held and
	// releases it.
	confirm func(yes bool)
	// dialog is the yes/no dialog drawn over the panes while one asks
	// (confirm), nil otherwise.
	dialog *amenu

	// The mouse (attachmouse.go): the open menu and the status bar's
	// buttons, by column from its start.
	menu       *amenu
	statusHits []hint
	// Text selection (selection.go): the drag in progress, the pane that
	// shows a selection, and OSC 52 for the outer terminal, written with
	// the next frame.
	drag  *selDrag
	selOn *pane
	clip  []byte

	bare      bool     // the view has no dashboard: detaching leaves
	statusBar bool     // the view's chrome has the status bar
	dashboard bool     // there is a dashboard to go back to
	side      *sidebar // the projects sidebar, nil for none
	// sideFeed is the sidebar's projects from the server (pollState).
	sideFeed *sideFeed
	sideW    int // its width, 0 without one
	// kb is the area with this console's keyboard (focus.go): the pane,
	// the sidebar or the info panel. While another area has it the pane
	// gets no keys, paste or prefix-twice.
	kb area
	// previewSeq counts the sidebar cursor's moves, so a preview only
	// shows the row it last rested on; previewing: one is pending
	// (previewSoon).
	previewSeq uint64
	previewing bool
	// The info panel right of a thread's pane (infopanel.go), nil for
	// none; infoW is its width, 0 while it doesn't show.
	info        *infoPanel
	infoW       int
	cols, rows  int // the window
	paneCols    int // the columns between the sidebar and the info panel
	paneRows    int // the rows above the status bar and the rule over it
	statusText  string
	statusDrawn string
	lastCursor  string
	title       string
	outer       map[int]bool // focus reports and SGR set on the outer terminal
	outerTrack  int          // the mouse tracking mode set there: 1000, 1002, 1003 or 0
	bell        bool         // ring the outer terminal's bell with the next frame
	osc         []byte       // OS notifications of mods to draw with the next frame (modnotice.go)
	buf         []byte

	// colors are the outer terminal's colours as it answered askColors;
	// sentColors those last sent to the panes, under pal, the theme's
	// pane palette the panes are drawn with (outercolors.go).
	colors, sentColors emu.Colors
	pal                *emu.Palette
	fill               string // th().fillReset() as last drawn
	painted            []byte // a frame painted in it (renderLoop)

	// detaching is set before DETACH is written. The server may hang up
	// as soon as it reads it, so from then on a failed write or a closed
	// stream is the detach completing, not a lost server. then, set
	// before detaching, is the dashboard key that detach carries.
	detaching atomic.Bool
	quitting  bool // prefix+q: this console leaves; read once detaching is set
	then      string
	goTo      *Target // a sidebar click in a bare view, carried like then
	over      *Over   // a popup to open over the session, carried like then

	// The mods' commands (attachcommands.go): those that run, read
	// every cmdReload (cmdsAt), the command whose view open this
	// session watches for, and how to send one (nil: to the server).
	cmds        []mod.ModCommand
	cmdsAt      time.Time
	wait        *cmdWait
	sendCommand func(proto.ModCommandParams) (proto.ModCommandResult, error)

	// polledAt is when the state poll last drew (applyPoll).
	polledAt time.Time

	wake    chan struct{}
	endOnce sync.Once
	end     chan struct{}
	result  Result
	// paneEnded is told when a pane's stream says its session ended
	// (ended), for goneResult.
	paneEnded chan struct{}
}

func newClient(p tmclient.Paths, l *log.Logger) (*client, error) {
	enc, err := emu.NewEncoder()
	if err != nil {
		return nil, err
	}
	return &client{paths: p, log: l, enc: enc, outer: map[int]bool{}, panes: map[string]*pane{},
		wake: make(chan struct{}, 1), end: make(chan struct{}), paneEnded: make(chan struct{}, 1)}, nil
}

// setWindow records the window's size. c.mu held, or no other goroutine
// running.
func (c *client) setWindow(cols, rows int) {
	c.cols, c.rows = cols, rows
	c.paneCols = max(cols-c.sideW-c.infoW, 1)
	c.paneRows = rows
	if c.statusBar {
		c.paneRows = max(rows-c.barRows(), 1)
	}
}

// barRows is the status bar's height with the rule above it: 2, 1 for
// a compact one (the view's StatusCompact), 0 for none. c.mu held.
func (c *client) barRows() int {
	switch {
	case !c.statusBar:
		return 0
	case c.geo.Status == 1:
		return 1
	}
	return 2
}

// empty says whether no pane is open.
func (c *client) empty() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.panes) == 0
}

// open attaches to session id as a new pane, its snapshot loaded. The
// pane isn't in the window yet.
func (c *client) open(id string) (*pane, error) {
	c.mu.Lock() // events resizes the window under it
	cols, rows := c.cols, c.rows
	c.mu.Unlock()
	conn, info, err := tmclient.Attach(c.paths, proto.AttachParams{Session: id, Cols: uint16(cols), Rows: uint16(rows), Console: c.me})
	if err != nil {
		return nil, err
	}
	r, err := emu.NewRenderer(uint16(cols), uint16(rows))
	if err != nil {
		conn.Close()
		return nil, err
	}
	p := &pane{conn: conn, r: r, info: *info,
		status: statusCache{}.statuses([]proto.SessionInfo{*info})[info.ID]}
	// The stream starts with the pane's snapshot.
	typ, payload, err := conn.ReadFrame()
	if err == nil && typ != proto.FrameSnapshot {
		err = fmt.Errorf("stream starts with frame %d, not a snapshot", typ)
	}
	if err == nil {
		c.mu.Lock()
		err = c.loadSnapshot(p, payload)
		c.mu.Unlock()
	}
	if err != nil {
		conn.Close()
		r.Close()
		return nil, fmt.Errorf("attach %s: %w", info.ID, err)
	}
	return p, nil
}

// sync makes the window show v: it opens a pane for every session new to
// the layout, closes those that left it, and lays the window out again.
// A build mismatch is returned (the client re-execs); a session that
// can't be attached is skipped with a note in the status bar.
func (c *client) sync(v view.View) error {
	id := v.Shown()
	c.mu.Lock()
	have := c.panes[id] != nil
	c.mu.Unlock()
	var opened []*pane
	var note string
	if id != "" && !have {
		p, err := c.open(id)
		var verr *proto.MismatchError
		switch {
		case errors.As(err, &verr):
			return err
		case err != nil:
			c.log.Printf("view %s: %s: %v", v.Name, id, err)
			clog("attach: view %s: %s: %v", v.Name, id, err)
			note = id + ": " + err.Error()
			if perr := (*proto.Error)(nil); errors.As(err, &perr) {
				note = id + ": " + perr.Message
			}
		default:
			opened = append(opened, p)
		}
	}
	if !c.lock() || v.Name == c.v.Name && v.Seq < c.v.Seq {
		// Closed, or a newer version was drawn meanwhile.
		if !c.closed {
			c.mu.Unlock()
		}
		for _, p := range opened {
			p.discard()
		}
		return nil
	}
	var gone []*pane
	for id, p := range c.panes {
		if !v.Has(id) {
			gone = append(gone, p)
			delete(c.panes, id)
			c.free(p)
		}
	}
	for _, p := range opened {
		c.panes[p.info.ID] = p
	}
	if note != "" {
		c.flash = note
	}
	c.v = v
	c.relayout()
	colors := c.sentColors
	c.mu.Unlock()
	for _, p := range gone {
		c.send(p, proto.FrameDetach, nil)
		p.conn.Close()
	}
	for _, p := range opened {
		// A pane new to the window gets the colours the others have.
		if colors.Known != 0 {
			c.send(p, proto.FrameColors, colors.Encode())
		}
		go c.readLoop(p)
	}
	c.poke()
	return nil
}

// viewLoop follows the view: each new version is drawn, and the attach
// ends when the view leaves its layout mode or the server goes away.
func (c *client) viewLoop(watch <-chan struct{}) {
	for {
		select {
		case <-c.end:
			return
		case <-watch:
		}
		if !c.vc.Up() {
			c.finish(Result{Reason: "lost the server"})
			return
		}
		v := c.vc.View()
		if !c.lock() {
			return
		}
		same := v.Name == c.v.Name && v.Seq == c.v.Seq
		c.mu.Unlock()
		if same {
			continue
		}
		if v.Focus == "" {
			// The session left the view: its stream says why (a remote
			// pane's "mini turned its panes off …"), maybe just after.
			c.finish(c.goneResult())
			return
		}
		if v.Mode != view.ModeLayout {
			c.finish(c.endResult())
			return
		}
		c.syncMu.Lock()
		err := c.sync(v)
		c.syncMu.Unlock()
		if err != nil {
			c.finish(Result{Reason: err.Error()})
			return
		}
	}
}

// apply draws v now when it is newer than what the window shows and
// still a layout; the view loop handles the rest.
func (c *client) apply(v view.View) {
	if !c.lock() {
		return
	}
	newer := v.Name == c.v.Name && v.Seq > c.v.Seq
	c.mu.Unlock()
	if newer && v.Mode == view.ModeLayout && v.Focus != "" {
		c.syncMu.Lock()
		defer c.syncMu.Unlock()
		c.sync(v)
	}
}

// endResult is how the attach ends when the view leaves its layout: a
// detach (here or from another console), or the last session ending.
func (c *client) endResult() Result {
	if c.detaching.Load() {
		return c.detachResult()
	}
	c.mu.Lock()
	reason := c.lastReason
	c.mu.Unlock()
	if reason != "" {
		return Result{Reason: reason}
	}
	return detached
}

// goneResult is endResult once the view says the last session is gone:
// it waits, up to closeWait, for the pane's stream to say why first.
func (c *client) goneResult() Result {
	deadline := time.NewTimer(closeWait)
	defer deadline.Stop()
	for !c.detaching.Load() {
		c.mu.Lock()
		told := c.lastReason != "" || len(c.panes) == 0
		c.mu.Unlock()
		if told {
			break
		}
		select {
		case <-c.paneEnded:
		case <-c.end:
			return c.result
		case <-deadline.C:
			return c.endResult()
		}
	}
	return c.endResult()
}

// close frees every pane. Goroutines still running see c.closed through
// lock and stop.
func (c *client) close() {
	c.mu.Lock()
	c.closed = true
	var ps []*pane
	for _, p := range c.panes {
		ps = append(ps, p)
		c.free(p)
	}
	c.enc.Close()
	c.mu.Unlock()
	for _, p := range ps {
		p.conn.Close()
	}
}

// free releases p's mirror and renderer. c.mu held.
func (c *client) free(p *pane) {
	p.gone = true
	if p.mirror != nil {
		p.mirror.Close()
		p.mirror = nil
	}
	if p.r != nil {
		p.r.Close()
		p.r = nil
	}
	if c.focus == p {
		c.focus = nil
	}
	c.forget(p)
}

// forget drops the selection state that points into p's mirror, the
// drag in progress and the highlighted selection: p is freed, or its
// mirror replaced. c.mu held.
func (c *client) forget(p *pane) {
	if c.selOn == p {
		c.selOn = nil
	}
	if c.drag != nil && c.drag.p == p {
		c.drag = nil
	}
}

// lock takes c.mu unless the client is closed.
func (c *client) lock() bool {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return false
	}
	return true
}

// finish ends the attach with res; the first call wins.
func (c *client) finish(res Result) {
	c.endOnce.Do(func() {
		c.mu.Lock()
		if c.focus != nil {
			res.Session = c.focus.info.ID
		} else if res.Session == "" {
			res.Session = c.v.Focus
		}
		res.SideFocus = c.kb == areaSide && !res.Quit
		c.mu.Unlock()
		c.result = res
		c.log.Printf("end: %s", res.Reason)
		close(c.end)
	})
}

func (c *client) send(p *pane, typ proto.FrameType, payload []byte) {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	if err := p.conn.WriteFrame(typ, payload); err != nil {
		c.lost(p, err)
	}
}

// lost handles p's connection failing with err: the detach completing,
// a pane closed on purpose, or the server gone.
func (c *client) lost(p *pane, err error) {
	if c.detaching.Load() {
		c.finish(c.detachResult())
		return
	}
	c.mu.Lock()
	gone := p.gone || c.closed || p.restarting
	c.mu.Unlock()
	if !gone {
		c.finish(Result{Reason: "lost the server: " + errString(err)})
	}
}

var detached = Result{Reason: "detached", Detached: true}

func (c *client) poke() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// loadSnapshot replaces p's mirror (on attach, and when the server
// resyncs a client that fell behind). c.mu held.
func (c *client) loadSnapshot(p *pane, payload []byte) error {
	mirror, err := emu.Decode(payload)
	if err != nil {
		return err
	}
	if p.mirror != nil {
		p.mirror.Close()
		c.log.Printf("resync %s: new snapshot of %d bytes", p.info.ID, len(payload))
	}
	p.mirror = mirror
	p.scrolled = false
	p.r.Invalidate()
	c.forget(p)
	c.bindMirror(p)
	return nil
}

// bindMirror points p.mirror's hooks at p: the program's clipboard
// writes and its mode-2026 holds. Whenever a mirror moves to another
// pane (reattach), it is bound again there. c.mu held.
func (c *client) bindMirror(p *pane) {
	mirror := p.mirror
	p.held = false
	mirror.OnClipboard(c.forwardClipboard(p))
	// Runs inside mirror.Write with c.mu held. At hold start the terminal
	// still shows the last complete frame: capture it and keep drawing it.
	mirror.OnRenderHold(func(held bool) {
		if held && p.r != nil {
			p.r.Capture(mirror)
			p.heldAt = time.Now()
		}
		p.held = held
	})
}

func (c *client) readLoop(p *pane) {
	for {
		typ, payload, err := p.conn.ReadFrame()
		if err != nil {
			c.lost(p, err)
			return
		}
		if !c.lock() {
			return
		}
		if p.gone {
			c.mu.Unlock()
			return
		}
		switch typ {
		case proto.FrameSnapshot:
			if err = c.loadSnapshot(p, payload); err == nil {
				c.status() // a view-only remote pane's size note
			}
		case proto.FrameOutput:
			p.mirror.Write(payload)
		case proto.FrameResize:
			var cols, rows uint16
			if cols, rows, err = proto.ParseSize(payload); err == nil {
				err = p.mirror.Resize(cols, rows)
				p.r.Invalidate()
				c.status() // a view-only remote pane's size note
			}
		case proto.FrameDigest:
			mine, derr := p.mirror.Digest()
			if derr == nil && mine == string(payload) {
				c.log.Printf("digest ok %s", mine)
			} else {
				c.log.Printf("digest mismatch server=%s client=%s %v", payload, mine, derr)
				clog("attach: digest mismatch server=%s client=%s %v", payload, mine, derr)
			}
		case proto.FrameClosed:
			if string(payload) == proto.ClosedRestarting {
				p.restarting = true
				c.flash = paneName(p.info) + " is restarting"
				c.status()
				c.mu.Unlock()
				c.poke()
				go c.reattach(p)
				return
			}
			c.mu.Unlock()
			c.ended(p, string(payload))
			return
		}
		c.mu.Unlock()
		if err != nil {
			c.ended(p, err.Error())
			return
		}
		c.poke()
	}
}

// ended handles p's session ending (or its stream failing) with reason.
// The server takes the session out of the view, which every console then
// draws; here the pane goes at once and the status bar says why. With
// the last pane gone the attach ends once the view says so, or after
// endWait.
func (c *client) ended(p *pane, reason string) {
	if !c.lock() {
		return
	}
	if p.gone {
		c.mu.Unlock()
		return
	}
	delete(c.panes, p.info.ID)
	c.free(p)
	last := len(c.panes) == 0
	if last {
		c.lastReason = reason
	} else {
		c.flash = paneName(p.info) + ": " + reason
		c.relayout()
	}
	c.mu.Unlock()
	select {
	case c.paneEnded <- struct{}{}:
	default:
	}
	p.conn.Close()
	if last {
		time.AfterFunc(endWait, func() { c.finish(Result{Reason: reason}) })
	}
	c.poke()
}

// relayout places the panes in the window after a change of the view or
// the window. The view is laid out at its size, the window of its latest
// client (docs/SPEC.md §3.3), and drawn into this window from the top
// left: cropped when this one is smaller, padded when it is larger. c.mu
// held.
func (c *client) relayout() {
	vcols, vrows := int(c.v.Cols), int(c.v.Rows)
	if vcols == 0 || vrows == 0 {
		vcols, vrows = c.cols, c.rows
	}
	c.geo = c.v.Lay(vcols, vrows)
	c.sideW = 0
	if c.side != nil {
		c.sideW = min(c.geo.SideW, max(c.cols-1, 1))
	}
	if c.sideW == 0 && c.kb == areaSide {
		c.kb = areaMain // hidden: the keyboard skips it
	}
	c.infoW = 0
	if c.info != nil {
		c.infoW = min(c.geo.InfoW, max(c.cols-c.sideW-1, 0))
	}
	if c.infoW == 0 {
		if c.kb == areaInfo {
			c.kb = areaMain
		}
	} else {
		c.infoLayout()
	}
	c.statusBar = c.geo.Status > 0
	c.paneCols = max(c.cols-c.sideW-c.infoW, 1)
	c.paneRows = max(c.rows-c.geo.Status, 1)
	own := view.Rect{X: c.paneX(), Y: 0, W: c.paneCols, H: c.paneRows}
	c.focus = c.panes[c.v.Focus]
	c.typedFocus()
	vis := c.visible()
	// The pane draws alone, unless it shares the window with the sidebar,
	// the info panel or the status bar (which has a rule above it).
	c.single = len(vis) == 1 && c.side == nil && c.infoW == 0 && !c.statusBar
	for _, p := range vis {
		if c.single {
			p.rect = own
			p.r.SetSize(uint16(own.W), uint16(own.H))
			continue
		}
		p.rect = clip(c.geo.Area, own)
		if p.rect.W > 0 && p.rect.H > 0 {
			p.r.SetRect(p.rect.X, p.rect.Y, p.rect.W, p.rect.H)
		}
	}
	c.full = true
	c.status()
}

// sideX is the sidebar's first column: 0, or on the right the window's
// last sideW. The view is drawn from the window's top left, so on the
// right the sidebar keeps to this window's edge. c.mu held.
func (c *client) sideX() int {
	if c.v.Sidebar.Right() {
		return max(c.cols-c.sideW, 0)
	}
	return 0
}

// paneX is the pane's first column: right of a sidebar on the left, else
// the window's first. c.mu held.
func (c *client) paneX() int {
	if c.v.Sidebar.Right() {
		return 0
	}
	return c.sideW
}

// onSide says whether window column x is the sidebar's. c.mu held.
func (c *client) onSide(x int) bool { return x >= c.sideX() && x < c.sideX()+c.sideW }

// sideCol is window column x as the sidebar's drawing has it (sideLocal),
// -1 off it. c.mu held.
func (c *client) sideCol(x int) int { return sideLocal(x, c.sideX(), c.sideW, c.v.Sidebar.Right()) }

// clip is r within area; empty when they don't meet.
func clip(r, area view.Rect) view.Rect {
	x0, y0 := max(r.X, area.X), max(r.Y, area.Y)
	x1, y1 := min(r.X+r.W, area.X+area.W), min(r.Y+r.H, area.Y+area.H)
	if x1 <= x0 || y1 <= y0 {
		return view.Rect{}
	}
	return view.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// visible are the panes drawn: all the view shows that are open here.
// c.mu held.
func (c *client) visible() []*pane {
	var out []*pane
	if p := c.panes[c.v.Shown()]; p != nil {
		out = append(out, p)
	}
	return out
}

// shown are the visible panes that aren't cropped away. c.mu held.
func (c *client) shown() []*pane {
	var out []*pane
	for _, p := range c.visible() {
		if p.rect.W > 0 && p.rect.H > 0 {
			out = append(out, p)
		}
	}
	return out
}

// leaves are the open panes: the view's one, when open. c.mu held.
func (c *client) leaves() []*pane { return c.visible() }

// needClaim says whether typing into this console must first claim the
// view's size (docs/SPEC.md §3.3): when another console sizes the view,
// or a pane it shows has another size than its rectangle. It claims once
// per version of the view. c.mu held.
func (c *client) needClaim() bool {
	if c.claimed && c.claimSeq == c.v.Seq {
		return false
	}
	need := c.v.Latest != c.me
	for _, p := range c.visible() {
		if c.remoteBlock(p) != "" {
			// Keys don't go there, so they size nothing.
			return false
		}
		r := c.geo.Area
		if cols, rows := p.mirror.Size(); int(cols) != r.W || int(rows) != r.H {
			need = true
		}
	}
	if need {
		c.claimSeq, c.claimed = c.v.Seq, true
	}
	return need
}

// claim tells the server that the user typed into session id here.
func (c *client) claim(id string) {
	if _, err := c.vc.Do(proto.MethodViewInput, proto.ViewParams{Session: id}); err != nil {
		c.log.Printf("claim %s: %v", id, err)
		clog("attach: claim %s: %v", id, err)
	}
}

// act runs a view action; a failure shows in the status bar. The view
// it answers is drawn before act returns, so the next event already sees
// it here.
func (c *client) act(method string, p proto.ViewParams) {
	v, err := c.vc.Do(method, p)
	if err == nil {
		c.apply(v)
		return
	}
	c.failed(err)
}

// failed says in the status bar why a view action failed.
func (c *client) failed(err error) {
	if c.lock() {
		var perr *proto.Error
		msg := err.Error()
		if errors.As(err, &perr) {
			msg = perr.Message
		}
		c.flash = msg
		c.status()
		c.mu.Unlock()
		c.poke()
	}
}

// events handles the outer terminal's events and the server's digest
// requests until ctx ends.
func (c *client) events(ctx context.Context, evs <-chan term.Event, digests <-chan struct{}, in *os.File) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-digests:
			// Consistency check (view.digest, e2e): the server puts its
			// digest into the stream, readLoop compares it with the
			// mirror's.
			if c.lock() {
				p := c.focus
				c.mu.Unlock()
				if p != nil {
					c.send(p, proto.FrameDigestReq, nil)
				}
			}
		case ev, ok := <-evs:
			if !ok {
				return
			}
			if ev.Kind == term.Detach {
				c.finish(Result{Reason: "detached (" + ev.Why + ")", Detached: true, Quit: true})
				continue
			}
			// The user really resized the window: the view takes its size
			// and its panes follow, whoever typed last (docs/SPEC.md §3.3).
			cols, rows, ok := term.Size(in)
			if !ok {
				continue
			}
			if !c.lock() {
				return
			}
			c.setWindow(cols, rows)
			c.relayout()
			c.mu.Unlock()
			c.act(proto.MethodViewSize, proto.ViewParams{Cols: uint16(cols), Rows: uint16(rows), Resize: true})
			c.poke()
		}
	}
}

// inputLoop handles the outer terminal's input until ctx ends. stop
// cancels the terminal read and waits until it returned: a read left
// blocked after a detach would take the next input meant for the
// dashboard, which reads the terminal next, and lose that key or click.
// replayWait is how long replay waits for the focused pane to show.
const replayWait = 5 * time.Second

// replay runs the keys typed through a coordinator's row (typeAhead) as
// typed here, once the view's focused pane shows: they reach its
// program in order, before any key the terminal sends after them. They
// go only to the coordinator they were typed to (typeAheadFor): with
// another pane focused (another console attached one in the shared
// view), or none within replayWait, they are dropped, and the status
// bar says so.
func (c *client) replay(ctx context.Context) {
	keys, want := c.typeAhead, c.typeAheadFor
	c.typeAhead, c.typeAheadFor = nil, ""
	if len(keys) == 0 {
		return
	}
	got := ""
	for deadline := time.Now().Add(replayWait); ctx.Err() == nil; time.Sleep(10 * time.Millisecond) {
		c.mu.Lock()
		if c.focus != nil {
			got = c.focus.info.ID
		}
		c.mu.Unlock()
		if got != "" || time.Now().After(deadline) {
			break
		}
	}
	drop := func() {
		if !c.lock() {
			return
		}
		c.flash = "the coordinator's pane didn't show: what you typed was dropped"
		c.status()
		c.mu.Unlock()
		c.poke()
	}
	if want == "" || got != want {
		drop()
		return
	}
	for _, k := range keys {
		if ctx.Err() != nil {
			return
		}
		// Each key only while the coordinator's pane still has the
		// focus: the shared view may move it meanwhile.
		beforeReplayKey(c)
		c.mu.Lock()
		still := c.focus != nil && c.focus.info.ID == want
		c.mu.Unlock()
		if !still {
			drop()
			return
		}
		c.key(k)
	}
}

// beforeReplayKey runs before replay checks the focus for each key; tests
// move the focus there.
var beforeReplayKey = func(*client) {}

func (c *client) inputLoop(ctx context.Context, in *os.File) (stop func()) {
	var r io.Reader = in
	cr, err := uv.NewCancelReader(in)
	if err == nil {
		r = cr
	} else {
		c.log.Printf("input: no cancelable reader: %v", err)
	}
	events := make(chan uv.Event, 64)
	c.mu.Lock()
	c.keyQueue = events
	c.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := uv.NewTerminalReader(r, os.Getenv("TERM")).StreamEvents(ctx, events)
		// EOF or EIO: the outer terminal is gone. That is a detach.
		c.finish(Result{Reason: "detached (terminal closed: " + errString(err) + ")", Detached: true, Quit: true})
	}()
	go func() {
		// The keys typed through a coordinator's row go first; the
		// terminal's wait in events meanwhile.
		c.replay(ctx)
		// Past ctx it keeps draining until the reader is done: its last
		// sends block on a full channel, and with them the reader.
		for {
			select {
			case <-done:
				return
			case ev := <-events:
				if ctx.Err() == nil {
					c.handle(ev)
				}
			}
		}
	}()
	return func() {
		if cr == nil {
			return
		}
		cr.Cancel()
		select {
		case <-done:
			cr.Close()
		case <-time.After(time.Second):
			c.log.Printf("input: the terminal read didn't stop")
			cr.Close()
		}
	}
}

func (c *client) handle(ev uv.Event) {
	switch e := ev.(type) {
	case uv.KeyPressEvent:
		c.key(uv.Key(e))
	case typedReady:
		c.flushTyped()
	case uv.MouseClickEvent, uv.MouseReleaseEvent, uv.MouseMotionEvent, uv.MouseWheelEvent:
		c.mouse(ev)
	case uv.FocusEvent:
		c.focusReport(true)
	case uv.BlurEvent:
		c.focusReport(false)
	case uv.PasteEvent:
		if !c.lock() {
			return
		}
		p := c.focus
		if p == nil || c.kb != areaMain {
			c.mu.Unlock()
			return
		}
		if why := c.remoteBlock(p); why != "" {
			c.flash = why
			c.status()
			c.mu.Unlock()
			c.poke()
			return
		}
		b, err := emu.Paste(p.mirror, []byte(e.Content))
		claim := c.needClaim()
		c.mu.Unlock()
		if claim {
			c.claim(p.info.ID)
		}
		if err == nil {
			c.send(p, proto.FrameInput, b)
		}
	case uv.DarkColorSchemeEvent, uv.LightColorSchemeEvent:
		scheme := emu.SchemeDark
		if _, ok := e.(uv.LightColorSchemeEvent); ok {
			scheme = emu.SchemeLight
		}
		// The panes' programs get the scheme. The console's theme follows
		// the background instead, like the dashboard's: a terminal whose
		// scheme (the system's, say) isn't its background's would flip
		// the theme between the two views. It and the other colours may
		// have changed with the scheme: ask them again.
		if !c.lock() {
			return
		}
		ps := c.leaves()
		c.osc = append(c.osc, askColors...)
		c.mu.Unlock()
		c.poke()
		for _, p := range ps {
			c.send(p, proto.FrameColorScheme, []byte{byte(scheme)})
		}
	case uv.ForegroundColorEvent:
		c.gotColor(emu.KnownFg, e.Color)
	case uv.BackgroundColorEvent:
		c.gotColor(emu.KnownBg, e.Color)
		c.background(e.IsDark())
	case uv.UnknownOscEvent:
		if i, col, ok := paletteAnswer(string(e)); ok {
			c.gotColor(1<<i, col)
		}
	case uv.PrimaryDeviceAttributesEvent:
		// The fence after the colour queries (askColors).
		c.sendColors()
	}
}

// background follows the outer terminal's background, light or dark:
// the theme's roles for it, and the sidebar, info panel and status bar
// drawn again in them.
func (c *client) background(dark bool) {
	if !setScheme(dark) || !c.lock() {
		return
	}
	c.full = true
	c.status()
	c.mu.Unlock()
	c.poke()
}

// key handles a key from the outer terminal: the prefix and the command
// after it (docs/SPEC.md §4), or a key for the focused program.
func (c *client) key(k uv.Key) {
	if c.holdTyped(k) {
		return
	}
	if !c.lock() {
		return
	}
	if !c.pending && c.prefix.match(k) && (c.menu != nil || c.confirm != nil) {
		// The prefix works from a menu or a question too: it closes the
		// menu, or answers no, and starts a command.
		if c.menu != nil || c.dialog != nil {
			c.closeMenu()
		}
		c.confirm = nil
	}
	if c.menu != nil {
		c.menuKey(k)
		return
	}
	if answer := c.confirm; answer != nil {
		// Y says yes, N or esc no; no other key answers (a lowercase y
		// or n included: stray typing can't say yes).
		switch keyName(k) {
		case "Y":
			answer(true)
		case "N", "esc":
			answer(false)
		default:
			c.mu.Unlock()
		}
		return
	}
	pending := c.pending
	if c.kb == areaSide && !pending && !c.prefix.match(k) {
		c.sideKeyboard(k)
		return
	}
	if c.kb == areaInfo && !pending && !c.prefix.match(k) {
		c.infoKeyboard(k)
		return
	}
	do := prefixStep(c.prefix, pending, k, c.dashboard)
	var cmd mod.ModCommand
	if pending && do == (prefixDo{}) {
		// No core command: a mod's, if one has the key.
		cmd, do.command = c.sessionCommand(keyName(k))
	}
	c.pending = do.arm
	redraw := pending || do.arm || c.flash != ""
	c.flash = ""
	if redraw {
		c.status()
	}
	kb := c.kb // read under the lock: a click may move it
	c.mu.Unlock()
	if redraw {
		c.poke()
	}
	if do.input {
		if kb == areaMain { // another area has the keyboard: nothing leaks
			c.input(k)
		}
		return
	}
	if do.command {
		c.runCommand(cmd)
		return
	}
	c.run(do)
}

// run does what prefixStep decided, but for input.
func (c *client) run(do prefixDo) {
	switch {
	case do.quit:
		c.quit()
	case do.detach:
		c.detachThen(do.then)
	case do.popup != "" && c.remotePopup(do.popup):
	case do.popup != "":
		c.popupOver("", do.popup)
	case do.pane != "":
		c.paneCommand(do.pane)
	case do.remote:
		c.askRemote()
	case do.repaint:
		c.repaint()
	case do.saveClear:
		c.saveClearFocus()
	}
}

// remotePopup opens the project popup (prefix+A, I, T) of a paired
// server's pane: that server's popup of its project (remotepopup.go),
// over the pane, or, when the sidebar's mirror says that server serves
// none to this one, why not in the status bar. false for this server's
// panes and the other popups, which open as ever.
func (c *client) remotePopup(key string) bool {
	if !projectPopups[key] || !c.lock() {
		return false
	}
	p := c.focus
	if p == nil || p.info.Server == "" {
		c.mu.Unlock()
		return false
	}
	if c.side != nil && !remotePopupOK(c.side.peers, p.info.Server) {
		c.flash = remoteNoPopup(c.serverShown(p.info.Server), p.info.Project)
		c.status()
		c.mu.Unlock()
		c.poke()
		return true
	}
	c.goOver(&Over{Key: key, Project: "@" + p.info.Server + "/" + p.info.Project})
	return true
}

// repaint asks the focused pane, a paired server's, for a fresh repaint
// from that server (docs/SPEC.md §16.12); this server's own panes are
// always current.
func (c *client) repaint() {
	if !c.lock() {
		return
	}
	p := c.focus
	if p == nil || p.info.Server == "" {
		c.flash = "prefix+" + keyShown(boundKey("R")) + " repaints a paired server's pane; this one is this server's"
		c.status()
		c.mu.Unlock()
		c.poke()
		return
	}
	c.flash = "repainting from " + c.serverShown(p.info.Server)
	c.status()
	c.mu.Unlock()
	c.poke()
	c.send(p, proto.FrameRepaintReq, nil)
}

// prefixDo is what a key does given whether the prefix came before it.
type prefixDo struct {
	arm    bool   // it is the prefix: the next key is a command
	input  bool   // the program gets it
	quit   bool   // this console leaves: the view stays as it is
	detach bool   // detach, then run then on the dashboard
	then   string //
	popup  string // a dashboard key whose popup opens over the session
	pane   string // a sidebar command (paneCommands)
	// remote asks to turn the focused coordinator's remote control on
	// or off.
	remote bool
	// repaint asks a paired server's pane for a fresh repaint (prefix+R).
	repaint bool
	// saveClear saves and clears the focused session's project's
	// coordinator (prefix+C).
	saveClear bool
	// command: a mod's command has the key (attachcommands.go).
	command bool
}

// paneCommands are the keys that, after the prefix, act on the window
// itself: the sidebar's width ({ }), slim or hidden (b), the keyboard
// (tab), the info panel (|) and the last place (;).
var paneCommands = prefixCmdKeys(cmdPane)

// keyName names k as paneCommands does.
func keyName(k uv.Key) string {
	switch {
	case k.Code == uv.KeySpace || k.Text == " ":
		return "space"
	case k.Code == uv.KeyTab && k.Mod == 0:
		return "tab"
	case k.Text != "":
		return k.Text
	}
	return k.String()
}

// prefixStep decides what k does. After the prefix: q quits this
// console, d detaches, a
// dashboard key detaches and runs there (only when there is a dashboard
// to go back to), a pane command acts on the panes, r asks to turn remote
// control on or off, the prefix again goes to the program, and anything
// else cancels.
func prefixStep(prefix chord, pending bool, k uv.Key, dashboard bool) prefixDo {
	name := keyName(k)
	if pending && !prefix.match(k) {
		// A bound key runs as its action's default (keybind.go), a twin as its shifted key (prefixTwins).
		name = prefixName(name)
	}
	switch {
	case !pending && prefix.match(k):
		return prefixDo{arm: true}
	case !pending, prefix.match(k):
		return prefixDo{input: true}
	case name == "q":
		return prefixDo{quit: true}
	case name == "d":
		return prefixDo{detach: true}
	case name == "r":
		return prefixDo{remote: true}
	case name == "R":
		return prefixDo{repaint: true}
	case name == "C":
		return prefixDo{saveClear: true}
	case popupCommands[name] && dashboard:
		return prefixDo{popup: name}
	case prefixCommands[name] && dashboard:
		return prefixDo{detach: true, then: name}
	case paneCommands[name]:
		return prefixDo{pane: name}
	}
	return prefixDo{}
}

// detachThen leaves the layout; then is the dashboard key to run
// afterwards. A shared view goes back to its dashboard, on every console;
// a bare one has none, so this console leaves it.
func (c *client) detachThen(then string) {
	c.then = then
	c.detaching.Store(true) // publishes then to lost
	if !c.bare {
		if _, err := c.vc.Do(proto.MethodViewDashboard, proto.ViewParams{}); err != nil {
			c.log.Printf("%s: %v", proto.MethodViewDashboard, err)
			clog("attach: %s: %v", proto.MethodViewDashboard, err)
		}
	}
	c.leaveAll()
}

// quit ends this console, as closing its window does: the view, its
// sessions and the other consoles carry on.
func (c *client) quit() {
	c.quitting = true
	c.detaching.Store(true) // publishes quitting to lost
	c.leaveAll()
}

// leaveAll is leave with every leaf.
func (c *client) leaveAll() {
	var ps []*pane
	if c.lock() {
		ps = c.leaves()
		c.mu.Unlock()
	}
	c.leave(ps)
}

// leave detaches the panes ps and ends the attach as detachResult says.
// The caller has set detaching first, once it published what lost reads
// (then, quitting, over): a pane that fails as it detaches is then not
// lost.
func (c *client) leave(ps []*pane) {
	for _, p := range ps {
		c.send(p, proto.FrameDetach, nil)
	}
	c.finish(c.detachResult())
}

// detachResult is how a detach ends. c.then is read only once detaching
// is set.
func (c *client) detachResult() Result {
	res := detached
	if c.quitting {
		res.Quit = true
		return res
	}
	res.Then, res.GoTo, res.Over = c.then, c.goTo, c.over
	return res
}

// popupOver leaves the attach for the dashboard key's popup, drawn over
// this pane, about project ("" for the focused pane's): the view stays
// as it is, on every console, and closing the popup attaches again
// (docs/SPEC.md §4).
func (c *client) popupOver(project, key string) { c.popupOverTask(project, key, 0) }

// popupTask leaves the attach for the task view on task id of project,
// over this pane (a click on the info panel's task).
func (c *client) popupTask(project string, id int) { c.popupOverTask(project, "T", id) }

// popupOverTask is popupOver, the task view showing task id when it is
// not 0.
func (c *client) popupOverTask(project, key string, id int) {
	if !c.lock() {
		return
	}
	c.goOver(&Over{Key: key, Project: project, Task: id})
}

// goOver leaves the attach for the dashboard over this pane, which runs
// o and attaches again once its popup closes. c.mu held; released here.
func (c *client) goOver(o *Over) {
	o.Screen = c.paneLines(o)
	if p := c.focus; p != nil {
		o.Session, o.Title = p.info.ID, oneLine(p.info.ID+" · "+strings.TrimPrefix(p.info.Project+" "+sessionName(p.info), " "))
		if o.Project == "" {
			o.Project = p.info.Project
		}
	}
	ps := c.leaves()
	c.mu.Unlock()
	c.over = o
	c.detaching.Store(true) // publishes over to lost
	c.leave(ps)
}

// paneLines are the pane area's rows as shown, as plain text: what a
// popup over the session dims. It also places o's pane in that area, so
// that the popup can follow the pane's output the same way. c.mu held.
func (c *client) paneLines(o *Over) []string {
	p := c.focus
	if p == nil || p.mirror == nil {
		return make([]string, c.paneRows)
	}
	o.X, o.Y, o.H = p.rect.X-c.paneX(), p.rect.Y, p.rect.H
	if p.r != nil {
		o.Top = p.r.Top()
	}
	s, err := p.mirror.Screen()
	if err != nil {
		return make([]string, c.paneRows)
	}
	return o.place(strings.Split(s, "\n"), c.paneRows)
}

// paneCommand runs a command on the window: the sidebar's width, hidden
// or slim, its keyboard.
func (c *client) paneCommand(cmd string) {
	switch cmd {
	case "{", "}", "B":
		c.sideKey(cmd)
	case "tab":
		c.nextArea()
	case "|":
		c.infoToggle()
	case ";":
		c.goLastPlace()
	}
	c.poke()
}

// viewOnlyNote starts the note on a key dropped at a view-only pane.
const viewOnlyNote = "view-only: "

// remoteBlock is why keys don't reach p, a paired server's pane: its
// server lets this one watch only, or the link is down (keys typed then
// are dropped, never kept for later; docs/SPEC.md §16.12). "" when they
// go, and for this server's own panes. c.mu held.
func (c *client) remoteBlock(p *pane) string {
	alias := p.info.Server
	if alias == "" {
		return ""
	}
	shown := c.serverShown(alias)
	if p.info.ViewOnly {
		return viewOnlyNote + shown + " shares its panes without typing"
	}
	for _, pm := range c.side.peers {
		if pm.Alias == alias && pm.State != proto.LinkUp {
			return shown + " is offline: keys aren't sent"
		}
	}
	return ""
}

// serverShown is what the console calls the paired server alias: its
// display name, as the sidebar's mirror has it. c.mu held.
func (c *client) serverShown(alias string) string {
	if c.side == nil {
		return alias
	}
	return peerShown(c.side.peers, alias)
}

// remoteNote is the status bar's note on a paired server's pane: how it
// is linked, or that keys don't go. A view-only pane keeps the far
// session's size, drawn from the top left, cropped or padded: when that
// isn't its rectangle's, the note says the size. c.mu held.
func (c *client) remoteNote(p *pane) string {
	in := p.info
	for _, pm := range c.side.peers {
		if pm.Alias != in.Server {
			continue
		}
		shown := cmp.Or(pm.Name, pm.Alias)
		if pm.State != proto.LinkUp {
			return "⚠ " + shown + " offline · keys aren't sent"
		}
		note := "remote"
		if in.ViewOnly {
			note = "view-only"
			if p.mirror != nil {
				cols, rows := p.mirror.Size()
				if r := c.geo.Area; cols > 0 && (int(cols) != r.W || int(rows) != r.H) {
					note += fmt.Sprintf(" · %d×%d on %s", cols, rows, shown)
				}
			}
		}
		if pm.RTT > 0 {
			note += " · " + rttWords(pm.RTT)
		}
		return note
	}
	return "remote"
}

// remoteInfo is a paired server's pane in, its state as the mirror
// last had it, and whether it takes keys: the peer's pane switch, which
// it tells the moment it changes (docs/SPEC.md §16.12).
func remoteInfo(peers []proto.PeerMirror, in proto.SessionInfo) proto.SessionInfo {
	_, sid, _ := strings.Cut(in.ID, "/")
	for _, pm := range peers {
		if pm.Alias != in.Server {
			continue
		}
		switch pm.Panes {
		case proto.PanesView:
			in.ViewOnly = true
		case proto.PanesType:
			in.ViewOnly = false
		}
		for _, pr := range pm.Projects {
			for _, s := range pr.Sessions {
				if s.ID == sid {
					in.State, in.Reason, in.TodosDone, in.TodosTotal, in.Current = s.State, s.Reason, s.TodosDone, s.TodosTotal, s.Current
					in.Model, in.Context, in.ContextWindow, in.Task = s.Model, s.Context, s.ContextWindow, s.Task
					return in
				}
			}
		}
	}
	return in
}

// paneName is how the status bar names a session: its thread's name
// (threadName), else its own id.
func paneName(s proto.SessionInfo) string {
	if s.Thread != "" {
		return threadName(s.Task, s.Thread)
	}
	return s.ID
}

// status redraws the status bar. c.mu held.
func (c *client) status() {
	c.notePlace()
	if !c.statusBar || c.focus == nil {
		return
	}
	where := ""
	switch {
	case c.kb == areaSide && c.previewing:
		where = previewWait
	case c.kb == areaSide:
		where = previewNote
	case c.kb == areaInfo:
		where = c.infoHintNow()
	}
	if c.flash != "" {
		where = c.flash // a note in passing
	}
	// With the sidebar hidden, what needs you is counted at the right end:
	// a click on it opens the dashboard there.
	badge := ""
	if c.side != nil && c.sideW == 0 {
		badge = needsBadge(needsYouCount(c.side.projects, c.side.sessions))
	}
	bw := ansi.StringWidth(badge)
	if c.cols < bw+20 {
		badge, bw = "", 0 // no room
	}
	sw := c.cols
	if bw > 0 {
		sw -= bw + 1
	}
	if where == "" && c.focus.info.Server != "" {
		where = c.remoteNote(c.focus)
	}
	info := c.focus.info
	if info.Server != "" {
		// The status bar's breadcrumb names the server as the sidebar
		// does: its display name, when it has one.
		info.Server = c.serverShown(info.Server)
	}
	line, hits := statusBar(info, c.focus.status, c.paneTitle(), c.pending, sw, where, c.cells)
	line = plainUnder(line)
	if bw > 0 {
		line += reset + badge + " "
		hits = append(hits, hint{c.cols - bw - 1, c.cols - 1, thenNeeds})
	}
	if c.single {
		c.focus.r.SetStatus(line)
	}
	c.statusText, c.statusHits = line, hits
}

// paneTitle is the title of the focused pane's thread, from the
// sidebar's projects; "" for none. c.mu held.
func (c *client) paneTitle() string {
	in := c.focus.info
	if c.side == nil || in.Thread == "" {
		return ""
	}
	if in.Server != "" {
		for _, pm := range c.side.peers {
			for _, pr := range pm.Projects {
				if pm.Alias != in.Server || pr.Project != in.Project || pr.Watch == nil {
					continue
				}
				for _, t := range pr.Watch.Threads {
					if t.ID == in.Thread {
						return t.Title
					}
				}
			}
		}
		return ""
	}
	for _, p := range c.side.projects {
		if p.Slug != in.Project {
			continue
		}
		for _, t := range p.Threads {
			if t.Record != nil && t.ID == in.Thread {
				return t.Title
			}
		}
	}
	return ""
}

// input sends a key to the focused program.
func (c *client) input(k uv.Key) {
	if !c.lock() {
		return
	}
	p := c.focus
	if p == nil {
		c.mu.Unlock()
		return
	}
	// Shift+PgUp/PgDn scroll this client's own scrollback, for programs on
	// the main screen (shells, inline apps). Full-screen apps get the key.
	if k.Mod == uv.ModShift && (k.Code == uv.KeyPgUp || k.Code == uv.KeyPgDown) && !p.mirror.Modes().AltScreen {
		_, rows := p.mirror.Size()
		d := int(rows) / 2
		if k.Code == uv.KeyPgUp {
			d = -d
		}
		p.mirror.ScrollViewport(d)
		p.scrolled = true
		c.mu.Unlock()
		c.poke()
		return
	}
	if why := c.remoteBlock(p); why != "" {
		c.flash = why
		c.status()
		c.mu.Unlock()
		c.poke()
		return
	}
	if p.scrolled {
		p.mirror.ScrollViewportBottom()
		p.scrolled = false
		c.poke()
	}
	c.clearSel()
	ek, ok := toKey(k)
	var b []byte
	var err error
	if ok {
		b, err = c.enc.Key(p.mirror, ek)
	}
	claim := c.needClaim()
	c.mu.Unlock()
	if claim {
		c.claim(p.info.ID)
	}
	c.log.Printf("key %s -> %q %v", k, b, err)
	if err == nil && len(b) > 0 {
		c.send(p, proto.FrameInput, b)
	}
}

// wheelRows is how far one turn of the wheel scrolls a pane's local
// scrollback.
const wheelRows = 3

// wheelScroll scrolls p's local scrollback when m turns the wheel, and
// says whether it did. c.mu held.
func wheelScroll(p *pane, m emu.Mouse) bool {
	if m.Action != emu.MousePress || m.Button != emu.MouseWheelUp && m.Button != emu.MouseWheelDown {
		return false
	}
	d := wheelRows
	if m.Button == emu.MouseWheelUp {
		d = -d
	}
	p.mirror.ScrollViewport(d)
	p.scrolled = true
	p.r.Invalidate()
	return true
}

// mouse handles a mouse event (attachmouse.go): the open menu, a
// question in the status bar, the sidebar and the status bar are tm's.
// Over the pane, a click takes the keyboard back from the sidebar; the
// event goes to the program when it tracks the mouse, and otherwise a
// right-click opens the ≡ menu and, on the main screen, the wheel
// scrolls the local scrollback. The outer terminal reports the mouse
// while the focused program tracks it or the sidebar shows (see
// outerModes).
func (c *client) mouse(ev uv.Event) {
	m, ok := toMouse(ev)
	if !ok || !c.lock() {
		return
	}
	press := m.Action == emu.MousePress && (m.Button == emu.MouseLeft || m.Button == emu.MouseRight || m.Button == emu.MouseMiddle)
	// The status bar runs across the window, the rule above it too.
	status := c.statusBar && (m.Y >= c.paneRows)
	if (!c.onSide(m.X) || m.Y >= c.sideRows()) && setSideHover("") {
		c.poke() // off the sidebar: its tooltip goes
	}
	switch {
	case c.menu != nil:
		c.menuMouse(m)
		return
	case c.drag != nil && m.Action != emu.MousePress:
		// A drag selection follows the mouse wherever it goes.
		c.selMouse(c.drag.p, m)
		c.mu.Unlock()
		return
	case press && c.confirm != nil:
		// The dialog's buttons answer; a click outside it says no, as
		// a click outside a popup closes it.
		d := c.dialog
		key := ""
		if d != nil && m.Button == emu.MouseLeft {
			key = d.action(m.X, m.Y)
		}
		w, h := 0, 0
		if d != nil {
			w, h = d.size()
		}
		switch {
		case key == "Y":
			c.confirm(true)
		case key == "N", key == "esc", d == nil, m.X < d.x || m.X >= d.x+w || m.Y < d.y || m.Y >= d.y+h:
			c.confirm(false)
		default:
			c.mu.Unlock()
		}
		return
	case c.side != nil && (c.onSide(m.X) && m.Y < c.sideRows() || c.side.drag):
		if press && m.Button == emu.MouseRight && !c.side.drag {
			c.sideMenu(m)
			return
		}
		c.sideMouse(m)
		return
	case c.infoW > 0 && (m.X >= c.infoX() && !status || c.info.drag):
		c.infoMouse(m)
		return
	case status:
		c.statusMouse(m)
		return
	}
	var p *pane
	for _, q := range c.shown() {
		if r := q.rect; m.X >= r.X && m.X < r.X+r.W && m.Y >= r.Y && m.Y < r.Y+r.H {
			p = q
		}
	}
	if p == nil { // padding, or the rule above the status bar
		c.mu.Unlock()
		return
	}
	_, click := ev.(uv.MouseClickEvent)
	if click && c.kb != areaMain {
		// A click on the pane gives it the keyboard.
		c.kb = areaMain
		c.status()
		c.poke()
	}
	if c.remoteBlock(p) != "" {
		// A paired server's pane that takes no keys (docs/SPEC.md
		// §16.12): the mouse is tm's, whatever the program tracks. The
		// wheel scrolls the local scrollback, a left drag selects.
		switch {
		case press && m.Button == emu.MouseRight:
			c.openMenu(c.menuTitle(), c.sessionItems(), m.X, m.Y, false)
		case wheelScroll(p, m):
		case m.Button == emu.MouseLeft:
			c.selMouse(p, m)
		}
		c.mu.Unlock()
		c.poke()
		return
	}
	if !p.mirror.Modes().MouseTracking() {
		// tm's: the program doesn't take the mouse. A left drag selects,
		// and on the main screen (a shell, an inline app such as Claude's
		// default renderer) the wheel scrolls the local scrollback, as
		// Shift+PgUp/PgDn do; like them it sends the program nothing, so
		// it doesn't claim the size.
		local := false
		switch {
		case press && m.Button == emu.MouseRight:
			c.openMenu(c.menuTitle(), c.sessionItems(), m.X, m.Y, false)
		case !p.mirror.Modes().AltScreen && wheelScroll(p, m):
			local = true
		case m.Button == emu.MouseLeft:
			c.selMouse(p, m)
		}
		_, wheel := ev.(uv.MouseWheelEvent)
		claim := (click || wheel && !local) && c.needClaim()
		c.mu.Unlock()
		if claim {
			c.claim(p.info.ID)
		}
		c.poke()
		return
	}
	if c.remoteBlock(p) != "" {
		c.mu.Unlock()
		c.poke()
		return
	}
	claim := false
	if _, wheel := ev.(uv.MouseWheelEvent); click || wheel {
		claim = c.needClaim()
	}
	m.X -= p.rect.X
	m.Y -= p.rect.Y
	m.Y += p.r.Top()
	cols, rows := p.mirror.Size()
	var b []byte
	var err error
	if m.X < int(cols) && m.Y < int(rows) {
		b, err = c.enc.Mouse(p.mirror, m)
	}
	c.mu.Unlock()
	if claim {
		c.claim(p.info.ID)
	}
	if err == nil && len(b) > 0 {
		c.send(p, proto.FrameInput, b)
	}
}

func (c *client) focusReport(gained bool) {
	if !c.lock() {
		return
	}
	p := c.focus
	if p == nil || c.remoteBlock(p) != "" {
		c.mu.Unlock()
		return
	}
	b := emu.Focus(p.mirror, gained)
	// Gaining the window's focus claims the size like typing does
	// (docs/SPEC.md §3.3); losing it does nothing.
	claim := gained && c.needClaim()
	c.mu.Unlock()
	if claim {
		c.claim(p.info.ID)
	}
	if b != nil {
		c.send(p, proto.FrameInput, b)
	}
}

// outerModes turns mouse tracking and focus reports on the outer terminal
// on or off to match the focused program, so native selection works
// whenever the program doesn't want the mouse. c.mu held.
//
// Terminals keep one mouse tracking mode, not three flags: resetting any
// of 1000, 1002 and 1003 stops all tracking. So the client moves from
// the mode it set to the one it wants, never resetting one after setting
// another.
func (c *client) outerModes() []byte {
	var m emu.Modes
	if c.focus != nil {
		m = c.focus.mirror.Modes()
	}
	track := 0
	switch {
	case m.AnyMouse:
		track = 1003
	case m.ButtonMouse:
		track = 1002
	case m.NormalMouse:
		track = 1000
	}
	if (c.side != nil || c.focus != nil && c.remoteBlock(c.focus) != "") && track < 1002 {
		// The sidebar takes clicks and drags whatever the program wants,
		// and so does a remote pane that takes no keys (its wheel scrolls
		// here); selecting text in a pane then needs Shift, or tm's own
		// drag.
		track = 1002
	}
	if c.side != nil && c.sideW > sideSlim {
		// Every motion, for the sidebar's tooltip; the program still
		// gets only what it asked for (emu's encoder).
		track = 1003
	}
	var b []byte
	set := func(n int, on bool) {
		b = append(b, "\x1b[?"...)
		b = strconv.AppendInt(b, int64(n), 10)
		if on {
			b = append(b, 'h')
		} else {
			b = append(b, 'l')
		}
	}
	if track != c.outerTrack {
		if c.outerTrack != 0 {
			set(c.outerTrack, false)
		}
		if track != 0 {
			set(track, true)
		}
		c.outerTrack = track
	}
	// Always SGR coordinates from the outer terminal while it reports.
	want := map[int]bool{1004: m.Focus, 1006: track != 0}
	for _, n := range []int{1004, 1006} {
		if c.outer[n] != want[n] {
			c.outer[n] = want[n]
			set(n, want[n])
		}
	}
	return b
}

func (c *client) renderLoop(out io.Writer) Result {
	tick := time.NewTicker(frameInterval)
	defer tick.Stop()
	for {
		select {
		case <-c.end:
			return c.result
		case <-c.wake:
		}
		select {
		case <-c.end:
			return c.result
		case <-tick.C:
		}
		c.drawFrame(out)
	}
}

// drawFrame draws what changed since the last frame on out.
func (c *client) drawFrame(out io.Writer) {
	if !c.lock() {
		return
	}
	vis := c.shown()
	held := false
	// The theme's pane palette, for this scheme: the panes are drawn
	// with it, and their programs' colour queries answered with it.
	t := th()
	pal := t.Pane
	repal := pal != c.pal
	c.pal = pal
	c.refill(t, vis)
	for _, p := range vis {
		p.r.SetPalette(pal)
		if p.held && time.Since(p.heldAt) > maxHold {
			p.held = false // the program never ended its update; draw anyway
		}
		held = held || p.held
	}
	var b []byte
	var err error
	switch {
	case c.focus == nil:
	case c.single && len(vis) == 1:
		c.full = false // the renderer repaints after SetSize
		b, err = vis[0].r.Frame(vis[0].mirror, vis[0].held)
	default:
		b, err = c.frameSplit(vis)
	}
	if err == nil && c.menu != nil && (len(b) > 0 || !c.menu.drawn) {
		b = c.appendMenu(b, c.menu)
	}
	if err == nil && c.dialog != nil && (len(b) > 0 || !c.dialog.drawn) {
		b = c.appendMenu(b, c.dialog)
	}
	if err == nil {
		b = c.paint(t, b)
	}
	if err == nil {
		if modes := c.outerModes(); len(modes) > 0 { // usually none: no copy of the frame
			b = append(modes, b...)
		}
		if c.bell {
			b = append(b, '\a')
			c.bell = false
		}
		b = append(b, c.osc...)
		c.osc = nil
		b = append(b, c.clip...)
		c.clip = nil
	}
	c.mu.Unlock()
	if repal {
		c.sendColors()
	}
	if err != nil {
		c.finish(Result{Reason: "render: " + err.Error()})
		return
	}
	if len(b) > 0 {
		if _, err := out.Write(b); err != nil {
			c.finish(Result{Reason: "detached (terminal closed: " + err.Error() + ")", Detached: true, Quit: true})
			return
		}
	}
	if held {
		time.AfterFunc(maxHold, c.poke) // draw once the hold times out
	}
}

// refill follows the console's default colours under theme t: the
// palette's when it has a pane palette (theme.go), the window drawn
// again in them when they change. c.mu held.
func (c *client) refill(t *Theme, vis []*pane) {
	fill := t.fillReset()
	if fill == c.fill {
		return
	}
	c.fill, c.full = fill, true
	for _, p := range vis {
		p.r.Invalidate()
	}
}

// paint is the frame b with the console's own areas, blanks and erases
// in theme t's default colours (theme.go); b when it has none. c.mu
// held.
func (c *client) paint(t *Theme, b []byte) []byte {
	if len(b) == 0 || !t.painting() {
		return b
	}
	c.painted = t.appendPainted(append(c.painted[:0], c.fill...), b)
	return c.painted
}

// frameSplit draws the pane beside the sidebar or above the status bar:
// the sidebar, the pane's changes, the status bar, then the
// focused pane's cursor, all in one synchronised update; nil when nothing
// changed. A full repaint (a new view, the window resized) overwrites
// every cell in place and erases only what nothing draws, never the
// whole screen first: a terminal that shows the update before its end
// (no mode 2026, over a slow link) doesn't flash blank (T372). c.mu held.
func (c *client) frameSplit(vis []*pane) ([]byte, error) {
	b := append(c.buf[:0], "\x1b[?2026h\x1b[?25l"...)
	wrote := false
	if c.full {
		b = c.appendGaps(b, vis)
		for _, p := range vis {
			p.r.Invalidate()
		}
		c.statusDrawn, c.full, wrote = "", false, true
		if c.side != nil {
			c.side.drawn = nil
		}
		if c.info != nil {
			c.info.drawn = nil
		}
	}
	if c.side != nil {
		b, wrote = c.appendSidebar(b, wrote)
	}
	if c.infoW > 0 {
		b, wrote = c.appendInfo(b, wrote)
	}
	for _, p := range vis {
		part, err := p.r.Frame(p.mirror, p.held)
		if err != nil {
			return nil, err
		}
		if len(part) > 0 {
			b, wrote = append(b, part...), true
		}
	}
	if c.statusBar && c.statusText != c.statusDrawn {
		// A faint rule across the window under the sidebar, the pane
		// and the info panel (none when compact), then the bar.
		if c.rows > 1 && c.barRows() > 1 {
			b = append(b, fmt.Sprintf("\x1b[%d;1H\x1b[0m", c.rows-1)...)
			b = append(b, th().Faint.Render(strings.Repeat("─", max(c.cols, 0)))...)
		}
		b = append(b, fmt.Sprintf("\x1b[%d;1H\x1b[0m", c.rows)...)
		b = append(b, c.statusText...)
		b = append(b, "\x1b[0m"...)
		if n := c.cols - ansi.StringWidth(c.statusText); n > 0 {
			b = append(b, fmt.Sprintf("\x1b[%dX", n)...) // what a longer bar left
		}
		c.statusDrawn, wrote = c.statusText, true
	}
	var cur string // the focused pane cropped away: no cursor
	if r := c.focus.rect; r.W > 0 && r.H > 0 {
		cur = string(c.focus.r.Cursor())
	}
	title := oneLine(c.focus.mirror.Title())
	if !wrote && cur == c.lastCursor && title == c.title {
		c.buf = b
		return nil, nil
	}
	b = append(b, cur...)
	c.lastCursor = cur
	if title != c.title {
		c.title = title
		b = append(b, "\x1b]2;"...)
		b = append(b, title...)
		b = append(b, '\a')
	}
	b = append(b, "\x1b[?2026l"...)
	c.buf = b
	return b, nil
}

// appendGaps erases, on a full repaint, the cells above the status bar
// that neither the sidebar, the info panel nor a pane draws over: what
// the previous frame left there (a menu, the dashboard). c.mu held.
func (c *client) appendGaps(b []byte, vis []*pane) []byte {
	b = append(b, "\x1b[0m"...)
	for y := range c.rows - c.barRows() {
		var drawn [][2]int // the columns drawn on row y, [from, to)
		if c.side != nil && c.sideW > 0 && y < c.sideRows() {
			drawn = append(drawn, [2]int{c.sideX(), c.sideX() + c.sideW})
		}
		if c.info != nil && c.infoW > 0 && y < c.infoRows() {
			drawn = append(drawn, [2]int{c.infoX(), c.infoX() + c.infoW})
		}
		for _, p := range vis {
			if r := p.rect; y >= r.Y && y < r.Y+r.H {
				drawn = append(drawn, [2]int{r.X, r.X + r.W})
			}
		}
		slices.SortFunc(drawn, func(a, b [2]int) int { return a[0] - b[0] })
		x := 0
		for _, d := range append(drawn, [2]int{c.cols, c.cols}) {
			if d[0] > x {
				b = fmt.Appendf(b, "\x1b[%d;%dH\x1b[%dX", y+1, x+1, d[0]-x)
			}
			x = max(x, d[1])
		}
	}
	return b
}

func errString(err error) string {
	if err == nil || errors.Is(err, io.EOF) {
		return "EOF"
	}
	return err.Error()
}

// statePoll is how often the status bar asks the server for state.
const statePoll = 500 * time.Millisecond

// pollViewsNow says whether pollState asks mod.views on this tick: when
// the mod sections shown (shownKey) changed since sent, every other
// tick (about once a second) while some show, so their trees stay
// current and the server keeps them open (modview.ShownLife), and every
// tick while a command or a section's click waits for the view its mod
// opens (cmdWait). None shown, it says so once, then costs nothing.
func pollViewsNow(key, sent string, odd, waiting bool) bool {
	return waiting || key != sent || key != "" && !odd
}

// bandShown are the attached sidebar's band sections, and the sections of
// the projects whose rows are on screen, for mod.views,
// keeping secs, the sidebar sections session.list names.
func (c *client) bandShown(secs []proto.ModSection) []modview.Shown {
	if !c.lock() {
		return nil
	}
	defer c.mu.Unlock()
	if c.side == nil {
		return nil
	}
	c.side.sections = secs
	if !c.bandShows() {
		return nil
	}
	in, rows := c.band(), c.sideTree()
	return append(bandShown(in), projShown(rows, in, sideOnScreen(rows, c.sideW, c.sideRows(), c.sideFoot()))...)
}

// setViews takes mod.views' list, and hands it on to the dashboard this
// console may show next (handViews). c.mu held.
func (c *client) setViews(list []proto.ModView) {
	c.views = list
	handViews(list)
	if c.side != nil {
		c.side.band.forget(func(k string) bool {
			return slices.ContainsFunc(list, func(v proto.ModView) bool { return strings.HasPrefix(k, v.Mod+"/"+v.View+"/") })
		})
	}
}

// idleRedraw is how often a console draws again while its poll finds
// nothing changed: what shows ages ("4s", a mod's countdown) stays
// current to the second.
const idleRedraw = time.Second

// polled is what one tick of pollState read, for applyPoll.
type polled struct {
	res        proto.SessionListResult
	connected  bool // session.list answered
	ring       bool
	osc        []byte
	projects   []ProjectData // nil: unchanged since the last tick
	peers      []proto.PeerMirror
	ctxHint    int
	needsFirst bool
	statuses   map[string]*thread.Status
	info       *infoData
	views      *proto.ModViewsResult
	cmds       []mod.ModCommand
	reload     bool // cmds were read
}

// pollState keeps the status bar, the sidebar and the info panel current
// and rings the bell when the server sent a notification (a session
// blocked, a thread reported; docs/SPEC.md §4). It polls session.list,
// and mod.views while the info panel or the sidebar shows mod sections
// (pollViewsNow); a lost server is noticed by the attach streams
// themselves. The sidebar's projects come from the server's
// projects.watch (sideFeed); what it reads from files it reads again
// only where they changed (sidecache.go), and it draws again only when
// what it read changed, or idleRedraw after the last time.
func (c *client) pollState(ctx context.Context) {
	var ctl *tmclient.Client
	defer func() {
		if ctl != nil {
			ctl.Close()
		}
	}()
	var alerts uint64
	first := true
	noticed := map[string]bool{}
	// sent is the mod sections the last mod.views said this console
	// shows (shownKey); none sent yet at first, so the first poll says
	// what shows, sections the dashboard showed before it too.
	sent, odd := "\x00", false
	var conf configCache
	feed := c.sideFeed
	if c.side != nil {
		if feed == nil {
			feed = &sideFeed{}
		}
		go feed.run(ctx, c.paths)
	}
	var info infoCache
	stats := statusCache{}
	tick := time.NewTicker(statePoll)
	defer tick.Stop()
	for {
		if ctl == nil {
			ctl, _ = tmclient.Connect(c.paths, false)
		}
		var pl polled
		if ctl != nil {
			if err := ctl.Call(proto.MethodSessionList, nil, &pl.res); err != nil {
				ctl.Close()
				ctl = nil
			}
		}
		res := &pl.res
		if ctl != nil {
			pl.connected = true
			pl.ring = !first && res.Alerts > alerts
			alerts, first = res.Alerts, false
			pl.osc = claimNotices(ctl, res.ModNotices, noticed)
		}
		pl.ctxHint = config.DefaultContextHint
		if c.side != nil {
			if projects, changed := feed.take(); changed {
				pl.projects = projects
			}
			pl.ctxHint, pl.needsFirst = conf.uiHints()
			if ctl != nil {
				pl.peers = loadPeers(ctl.Call)
			}
		}
		pl.statuses = stats.statuses(res.Sessions)
		var shown []modview.Shown
		pl.info, shown = c.pollInfo(&info, res.Sessions, pl.peers)
		var secs []proto.ModSection
		if res.Mods != nil {
			secs = res.Mods.Sections
		}
		shown = append(shown, c.bandShown(secs)...)
		w := c.waiting()
		if key := shownKey(shown); ctl != nil && pollViewsNow(key, sent, odd, w != nil) {
			pl.views, sent = pollViews(ctl, shown), key
		}
		odd = !odd
		c.mu.Lock()
		cmdsAt := c.cmdsAt
		c.mu.Unlock()
		if pl.reload = time.Since(cmdsAt) >= cmdReload; pl.reload {
			cfg, err := conf.get()
			if err != nil {
				cfg = nil
			}
			pl.cmds = commandsWith(ctl, cfg)
		}
		c.applyPoll(&pl, time.Now())
		if w != nil {
			c.takeOpen(w, pl.views)
		}
		select {
		case <-ctx.Done():
			return
		case <-c.end:
			return
		case <-tick.C:
		}
	}
}

// applyPoll takes what a tick of pollState read, and draws again when
// it changed what shows, or idleRedraw after the last time (at now). It
// reports whether it drew.
func (c *client) applyPoll(pl *polled, now time.Time) bool {
	if !c.lock() {
		return false
	}
	changed := pl.ring || len(pl.osc) > 0
	res := &pl.res
	if c.side != nil {
		if pl.projects != nil {
			c.side.projects, changed = pl.projects, true
		}
		if !reflect.DeepEqual(c.side.sessions, res.Sessions) {
			c.side.sessions, changed = res.Sessions, true
		}
		if !reflect.DeepEqual(c.side.peers, pl.peers) {
			c.side.peers, changed = pl.peers, true
		}
		if c.side.ctxHint != pl.ctxHint || c.side.needsFirst != pl.needsFirst {
			c.side.ctxHint, c.side.needsFirst, changed = pl.ctxHint, pl.needsFirst, true
		}
		if pl.connected {
			if si := (ServerInfo{Version: res.Version, Build: res.Build, Latest: res.Latest, Upgrade: res.Upgrade}); si != c.side.server {
				c.side.server, changed = si, true
			}
			if n := oldConsoleNews(c.side.server); n != "" && c.flash == "" {
				c.flash, changed = n, true
			}
		}
	}
	for _, p := range c.panes {
		for _, s := range res.Sessions {
			if s.ID == p.info.ID && (p.status != pl.statuses[s.ID] || !reflect.DeepEqual(p.info, s)) {
				p.info, p.status, changed = s, pl.statuses[s.ID], true
			}
		}
		if p.info.Server != "" {
			if s := remoteInfo(c.side.peers, p.info); !reflect.DeepEqual(p.info, s) {
				if p == c.focus && s.ViewOnly != p.info.ViewOnly && strings.HasPrefix(c.flash, viewOnlyNote) {
					c.flash = "" // its server raised or lowered its switch
				}
				p.info, changed = s, true
			}
		}
	}
	if cells := newModCells(res.Mods, res.Sessions); !reflect.DeepEqual(cells, c.cells) {
		c.cells, changed = cells, true
	}
	if pl.views != nil {
		changed = changed || !reflect.DeepEqual(c.views, pl.views.Views)
		c.setViews(pl.views.Views)
	}
	if c.showInfo(pl.info) {
		changed = true
	}
	if pl.reload {
		changed = changed || !reflect.DeepEqual(c.cmds, pl.cmds)
		c.cmds, c.cmdsAt = pl.cmds, now
	}
	draw := changed || now.Sub(c.polledAt) >= idleRedraw-statePoll/4 // ticks jitter
	if draw {
		if !changed && c.info != nil {
			c.infoLayout() // its ages
		}
		c.polledAt = now
		c.status()
	}
	c.bell = c.bell || pl.ring
	c.osc = append(c.osc, pl.osc...)
	c.mu.Unlock()
	if draw {
		c.poke()
	}
	return draw
}

// adopt moves np's connection, mirror and renderer, attached anew to
// p's session, into p and returns p's old connection, for the caller to
// close; ok is false, with nothing moved, when p is gone. The mirror's
// hooks are bound to p, not np. c.mu and p.wmu held.
func (c *client) adopt(p, np *pane) (old *tmclient.Client, ok bool) {
	if p.gone {
		return nil, false
	}
	old = p.conn
	if p.mirror != nil {
		p.mirror.Close()
	}
	if p.r != nil {
		p.r.Close()
	}
	p.conn, p.mirror, p.r, p.info = np.conn, np.mirror, np.r, np.info
	p.scrolled = false
	c.forget(p)
	c.bindMirror(p)
	p.restarting = false
	return old, true
}

// discard closes a pane open made that never joined the window.
func (p *pane) discard() {
	p.conn.Close()
	p.mirror.Close()
	p.r.Close()
}

// reattachWait bounds how long a restarting session may take to be back.
const reattachWait = 15 * time.Second

// reattach attaches p to its session again once the server relaunched it
// under the same id (proto.ClosedRestarting), keeping p's place in the
// layout; if it doesn't come back, the pane ends.
func (c *client) reattach(p *pane) {
	deadline := time.Now().Add(reattachWait)
	for {
		if !c.lock() {
			return
		}
		gone, id := p.gone, p.info.ID
		c.mu.Unlock()
		if gone {
			return
		}
		np, err := c.open(id)
		if err == nil {
			p.wmu.Lock()
			if !c.lock() {
				p.wmu.Unlock()
				np.discard()
				return
			}
			old, ok := c.adopt(p, np)
			if !ok { // the pane left the window meanwhile
				c.mu.Unlock()
				p.wmu.Unlock()
				np.discard()
				return
			}
			// Place the new renderer; like any attach, no resize.
			c.relayout()
			c.flash = paneName(p.info) + " is back"
			c.status()
			c.mu.Unlock()
			p.wmu.Unlock()
			old.Close()
			go c.readLoop(p)
			c.poke()
			return
		}
		if time.Now().After(deadline) {
			if !c.lock() {
				return
			}
			p.restarting = false
			c.mu.Unlock()
			c.ended(p, "session restart failed: "+errMessage(err))
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}
