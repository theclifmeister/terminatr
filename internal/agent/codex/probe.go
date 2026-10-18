package codex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/plat/caps"
	"github.com/theclifmeister/terminatr/internal/plat/shell"
)

// The coordinator's sandbox (docs/SPEC.md §8.6, Codex) rests on Codex's
// experimental network proxy and its permission profile syntax, either of
// which a newer Codex may rename or change. Codex ignores a -c key it
// doesn't know (unless --strict-config), so a renamed key would leave
// the coordinator either unsandboxed or with the network wide open, and a
// changed one may stop it from starting. So before a coordinator starts,
// its profile flags run under `codex sandbox` with a probe inside: tm's
// socket must connect and a TCP connection to a loopback listener must
// be refused (limited mode refuses every address but the proxy's). When
// that doesn't hold, the profile flags are dropped (the coordinator runs
// as before them: --approve-for-me, workspace-write, network off) and
// the launch carries a warning naming the Codex version.
//
// Windows is different (docs/CODEX.md): Codex limits the network to its
// proxy only in its elevated sandbox (firewall rules for a dedicated
// sandbox account, installed with administrator approval). Its unelevated
// sandbox, a restricted token, refuses managed networking, and `codex
// sandbox` there never starts the proxy, so the limited profile can't
// hold (measured on Codex 0.162.1, T462: loopback TCP connects under it).
// There a coordinator's profile turns the network off instead
// (closedNetwork), which the restricted token enforces only through
// proxy variables pointing nowhere: gh, az and git calls fail in the
// sandbox and ask auto-review, as on other systems, but a program that
// ignores them still connects. tm's socket stays reachable, through the
// write grant on its folder (socketAccess). Before a coordinator starts
// the probe then checks that Codex takes the profile and tm's socket
// connects; tm doctor also checks, in a scratch folder, that a file
// outside the workspace can't be written (not at launch: Codex's
// workspace there includes %TEMP%, and no other folder is safe to try).

// ProbeCommand is the tm command the probe runs inside the sandbox (main
// dispatches it): `tm codex-sandbox-probe <unix socket> <tcp address>`.
const ProbeCommand = "codex-sandbox-probe"

// probeWant is what the probe prints when the sandbox holds.
const probeWant = "unix=ok tcp=refused"

// threadWant is what a thread's probe prints when its sandbox holds:
// tm's socket connects and a file outside the workspace can't be made.
const threadWant = "unix=ok write=denied"

// ProbeMain is the probe: it dials the unix socket and the TCP address
// and prints how each went. With a third argument, a path to create, it
// prints whether that write was denied instead of the TCP result (the
// Windows form, whose network can't be limited, and tm doctor's thread
// check before its network probe).
func ProbeMain(args []string) int {
	if len(args) != 2 && len(args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: tm "+ProbeCommand+" SOCKET HOST:PORT [WRITE-PATH]")
		return 2
	}
	dial := func(network, addr string) string {
		c, err := net.DialTimeout(network, addr, 3*time.Second)
		if err != nil {
			return "refused"
		}
		c.Close()
		return "ok"
	}
	if len(args) == 3 {
		w := "denied"
		if f, err := os.OpenFile(args[2], os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600); err == nil {
			f.Close()
			os.Remove(args[2])
			w = "ok"
		}
		fmt.Printf("unix=%s write=%s\n", dial("unix", args[0]), w)
		return 0
	}
	fmt.Printf("unix=%s tcp=%s\n", dial("unix", args[0]), dial("tcp", args[1]))
	return 0
}

