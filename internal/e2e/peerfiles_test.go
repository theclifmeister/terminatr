package e2e

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/relay"
)

// fileRow is a transfer as tm link files --json lists it.
type fileRow struct {
	ID, Message, Peer, Name, State, Code string
	Out                                  bool
	Size, Offset                         int64
}

func fileRows(env *Env, project string) []fileRow {
	var rows []fileRow
	json.Unmarshal([]byte(env.MustCLI("link", "files", "--project", project, "--json")), &rows)
	return rows
}

func fileOf(env *Env, project, id string) fileRow {
	for _, r := range fileRows(env, project) {
		if r.ID == id {
			return r
		}
	}
	return fileRow{}
}

func waitFile(t *testing.T, env *Env, project, id string, timeout time.Duration, ok func(fileRow) bool, what string) fileRow {
	t.Helper()
	var r fileRow
	if !Poll(timeout, func() bool { r = fileOf(env, project, id); return ok(r) }) {
		t.Fatalf("%s's %s never %s: %+v", project, id, what, r)
	}
	return r
}

func stateIs(s string) func(fileRow) bool { return func(r fileRow) bool { return r.State == s } }

// randomAt writes n random bytes to a new file in dir.
func randomAt(t *testing.T, dir, name string, n int) (string, []byte) {
	t.Helper()
	b := make([]byte, n)
	rand.Read(b)
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p, b
}

var sentFileRE = regexp.MustCompile(`^queued (M-[0-9A-F]{4}-\d+) with (\S+) \(([^)]*)\) for mini/api `)

// sendFile sends path from acme to mini/api as the user, and says the
// message and the transfer.
func sendFile(t *testing.T, studio *Env, path string) (msg, xid string) {
	t.Helper()
	out := studio.MustCLI("link", "send", "mini/api", "a file for you", "--file", path, "--project", "acme")
	m := sentFileRE.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("send: %s", out)
	}
	var x string
	if !Poll(10*time.Second, func() bool {
		for _, r := range fileRows(studio, "acme") {
			if r.Message == m[1] {
				x = r.ID
			}
		}
		return x != ""
	}) {
		t.Fatalf("no transfer for %s", m[1])
	}
	return m[1], x
}

