package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/relay"
	"github.com/theclifmeister/terminatr/internal/update"
)

func TestRelayServeFlags(t *testing.T) {
	h := newHarness(t)
	h.expect(2, "usage: tm relay serve", human, "relay")
	h.expect(2, "--cert and --key go together", human, "relay", "serve", "--cert", "c.pem")
	h.expect(2, "or --plain", human, "relay", "serve")
	h.expect(2, "or --plain", human, "relay", "serve", "--plain", "--cert", "c", "--key", "k")
	h.expect(2, "--trusted-proxy nope", human, "relay", "serve", "--plain", "--trusted-proxy", "nope")
	h.expect(2, "--max-per-ip 0", human, "relay", "serve", "--plain", "--max-per-ip", "0")
	h.expect(1, "human-only", coord, "relay", "serve", "--plain")
	h.expect(1, "human-only", thr, "relay", "serve", "--plain")

	h.expect(2, "--state, --email", human, "relay", "serve", "--plain", "--state", "/x")
	h.expect(2, "or --plain", human, "relay", "serve", "--domain", "relay.example.net", "--cert", "c", "--key", "k")
	h.expect(2, "--domain https://relay.example.net", human, "relay", "serve", "--domain", "https://relay.example.net")
	h.expect(2, "--email x", human, "relay", "serve", "--domain", "relay.example.net", "--email", "x")

	var gotAddr, gotCert string
	var got relay.TLS
	defer func(old func(context.Context, string, *relay.Server, relay.TLS) error) { relayServe = old }(relayServe)
	relayServe = func(ctx context.Context, addr string, s *relay.Server, t relay.TLS) error {
		gotAddr, gotCert, got = addr, t.CertFile, t
		return nil
	}
	h.ok(human, "relay", "serve", "--plain", "--listen", "127.0.0.1:8080",
		"--trusted-proxy", "172.16.0.0/12", "--trusted-proxy", "10.1.2.3", "--max-conns", "50", "--bandwidth", "1.5")
	if gotAddr != "127.0.0.1:8080" || gotCert != "" {
		t.Fatalf("addr %q cert %q", gotAddr, gotCert)
	}
	h.ok(human, "relay", "serve", "--cert", "c.pem", "--key", "k.pem")
	if gotAddr != ":443" || gotCert != "c.pem" {
		t.Fatalf("defaults: addr %q cert %q", gotAddr, gotCert)
	}
	h.ok(human, "relay", "serve", "--domain", "relay.example.net", "--state", "/var/lib/x", "--email", "me@example.net")
	if got.Domain != "relay.example.net" || got.CacheDir != "/var/lib/x" || got.Email != "me@example.net" || got.CertFile != "" {
		t.Fatalf("--domain: %+v", got)
	}
	// Without --state: systemd's StateDirectory, else under the home.
	h.env["STATE_DIRECTORY"] = "/var/lib/tm-relay"
	h.ok(human, "relay", "serve", "--domain", "relay.example.net")
	if got.CacheDir != "/var/lib/tm-relay" {
		t.Fatalf("STATE_DIRECTORY: %+v", got)
	}
	delete(h.env, "STATE_DIRECTORY")
	h.ok(human, "relay", "serve", "--domain", "relay.example.net")
	if got.CacheDir != filepath.Join(h.root, "relay") {
		t.Fatalf("default state: %+v", got)
	}
}

