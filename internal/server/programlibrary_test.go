package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// TestSeatLibraryFiles: library.list lists a coordinator's own files as
// thread seat, library.read reads one and library.thumbnail answers for
// one; a path is refused as for a thread's.
func TestSeatLibraryFiles(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	pr, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(pr.Path("library"), 0o755)
	os.WriteFile(pr.Path("library", "site.html"), []byte("<h1>Site</h1>"), 0o644)
	app, err := client.DialAs(p, proto.ClientApp)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	var lib proto.LibraryListResult
	if err := app.Call(proto.MethodLibraryList, proto.ProjectFilesParams{Project: "web"}, &lib); err != nil {
		t.Fatal(err)
	}
	if len(lib.Files) != 1 || lib.Files[0].Thread != proto.LibrarySeatID || lib.Files[0].Title != "Coordinator's files" {
		t.Fatalf("library.list: %+v", lib)
	}
	var read proto.LibraryReadResult
	if err := app.Call(proto.MethodLibraryRead, proto.LibraryReadParams{Project: "web", ID: proto.LibrarySeatID, Name: "site.html"}, &read); err != nil || string(read.Data) != "<h1>Site</h1>" {
		t.Fatalf("library.read: %+v %v", read, err)
	}
	for _, bad := range []string{"../PROJECT.md", "..", "a/b", ".hidden"} {
		var perr *proto.Error
		err := app.Call(proto.MethodLibraryRead, proto.LibraryReadParams{Project: "web", ID: proto.LibrarySeatID, Name: bad}, &read)
		if !errors.As(err, &perr) || perr.Code != "invalid-file" {
			t.Errorf("library.read %q: %v", bad, err)
		}
	}
	var perr *proto.Error
	if err := app.Call(proto.MethodLibraryRead, proto.LibraryReadParams{Project: "web", ID: proto.LibrarySeatID, Name: "nope.html"}, &read); !errors.As(err, &perr) || perr.Code != "unknown-file" {
		t.Errorf("a missing file: %v", err)
	}
}