// closedNetwork turns a launch's argv into its closed-network form:
// features.network_proxy dropped, the profile's network off rather than
// limited to the proxy, without the domains and local binding that only
// limited mode reads. tm's socket stays in the profile. A coordinator's
// and a thread's form on Windows, and a thread's where its limited
// network doesn't hold.
func closedNetwork(argv []string) []string {
	out := make([]string, 0, len(argv))
	for i := 0; i < len(argv); i++ {
		if argv[i] == "-c" && i+1 < len(argv) {
			v := argv[i+1]
			if strings.HasPrefix(v, "features.network_proxy=") {
				i++
				continue
			}
			if mode := proxied(v); mode != "" {
				v = strings.Replace(v, mode, "enabled=false", 1)
				v = cutTable(v, ",domains=")
				v = strings.Replace(v, ",allow_local_binding=true", "", 1)
			}
			out = append(out, "-c", v)
			i++
			continue
		}
		out = append(out, argv[i])
	}
	return out
}

// proxied is the network part of profile flag value v that the proxy
// enforces (`enabled=true,mode="limited"` for a coordinator, "full" for
// a thread), or "".
func proxied(v string) string {
	if !strings.HasPrefix(v, "permissions=") {
		return ""
	}
	for _, m := range []string{`enabled=true,mode="limited"`, `enabled=true,mode="full"`} {
		if strings.Contains(v, m) {
			return m
		}
	}
	return ""
}

// cutTable is v without its inline table key (",domains=", say) and the
// table after it, up to its closing brace; quoted keys may hold braces.
func cutTable(v, key string) string {
	at := strings.Index(v, key+"{")
	if at < 0 {
		return v
	}
	depth, quoted := 0, false
	for i := at + len(key); i < len(v); i++ {
		switch c := v[i]; {
		case quoted && c == '\\':
			i++
		case c == '"':
			quoted = !quoted
		case quoted:
		case c == '{':
			depth++
		case c == '}':
			if depth--; depth == 0 {
				return v[:at] + v[i+1:]
			}
		}
	}
	return v
}

// unbound is a profile's flags without the local binding, for the
// network probe: allow_local_binding reopens loopback, which the probe
// relies on being refused.
func unbound(flags []string) []string {
	out := slices.Clone(flags)
	for i, v := range out {
		if strings.HasPrefix(v, "permissions=") {
			out[i] = strings.Replace(v, ",allow_local_binding=true", "", 1)
		}
	}
	return out
}

// profileFlags finds the profile's -c flags in argv: the indexes of each
// flag and its value, the profile's name from default_permissions, and
// the other pairs, for `codex sandbox`, where -P names the profile.
func profileFlags(argv []string) (idx []int, name string, flags []string) {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] != "-c" {
			continue
		}
		v := argv[i+1]
		switch {
		case strings.HasPrefix(v, "default_permissions="):
			name = strings.Trim(strings.TrimPrefix(v, "default_permissions="), `"`)
		case strings.HasPrefix(v, "features.network_proxy="), strings.HasPrefix(v, "permissions="):
			flags = append(flags, "-c", v)
		default:
			continue
		}
		idx = append(idx, i, i+1)
		i++
	}
	return idx, name, flags
}

// probeFunc checks that the profile flags hold under the codex binary
// and returns its version as m's version_args print it ("" when it
// can't tell); windows checks the Windows form (closedNetwork). A
// variable for tests.
var probeFunc = probeProfile

// probeCache keeps each verdict for a binary (path, size, mtime) and
// flags, so a coordinator's restart doesn't probe again.
var probeCache sync.Map // string -> probeVerdict

type probeVerdict struct {
	version string
	err     error
}

// sandboxed returns l with the profile kept when it holds, or dropped,
// with a warning, when it doesn't.
func sandboxed(l agent.Launch, spec agent.LaunchSpec, m *agent.Manifest, windows bool) agent.Launch {
	if windows {
		l.Argv = closedNetwork(l.Argv)
	}
	idx, name, flags := profileFlags(l.Argv)
	if len(idx) == 0 {
		return l
	}
	version, err := probeFunc(m, l.Argv[0], spec.TMBin, spec.Cwd, spec.Socket, name, flags, windows)
	if err == nil {
		return l
	}
	drop := map[int]bool{}
	for _, i := range idx {
		drop[i] = true
	}
	argv := make([]string, 0, len(l.Argv)-len(idx))
	for i, a := range l.Argv {
		if !drop[i] {
			argv = append(argv, a)
		}
	}
	l.Argv = argv
	if version == "" {
		version = "(version unknown)"
	}
	l.Warnings = append(l.Warnings, fmt.Sprintf(
		"codex %s: the coordinator's sandbox profile doesn't hold (%v); running the coordinator without it (--approve-for-me: workspace-write, network off, tm calls go to auto-review)",
		version, err))
	return l
}

