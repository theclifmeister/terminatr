package codex

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/theclifmeister/terminatr/internal/agent"
)

// TestProbeVerdicts: what the sandbox run says becomes a reason to fall
// back, or nil, for each way a newer Codex could break the profile.
func TestProbeVerdicts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script stands in for codex")
	}
	cases := []struct {
		script, want string
	}{
		{`echo "unix=ok tcp=refused proxy=ok"`, ""},
		{`echo "WARNING: something"; echo "unix=ok tcp=refused proxy=ok"`, ""},
		{`echo "unix=ok tcp=ok proxy=ok"`, "the network isn't limited"},
		{`echo "unix=refused tcp=refused proxy=ok"`, "the profile wasn't applied"},
		// The direct refusal alone doesn't show the domains hold.
		{`echo "unix=ok tcp=refused"`, "unexpected probe output"},
		{`echo "unix=ok tcp=refused proxy=none"`, "no network proxy in the sandbox"},
		{`echo "unix=ok tcp=refused proxy=open"`, "lets a host outside the domains through"},
		{`echo "unix=ok tcp=refused proxy=closed"`, "refuses a listed domain"},
		{`echo "unix=ok tcp=refused proxy=bad"`, "didn't answer"},
		// The probe asks the proxy about a listed host it added and an
		// unlisted one.
		{`case "$*" in *'domains={"localhost"="allow"}'*'--proxy localhost:'*' tm-probe.invalid:443') echo "unix=ok tcp=refused proxy=ok";; *) echo "args: $*";; esac`, ""},
		{"echo 'Error loading config.toml: unknown configuration field `features.network_proxy` in -c/--config override' >&2; exit 1", "codex sandbox refused it: Error loading config.toml: unknown configuration field"},
		{`echo "error: unexpected argument '-P' found" >&2; exit 2`, "codex sandbox refused it: error: unexpected argument '-P'"},
		{`echo something else`, "unexpected probe output"},
	}
	for _, tc := range cases {
		codex := filepath.Join(t.TempDir(), "codex")
		os.WriteFile(codex, []byte("#!/bin/sh\n"+tc.script+"\n"), 0o700)
		err := probeOnce(codex, "/bin/tm", t.TempDir(), "/run/tm.sock", "tm", []string{"-c", "features.network_proxy=true", "-c", `permissions={tm={network={enabled=true,mode="full"}}}`}, false)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%s: %v, want %q", tc.script, err, tc.want)
		}
	}
	// On Windows the network can't be limited: the probe holds when tm's
	// socket connects, whatever TCP does.
	windows := []struct {
		script, want string
	}{
		{`echo "unix=ok tcp=ok"`, ""},
		{`echo "unix=ok tcp=refused"`, ""},
		{`echo "unix=refused tcp=ok"`, "the profile wasn't applied"},
		{`echo "error: bad" >&2; exit 2`, "codex sandbox refused it: error: bad"},
		{`echo something else`, "unexpected probe output"},
	}
	for _, tc := range windows {
		codex := filepath.Join(t.TempDir(), "codex")
		os.WriteFile(codex, []byte("#!/bin/sh\n"+tc.script+"\n"), 0o700)
		err := probeOnce(codex, "/bin/tm", t.TempDir(), "/run/tm.sock", "tm", []string{"-c", "permissions={tm={}}"}, true)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("windows %s: %v, want %q", tc.script, err, tc.want)
		}
	}
}

