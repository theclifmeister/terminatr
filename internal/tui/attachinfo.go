package tui

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	tmclient "github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/emu"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/plat/open"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/view"
)

// The info panel in the attach view: the client's panel beside the
// session, its keys, mouse and polls (infopanel.go lays it out).

// infoX is the panel's first column (its border). c.mu held.
func (c *client) infoX() int {
	if c.v.Sidebar.Right() {
		return max(c.cols-c.sideW-c.infoW, 0) // between the pane and the sidebar
	}
	return c.cols - c.infoW
}

// infoClock is the clock the panel's ages are drawn by; tests pin it.
var infoClock = time.Now

// infoLayout lays the panel's content out at its width: when its data
// or its width changes, not with every frame. c.mu held.
func (c *client) infoLayout() {
	ip := c.info
	ip.lay = infoLay(ip.data, max(c.infoW-1, 1), infoClock(), c.infoOpts())
	ip.lines, ip.hits = ip.lay.lines, ip.lay.hits
	ip.top = min(ip.top, max(len(ip.lines)-c.infoRows(), 0))
	if ip.sel != "" && !slices.ContainsFunc(ip.lay.secs, func(s *sideSection) bool { return s.id == ip.sel }) {
		ip.sel = "" // its section is gone
	}
}

// infoOpts are this console's state of the panel's mod sections. c.mu
// held.
func (c *client) infoOpts() panelOpts {
	ip := c.info
	o := panelOpts{order: ip.detOrder, folds: c.v.Tree, sel: ip.sel, focused: c.kb == areaInfo, rows: c.infoRows(), cache: &ip.cache}
	if ip.data != nil && ip.data.watch != nil {
		o.order = ip.coordOrder
	}
	return o
}

// infoSelShows says whether the selected header is on screen. c.mu held.
func (c *client) infoSelShows() bool {
	ip := c.info
	i := slices.IndexFunc(ip.lay.secs, func(s *sideSection) bool { return s.id == ip.sel })
	if i < 0 {
		return false
	}
	h := ip.lay.heads[i]
	return h >= ip.top && h < ip.top+c.infoRows()
}

// infoScroll moves the panel's content by d lines. c.mu held.
func (c *client) infoScroll(d int) {
	ip := c.info
	ip.top = min(max(ip.top+d, 0), max(len(ip.lines)-c.infoRows(), 0))
	if ip.sel != "" && !c.infoSelShows() {
		// A header scrolled off screen drops the selection.
		ip.sel = ""
		c.infoLayout()
	}
}

// infoRows is the panel's height: the window less the status bar and the
// rule over it, which run across the window, as under the sidebar.
// c.mu held.
func (c *client) infoRows() int {
	return max(c.rows-c.barRows(), 1)
}

// appendInfo draws the panel's changed lines: its border, then its
// content from top. c.mu held.
func (c *client) appendInfo(b []byte, wrote bool) ([]byte, bool) {
	ip := c.info
	border := th().Faint.Render("│")
	if c.kb == areaInfo {
		border = th().Accent.Render("│")
	}
	x, w := c.infoX(), c.infoW-1
	lines := make([]string, c.infoRows())
	for y := range lines {
		l := ""
		if i := ip.top + y; i < len(ip.lines) {
			l = ip.lines[i]
		}
		lines[y] = border + fit(plainUnder(l), w) + reset
		if y < len(ip.drawn) && ip.drawn[y] == lines[y] {
			continue
		}
		b = append(b, fmt.Sprintf("\x1b[%d;%dH\x1b[0m", y+1, x+1)...)
		b = append(b, lines[y]...)
		wrote = true
	}
	ip.drawn = lines
	return b, wrote
}

// infoHint is the status bar's note while the panel has the keyboard;
// remoteHint beside a paired server's pane, whose task and questions are
// that server's.
const (
	infoHint   = "info panel ▸ ↑ ↓ scroll · enter task · esc back to the pane"
	remoteHint = "info panel ▸ ↑ ↓ scroll · esc back to the pane"
)