// TestProgramLibrary: library.list's program form lists the director's own
// files first, then each member's in the program's order, each marked with
// its project; a paired member that can't be read says so in the note. Its
// read and thumbnail name the director's own file and no other path; a
// member's file is read as any file of its project. Off, programs-off.
func TestProgramLibrary(t *testing.T) {
	p := testPaths(t)
	web, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	api, err := project.New(project.Options{Slug: "api"})
	if err != nil {
		t.Fatal(err)
	}
	g, err := program.New(program.Options{Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	writeConfig(t, "[features]\nprograms = true\n[programs.acme]\nprojects = [\"web\", \"api\", \"mini/far\"]\n")
	os.MkdirAll(g.Path("library"), 0o755)
	os.WriteFile(g.Path("library", "plan.md"), []byte("# Plan\n"), 0o644)
	os.MkdirAll(api.Path("library"), 0o755)
	os.WriteFile(api.Path("library", "api.md"), []byte("# API\n"), 0o644)
	th, err := thread.Create(web, thread.Record{Title: "Report", Task: "T1", State: thread.Running})
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(thread.Path(web, th.ID, "library"), 0o755)
	os.WriteFile(thread.Path(web, th.ID, "library", "notes.md"), []byte("# Notes\n"), 0o644)
	startServer(t, p)
	app, err := client.DialAs(p, proto.ClientApp)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	var lib proto.LibraryListResult
	if err := app.Call(proto.MethodLibraryList, proto.ProjectFilesParams{Program: "acme"}, &lib); err != nil {
		t.Fatal(err)
	}
	type row struct{ name, project, id string }
	want := []row{{"plan.md", "", proto.LibrarySeatID}, {"notes.md", "web", th.ID}, {"api.md", "api", proto.LibrarySeatID}}
	if len(lib.Files) != len(want) {
		t.Fatalf("library.list: %+v", lib)
	}
	for i, w := range want {
		if f := lib.Files[i]; f.Name != w.name || f.Project != w.project || f.Thread != w.id || f.Server != "" {
			t.Errorf("file %d: %+v, want %+v", i, f, w)
		}
	}
	if lib.Note != "far on mini can't be read now" {
		t.Errorf("note %q", lib.Note)
	}

	var read proto.LibraryReadResult
	if err := app.Call(proto.MethodLibraryRead, proto.LibraryReadParams{Program: "acme", ID: proto.LibrarySeatID, Name: "plan.md"}, &read); err != nil || string(read.Data) != "# Plan\n" {
		t.Fatalf("read the director's: %+v %v", read, err)
	}
	if err := app.Call(proto.MethodLibraryRead, proto.LibraryReadParams{Project: "api", ID: proto.LibrarySeatID, Name: "api.md"}, &read); err != nil || string(read.Data) != "# API\n" {
		t.Fatalf("read a member's: %+v %v", read, err)
	}
	code := func(err error) string {
		var perr *proto.Error
		if errors.As(err, &perr) {
			return perr.Code
		}
		return ""
	}
	for _, c := range []struct {
		params proto.LibraryReadParams
		want   string
	}{
		{proto.LibraryReadParams{Program: "acme", ID: th.ID, Name: "notes.md"}, proto.ErrBadParams},
		{proto.LibraryReadParams{Program: "acme", Project: "web", ID: proto.LibrarySeatID, Name: "plan.md"}, proto.ErrBadParams},
		{proto.LibraryReadParams{Program: "acme", ID: proto.LibrarySeatID, Name: "../PROGRAM.md"}, "invalid-file"},
		{proto.LibraryReadParams{Program: "../acme", ID: proto.LibrarySeatID, Name: "plan.md"}, "invalid-program"},
	} {
		if err := app.Call(proto.MethodLibraryRead, c.params, &read); code(err) != c.want {
			t.Errorf("%+v: %v, want %s", c.params, err, c.want)
		}
	}
	var th2 proto.LibraryThumbnailResult
	if err := app.Call(proto.MethodLibraryThumbnail, proto.LibraryThumbnailParams{Program: "acme", ID: proto.LibrarySeatID, Name: "plan.md"}, &th2); code(err) != proto.ErrNoThumbnail {
		t.Errorf("thumbnail of a note: %v", err)
	}
	if err := app.Call(proto.MethodLibraryList, proto.ProjectFilesParams{Program: "acme", Project: "web"}, &lib); code(err) != proto.ErrBadParams {
		t.Errorf("both: %v", err)
	}

	writeConfig(t, "[programs.acme]\nprojects = [\"web\"]\n")
	if err := app.Call(proto.MethodLibraryList, proto.ProjectFilesParams{Program: "acme"}, &lib); code(err) != config.ProgramsOff {
		t.Errorf("list while off: %v", err)
	}
	if err := app.Call(proto.MethodLibraryRead, proto.LibraryReadParams{Program: "acme", ID: proto.LibrarySeatID, Name: "plan.md"}, &read); code(err) != config.ProgramsOff {
		t.Errorf("read while off: %v", err)
	}
}

// TestPairedMemberLibrary: through a real link, a director's paired member
// lists its coordinator's own files and its threads', marked with its server
// and project, and they read over the link, all while that project's pages
// switch is on there. Off, the member stays out with no note. No path crosses
// and only a library's files read.
func TestPairedMemberLibrary(t *testing.T) {
	testPaths(t)
	web, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	config.SetProject("web", "share", true)
	os.MkdirAll(web.Path("library"), 0o755)
	os.WriteFile(web.Path("library", "site.html"), []byte("<h1>Site</h1>"), 0o644)
	ticker := filepath.Join(t.TempDir(), "ticker.json")
	ex := peerExport{
		sessions: func() []proto.SessionInfo { return nil },
		state: func(slug string, ss []proto.SessionInfo) (proto.ProjectWatch, error) {
			p, err := project.Open(slug)
			if err != nil {
				return proto.ProjectWatch{}, err
			}
			return ProjectWatchOf(p, ss, ticker), nil
		},
		changes: func() (<-chan struct{}, func()) { return make(chan struct{}), func() {} },
	}
	on := linkedServersWith(t, ex, func(p *peer.Peer) { p.Pages = []string{"web"} })
	s := &Server{peers: on}
	files, unread := s.pairedLibrary("mini", "web")
	if unread != "" || len(files) != 1 {
		t.Fatalf("pages on: %+v %q", files, unread)
	}
	if f := files[0]; f.Name != "site.html" || f.Thread != thread.SeatID || f.Project != "web" || f.Server != "mini" || f.Title != "Coordinator's files" {
		t.Fatalf("the file: %+v", f)
	}
	r, err := on.LibraryRead(context.Background(), "mini", proto.PeerLibraryReadParams{Project: "web", Thread: thread.SeatID, Name: "site.html"})
	if err != nil || string(r.Data) != "<h1>Site</h1>" {
		t.Fatalf("read over the link: %+v %v", r, err)
	}
	if _, err := on.LibraryRead(context.Background(), "mini", proto.PeerLibraryReadParams{Project: "web", Thread: thread.SeatID, Name: "../PROJECT.md"}); err == nil {
		t.Fatal("a path read over the link")
	}

	off := linkedServersWith(t, ex, func(p *peer.Peer) {})
	s = &Server{peers: off}
	if files, unread := s.pairedLibrary("mini", "web"); len(files) != 0 || unread != "" {
		t.Fatalf("pages off: %+v %q", files, unread)
	}
}
