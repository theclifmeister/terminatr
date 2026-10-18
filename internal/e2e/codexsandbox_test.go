package e2e

// The thread sandbox against the installed codex (T189): `codex sandbox`
// runs a shell under permission profile "tm" exactly as codex.toml
// renders it for a thread, with no login and no model. Seatbelt on
// macOS, bubblewrap or Landlock on Linux. It skips when codex isn't on
// PATH; CI installs the tested version on Linux and sets
// E2E_CODEX_SANDBOX=1, which makes a skip a failure.

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/agent/codex"
	"github.com/theclifmeister/terminatr/internal/gittest"
)

// sandboxArgs picks the profile's -c flags out of a thread's launch, in
// the network form it starts with here (codex.ThreadNetwork), and says
// which form.
func sandboxArgs(t *testing.T, spec agent.LaunchSpec) (string, []string) {
	t.Helper()
	m, _ := agent.Builtin("codex")
	pm, err := agent.ParseManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	l, err := agent.FromManifest(pm).Launch(spec)
	if err != nil {
		t.Fatal(err)
	}
	form, argv := codex.ThreadNetwork(l.Argv, runtime.GOOS)
	var out []string
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "-c" && (strings.HasPrefix(argv[i+1], "permissions=") || strings.HasPrefix(argv[i+1], "default_permissions=") ||
			strings.HasPrefix(argv[i+1], "features.network_proxy=")) {
			out = append(out, "-c", argv[i+1])
		}
	}
	if want := map[string]int{codex.NetworkLimited: 6, codex.NetworkOpen: 4}[form]; len(out) != want {
		t.Fatalf("no thread profile in %q", l.Argv)
	}
	return form, out
}