// infoHintNow is infoHint, naming a when a coordinator's panel has open
// questions to answer, and the section keys while it has mod sections.
// c.mu held.
func (c *client) infoHintNow() string {
	if c.info == nil {
		return infoHint
	}
	if d := c.info.data; d != nil && d.remote != nil {
		return remoteHint
	}
	hint := infoHint
	if keys := panelHintKeys(c.info.lay, c.info.sel); keys != "" {
		enter := " · enter task"
		if c.info.sel != "" {
			enter = ""
		}
		hint = strings.Replace(hint, " · enter task", " · "+keys+enter, 1)
	}
	if d := c.info.data; d != nil && d.watch != nil && d.watch.Questions > 0 {
		return strings.Replace(hint, " · esc", " · a answers questions · esc", 1)
	}
	return hint
}

// setInfo changes the view's info panel: a layout change, so every
// console's pane follows. save also keeps it in ui.json, as the default
// of new views.
func (c *client) setInfo(l view.Info, save bool) {
	c.act(proto.MethodViewInfo, proto.ViewParams{Info: &l})
	if !save || !c.lock() {
		return
	}
	path := ""
	if c.side != nil {
		path = c.side.uiFile
	}
	c.mu.Unlock()
	if path == "" {
		return
	}
	if err := SaveInfo(path, l); err != nil && c.lock() {
		c.flash = "ui.json: " + err.Error()
		c.status()
		c.mu.Unlock()
		c.poke()
	}
}

// infoToggle shows or hides the panel (prefix+|).
func (c *client) infoToggle() {
	if !c.lock() {
		return
	}
	if !c.v.Panel {
		c.flash = "the info panel shows beside a thread's or a coordinator's pane"
		c.status()
		c.mu.Unlock()
		return
	}
	l, msg := c.v.Info.Toggle(c.viewCols(), c.geo.SideW)
	if l.Off && c.kb == areaInfo {
		c.kb = areaMain
	}
	c.flash = msg
	c.status()
	c.mu.Unlock()
	c.setInfo(l, true)
}

// infoKeyboard runs a key while the panel has the keyboard, by the
// panel's key table (panelActions): the arrows scroll it, ] and [ select
// a mod section's header, ← → and enter fold, unfold or open the
// selected one; with none selected enter shows the task; a has a
// coordinator open its questions; esc drops the selection, then, as tab
// (prefix+tab: to the next area, the pane), gives the keyboard back to
// the pane; any other key is dropped. c.mu held; released here.
func (c *client) infoKeyboard(k uv.Key) {
	c.flash = ""
	ip := c.info
	task, answer, clear := false, false, false
	var do *sideStep
	name := keyName(k)
	op, ok := panelOp(name)
	switch {
	case ip.data != nil && ip.data.remote != nil && (name == "a" || ok && op == panelEnter && ip.sel == ""):
		// The task and the questions are the paired server's.
		c.flash = ip.data.remote.alias + "'s: view-only here"
	case ip.data != nil && ip.data.remote != nil && ok && op == panelSaveClear:
		c.flash = ip.data.remote.alias + "'s: view-only here"
	case name == "a":
		answer = true
	case ok && op == panelSaveClear:
		clear = ip.data != nil && ip.data.watch != nil
		if !clear {
			c.flash = barKey("panel.save-clear", "c") + " beside a coordinator saves and clears it"
		}
	case name == "tab":
		c.kb = areaMain
		c.infoLayout()
	case !ok:
	case op == panelUp:
		c.infoScroll(-1)
	case op == panelDown:
		c.infoScroll(1)
	case op == panelPageUp:
		c.infoScroll(-c.rows / 2)
	case op == panelPageDown:
		c.infoScroll(c.rows / 2)
	case op == panelFirst:
		c.infoScroll(-len(ip.lines))
	case op == panelLast:
		c.infoScroll(len(ip.lines))
	case op == panelBack && ip.sel != "":
		ip.sel = ""
		c.infoLayout()
	case op == panelBack:
		c.kb = areaMain
		c.infoLayout()
	default:
		st, next, took := panelStep(ip.lay, ip.sel, op)
		switch {
		case !took && op == panelEnter:
			task = true
		case !took:
		case next != ip.sel:
			ip.sel = next
			c.infoLayout()
			c.infoReveal()
		default:
			do = &st
		}
	}
	c.status()
	c.mu.Unlock()
	c.poke()
	if do != nil {
		c.infoSection(*do)
	}
	if task {
		c.infoTask(0)
	}
	if answer {
		c.infoQuestions()
	}
	if clear {
		c.saveClear(ip.data.slug)
	}
}