// The Dockerfile, the compose example and the systemd unit run tm relay
// serve with flags it takes.
func TestRelayDeployFilesUseRealFlags(t *testing.T) {
	h := newHarness(t)
	defer func(old func(context.Context, string, *relay.Server, relay.TLS) error) { relayServe = old }(relayServe)
	relayServe = func(context.Context, string, *relay.Server, relay.TLS) error { return nil }
	dir := filepath.Join("..", "..", "deploy", "relay")
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	jsonLine := func(text, prefix string) []string {
		for _, l := range strings.Split(text, "\n") {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(l), prefix); ok {
				var args []string
				if err := json.Unmarshal([]byte(rest), &args); err != nil {
					t.Fatalf("%s: %v", l, err)
				}
				return args
			}
		}
		t.Fatalf("no %q line", prefix)
		return nil
	}
	runs := map[string][]string{
		"Dockerfile":        jsonLine(read("Dockerfile"), "CMD "),
		"compose.acme.yaml": jsonLine(read("compose.acme.yaml"), "command: "),
	}
	for _, l := range strings.Split(read("tm-relay.service"), "\n") {
		if rest, ok := strings.CutPrefix(l, "ExecStart=/usr/local/bin/tm relay serve "); ok {
			runs["tm-relay.service"] = strings.Fields(rest)
		}
	}
	if len(runs["tm-relay.service"]) == 0 {
		t.Fatal("tm-relay.service: no ExecStart running tm relay serve")
	}
	for name, args := range runs {
		code, out, errs := h.run(human, "", append([]string{"relay", "serve"}, args...)...)
		if code != 0 {
			t.Errorf("%s: tm relay serve %v: exit %d %s%s", name, args, code, out, errs)
		}
	}
}

// The unit tm relay install writes is the one in deploy/relay, for
// another name.
func TestRelayUnitIsTheDeployFile(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "deploy", "relay", "tm-relay.service"))
	if err != nil {
		t.Fatal(err)
	}
	if want := relayUnit("relay.example.net", ""); string(b) != want {
		t.Fatalf("deploy/relay/tm-relay.service differs from relayUnit; write it as:\n%s", want)
	}
	for _, want := range []string{"DynamicUser=yes", "AmbientCapabilities=CAP_NET_BIND_SERVICE", "StateDirectory=tm-relay", "ProtectSystem=strict"} {
		if !strings.Contains(string(b), "\n"+want+"\n") {
			t.Errorf("no %s", want)
		}
	}
	if strings.Contains(string(b), "LoadCredential") {
		t.Error("LoadCredential in the ACME unit")
	}
	if d := unitDomain(relayUnit("r.example.org", "me@example.org")); d != "r.example.org" {
		t.Errorf("unitDomain %q", d)
	}
}

type relayRig struct {
	s     *relayService
	calls []string
	// health answers each /healthz request in turn; the last repeats.
	health []error
	asked  []string
}

func newRelayRig(t *testing.T) *relayRig {
	t.Helper()
	root := t.TempDir()
	exe := filepath.Join(t.TempDir(), "tm")
	os.WriteFile(exe, []byte("tm v0.27.0"), 0o755)
	r := &relayRig{}
	r.s = &relayService{
		Root: root, Check: func() error { return nil }, IsRoot: func() bool { return true },
		Exe: exe, Version: "v0.27.0", GOARCH: "amd64",
		Systemctl: func(args ...string) error { r.calls = append(r.calls, strings.Join(args, " ")); return nil },
		Health: func(_ context.Context, url string) error {
			r.asked = append(r.asked, url)
			if len(r.health) == 0 {
				return nil
			}
			err := r.health[0]
			if len(r.health) > 1 {
				r.health = r.health[1:]
			}
			return err
		},
		Lookup: func(string) ([]string, error) { return []string{"203.0.113.7"}, nil },
	}
	old := newRelayService
	newRelayService = func(*Env) *relayService { return r.s }
	t.Cleanup(func() { newRelayService = old })
	return r
}

