package tui

import (
	"io"
	"log"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// sessionCmds are commandDash's commands, and one on a (a prefix
// command's key, which the core keeps).
func sessionCmds() []mod.ModCommand {
	no := false
	return []mod.ModCommand{
		{Mod: "lab", Name: "Labeller", Command: mod.Command{ID: "relabel", Title: "Relabel", Key: "g"}},
		{Mod: "lab", Name: "Labeller", Command: mod.Command{ID: "sweep", Title: "Sweep", Key: "Z"}},
		{Mod: "lab", Name: "Labeller", Command: mod.Command{ID: "quiet", Title: "Quiet", Menu: &no}},
		{Mod: "lab", Name: "Labeller", Command: mod.Command{ID: "pop", Title: "Pop", Key: "a"}},
		{Mod: "only", Name: "Only", Projects: []string{"beta"}, Command: mod.Command{ID: "go", Title: "Go beta", Key: "G"}},
	}
}

// commandClient is a session's client focused on alpha's thread t-0001
// (task T3), with sessionCmds and the commands it sends on sent.
func commandClient() (*client, chan proto.ModCommandParams) {
	sent := make(chan proto.ModCommandParams, 4)
	c := &client{dashboard: true, prefix: chord{'b'}, cmds: sessionCmds(), end: make(chan struct{}), log: log.New(io.Discard, "", 0),
		focus: &pane{info: proto.SessionInfo{ID: "s-2", Role: proto.RoleThread, Project: "alpha", Thread: "t-0001", Task: "T3"}}}
	c.sendCommand = func(p proto.ModCommandParams) (proto.ModCommandResult, error) {
		sent <- p
		return proto.ModCommandResult{After: 4}, nil
	}
	return c, sent
}

// TestSessionCommandKeys: in a session, prefix then a command's key runs
// it in the session's project, the session selected; the plain key goes
// to the program; the core's keys (j, a) stay the core's, and a mod
// that doesn't run for the project isn't sent.
func TestSessionCommandKeys(t *testing.T) {
	c, sent := commandClient()
	if do := prefixStep(c.prefix, false, cmdKey("g"), true); !do.input {
		t.Fatalf("plain g: %+v, not the program's", do)
	}
	press := func(k string) {
		c.key(uv.Key{Code: 'b', Mod: uv.ModCtrl})
		c.key(cmdKey(k))
	}
	press("g")
	select {
	case p := <-sent:
		want := proto.ModTarget{Project: "alpha", Session: "s-2", Thread: "t-0001", Task: "T3"}
		if p.Mod != "lab" || p.Command != "relabel" || p.Project != "alpha" || !p.Console || p.Selection == nil || *p.Selection != want {
			t.Fatalf("sent %+v %+v", p, p.Selection)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("prefix+g sent nothing")
	}
	if !strings.Contains(c.flash, "ran Relabel") {
		t.Errorf("flash %q", c.flash)
	}
	// The answer's After is what the session watches the opens from.
	deadline := time.Now().Add(5 * time.Second)
	for c.waiting() == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if w := c.waiting(); w == nil || w.mod != "lab" || w.after != 4 {
		t.Fatalf("wait %+v", w)
	}
	for _, k := range []string{"Z", "G"} {
		press(k)
	}
	if !strings.Contains(c.flash, "Go beta (only) doesn't run for alpha") {
		t.Errorf("flash %q", c.flash)
	}
	select {
	case p := <-sent:
		t.Fatalf("sent %+v", p)
	case <-time.After(50 * time.Millisecond):
	}
	for _, k := range []string{"a", "A", "n", "Z", "{", ";", "d", "?"} {
		if commandKeyOf(mod.ModCommand{Command: mod.Command{Key: k}}, c.prefix.String()) != "" {
			t.Errorf("a command kept the core's %s", k)
		}
	}
}

// TestSessionCommandMenu: the session's ≡ menu lists, after the prefix
// commands, the commands with a menu entry that run for its project,
// each with its prefix key (none when the core has it).
func TestSessionCommandMenu(t *testing.T) {
	c, sent := commandClient()
	var got []string
	var run func()
	for _, it := range c.sessionItems() {
		if strings.HasSuffix(it.label, " · lab") {
			got = append(got, it.label+"="+it.key)
		}
		if it.label == "Sweep · lab" {
			run = it.run
		}
	}
	if g, want := strings.Join(got, " | "), "Relabel · lab=prefix+g | Sweep · lab= | Pop · lab="; g != want {
		t.Fatalf("menu\n got %s\nwant %s", g, want)
	}
	run()
	if p := <-sent; p.Command != "sweep" {
		t.Fatalf("sent %+v", p)
	}
}

// TestSessionCommandOpen: a full view the command's mod opens leaves the
// session for the dashboard over it, showing that view; an older open,
// or another mod's, doesn't.
func TestSessionCommandOpen(t *testing.T) {
	c, _ := commandClient()
	w := &cmdWait{mod: "lab", after: 4, until: time.Now().Add(time.Minute)}
	c.wait = w
	views := []proto.ModView{{Mod: "lab", View: "out", Slot: mod.SlotFull}, {Mod: "x", View: "y", Slot: mod.SlotFull}}
	c.takeOpen(w, &proto.ModViewsResult{Views: views, Open: []modview.Open{{Seq: 4, Mod: "lab", View: "out"}, {Seq: 5, Mod: "x", View: "y"}}})
	if c.over != nil || c.wait == nil {
		t.Fatalf("an old or other mod's open: over %+v", c.over)
	}
	c.takeOpen(w, &proto.ModViewsResult{Views: views, Open: []modview.Open{{Seq: 6, Mod: "lab", View: "out", Project: "alpha"}}})
	o := c.result.Over
	if o == nil || !o.View || o.After != 5 || o.Project != "alpha" || o.Session != "s-2" || c.wait != nil {
		t.Fatalf("over %+v, wait %+v", o, c.wait)
	}
}

// TestDashOverView: over a session for a mod's view, the dashboard
// waits for the view instead of attaching again at once, and opens it.
func TestDashOverView(t *testing.T) {
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 100, Height: 30,
		Over: &Over{Project: "alpha", Session: "s-1", Screen: []string{"", "row"}, View: true, After: 5}})
	m.setData(src.data)
	if m.overDone() {
		t.Fatal("back to the session before the view showed")
	}
	m.setModViews(modViewsMsg{res: proto.ModViewsResult{
		Views: []proto.ModView{{Mod: "lab", Name: "Lab", View: "out", Title: "Out", Slot: mod.SlotFull}},
		Open:  []modview.Open{{Seq: 5, Mod: "lab", View: "old"}, {Seq: 6, Mod: "lab", View: "out", Project: "alpha"}}}})
	if fv, ok := m.top().(*fullView); !ok || fv.view != "out" {
		t.Fatalf("top %T", m.top())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.result.Attach != "s-1" {
		t.Fatalf("esc: %+v", m.result)
	}
}

// TestDashPrefixCommand: prefix then a command's key runs it on the
// dashboard as the key does; over a session it goes back to the session,
// which runs it.
func TestDashPrefixCommand(t *testing.T) {
	src, m := commandDash(t)
	m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	drain(t, m, keyPress(m, "g"))
	if len(src.commands) != 1 || src.commands[0].Command != "relabel" {
		t.Fatalf("prefix+g: %+v", src.commands)
	}
	src.data.ModCommands = sessionCmds()
	m = newDash(DashOptions{Source: src, Width: 100, Height: 30,
		Over: &Over{Key: "A", Project: "alpha", Session: "s-1", Screen: []string{"", "row"}}})
	m.setData(src.data)
	m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	if !quits(keyPress(m, "g")) || m.result.Command != "g" || m.result.Attach != "s-1" || len(src.commands) != 1 {
		t.Fatalf("prefix+g over a session: %+v, sent %d", m.result, len(src.commands))
	}
}
