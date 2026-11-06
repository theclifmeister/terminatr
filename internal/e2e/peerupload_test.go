package e2e

import (
	"bytes"
	"crypto/rand"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/appapi"
	"github.com/theclifmeister/terminatr/internal/appapi/apptest"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
)

var uploadPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00")

// uploadTo sends data to session through the bridge a chunk at a time, as the
// composer does, and answers the last result.
func uploadTo(b *apptest.Bridge, session, name string, data []byte) (appapi.Upload, error) {
	var up appapi.Upload
	offset := 0
	for {
		end := min(len(data), offset+proto.UploadChunk)
		p := appapi.UploadParams{Session: session, Upload: up.Upload, Offset: int64(offset), Data: data[offset:end]}
		if up.Upload == "" {
			p.Name, p.Size = name, int64(len(data))
		}
		if err := b.Call(proto.MethodSessionUpload, p, &up); err != nil {
			return up, err
		}
		offset = end
		if up.Ref != "" || offset >= len(data) {
			return up, nil
		}
	}
}

// uploadPair is studio and mini paired through a relay, mini's demo shared
// with a thread running: the thread's id on studio, and the bridge there.
func uploadPair(t *testing.T) (studio, mini *Env, id string, b *apptest.Bridge, rs *relay.Server, hs *httptest.Server) {
	t.Helper()
	mini, _, _ = threadEnv(t)
	studio = New(t)
	rs = relay.New(relay.DefaultConfig())
	hs = httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	pairDirect(t, studio, mini, "studio", "mini", "ws"+strings.TrimPrefix(hs.URL, "http"))
	writeConfig(t, mini, "[projects.demo]\nguard = false\nshare = true\n")
	mini.MustCLI("server", "start")
	studio.MustCLI("server", "start")
	waitPeerState(t, studio, "mini", "up", "")
	mini.MustCLI("peer", "panes", "studio", "write")
	mini.MustCLI("thread", "start", "Fix the login", "--project", "demo")
	th := threadSession(t, mini, "demo", "t-0001")
	mini.WaitState(th, "idle", agentWait)
	b = runBridge(t, studio)
	if !Poll(wait, func() bool {
		var l appapi.PeerList
		if b.Call(proto.MethodPeerList, nil, &l) != nil {
			return false
		}
		return slices.ContainsFunc(l.Peers, func(p appapi.Peer) bool { return p.Alias == "mini" && p.Uploads && p.Panes == proto.PanesType })
	}) {
		t.Fatal("studio never heard that mini takes files")
	}
	return studio, mini, "mini/" + th.ID, b, rs, hs
}

// TestPeerSessionUpload (T1077): two servers through a relay. An image and a
// PDF over several of session.upload's chunks land in mini's project's
// uploads/ at mode 0600, journaled there; the prompt reaches mini's agent with
// mini's paths on its Attached lines; and the refusals hold on mini: pane
// switch below write, a type, a signature, too big.
func TestPeerSessionUpload(t *testing.T) {
	_, mini, id, b, _, _ := uploadPair(t)
	uploads := filepath.Join(mini.Home, "projects", "demo", "uploads")

	png, err := uploadTo(b, id, "Screen Shot.png", append(slices.Clone(uploadPNG), make([]byte, proto.UploadChunk+1000)...))
	if err != nil {
		t.Fatalf("the image: %v", err)
	}
	pdf := append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte("x"), 40<<10)...)
	doc, err := uploadTo(b, id, "plan.pdf", pdf)
	if err != nil {
		t.Fatalf("the PDF: %v", err)
	}
	if !strings.HasSuffix(png.Ref, "-Screen-Shot.png") || png.Type != "image/png" || !strings.HasSuffix(doc.Ref, "-plan.pdf") || doc.Type != "application/pdf" {
		t.Fatalf("refs: %+v %+v", png, doc)
	}
	for _, ref := range []string{png.Ref, doc.Ref} {
		fi, err := os.Stat(filepath.Join(uploads, ref))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %v", ref, fi.Mode())
		}
	}
	if got, _ := os.ReadFile(filepath.Join(uploads, doc.Ref)); !bytes.Equal(got, pdf) {
		t.Errorf("the PDF came out as %d bytes", len(got))
	}
	journal, _ := os.ReadFile(filepath.Join(mini.Home, "projects", "demo", "JOURNAL.md"))
	if !strings.Contains(string(journal), "session.upload") || !strings.Contains(string(journal), doc.Ref) {
		t.Errorf("mini's journal: %s", journal)
	}

	var res appapi.PromptResult
	if err := b.Call(proto.MethodSessionPrompt, appapi.PromptParams{ID: id, Text: "look at these", Files: []string{png.Ref, doc.Ref}}, &res); err != nil {
		t.Fatalf("the prompt: %v", err)
	}
	r := mini.WaitFake("prompt", agentWait, func(r FakeRecord) bool { return strings.Contains(r.Str("text"), "look at these") })
	text := r.Str("text")
	for _, ref := range []string{png.Ref, doc.Ref} {
		if !strings.Contains(text, "Attached: "+filepath.Join(uploads, ref)) {
			t.Errorf("the prompt doesn't name %s on mini:\n%s", ref, text)
		}
	}

	// Files alone are a prompt too.
	if err := b.Call(proto.MethodSessionPrompt, appapi.PromptParams{ID: id, Files: []string{doc.Ref}}, &res); err != nil {
		t.Fatalf("files alone: %v", err)
	}
	if err := b.Call(proto.MethodSessionPrompt, appapi.PromptParams{ID: id, Text: "x", Files: []string{"20200101-000000-gone.png"}}, &res); appCode(err) != proto.ErrFileGone {
		t.Errorf("a ref gone: %v", err)
	}

	count := func() int {
		entries, _ := os.ReadDir(uploads)
		return len(entries)
	}
	n := count()
	for name, tc := range map[string]struct {
		name string
		data []byte
	}{
		"a type":      {"key.pem", []byte("secret")},
		"a signature": {"fake.png", []byte("not an image")},
	} {
		if _, err := uploadTo(b, id, tc.name, tc.data); appCode(err) != proto.ErrBadParams {
			t.Errorf("%s: %v", name, err)
		}
	}
	var up appapi.Upload
	if err := b.Call(proto.MethodSessionUpload, appapi.UploadParams{Session: id, Name: "big.txt", Size: proto.MaxUpload + 1, Data: []byte("x")}, &up); appCode(err) != proto.ErrBadParams {
		t.Errorf("too big: %v", err)
	}

	// Below write, nothing goes, at the next request, mid-file too.
	begun, err := func() (appapi.Upload, error) {
		var u appapi.Upload
		err := b.Call(proto.MethodSessionUpload, appapi.UploadParams{Session: id, Name: "mid.txt", Size: 8, Data: []byte("half")}, &u)
		return u, err
	}()
	if err != nil {
		t.Fatal(err)
	}
	mini.MustCLI("peer", "panes", "studio", "view")
	if err := b.Call(proto.MethodSessionUpload, appapi.UploadParams{Session: id, Upload: begun.Upload, Offset: 4, Data: []byte("half")}, &up); appCode(err) != proto.ErrPanesOff {
		t.Errorf("view, the next chunk: %v", err)
	}
	if _, err := uploadTo(b, id, "a.txt", []byte("hello")); appCode(err) != proto.ErrPanesOff {
		t.Errorf("view, a new file: %v", err)
	}
	if err := b.Call(proto.MethodSessionPrompt, appapi.PromptParams{ID: id, Text: "x", Files: []string{doc.Ref}}, &res); appCode(err) != proto.ErrPanesOff {
		t.Errorf("view, a prompt: %v", err)
	}
	if got := count(); got != n {
		t.Errorf("uploads went from %d to %d", n, got)
	}
}

