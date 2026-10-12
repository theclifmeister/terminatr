package server

// The views user mods draw (docs/SPEC.md §7.5, Views of user mods; mods
// design §6): a code mod with the views grant publishes the widget tree
// of each view its manifest declares ($.ui.view.set), and consoles poll
// them (mod.views), saying which they show: the server turns that into
// view.opened and view.closed for the mod. A human's key or click in a
// view reaches the mod as ui.key or ui.click (mod.view.input), and only
// then may the mod ask for one of its views to open ($.ui.view.open);
// where a view's cursor went reaches it as ui.cursor.
// A view whose host failed shows that, never its stale trees, and a
// restarted host starts with no trees and gets view.opened again for
// the views on show.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modaction"
	"github.com/theclifmeister/terminatr/internal/modbroker"
	"github.com/theclifmeister/terminatr/internal/modbus"
	"github.com/theclifmeister/terminatr/internal/modsup"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// modViewStore is the server's mod views, made on first use (a test's
// bare Server too).
func (s *Server) modViewStore() *modview.Store {
	s.modViewsMu.Lock()
	defer s.modViewsMu.Unlock()
	if s.modViewSt == nil {
		s.modViewSt = modview.New()
	}
	return s.modViewSt
}

// declaredView is view id of mod m as its manifest declares it.
func declaredView(m mod.Mod, id string) (mod.View, bool) {
	if m.Manifest == nil {
		return mod.View{}, false
	}
	for _, v := range m.Manifest.Provides.Views {
		if v.ID == id {
			return v, true
		}
	}
	return mod.View{}, false
}

// ui.view.set {view, widget, options: {project, count, height}}: the
// tree of one of the mod's views, for a project it runs for or for all;
// a null widget clears it.
func (s *Server) modViewSet(_ context.Context, c *modbroker.Call) (any, error) {
	var a struct {
		View    string          `json:"view"`
		Widget  json.RawMessage `json:"widget"`
		Options *struct {
			Project string `json:"project"`
			Task    string `json:"task"`
			Thread  string `json:"thread"`
			Count   *int   `json:"count"`
			Height  int    `json:"height"`
		} `json:"options"`
	}
	if err := c.Decode(&a); err != nil {
		return nil, err
	}
	v, ok := declaredView(c.Mod, a.View)
	if !ok {
		return nil, modbroker.BadParams("ui.view.set: %s declares no view %q in [provides].views", c.Mod.ID, a.View)
	}
	t := modview.Tree{}
	if o := a.Options; o != nil {
		if o.Project != "" {
			if err := c.Project(o.Project); err != nil {
				return nil, err
			}
		}
		if err := checkSectionTarget(v.Slot, o.Project, o.Task, o.Thread); err != nil {
			return nil, modbroker.BadParams("ui.view.set: %v", err)
		}
		t.Project, t.Task, t.Thread, t.Count, t.Height = o.Project, o.Task, o.Thread, o.Count, o.Height
	}
	if len(a.Widget) > 0 && string(a.Widget) != "null" {
		w, err := widget.Decode(a.Widget)
		if err != nil {
			return nil, modbroker.BadParams("ui.view.set: %v", err)
		}
		for _, b := range widget.BoundActions(w) {
			if spec, ok := modaction.Lookup(b.ID); ok && !c.Mod.Binds(spec) {
				return nil, modbroker.Denied("ui.view.set: binding %s needs it in [permissions] actions, with the %s grant, which %s doesn't have", b.ID, mod.PermActions, c.Mod.ID)
			}
		}
		t.Tree = w
	}
	if err := s.modViewStore().Set(c.Mod.ID, a.View, t, len(a.Widget)); err != nil {
		var full modview.ErrFull
		if errors.As(err, &full) {
			return nil, modbroker.Denied("ui.view.set: %v", err)
		}
		return nil, err
	}
	return nil, nil
}