// limited returns a thread's launch l with its network limited to tm's
// socket and its hosts when that holds on this Codex (T574 case 5),
// else closed, with a warning: a Codex that renames the proxy feature
// or a key would otherwise leave the network open (T577). The probe is
// the coordinator's (tm's socket connects, a loopback address outside
// the domains is refused), without the local binding. On Windows, where
// Codex's proxy needs its elevated sandbox, the network is off.
func limited(l agent.Launch, spec agent.LaunchSpec, m *agent.Manifest, windows bool) agent.Launch {
	idx, name, flags := profileFlags(l.Argv)
	if len(idx) == 0 {
		return l // yolo: no sandbox to shape
	}
	if windows {
		l.Argv = closedNetwork(l.Argv)
		return l
	}
	version, err := probeFunc(m, l.Argv[0], spec.TMBin, spec.Cwd, spec.Socket, name, unbound(flags), false)
	if err == nil {
		return l
	}
	l.Argv = closedNetwork(l.Argv)
	if version == "" {
		version = "(version unknown)"
	}
	l.Warnings = append(l.Warnings, fmt.Sprintf(
		"codex %s: the thread's limited network doesn't hold (%v); running the thread with its network off, its hosts unreachable",
		version, err))
	return l
}

// probeProfile runs `codex sandbox -P <name> -C cwd <flags> -- tm
// codex-sandbox-probe <sock> <loopback>` and reports why the sandbox
// doesn't hold, if it doesn't.
func probeProfile(m *agent.Manifest, codex, tmBin, cwd, sock, name string, flags []string, windows bool) (string, error) {
	path, err := exec.LookPath(codex)
	if err != nil {
		return "", err
	}
	version, _ := agent.Version(context.Background(), path, m, nil)
	key := fmt.Sprint(windows) + "\x00" + path + "\x00" + tmBin + "\x00" + cwd + "\x00" + sock + "\x00" + name + "\x00" + strings.Join(flags, "\x00")
	if fi, err := os.Stat(path); err == nil {
		key += fmt.Sprintf("\x00%d\x00%d", fi.Size(), fi.ModTime().UnixNano())
	}
	if v, ok := probeCache.Load(key); ok {
		p := v.(probeVerdict)
		return p.version, p.err
	}
	err = probeOnce(path, tmBin, cwd, sock, name, flags, windows)
	probeCache.Store(key, probeVerdict{version, err})
	return version, err
}

func probeOnce(codex, tmBin, cwd, sock, name string, flags []string, windows bool) error {
	if tmBin == "" || cwd == "" || sock == "" || name == "" {
		return errors.New("nothing to probe with: no tm binary, cwd, socket or profile name")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("probe listener: %v", err)
	}
	defer ln.Close()
	args := append([]string{"sandbox", "-P", name, "-C", cwd}, flags...)
	args = append(args, "--", tmBin, ProbeCommand, sock, ln.Addr().String())
	out, err := run(codex, flagsEnv(), args...)
	last := lastLine(out)
	switch {
	case err != nil:
		return fmt.Errorf("codex sandbox refused it: %s", firstLine(out, err))
	case last == probeWant, windows && strings.HasPrefix(last, "unix=ok tcp="):
		return nil
	case strings.HasPrefix(last, "unix=refused"):
		return errors.New("the profile wasn't applied: tm's socket is unreachable in it")
	case strings.HasSuffix(last, "tcp=ok"):
		return errors.New("the network isn't limited to tm's socket: features.network_proxy or network.mode was ignored")
	}
	return fmt.Errorf("unexpected probe output %q", firstLine(out, nil))
}