// TestPeerSessionUploadLinkDrops: the link going mid-file fails the next chunk
// in words that say so, and mini keeps no part of it.
func TestPeerSessionUploadLinkDrops(t *testing.T) {
	studio, mini, id, b, _, _ := uploadPair(t)
	uploads := filepath.Join(mini.Home, "projects", "demo", "uploads")
	data := make([]byte, 2<<20)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	data = append(slices.Clone(uploadPNG), data...)

	var up appapi.Upload
	if err := b.Call(proto.MethodSessionUpload, appapi.UploadParams{Session: id, Name: "drop.png", Size: int64(len(data)), Data: data[:proto.UploadChunk]}, &up); err != nil {
		t.Fatal(err)
	}
	mini.MustCLI("server", "stop", "--yes")
	waitPeerState(t, studio, "mini", "offline", proto.LinkReasonPeer)
	err := b.Call(proto.MethodSessionUpload, appapi.UploadParams{Session: id, Upload: up.Upload, Offset: up.Received, Data: data[proto.UploadChunk : 2*proto.UploadChunk]}, &up)
	var perr *appapi.Error
	if !errors.As(err, &perr) || perr.Code != proto.ErrRefused || !strings.Contains(perr.Message, "mini") {
		t.Fatalf("after the drop: %v", err)
	}
	if entries, _ := os.ReadDir(uploads); len(entries) != 0 {
		t.Errorf("a partial file: %v", entries)
	}
}

// TestPeerSessionUploadResponsive: while a file goes over the link, a key typed
// into a pane of the same link echoes promptly: the upload leaves it room.
func TestPeerSessionUploadResponsive(t *testing.T) {
	studio, _, id, b, _, _ := uploadPair(t)
	w := studio.Attach(100, 30, id)
	w.WaitFor("Fake Claude Code", agentWait)
	w.WaitUntil("the pane takes keys", wait, func(sc string) bool { return lastLine(sc, "mini › demo › ") && !strings.Contains(sc, "view-only") })

	data := make([]byte, 16<<20)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	data = append(slices.Clone(uploadPNG), data...)
	done := make(chan error, 1)
	go func() {
		_, err := uploadTo(b, id, "big.png", data)
		done <- err
	}()
	time.Sleep(time.Second)

	var took []time.Duration
	typed := ""
	for _, c := range "abcdefghijklmnopqrst" {
		typed += string(c)
		t0 := time.Now()
		w.Type(string(c))
		w.WaitFor("❯ "+typed, 10*time.Second)
		took = append(took, time.Since(t0))
	}
	select {
	case err := <-done:
		t.Fatalf("the upload ended before the keys did (%v): nothing measured", err)
	default:
	}
	slices.Sort(took)
	if p95 := took[len(took)*95/100-1]; p95 > 300*time.Millisecond {
		t.Fatalf("keys echoed in %v at p95 during an upload (all: %v)", p95, took)
	}
	if err := <-done; err != nil {
		t.Fatalf("the upload: %v", err)
	}
}