func TestSmokeCodexSandbox(t *testing.T) {
	skip := t.Skip
	if os.Getenv("E2E_CODEX_SANDBOX") == "1" {
		skip = t.Fatal
	}
	bin, err := exec.LookPath("codex")
	if err != nil {
		skip("codex is not on PATH")
	}
	// `codex sandbox -P` and the ":workspace" profile the thread
	// profile extends came in 0.128.
	if help, _ := exec.Command(bin, "sandbox", "--help").CombinedOutput(); !strings.Contains(string(help), "--permission-profile") {
		skip("codex sandbox has no --permission-profile (Codex before 0.128): the thread profile needs 0.128 or later")
	}
	if b, _ := os.ReadFile("/proc/sys/kernel/apparmor_restrict_unprivileged_userns"); strings.TrimSpace(string(b)) == "1" {
		skip("bwrap can't map a user namespace here: sysctl kernel.apparmor_restrict_unprivileged_userns=0 to run it")
	}
	env := New(t)
	// Everything outside /tmp and $TMPDIR, which the profile makes
	// writable: as in a real install, below the user's home. The socket
	// path must stay short.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(home, ".tmcxs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	root, _ = filepath.EvalSymlinks(root)
	repo, wt := filepath.Join(root, "repo"), filepath.Join(root, "wt")
	proj, outside, run := filepath.Join(root, "proj"), filepath.Join(root, "outside"), filepath.Join(root, "run")
	for _, d := range []string{repo, proj, outside, run} {
		os.MkdirAll(d, 0o700)
	}
	os.WriteFile(filepath.Join(proj, "TASKS.md"), []byte("tasks\n"), 0o600)
	// origin in the temporary folder, which the profile makes writable:
	// a push then writes only what a push to a real remote writes here.
	origin, other := filepath.Join(t.TempDir(), "origin.git"), filepath.Join(root, "other")
	gittest.Run(t, root, "init", "-q", "--bare", "-b", "main", origin)
	gittest.Run(t, repo, "init", "-q", "-b", "main")
	gittest.Run(t, repo, "commit", "-q", "--allow-empty", "-m", "first")
	gittest.Run(t, repo, "remote", "add", "origin", origin)
	gittest.Run(t, repo, "push", "-q", "origin", "main")
	gittest.Run(t, repo, "worktree", "add", "-q", "-b", "wt", wt)
	gittest.Run(t, repo, "worktree", "add", "-q", "-b", "other", other)

	// The server on a socket the profile doesn't make writable.
	env.Socket = filepath.Join(run, "tm.sock")
	env.Vars = append(env.Vars, "TERMINATR_SOCKET="+env.Socket)
	env.MustCLI("server", "start")
	// Before root goes, socket and all.
	t.Cleanup(func() { env.CLI("server", "stop") })

	// The thread's access policy (internal/server accessFor).
	common := filepath.Join(repo, ".git")
	access := agent.Access{Read: []string{proj},
		Write: []string{filepath.Join(common, "objects"), filepath.Join(common, "refs"), filepath.Join(common, "logs"),
			filepath.Join(common, "worktrees", "wt")},
		WriteFiles: []string{filepath.Join(common, "packed-refs"), filepath.Join(common, "packed-refs.lock")},
		NoWrite: []string{proj, filepath.Join(common, "hooks"), filepath.Join(common, "info"),
			filepath.Join(common, "worktrees", "other")},
		NoWriteFiles: []string{filepath.Join(common, "config"), filepath.Join(common, "config.lock")}}
	form, flags := sandboxArgs(t, agent.LaunchSpec{
		Role: agent.RoleThread, SessionID: "s-1", Cwd: wt, RepoRoot: repo,
		GitDir: filepath.Join(repo, ".git", "worktrees", "wt"), RuntimeDir: run,
		TMBin: env.Bin, Socket: env.Socket, Access: access,
		// localhost listed as tm's probe lists it (never a launch's): the
		// proxy takes it without the internet.
		Sandbox: agent.SandboxLists{Domains: []string{"example.com", "localhost"}},
	})
	args := append([]string{"sandbox", "-P", "tm", "-C", wt}, flags...)
	// A loopback listener: an address outside the thread's domains,
	// which its limited network refuses (T574 case 5).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	// Each probe prints its name and ok or no.
	probe := func(name, cmd string) string {
		return cmd + " >/dev/null 2>&1 && echo " + name + "=ok || echo " + name + "=no"
	}
	tmpdir := os.Getenv("TMPDIR")
	if tmpdir == "" {
		tmpdir = "/tmp"
	}
	script := strings.Join([]string{
		probe("worktree", "echo x > "+filepath.Join(wt, "f")),
		probe("commit", "git -c user.name=t -c user.email=t@t -C "+wt+" add f && git -c user.name=t -c user.email=t@t -C "+wt+" commit -qm f"),
		probe("push", "git -C "+wt+" push -q origin HEAD"),
		probe("hooks", "echo x > "+filepath.Join(common, "hooks", "pre-commit")),
		probe("config", "git -C "+wt+" config tm.probe x"),
		probe("info", "echo x >> "+filepath.Join(common, "info", "exclude")),
		probe("other-worktree", "echo x > "+filepath.Join(common, "worktrees", "other", "HEAD")),
		probe("ref-delete", "git -C "+wt+" branch -q gone && git -C "+wt+" branch -q -d gone"),
		probe("tmp", "echo x > /tmp/tmcxs-$$ && rm /tmp/tmcxs-$$"),
		probe("tmpdir", "echo x > "+filepath.Join(tmpdir, "tmcxs-$$")+" && rm "+filepath.Join(tmpdir, "tmcxs-$$")),
		probe("outside", "echo x > "+filepath.Join(outside, "f")),
		probe("home", "echo x > "+filepath.Join(root, "f")),
		probe("project-read", "cat "+filepath.Join(proj, "TASKS.md")),
		probe("project-write", "echo x >> "+filepath.Join(proj, "TASKS.md")),
		probe("socket", env.Bin+" server status"),
		probe("loopback", env.Bin+" codex-sandbox-probe "+env.Socket+" "+ln.Addr().String()+" | grep -q tcp=ok"),
		// Through Codex's proxy: a host outside the domains refused, a
		// listed one taken (security review, PR #654).
		probe("proxy", env.Bin+" codex-sandbox-probe "+env.Socket+" "+ln.Addr().String()+" --proxy localhost:"+port+" tm-probe.invalid:443 | grep -q proxy=ok"),
	}, "\n")
	cmd := exec.Command(bin, append(args, "--", "sh", "-c", script)...)
	cmd.Dir = wt
	// The npm codex is a node script: the user's PATH after tm's.
	cmd.Env = append(env.Vars, "PATH="+filepath.Dir(env.Bin)+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	t.Logf("codex %s\n%s", strings.Join(args, " "), out)
	if err != nil {
		t.Fatalf("codex sandbox: %v", err)
	}
	// Deleting a ref takes packed-refs.lock in the common dir. Seatbelt
	// grants it by path; bubblewrap binds only paths that exist, and the
	// lock never does until git takes it, so on Linux a thread can't
	// delete a ref (the guard refuses most deletes anyway).
	refDelete := "ok"
	if runtime.GOOS == "linux" {
		refDelete = "no"
	}
	want := map[string]string{
		"worktree": "ok", "commit": "ok", "push": "ok", "ref-delete": refDelete, "tmp": "ok", "tmpdir": "ok",
		"hooks": "no", "config": "no", "info": "no", "other-worktree": "no",
		"outside": "no", "home": "no", "project-read": "ok", "project-write": "no",
		"socket": "ok", "loopback": "no", "proxy": "ok",
	}
	if form == codex.NetworkOpen {
		// Linux and Windows: the open form threads had before (Codex's
		// Linux sandbox can't allow tm's socket with its proxy on; on
		// Windows its proxy needs the elevated sandbox): the socket and
		// loopback connect, no proxy.
		want["loopback"], want["proxy"] = "ok", "no"
	}
	got := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			got[k] = v
		}
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q, want %q", k, got[k], v)
		}
	}
	for _, f := range []string{filepath.Join(outside, "f"), filepath.Join(root, "f")} {
		if _, err := os.Stat(f); err == nil {
			t.Errorf("%s was written", f)
		}
	}
}
