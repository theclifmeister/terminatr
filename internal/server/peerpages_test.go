package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// TestPeerPagesAllowlist: of a task and a library file with every field
// set, a paired server's pages get exactly the fields decided on. A
// field added to tasks.Task, tasks.Step or thread.LibFile fails here
// until it is decided: copied in peerpages.go (and to the proto type),
// or listed below as staying home. The proto types are pinned too, so a
// field added to what crosses is a decision as well.
func TestPeerPagesAllowlist(t *testing.T) {
	var task tasks.Task
	fillAll(reflect.ValueOf(&task).Elem())
	task.Status = tasks.Ready
	pt := peerPageTaskOf(&task)
	if z := zeroFields(reflect.ValueOf(pt)); len(z) > 0 {
		t.Errorf("task fields not copied: %v", z)
	}
	// Title, status ("ready"), thread, created, updated and the step's
	// text: never the notes, owner, repo or design.
	if b, _ := json.Marshal(pt); strings.Count(string(b), "SECRET") != 5 {
		t.Errorf("a task's other words crossed: %s", b)
	}
	// Priority (T927) stays home: a paired server's board lists in its
	// own order, without one.
	if want := []string{"After", "Archived", "Created", "Design", "ID", "Notes", "Owner", "Priority", "Repo", "Status", "Steps", "Thread", "Title", "Updated"}; !slices.Equal(fieldNames(reflect.TypeFor[tasks.Task]()), want) {
		t.Errorf("tasks.Task has %v: decide what of a new field a paired server's board sees (peerPageTaskOf)", fieldNames(reflect.TypeFor[tasks.Task]()))
	}
	if want := []string{"Done", "N", "Text"}; !slices.Equal(fieldNames(reflect.TypeFor[tasks.Step]()), want) {
		t.Errorf("tasks.Step has %v: decide what a paired server sees (peerPageTaskOf)", fieldNames(reflect.TypeFor[tasks.Step]()))
	}

	var f thread.LibFile
	fillAll(reflect.ValueOf(&f).Elem())
	pf := peerLibraryFileOf(f)
	if z := zeroFields(reflect.ValueOf(pf)); len(z) > 0 {
		t.Errorf("library file fields not copied: %v", z)
	}
	if want := []string{"Message", "Name", "Peer", "Size", "State", "Task", "Thread", "Time", "Title"}; !slices.Equal(fieldNames(reflect.TypeFor[thread.LibFile]()), want) {
		t.Errorf("thread.LibFile has %v: decide what a paired server's library sees (peerLibraryFileOf)", fieldNames(reflect.TypeFor[thread.LibFile]()))
	}

	for typ, want := range map[reflect.Type][]string{
		reflect.TypeFor[proto.PeerPages]():       {"Now", "Overview", "Project", "Tasks", "Truncated"},
		reflect.TypeFor[proto.PeerOverview]():    {"Body", "Goal", "Repos"},
		reflect.TypeFor[proto.PeerPageTask]():    {"Created", "ID", "Status", "Steps", "Thread", "Title", "Updated"},
		reflect.TypeFor[proto.PeerStep]():        {"Done", "Text"},
		reflect.TypeFor[proto.PeerLibrary]():     {"Files"},
		reflect.TypeFor[proto.PeerLibraryFile](): {"Name", "Size", "State", "Task", "Thread", "Time", "Title"},
	} {
		if got := fieldNames(typ); !slices.Equal(got, want) {
			t.Errorf("%s crosses %v, decided %v: a field added to the pages is a decision (§16.17)", typ, got, want)
		}
	}
}

