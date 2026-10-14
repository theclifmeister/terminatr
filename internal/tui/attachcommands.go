package tui

import (
	"errors"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
)

// The mods' commands in a session (docs/SPEC.md §5.3, Commands): prefix
// then a command's key runs it, as on the dashboard; a plain key still
// goes to the program. The core's prefix commands come first (coreKey),
// so a command whose key the core has keeps only its ≡ menu entry, which
// the session's menu lists after the prefix commands. A command runs in
// the focused session's project, with that session as the selection
// (sessionTarget). A view the mod then opens shows over the session (a
// full view, a project tab), or on the dashboard (a card): the session
// watches the mod's opens for a while (cmdWait).

// cmdReload is how often a session reads the mods' commands again.
const cmdReload = 2 * time.Second

// cmdWait is a command run from this session: an open of mod's view
// with a Seq greater than after, until until, shows here.
type cmdWait struct {
	mod   string
	after uint64
	until time.Time
}

// loadCommands are the mods' commands that run: none of a user mod's
// while ctl's server runs in safe mode.
func loadCommands(ctl *server.Client) []mod.ModCommand {
	cfg, err := config.Load()
	if err != nil {
		cfg = nil
	}
	return commandsWith(ctl, cfg)
}

// commandsWith are loadCommands' commands, with config cfg (nil for one
// that doesn't load).
func commandsWith(ctl *server.Client, cfg *config.Config) []mod.ModCommand {
	reg, err := mod.Load(cfg)
	if err != nil {
		return nil
	}
	noMods := false
	if ctl != nil {
		var hosts proto.ModHostsResult
		if ctl.Call(proto.MethodModHosts, nil, &hosts) == nil {
			noMods = hosts.NoMods
		}
	}
	return runningCommands(reg.Commands(), noMods)
}

// sessionCommand is the command prefix then key k runs: one that has its
// key, for the focused session's project. c.mu held.
func (c *client) sessionCommand(k string) (mod.ModCommand, bool) {
	for _, mc := range c.cmds {
		if commandKeyOf(mc, c.prefix.String()) == k {
			return mc, true
		}
	}
	return mod.ModCommand{}, false
}

// commandItems are the ≡ menu's entries running the mods' commands for
// the focused session's project, each with its mod's id. c.mu held.
func (c *client) commandItems() []aitem {
	if c.focus == nil {
		return nil
	}
	var out []aitem
	for _, mc := range c.cmds {
		if !mc.InMenu() || !mc.Runs(c.focus.info.Project) {
			continue
		}
		key := ""
		if k := commandKeyOf(mc, c.prefix.String()); k != "" {
			key = "prefix+" + k
		}
		out = append(out, aitem{label: mc.Title + " · " + mc.Mod, key: key, run: func() { c.runCommand(mc) }})
	}
	return out
}

// runCommand sends mc to its mod, in the focused session's project with
// that session selected, and watches for the view the mod opens.
func (c *client) runCommand(mc mod.ModCommand) {
	if !c.lock() {
		return
	}
	var info proto.SessionInfo
	if c.focus != nil {
		info = c.focus.info
	}
	slug := info.Project
	run := mc.Runs(slug)
	if run {
		c.flash = "ran " + mc.Title
	} else {
		c.flash = mc.Title + " (" + mc.Mod + ") doesn't run for " + slug
	}
	c.status()
	send := c.sendCommand
	c.mu.Unlock()
	c.poke()
	if !run {
		return
	}
	p := proto.ModCommandParams{Mod: mc.Mod, Command: mc.ID, Project: slug, Selection: sessionTarget(info, slug), Console: true}
	go func() {
		if send == nil {
			send = sendCommand(c.paths)
		}
		res, err := send(p)
		if !c.lock() {
			return
		}
		if err != nil {
			var perr *proto.Error
			c.flash = err.Error()
			if errors.As(err, &perr) {
				c.flash = perr.Message
			}
		} else {
			c.wait = &cmdWait{mod: mc.Mod, after: res.After, until: time.Now().Add(modview.InputWindow + 2*statePoll)}
		}
		c.status()
		c.mu.Unlock()
		c.poke()
	}()
}

// sendCommand sends mod.command to the server at paths.
func sendCommand(paths server.Paths) func(proto.ModCommandParams) (proto.ModCommandResult, error) {
	return func(p proto.ModCommandParams) (proto.ModCommandResult, error) {
		var res proto.ModCommandResult
		ctl, err := server.Connect(paths, false)
		if err != nil {
			return res, err
		}
		defer ctl.Close()
		err = ctl.Call(proto.MethodModCommand, p, &res)
		return res, err
	}
}

// keyCommand runs the mod's command on key k, typed after the prefix
// over a popup on this session (Options.Command), once the commands
// are read.
func (c *client) keyCommand(k string) {
	cmds := loadCommands(nil)
	if !c.lock() {
		return
	}
	if c.cmds == nil {
		c.cmds = cmds
	}
	mc, ok := c.sessionCommand(k)
	c.mu.Unlock()
	if ok {
		c.runCommand(mc)
	}
}

// waiting is the command whose view open this session watches for, nil
// for none (or none any more).
func (c *client) waiting() *cmdWait {
	if !c.lock() {
		return nil
	}
	defer c.mu.Unlock()
	if c.wait != nil && time.Now().After(c.wait.until) {
		c.wait = nil
	}
	return c.wait
}

// takeOpen acts on the view w's mod asked to open, if res has it: a
// full view or a project tab opens over the session, a card on the
// dashboard.
func (c *client) takeOpen(w *cmdWait, res *proto.ModViewsResult) {
	if res == nil {
		return
	}
	for _, o := range res.Open {
		if o.Mod != w.mod || o.Seq <= w.after {
			continue
		}
		slot := ""
		for _, v := range res.Views {
			if v.Mod == o.Mod && v.View == o.View {
				slot = v.Slot
			}
		}
		if !c.lock() {
			return
		}
		if c.wait != w {
			c.mu.Unlock()
			return
		}
		c.wait = nil
		c.mu.Unlock()
		switch {
		case !c.dashboard:
			// A bare view has no dashboard to show it on.
		case slot == mod.SlotFull, slot == mod.SlotProjectTab:
			c.viewOver(o)
		case slot == mod.SlotDashboardCard:
			c.detachThen("")
		}
		return
	}
}

// viewOver leaves the attach for the dashboard over this pane, which
// shows the view open o asks for (Over.View) and attaches again once it
// closes.
func (c *client) viewOver(o modview.Open) {
	if c.lock() {
		c.goOver(&Over{Project: o.Project, View: true, After: o.Seq - 1})
	}
}
