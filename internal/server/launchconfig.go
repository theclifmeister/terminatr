package server

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