// TestPeerPagesNeverLeaks: through a real link, a paired server whose
// project pages switch here is on for web reads its board (titles,
// statuses, steps; no notes or owners), its overview (goal and
// PROJECT.md; how many repositories, never where) and its threads'
// library files, and reads one of those. Nothing of the memory crosses;
// a link's file can't be read; a project not shared answers not-shared
// even with the switch on; off refuses every page with pages-off, the
// same whether the project exists or not; the project list says which
// projects' pages are on.
func TestPeerPagesNeverLeaks(t *testing.T) {
	testPaths(t)
	repo := filepath.Join(t.TempDir(), "SECRETREPO")
	os.MkdirAll(repo, 0o755)
	for _, o := range []project.Options{{Slug: "web", Goal: "the web shop", Repos: []string{repo}}, {Slug: "secret-proj"}} {
		if _, err := project.New(o); err != nil {
			t.Fatal(err)
		}
	}
	if err := config.SetProject("web", "share", true); err != nil {
		t.Fatal(err)
	}
	web, _ := project.Open("web")
	pm, _ := os.ReadFile(web.Path("PROJECT.md"))
	os.WriteFile(web.Path("PROJECT.md"), append(pm, []byte("\nKeep the shop fast.\n")...), 0o644)
	if _, err := web.Tasks().Add(human, []tasks.NewTask{
		{Title: "Ship it", Status: "ready", Notes: "SECRET-NOTES", Steps: []string{"write it", "test it"}, Owner: "SECRET-OWNER"},
		{Title: "Old", Status: "done"},
	}); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(web.Path("memory"), 0o755)
	os.WriteFile(web.Path("memory", "build.md"), []byte("+++\ntitle = \"SECRET-NOTE-TITLE\"\n+++\nSECRET-BODY\n"), 0o644)
	os.WriteFile(web.Path("MEMORY.md"), []byte("SECRET-MEMORY\n"), 0o644)
	os.WriteFile(web.Path("CONTEXT.md"), []byte("SECRET-CONTEXT\n"), 0o644)
	r, err := thread.Create(web, thread.Record{Title: "Report", State: thread.Stopped})
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(thread.Path(web, r.ID, "library"), 0o755)
	os.WriteFile(thread.Path(web, r.ID, "library", "notes.md"), []byte("# the findings\n"), 0o644)
	os.WriteFile(thread.Path(web, r.ID, "library", ".hidden"), []byte("SECRET-HIDDEN"), 0o644)

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
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	off := linkedServers(t, ex)
	if !slices.Contains(off.Status().Peers[0].Caps, peer.CapPages) {
		t.Fatalf("mini serves pages, but its hello says %v", off.Status().Peers[0].Caps)
	}
	for _, slug := range []string{"web", "secret-proj", "nope"} {
		if _, err := off.Pages(ctx, "mini", slug); code(err) != proto.ErrPagesOff {
			t.Errorf("pages off, %s: %v, want pages-off", slug, err)
		}
	}
	if _, err := off.Library(ctx, "mini", "web"); code(err) != proto.ErrPagesOff {
		t.Errorf("library, pages off: %v, want pages-off", err)
	}
	if _, err := off.LibraryRead(ctx, "mini", proto.PeerLibraryReadParams{Project: "web", Thread: r.ID, Name: "notes.md"}); code(err) != proto.ErrPagesOff {
		t.Errorf("library read, pages off: %v, want pages-off", err)
	}
	var list proto.PeerProjects
	if err := off.Call(ctx, "mini", proto.MethodPeerProjects, nil, &list); err != nil || len(list.Projects) != 1 || list.Projects[0].Pages {
		t.Errorf("projects, pages off: %+v %v", list, err)
	}

	on := linkedServersWith(t, ex, func(p *peer.Peer) { p.Pages = []string{"web", "secret-proj"} })
	if err := on.Call(ctx, "mini", proto.MethodPeerProjects, nil, &list); err != nil || len(list.Projects) != 1 || !list.Projects[0].Pages {
		t.Errorf("projects, pages on: %+v %v", list, err)
	}
	var raw json.RawMessage
	if err := on.Call(ctx, "mini", proto.MethodPeerPages, proto.PeerPagesParams{Project: "web"}, &raw); err != nil {
		t.Fatal(err)
	}
	var pp proto.PeerPages
	json.Unmarshal(raw, &pp)
	if pp.Project != "web" || pp.Overview.Goal != "the web shop" || pp.Overview.Repos != 1 || !strings.Contains(pp.Overview.Body, "Keep the shop fast.") || pp.Truncated {
		t.Errorf("overview: %+v", pp)
	}
	if len(pp.Tasks) != 2 || pp.Tasks[0].Title != "Ship it" || pp.Tasks[0].Status != "ready" || len(pp.Tasks[0].Steps) != 2 ||
		pp.Tasks[0].Steps[1].Text != "test it" || pp.Tasks[1].Status != "done" {
		t.Errorf("board: %+v", pp.Tasks)
	}
	var libRaw json.RawMessage
	if err := on.Call(ctx, "mini", proto.MethodPeerPagesLibrary, proto.PeerPagesParams{Project: "web"}, &libRaw); err != nil {
		t.Fatal(err)
	}
	var lib proto.PeerLibrary
	json.Unmarshal(libRaw, &lib)
	if len(lib.Files) != 1 || lib.Files[0].Name != "notes.md" || lib.Files[0].Thread != r.ID {
		t.Errorf("library: %+v", lib)
	}
	chunk, err := on.LibraryRead(ctx, "mini", proto.PeerLibraryReadParams{Project: "web", Thread: r.ID, Name: "notes.md"})
	if err != nil || string(chunk.Data) != "# the findings\n" || chunk.Size != 15 {
		t.Errorf("library read: %+v %v", chunk, err)
	}
	for _, bad := range []proto.PeerLibraryReadParams{
		{Project: "web", Thread: r.ID, Name: ".hidden"},
		{Project: "web", Thread: r.ID, Name: "../thread.toml"},
		{Project: "web", Thread: "M-AB12-1", Name: "x.pdf"},
		{Project: "web", Thread: "t-0099", Name: "notes.md"},
	} {
		if c, err := on.LibraryRead(ctx, "mini", bad); err == nil {
			t.Errorf("%+v read: %q", bad, c.Data)
		}
	}
	// On for a project that isn't shared: not-shared, as any other.
	if _, err := on.Pages(ctx, "mini", "secret-proj"); code(err) != proto.ErrNotShared {
		t.Errorf("secret-proj, pages on but not shared: %v, want not-shared", err)
	}
	if _, err := on.Library(ctx, "mini", "secret-proj"); code(err) != proto.ErrNotShared {
		t.Errorf("secret-proj library: %v, want not-shared", err)
	}
	// Memory has no method.
	if err := on.Call(ctx, "mini", proto.MethodMemoryList, proto.ProjectFilesParams{Project: "web"}, &raw); code(err) != proto.ErrUnknownMethod {
		t.Errorf("memory.list over the link: %v, want unknown-method", err)
	}
	for _, never := range []string{"SECRET", "secret-proj", repo, "/Users/", `"notes"`, `"owner"`, `"repo"`, `"memory"`, "MEMORY.md", "CONTEXT.md", ".hidden"} {
		for _, b := range []json.RawMessage{raw, libRaw} {
			if strings.Contains(string(b), never) {
				t.Errorf("%q crossed:\n%s", never, b)
			}
		}
	}
}

