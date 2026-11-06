package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestPeerUploadAllowlist: of an upload's answer with every field set, a
// paired server gets exactly the fields decided on, and the types the link
// carries are pinned: a new field is a decision. So is what the server answers
// as a whole being an Exporter that takes files.
func TestPeerUploadAllowlist(t *testing.T) {
	var r proto.SessionUploadResult
	fillAll(reflect.ValueOf(&r).Elem())
	if z := zeroFields(reflect.ValueOf(peerUploadOf(r))); len(z) > 0 {
		t.Errorf("upload fields not copied: %v", z)
	}
	for typ, want := range map[reflect.Type][]string{
		reflect.TypeFor[proto.SessionUploadResult]():     {"Name", "Received", "Ref", "Size", "Type", "Upload"},
		reflect.TypeFor[proto.PeerSessionUpload]():       {"Received", "Ref", "Size", "Type", "Upload"},
		reflect.TypeFor[proto.PeerSessionUploadParams](): {"Data", "Name", "Offset", "Project", "Session", "Size", "Upload"},
		reflect.TypeFor[proto.PeerSessionFilesParams]():  {"Files", "Project", "Session"},
		reflect.TypeFor[proto.PeerSessionFiles]():        {"Paths"},
	} {
		if got := fieldNames(typ); !slices.Equal(got, want) {
			t.Errorf("%s has %v: decide what of a new field a paired server sees (peerupload.go)", typ, got)
		}
	}
	var _ peer.UploadExporter = peerExport{}
}

// TestUploadShares: a paired server holds at most its share of the uploads in
// flight, others have room beside it, this server's own human has room beside
// them all, and a server's hour has a budget.
func TestUploadShares(t *testing.T) {
	now := time.Now()
	begin := func(u *uploads, peer string, size int64) *proto.Error {
		_, perr := u.begin(&upload{session: "s", peer: peer, size: size, at: now})
		return perr
	}
	var u uploads
	for i := range maxPeerUploads {
		if perr := begin(&u, "mini", 1); perr != nil {
			t.Fatalf("mini's upload %d: %v", i, perr)
		}
	}
	if begin(&u, "mini", 1) == nil {
		t.Error("mini past its count")
	}
	if perr := begin(&u, "studio", 1); perr != nil {
		t.Errorf("studio beside mini: %v", perr)
	}

	u = uploads{}
	if perr := begin(&u, "mini", proto.MaxUpload); perr != nil {
		t.Fatal(perr)
	}
	if begin(&u, "mini", maxPeerUploadsBytes-proto.MaxUpload+1) == nil {
		t.Error("mini past its bytes")
	}
	if perr := begin(&u, "studio", maxPeersUploadsBytes-proto.MaxUpload); perr != nil {
		t.Errorf("studio beside mini: %v", perr)
	}
	if begin(&u, "other", 1) == nil {
		t.Error("the paired servers past their bytes together")
	}
	if perr := begin(&u, "", proto.MaxUpload); perr != nil {
		t.Errorf("this server's human has room for a file: %v", perr)
	}

	// The count together: the human keeps a place.
	u = uploads{}
	for i, p := range []string{"a", "a", "a", "b", "b", "b", "c"} {
		if perr := begin(&u, p, 1); perr != nil {
			t.Fatalf("%d: %v", i, perr)
		}
	}
	if begin(&u, "d", 1) == nil {
		t.Error("the paired servers past their count together")
	}
	if perr := begin(&u, "", 1); perr != nil {
		t.Errorf("this server's human has a place: %v", perr)
	}

	// The hour.
	u = uploads{budgets: map[string]*uploadBudget{"mini": {at: now, bytes: peerUploadBudget}}}
	if begin(&u, "mini", 1) == nil {
		t.Error("mini past its hour")
	}
	if perr := begin(&u, "studio", 1); perr != nil {
		t.Errorf("studio has its own hour: %v", perr)
	}
	u.budgets["mini"].at = now.Add(-peerUploadBudgetEvery - time.Minute)
	if perr := begin(&u, "mini", 1); perr != nil {
		t.Errorf("a new hour: %v", perr)
	}
}

// uploadFix is a server with a shared project's coordinator, an unshared
// project's and the exporter a link would call.
type uploadFix struct {
	ex            peerExport
	dir           string
	shared, plain string
}

func newUploadFix(t *testing.T) *uploadFix {
	t.Helper()
	testPaths(t)
	proj := newWatchProject(t)
	if err := config.SetProject("demo", "share", true); err != nil {
		t.Fatal(err)
	}
	sessions := map[string]proto.SessionInfo{
		"s-coord": {ID: "s-coord", Project: "demo", Role: proto.RoleCoordinator},
		"s-plain": {ID: "s-plain"},
		"s-shell": {ID: "s-shell", Project: "demo", Role: "shell"},
		"s-dir":   {ID: "s-dir", Project: "demo", Role: proto.RoleDirector},
		"s-other": {ID: "s-other", Project: "quiet", Role: proto.RoleCoordinator},
	}
	ex := peerExport{uploads: &Server{log: log.New(io.Discard, "", 0)}, sessionInfo: func(id string) (proto.SessionInfo, bool) {
		info, ok := sessions[id]
		return info, ok
	}}
	return &uploadFix{ex: ex, dir: proj.Path("uploads"), shared: "s-coord", plain: "s-plain"}
}

