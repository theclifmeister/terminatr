package codex

import (
	"net"
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
		{`echo "unix=ok tcp=refused"`, ""},
		{`echo "WARNING: something"; echo "unix=ok tcp=refused"`, ""},
		{`echo "unix=ok tcp=ok"`, "the network isn't limited"},
		{`echo "unix=refused tcp=refused"`, "the profile wasn't applied"},
		{"echo 'Error loading config.toml: unknown configuration field `features.network_proxy` in -c/--config override' >&2; exit 1", "codex sandbox refused it: Error loading config.toml: unknown configuration field"},
		{`echo "error: unexpected argument '-P' found" >&2; exit 2`, "codex sandbox refused it: error: unexpected argument '-P'"},
		{`echo something else`, "unexpected probe output"},
	}
	for _, tc := range cases {
		codex := filepath.Join(t.TempDir(), "codex")
		os.WriteFile(codex, []byte("#!/bin/sh\n"+tc.script+"\n"), 0o700)
		err := probeOnce(codex, "/bin/tm", t.TempDir(), "/run/tm.sock", "tm", []string{"-c", "features.network_proxy=true"}, false)
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

// TestWindowsCoordinator: on Windows a coordinator's profile turns the
// network off instead of limiting it to the proxy, and keeps tm's socket.
func TestWindowsCoordinator(t *testing.T) {
	prof := `permissions={tm={extends=":workspace",filesystem={"/p"="write"},network={enabled=true,mode="limited",unix_sockets={"/run/tm.sock"="allow"}}}}`
	argv := []string{"codex", "-c", `default_permissions="tm"`, "-c", "features.network_proxy=true", "--approve-for-me", "-c", prof, "-c", "hooks={}"}
	want := []string{"codex", "-c", `default_permissions="tm"`, "--approve-for-me",
		"-c", `permissions={tm={extends=":workspace",filesystem={"/p"="write"},network={enabled=false,unix_sockets={"/run/tm.sock"="allow"}}}}`,
		"-c", "hooks={}"}
	got := windowsCoordinator(argv)
	if !slices.Equal(got, want) {
		t.Errorf("windowsCoordinator\n%q\nwant\n%q", got, want)
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
	if got := windowsCoordinator([]string{"codex", "--approve-for-me"}); !slices.Equal(got, []string{"codex", "--approve-for-me"}) {
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
