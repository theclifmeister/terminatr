package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tui"
	"github.com/theclifmeister/terminatr/internal/view"
)

// dashboardCmd is `tm` with no arguments, or `tm --own`: a console of
// the server-owned view (docs/SPEC.md §3.3, §4), view main unless own.
// It shows the view's screen, the dashboard or the attached layout, and
// follows it when this console or another changes it.
func (e *Env) dashboardCmd(own bool) int {
	return e.report("tm", e.fullConsole(own, nil))
}

// uiFile is ui.json, the console's layout (docs/SPEC.md §5.1); "" when
// there is no home.
func uiFile() string {
	if d, err := home.Dir(); err == nil {
		return filepath.Join(d, "ui.json")
	}
	return ""
}

// warnRaw notes, before a console starts, a terminal that a killed
// client left in raw mode.
func (e *Env) warnRaw(prefix string) {
	if rawMode(os.Stdin) {
		fmt.Fprintln(e.Stderr, prefix+": this terminal looks like it was left in raw mode (a client killed with SIGKILL?); run `reset` if it misbehaves")
	}
}

// console is what a console joins its view with: the window's size
// (80×24 without a terminal) and its layout in ui.json.
type console struct {
	uiFile     string
	cols, rows uint16
	lay        tui.Layout
}

func newConsole() console {
	c := console{uiFile: uiFile()}
	var ok bool
	if c.cols, c.rows, ok = termSize(); !ok {
		c.cols, c.rows = 80, 24
	}
	c.lay = tui.ConsoleLayout(c.uiFile)
	return c
}

// params are the console's view.subscribe params: its size and layout.
func (c console) params() proto.ViewSubscribeParams {
	side, info, tree := c.lay.Sidebar, c.lay.Info, c.lay.Tree
	return proto.ViewSubscribeParams{Cols: c.cols, Rows: c.rows, Sidebar: &side, Info: &info, Tree: &tree, StatusCompact: c.lay.StatusCompact()}
}

// joinBare joins a bare view of the console's own on session id, with a
// status bar.
func (c console) joinBare(p client.Paths, id string) (*tui.ViewConn, error) {
	params := c.params()
	params.Own, params.Bare, params.StatusBar, params.Session = true, true, true, id
	return tui.JoinView(p, params)
}