// infoReveal scrolls the panel so the selected header shows. c.mu held.
func (c *client) infoReveal() {
	ip := c.info
	i := slices.IndexFunc(ip.lay.secs, func(s *sideSection) bool { return s.id == ip.sel })
	if i < 0 || ip.lay.heads[i] < 0 {
		return
	}
	h, rows := ip.lay.heads[i], c.infoRows()
	switch {
	case h < ip.top:
		ip.top = h
	case h >= ip.top+rows:
		ip.top = h - rows + 1
	}
	ip.top = min(max(ip.top, 0), max(len(ip.lines)-rows, 0))
}

// infoSection runs what a key or a click on a mod section does
// (sectionDo: folds through view.sections, opens through ui.click), then
// lays the panel out with its folds.
func (c *client) infoSection(st sideStep) {
	c.sectionDo(st)
	if c.lock() {
		c.infoLayout()
		c.status()
		c.mu.Unlock()
		c.poke()
	}
}

// openQuestions has project slug's coordinator open its questions
// (questions.open); a variable for tests.
var openQuestions = func(paths tmclient.Paths, slug string) (proto.QuestionsOpenResult, error) {
	var res proto.QuestionsOpenResult
	cl, err := tmclient.Connect(paths, false)
	if err != nil {
		return res, err
	}
	defer cl.Close()
	err = cl.Call(proto.MethodQuestionsOpen, proto.QuestionsOpenParams{Project: slug}, &res)
	return res, err
}

// infoQuestions has the coordinator whose panel shows open its questions
// in its question dialog, and says so in the status bar.
func (c *client) infoQuestions() {
	if !c.lock() {
		return
	}
	d, paths := c.info.data, c.paths
	c.mu.Unlock()
	slug, n := "", 0
	if d != nil && d.watch != nil {
		slug, n = d.slug, d.watch.Questions
	}
	flash := "no open questions"
	if n > 0 {
		flash = questionsFlash(openQuestions(paths, slug))
	}
	if c.lock() {
		c.flash = flash
		c.status()
		c.mu.Unlock()
		c.poke()
	}
}

// coordinatorClear has project slug's coordinator save and clear
// (coordinator.clear); a variable for tests.
var coordinatorClear = func(paths tmclient.Paths, slug string) (proto.CoordinatorClearResult, error) {
	var res proto.CoordinatorClearResult
	cl, err := tmclient.Connect(paths, false)
	if err != nil {
		return res, err
	}
	defer cl.Close()
	err = cl.Call(proto.MethodCoordinatorClear, proto.CoordinatorClearParams{Project: slug}, &res)
	return res, err
}

// saveClear has project slug's coordinator save what it learned, then
// clears it (the context hint's key, in the panel or the sidebar), and
// says so in the status bar. Not with c.mu held: it calls the server.
func (c *client) saveClear(slug string) {
	if !c.lock() {
		return
	}
	paths := c.paths
	c.mu.Unlock()
	res, err := coordinatorClear(paths, slug)
	if c.lock() {
		c.flash = saveClearWords(slug, res, err)
		c.status()
		c.mu.Unlock()
		c.poke()
	}
}

