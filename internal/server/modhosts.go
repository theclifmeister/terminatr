package server

// The user mods' hosts (docs/SPEC.md §3.7, Supervisor): the server runs
// every enabled code mod in a `tm mod host` child under the mod
// supervisor (package modsup), and follows config.toml and
// ~/.terminatr/mods as they change: a mod enabled, disabled, regranted,
// limited to other projects, reset or updated takes effect without a
// server restart.

import (
	"cmp"
	"context"
	"path/filepath"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modsup"
	"github.com/theclifmeister/terminatr/internal/version"
)

// envModsPoll shortens the look at config.toml and the installed mods
// (tests).
const envModsPoll = "TERMINATR_MODS_POLL"

const defaultModsPoll = 3 * time.Second

// newModHosts makes the supervisor; nil when the home folders can't be
// named (no mods then).
func (s *Server) newModHosts() *modsup.Supervisor {
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
// every change, until ctx ends; it then stops their hosts.
func (s *Server) runModHosts(ctx context.Context, sup *modsup.Supervisor) {
	defer sup.Close()
	poll := cmp.Or(envDuration(envModsPoll), defaultModsPoll)
	t := time.NewTicker(poll)
	defer t.Stop()
	for {
		if specs, ok := s.modSpecs(); ok {
			sup.Set(specs)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// modSpecs are the code mods that run: installed, valid, compatible and
// enabled. A config.toml that doesn't load changes nothing (ok false).
func (s *Server) modSpecs() ([]modsup.Spec, bool) {
	cfg, err := config.Load()
	if err != nil {
		return nil, false
	}
	reg, err := mod.Load(cfg)
	if err != nil {
		return nil, false
	}
	var specs []modsup.Spec
	for _, m := range reg.Mods {
		if m.State != mod.Enabled || m.Manifest == nil || !m.Manifest.IsCode() {
			continue
		}
		specs = append(specs, modsup.Spec{ID: m.ID, Entry: filepath.Join(m.Dir, filepath.FromSlash(m.Manifest.Provides.Main)),
			Grants: m.Grants, Projects: m.Projects, Settings: m.Settings})
	}
	return specs, true
}

// ModHosts is the supervised code mods' state (the Mods tab, tm mod
// list, tm doctor); nil before the server runs them.
func (s *Server) ModHosts() []modsup.Status {
	if sup := s.modSup.Load(); sup != nil {
		return sup.Status()
	}
	return nil
}
