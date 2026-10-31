package server

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// pngBytes is the start of a PNG, enough for its signature to sniff.
var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 64)...)

// TestUploadName: a stored name keeps letters, digits and . _ -, never
// a folder or a leading dot, and is bounded with its extension kept.
func TestUploadName(t *testing.T) {
	for in, want := range map[string]string{
		"shot.png":                        "shot.png",
		"Screen Shot 2026 at 1.png":       "Screen-Shot-2026-at-1.png",
		"../../etc/passwd.txt":            "passwd.txt",
		`C:\x\notes.md`:                   "notes.md",
		".hidden.png":                     "hidden.png",
		"résumé.pdf":                      "r-sum-.pdf",
		strings.Repeat("a", 200) + ".png": strings.Repeat("a", maxUploadName-4) + ".png",
	} {
		if got := uploadName(in); got != want {
			t.Errorf("uploadName(%q) = %q, want %q", in, got, want)
		}
	}
	for name, want := range map[string]string{"a.PNG": "image/png", "a.pdf": "application/pdf", "a.md": "text/markdown",
		"a.key": "", "a.zip": "", "a": "", ".png": "", "a.sh": ""} {
		if got := uploadType(name); got != want {
			t.Errorf("uploadType(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestSniffs: an image or a PDF must have its signature, text must be
// UTF-8 without NUL.
func TestSniffs(t *testing.T) {
	cases := []struct {
		typ  string
		data []byte
		want bool
	}{
		{"image/png", pngBytes, true},
		{"image/jpeg", pngBytes, false},
		{"image/png", []byte("not an image"), false},
		{"application/pdf", []byte("%PDF-1.7\n"), true},
		{"text/plain", []byte("hello\n"), true},
		{"text/markdown", []byte("a\x00b"), false},
		{"text/plain", []byte{0xff, 0xfe}, false},
	}
	for _, c := range cases {
		if got := sniffs(c.typ, c.data); got != c.want {
			t.Errorf("sniffs(%s, %q) = %v", c.typ, c.data, got)
		}
	}
}

// TestPromptFiles: refs become "Attached: <path>" lines after the text;
// a ref that names a folder, a link or a missing file is refused.
func TestPromptFiles(t *testing.T) {
	testPaths(t)
	p := newWatchProject(t)
	up := p.Path("uploads")
	if err := os.WriteFile(filepath.Join(up, "1-a.png"), pngBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(up, "2-b.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(up, "2-b.md"), filepath.Join(up, "3-c.md")); err != nil {
		t.Fatal(err)
	}
	got, perr := promptFiles("demo", "look at these\n", []string{"1-a.png", "2-b.md"})
	if perr != nil {
		t.Fatal(perr)
	}
	want := "look at these\n\nAttached: " + filepath.Join(up, "1-a.png") + "\nAttached: " + filepath.Join(up, "2-b.md")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, _ := promptFiles("demo", "", []string{"1-a.png"}); got != "Attached: "+filepath.Join(up, "1-a.png") {
		t.Errorf("files alone: %q", got)
	}
	for ref, code := range map[string]string{"../PROJECT.md": proto.ErrBadParams, "x/1-a.png": proto.ErrBadParams,
		".1-a.png": proto.ErrBadParams, "PROJECT": proto.ErrBadParams, "3-c.md": proto.ErrFileGone, "9-none.png": proto.ErrFileGone} {
		if _, perr := promptFiles("demo", "t", []string{ref}); perr == nil || perr.Code != code {
			t.Errorf("ref %q: %v, want %s", ref, perr, code)
		}
	}
	if _, perr := promptFiles("", "t", []string{"1-a.png"}); perr == nil || perr.Code != proto.ErrRefused {
		t.Errorf("no project: %v", perr)
	}
	many := make([]string, proto.MaxPromptFiles+1)
	if _, perr := promptFiles("demo", "t", many); perr == nil || perr.Code != proto.ErrBadParams {
		t.Errorf("too many: %v", perr)
	}
}

// TestUploads: chunks go in at the offset taken so far, the upload is
// handed back once its size is in, and the bounds hold.
func TestUploads(t *testing.T) {
	var u uploads
	now := time.Now()
	id, perr := u.begin(&upload{session: "s-1", size: 4, at: now})
	if perr != nil {
		t.Fatal(perr)
	}
	if _, _, perr := u.add(id, "s-2", 0, []byte("ab"), now); perr == nil {
		t.Error("another session's chunk taken")
	}
	if _, _, perr := u.add(id, "s-1", 1, []byte("ab"), now); perr == nil {
		t.Error("a chunk at the wrong offset taken")
	}
	if got, up, perr := u.add(id, "s-1", 0, []byte("ab"), now); perr != nil || up != nil || got != 2 {
		t.Fatalf("first chunk: %d %v %v", got, up, perr)
	}
	if got, up, perr := u.add(id, "s-1", 2, []byte("cd"), now); perr != nil || up == nil || got != 4 || string(up.data) != "abcd" {
		t.Fatalf("last chunk: %d %v %v", got, up, perr)
	}
	if _, _, perr := u.add(id, "s-1", 4, nil, now); perr == nil {
		t.Error("a finished upload still taken")
	}
	// Too much data, and idle ones dropped.
	id, _ = u.begin(&upload{session: "s-1", size: 2, at: now})
	if _, _, perr := u.add(id, "s-1", 0, []byte("abc"), now); perr == nil {
		t.Error("more than the size taken")
	}
	id, _ = u.begin(&upload{session: "s-1", size: 2, at: now})
	if _, _, perr := u.add(id, "s-1", 0, []byte("a"), now.Add(uploadIdle+time.Second)); perr == nil {
		t.Error("an idle upload kept")
	}
	// The bounds.
	for i := range maxUploads {
		if _, perr := u.begin(&upload{session: "s-" + strconv.Itoa(i), size: 1, at: now}); perr != nil {
			t.Fatalf("upload %d: %v", i, perr)
		}
	}
	if _, perr := u.begin(&upload{session: "s-x", size: 1, at: now}); perr == nil || perr.Code != proto.ErrRefused {
		t.Errorf("one more than maxUploads: %v", perr)
	}
	u = uploads{}
	if _, perr := u.begin(&upload{size: maxUploadsBytes + 1, at: now}); perr == nil {
		t.Error("more than maxUploadsBytes")
	}
}

// TestSessionUpload: a file sent in chunks to a project's session lands
// in its uploads/ under a time-stamped name, journaled; a plain
// session, a type tm doesn't take, bytes not of its type and a paired
// server's session are refused, and session.prompt refuses a bad ref
// before anything is sent.
func TestSessionUpload(t *testing.T) {
	p := testPaths(t)
	proj := newWatchProject(t)
	if err := os.MkdirAll(p.AgentsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := "manifest_version = 1\nname = \"sleeper\"\n[launch]\ncommand = " + strconv.Quote(testSh(t)) + "\nargs = [\"-c\", \"sleep 30\"]\n"
	if err := os.WriteFile(filepath.Join(p.AgentsDir(), "sleeper.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	startServer(t, p)
	c, err := client.Dial(p, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	code := func(err error) string {
		var perr *proto.Error
		if errors.As(err, &perr) {
			return perr.Code
		}
		return ""
	}
	var coord, plain proto.SessionStartResult
	call(t, c, proto.MethodSessionStart, proto.SessionStartParams{Agent: "sleeper", Cwd: t.TempDir(), Role: proto.RoleCoordinator, Project: "demo"}, &coord)
	call(t, c, proto.MethodSessionStart, proto.SessionStartParams{Argv: []string{testSh(t)}, Cwd: t.TempDir()}, &plain)
	id := coord.Session.ID

	// Two chunks.
	data := append(append([]byte{}, pngBytes...), bytes.Repeat([]byte{1}, proto.UploadChunk)...)
	var res proto.SessionUploadResult
	call(t, c, proto.MethodSessionUpload, proto.SessionUploadParams{Session: id, Name: "Screen Shot.png", Size: int64(len(data)), Data: data[:proto.UploadChunk]}, &res)
	if res.Ref != "" || res.Received != proto.UploadChunk || res.Upload == "" {
		t.Fatalf("first chunk: %+v", res)
	}
	call(t, c, proto.MethodSessionUpload, proto.SessionUploadParams{Session: id, Upload: res.Upload, Offset: res.Received, Data: data[proto.UploadChunk:]}, &res)
	if !strings.HasSuffix(res.Ref, "-Screen-Shot.png") || res.Type != "image/png" || res.Size != int64(len(data)) {
		t.Fatalf("last chunk: %+v", res)
	}
	stored := proj.Path("uploads", res.Ref)
	if got, err := os.ReadFile(stored); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("stored: %v", err)
	}
	if fi, _ := os.Stat(stored); fi.Mode().Perm() != 0o600 {
		t.Errorf("stored mode %v", fi.Mode())
	}
	journal, _ := os.ReadFile(proj.Path("JOURNAL.md"))
	if !strings.Contains(string(journal), "session.upload") || !strings.Contains(string(journal), res.Ref) {
		t.Errorf("journal: %s", journal)
	}

	refused := []struct {
		name string
		p    proto.SessionUploadParams
		code string
	}{
		{"plain session", proto.SessionUploadParams{Session: plain.Session.ID, Name: "a.png", Size: 1, Data: []byte("x")}, proto.ErrRefused},
		{"paired session", proto.SessionUploadParams{Session: "mini/s-1", Name: "a.png", Size: 1, Data: []byte("x")}, proto.ErrRefused},
		{"type", proto.SessionUploadParams{Session: id, Name: "id_rsa.key", Size: 1, Data: []byte("x")}, proto.ErrBadParams},
		{"bytes", proto.SessionUploadParams{Session: id, Name: "a.png", Size: 4, Data: []byte("text")}, proto.ErrBadParams},
		{"size", proto.SessionUploadParams{Session: id, Name: "a.png", Size: proto.MaxUpload + 1, Data: []byte("x")}, proto.ErrBadParams},
		{"chunk", proto.SessionUploadParams{Session: id, Name: "a.txt", Size: proto.UploadChunk + 1, Data: make([]byte, proto.UploadChunk+1)}, proto.ErrBadParams},
		{"unknown upload", proto.SessionUploadParams{Session: id, Upload: "nope", Offset: 1, Data: []byte("x")}, proto.ErrBadParams},
	}
	for _, r := range refused {
		if err := c.Call(proto.MethodSessionUpload, r.p, nil); code(err) != r.code {
			t.Errorf("%s: %v, want %s", r.name, err, r.code)
		}
	}
	if err := c.Call(proto.MethodSessionPrompt, proto.SessionPromptParams{ID: id, Files: []string{"../PROJECT.md"}}, nil); code(err) != proto.ErrBadParams {
		t.Errorf("prompt with a bad ref: %v", err)
	}
	if err := c.Call(proto.MethodSessionPrompt, proto.SessionPromptParams{ID: plain.Session.ID, Text: "x", Files: []string{res.Ref}}, nil); code(err) != proto.ErrRefused {
		t.Errorf("prompt with files to a plain session: %v", err)
	}
	if err := c.Call(proto.MethodSessionPrompt, proto.SessionPromptParams{ID: "mini/s-1", Text: "x", Files: []string{res.Ref}}, nil); code(err) != proto.ErrRefused {
		t.Errorf("prompt with files to a paired session: %v", err)
	}
	// No text: a plain prompt is refused as empty; an interrupt is a Stop
	// (T893), refused while there is no turn to stop, with files a prompt.
	if err := c.Call(proto.MethodSessionPrompt, proto.SessionPromptParams{ID: id}, nil); code(err) != proto.ErrBadParams {
		t.Errorf("empty prompt: %v", err)
	}
	if err := c.Call(proto.MethodSessionPrompt, proto.SessionPromptParams{ID: id, Mode: proto.PromptInterrupt}, nil); code(err) != proto.ErrRefused {
		t.Errorf("stop with no turn: %v", err)
	}
	if err := c.Call(proto.MethodSessionPrompt, proto.SessionPromptParams{ID: plain.Session.ID, Mode: proto.PromptInterrupt}, nil); code(err) != proto.ErrRefused {
		t.Errorf("stop a plain session: %v", err)
	}
}