// saveClearWords says what came of a save and clear of slug's
// coordinator.
func saveClearWords(slug string, res proto.CoordinatorClearResult, err error) string {
	if err != nil {
		var perr *proto.Error
		if errors.As(err, &perr) {
			return "not cleared: " + perr.Message
		}
		return "not cleared: " + err.Error()
	}
	return fmt.Sprintf("asked %s's coordinator to save what it learned; it is cleared at its next idle moment after that", slug)
}

// questionsFlash says what came of asking the coordinator to open its
// questions.
func questionsFlash(res proto.QuestionsOpenResult, err error) string {
	if err != nil {
		var perr *proto.Error
		if errors.As(err, &perr) {
			return "not asked: " + perr.Message
		}
		return "not asked: " + err.Error()
	}
	return "asked the coordinator: your questions open in its dialog once it is free"
}

// infoTask opens the task view on task id over the session; 0 is the
// thread's own task (enter).
func (c *client) infoTask(id int) {
	if !c.lock() {
		return
	}
	d := c.info.data
	if id == 0 && d != nil && d.task != nil {
		id = d.task.ID
	}
	if d == nil || id == 0 || !c.dashboard {
		switch {
		case id != 0:
			c.flash = "T" + strconv.Itoa(id) + ": tm shows tasks on the dashboard"
		case d != nil && d.watch != nil:
			c.flash = "click a task to open it"
		default:
			c.flash = "no task"
		}
		c.status()
		c.mu.Unlock()
		c.poke()
		return
	}
	slug := d.slug
	c.kb = areaMain
	c.mu.Unlock()
	c.popupTask(slug, id)
}

// openURL opens a web address in the browser (a variable for tests).
var openURL = open.URL

// infoMouse handles the mouse over the panel or dragging its border: the
// border drags, a click on the task or the PR opens it, any other click
// gives the panel the keyboard; the wheel scrolls it. c.mu held;
// released here.
func (c *client) infoMouse(m emu.Mouse) {
	ip := c.info
	switch {
	case ip.drag && m.Action == emu.MouseMotion:
		// The border's column in the view's window, which may be another
		// size than this one.
		gap := c.cols - m.X
		if c.v.Sidebar.Right() {
			gap -= c.sideW
		}
		x := c.viewCols() - gap
		l := c.v.Info.DragTo(x, c.viewCols(), c.geo.SideW)
		same := l == c.v.Info
		c.mu.Unlock()
		if !same {
			c.setInfo(l, false)
		}
		c.poke()
		return
	case ip.drag && m.Action == emu.MouseRelease:
		ip.drag = false
		l := c.v.Info
		c.mu.Unlock()
		c.setInfo(l, true)
		return
	case m.Action == emu.MousePress && (m.Button == emu.MouseWheelUp || m.Button == emu.MouseWheelDown):
		c.infoScroll(map[bool]int{true: -3, false: 3}[m.Button == emu.MouseWheelUp])
		c.mu.Unlock()
		c.poke()
		return
	case m.Action != emu.MousePress:
		c.mu.Unlock()
		return
	case m.Button == emu.MouseRight:
		c.openMenu(c.menuTitle(), c.sessionItems(), m.X, m.Y, false)
		c.mu.Unlock()
		c.poke()
		return
	case m.Button != emu.MouseLeft:
		c.mu.Unlock()
		return
	}
	if m.X == c.infoX() {
		ip.drag = true
		c.mu.Unlock()
		return
	}
	var h infoHit
	i := ip.top + m.Y
	if i >= 0 && i < len(ip.hits) {
		h = ip.hits[i]
	}
	// A click in the panel gives it the keyboard.
	c.kb = areaInfo
	var do *sideStep
	if h.kind == hitSection {
		// A click on a mod section selects its header and folds, unfolds
		// or opens it.
		if j := slices.IndexFunc(ip.lay.secs, func(s *sideSection) bool { return s.id == h.sec }); j >= 0 {
			st := panelHeadClick(ip.lay.secs[j], m.X-c.infoX()-1, ip.lay.headAt(i) >= 0)
			do = &st
			ip.sel = h.sec
		}
	}
	c.infoLayout()
	slug := ""
	if ip.data != nil {
		slug = ip.data.slug
	}
	c.status()
	c.mu.Unlock()
	c.poke()
	if do != nil {
		c.infoSection(*do)
	}
	switch h.kind {
	case hitTask:
		c.infoTask(h.task)
	case hitSession:
		c.sideGo(Target{Project: slug, Session: h.session})
	case hitQuestions:
		c.infoQuestions()
	case hitSaveClear:
		c.saveClear(slug)
	case hitPR:
		if err := openURL(h.url); err != nil && c.lock() {
			c.flash = "can't open the browser: " + err.Error() + "; the PR is " + h.url
			c.status()
			c.mu.Unlock()
			c.poke()
		}
	}
}

