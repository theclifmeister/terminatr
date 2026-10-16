package modbroker

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/plat/caps"
)

// testBroker is a Broker over the fixture with one mod, m, whose
// manifest asks for every permission; grants are what it runs with.
type testBroker struct {
	*Broker
	f     fixture
	m     *mod.Mod
	mu    sync.Mutex
	audit []string
}

func newTestBroker(t *testing.T, grants ...string) *testBroker {
	f := newFixture(t)
	man := &mod.Manifest{ID: "m", Permissions: mod.Permissions{
		ProjectRead: true, Inbox: true, Net: []string{"example.com"}, Commands: []string{"env", "sleep", "echo", "git"},
		Files: mod.Files{Read: []string{"~/notes"}, Write: []string{"~/out"}},
	}}
	tb := &testBroker{f: f, m: &mod.Mod{ID: "m", State: mod.Enabled, Manifest: man, Grants: grants,
		Settings: map[string]any{"warn_after_hours": int64(24)}}}
	tb.Broker = New(Options{
		Mod: func(id string) (mod.Mod, bool) {
			if id != "m" {
				return mod.Mod{}, false
			}
			return *tb.m, true
		},
		Home: f.home, TMHome: f.p.TMHome, StateDir: filepath.Dir(f.p.Own), Secrets: f.p.Secrets,
		Audit: func(id, line string) {
			tb.mu.Lock()
			defer tb.mu.Unlock()
			tb.audit = append(tb.audit, id+" "+line)
		},
	})
	return tb
}

func (tb *testBroker) call(t *testing.T, method string, params any) (any, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	return tb.Call(context.Background(), "m", method, raw)
}

func (tb *testBroker) audited(s string) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	for _, l := range tb.audit {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}

func code(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return 0
}

func TestCallRefusals(t *testing.T) {
	tb := newTestBroker(t)
	if _, err := tb.Call(context.Background(), "nope", "settings", nil); code(err) != CodeDenied {
		t.Errorf("an unknown mod: %v", err)
	}
	if _, err := tb.call(t, "no.such", nil); code(err) != CodeNoMethod {
		t.Errorf("an unknown method: %v", err)
	}
	if _, err := tb.call(t, "http.fetch", map[string]string{"url": "https://example.com/"}); code(err) != CodeDenied {
		t.Errorf("fetch without net: %v", err)
	}
	if _, err := tb.call(t, "process.run", map[string]any{"argv": []string{"env"}}); code(err) != CodeDenied {
		t.Errorf("run without commands: %v", err)
	}
	if !tb.audited("m broker: denied process.run") {
		t.Errorf("refusal not audited: %q", tb.audit)
	}
	tb.m.State = mod.Disabled
	if _, err := tb.call(t, "settings", nil); code(err) != CodeDenied {
		t.Errorf("a disabled mod: %v", err)
	}
}

func TestSettingsAndStore(t *testing.T) {
	tb := newTestBroker(t)
	got, err := tb.call(t, "settings", nil)
	if err != nil || got.(map[string]any)["warn_after_hours"] != int64(24) {
		t.Fatalf("settings = %v, %v", got, err)
	}
	if _, err := tb.call(t, "store.set", map[string]any{"key": "seen", "value": []int{1, 2}}); err != nil {
		t.Fatal(err)
	}
	got, err = tb.call(t, "store.get", map[string]string{"key": "seen"})
	if err != nil || string(got.(json.RawMessage)) != "[1,2]" {
		t.Fatalf("store.get = %s, %v", got, err)
	}
	if _, err := tb.call(t, "store.del", map[string]string{"key": "seen"}); err != nil {
		t.Fatal(err)
	}
	if got, err := tb.call(t, "store.get", map[string]string{"key": "seen"}); err != nil || got != nil {
		t.Fatalf("store.get after del = %v, %v", got, err)
	}
	big := strings.Repeat("x", maxStore)
	if _, err := tb.call(t, "store.set", map[string]any{"key": "big", "value": big}); code(err) != CodeDenied {
		t.Fatalf("an over-size store: %v", err)
	}
	if _, err := tb.call(t, "store.get", map[string]string{"key": ""}); code(err) != CodeBadParams {
		t.Fatalf("an empty key: %v", err)
	}
}