// TestThreadNetwork: a thread's network stays limited to its socket and
// domains when the probe holds, without the local binding in the probe
// (it reopens loopback); else it is closed, its domains and binding
// gone, with a warning. On Windows it is closed without a probe.
func TestThreadNetwork(t *testing.T) {
	prof := `permissions={tm={extends=":workspace",filesystem={"/w"="write"},network={enabled=true,mode="full",unix_sockets={"/run/tm.sock"="allow"},domains={"a.example"="allow","odd}\"{"="allow"},allow_local_binding=true}}}`
	argv := []string{"codex", "-a", "on-request", "-c", `default_permissions="tm"`, "-c", "features.network_proxy=true", "-c", prof, "-c", "hooks={}"}
	closed := []string{"codex", "-a", "on-request", "-c", `default_permissions="tm"`,
		"-c", `permissions={tm={extends=":workspace",filesystem={"/w"="write"},network={enabled=false,unix_sockets={"/run/tm.sock"="allow"}}}}`,
		"-c", "hooks={}"}
	if got := closedNetwork(argv); !slices.Equal(got, closed) {
		t.Errorf("closedNetwork\n%q\nwant\n%q", got, closed)
	}
	if _, err := toml.Decode(closed[6], &struct{}{}); err != nil {
		t.Errorf("closed profile: %v", err)
	}
	m := load(t).m
	spec := agent.LaunchSpec{Role: agent.RoleThread, Cwd: "/w", TMBin: "/bin/tm", Socket: "/run/tm.sock"}
	var probed []string
	verdict := error(nil)
	probeFunc = func(_ *agent.Manifest, _, _, _, _, _ string, flags []string, w bool) (string, error) {
		probed = flags
		return "0.170.0", verdict
	}
	defer func() { probeFunc = probeProfile }()
	l := limited(agent.Launch{Argv: argv}, spec, m, false)
	if !slices.Equal(l.Argv, argv) || len(l.Warnings) > 0 {
		t.Errorf("held: %q %q", l.Argv, l.Warnings)
	}
	if len(probed) != 4 || strings.Contains(probed[3], "allow_local_binding") || !strings.Contains(probed[3], `mode="full"`) {
		t.Errorf("probed %q", probed)
	}
	verdict = errors.New("the network isn't limited to tm's socket: features.network_proxy or network.mode was ignored")
	l = limited(agent.Launch{Argv: argv}, spec, m, false)
	if !slices.Equal(l.Argv, closed) || len(l.Warnings) != 1 || !strings.Contains(l.Warnings[0], "codex 0.170.0: the thread's limited network doesn't hold") {
		t.Errorf("not held: %q %q", l.Argv, l.Warnings)
	}
	probed = nil
	if l = limited(agent.Launch{Argv: argv}, spec, m, true); !slices.Equal(l.Argv, closed) || probed != nil || len(l.Warnings) > 0 {
		t.Errorf("windows: %q %q, probed %q", l.Argv, l.Warnings, probed)
	}
	if l = limited(agent.Launch{Argv: []string{"codex", "--dangerously-bypass-approvals-and-sandbox"}}, spec, m, false); len(l.Argv) != 2 || probed != nil {
		t.Errorf("yolo: %q, probed %q", l.Argv, probed)
	}
}

// TestClosedNetworkManifest: closedNetwork closes the network of the
// manifest's own thread and coordinator profiles, as rendered, and of a
// profile whose network it doesn't recognise: enabled false whatever it
// said, no mode, domains or binding, no proxy feature (security review,
// PR #654).
func TestClosedNetworkManifest(t *testing.T) {
	a := load(t)
	type netw struct {
		Enabled     *bool
		Mode        string
		Domains     map[string]string
		Binding     *bool             `toml:"allow_local_binding"`
		UnixSockets map[string]string `toml:"unix_sockets"`
	}
	check := func(what string, argv []string) {
		t.Helper()
		got := closedNetwork(argv)
		if slices.Contains(got, "features.network_proxy=true") {
			t.Errorf("%s: the proxy feature stays: %q", what, got)
		}
		i := slices.IndexFunc(got, func(s string) bool { return strings.HasPrefix(s, "permissions=") })
		if i < 0 {
			t.Fatalf("%s: no profile in %q", what, got)
		}
		var v struct {
			Permissions map[string]struct{ Network netw }
		}
		if _, err := toml.Decode(got[i], &v); err != nil {
			t.Fatalf("%s: %s: %v", what, got[i], err)
		}
		n := v.Permissions["tm"].Network
		if n.Enabled == nil || *n.Enabled || n.Mode != "" || n.Domains != nil || n.Binding != nil || n.UnixSockets["/run/tm.sock"] != "allow" {
			t.Errorf("%s: %s", what, got[i])
		}
	}
	thread, err := a.Agent.Launch(agent.LaunchSpec{Role: agent.RoleThread, Cwd: "/w", TMBin: "/bin/tm", Socket: "/run/tm.sock",
		Access: agent.Access{Write: []string{"/w"}}, Sandbox: agent.SandboxLists{Domains: []string{"github.com", "odd}{.example"}, LocalBinding: true}})
	if err != nil {
		t.Fatal(err)
	}
	check("thread", thread.Argv)
	coord, err := a.Agent.Launch(agent.LaunchSpec{Role: "coordinator", Cwd: "/p", TMBin: "/bin/tm", Socket: "/run/tm.sock", Access: agent.Access{Read: []string{"/p"}}})
	if err != nil {
		t.Fatal(err)
	}
	check("coordinator", coord.Argv)
	for _, odd := range []string{
		`permissions={tm={network={mode="other",enabled=true,unix_sockets={"/run/tm.sock"="allow"}}}}`,
		`permissions={tm={network={unix_sockets={"/run/tm.sock"="allow"},enabled=true}}}`,
		`permissions={tm={network={unix_sockets={"/run/tm.sock"="allow"}}}}`,
	} {
		check(odd, []string{"codex", "-c", "features.network_proxy=true", "-c", odd})
	}
}

