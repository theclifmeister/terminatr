package server

// The mods' cells (docs/SPEC.md §5.3, Cells; package modcell): what code
// mods publish into their columns ($.ui.cell.set) and the status bar
// ($.ui.status.set), kept by the server and sent with the rows consoles
// already ask for: session.list's Mods and each session's Cells, and a
// project's ProjectWatch. The store is written to the mods' cache, so the
// last cells show again after a restart, before the hosts publish anew.

import (
	"context"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modbroker"
	"github.com/theclifmeister/terminatr/internal/modcell"
	"github.com/theclifmeister/terminatr/internal/modsup"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// cellsFile is the store's file in the mods' cache (home.ModCacheDir).
const cellsFile = "cells.json"

// modCells are the store and the mods whose cells show.
type modCells struct {
	once  sync.Once
	store *modcell.Store
	// shown are the enabled code mods, by id, as the registry last had
	// them (noteMods); nil before it was first read.
	shown atomic.Pointer[map[string]shownMod]
}

// shownMod is an enabled code mod: its name and columns.
type shownMod struct {
	name string
	cols []mod.Column
}

// cells is the store, opened on first use.
func (s *Server) cells() *modcell.Store {
	s.modCl.once.Do(func() {
		path := ""
		if dir, err := home.ModCacheDir(); err == nil {
			path = filepath.Join(dir, cellsFile)
		}
		s.modCl.store = modcell.Open(path)
	})
	return s.modCl.store
}

// noteMods takes the registry as runModHosts read it: the enabled code
// mods' cells show, the cells of every other mod, and of columns their
// manifests no longer declare, are dropped.
func (s *Server) noteMods(reg *mod.Registry) {
	shown := map[string]shownMod{}
	for _, m := range reg.Mods {
		if m.State == mod.Enabled && m.Manifest != nil && m.Manifest.IsCode() {
			shown[m.ID] = shownMod{name: m.Manifest.Name, cols: m.Manifest.Provides.Columns}
		}
	}
	s.modCl.shown.Store(&shown)
	s.cells().Keep(func(id string) bool { _, ok := shown[id]; return ok },
		func(id, col string) bool { return declares(shown[id].cols, col) })
}

func declares(cols []mod.Column, id string) bool {
	return slices.ContainsFunc(cols, func(c mod.Column) bool { return c.ID == id })
}

// cellShows is whether mod id's cells show now: it is an enabled code mod
// (before the registry was first read, every cached mod's do) and its
// host hasn't failed, whose cells would be stale.
func (s *Server) cellShows() func(id string) bool {
	shown := s.modCl.shown.Load()
	failed := map[string]bool{}
	for _, st := range s.ModHosts() {
		if st.State == modsup.Failed {
			failed[st.ID] = true
		}
	}
	return func(id string) bool {
		if failed[id] {
			return false
		}
		if shown == nil {
			return true
		}
		_, ok := (*shown)[id]
		return ok
	}
}

// modCellsView is session.list's Mods: the shown mods' columns, the cells
// of projects, tasks and threads, and the status segments; nil when there
// is none of them.
func (s *Server) modCellsView() *proto.ModCells {
	show := s.cellShows()
	out := &proto.ModCells{
		Cells:  s.cells().Cells(show, func(t proto.CellTarget) bool { return t.Session == "" }),
		Status: s.cells().Status(show),
	}
	if shown := s.modCl.shown.Load(); shown != nil {
		ids := make([]string, 0, len(*shown))
		for id := range *shown {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			if !show(id) {
				continue
			}
			for _, c := range (*shown)[id].cols {
				out.Columns = append(out.Columns, proto.ModColumn{ID: id + "." + c.ID, Title: c.Title, Width: c.Width, Where: c.Where})
			}
		}
		for i := range out.Status {
			out.Status[i].Name = (*shown)[out.Status[i].Mod].name
		}
	}
	if len(out.Columns) == 0 && len(out.Cells) == 0 && len(out.Status) == 0 {
		return nil
	}
	return out
}

// sessionCells are the shown cells for session id.
func (s *Server) sessionCells(id string) []proto.ModCell {
	return s.cells().Cells(s.cellShows(), func(t proto.CellTarget) bool { return t.Session == id })
}

// projectCells are the shown cells for project slug, its tasks and its
// threads.
func (s *Server) projectCells(slug string) []proto.ModCell {
	return s.cells().Cells(s.cellShows(), func(t proto.CellTarget) bool { return t.Project == slug })
}

// ui.cell.set {column, target, spans}: the mod's cell in one of its own
// columns; null or [] spans clear it. A project's target needs the mod
// to run for it; a session's, for the session's project.
func (s *Server) modCellSet(_ context.Context, c *modbroker.Call) (any, error) {
	var a struct {
		Column string           `json:"column"`
		Target proto.CellTarget `json:"target"`
		Spans  []widget.Span    `json:"spans"`
	}
	if err := c.Decode(&a); err != nil {
		return nil, err
	}
	if c.Mod.Manifest == nil || !declares(c.Mod.Manifest.Provides.Columns, a.Column) {
		return nil, modbroker.BadParams("ui.cell.set: %q is not one of %s's columns", a.Column, c.Mod.ID)
	}
	if err := modcell.CheckTarget(a.Target); err != nil {
		return nil, modbroker.BadParams("ui.cell.set: %v", err)
	}
	slug := a.Target.Project
	if a.Target.Session != "" {
		sess, perr := s.session(a.Target.Session)
		if perr != nil {
			return nil, modbroker.NotFound("ui.cell.set: no session %s", a.Target.Session)
		}
		slug = sess.Info().Project
	}
	if slug != "" {
		if err := c.Project(slug); err != nil {
			return nil, err
		}
	} else if len(c.Mod.Projects) > 0 {
		return nil, modbroker.Denied("%s runs for some projects only, and session %s has none", c.Mod.ID, a.Target.Session)
	}
	if err := modcell.Check(a.Spans); err != nil {
		return nil, modbroker.BadParams("ui.cell.set: %v", err)
	}
	if _, err := s.cells().Set(c.Mod.ID, a.Column, a.Target, a.Spans); err != nil {
		return nil, modbroker.Denied("ui.cell.set: %v", err)
	}
	return nil, nil
}

// ui.status.set {spans}: the mod's status-bar segment; null or [] clear
// it.
func (s *Server) modStatusSet(_ context.Context, c *modbroker.Call) (any, error) {
	var a struct {
		Spans []widget.Span `json:"spans"`
	}
	if err := c.Decode(&a); err != nil {
		return nil, err
	}
	if err := modcell.Check(a.Spans); err != nil {
		return nil, modbroker.BadParams("ui.status.set: %v", err)
	}
	s.cells().SetStatus(c.Mod.ID, a.Spans)
	return nil, nil
}
