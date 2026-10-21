package server

// terminatr's mod (docs/SPEC.md §8.6, Mods): an agent that ships one
// (agent.Modder) loads it only when [agent_mod] enabled is on and the agent's
// version is at least the one the mod was tested on. Otherwise the
// session starts as before, on its command hooks alone, which stay in
// either case.

import (
	"context"
	"errors"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/plat/shell"
)

// launchConfig is config.toml as one launch reads it, once
// (config.Load's config and error): the settings a launch asks for are
// read from it.
type launchConfig struct {
	cfg *config.Config
	err error
}

// loadLaunchConfig reads config.toml for a launch.
func loadLaunchConfig() launchConfig {
	cfg, err := config.Load()
	return launchConfig{cfg: cfg, err: err}
}

// safety is project slug's resolved settings; a config.toml that doesn't
// load leaves the defaults at protection sandboxed, so the guard keeps
// its rules: a broken file never opens anything.
func (l launchConfig) safety(slug string) config.Safety {
	closed := config.Defaults
	closed.Protection = config.ProtectSandboxed
	if l.err != nil {
		return closed
	}
	s, err := l.cfg.Safety(slug)
	if err != nil {
		return closed
	}
	return s
}

// agentMod is [agent_mod] enabled; a config.toml that doesn't load leaves
// mods off.
func (l launchConfig) agentMod() bool {
	return l.err == nil && l.cfg.AgentMod
}

// envBand is set to "off" in a session with the mod when [agent_mod] band is
// false: the mod then draws no band, status entry or toast.
const envBand = "TERMINATR_BAND"

// band is [agent_mod] band; a config.toml that doesn't load leaves it
// on, as when unset.
func (l launchConfig) band() bool {
	return l.err != nil || l.cfg.AgentModBand
}

// envPane is set to "off" in a coordinator session with the mod unless
// [agent_mod] pane is true: the mod then opens its /tm pane only when asked.
const envPane = "TERMINATR_PANE"

// pane is [agent_mod] pane; a config.toml that doesn't load leaves it
// off, as when unset (T90).
func (l launchConfig) pane() bool {
	return l.err == nil && l.cfg.AgentModPane
}

// modsFor reports whether a session of a gets terminatr's mod, by the
// settings in lc, and logs why not when the setting asks for it.
func (s *Server) modsFor(a agent.Agent, id string, lc launchConfig) bool {
	m, ok := a.(agent.Modder)
	if !ok || (!m.ModRequired() && !lc.agentMod()) {
		return false
	}
	v, err := s.versionOf(a, s.baseEnv())
	if err != nil {
		s.log.Printf("session %s: no mod: %s version: %v", id, a.Name(), err)
		return false
	}
	if !agent.VersionAtLeast(v, m.ModsMinVersion()) {
		s.log.Printf("session %s: no mod: %s %s is older than %s", id, a.Name(), v, m.ModsMinVersion())
		return false
	}
	return true
}

// versionOf returns a's version as its version_args print it, the
// binary found on env's PATH as a session would find it (agent.Versions
// caches it per binary).
func (s *Server) versionOf(a agent.Agent, env []string) (string, error) {
	m := agent.ManifestOf(a)
	if m == nil || len(m.Identify.VersionArgs) == 0 {
		return "", errors.New("the manifest has no version_args")
	}
	pathEnv, _ := shell.EnvValue(env, "PATH")
	path, err := shell.LookPathIn(m.Launch.Command, pathEnv)
	if err != nil {
		return "", err
	}
	if s.versions == nil {
		return agent.Version(context.Background(), path, m, env)
	}
	return s.versions.Of(context.Background(), path, m, env)
}