// TestProbeDomain: the probe's profile lists probeListed beside the
// thread's domains, or as its only one.
func TestProbeDomain(t *testing.T) {
	for in, want := range map[string]string{
		`permissions={tm={network={enabled=true,mode="full",unix_sockets={"/s"="allow"},domains={"github.com"="allow"}}}}`: `permissions={tm={network={enabled=true,mode="full",unix_sockets={"/s"="allow"},domains={"localhost"="allow","github.com"="allow"}}}}`,
		`permissions={tm={network={enabled=true,mode="limited",unix_sockets={"/s"="allow"}}}}`:                             `permissions={tm={network={enabled=true,mode="limited",domains={"localhost"="allow"},unix_sockets={"/s"="allow"}}}}`,
	} {
		got := probeDomain([]string{"-c", "features.network_proxy=true", "-c", in})
		if got[3] != want || got[1] != "features.network_proxy=true" {
			t.Errorf("probeDomain(%s)\n= %s\nwant %s", in, got[3], want)
		}
		if _, err := toml.Decode(got[3], &struct{}{}); err != nil {
			t.Errorf("%s: %v", got[3], err)
		}
	}
}

// TestProxyVerdict: the proxy check against a stand-in proxy: it must
// refuse the unlisted host and take the listed one.
func TestProxyVerdict(t *testing.T) {
	serve := func(allow func(target string) bool) string {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Skip("no loopback listener here:", err)
		}
		t.Cleanup(func() { ln.Close() })
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				go func(c net.Conn) {
					defer c.Close()
					req, err := http.ReadRequest(bufio.NewReader(c))
					if err != nil {
						return
					}
					if allow(req.Host) {
						c.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))
					} else {
						c.Write([]byte("HTTP/1.1 403 Forbidden\r\nx-proxy-error: blocked-by-allowlist\r\nContent-Length: 0\r\n\r\n"))
					}
				}(c)
			}
		}()
		return "http://" + ln.Addr().String()
	}
	listed := func(h string) bool { return strings.HasPrefix(h, "localhost:") }
	cases := []struct {
		proxy string
		want  string
	}{
		{serve(listed), "ok"},
		{serve(func(string) bool { return true }), "open"},
		{serve(func(string) bool { return false }), "closed"},
		{"", "none"},
		{"http://127.0.0.1:1", "bad"},
	}
	for _, tc := range cases {
		if got := proxyVerdict(tc.proxy, "localhost:9", probeUnlisted); got != tc.want {
			t.Errorf("proxy %q: %s, want %s", tc.proxy, got, tc.want)
		}
	}
}