// pollInfo reads the panel's data for the focused session through ic,
// outside c.mu, and the mods' sections it shows, by the views mod.views
// last listed (pollState asks it, and showInfo takes its answer):
// beside a coordinator its coordinator sections, beside a thread the
// details sections for that thread; nil, nil without the panel. A
// paired server's pane's is from peers, the mirror, with no sections.
func (c *client) pollInfo(ic *infoCache, sessions []proto.SessionInfo, peers []proto.PeerMirror) (*infoData, []modview.Shown) {
	if !c.lock() {
		return nil, nil
	}
	id, on := c.v.Focus, c.info != nil
	var prev []proto.ModView
	var remote *proto.SessionInfo
	w := 0
	if on {
		prev, w = c.views, c.infoW-1
		if c.focus != nil && c.focus.info.ID == id && c.focus.info.Server != "" {
			in := c.focus.info
			remote = &in
		}
	}
	c.mu.Unlock()
	if !on {
		return nil, nil
	}
	if remote != nil {
		// A paired server's pane: the mirror's, and no mod sections.
		return loadRemoteInfo(peers, *remote), nil
	}
	var d *infoData
	for _, s := range sessions {
		if s.ID == id {
			d = ic.load(c.paths, s, sessions)
		}
	}
	var shown []modview.Shown
	if d != nil && w > 0 {
		shown = panelShown(prev, infoTarget(d), panelOpts{}, w)
	}
	return d, shown
}

// infoTarget is what the mods' sections in d's panel show for: a
// coordinator's project, or a thread.
func infoTarget(d *infoData) panelTarget {
	if d.watch != nil {
		return panelTarget{slot: mod.SlotCoordinatorSection, project: d.slug}
	}
	return panelTarget{slot: mod.SlotDetailsSection, project: d.slug, thr: d.session.Thread}
}

// showInfo shows d in the panel, with the mods' views as mod.views last
// listed them for its sections, and reports whether it differs from
// what the panel showed. c.mu held.
func (c *client) showInfo(d *infoData) bool {
	if c.info == nil {
		return false
	}
	if d != nil && d.remote == nil {
		d.sections = c.views
	}
	if reflect.DeepEqual(c.info.data, d) {
		return false
	}
	c.info.data = d
	c.infoLayout()
	return true
}

// pollViews is mod.views on ctl, saying the console shows shown: the
// mods' views and the views they ask to open, nil without a server (or
// an older one).
func pollViews(ctl *tmclient.Client, shown []modview.Shown) *proto.ModViewsResult {
	if ctl == nil {
		return nil
	}
	var res proto.ModViewsResult
	if ctl.Call(proto.MethodModViews, proto.ModViewsParams{Shown: shown}, &res) != nil {
		return nil
	}
	return &res
}
