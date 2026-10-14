package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// VersionTimeout bounds one `<agent> --version` (the manifest's
// version_args).
const VersionTimeout = 5 * time.Second

// Versions caches each agent binary's version by its path, size and
// modification time, so an upgrade is seen at the next ask while a
// binary that hasn't changed is run once. The zero Versions is ready.
type Versions struct {
	mu   sync.Mutex
	seen map[string]versionSeen
	// Run runs the binary with args under env and returns its standard
	// output (exec by default; tests).
	Run func(ctx context.Context, path string, args, env []string) ([]byte, error)
}

type versionSeen struct {
	size    int64
	mod     time.Time
	version string
}

// versions is the process's cache, behind Version.
var versions Versions

// Version is the version the binary at path prints for the manifest's
// version_args, run under env (nil: tm's own) within VersionTimeout and
// cached per binary for the process. It fails when the manifest has no
// version_args or the output holds no version.
func Version(ctx context.Context, path string, m *Manifest, env []string) (string, error) {
	return versions.Of(ctx, path, m, env)
}

// Of is Version on vs's cache.
func (vs *Versions) Of(ctx context.Context, path string, m *Manifest, env []string) (string, error) {
	if m == nil || len(m.Identify.VersionArgs) == 0 {
		return "", errors.New("the manifest has no version_args")
	}
	args := m.Identify.VersionArgs
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	key := path + "\x00" + strings.Join(args, "\x00")
	vs.mu.Lock()
	defer vs.mu.Unlock()
	if c, ok := vs.seen[key]; ok && c.size == fi.Size() && c.mod.Equal(fi.ModTime()) {
		return c.version, nil
	}
	run := vs.Run
	if run == nil {
		run = runVersion
	}
	ctx, cancel := context.WithTimeout(ctx, VersionTimeout)
	defer cancel()
	out, err := run(ctx, path, args, env)
	if err != nil {
		return "", err
	}
	v := ParseVersion(string(out))
	if v == "" {
		return "", errors.New("no version in " + strings.TrimSpace(string(out)))
	}
	if vs.seen == nil {
		vs.seen = map[string]versionSeen{}
	}
	vs.seen[key] = versionSeen{size: fi.Size(), mod: fi.ModTime(), version: v}
	return v, nil
}

func runVersion(ctx context.Context, path string, args, env []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	cmd.WaitDelay = time.Second
	return cmd.Output()
}