func uploadCode(err error) string {
	var pe *proto.Error
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

// TestPeerUpload: a chunk at a time, a paired server's file lands in the
// shared session's uploads/ as session.upload's does, nothing before its last
// byte; the same refusals hold; and the paths answered are what its agent reads.
func TestPeerUpload(t *testing.T) {
	f := newUploadFix(t)
	ctx := context.Background()
	mini := caller.ForPeer("mini")
	send := func(c caller.Caller, session, name string, data []byte) (proto.PeerSessionUpload, error) {
		return f.ex.SessionUpload(ctx, c, proto.PeerSessionUploadParams{Project: "demo", Session: session, Name: name, Size: int64(len(data)), Data: data})
	}

	data := append(append([]byte{}, pngBytes...), bytes.Repeat([]byte{1}, 100)...)
	first, err := f.ex.SessionUpload(ctx, mini, proto.PeerSessionUploadParams{Project: "demo", Session: f.shared, Name: "A Shot.png", Size: int64(len(data)), Data: data[:10]})
	if err != nil || first.Ref != "" || first.Received != 10 {
		t.Fatalf("first chunk: %+v %v", first, err)
	}
	if entries, _ := os.ReadDir(f.dir); len(entries) != 0 {
		t.Errorf("a file before the last byte: %v", entries)
	}
	// Not another server's to go on with, nor the human's.
	if _, err := f.ex.SessionUpload(ctx, caller.ForPeer("other"), proto.PeerSessionUploadParams{Project: "demo", Session: f.shared, Upload: first.Upload, Offset: 10, Data: data[10:]}); uploadCode(err) != proto.ErrBadParams {
		t.Errorf("another server's chunk: %v", err)
	}
	last, err := f.ex.SessionUpload(ctx, mini, proto.PeerSessionUploadParams{Project: "demo", Session: f.shared, Upload: first.Upload, Offset: 10, Data: data[10:]})
	if err != nil || !strings.HasSuffix(last.Ref, "-A-Shot.png") || last.Type != "image/png" || last.Size != int64(len(data)) {
		t.Fatalf("last chunk: %+v %v", last, err)
	}
	stored := filepath.Join(f.dir, last.Ref)
	if got, err := os.ReadFile(stored); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("stored: %v", err)
	}
	if fi, _ := os.Stat(stored); fi.Mode().Perm() != 0o600 {
		t.Errorf("stored mode %v", fi.Mode())
	}
	journal, _ := os.ReadFile(filepath.Join(filepath.Dir(f.dir), "JOURNAL.md"))
	if !strings.Contains(string(journal), "session.upload") || !strings.Contains(string(journal), last.Ref) || !strings.Contains(string(journal), "mini") {
		t.Errorf("journal: %s", journal)
	}
	got, err := f.ex.SessionFiles(mini, proto.PeerSessionFilesParams{Project: "demo", Session: f.shared, Files: []string{last.Ref}})
	if err != nil || !slices.Equal(got.Paths, []string{stored}) {
		t.Fatalf("paths: %+v %v (want %s)", got, err, stored)
	}

	before, _ := os.ReadDir(f.dir)
	for _, tc := range []struct {
		name string
		err  func() error
		want string
	}{
		{"not a peer", func() error {
			_, err := send(caller.Caller{Kind: caller.Human}, f.shared, "a.png", pngBytes)
			return err
		}, proto.ErrRefused},
		{"a remote id", func() error { _, err := send(mini, "mini/"+f.shared, "a.png", pngBytes); return err }, proto.ErrUnknownSession},
		{"no such session", func() error { _, err := send(mini, "s-nope", "a.png", pngBytes); return err }, proto.ErrUnknownSession},
		{"a session of no project", func() error { _, err := send(mini, f.plain, "a.png", pngBytes); return err }, proto.ErrUnknownSession},
		{"a shell", func() error { _, err := send(mini, "s-shell", "a.png", pngBytes); return err }, proto.ErrUnknownSession},
		{"a director", func() error { _, err := send(mini, "s-dir", "a.png", pngBytes); return err }, proto.ErrUnknownSession},
		{"another project's session", func() error { _, err := send(mini, "s-other", "a.png", pngBytes); return err }, proto.ErrUnknownSession},
		{"a project not shared", func() error {
			_, err := f.ex.SessionUpload(ctx, mini, proto.PeerSessionUploadParams{Project: "quiet", Session: f.shared, Name: "a.png", Size: 1, Data: []byte("x")})
			return err
		}, proto.ErrNotShared},
		{"a type", func() error { _, err := send(mini, f.shared, "id_rsa.key", []byte("x")); return err }, proto.ErrBadParams},
		{"a signature", func() error { _, err := send(mini, f.shared, "a.png", []byte("text")); return err }, proto.ErrBadParams},
		{"text with a NUL", func() error { _, err := send(mini, f.shared, "a.txt", []byte("a\x00b")); return err }, proto.ErrBadParams},
		{"too big", func() error {
			_, err := f.ex.SessionUpload(ctx, mini, proto.PeerSessionUploadParams{Project: "demo", Session: f.shared, Name: "a.txt", Size: proto.MaxUpload + 1, Data: []byte("x")})
			return err
		}, proto.ErrBadParams},
		{"a bad ref", func() error {
			_, err := f.ex.SessionFiles(mini, proto.PeerSessionFilesParams{Project: "demo", Session: f.shared, Files: []string{"../PROJECT.md"}})
			return err
		}, proto.ErrBadParams},
		{"a ref gone", func() error {
			_, err := f.ex.SessionFiles(mini, proto.PeerSessionFilesParams{Project: "demo", Session: f.shared, Files: []string{"x.png"}})
			return err
		}, proto.ErrFileGone},
		{"paths of another project", func() error {
			_, err := f.ex.SessionFiles(mini, proto.PeerSessionFilesParams{Project: "quiet", Session: f.shared, Files: []string{last.Ref}})
			return err
		}, proto.ErrNotShared},
	} {
		if err := tc.err(); uploadCode(err) != tc.want {
			t.Errorf("%s: %v, want %s", tc.name, err, tc.want)
		}
	}
	if after, _ := os.ReadDir(f.dir); len(after) != len(before) {
		t.Errorf("a refusal left a file: %v", after)
	}

	// A bad signature's bytes aren't written, and words hold no path.
	_, err = send(mini, f.shared, "b.png", []byte("text"))
	if err == nil || strings.Contains(err.Error(), f.dir) {
		t.Errorf("words: %v", err)
	}
}