// TestPeerLinkFiles (T689): files between acme on studio and api on
// mini. A 5 MB file is offered with its message, mini's user is asked
// and accepts, and the file comes over the link with the same hash,
// beside its message on both sides. A transfer whose receiving server
// is killed halfway resumes from its part once it is back. A sender's
// copy that no longer matches its hash is fetched again once, then
// fails, and studio is told; a decline, a cancel and a side with files
// off end in fixed words.
func TestPeerLinkFiles(t *testing.T) {
	studio, mini := New(t), New(t)
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	pairDirect(t, studio, mini, "studio", "mini", "ws"+strings.TrimPrefix(hs.URL, "http"))
	studio.MustCLI("project", "new", "acme")
	mini.MustCLI("project", "new", "api")
	studio.MustCLI("project", "link", "acme", "mini/api", "both", "--no-ask")
	mini.MustCLI("project", "link", "api", "studio/acme")
	// Eight messages: more than the default rate allows at once.
	setDefaults(t, studio, "link_file_kbps = 2048\nlink_burst = 20\nlink_per_hour = 120\n")
	setDefaults(t, mini, "link_burst = 20\nlink_per_hour = 120\n")
	studio.MustCLI("server", "start")
	mini.MustCLI("server", "start")
	waitPeerState(t, studio, "mini", "up", "")
	src := t.TempDir()

	// Offer, accept, transfer.
	path, data := randomAt(t, src, "report.bin", 5<<20)
	msg, xid := sendFile(t, studio, path)
	waitFile(t, mini, "api", xid, 30*time.Second, stateIs("offered"), "offered")
	items := mini.MustCLI("inbox", "list", "--project", "api")
	if !strings.Contains(items, "link-file [needs user]: studio/acme offers a file with "+msg+" (report.bin, 5.0 MB); accept or decline it") {
		t.Fatalf("api's inbox:\n%s", items)
	}
	if out := mini.MustCLI("link", "accept", xid, "--project", "api"); !strings.Contains(out, "accepted "+xid+": report.bin (5.0 MB) with "+msg+" comes over the link") {
		t.Fatalf("accept: %s", out)
	}
	waitFile(t, mini, "api", xid, 60*time.Second, stateIs("received"), "received")
	waitFile(t, studio, "acme", xid, 30*time.Second, stateIs("received"), "received on studio")
	landed := filepath.Join(mini.Home, "projects", "api", "messages", "@studio", "acme", msg, "report.bin")
	if got, err := os.ReadFile(landed); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("landed: %v", err)
	}
	if fi, _ := os.Stat(landed); fi.Mode().Perm() != 0o644 {
		t.Errorf("mode %v", fi.Mode())
	}
	if read := mini.MustCLI("link", "read", msg, "--file", "report.bin", "--project", "api"); !strings.Contains(read, "binary or over 1 MB, so only its name, size and hash show") {
		t.Fatalf("read: %s", read)
	}
	if log := mini.MustCLI("link", "log", "studio/acme", "--project", "api"); !strings.Contains(log, "▸ report.bin  5.0 MB  received") {
		t.Fatalf("log:\n%s", log)
	}
	journal, _ := os.ReadFile(filepath.Join(mini.Home, "projects", "api", "JOURNAL.md"))
	if !strings.Contains(string(journal), "link.file-received studio/acme "+xid+" (5.0 MB") || strings.Contains(string(journal), "report.bin") {
		t.Fatalf("api's journal:\n%s", journal)
	}

	// Kill the receiving server partway: the part is kept, and the
	// transfer resumes once it is back.
	path, data = randomAt(t, src, "big.bin", 8<<20)
	msg, xid = sendFile(t, studio, path)
	waitFile(t, mini, "api", xid, 30*time.Second, stateIs("offered"), "offered")
	mini.MustCLI("link", "accept", xid, "--project", "api")
	r := waitFile(t, mini, "api", xid, 60*time.Second, func(r fileRow) bool { return r.State == "receiving" && r.Offset >= 1<<20 }, "partway")
	if p, err := os.FindProcess(mini.ServerPID()); err == nil {
		p.Kill()
	}
	part := filepath.Join(mini.Home, "state", "link", "incoming", xid+".part")
	if fi, err := os.Stat(part); err != nil || fi.Size() < r.Offset {
		t.Fatalf("part after the kill: %v (%+v)", err, r)
	}
	time.Sleep(time.Second)
	mini.MustCLI("server", "start")
	waitFile(t, mini, "api", xid, 90*time.Second, stateIs("received"), "received after the restart")
	landed = filepath.Join(mini.Home, "projects", "api", "messages", "@studio", "acme", msg, "big.bin")
	if got, _ := os.ReadFile(landed); !bytes.Equal(got, data) {
		t.Fatal("the resumed file differs")
	}

	// The sender's copy changed under it: fetched twice, then failed,
	// and studio hears why.
	path, _ = randomAt(t, src, "changed.bin", 1<<20)
	msg, xid = sendFile(t, studio, path)
	waitFile(t, mini, "api", xid, 30*time.Second, stateIs("offered"), "offered")
	randomAt(t, filepath.Join(studio.Home, "projects", "acme", "messages", "@mini", "api", msg), "changed.bin", 1<<20)
	mini.MustCLI("link", "accept", xid, "--project", "api")
	if r := waitFile(t, mini, "api", xid, 60*time.Second, func(r fileRow) bool { return r.State == "failed" }, "failed"); r.Code != "bad-content" {
		t.Fatalf("failed with %+v", r)
	}
	waitFile(t, studio, "acme", xid, 30*time.Second, stateIs("failed"), "failed on studio")
	if items := studio.MustCLI("inbox", "list", "--project", "acme"); !strings.Contains(items, "your file changed.bin with "+msg+" to mini/api wasn't received: the bytes that came didn't match") {
		t.Fatalf("acme's inbox:\n%s", items)
	}

	// Declined; cancelled by the sender; a side with files off.
	path, _ = randomAt(t, src, "no.txt", 100)
	_, xid = sendFile(t, studio, path)
	waitFile(t, mini, "api", xid, 30*time.Second, stateIs("offered"), "offered")
	mini.MustCLI("link", "decline", xid, "--project", "api")
	waitFile(t, studio, "acme", xid, 30*time.Second, stateIs("declined"), "declined on studio")
	_, xid = sendFile(t, studio, path)
	waitFile(t, mini, "api", xid, 30*time.Second, stateIs("offered"), "offered")
	studio.MustCLI("link", "cancel", xid, "--project", "acme")
	waitFile(t, mini, "api", xid, 30*time.Second, stateIs("cancelled"), "cancelled on mini")
	if items := mini.MustCLI("inbox", "list", "--project", "api"); strings.Contains(items, xid) {
		t.Fatalf("the cancelled offer still asks:\n%s", items)
	}
	if out := mini.MustCLI("project", "link", "api", "studio/acme", "--files", "off"); !strings.Contains(out, "files off on this side") {
		t.Fatalf("files off: %s", out)
	}
	_, xid = sendFile(t, studio, path)
	if r := waitFile(t, studio, "acme", xid, 30*time.Second, stateIs("undelivered"), "undelivered"); r.Code != "files-off" {
		t.Fatalf("files off: %+v", r)
	}
	if r := studio.CLI("link", "send", "mini/api", "x", "--file", "t-0001/none.txt", "--project", "acme"); r.Code != 1 || !strings.Contains(r.Stderr, "bad-attachment") {
		t.Fatalf("no such library file: %d %s", r.Code, r.Stderr)
	}
}