// checkSectionTarget says why a view of slot can't be for task or thread thr
// of project: only a details.section is for one of them, one at a time,
// of a project named, by its ref (T12, t-0003).
func checkSectionTarget(slot, project, task, thr string) error {
	switch {
	case task == "" && thr == "":
		return nil
	case slot != mod.SlotDetailsSection:
		return fmt.Errorf("only a %s is for a task or a thread", mod.SlotDetailsSection)
	case project == "":
		return errors.New("a task or a thread needs its project")
	case task != "" && thr != "":
		return errors.New("a task or a thread, not both")
	case task != "":
		if n, ok := tasks.ParseRef(task); !ok || task != fmt.Sprintf("T%d", n) {
			return fmt.Errorf("task %q isn't a task's ref (T12)", task)
		}
	case !thread.ValidID(thr):
		return fmt.Errorf("thread %q isn't a thread's id (t-0003)", thr)
	}
	return nil
}

// checkShownTarget checks what a shown view is for: a details.section's
// task or thread, as a tree's (checkSectionTarget), or the task a
// project tab was opened to show (mod API 1.3), which its mod hears in
// view.opened.
func checkShownTarget(slot, project, task, thr string) error {
	if slot == mod.SlotProjectTab && task != "" && thr == "" {
		return checkSectionTarget(mod.SlotDetailsSection, project, task, "")
	}
	return checkSectionTarget(slot, project, task, thr)
}

// ui.view.open {view}: show one of the mod's views on the console of the
// human input its handler runs for, once; refused otherwise.
func (s *Server) modViewOpen(_ context.Context, c *modbroker.Call) (any, error) {
	var a struct {
		View string `json:"view"`
	}
	if err := c.Decode(&a); err != nil {
		return nil, err
	}
	v, ok := declaredView(c.Mod, a.View)
	if !ok {
		return nil, modbroker.BadParams("ui.view.open: %s declares no view %q in [provides].views", c.Mod.ID, a.View)
	}
	if mod.IsSection(v.Slot) {
		return nil, modbroker.BadParams("ui.view.open: %s is a panel section: it shows with its panel, never opened", a.View)
	}
	if !s.modViewStore().Open(c.Mod.ID, a.View) {
		return nil, modbroker.Denied("ui.view.open works only in a handler for a human input (a key or a click in one of %s's views), once", c.Mod.ID)
	}
	c.Audit("ui.view.open %s", a.View)
	return nil, nil
}

// viewMods are the mods whose views consoles may show: enabled code
// mods with the views grant that declare views.
func (s *Server) viewMods() []mod.Mod {
	r := s.modRegistry()
	if r == nil {
		return nil
	}
	var out []mod.Mod
	for _, m := range r.Mods {
		if m.State == mod.Enabled && m.Manifest != nil && m.Manifest.IsCode() && m.Granted(mod.PermViews) && len(m.Manifest.Provides.Views) > 0 {
			out = append(out, m)
		}
	}
	return out
}

// modViews is mod.views for console pid: what it shows now goes to the
// mods as view.opened and view.closed; the answer is every view it may
// show and the mods' open requests for it.
func (s *Server) modViews(p proto.ModViewsParams, pid int, c caller.Caller) (any, *proto.Error) {
	if c.IsAgent() {
		return nil, proto.Errorf("human-only", "only a console shows mod views")
	}
	mods := s.viewMods()
	hosts := map[string]modsup.Status{}
	for _, st := range s.ModHosts() {
		hosts[st.ID] = st
	}
	logs, _ := home.ModLogsDir()
	store := s.modViewStore()
	res := proto.ModViewsResult{Views: []proto.ModView{}}
	for _, m := range mods {
		mv := proto.ModView{Mod: m.ID, Name: mod.Clean(m.Manifest.Name, false), Projects: m.Projects}
		st, ok := hosts[m.ID]
		failed := ok && (st.State == modsup.Failed || st.State == modsup.Backoff)
		for _, v := range m.Manifest.Provides.Views {
			mv.View, mv.Title, mv.Slot = v.ID, mod.Clean(v.Title, false), v.Slot
			mv.State, mv.Err, mv.Log, mv.Trees = "", "", "", nil
			if failed {
				mv.State, mv.Err = proto.ModViewFailed, oneLine(st.Err, 200)
				if logs != "" {
					mv.Log = filepath.Join(logs, m.ID+".log")
				}
			} else {
				mv.Trees = store.Trees(m.ID, v.ID)
			}
			res.Views = append(res.Views, mv)
		}
	}
	// Only views the mods have, for projects they run for.
	shown := slices.DeleteFunc(slices.Clone(p.Shown), func(v modview.Shown) bool {
		i := slices.IndexFunc(mods, func(m mod.Mod) bool { return m.ID == v.Mod })
		if i < 0 {
			return true
		}
		d, ok := declaredView(mods[i], v.View)
		return !ok || d.Slot != v.Slot || v.Project != "" && !mods[i].Runs(v.Project) ||
			checkShownTarget(v.Slot, v.Project, v.Task, v.Thread) != nil
	})
	opened, closed := store.Show(pid, shown)
	s.publishViews(opened, closed)
	res.Open = store.Opens(pid)
	return res, nil
}