func TestRelayInstall(t *testing.T) {
	h := newHarness(t)
	r := newRelayRig(t)
	h.expect(2, "--domain NAME", human, "relay", "install")
	h.expect(2, "--domain relay.example.net:443", human, "relay", "install", "--domain", "relay.example.net:443")
	h.expect(2, "--email", human, "relay", "install", "--domain", "relay.example.net", "--email", "x\nExecStartPre=/bin/sh")
	h.expect(1, "human-only", coord, "relay", "install", "--domain", "relay.example.net")
	r.s.IsRoot = func() bool { return false }
	h.expect(3, "run it as root (sudo tm relay install --domain relay.example.net)", human, "relay", "install", "--domain", "relay.example.net")
	r.s.IsRoot = func() bool { return true }
	r.s.Check = func() error { return errors.New("the relay service is for Linux with systemd, and this is darwin") }
	h.expect(3, "install: the relay service is for Linux with systemd", human, "relay", "install", "--domain", "relay.example.net")
	r.s.Check = func() error { return nil }
	if len(r.calls) != 0 {
		t.Fatalf("refusals ran systemctl: %v", r.calls)
	}

	out := h.ok(human, "relay", "install", "--domain", "Relay.Example.NET", "--email", "me@example.net")
	unit, err := os.ReadFile(filepath.Join(r.s.Root, "etc", "systemd", "system", "tm-relay.service"))
	if err != nil {
		t.Fatal(err)
	}
	if string(unit) != relayUnit("relay.example.net", "me@example.net") ||
		!strings.Contains(string(unit), "--domain relay.example.net --state ${STATE_DIRECTORY} --email me@example.net --update-request ${STATE_DIRECTORY}/update-request\n") {
		t.Fatalf("unit:\n%s", unit)
	}
	if b, _ := os.ReadFile(filepath.Join(r.s.Root, "usr", "local", "bin", "tm")); string(b) != "tm v0.27.0" {
		t.Fatalf("binary %q", b)
	}
	if fi, _ := os.Stat(filepath.Join(r.s.Root, "usr", "local", "bin", "tm")); fi.Mode().Perm() != 0o755 {
		t.Fatalf("binary mode %v", fi.Mode())
	}
	if got := strings.Join(r.calls, "; "); got != "daemon-reload; enable tm-relay; restart tm-relay; enable --now tm-relay-update.path; enable --now tm-relay-update.timer" {
		t.Fatalf("systemctl: %s", got)
	}
	// The update units: a daily timer and a path unit on the relay's
	// request file, both starting tm relay update as root.
	for name, want := range map[string]string{
		"tm-relay-update.service": relayUpdateServiceUnit,
		"tm-relay-update.timer":   relayUpdateTimerUnit,
		"tm-relay-update.path":    relayUpdatePathUnit,
	} {
		b, err := os.ReadFile(filepath.Join(r.s.Root, "etc", "systemd", "system", name))
		if err != nil || string(b) != want {
			t.Fatalf("%s: %v\n%s", name, err, b)
		}
	}
	for _, want := range []string{"\nType=oneshot\n", "\nExecStart=/usr/local/bin/tm relay update\n"} {
		if !strings.Contains(relayUpdateServiceUnit, want) || strings.Contains(relayUpdateServiceUnit, "DynamicUser") {
			t.Fatalf("tm-relay-update.service: no %q, or not root", want)
		}
	}
	if !strings.Contains(relayUpdateTimerUnit, "\nOnCalendar=daily\n") || !strings.Contains(relayUpdatePathUnit, "\nPathExists=/var/lib/private/tm-relay/update-request\n") {
		t.Fatal("timer or path unit")
	}
	if !strings.Contains(out, "Auto-update: on") {
		t.Fatalf("out: %s", out)
	}
	if len(r.asked) != 1 || r.asked[0] != "https://relay.example.net/healthz" {
		t.Fatalf("healthz: %v", r.asked)
	}
	if !strings.Contains(out, "tm peer pair --relay wss://relay.example.net") {
		t.Fatalf("out: %s", out)
	}

	// Unreachable: the relay is up, so it names the port and the DNS,
	// and changes no firewall.
	r.calls, r.asked = nil, nil
	r.health = []error{errors.New("dial tcp 203.0.113.7:443: i/o timeout")}
	code, _, errs := h.run(human, "", "relay", "install", "--domain", "relay.example.net")
	if code != 3 || !strings.Contains(errs, "open port 443") || !strings.Contains(errs, "resolves to 203.0.113.7") || !strings.Contains(errs, "i/o timeout") {
		t.Fatalf("unreachable: %d %s", code, errs)
	}
	for _, c := range r.calls {
		if strings.Contains(c, "ufw") || strings.Contains(c, "firewall") {
			t.Fatalf("touched the firewall: %v", r.calls)
		}
	}
	// It asked again before giving up (a first certificate takes time).
	r.s.Wait, r.s.Every = 50*time.Millisecond, time.Millisecond
	r.asked = nil
	r.health = []error{errors.New("tls: internal error"), nil}
	h.ok(human, "relay", "install", "--domain", "relay.example.net")
	if len(r.asked) != 2 {
		t.Fatalf("asked %d times", len(r.asked))
	}
	// The service died: the journal, not the firewall.
	r.health = []error{errors.New("connection refused")}
	r.s.Wait = 0
	r.s.Systemctl = func(args ...string) error {
		if args[0] == "is-active" {
			return errors.New("inactive")
		}
		return nil
	}
	h.expect(3, "journalctl -u tm-relay", human, "relay", "install", "--domain", "relay.example.net")

	// Uninstall stops it, removes the unit and keeps the binary.
	r.calls = nil
	r.s.Systemctl = func(args ...string) error { r.calls = append(r.calls, strings.Join(args, " ")); return nil }
	h.ok(human, "relay", "uninstall")
	if _, err := os.Stat(filepath.Join(r.s.Root, "etc", "systemd", "system", "tm-relay.service")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unit kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.s.Root, "usr", "local", "bin", "tm")); err != nil {
		t.Fatalf("binary removed: %v", err)
	}
	if got := strings.Join(r.calls, "; "); got != "disable --now tm-relay; disable --now tm-relay-update.timer; disable --now tm-relay-update.path; daemon-reload" {
		t.Fatalf("systemctl: %s", got)
	}
	if entries, _ := os.ReadDir(filepath.Join(r.s.Root, "etc", "systemd", "system")); len(entries) != 0 {
		t.Fatalf("units kept: %v", entries)
	}
	if out := h.ok(human, "relay", "uninstall"); !strings.Contains(out, "isn't installed") {
		t.Fatalf("again: %s", out)
	}
}