// TestPeerUploadLinkDrops: the bytes of an upload go when its link ends, and
// when the pane switch stops it: nothing stays in memory, nothing on disk.
func TestPeerUploadLinkDrops(t *testing.T) {
	f := newUploadFix(t)
	mini := caller.ForPeer("mini")
	gone, end := context.WithCancel(context.Background())
	defer end()
	r, err := f.ex.SessionUpload(gone, mini, proto.PeerSessionUploadParams{Project: "demo", Session: f.shared, Name: "a.txt", Size: 4, Data: []byte("ab")})
	if err != nil {
		t.Fatal(err)
	}
	end()
	u := &f.ex.uploads.uploads
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		u.mu.Lock()
		n := len(u.m)
		u.mu.Unlock()
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the upload outlived its link")
		}
	}
	if _, err := f.ex.SessionUpload(context.Background(), mini, proto.PeerSessionUploadParams{Project: "demo", Session: f.shared, Upload: r.Upload, Offset: 2, Data: []byte("cd")}); uploadCode(err) != proto.ErrBadParams {
		t.Errorf("a chunk after the drop: %v", err)
	}
	if entries, _ := os.ReadDir(f.dir); len(entries) != 0 {
		t.Errorf("a partial file: %v", entries)
	}

	// The switch going off forgets it too.
	if _, err := f.ex.SessionUpload(context.Background(), mini, proto.PeerSessionUploadParams{Project: "demo", Session: f.shared, Name: "a.txt", Size: 4, Data: []byte("ab")}); err != nil {
		t.Fatal(err)
	}
	f.ex.DropUploads(caller.ForPeer("other"))
	u.mu.Lock()
	kept := len(u.m)
	u.mu.Unlock()
	if kept != 1 {
		t.Errorf("another server's switch dropped mini's: %d left", kept)
	}
	f.ex.DropUploads(mini)
	u.mu.Lock()
	kept = len(u.m)
	u.mu.Unlock()
	if kept != 0 {
		t.Errorf("%d left", kept)
	}

	// A peer idles out sooner than the human.
	now := time.Now()
	var v uploads
	id, _ := v.begin(&upload{session: "s", peer: "mini", size: 1, at: now})
	own, _ := v.begin(&upload{session: "s", size: 1, at: now})
	v.mu.Lock()
	v.sweep(now.Add(peerUploadIdle + time.Second))
	_, peerLeft := v.m[id]
	_, ownLeft := v.m[own]
	v.mu.Unlock()
	if peerLeft || !ownLeft {
		t.Errorf("idle: peer's left %v, own left %v", peerLeft, ownLeft)
	}
}
