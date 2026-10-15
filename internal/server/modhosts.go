package server

// The user mods' hosts (docs/SPEC.md §3.7, Supervisor): the server runs
// every enabled code mod in a `tm mod host` child under the mod
// supervisor (package modsup), and follows config.toml and
// ~/.terminatr/mods as they change: a mod enabled, disabled, regranted,
// limited to other projects, reset or updated takes effect without a
// server restart. In safe mode (tm --no-mods, mod.EnvNoMods) no user
// mod runs; bundled ones still do.

import (
	"cmp"
	"context"
	"path/filepath"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modsup"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/version"
)

// envModsPoll shortens the look at config.toml and the installed mods
// (tests).
const envModsPoll = "TERMINATR_MODS_POLL"

const defaultModsPoll = 3 * time.Second

// newModHosts makes the supervisor; nil when the home folders can't be
// named (no mods then).
func (s *Server) newModHosts() *modsup.Supervisor {
	if s.noMods {
		s.log.Printf("mods: safe mode (%s): no user mod runs", mod.EnvNoMods)
	}
	cache, err1 := home.ModCacheDir()
	state, err2 := home.ModStateDir()
	logs, err3 := home.ModLogsDir()
	if err := cmp.Or(err1, err2, err3); err != nil {
		s.log.Printf("mods: %v; no code mods run", err)
		return nil
	}
	return modsup.New(modsup.Options{
		Bin: s.opts.Bin, CacheDir: cache, StateDir: state, LogDir: logs,
		Subscribe: s.SubscribeMod, Broker: s.ModRequest, Logf: s.log.Printf,
		TMVersion: version.Version, API: mod.APIVersion(),
	})
}

// runModHosts gives the supervisor the enabled code mods now and after
// every change, until ctx ends; it then stops their hosts. Each poll
// only stats the mods' files (s.installedMods): the specs are worked out again
// when the registry was read anew. A new registry (config.toml or a
// mod changed) also rewrites the agents' live themes, which follow
// [ui] theme (§8.6): a pick the console's colours don't show, such as
// the built-in theme after a mod's without a pane palette, still
// reaches the running Claude panes.
func (s *Server) runModHosts(ctx context.Context, sup *modsup.Supervisor) {
	defer sup.Close()
	poll := cmp.Or(envDuration(envModsPoll), defaultModsPoll)
	t := time.NewTicker(poll)
	defer t.Stop()
	var applied *mod.Registry
	for {
		if reg, ok, _ := s.installedMods(0); ok && reg != nil && reg != applied {
			sup.Set(s.specsOf(reg))
			applied = reg
			s.mu.Lock()
			s.themeChangedLocked()
			s.mu.Unlock()
		}
		s.expireModViews()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// modSpecs are the code mods that run: installed, valid, compatible and
// enabled, and bundled in safe mode. A config.toml that doesn't load
// changes nothing (ok false).
func (s *Server) modSpecs() ([]modsup.Spec, bool) {
	// The broker answers the hosts started from reg by reg (one reader,
	// s.installedMods), not by a registry it read before the mod was installed or
	// enabled: that one would refuse every call of a new host's first
	// second (T301 B7).
	reg, ok, _ := s.installedMods(0)
	if !ok || reg == nil {
		return nil, false
	}
	return s.specsOf(reg), true
}

// specsOf is modSpecs from registry reg.
func (s *Server) specsOf(reg *mod.Registry) []modsup.Spec {
	s.noteMods(reg)
	var specs []modsup.Spec
	for _, m := range reg.Mods {
		if m.State != mod.Enabled || m.Manifest == nil || !m.Manifest.IsCode() || s.noMods && !m.Builtin {
			continue
		}
		// A mod that draws columns runs from the server's start: a
		// lazy host never says mod.ready, so its cached cells would stay
		// stale after a restart until an event arrived. The others (the
		// Memory tab) wait, lazily, for their first event.
		sp := modsup.Spec{ID: m.ID, Grants: m.Grants, Projects: m.Projects, Settings: m.Settings,
			Eager: len(m.Manifest.Provides.Columns) > 0}
		if m.Builtin {
			// Precompiled into tm (go generate ./internal/mod): it
			// runs without esbuild.
			b, err := builtinBundle(m)
			if err != nil {
				s.log.Printf("mods: built-in %s: %v", m.ID, err)
				continue
			}
			sp.Bundle = b
		} else {
			sp.Entry = filepath.Join(m.Dir, filepath.FromSlash(m.Manifest.Provides.Main))
		}
		specs = append(specs, sp)
	}
	return specs
}

// builtinBundles are the built-in code mods' bundles, by mod id, read
// once: they are part of tm.
var builtinBundles sync.Map

// builtinBundle is built-in mod m's precompiled bundle.
func builtinBundle(m mod.Mod) ([]byte, error) {
	if b, ok := builtinBundles.Load(m.ID); ok {
		return b.([]byte), nil
	}
	b, err := m.ReadFile(m.Manifest.Provides.Main)
	if err != nil {
		return nil, err
	}
	builtinBundles.Store(m.ID, b)
	return b, nil
}

// ModHosts is the supervised code mods' state (the Mods tab, tm mod
// list, tm doctor); nil before the server runs them.
func (s *Server) ModHosts() []modsup.Status {
	if sup := s.modSup.Load(); sup != nil {
		return sup.Status()
	}
	return nil
}

// modHosts is mod.hosts: ModHosts, and safe mode.
func (s *Server) modHosts() proto.ModHostsResult {
	res := proto.ModHostsResult{NoMods: s.noMods, Hosts: []proto.ModHost{}}
	for _, st := range s.ModHosts() {
		res.Hosts = append(res.Hosts, proto.ModHost{ID: st.ID, State: string(st.State), Since: st.Since,
			PID: st.PID, RSS: st.RSS, Restarts: st.Restarts, Next: st.Next, Err: st.Err, Sandbox: st.Sandbox})
	}
	return res
}

// restartMod is mod.restart: the human starts mod id's host again, with
// the mods as they are now, so a mod just enabled runs (tm mod dev); an
// agent is refused, and so is a user mod in safe mode (a built-in one
// runs there).
func (s *Server) restartMod(id string, c caller.Caller) (any, *proto.Error) {
	if c.IsAgent() {
		return nil, proto.Errorf("human-only", "the user restarts mods")
	}
	if err := mod.CheckID(id); err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	if s.noMods && !mod.IsBundled(id) {
		return nil, proto.Errorf(proto.ErrRefused, "the server runs in safe mode (%s): no user mod runs; restart it without", mod.EnvNoMods)
	}
	sup := s.modSup.Load()
	if sup != nil {
		if specs, ok := s.modSpecs(); ok {
			sup.Set(specs)
		}
	}
	if sup == nil || !sup.Restart(id) {
		return nil, proto.Errorf(proto.ErrBadParams, "mod %q doesn't run: only an enabled code mod has a host", id)
	}
	s.log.Printf("mod.restart %s", id)
	return s.modHosts(), nil
}