// relayRelease serves release tag with a tm archive for linux/amd64 and
// a checksums.txt that is right unless bad.
func relayRelease(t *testing.T, s *relayService, tag string, bad bool) {
	t.Helper()
	body := "tm " + tag
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	tw.WriteHeader(&tar.Header{Name: "tm", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
	tw.Write([]byte(body))
	tw.Close()
	zw.Close()
	archive := buf.Bytes()
	name := update.ArchiveName("linux", "amd64")
	sum := sha256.Sum256(archive)
	if bad {
		sum[0] ^= 1
	}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			fmt.Fprintf(w, `{"tag_name":%q,"assets":[{"name":%q,"browser_download_url":"%s/a"},{"name":"checksums.txt","browser_download_url":"%s/sums"}]}`,
				tag, name, srv.URL, srv.URL)
		case "/a":
			w.Write(archive)
		case "/sums":
			fmt.Fprintf(w, "%x  %s\n", sum, name)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	s.Client = update.NewClient(func(k string) string {
		if k == update.EnvAPI {
			return srv.URL
		}
		return ""
	})
}

func TestRelayUpdate(t *testing.T) {
	h := newHarness(t)
	r := newRelayRig(t)
	bin := filepath.Join(r.s.Root, "usr", "local", "bin", "tm")
	h.ok(human, "relay", "install", "--domain", "relay.example.net")
	h.expect(2, "usage", human, "relay", "update", "--domain", "x.example.net")

	// A checksum that doesn't match changes nothing.
	relayRelease(t, r.s, "v0.28.0", true)
	r.calls, r.asked = nil, nil
	h.expect(3, "does not match checksums.txt", human, "relay", "update")
	if b, _ := os.ReadFile(bin); string(b) != "tm v0.27.0" {
		t.Fatalf("binary replaced: %q", b)
	}
	if len(r.calls) != 0 {
		t.Fatalf("restarted after a bad download: %v", r.calls)
	}

	relayRelease(t, r.s, "v0.28.0", false)
	out := h.ok(human, "relay", "update")
	if b, _ := os.ReadFile(bin); string(b) != "tm v0.28.0" {
		t.Fatalf("binary %q", b)
	}
	if got := strings.Join(r.calls, "; "); got != "restart tm-relay" {
		t.Fatalf("systemctl: %s", got)
	}
	if len(r.asked) != 1 || r.asked[0] != "https://relay.example.net/healthz" || !strings.Contains(out, "v0.28.0") {
		t.Fatalf("healthz %v; out %s", r.asked, out)
	}
	if entries, _ := os.ReadDir(filepath.Dir(bin)); len(entries) != 1 {
		t.Errorf("leftovers next to tm: %v", entries)
	}

	// Already the latest: nothing to do.
	r.s.Version, r.calls = "v0.28.0", nil
	if out := h.ok(human, "relay", "update"); !strings.Contains(out, "nothing to update") || len(r.calls) != 0 {
		t.Fatalf("latest: %v %s", r.calls, out)
	}
}

// --self-signed makes a key and a certificate in the state directory,
// keeps the key across runs and logs the address with its pin.
func TestRelayServeSelfSigned(t *testing.T) {
	h := newHarness(t)
	h.expect(2, "--ip goes with --self-signed", human, "relay", "serve", "--plain", "--ip", "203.0.113.5")
	h.expect(2, "--ip nope", human, "relay", "serve", "--self-signed", "--ip", "nope")
	h.expect(2, "or --plain", human, "relay", "serve", "--self-signed", "--domain", "relay.example.net")
	var got relay.TLS
	defer func(old func(context.Context, string, *relay.Server, relay.TLS) error) { relayServe = old }(relayServe)
	relayServe = func(ctx context.Context, addr string, s *relay.Server, t relay.TLS) error { got = t; return nil }
	state := filepath.Join(t.TempDir(), "state")
	_, _, errs := h.run(human, "", "relay", "serve", "--self-signed", "--ip", "203.0.113.5", "--state", state)
	pin, err := relay.SelfSignedPin(state)
	if err != nil {
		t.Fatal(err)
	}
	if got.CertFile != filepath.Join(state, relay.SelfSignedCert) || got.KeyFile != filepath.Join(state, relay.SelfSignedKey) || got.Domain != "" {
		t.Fatalf("TLS %+v", got)
	}
	if !strings.Contains(errs, "wss://203.0.113.5#sha256="+pin) {
		t.Fatalf("no address with the pin: %s", errs)
	}
	// Another address, another port: the same key, so the same pin.
	_, _, errs = h.run(human, "", "relay", "serve", "--self-signed", "--listen", "192.168.1.20:8443", "--state", state)
	if !strings.Contains(errs, "wss://192.168.1.20:8443#sha256="+pin) || !strings.Contains(errs, "private address") {
		t.Fatalf("second run: %s", errs)
	}
	// No --ip and no address to listen on: this host's own.
	defer func(old func() (netip.Addr, error)) { localIP = old }(localIP)
	localIP = func() (netip.Addr, error) { return netip.MustParseAddr("2001:db8::7"), nil }
	h.env["STATE_DIRECTORY"] = state
	_, _, errs = h.run(human, "", "relay", "serve", "--self-signed")
	if !strings.Contains(errs, "wss://[2001:db8::7]#sha256="+pin) {
		t.Fatalf("detected: %s", errs)
	}
}

func TestRelayInstallIP(t *testing.T) {
	h := newHarness(t)
	r := newRelayRig(t)
	h.expect(2, "not both", human, "relay", "install", "--ip", "--domain", "relay.example.net")
	h.expect(2, "--ip relay.example.net", human, "relay", "install", "--ip", "relay.example.net")
	h.expect(2, "usage", human, "relay", "install", "203.0.113.5")
	h.expect(2, "usage", human, "relay", "update", "--ip")

	out := h.ok(human, "relay", "install", "--ip", "203.0.113.5")
	unit, err := os.ReadFile(filepath.Join(r.s.Root, "etc", "systemd", "system", "tm-relay.service"))
	if err != nil {
		t.Fatal(err)
	}
	if string(unit) != relayIPUnit("203.0.113.5") || !strings.Contains(string(unit), "--self-signed --ip 203.0.113.5 --state ${STATE_DIRECTORY} --update-request ${STATE_DIRECTORY}/update-request\n") {
		t.Fatalf("unit:\n%s", unit)
	}
	state := filepath.Join(r.s.Root, "var", "lib", "private", "tm-relay")
	pin, err := relay.SelfSignedPin(state)
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(filepath.Join(state, relay.SelfSignedKey)); fi.Mode().Perm() != 0o600 {
		t.Fatalf("key mode %v", fi.Mode())
	}
	address := "wss://203.0.113.5#sha256=" + pin
	if len(r.asked) != 1 || r.asked[0] != "https://203.0.113.5/healthz#sha256="+pin {
		t.Fatalf("healthz: %v", r.asked)
	}
	if !strings.Contains(out, "tm peer pair --relay "+address) || !strings.Contains(out, "Back up /var/lib/private/tm-relay") || strings.Contains(out, "this host's own") {
		t.Fatalf("out: %s", out)
	}
	// The unit runs a serve that takes its flags.
	defer func(old func(context.Context, string, *relay.Server, relay.TLS) error) { relayServe = old }(relayServe)
	relayServe = func(context.Context, string, *relay.Server, relay.TLS) error { return nil }
	args := strings.Fields(strings.ReplaceAll(unitExec(t, string(unit)), "${STATE_DIRECTORY}", state))
	h.ok(human, args...)
	if p, _ := relay.SelfSignedPin(state); p != pin {
		t.Fatal("serve changed the key")
	}

	// Detected, private: says so, and installing again keeps the key.
	defer func(old func() (netip.Addr, error)) { localIP = old }(localIP)
	localIP = func() (netip.Addr, error) { return netip.MustParseAddr("192.168.1.20"), nil }
	out = h.ok(human, "relay", "install", "--ip")
	if !strings.Contains(out, "wss://192.168.1.20#sha256="+pin) || !strings.Contains(out, "this host's own") || !strings.Contains(out, "private address") {
		t.Fatalf("detected: %s", out)
	}

	// Update restarts it and checks it serves the same key.
	relayRelease(t, r.s, "v0.28.0", false)
	r.asked = nil
	out = h.ok(human, "relay", "update")
	if len(r.asked) != 1 || r.asked[0] != "https://192.168.1.20/healthz#sha256="+pin || !strings.Contains(out, "same key: wss://192.168.1.20#sha256="+pin) {
		t.Fatalf("update: %v %s", r.asked, out)
	}
	// Something else answering there: said so.
	r.health = []error{&relay.PinError{Host: "192.168.1.20", Want: pin, Got: strings.Repeat("0", 64)}}
	r.s.Wait = 0
	h.expect(3, "another key than tm-relay's", human, "relay", "install", "--ip=192.168.1.20")
}

// unitExec is the tm command line of a unit's ExecStart.
func unitExec(t *testing.T, unit string) string {
	t.Helper()
	for _, l := range strings.Split(unit, "\n") {
		if rest, ok := strings.CutPrefix(l, "ExecStart="+relayBin+" "); ok {
			return rest
		}
	}
	t.Fatalf("no ExecStart in\n%s", unit)
	return ""
}

// relayHealth takes a pinned relay by its key alone.
func TestRelayHealthPinned(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ok") }))
	defer srv.Close()
	pin := relay.CertPin(srv.Certificate())
	if err := relayHealth(context.Background(), srv.URL+"/healthz"); err == nil {
		t.Fatal("trusted a self-signed certificate without a pin")
	}
	if err := relayHealth(context.Background(), srv.URL+"/healthz#sha256="+pin); err != nil {
		t.Fatal(err)
	}
	var pe *relay.PinError
	if err := relayHealth(context.Background(), srv.URL+"/healthz#sha256="+strings.Repeat("ab", 32)); !errors.As(err, &pe) {
		t.Fatalf("wrong pin: %v", err)
	}
}

// --auto-update=off writes the timer but leaves it disabled; the path
// unit is on either way. Only install takes the flag.
func TestRelayInstallAutoUpdateOff(t *testing.T) {
	h := newHarness(t)
	r := newRelayRig(t)
	h.expect(2, "--auto-update=on or --auto-update=off", human, "relay", "install", "--domain", "relay.example.net", "--auto-update=maybe")
	h.expect(2, "usage", human, "relay", "update", "--auto-update=off")
	out := h.ok(human, "relay", "install", "--ip", "203.0.113.5", "--auto-update=off")
	if got := strings.Join(r.calls, "; "); got != "daemon-reload; enable tm-relay; restart tm-relay; enable --now tm-relay-update.path; disable --now tm-relay-update.timer" {
		t.Fatalf("systemctl: %s", got)
	}
	if !strings.Contains(out, "Auto-update: off") {
		t.Fatalf("out: %s", out)
	}
	if _, err := os.Stat(filepath.Join(r.s.Root, "etc", "systemd", "system", "tm-relay-update.timer")); err != nil {
		t.Fatal(err)
	}
	r.calls = nil
	h.ok(human, "relay", "install", "--ip", "203.0.113.5", "--auto-update", "on")
	if !strings.HasSuffix(strings.Join(r.calls, "; "), "enable --now tm-relay-update.timer") {
		t.Fatalf("systemctl: %v", r.calls)
	}
}

// tm relay update removes a linked server's request before anything
// else, so a failing update isn't started again and again.
func TestRelayUpdateTakesTheRequest(t *testing.T) {
	h := newHarness(t)
	r := newRelayRig(t)
	h.ok(human, "relay", "install", "--domain", "relay.example.net")
	req := filepath.Join(r.s.Root, "var", "lib", "private", "tm-relay", "update-request")
	os.MkdirAll(filepath.Dir(req), 0o700)
	os.WriteFile(req, []byte("update to the latest release\n"), 0o600)
	relayRelease(t, r.s, "v0.28.0", true)
	h.expect(3, "does not match checksums.txt", human, "relay", "update")
	if _, err := os.Stat(req); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("request kept: %v", err)
	}
	os.WriteFile(req, nil, 0o600)
	relayRelease(t, r.s, "v0.27.0", false)
	if out := h.ok(human, "relay", "update"); !strings.Contains(out, "A linked server asked") || !strings.Contains(out, "nothing to update") {
		t.Fatalf("out: %s", out)
	}
}

// The update units in deploy/relay are the ones tm relay install writes.
func TestRelayUpdateUnitsAreTheDeployFiles(t *testing.T) {
	for name, want := range map[string]string{
		"tm-relay-update.service": relayUpdateServiceUnit,
		"tm-relay-update.timer":   relayUpdateTimerUnit,
		"tm-relay-update.path":    relayUpdatePathUnit,
	} {
		b, err := os.ReadFile(filepath.Join("..", "..", "deploy", "relay", name))
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != want {
			t.Errorf("deploy/relay/%s differs from tm relay install's; write it as:\n%s", name, want)
		}
	}
}