// probeTimeout bounds each codex run of the probe.
const probeTimeout = 15 * time.Second

func run(path string, env []string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	cmd := shell.CLICommand(ctx, path, args...)
	shell.SetEnv(cmd, env)
	var b bytes.Buffer
	cmd.Stdout, cmd.Stderr = &b, &b
	err := cmd.Run()
	if ctx.Err() != nil {
		err = fmt.Errorf("timed out after %s", probeTimeout)
	}
	return b.String(), err
}

// flagsEnv is the environment for the probe's codex: the server's,
// without what a Codex session gives the commands it runs (unset_env).
func flagsEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "CODEX_THREAD_ID=") && !strings.HasPrefix(kv, "CODEX_SANDBOX") {
			env = append(env, kv)
		}
	}
	return env
}

// lastLine is out's last non-empty line, skipping Codex's warnings.
func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" && !strings.HasPrefix(l, "WARNING") {
			return l
		}
	}
	return ""
}

// firstLine is the first line of out worth quoting (Codex's error, not
// its warnings), else err, bounded.
func firstLine(out string, err error) string {
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "WARNING") {
			if len(l) > 200 {
				l = l[:200] + "…"
			}
			return l
		}
	}
	if err != nil {
		return err.Error()
	}
	return "no output"
}

// probeScratch makes a probe's scratch folders (base holding work,
// outside and run) and listens on a unix socket in run; done removes
// them and closes the socket.
func probeScratch() (work, outside, base, sock string, done func(), err error) {
	base, err = os.MkdirTemp("", "tm-probe-")
	if err != nil {
		return
	}
	if real, rerr := filepath.EvalSymlinks(base); rerr == nil {
		base = real
	}
	work, outside, rundir := filepath.Join(base, "work"), filepath.Join(base, "outside"), filepath.Join(base, "run")
	for _, d := range []string{work, outside, rundir} {
		if err = os.Mkdir(d, 0o700); err != nil {
			os.RemoveAll(base)
			return
		}
	}
	sock = filepath.Join(rundir, "s.sock")
	ln, lerr := net.Listen("unix", sock)
	if lerr != nil {
		os.RemoveAll(base)
		err = lerr
		return
	}
	done = func() {
		ln.Close()
		os.RemoveAll(base)
	}
	return
}

// probeTarget is the codex a probe runs, its version, and the profile in
// argv (none: noProfile is the error).
func (a *Agent) probeTarget(argv []string, noProfile string) (path, version, name string, flags []string, err error) {
	idx, name, flags := profileFlags(argv)
	if len(idx) == 0 {
		return "", "", "", nil, errors.New(noProfile)
	}
	path, err = exec.LookPath(argv[0])
	if err != nil {
		return "", "", "", nil, err
	}
	version, _ = agent.Version(context.Background(), path, a.m, nil)
	return path, version, name, flags, nil
}

// ProbeSandbox checks the installed Codex as a coordinator's launch
// would, against a scratch folder and socket (agent.SandboxProber, for
// tm doctor), uncached: the Codex version and why it doesn't hold. On
// Windows it checks tm's socket and a write outside the workspace
// instead of the network.
func (a *Agent) ProbeSandbox(tmBin string) (string, error) {
	if caps.Windows {
		return a.probeWindowsCoordinator(tmBin)
	}
	dir, err := os.MkdirTemp("", "tm-probe-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	sock := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return "", err
	}
	defer ln.Close()
	l, err := a.Agent.Launch(agent.LaunchSpec{
		Role: "coordinator", Cwd: dir, RuntimeDir: dir, TMBin: tmBin, Socket: sock,
		Access: agent.Access{Read: []string{dir}},
	})
	if err != nil {
		return "", err
	}
	path, version, name, flags, err := a.probeTarget(l.Argv, "the manifest gives the coordinator no profile")
	if err != nil {
		return "", err
	}
	return version, probeOnce(path, tmBin, dir, sock, name, flags, false)
}

