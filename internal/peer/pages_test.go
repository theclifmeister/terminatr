package peer

import (
	"context"
	"crypto/ed25519"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// pagesExport is fakeExport serving web's pages.
type pagesExport struct {
	*fakeExport
}

func (f *pagesExport) Projects(c caller.Caller) ([]proto.PeerProject, error) {
	return []proto.PeerProject{{Project: "web"}, {Project: "api"}}, nil
}

func (f *pagesExport) Pages(c caller.Caller, slug string) (proto.PeerPages, error) {
	if slug != "web" {
		return proto.PeerPages{}, proto.Errorf(proto.ErrNotShared, "no shared project %q", slug)
	}
	return proto.PeerPages{Project: "web", Overview: proto.PeerOverview{Goal: "the shop"}, Tasks: []proto.PeerPageTask{{ID: 1, Title: "x", Status: "ready"}}}, nil
}

func (f *pagesExport) Library(c caller.Caller, slug string) (proto.PeerLibrary, error) {
	return proto.PeerLibrary{Files: []proto.PeerLibraryFile{{Thread: "t-0001", Name: "a.md"}}}, nil
}

func (f *pagesExport) LibraryRead(c caller.Caller, p proto.PeerLibraryReadParams) (proto.LibraryReadResult, error) {
	return proto.LibraryReadResult{Name: p.Name, Size: 2, Data: []byte("hi")}, nil
}

// pagesPair is a session to a dispatcher whose pages switch is on for
// the projects in *on.
func pagesPair(t *testing.T, ex Exporter, on *[]string) *session {
	t.Helper()
	ca, cb, err := tcpPair()
	if err != nil {
		t.Fatal(err)
	}
	sa, sb := newSession(ca), newSession(cb)
	sa.ping, sb.ping = time.Hour, time.Hour
	var mu sync.Mutex
	sb.methods = (&dispatcher{ex: ex, caller: caller.ForPeer("studio"), pages: func(p string) bool {
		mu.Lock()
		defer mu.Unlock()
		return slices.Contains(*on, p)
	}}).methods()
	go sa.serve(func(time.Duration) {})
	go sb.serve(func(time.Duration) {})
	t.Cleanup(func() { ca.Close(); cb.Close() })
	return sa
}

// TestPeerPagesGate: each page answers only while the switch is on for
// its project, pages-off otherwise (whatever the project), the switch
// read at each request; peer.projects says it per project; an exporter
// without pages has no such methods at all.
func TestPeerPagesGate(t *testing.T) {
	ex := &pagesExport{fakeExport: newFakeExport()}
	var on []string
	a := pagesPair(t, ex, &on)
	ctx := context.Background()
	calls := []struct {
		method string
		params any
	}{
		{proto.MethodPeerPages, proto.PeerPagesParams{Project: "web"}},
		{proto.MethodPeerPagesLibrary, proto.PeerPagesParams{Project: "web"}},
		{proto.MethodPeerPagesLibraryRead, proto.PeerLibraryReadParams{Project: "web", Thread: "t-0001", Name: "a.md"}},
	}
	for _, c := range calls {
		if err := a.call(ctx, c.method, c.params, nil); code(err) != proto.ErrPagesOff {
			t.Errorf("%s off: %v, want pages-off", c.method, err)
		}
	}
	on = []string{"api"}
	if err := a.call(ctx, proto.MethodPeerPages, proto.PeerPagesParams{Project: "web"}, nil); code(err) != proto.ErrPagesOff {
		t.Errorf("on for another project: %v, want pages-off", err)
	}
	on = []string{"web"}
	for _, c := range calls {
		if err := a.call(ctx, c.method, c.params, nil); err != nil {
			t.Errorf("%s on: %v", c.method, err)
		}
	}
	var pp proto.PeerPages
	if err := a.call(ctx, proto.MethodPeerPages, proto.PeerPagesParams{Project: "web"}, &pp); err != nil || pp.Overview.Goal != "the shop" || pp.Now.IsZero() {
		t.Errorf("pages: %+v %v", pp, err)
	}
	if err := a.call(ctx, proto.MethodPeerPagesLibraryRead, proto.PeerLibraryReadParams{Project: "web", Thread: "t-0001", Name: "a.md", Offset: -1}, nil); code(err) != proto.ErrBadParams {
		t.Errorf("negative offset: %v", err)
	}
	var list proto.PeerProjects
	if err := a.call(ctx, proto.MethodPeerProjects, nil, &list); err != nil || len(list.Projects) != 2 || !list.Projects[0].Pages || list.Projects[1].Pages {
		t.Errorf("projects: %+v %v", list, err)
	}
	// None of them writes, and the memory has no method.
	plain := pagesPair(t, newFakeExport(), &on)
	for _, m := range []string{proto.MethodPeerPages, proto.MethodPeerPagesLibrary, proto.MethodPeerPagesLibraryRead, proto.MethodMemoryList, proto.MethodMemoryRead} {
		if err := plain.call(ctx, m, proto.PeerPagesParams{Project: "web"}, nil); code(err) != proto.ErrUnknownMethod {
			t.Errorf("%s without pages: %v", m, err)
		}
	}
	if err := a.call(ctx, proto.MethodMemoryList, proto.PeerPagesParams{Project: "web"}, nil); code(err) != proto.ErrUnknownMethod {
		t.Errorf("memory.list: %v", err)
	}
}

// TestPagesCap: a server whose exporter serves pages says pages in its
// hello, one whose exporter doesn't, not.
func TestPagesCap(t *testing.T) {
	with := &Manager{opts: Options{Export: &pagesExport{fakeExport: newFakeExport()}}}
	without := &Manager{opts: Options{Export: newFakeExport()}}
	if !slices.Contains(with.caps(), CapPages) || slices.Contains(without.caps(), CapPages) {
		t.Errorf("caps: %v and %v", with.caps(), without.caps())
	}
	if n := len(with.caps()); n > maxCaps {
		t.Errorf("%d caps, over the %d a hello keeps", n, maxCaps)
	}
}

// TestPagesAreCleaned: a peer's pages arrive with no escape or bidi
// override in any string, a title and a step on one line, ids checked,
// the body kept (past the 4 KiB any other string is cut to) up to its
// most, and each list cut to its most.
func TestPagesAreCleaned(t *testing.T) {
	body := strings.Repeat("line of the body\n", 1000) + osc52
	in := proto.PeerPages{Project: "web", Overview: proto.PeerOverview{Goal: "ship\u202e\nnow", Body: body, Repos: -1},
		Tasks: []proto.PeerPageTask{
			{ID: 1, Title: "fix\x1b[2J it\nnow", Status: "ready", Thread: "t-0001", Steps: []proto.PeerStep{{Text: "a\nb" + osc52, Done: true}}},
			{ID: 0, Title: "no id"},
			{ID: 2, Title: "bad", Status: "re view", Thread: "../t-1"},
		}}
	for i := range proto.PeerPagesMaxSteps + 3 {
		in.Tasks[0].Steps = append(in.Tasks[0].Steps, proto.PeerStep{Text: strings.Repeat("s", i)})
	}
	for i := range proto.PeerPagesMaxTasks + 5 {
		in.Tasks = append(in.Tasks, proto.PeerPageTask{ID: 10 + i, Title: "t"})
	}
	p := cleanPages(in)
	if hasEscape(t, p) {
		t.Fatalf("an escape crossed")
	}
	if strings.Contains(p.Overview.Goal, "\n") || p.Overview.Repos != 0 {
		t.Errorf("overview: %q %d", p.Overview.Goal, p.Overview.Repos)
	}
	if len(p.Overview.Body) < 16000 || len(p.Overview.Body) > proto.PeerPagesMaxBody || !strings.Contains(p.Overview.Body, "\n") {
		t.Errorf("body: %d bytes", len(p.Overview.Body))
	}
	if len(p.Tasks) != proto.PeerPagesMaxTasks || !p.Truncated || p.Tasks[0].ID != 1 || p.Tasks[1].ID != 2 {
		t.Fatalf("tasks: %d, truncated %v", len(p.Tasks), p.Truncated)
	}
	t0, t1 := p.Tasks[0], p.Tasks[1]
	if strings.Contains(t0.Title, "\n") || len(t0.Steps) != proto.PeerPagesMaxSteps || strings.Contains(t0.Steps[0].Text, "\n") || !t0.Steps[0].Done {
		t.Errorf("task 1: %+v", t0)
	}
	if t1.Thread != "" || t1.Status != "" {
		t.Errorf("task 2: %+v", t1)
	}
	lib := cleanLibrary(proto.PeerLibrary{Files: []proto.PeerLibraryFile{
		{Thread: "t-0001", Name: "a\nb.md" + osc52, Size: -4, State: "x y"}, {Thread: "../x", Name: "c"}, {Thread: "t-0002"},
	}})
	if hasEscape(t, lib) || len(lib.Files) != 1 || strings.Contains(lib.Files[0].Name, "\n") || lib.Files[0].Size != 0 || lib.Files[0].State != "" {
		t.Errorf("library: %+v", lib)
	}
}

// TestSetPages: the switch is on for each project named, off for every
// other, kept when the server is paired again, and refuses what isn't a
// project's slug.
func TestSetPages(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "peer")
	pubKey, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	p := Peer{Alias: "mini", Key: pubKey, Relay: "wss://relay.example"}
	if err := SavePeers(dir, []Peer{p}); err != nil {
		t.Fatal(err)
	}
	for _, s := range []struct {
		slug string
		on   bool
	}{{"web", true}, {"api", true}, {"web", true}, {"api", false}} {
		if found, err := SetPages(dir, "mini", s.slug, s.on); !found || err != nil {
			t.Fatalf("%v: %v %v", s, found, err)
		}
	}
	peers, _ := LoadPeers(dir)
	if !slices.Equal(peers[0].Pages, []string{"web"}) || !peers[0].PagesOn("web") || peers[0].PagesOn("api") || peers[0].PagesOn("") {
		t.Errorf("pages: %v", peers[0].Pages)
	}
	if _, err := SetPages(dir, "mini", "../etc", true); err == nil {
		t.Error("a path was taken for a project")
	}
	if found, _ := SetPages(dir, "nobody", "web", true); found {
		t.Error("found a peer that isn't")
	}
	bad := p
	bad.Pages = []string{"Not A Slug"}
	if err := bad.check(); err == nil {
		t.Error("a bad slug passed the check")
	}
}
