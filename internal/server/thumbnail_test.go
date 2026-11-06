package server

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
)

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range m.Pix {
		m.Pix[i] = 200
	}
	var b bytes.Buffer
	if err := png.Encode(&b, m); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// hugeHeader is the start of a PNG saying it is 10000 by 10000: all the
// server may read of a 100 MP image.
func hugeHeader() []byte {
	return []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR\x00\x00\x27\x10\x00\x00\x27\x10\x08\x06\x00\x00\x00\x4c\x15\xdb\x52")
}

func thumbs(t *testing.T) []string {
	t.Helper()
	d, err := home.ThumbCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	filepath.WalkDir(d, func(path string, e os.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			out = append(out, path)
		}
		return nil
	})
	return out
}

func wantThumb(t *testing.T, app *client.Client, pr *project.Project, id, name string, w, h int) {
	t.Helper()
	var r proto.LibraryThumbnailResult
	if err := app.Call(proto.MethodLibraryThumbnail, proto.LibraryThumbnailParams{Project: pr.Slug, ID: id, Name: name}, &r); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(r.Data))
	if err != nil || r.Name != name || r.Width != w || r.Height != h || cfg.Width != w || cfg.Height != h {
		t.Fatalf("%s: %q %dx%d, JPEG %+v %v", name, r.Name, r.Width, r.Height, cfg, err)
	}
}

func wantNoThumb(t *testing.T, app *client.Client, pr *project.Project, id, name, code string) {
	t.Helper()
	var r proto.LibraryThumbnailResult
	err := app.Call(proto.MethodLibraryThumbnail, proto.LibraryThumbnailParams{Project: pr.Slug, ID: id, Name: name}, &r)
	var pe *proto.Error
	if !errors.As(err, &pe) || pe.Code != code {
		t.Fatalf("%s %s: %+v %v, want %s", id, name, r, err, code)
	}
}

// TestLibraryThumbnailSetups: the same in the design's four setups, and
// with no git to run: library files belong to threads, not repos.
func TestLibraryThumbnailSetups(t *testing.T) {
	git := func(t *testing.T, dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	repo := func(t *testing.T) string {
		d := t.TempDir()
		git(t, d, "init", "-q")
		return d
	}
	for name, repos := range map[string]func(t *testing.T) []string{
		"plain folder":  func(*testing.T) []string { return nil },
		"repo, no tags": func(t *testing.T) []string { return []string{repo(t)} },
		"several repos": func(t *testing.T) []string { return []string{repo(t), repo(t), repo(t)} },
		"no PRs, another host": func(t *testing.T) []string {
			d := repo(t)
			git(t, d, "remote", "add", "origin", "https://git.example.org/acme/web.git")
			return []string{d}
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := testPaths(t)
			startServer(t, p)
			pr, err := project.New(project.Options{Slug: "web", Repos: repos(t)})
			if err != nil {
				t.Fatal(err)
			}
			th, err := thread.Create(pr, thread.Record{Title: "Shots", Task: "T1", State: thread.Running})
			if err != nil {
				t.Fatal(err)
			}
			os.MkdirAll(thread.Path(pr, th.ID, "library"), 0o755)
			os.WriteFile(thread.Path(pr, th.ID, "library", "a.png"), testPNG(t, 800, 200), 0o644)
			app, err := client.DialAs(p, proto.ClientApp)
			if err != nil {
				t.Fatal(err)
			}
			defer app.Close()
			t.Setenv("PATH", "") // no git, no anything
			wantThumb(t, app, pr, th.ID, "a.png", 400, 100)
		})
	}
}