// TestWindowsCoordinator: on Windows a coordinator's profile turns the
// network off instead of limiting it to the proxy, and keeps tm's socket.
func TestWindowsCoordinator(t *testing.T) {
	prof := `permissions={tm={extends=":workspace",filesystem={"/p"="write"},network={enabled=true,mode="limited",unix_sockets={"/run/tm.sock"="allow"}}}}`
	argv := []string{"codex", "-c", `default_permissions="tm"`, "-c", "features.network_proxy=true", "--approve-for-me", "-c", prof, "-c", "hooks={}"}
	want := []string{"codex", "-c", `default_permissions="tm"`, "--approve-for-me",
		"-c", `permissions={tm={extends=":workspace",filesystem={"/p"="write"},network={enabled=false,unix_sockets={"/run/tm.sock"="allow"}}}}`,
		"-c", "hooks={}"}
	got := closedNetwork(argv)
	if !slices.Equal(got, want) {
		t.Errorf("closedNetwork\n%q\nwant\n%q", got, want)
	}
	var v struct {
		Permissions map[string]struct {
			Network struct {
				Enabled     bool
				Mode        string
				UnixSockets map[string]string `toml:"unix_sockets"`
			}
		}
	}
	if _, err := toml.Decode(got[5], &v); err != nil || v.Permissions["tm"].Network.Enabled || v.Permissions["tm"].Network.Mode != "" ||
		v.Permissions["tm"].Network.UnixSockets["/run/tm.sock"] != "allow" {
		t.Errorf("windows profile %s: %v %+v", got[5], err, v)
	}
	if got := closedNetwork([]string{"codex", "--approve-for-me"}); !slices.Equal(got, []string{"codex", "--approve-for-me"}) {
		t.Errorf("no profile: %q", got)
	}

	// The launch probes and keeps the Windows form.
	var probed []string
	var onWindows bool
	probeFunc = func(_ *agent.Manifest, _, _, _, _, _ string, flags []string, w bool) (string, error) {
		probed, onWindows = flags, w
		return "0.162.1", nil
	}
	defer func() { probeFunc = probeProfile }()
	l := sandboxed(agent.Launch{Argv: argv}, agent.LaunchSpec{Cwd: "/p", TMBin: "/bin/tm", Socket: "/run/tm.sock"}, nil, true)
	if !slices.Equal(l.Argv, want) || len(l.Warnings) > 0 || !onWindows || !slices.Equal(probed, []string{"-c", want[5]}) {
		t.Errorf("sandboxed on windows: %q %q, probed %q (windows %v)", l.Argv, l.Warnings, probed, onWindows)
	}
}

// TestProbeMain: the in-sandbox side reports each dial.
func TestProbeMain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets")
	}
	dir, _ := os.MkdirTemp("", "tmp-")
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	tcp, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := tcp.Addr().String()
	tcp.Close() // nothing listens there now
	r, w, _ := os.Pipe()
	stdout := os.Stdout
	os.Stdout = w
	code := ProbeMain([]string{sock, addr})
	os.Stdout = stdout
	w.Close()
	out := make([]byte, 100)
	n, _ := r.Read(out)
	if code != 0 || strings.TrimSpace(string(out[:n])) != "unix=ok tcp=refused" {
		t.Errorf("ProbeMain = %d %q", code, out[:n])
	}
	if ProbeMain(nil) != 2 {
		t.Error("no args accepted")
	}
}

// TestProfileFlags: the profile's flags are found, -P's name read.
func TestProfileFlags(t *testing.T) {
	argv := []string{"codex", "-c", "a=1", "-c", `default_permissions="tm"`, "--approve-for-me", "-c", "features.network_proxy=true", "-c", "permissions={tm={}}", "-m", "x"}
	idx, name, flags := profileFlags(argv)
	if name != "tm" || len(idx) != 6 || strings.Join(flags, " ") != "-c features.network_proxy=true -c permissions={tm={}}" {
		t.Errorf("idx %v name %q flags %q", idx, name, flags)
	}
	if idx, _, _ := profileFlags([]string{"codex", "-c", "a=1"}); idx != nil {
		t.Errorf("no profile: %v", idx)
	}
}