// TestPeerPageTasksBudget: a board too long to cross whole drops its
// done tasks first, keeps board order, and says so.
func TestPeerPageTasksBudget(t *testing.T) {
	var ts []*tasks.Task
	for i := 1; i <= 6; i++ {
		st := tasks.Ready
		if i%2 == 0 {
			st = tasks.Done
		}
		ts = append(ts, &tasks.Task{ID: i, Title: strings.Repeat("x", 100), Status: st})
	}
	all, cut := peerPageTasks(ts, 1<<20)
	if len(all) != 6 || cut {
		t.Fatalf("everything fits: %d tasks, truncated %v", len(all), cut)
	}
	one := pageTaskBytes(all[0])
	got, cut := peerPageTasks(ts, 4*one)
	var ids []int
	for _, pt := range got {
		ids = append(ids, pt.ID)
	}
	if !cut || !slices.Equal(ids, []int{1, 2, 3, 5}) {
		t.Errorf("budget for four: %v (truncated %v), want the three live ones and the first done, in board order", ids, cut)
	}
}

// TestPeerPagesOlderPeer: a paired server whose tm serves no pages (its
// hello doesn't list pages) is never asked: no-pages, which the console
// words as "not shared", and its projects say no pages.
func TestPeerPagesOlderPeer(t *testing.T) {
	testPaths(t)
	if _, err := project.New(project.Options{Slug: "web"}); err != nil {
		t.Fatal(err)
	}
	config.SetProject("web", "share", true)
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
	// Only the Exporter's own methods: a tm from before pages.
	old := struct{ peer.Exporter }{ex}
	studio := linkedServersWith(t, old, func(p *peer.Peer) { p.Pages = []string{"web"} })
	if slices.Contains(studio.Status().Peers[0].Caps, peer.CapPages) {
		t.Fatalf("an old exporter's hello says %v", studio.Status().Peers[0].Caps)
	}
	ctx := context.Background()
	if _, err := studio.Pages(ctx, "mini", "web"); code(err) != proto.ErrNoPages {
		t.Errorf("pages of an old peer: %v, want no-pages", err)
	}
	if _, err := studio.Library(ctx, "mini", "web"); code(err) != proto.ErrNoPages {
		t.Errorf("library of an old peer: %v, want no-pages", err)
	}
	if pe := pagesError("mini", peer.ErrNoPages); pe.Code != proto.ErrNoPages {
		t.Errorf("words: %+v", pe)
	}
	var list proto.PeerProjects
	if err := studio.Call(ctx, "mini", proto.MethodPeerProjects, nil, &list); err != nil || len(list.Projects) != 1 || list.Projects[0].Pages {
		t.Errorf("an old peer's projects: %+v %v", list, err)
	}
}