// The parsed store is kept between calls, and a file changed behind the
// broker's back is read again.
func TestStoreCache(t *testing.T) {
	tb := newTestBroker(t)
	if _, err := tb.call(t, "store.set", map[string]any{"key": "a", "value": 1}); err != nil {
		t.Fatal(err)
	}
	if got, _ := tb.call(t, "store.get", map[string]string{"key": "a"}); string(got.(json.RawMessage)) != "1" {
		t.Fatalf("store.get = %v", got)
	}
	// A refused edit leaves the cached store as it was.
	big := strings.Repeat("x", maxStore)
	if _, err := tb.call(t, "store.set", map[string]any{"key": "big", "value": big}); code(err) != CodeDenied {
		t.Fatalf("an over-size store: %v", err)
	}
	if got, _ := tb.call(t, "store.get", map[string]string{"key": "big"}); got != nil {
		t.Fatalf("a refused value was kept: %v", got)
	}
	path := filepath.Join(tb.OwnDir("m"), storeFile)
	if err := os.WriteFile(path, []byte(`{"a":2,"pad":"xxxxxxxxxx"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := tb.call(t, "store.get", map[string]string{"key": "a"}); string(got.(json.RawMessage)) != "2" {
		t.Fatalf("store.get after an outside edit = %v", got)
	}
}

// The broker's own files in the mod's folder are not written through fs.write.
func TestFsWriteReserved(t *testing.T) {
	tb := newTestBroker(t)
	for _, name := range []string{"store.json", "annotations.json", "./store.json"} {
		if _, err := tb.call(t, "fs.write", map[string]any{"path": name, "text": "{}"}); code(err) != CodeDenied {
			t.Errorf("fs.write %s: %v", name, err)
		}
	}
	if _, err := tb.call(t, "fs.write", map[string]any{"path": "sub/store.json", "text": "{}"}); err != nil {
		t.Errorf("a store.json in a subfolder: %v", err)
	}
}

func TestFiles(t *testing.T) {
	tb := newTestBroker(t)
	// Its own folder needs no grant.
	if _, err := tb.call(t, "fs.write", map[string]any{"path": "cache/x.json", "text": "{}"}); err != nil {
		t.Fatal(err)
	}
	if got, err := tb.call(t, "fs.read", map[string]string{"path": "cache/x.json"}); err != nil || got != "{}" {
		t.Fatalf("fs.read own = %v, %v", got, err)
	}
	if _, err := tb.call(t, "fs.read", map[string]string{"path": "~/notes/a.txt"}); code(err) != CodeDenied {
		t.Fatalf("a root without the files grant: %v", err)
	}
	tb.m.Grants = []string{mod.PermFiles}
	if got, err := tb.call(t, "fs.read", map[string]string{"path": "~/notes/a.txt"}); err != nil || got != "a" {
		t.Fatalf("fs.read granted = %v, %v", got, err)
	}
	if _, err := tb.call(t, "fs.read", map[string]string{"path": "~/notes/link-ssh/id_rsa"}); code(err) != CodeDenied {
		t.Fatalf("a secret through a link: %v", err)
	}
	if _, err := tb.call(t, "fs.write", map[string]any{"path": "~/notes/a.txt", "text": "b"}); code(err) != CodeDenied {
		t.Fatalf("writing a read root: %v", err)
	}
	if _, err := tb.call(t, "fs.write", map[string]any{"path": "~/out/log.txt", "text": "1\n"}); err != nil {
		t.Fatal(err)
	}
	if _, err := tb.call(t, "fs.write", map[string]any{"path": "~/out/log.txt", "text": "2\n", "append": true}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(tb.f.home, "out", "log.txt")); string(b) != "1\n2\n" {
		t.Fatalf("out/log.txt = %q", b)
	}
	if _, err := tb.call(t, "fs.write", map[string]any{"path": "~/out/no/such/x", "text": ""}); code(err) != CodeNotFound {
		t.Fatalf("a missing folder outside its own: %v", err)
	}
	if !tb.audited("fs.write " + filepath.Join(tb.f.home, "out", "log.txt")) {
		t.Errorf("write not audited: %q", tb.audit)
	}
	if _, err := tb.call(t, "fs.read", map[string]string{"path": "~/notes/missing"}); code(err) != CodeNotFound {
		t.Fatalf("a missing file: %v", err)
	}
}

func TestFetch(t *testing.T) {
	l := newPipeListener()
	srv := &httptest.Server{Listener: l, Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/away":
			http.Redirect(w, r, "https://evil.test/", http.StatusFound)
		case "/plain":
			http.Redirect(w, r, "http://example.com/", http.StatusFound)
		case "/again":
			http.Redirect(w, r, "/hello", http.StatusFound)
		default:
			w.Header().Set("X-Seen", r.Header.Get("X-Token"))
			w.Write([]byte("hi " + r.Method))
		}
	})}}
	srv.StartTLS()
	defer srv.Close()
	tb := newTestBroker(t, mod.PermNet)
	tls := srv.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	tb.Broker.net = newFetcher(&NetOptions{
		Addr:    func(netip.Addr) bool { return true },
		Resolve: func(string) string { return "127.0.0.1:443" },
		TLS:     tls,
		Dial:    l.dial,
	})
	got, err := tb.call(t, "http.fetch", map[string]any{"url": "https://example.com/hello", "options": map[string]any{
		"method": "post", "headers": map[string]string{"X-Token": "t1"}, "body": "x"}})
	if err != nil {
		t.Fatal(err)
	}
	res := got.(FetchResult)
	if res.Status != 200 || res.Body != "hi POST" || res.Headers["x-seen"] != "t1" {
		t.Fatalf("fetch = %+v", res)
	}
	if got, err := tb.call(t, "http.fetch", map[string]any{"url": "https://example.com/again"}); err != nil || got.(FetchResult).Body != "hi GET" {
		t.Fatalf("a redirect within the hosts = %v, %v", got, err)
	}
	for _, p := range []string{"/away", "/plain"} {
		if _, err := tb.call(t, "http.fetch", map[string]any{"url": "https://example.com" + p}); code(err) != CodeDenied {
			t.Errorf("redirect %s: %v", p, err)
		}
	}
	if _, err := tb.call(t, "http.fetch", map[string]any{"url": "https://other.com/"}); code(err) != CodeDenied {
		t.Errorf("another host: %v", err)
	}
	if _, err := tb.call(t, "http.fetch", map[string]any{"url": "https://example.com/", "headers": map[string]string{"Host": "other.com"}}); code(err) != CodeBadParams {
		t.Errorf("a Host header: %v", err)
	}
	if !tb.audited("http.fetch POST https://example.com/hello") {
		t.Errorf("fetch not audited: %q", tb.audit)
	}
	// The real dial check refuses the loopback test server.
	tb.Broker.net = newFetcher(&NetOptions{Resolve: func(string) string { return "127.0.0.1:443" }, TLS: tls, Dial: l.dial})
	if _, err := tb.call(t, "http.fetch", map[string]any{"url": "https://example.com/hello"}); code(err) != CodeFailed ||
		!strings.Contains(err.Error(), "no public address") {
		t.Errorf("a loopback address: %v", err)
	}
}

func TestRun(t *testing.T) {
	if caps.Windows {
		t.Skip("env and sleep are Unix programs")
	}
	tb := newTestBroker(t, mod.PermCommands)
	t.Setenv("GH_TOKEN", "secret-token")
	t.Setenv("TERMINATR_SESSION", "s-1")
	got, err := tb.call(t, "process.run", map[string]any{"argv": []string{"env"}})
	if err != nil {
		t.Fatal(err)
	}
	res := got.(RunResult)
	if res.Code != 0 || strings.Contains(res.Stdout, "secret-token") || strings.Contains(res.Stdout, "TERMINATR_") ||
		!strings.Contains(res.Stdout, "PATH=") || !strings.Contains(res.Stdout, "NO_COLOR=1") {
		t.Fatalf("env = %+v", res)
	}
	if _, err := tb.call(t, "process.run", map[string]any{"argv": []string{"sh", "-c", "env"}}); code(err) != CodeDenied {
		t.Errorf("an ungranted program: %v", err)
	}
	if _, err := tb.call(t, "process.run", map[string]any{"argv": []string{"/usr/bin/env"}}); code(err) != CodeDenied {
		t.Errorf("a path for a granted name: %v", err)
	}
	if _, err := tb.call(t, "process.run", map[string]any{"argv": []string{"env"}, "options": map[string]any{"cwd": "~/notes"}}); code(err) != CodeDenied {
		t.Errorf("a cwd outside its folder without files: %v", err)
	}
	if _, err := tb.call(t, "process.run", map[string]any{"argv": []string{"env"}, "cwd": "~/notes"}); code(err) != CodeDenied {
		t.Errorf("a flat cwd outside its folder without files: %v", err)
	}
	got, err = tb.call(t, "process.run", map[string]any{"argv": []string{"sleep", "5"}, "options": map[string]any{"timeout_ms": 100}})
	if err != nil || !got.(RunResult).TimedOut {
		t.Errorf("a timeout = %+v, %v", got, err)
	}
	if !tb.audited("process.run env in ") {
		t.Errorf("run not audited: %q", tb.audit)
	}
}

// pipeListener serves connections made by dial, in memory.
type pipeListener struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{conns: make(chan net.Conn), done: make(chan struct{})}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *pipeListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 443} }

func (l *pipeListener) dial(ctx context.Context, _ string, _ netip.AddrPort) (net.Conn, error) {
	client, server := net.Pipe()
	select {
	case l.conns <- server:
		return client, nil
	case <-l.done:
		return nil, net.ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