// TestProbeMainWrite: with a third argument the probe reports whether
// the write was denied, and removes the file when it wasn't.
func TestProbeMainWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets")
	}
	dir := t.TempDir()
	sock := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	probe := func(path string) string {
		r, w, _ := os.Pipe()
		stdout := os.Stdout
		os.Stdout = w
		code := ProbeMain([]string{sock, "127.0.0.1:1", path})
		os.Stdout = stdout
		w.Close()
		out := make([]byte, 100)
		n, _ := r.Read(out)
		if code != 0 {
			t.Errorf("code %d", code)
		}
		return strings.TrimSpace(string(out[:n]))
	}
	if got := probe(filepath.Join(dir, "f")); got != "unix=ok write=ok" {
		t.Errorf("writable = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "f")); err == nil {
		t.Error("the probe left its file")
	}
	if got := probe(filepath.Join(dir, "no", "such", "f")); got != "unix=ok write=denied" {
		t.Errorf("unwritable = %q", got)
	}
}

// fakeCodex puts a shell script named codex first on PATH: --version
// answers, and `sandbox` runs sandbox (counting its runs in the file
// the returned function reads).
func fakeSandboxCodex(t *testing.T, sandbox string) (runs func() int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a shell script stands in for codex")
	}
	dir := t.TempDir()
	count := filepath.Join(dir, "runs")
	script := "#!/bin/sh\n" +
		`case "$1" in --version) echo "codex-cli 0.162.1"; exit 0;; esac` + "\n" +
		`echo x >> "` + count + `"` + "\n" + sandbox + "\n"
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() int {
		b, _ := os.ReadFile(count)
		return strings.Count(string(b), "x")
	}
}

// TestProbeThreadSandbox: what a thread's sandbox runs say (tm doctor)
// becomes the version, or the reason it doesn't hold: the write probe
// (its last argument a path), then the network probe, whose host
// outside the thread's domains must be refused.
func TestProbeThreadSandbox(t *testing.T) {
	answers := func(write, tcp string) string {
		return `case "$*" in */probe) ` + write + `;; *) ` + tcp + `;; esac`
	}
	held := `echo "unix=ok tcp=refused proxy=ok"`
	cases := []struct {
		script, want string
	}{
		{answers(`echo "unix=ok write=denied"`, held), ""},
		{answers(`echo "WARNING: something"; echo "unix=ok write=denied"`, held), ""},
		{answers(`echo "unix=ok write=ok"`, held), "the sandbox doesn't apply"},
		{answers(`echo "unix=refused write=denied"`, held), "tm's socket is unreachable"},
		{`echo "error: bad" >&2; exit 2`, "codex sandbox refused it: error: bad"},
		{`echo something else`, "unexpected probe output"},
		{answers(`echo "unix=ok write=denied"`, `echo "unix=ok tcp=ok proxy=ok"`), "the network isn't limited"},
		{answers(`echo "unix=ok write=denied"`, `echo "unix=ok tcp=refused proxy=open"`), "outside the domains"},
	}
	a := load(t)
	for _, tc := range cases {
		fakeSandboxCodex(t, tc.script)
		version, err := a.ProbeThreadSandbox("/bin/tm")
		switch {
		case tc.want == "" && err != nil, tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: %v, want %q", tc.script, err, tc.want)
		case version != "0.162.1":
			t.Errorf("%s: version %q", tc.script, version)
		}
	}
}

// TestProbeProfileCache: a verdict is kept for a binary (path, size,
// mtime) and the same arguments, so a launch doesn't run the sandbox
// twice; another binary or another argument is probed again.
func TestProbeProfileCache(t *testing.T) {
	runs := fakeSandboxCodex(t, `echo "unix=ok tcp=refused proxy=ok"`)
	m := agent.ManifestOf(load(t))
	cwd := t.TempDir()
	probe := func(name string) (string, error) {
		return probeProfile(m, "codex", "/bin/tm", cwd, "/run/"+t.Name()+".sock", name, []string{"-c", "x=1"}, false)
	}
	name := "cache-" + filepath.Base(t.TempDir())
	for i := 0; i < 3; i++ {
		if v, err := probe(name); err != nil || v != "0.162.1" {
			t.Fatalf("probe %d: %q %v", i, v, err)
		}
	}
	if n := runs(); n != 1 {
		t.Errorf("%d sandbox runs for 3 identical probes, want 1", n)
	}
	if _, err := probe(name + "-other"); err != nil {
		t.Fatal(err)
	}
	if n := runs(); n != 2 {
		t.Errorf("%d sandbox runs after another profile, want 2", n)
	}
}
