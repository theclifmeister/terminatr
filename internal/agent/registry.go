package agent

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/guard"
	"github.com/theclifmeister/terminatr/internal/mod"
)

//go:embed manifests/*.toml
var builtin embed.FS

// The consent screen names a mod's agent manifest that replaces a
// built-in agent (mod.BuiltinAgents).
func init() {
	names, _ := fs.Glob(builtin, "manifests/*.toml")
	for _, n := range names {
		mod.BuiltinAgents = append(mod.BuiltinAgents, strings.TrimSuffix(path.Base(n), ".toml"))
	}
}

// goAgents holds agents that need Go code. A Go agent registers a
// constructor that receives its manifest and wraps FromManifest.
var goAgents = map[string]func(*Manifest) Agent{}

// RegisterGo registers Go code for the agent named name. Call it from an
// init function in internal/agent/<name>.
func RegisterGo(name string, wrap func(*Manifest) Agent) { goAgents[name] = wrap }

// Registry is the set of agents known to this binary and this user.
type Registry struct {
	agents map[string]Agent
	// Source records where each agent's manifest came from, for `tm agent list`.
	Source map[string]string
}

// Load reads the built-in manifests, then user manifests from userDir
// (~/.terminatr/agents). A user manifest with a built-in's name replaces
// it. Broken user manifests are reported but don't stop the others.
func Load(userDir string) (*Registry, error) {
	r := &Registry{agents: map[string]Agent{}, Source: map[string]string{}}
	var errs []error

	names, _ := fs.Glob(builtin, "manifests/*.toml")
	for _, n := range names {
		data, err := builtin.ReadFile(n)
		if err != nil {
			return nil, err
		}
		if err := r.add(data, "builtin:"+n); err != nil {
			// A broken built-in manifest is a bug in this binary.
			return nil, fmt.Errorf("%s: %w", n, err)
		}
	}

	if userDir != "" {
		// The enabled mods' agents/*.toml install as if in userDir
		// (mods design §2 tier 3), the first enabled winning a name; the
		// user's own file of that name wins over them.
		dirs := modAgentDirs(userDir)
		for i := len(dirs) - 1; i >= 0; i-- {
			files, _ := filepath.Glob(filepath.Join(dirs[i].Dir, "*.toml"))
			for _, f := range files {
				data, err := readManifest(dirs[i].Dir, f)
				if err == nil {
					err = r.add(data, f)
				}
				if err != nil {
					errs = append(errs, fmt.Errorf("mod %s: %s: %w", dirs[i].Mod, filepath.Base(f), err))
				}
			}
		}
		files, _ := filepath.Glob(filepath.Join(userDir, "*.toml"))
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err == nil {
				err = r.add(data, f)
			}
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", f, err))
			}
		}
	}
	return r, errors.Join(errs...)
}

// modAgentDirs are the agent folders of the enabled mods installed
// beside userDir (<home>/mods beside <home>/agents).
func modAgentDirs(userDir string) []mod.AgentDir {
	modsDir := filepath.Join(filepath.Dir(userDir), "mods")
	if ents, _ := os.ReadDir(modsDir); len(ents) == 0 {
		return nil
	}
	cfg, _ := config.Load()
	var tables map[string]config.Mod
	if cfg != nil {
		tables = cfg.Mods
	}
	reg, err := mod.LoadDir(modsDir, tables)
	if err != nil {
		return nil
	}
	return reg.AgentDirs()
}

