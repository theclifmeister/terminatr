package server

// terminatr's mod (docs/SPEC.md §8.6, Mods): an agent that ships one
// (agent.Modder) loads it only when [agent_mod] enabled is on and the agent's
// version is at least the one the mod was tested on. Otherwise the
// session starts as before, on its command hooks alone, which stay in
// either case.

import (
	"context"
	"errors"
	"strings"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/plat/shell"
)

// modsSetting is [agent_mod] enabled; a config.toml that doesn't load leaves
// mods off.
func modsSetting() bool {
	cfg, err := config.Load()
	return err == nil && cfg.AgentMod
}

// envBand is set to "off" in a session with the mod when [agent_mod] band is
// false: the mod then draws no band, status entry or toast.
const envBand = "TERMINATR_BAND"

// bandSetting is [agent_mod] band; a config.toml that doesn't load leaves it
// on, as when unset.
func bandSetting() bool {
	cfg, err := config.Load()
	return err != nil || cfg.AgentModBand
}

// envPane is set to "off" in a coordinator session with the mod unless
// [agent_mod] pane is true: the mod then opens its /tm pane only when asked.
const envPane = "TERMINATR_PANE"

// paneSetting is [agent_mod] pane; a config.toml that doesn't load leaves it
// off, as when unset (T90).
func paneSetting() bool {
	cfg, err := config.Load()
	return err == nil && cfg.AgentModPane
}

// modsFor reports whether a session of a gets terminatr's mod, and logs
// why not when the setting asks for it.
func (s *Server) modsFor(a agent.Agent, id string) bool {
	m, ok := a.(agent.Modder)
	if !ok || (!m.ModRequired() && !modsSetting()) {
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
	path, err := shell.LookPathIn(m.Launch.Command, envValue(env, "PATH"))
	if err != nil {
		return "", err
	}
	if s.versions == nil {
		return agent.Version(context.Background(), path, m, env)
	}
	return s.versions.Of(context.Background(), path, m, env)
}

// envValue is the value of key in env (KEY=VALUE entries), the last one
// winning.
func envValue(env []string, key string) string {
	v := ""
	for _, kv := range env {
		if k, val, ok := strings.Cut(kv, "="); ok && k == key {
			v = val
		}
	}
	return v
}