// TestLibraryThumbnail: a thread's image gets a thumbnail, cached outside
// the library and kept when the thread packs; what makes none is refused
// no-thumbnail and remembered, a bad name or thread with the library's
// codes; a deleted file's goes with it.
func TestLibraryThumbnail(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	pr, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	th, err := thread.Create(pr, thread.Record{Title: "Shots", Task: "T1", State: thread.Running})
	if err != nil {
		t.Fatal(err)
	}
	lib := thread.Path(pr, th.ID, "library")
	os.MkdirAll(lib, 0o755)
	good := testPNG(t, 1000, 500)
	files := map[string][]byte{
		"shot.png":    good,
		"gone.png":    good,
		"corrupt.png": good[:len(good)/2],
		"huge.png":    hugeHeader(),
		"pic.svg":     []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`),
		"fake.png":    []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`),
		"notes.md":    []byte("# notes\n"),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(lib, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	secret := filepath.Join(t.TempDir(), "secret.png")
	os.WriteFile(secret, good, 0o644)
	os.Symlink(secret, filepath.Join(lib, "link.png"))
	os.WriteFile(filepath.Join(lib, "over.png"), make([]byte, proto.MaxLibraryRead+1), 0o644)

	app, err := client.DialAs(p, proto.ClientApp)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	wantThumb(t, app, pr, th.ID, "shot.png", 400, 200)
	wantThumb(t, app, pr, th.ID, "gone.png", 400, 200)
	for _, name := range []string{"corrupt.png", "huge.png", "pic.svg", "fake.png", "notes.md", "over.png"} {
		wantNoThumb(t, app, pr, th.ID, name, proto.ErrNoThumbnail)
		wantNoThumb(t, app, pr, th.ID, name, proto.ErrNoThumbnail) // and again, remembered
	}
	for _, c := range []struct{ id, name, code string }{
		{th.ID, "link.png", "unknown-file"}, // a link is no library file
		{th.ID, "none.png", "unknown-file"},
		{th.ID, "../thread.png", "invalid-file"},
		{th.ID, ".hidden.png", "invalid-file"},
		{"t-0099", "shot.png", "unknown-file"},
		{"nope", "shot.png", "unknown-thread"},
		{"M-AB12-1", "shot.png", "unknown-message"},
	} {
		wantNoThumb(t, app, pr, c.id, c.name, c.code)
	}
	// Outside the library, and nothing added to it.
	cached := thumbs(t)
	if len(cached) == 0 {
		t.Fatal("no cache")
	}
	for _, c := range cached {
		if strings.HasPrefix(c, pr.Dir) {
			t.Errorf("%s is in the project", c)
		}
	}
	ents, _ := os.ReadDir(lib)
	if len(ents) != len(files)+2 {
		t.Errorf("the library has %d files: %v", len(ents), ents)
	}

	// Deleting a file takes its thumbnail with it.
	before := len(thumbs(t))
	if _, err := thread.RemoveLibrary(pr, caller.Caller{Kind: caller.Human}, th.ID, "gone.png"); err != nil {
		t.Fatal(err)
	}
	if got := len(thumbs(t)); got != before-1 {
		t.Errorf("%d thumbnails after a delete, from %d", got, before)
	}

	// Packed: the same thumbnail, without unpacking; with the cache gone,
	// made from the tarball; a corrupt file still refused.
	r, err := thread.Load(pr, th.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.State = thread.Resolved
	os.Remove(filepath.Join(lib, "link.png"))
	if err := thread.Archive(pr, r); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(thread.Dir(pr, th.ID)); err == nil {
		t.Fatal("not packed")
	}
	stamp := thumbs(t)
	wantThumb(t, app, pr, th.ID, "shot.png", 400, 200)
	if got := thumbs(t); len(got) != len(stamp) {
		t.Errorf("packing made it again: %v, was %v", got, stamp)
	}
	d, _ := home.ThumbCacheDir()
	os.RemoveAll(d)
	wantThumb(t, app, pr, th.ID, "shot.png", 400, 200)
	wantNoThumb(t, app, pr, th.ID, "corrupt.png", proto.ErrNoThumbnail)
	wantNoThumb(t, app, pr, th.ID, "gone.png", "unknown-file")

	// Deleting from the tarball takes it too.
	before = len(thumbs(t))
	if _, err := thread.RemoveLibrary(pr, caller.Caller{Kind: caller.Human}, th.ID, "shot.png"); err != nil {
		t.Fatal(err)
	}
	if got := len(thumbs(t)); got != before-1 {
		t.Errorf("%d thumbnails after a packed delete, from %d", got, before)
	}
}

// TestLibraryThumbnailLinkedFile: a file a linked project sent is named
// by its message, as library.read names it.
func TestLibraryThumbnailLinkedFile(t *testing.T) {
	p := testPaths(t)
	d, err := home.Dir()
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(d, "config.toml"), []byte("[projects.a.links]\nb = \"both\"\n\n[projects.b.links]\na = \"both\"\n"), 0o600)
	startServer(t, p)
	a, err := project.New(project.Options{Slug: "a"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := project.New(project.Options{Slug: "b"})
	if err != nil {
		t.Fatal(err)
	}
	th, err := thread.Create(a, thread.Record{Title: "Shots", State: thread.Running})
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(thread.Path(a, th.ID, "library"), 0o755)
	os.WriteFile(thread.Path(a, th.ID, "library", "shot.png"), testPNG(t, 600, 600), 0o644)
	m, err := link.Send(caller.Caller{Kind: caller.Human}, a, "b", link.Opts{Text: "see", File: th.ID + "/shot.png"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := link.AcceptFile(caller.Caller{Kind: caller.Human}, b, m.Transfer); err != nil {
		t.Fatal(err)
	}
	app, err := client.DialAs(p, proto.ClientApp)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	wantThumb(t, app, b, m.ID, "shot.png", 400, 400)
	wantNoThumb(t, app, b, m.ID, "other.png", "unknown-file")

	before := len(thumbs(t))
	if err := link.RemoveFile(b, caller.Caller{Kind: caller.Human}, m.ID, "shot.png"); err != nil {
		t.Fatal(err)
	}
	if got := len(thumbs(t)); got != before-1 {
		t.Errorf("%d thumbnails after a delete, from %d", got, before)
	}
}