// readManifest reads a mod's agent manifest in its folder dir: a file
// there, not a link out of it, at most mod.MaxDataFile, plain text
// (mod.AgentText).
func readManifest(dir, path string) ([]byte, error) {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	if filepath.Dir(real) != root {
		return nil, errors.New("leads out of the mod's agents folder")
	}
	fi, err := os.Stat(real)
	if err != nil {
		return nil, err
	}
	if fi.Size() > mod.MaxDataFile {
		return nil, fmt.Errorf("over %d KiB", mod.MaxDataFile>>10)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return mod.AgentText("", data)
}

func (r *Registry) add(data []byte, source string) error {
	m, err := ParseManifest(data)
	if err != nil {
		return err
	}
	if base := strings.TrimSuffix(filepath.Base(source), ".toml"); base != m.Name {
		return fmt.Errorf("file name %q must match name = %q", base, m.Name)
	}
	r.agents[m.Name] = Wrap(m)
	r.Source[m.Name] = source
	return nil
}

// BuiltinAgent is the built-in agent name, from tm's own manifest alone:
// never a user's manifest of that name, nor a mod's. ok is false for a
// name tm doesn't ship.
func BuiltinAgent(name string) (Agent, bool) {
	b, ok := Builtin(name)
	if !ok {
		return nil, false
	}
	m, err := ParseManifest(b)
	if err != nil {
		return nil, false
	}
	return Wrap(m), true
}

// Wrap is the agent for m: its Go agent when one is registered under
// its name (RegisterGo), else the manifest alone.
func Wrap(m *Manifest) Agent {
	if wrap, ok := goAgents[m.Name]; ok {
		return wrap(m)
	}
	return FromManifest(m)
}

// Get returns the agent named name.
func (r *Registry) Get(name string) (Agent, bool) {
	a, ok := r.agents[name]
	return a, ok
}

// Identify returns the agent whose manifest claims this process.
func (r *Registry) Identify(p ProcessInfo) (Agent, bool) {
	for _, n := range r.Names() {
		if a := r.agents[n]; a.Identify(p) {
			return a, true
		}
	}
	return nil, false
}

// Names lists the known agents, sorted.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.agents))
	for n := range r.agents {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Builtin returns the text of a built-in manifest, e.g. as the starting
// point of a user manifest (docs/SPEC.md §8.7) or a test's copy.
func Builtin(name string) ([]byte, bool) {
	b, err := builtin.ReadFile("manifests/" + name + ".toml")
	return b, err == nil
}

// RoleFiles is the union of the agents' role_files, sorted: the extra
// names the coordinator's role file is linked under. reg nil means the
// built-in manifests.
func RoleFiles(reg *Registry) []string {
	if reg == nil {
		reg, _ = Load("")
	}
	var out []string
	if reg == nil {
		return nil
	}
	for _, n := range reg.Names() {
		if m := ManifestOf(reg.agents[n]); m != nil {
			for _, f := range m.RoleFiles {
				if !slices.Contains(out, f) {
					out = append(out, f)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// HomeSecrets are the credential stores, relative to home, that belong
// to no agent; each agent's own are in its manifest ([guard] secrets).
var HomeSecrets = []string{
	".ssh", ".aws", ".gnupg", ".netrc", ".git-credentials", ".npmrc", ".pypirc",
	".config/gh", ".config/gcloud", ".azure", ".azure-devops", ".docker/config.json", ".kube",
}

// Secrets is every credential store the guard and the mods broker keep
// everyone out of: HomeSecrets under home (none when home is empty) and
// GuardSecrets.
func Secrets(reg *Registry, home string, getenv func(string) string) []string {
	var out []string
	if home != "" {
		for _, p := range HomeSecrets {
			out = append(out, filepath.Join(home, p))
		}
	}
	return append(out, GuardSecrets(reg, home, getenv)...)
}

// GuardSecrets is every known agent's [guard] secrets, resolved: the
// built-in manifests' and reg's (nil: the built-ins only), so a user
// manifest replacing a built-in one doesn't drop what it protected.
func GuardSecrets(reg *Registry, home string, getenv func(string) string) []string {
	var out []string
	for _, m := range knownManifests(reg) {
		for _, p := range GuardPaths(m.Guard.Secrets, home, getenv) {
			if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out
}

// ShellStartup are the user's shell startup files, relative to home or
// under $ZDOTDIR or $XDG_CONFIG_HOME: what a shell runs before a
// command, and what an agent's snapshot of the user's shell (Claude's)
// is made from. zsh's come with their compiled .zwc twins, which it
// loads when they are newer, and its completion dumps (.zcompdump*,
// ShellState).
var ShellStartup = func() []string {
	out := []string{".profile", ".bashrc", ".bash_profile", ".bash_login", ".bash_logout", ".bash_aliases", ".bashrc.d",
		".zshrc.d", ".config/zsh", "$XDG_CONFIG_HOME/zsh", ".kshrc", ".mkshrc", ".config/fish"}
	for _, dir := range []string{"", "$ZDOTDIR/"} {
		for _, n := range []string{".zshenv", ".zprofile", ".zshrc", ".zlogin", ".zlogout", ".zcompdump"} {
			out = append(out, dir+n, dir+n+".zwc")
		}
	}
	return out
}()

// ShellState is every file a session's shell may run before its
// commands, which no session writes (the guard's floor): ShellStartup,
// the .zcompdump* files in home and $ZDOTDIR, and every known agent's
// [guard] shell_state, resolved as GuardSecrets does.
func ShellState(reg *Registry, home string, getenv func(string) string) []string {
	out := GuardPaths(ShellStartup, home, getenv)
	for _, dir := range GuardPaths([]string{".", "$ZDOTDIR"}, home, getenv) {
		es, _ := os.ReadDir(dir)
		for _, e := range es {
			if p := filepath.Join(dir, e.Name()); strings.HasPrefix(e.Name(), ".zcompdump") && !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	for _, m := range knownManifests(reg) {
		for _, p := range GuardPaths(m.Guard.ShellState, home, getenv) {
			if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out
}

// SharedTemps is the temp folders that every known agent's sessions of
// the user share ([guard] temp_scratch: Claude Code's /tmp/claude-<uid>,
// with its sessions' scratchpads and task output), resolved as
// AgentTemps does: a thread, which has its own (home.ThreadTempDir),
// never reads nor writes them (T587). Only per-user entries ("{uid}"):
// never a folder like /tmp, which the system's tools write too.
func SharedTemps(reg *Registry, home string, getenv func(string) string) []string {
	var out []string
	for _, m := range knownManifests(reg) {
		if !m.Guard.TempScratch {
			continue
		}
		for _, e := range m.Guard.Temp {
			if !strings.Contains(e, "{uid}") {
				continue
			}
			for _, p := range GuardTemps([]string{e}, home, getenv) {
				if !slices.Contains(out, p) {
					out = append(out, p)
				}
			}
		}
	}
	return out
}

// OwnTempFits reports whether the agent named name keeps its sandbox's
// TMPDIR in its temp folder when its temp variables are dir (a thread's
// own temp folder): its first [guard] temp entry, so resolved, is at
// most [guard] temp_max bytes. Else it falls back to a shared one, which
// a thread then can't be kept from.
func OwnTempFits(reg *Registry, name, dir string) bool {
	m := manifestNamed(reg, name)
	if m == nil || m.Guard.TempMax <= 0 {
		return true
	}
	t := GuardTemp(m.Guard.Temp, "", func(string) string { return dir })
	return len(t) <= m.Guard.TempMax
}

// knownManifests is the built-in manifests and reg's (nil: the
// built-ins only).
func knownManifests(reg *Registry) []*Manifest {
	var out []*Manifest
	names, _ := fs.Glob(builtin, "manifests/*.toml")
	for _, n := range names {
		data, _ := builtin.ReadFile(n)
		if m, err := ParseManifest(data); err == nil {
			out = append(out, m)
		}
	}
	if reg != nil {
		for _, n := range reg.Names() {
			if m := ManifestOf(reg.agents[n]); m != nil {
				out = append(out, m)
			}
		}
	}
	return out
}

// GuardWritable is the [guard] writable folders of the agent named
// name, resolved: reg's manifest, else the built-in one.
func GuardWritable(reg *Registry, name, home string, getenv func(string) string) []string {
	m := manifestNamed(reg, name)
	if m == nil {
		return nil
	}
	return GuardPaths(m.Guard.Writable, home, getenv)
}

// AgentTemp is the temp folder of the agent named name ([guard] temp,
// GuardTemp; "" for none) and, resolved in it, the subfolders its
// sandbox keeps from it ([guard] temp_private): reg's manifest, else
// the built-in one.
func AgentTemp(reg *Registry, name, home string, getenv func(string) string) (dir string, private []string) {
	m := manifestNamed(reg, name)
	if m == nil {
		return "", nil
	}
	if dir = GuardTemp(m.Guard.Temp, home, getenv); dir == "" {
		return "", nil
	}
	for _, p := range m.Guard.TempPrivate {
		private = append(private, filepath.Join(dir, p))
	}
	return dir, private
}

// AgentTemps is every temp folder the agent named name may write (each
// [guard] temp entry that resolves) and the subfolders of them its
// sandbox keeps from it.
func AgentTemps(reg *Registry, name, home string, getenv func(string) string) (dirs, private []string, scratch bool) {
	m := manifestNamed(reg, name)
	if m == nil {
		return nil, nil, false
	}
	// The first that resolves is the one the sandbox most likely uses;
	// the others count only when per user ("{uid}"), never a shared
	// folder like /tmp.
	for _, e := range m.Guard.Temp {
		if len(dirs) > 0 && !strings.Contains(e, "{uid}") {
			continue
		}
		dirs = append(dirs, GuardTemps([]string{e}, home, getenv)...)
	}
	for _, d := range dirs {
		for _, p := range m.Guard.TempPrivate {
			private = append(private, filepath.Join(d, p))
		}
	}
	return dirs, private, m.Guard.TempScratch
}

// GuardTools is the [guard.tools] of the agent named name: reg's
// manifest, else the built-in one.
func GuardTools(reg *Registry, name string) map[string]guard.Tool {
	if m := manifestNamed(reg, name); m != nil {
		return m.Guard.Tools
	}
	return nil
}

// manifestNamed is the manifest of the agent named name in reg, or with
// reg nil the built-in one; nil for none.
func manifestNamed(reg *Registry, name string) *Manifest {
	if reg != nil {
		if a, ok := reg.Get(name); ok {
			return ManifestOf(a)
		}
		return nil
	}
	data, ok := Builtin(name)
	if !ok {
		return nil
	}
	m, _ := ParseManifest(data)
	return m
}