// publishViews tells each mod which of its views opened and closed.
func (s *Server) publishViews(opened, closed []modview.Shown) {
	if s.modEv == nil {
		return
	}
	pub := func(name string, list []modview.Shown) {
		for _, v := range list {
			s.modEv.bus.PublishTo(v.Mod, name, v.Project, modbus.View{View: v.View, Slot: v.Slot, Project: v.Project, Task: v.Task, Thread: v.Thread, Width: v.Width, Height: v.Height})
		}
	}
	pub(modbus.ViewClosed, closed)
	pub(modbus.ViewOpened, opened)
}

// expireModViews closes the views of consoles that stopped polling.
func (s *Server) expireModViews() {
	s.publishViews(nil, s.modViewStore().Expire())
}

// modViewsRestarted starts mod id's views afresh for its new host: no
// trees, and view.opened again for each view a console shows.
func (s *Server) modViewsRestarted(id string) {
	store := s.modViewStore()
	store.Clear(id)
	s.publishViews(store.ShownBy(id), nil)
}

// modViewInput is mod.view.input: a human's declared key or click in a
// mod's view, to the mod as ui.key or ui.click, or where its cursor
// went, as ui.cursor. A key or a click lets the mod open a
// view on this console for a while (modview.InputWindow).
func (s *Server) modViewInput(p proto.ModViewInputParams, pid int, c caller.Caller) (any, *proto.Error) {
	if c.IsAgent() {
		return nil, proto.Errorf("human-only", "only the human types in mod views")
	}
	m, ok := s.modNamed(p.Mod)
	if !ok || !slices.ContainsFunc(s.viewMods(), func(o mod.Mod) bool { return o.ID == p.Mod }) {
		return nil, proto.Errorf(proto.ErrBadParams, "no mod %q draws views", p.Mod)
	}
	if _, ok := declaredView(m, p.View); !ok {
		return nil, proto.Errorf(proto.ErrBadParams, "%s has no view %q", p.Mod, p.View)
	}
	if p.Project != "" && !m.Runs(p.Project) {
		return nil, proto.Errorf(proto.ErrBadParams, "%s doesn't run for project %s", p.Mod, p.Project)
	}
	name := modbus.UIClick
	switch p.Kind {
	case widget.EventClick:
	case widget.EventCursor:
		name = modbus.UICursor
	case widget.EventKey:
		name = modbus.UIKey
		if p.Key == "" || len(p.Key) > 16 {
			return nil, proto.Errorf(proto.ErrBadParams, "no key")
		}
	default:
		return nil, proto.Errorf(proto.ErrBadParams, "kind %q is not key, click or cursor", p.Kind)
	}
	if len(p.Widget) > 64 || len(p.Row) > 256 {
		return nil, proto.Errorf(proto.ErrBadParams, "widget or row id too long")
	}
	in := modbus.Input{View: p.View, Widget: p.Widget, Row: p.Row}
	if name == modbus.UIKey {
		in.Key = p.Key
	}
	if p.Project != "" {
		in.Target = &modbus.Target{Project: p.Project}
	}
	// The cursor moves with a publish too: only a key or a click lets
	// the mod open a view.
	if name != modbus.UICursor {
		s.modViewStore().Input(p.Mod, pid, p.Project)
	}
	if s.modEv != nil {
		s.modEv.bus.PublishTo(p.Mod, name, p.Project, in)
	}
	return struct{}{}, nil
}