// probeWindowsCoordinator is ProbeSandbox on Windows: a coordinator's
// profile in its Windows form (closedNetwork), with the scratch
// folder above the workspace read-only as a coordinator's worktrees are,
// so the probe's write there must be denied.
func (a *Agent) probeWindowsCoordinator(tmBin string) (string, error) {
	work, outside, base, sock, done, err := probeScratch()
	if err != nil {
		return "", err
	}
	defer done()
	l, err := a.Agent.Launch(socketAccess(agent.LaunchSpec{
		Role: "coordinator", Cwd: work, RuntimeDir: base, TMBin: tmBin, Socket: sock,
		Access: agent.Access{Read: []string{work, base}},
	}, true))
	if err != nil {
		return "", err
	}
	path, version, name, flags, err := a.probeTarget(closedNetwork(l.Argv), "the manifest gives the coordinator no profile")
	if err != nil {
		return "", err
	}
	return version, writeVerdict(path, tmBin, work, sock, name, flags, filepath.Join(outside, "probe"))
}

// ProbeThreadSandbox checks a thread's profile under the installed Codex
// (agent.ThreadSandboxProber, for tm doctor): Codex's Windows sandbox
// needs a one-time setup, and without it every command of a thread asks
// for approval (docs/CODEX.md). The probe runs inside the profile, so it
// holds only when the sandbox applies and tm's socket (AF_UNIX on
// Windows) is reachable from it. Elsewhere it also checks the network
// is limited: a host outside the thread's domains (a loopback listener)
// refused, as a thread's launch checks (limited).
func (a *Agent) ProbeThreadSandbox(tmBin string) (string, error) {
	work, outside, base, sock, done, err := probeScratch()
	if err != nil {
		return "", err
	}
	defer done()
	l, err := a.Agent.Launch(socketAccess(agent.LaunchSpec{
		Role: "thread", Cwd: work, RuntimeDir: base, TMBin: tmBin, Socket: sock,
		Access:  agent.Access{Write: []string{work}, Read: []string{base}},
		Sandbox: agent.SandboxLists{Domains: []string{"example.com"}},
	}, caps.Windows))
	if err != nil {
		return "", err
	}
	argv := l.Argv
	if caps.Windows {
		argv = closedNetwork(argv)
	}
	// A thread's profile is its -c features and permissions flags;
	// profileFlags takes them as it takes the coordinator's.
	path, version, name, flags, err := a.probeTarget(argv, "the manifest gives a thread no profile")
	if err != nil {
		return "", err
	}
	if err := writeVerdict(path, tmBin, work, sock, name, flags, filepath.Join(outside, "probe")); err != nil || caps.Windows {
		return version, err
	}
	return version, probeOnce(path, tmBin, work, sock, name, flags, false)
}

// writeVerdict runs a probe that dials tm's socket and writes path,
// outside the workspace, and says why the sandbox doesn't hold.
func writeVerdict(codex, tmBin, cwd, sock, name string, flags []string, path string) error {
	args := append([]string{"sandbox", "-P", name, "-C", cwd}, flags...)
	args = append(args, "--", tmBin, ProbeCommand, sock, "127.0.0.1:1", path)
	out, err := run(codex, flagsEnv(), args...)
	switch last := lastLine(out); {
	case err != nil:
		return fmt.Errorf("codex sandbox refused it: %s", firstLine(out, err))
	case last == threadWant:
		return nil
	case strings.HasPrefix(last, "unix=refused"):
		return errors.New("tm's socket is unreachable from the sandbox")
	case strings.HasSuffix(last, "write=ok"):
		return errors.New("the sandbox doesn't apply: a file outside the workspace could be written")
	}
	return fmt.Errorf("unexpected probe output %q", firstLine(out, nil))
}