// fullConsole runs a full console: the dashboard and the attached
// layout of view main (own: a view of its own). goTo, from a sidebar
// click in tm attach or tm project open, is opened first: that bare view
// hands its console over to this one (docs/SPEC.md §3.3).
func (e *Env) fullConsole(own bool, goTo *tui.Target) error {
	if !isTTY(os.Stdin) || !isTTY(os.Stdout) {
		fmt.Fprintln(e.Stderr, "tm: the dashboard needs a terminal; see tm session list")
		return &exitError{ExitUsage}
	}
	e.warnRaw("tm")
	c, p, err := connect(true)
	if err != nil {
		return srvFail("dashboard", err)
	}
	modsNote := e.modsRunNote(c)
	c.Close()
	who := e.Caller
	if e.Identify != nil {
		if c, ok := e.Identify(); ok {
			who = caller.Narrower(who, c)
		}
	}
	src := &tui.ServerSource{Paths: p, Caller: who}
	defer src.Close()
	con := newConsole()
	uiFile := con.uiFile
	params := con.params()
	params.Own = own
	vc, err := tui.JoinView(p, params)
	if err != nil {
		return srvFail("dashboard", err)
	}
	defer vc.Close()
	args := []string{}
	if own {
		args = []string{"--own"}
	}
	st := tui.DashState{Message: modsNote}
	if goTo != nil {
		if err := tui.OpenTarget(p, vc, *goTo); err != nil {
			st.Message = err.Error()
		}
	}
	attach := vc.View().Mode == view.ModeLayout
	flash := modsNote  // the popup's last message, for the session's status bar
	command := ""      // a prefix command typed over the popup, for the session
	sideFocus := false // the sidebar has the keyboard, across screens
	// handover: the screen is up, the last screen's frame showing, for the
	// next to draw over (T372); the first starts it.
	handover := false
	for {
		var over *tui.Over
		if attach {
			ares, err := e.attach(p, vc, tui.Options{Sidebar: &tui.SidebarOptions{UIFile: uiFile, Focus: sideFocus},
				Flash: flash, Command: command, Handover: handover, Back: true}, args)
			if err != nil {
				return err
			}
			if ares.Quit {
				return nil
			}
			handover = true
			st.Message = strings.TrimPrefix(ares.Session+": "+ares.Reason, ": ")
			st.Then = ares.Then // prefix then p, ] or [ in the session
			st.SideFocus = ares.SideFocus
			if ares.Then != "" || ares.GoTo != nil || ares.Over != nil {
				// Left to run a key, open a row or a popup: no news.
				st.Message = ""
			}
			// Prefix then a, i, t, , or ?: the popup over the session,
			// which attaches again once it closes.
			over = ares.Over
		}
		// Start at the window's size now: waiting for the program's own
		// size leaves the dashboard at 80×24 until it arrives (Windows).
		wc, wr, _ := termSize()
		res, err := tui.Dashboard(tui.DashOptions{Source: src, In: os.Stdin, Out: os.Stdout, Width: int(wc), Height: int(wr),
			Cwd: e.Cwd, State: st, UIFile: uiFile, Prefix: tui.ConfigPrefix(), View: vc, Over: over, Handover: handover})
		if err != nil {
			return srvFail("dashboard", err)
		}
		if res.Attach == "" {
			return nil
		}
		handover = true
		attach, st, flash, command, sideFocus = true, tui.DashState{}, res.Message, res.Command, res.SideFocus
	}
}

// openCmd is `tm project open <slug>`: start or attach the project's
// coordinator, in a bare view of this console's own. Without a terminal
// it prints the session id. A click on the sidebar hands the console
// over to a full one of view main.
func (e *Env) openCmd(slug, agentName string) error {
	if _, err := project.Open(slug); err != nil {
		return err
	}
	c, p, err := connect(true)
	if err != nil {
		return err
	}
	// The pane gets the window less the sidebar and the status bar.
	con := newConsole()
	cols, rows := con.cols, con.rows
	id, err := tui.OpenCoordinator(c.Call, slug, agentName, max(int(cols)-con.lay.Sidebar.Cols(int(cols)), 1), int(max(rows, 2)-1))
	c.Close()
	if err != nil {
		return err
	}
	if !isTTY(os.Stdin) || !isTTY(os.Stdout) {
		fmt.Fprintln(e.Stdout, id)
		return nil
	}
	vc, err := con.joinBare(p, id)
	if err != nil {
		return err
	}
	args := []string{"project", "open", slug}
	if agentName != "" {
		args = append(args, "--agent", agentName)
	}
	res, err := e.attach(p, vc, tui.Options{Sidebar: &tui.SidebarOptions{UIFile: con.uiFile}}, args)
	vc.Close()
	if err != nil {
		return err
	}
	if res.GoTo != nil {
		return e.fullConsole(false, res.GoTo)
	}
	fmt.Fprintf(e.Stdout, "[%s: %s]\n", id, res.Reason)
	return nil
}

// modsRunNote is the hint when tm --no-mods found a server already
// running with user mods (safe mode is the server's, set when it
// starts); "" otherwise. It goes on stderr, which the dashboard covers
// at once, and in the console's message line, so it is read as it
// starts and not only after quitting (T301 B8).
func (e *Env) modsRunNote(c *client.Client) string {
	if !mod.NoMods(e.Getenv) || c.Started {
		return ""
	}
	var res proto.ModHostsResult
	if err := c.Call(proto.MethodModHosts, nil, &res); err != nil || res.NoMods {
		return ""
	}
	note := "--no-mods: the server was already running with user mods; tm --no-mods server restart restarts it without them"
	fmt.Fprintln(e.Stderr, "tm: "+note)
	return note
}