// TestLocalLinkFiles (T689): the same flow between two projects on one
// server: the file is a copy, landing beside its message, after the
// receiving user's accept; a coordinator can't accept.
func TestLocalLinkFiles(t *testing.T) {
	env := New(t)
	env.MustCLI("project", "new", "a")
	env.MustCLI("project", "new", "b")
	env.MustCLI("project", "link", "a", "b")
	env.MustCLI("server", "start")
	path, data := randomAt(t, t.TempDir(), "notes.md", 2<<20)
	out := env.MustCLI("link", "send", "b", "notes", "--file", path, "--project", "a")
	m := regexp.MustCompile(`^sent (M-\d+) with notes.md \(2.0 MB\) to b `).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("send: %s", out)
	}
	rows := fileRows(env, "b")
	if len(rows) != 1 || rows[0].State != "offered" {
		t.Fatalf("b's files %+v", rows)
	}
	if out := env.MustCLI("link", "accept", rows[0].ID, "--project", "b"); !strings.Contains(out, "is in place; tm link read "+m[1]+" --file notes.md") {
		t.Fatalf("accept: %s", out)
	}
	got, err := os.ReadFile(filepath.Join(env.Home, "projects", "b", "messages", "a", m[1], "notes.md"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("landed: %v", err)
	}
	if list := env.MustCLI("link", "list", "--project", "a"); strings.Contains(list, "file waits") {
		t.Fatalf("list: %s", list)
	}
}

// TestPeerLinkFileResponsive (T689): a file streams from mini to studio
// at the default pace through a relay capped at 2 MiB/s, while studio
// types into mini's thread pane over the same link: the keys' echo
// stays quick (p95 under 300 ms) and a link message still goes.
func TestPeerLinkFileResponsive(t *testing.T) {
	mini, _, _ := threadEnv(t)
	studio := New(t)
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	pairDirect(t, studio, mini, "studio", "mini", "ws"+strings.TrimPrefix(hs.URL, "http"))
	writeConfig(t, mini, "[projects.demo]\nshare = true\n")
	studio.MustCLI("project", "new", "acme")
	mini.MustCLI("project", "link", "demo", "studio/acme", "both", "--no-ask")
	studio.MustCLI("project", "link", "acme", "mini/demo", "both", "--no-ask")
	studio.MustCLI("project", "link", "acme", "mini/demo", "--files", "auto")
	mini.MustCLI("server", "start")
	studio.MustCLI("server", "start")
	waitPeerState(t, studio, "mini", "up", "")
	mini.MustCLI("peer", "panes", "studio", "write")
	mini.MustCLI("thread", "start", "Fix the login", "--project", "demo")
	th := threadSession(t, mini, "demo", "t-0001")
	mini.WaitState(th, "idle", agentWait)
	w := studio.Attach(100, 30, "mini/"+th.ID)
	w.WaitFor("Fake Claude Code", agentWait)
	w.WaitUntil("the pane takes keys", wait, func(sc string) bool { return lastLine(sc, "mini › demo › ") && !strings.Contains(sc, "view-only") })

	path, _ := randomAt(t, t.TempDir(), "trace.bin", 24<<20)
	out := mini.MustCLI("link", "send", "studio/acme", "the trace", "--file", path, "--project", "demo")
	if !strings.Contains(out, "with trace.bin (24.0 MB)") {
		t.Fatalf("send: %s", out)
	}
	var xid string
	if !Poll(60*time.Second, func() bool {
		for _, r := range fileRows(studio, "acme") {
			if r.State == "receiving" && r.Offset > 0 {
				xid = r.ID
			}
		}
		return xid != ""
	}) {
		t.Fatalf("no transfer running: %+v", fileRows(studio, "acme"))
	}

	var took []time.Duration
	typed := ""
	for _, c := range "abcdefghijklmnopqrst" {
		typed += string(c)
		t0 := time.Now()
		w.Type(string(c))
		w.WaitFor("❯ "+typed, 10*time.Second)
		took = append(took, time.Since(t0))
	}
	if r := fileOf(studio, "acme", xid); r.State != "receiving" {
		t.Fatalf("the transfer ended before the keys did (%+v): nothing measured", r)
	}
	slices.Sort(took)
	if p95 := took[len(took)*95/100-1]; p95 > 300*time.Millisecond {
		t.Fatalf("keys echoed in %v at p95 during a transfer (all: %v)", p95, took)
	}
	msg := regexp.MustCompile(`^queued (M-[0-9A-F]{4}-\d+) `).FindStringSubmatch(mini.MustCLI("link", "send", "studio/acme", "still here", "--project", "demo"))
	waitLinkState(t, mini, "demo", "studio/acme", msg[1], "sent")
	waitFile(t, studio, "acme", xid, 120*time.Second, stateIs("received"), "received")
}
