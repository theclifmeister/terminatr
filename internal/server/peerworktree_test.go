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

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/gittest"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// TestPeerWorktreeAllowlist: of a file's records with every field set, a
// paired server gets exactly the fields decided on. A field added to what
// the files methods answer fails here until it is decided: copied in
// peerworktree.go (and to the proto type), or listed below as staying home.
// The proto types are pinned too, params as well: a way to ask for ignored
// files or a binary's bytes is a decision.
func TestPeerWorktreeAllowlist(t *testing.T) {
	var e proto.FileEntry
	fillAll(reflect.ValueOf(&e).Elem())
	if z := zeroFields(reflect.ValueOf(peerFileEntryOf(e))); len(z) > 0 {
		t.Errorf("entry fields not copied: %v", z)
	}

	var list proto.FilesListResult
	fillAll(reflect.ValueOf(&list).Elem())
	list.Entries[0].Ignored = false
	pl := peerWorktreeListOf(list)
	if z := zeroFields(reflect.ValueOf(pl)); len(z) > 0 {
		t.Errorf("list fields not copied: %v", z)
	}
	list.Root = "/Users/me/ROOT"
	if b, _ := json.Marshal(peerWorktreeListOf(list)); strings.Contains(string(b), "ROOT") || strings.Contains(string(b), "ignored") {
		t.Errorf("Root or Ignored crossed: %s", b)
	}
	list.Entries[0].Ignored = true
	if got := peerWorktreeListOf(list); len(got.Entries) != 0 {
		t.Errorf("an ignored entry crossed: %+v", got.Entries)
	}

	var read proto.FilesReadResult
	fillAll(reflect.ValueOf(&read).Elem())
	if z := zeroFields(reflect.ValueOf(peerWorktreeReadOf(read))); len(z) > 0 {
		t.Errorf("read fields not copied: %v", z)
	}

	var diff proto.FilesDiffResult
	fillAll(reflect.ValueOf(&diff).Elem())
	pd := peerWorktreeDiffOf(diff)
	if z := zeroFields(reflect.ValueOf(pd)); len(z) > 0 {
		t.Errorf("diff fields not copied: %v", z)
	}
	if z := zeroFields(reflect.ValueOf(pd.Hunks[0])); len(z) > 0 {
		t.Errorf("hunk fields not copied: %v", z)
	}

	for typ, want := range map[reflect.Type][]string{
		reflect.TypeFor[proto.FileEntry]():       {"Changed", "Ignored", "Kind", "Link", "LinkOut", "MTime", "Name", "Path", "Size", "Status", "Withheld"},
		reflect.TypeFor[proto.FilesListResult](): {"Added", "Deleted", "Entries", "Files", "Git", "Path", "Root", "Truncated"},
		reflect.TypeFor[proto.FilesReadResult](): {"Binary", "Data", "MTime", "Name", "Offset", "Path", "Size", "Truncated"},
		reflect.TypeFor[proto.FilesDiffResult](): {"Added", "Base", "Binary", "Deleted", "From", "Hunks", "MTime", "Name", "Path", "Size", "Status", "TooBig", "Truncated"},
		reflect.TypeFor[proto.FilesHunk]():       {"Added", "New", "Old", "Removed"},
	} {
		if got := fieldNames(typ); !slices.Equal(got, want) {
			t.Errorf("%s has %v: decide what of a new field a paired server sees (peerworktree.go)", typ, got)
		}
	}
	for typ, want := range map[reflect.Type][]string{
		reflect.TypeFor[proto.PeerWorktreeParams]():      {"Project", "Session"},
		reflect.TypeFor[proto.PeerWorktreeRepos]():       {"Repos"},
		reflect.TypeFor[proto.PeerWorktreeListParams]():  {"Changed", "Path", "Project", "Query", "Repo", "Session"},
		reflect.TypeFor[proto.PeerWorktreeList]():        {"Added", "Deleted", "Entries", "Files", "Git", "Path", "Truncated"},
		reflect.TypeFor[proto.PeerFileEntry]():           {"Changed", "Kind", "Link", "LinkOut", "MTime", "Name", "Path", "Size", "Status", "Withheld"},
		reflect.TypeFor[proto.PeerWorktreeReadParams]():  {"Offset", "Path", "Project", "Repo", "Session"},
		reflect.TypeFor[proto.PeerWorktreeRead]():        {"Binary", "Data", "MTime", "Name", "Offset", "Path", "Size", "Truncated"},
		reflect.TypeFor[proto.PeerWorktreeStampParams](): {"Folders", "Project", "Repo", "Session"},
		reflect.TypeFor[proto.PeerWorktreeStamp]():       {"Stamp"},
		reflect.TypeFor[proto.PeerWorktreeDiffParams]():  {"Path", "Project", "Repo", "Session"},
		reflect.TypeFor[proto.PeerWorktreeDiff]():        {"Added", "Base", "Binary", "Deleted", "From", "Hunks", "MTime", "Name", "Path", "Size", "Status", "TooBig", "Truncated"},
		reflect.TypeFor[proto.PeerFileHunk]():            {"Added", "New", "Old", "Removed"},
	} {
		if got := fieldNames(typ); !slices.Equal(got, want) {
			t.Errorf("%s crosses %v, decided %v: a field added to a paired server's files is a decision", typ, got, want)
		}
	}
}

// worktreeFix is a shared project web with a thread's worktree, a second
// listed repo and sessions of every kind, and its exporter.
type worktreeFix struct {
	ex   peerExport
	web  *project.Project
	main string
}

func newWorktreeFix(t *testing.T) *worktreeFix {
	t.Helper()
	testPaths(t)
	main, g := gittest.Repo(t)
	main, _ = filepath.EvalSymlinks(main)
	write := func(name, text string) {
		t.Helper()
		p := filepath.Join(main, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", "build/\n")
	write("README.md", "one\ntwo\n")
	write(".env", "SECRET\n")
	write("old.key", strings.Repeat("SECRET line\n", 30))
	g("add", ".")
	g("commit", "-qm", "c")
	second, g2 := gittest.Repo(t)
	second, _ = filepath.EvalSymlinks(second)
	if err := os.WriteFile(filepath.Join(second, "SECOND.md"), []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g2("add", ".")
	g2("commit", "-qm", "c")
	offList, _ := gittest.Repo(t)

	tree := filepath.Join(t.TempDir(), "tree")
	g("worktree", "add", "-q", "-b", "thread", tree)
	tree, _ = filepath.EvalSymlinks(tree)
	wt := func(name, text string) {
		t.Helper()
		p := filepath.Join(tree, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wt("README.md", "one\nTHREE\n")
	wt("build/out.txt", "SECRET\n")
	wt("bin.dat", "\x00SECRET")
	gittest.Run(t, tree, "mv", "old.key", "renamed.txt")
	if err := os.Symlink(".env", filepath.Join(tree, "notes.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/hosts", filepath.Join(tree, "out")); err != nil {
		t.Fatal(err)
	}
	wt("folder/inner.txt", "text\n")
	wt("nested/inner.txt", "SECRET\n")
	gittest.Run(t, tree, "init", "-q", filepath.Join(tree, "nested"))

	for _, o := range []project.Options{{Slug: "web", Repos: []string{main, second}}, {Slug: "other", Repos: []string{offList}}, {Slug: "quiet", Repos: []string{main}}} {
		if _, err := project.New(o); err != nil {
			t.Fatal(err)
		}
	}
	for _, slug := range []string{"web", "other"} {
		if err := config.SetProject(slug, "share", true); err != nil {
			t.Fatal(err)
		}
	}
	web, err := project.Open("web")
	if err != nil {
		t.Fatal(err)
	}
	other, err := project.Open("other")
	if err != nil {
		t.Fatal(err)
	}
	made := func(p *project.Project, rec thread.Record) string {
		t.Helper()
		r, err := thread.Create(p, rec)
		if err != nil {
			t.Fatal(err)
		}
		return r.ID
	}
	running := made(web, thread.Record{Title: "t", State: thread.Stopped, Repo: main, Worktree: tree})
	plain := made(web, thread.Record{Title: "plain", State: thread.Stopped, Worktree: t.TempDir()})
	adopted := made(web, thread.Record{Title: "adopted", State: thread.Stopped, Repo: offList, Worktree: offList, Adopted: true, Session: "s-adopted"})
	gone := made(web, thread.Record{Title: "gone", State: thread.Stopped, Repo: main, Worktree: filepath.Join(t.TempDir(), "no")})
	foreign := made(other, thread.Record{Title: "theirs", State: thread.Stopped, Repo: offList, Worktree: offList})
	sessions := []proto.SessionInfo{
		{ID: "s-thread", Project: "web", Role: proto.RoleThread, Thread: running},
		{ID: "s-coord", Project: "web", Role: proto.RoleCoordinator},
		{ID: "s-plain", Project: "web", Role: proto.RoleThread, Thread: plain},
		{ID: "s-adopted", Project: "web", Role: proto.RoleThread, Thread: adopted, Cwd: offList},
		{ID: "s-gone", Project: "web", Role: proto.RoleThread, Thread: gone},
		{ID: "s-foreign", Project: "other", Role: proto.RoleThread, Thread: foreign},
		{ID: "s-director", Project: "web", Role: proto.RoleDirector},
		{ID: "s-shell", Project: "web", Role: "shell"},
		{ID: "s-quiet", Project: "quiet", Role: proto.RoleCoordinator},
		{ID: "mini/s-thread", Project: "web", Role: proto.RoleThread, Thread: running},
	}
	ex := peerExport{
		sessions:  func() []proto.SessionInfo { return sessions },
		state:     func(string, []proto.SessionInfo) (proto.ProjectWatch, error) { return proto.ProjectWatch{}, nil },
		changes:   func() (<-chan struct{}, func()) { return make(chan struct{}), func() {} },
		worktrees: &peerWorktrees{},
	}
	return &worktreeFix{ex: ex, web: web, main: main}
}

// TestPeerWorktreeRefusals: a request that breaks each rule answers that
// rule's code in its fixed words, by every method a rule can stop, with no
// path or secret in what crosses.
func TestPeerWorktreeRefusals(t *testing.T) {
	f := newWorktreeFix(t)
	ctx := context.Background()
	studio := caller.ForPeer("studio")
	read := func(c caller.Caller, session, repo, path string) error {
		_, err := f.ex.WorktreeRead(ctx, c, proto.PeerWorktreeReadParams{Project: "web", Session: session, Repo: repo, Path: path})
		return err
	}
	diff := func(session, path string) error {
		_, err := f.ex.WorktreeDiff(ctx, studio, proto.PeerWorktreeDiffParams{Project: "web", Session: session, Path: path})
		return err
	}
	list := func(session, repo, path string) error {
		_, err := f.ex.WorktreeList(ctx, studio, proto.PeerWorktreeListParams{Project: "web", Session: session, Repo: repo, Path: path})
		return err
	}
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"not a peer", read(caller.Caller{Kind: caller.Human}, "s-thread", "", "README.md"), proto.ErrFilesOff},
		{"a remote id", read(studio, "mini/s-thread", "", "README.md"), proto.ErrUnknownSession},
		{"a remote id, list", list("mini/s-thread", "", ""), proto.ErrUnknownSession},
		{"no such session", read(studio, "s-nope", "", "README.md"), proto.ErrUnknownSession},
		{"no session", read(studio, "", "", "README.md"), proto.ErrUnknownSession},
		{"another project's session", read(studio, "s-foreign", "", "README.md"), proto.ErrUnknownSession},
		{"a director", read(studio, "s-director", "", "README.md"), proto.ErrUnknownSession},
		{"a shell", read(studio, "s-shell", "", "README.md"), proto.ErrUnknownSession},
		{"a plain folder", read(studio, "s-plain", "", "README.md"), proto.ErrNoFiles},
		{"an adopted repo off the list", read(studio, "s-adopted", "", "README.md"), proto.ErrNoFiles},
		{"a worktree gone", read(studio, "s-gone", "", "README.md"), proto.ErrNoFiles},
		{"a repo for a thread", read(studio, "s-thread", "x", "README.md"), proto.ErrBadParams},
		{"a repo off the list", read(studio, "s-coord", "nope", "README.md"), proto.ErrNoFiles},
		{"a repo by its path", read(studio, "s-coord", f.main, "README.md"), proto.ErrNoFiles},
		{"a parent", read(studio, "s-thread", "", "../x"), proto.ErrInvalidPath},
		{"an absolute path", read(studio, "s-thread", "", "/etc/passwd"), proto.ErrInvalidPath},
		{".git", read(studio, "s-thread", "", ".git/config"), proto.ErrInvalidPath},
		{"an ignored folder", read(studio, "s-thread", "", "build"), proto.ErrIgnored},
		{"a folder", read(studio, "s-thread", "", "folder"), proto.ErrNotFile},
		{"a list of a file", list("s-thread", "", "README.md"), proto.ErrNotFile},
		{"a link out", read(studio, "s-thread", "", "out"), proto.ErrLinkOut},
		{"a nested checkout", read(studio, "s-thread", "", "nested/inner.txt"), proto.ErrWithheld},
		{"list of a nested checkout", list("s-thread", "", "nested"), proto.ErrWithheld},
		{"an ignored file", read(studio, "s-thread", "", "build/out.txt"), proto.ErrIgnored},
		{".env", read(studio, "s-thread", "", ".env"), proto.ErrWithheld},
		{"a link to .env", read(studio, "s-thread", "", "notes.txt"), proto.ErrWithheld},
		{"a file gone", read(studio, "s-thread", "", "no-such.txt"), proto.ErrFileGone},
		{"diff of .env", diff("s-thread", ".env"), proto.ErrWithheld},
		{"diff of a link to .env", diff("s-thread", "notes.txt"), proto.ErrWithheld},
		{"diff of a rename from a secret", diff("s-thread", "renamed.txt"), proto.ErrWithheld},
		{"list of an ignored folder", list("s-thread", "", "build"), proto.ErrIgnored},
		{"list of a parent", list("s-thread", "", ".."), proto.ErrInvalidPath},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code(tc.err) != tc.want {
				t.Fatalf("%v, want %s", tc.err, tc.want)
			}
			if want := proto.PeerWorktreeRefusal(tc.want).Message; !strings.Contains(tc.err.Error(), want) {
				t.Errorf("%v: not the fixed words %q", tc.err, want)
			}
			if strings.Contains(tc.err.Error(), "/") || strings.Contains(tc.err.Error(), "SECRET") {
				t.Errorf("what crosses: %v", tc.err)
			}
		})
	}

	// Sharing off, the switch on: not-shared, also for a project with one.
	if _, err := f.ex.WorktreeList(ctx, studio, proto.PeerWorktreeListParams{Project: "quiet", Session: "s-quiet"}); code(err) != proto.ErrNotShared {
		t.Errorf("not shared: %v", err)
	}
	if _, err := f.ex.WorktreeRepos(ctx, studio, proto.PeerWorktreeParams{Project: "quiet", Session: "s-quiet"}); code(err) != proto.ErrNotShared {
		t.Errorf("repos, not shared: %v", err)
	}
	if err := config.SetProject("web", "share", false); err != nil {
		t.Fatal(err)
	}
	if err := read(studio, "s-thread", "", "README.md"); code(err) != proto.ErrNotShared {
		t.Errorf("sharing stopped: %v", err)
	}
	if err := config.SetProject("web", "share", true); err != nil {
		t.Fatal(err)
	}

	folders := make([]string, proto.PeerWorktreeStamped+1)
	if _, err := f.ex.WorktreeStamp(ctx, studio, proto.PeerWorktreeStampParams{Project: "web", Session: "s-thread", Folders: folders}); code(err) != proto.ErrBadParams {
		t.Errorf("a stamp of too many: %v", err)
	}
}

// TestPeerWorktreeAnswers: what a thread and a coordinator do answer: text,
// marks, a diff, a stamp that doesn't look in an ignored folder, repo names
// and never a path.
func TestPeerWorktreeAnswers(t *testing.T) {
	f := newWorktreeFix(t)
	ctx := context.Background()
	studio := caller.ForPeer("studio")
	thr := proto.PeerWorktreeListParams{Project: "web", Session: "s-thread"}

	list, err := f.ex.WorktreeList(ctx, studio, thr)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]proto.PeerFileEntry{}
	for _, e := range list.Entries {
		got[e.Name] = e
	}
	if e := got["README.md"]; e.Status != proto.FileModified || e.Withheld {
		t.Errorf("README.md: %+v", e)
	}
	if !list.Git || list.Files == 0 {
		t.Errorf("list: %+v", list)
	}
	if e, ok := got[".env"]; !ok || !e.Withheld || e.Size != 0 {
		t.Errorf(".env: %+v", e)
	}
	if e, ok := got["notes.txt"]; !ok || !e.Withheld {
		t.Errorf("a link to .env: %+v", e)
	}
	if _, ok := got["build"]; ok {
		t.Error("an ignored folder listed")
	}
	if b, _ := json.Marshal(list); strings.Contains(string(b), f.main) || strings.Contains(string(b), "SECRET") {
		t.Errorf("list: %s", b)
	}

	res, err := f.ex.WorktreeRead(ctx, studio, proto.PeerWorktreeReadParams{Project: "web", Session: "s-thread", Path: "README.md"})
	if err != nil || string(res.Data) != "one\nTHREE\n" {
		t.Errorf("read: %q %v", res.Data, err)
	}
	bin, err := f.ex.WorktreeRead(ctx, studio, proto.PeerWorktreeReadParams{Project: "web", Session: "s-thread", Path: "bin.dat"})
	if err != nil || !bin.Binary || bin.Data != nil {
		t.Errorf("binary: %+v %v", bin, err)
	}
	d, err := f.ex.WorktreeDiff(ctx, studio, proto.PeerWorktreeDiffParams{Project: "web", Session: "s-thread", Path: "README.md"})
	if err != nil || d.Status != proto.FileModified || len(d.Hunks) != 1 || d.Hunks[0].Removed[0] != "two" {
		t.Errorf("diff: %+v %v", d, err)
	}

	stamp := func(folders ...string) string {
		t.Helper()
		st, err := f.ex.WorktreeStamp(ctx, studio, proto.PeerWorktreeStampParams{Project: "web", Session: "s-thread", Folders: folders})
		if err != nil {
			t.Fatal(err)
		}
		return st.Stamp
	}
	// One server's cache paces git for the stamps: another's looks now.
	fresh := func() { f.ex.worktrees = &peerWorktrees{} }
	fresh()
	before := stamp("build")
	tree := filepath.Dir(filepath.Join(treeOf(t, f), "build"))
	if err := os.WriteFile(filepath.Join(tree, "build", "more.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fresh()
	if stamp("build") != before {
		t.Error("a stamp looked in an ignored folder")
	}

	repos, err := f.ex.WorktreeRepos(ctx, studio, proto.PeerWorktreeParams{Project: "web", Session: "s-coord"})
	if err != nil || len(repos.Repos) != 2 {
		t.Fatalf("repos: %+v %v", repos, err)
	}
	if b, _ := json.Marshal(repos); strings.Contains(string(b), "/private") || strings.Contains(string(b), "/tmp") || strings.Contains(string(b), "/var") {
		t.Errorf("repos: %s", b)
	}
	if none, err := f.ex.WorktreeRepos(ctx, studio, proto.PeerWorktreeParams{Project: "web", Session: "s-thread"}); err != nil || len(none.Repos) != 0 {
		t.Errorf("a thread's repos: %+v %v", none, err)
	}
	first, err := f.ex.WorktreeList(ctx, studio, proto.PeerWorktreeListParams{Project: "web", Session: "s-coord"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.ex.WorktreeList(ctx, studio, proto.PeerWorktreeListParams{Project: "web", Session: "s-coord", Repo: repos.Repos[1]})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(entryNames(first), "README.md") || !strings.Contains(entryNames(second), "SECOND.md") {
		t.Errorf("first %s, second %s", entryNames(first), entryNames(second))
	}
}

func entryNames(l proto.PeerWorktreeList) string {
	var out []string
	for _, e := range l.Entries {
		out = append(out, e.Name)
	}
	return strings.Join(out, " ")
}

// treeOf is the fix's running thread's worktree.
func treeOf(t *testing.T, f *worktreeFix) string {
	t.Helper()
	for _, info := range f.ex.sessions() {
		if info.ID == "s-thread" {
			r, err := thread.Load(f.web, info.Thread)
			if err != nil {
				t.Fatal(err)
			}
			return r.Worktree
		}
	}
	t.Fatal("no thread")
	return ""
}

// TestPeerWorktreeOverLink: through a real link and relay: the capability in
// the hello, files-off with the switch off in the same words for a project
// real or not, reads with it on, peer.projects saying so, and a refusal in
// fixed words, with no path of this machine crossing.
func TestPeerWorktreeOverLink(t *testing.T) {
	f := newWorktreeFix(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Over the rate is the link's to say; the test waits it out.
	call := func(m *peer.Manager, method string, params, out any) error {
		for {
			err := m.Call(ctx, "mini", method, params, out)
			if code(err) != proto.ErrRateLimited {
				return err
			}
			time.Sleep(300 * time.Millisecond)
		}
	}

	off := linkedServers(t, f.ex)
	if !slices.Contains(off.Status().Peers[0].Caps, peer.CapWorktree) {
		t.Fatalf("mini serves folders, but its hello says %v", off.Status().Peers[0].Caps)
	}
	var said []string
	for _, slug := range []string{"web", "quiet", "nope"} {
		err := call(off, proto.MethodPeerWorktreeList, proto.PeerWorktreeListParams{Project: slug, Session: "s-thread"}, nil)
		if code(err) != proto.ErrFilesOff {
			t.Errorf("off, %s: %v", slug, err)
		} else {
			said = append(said, err.Error())
		}
	}
	if len(slices.Compact(said)) != 1 {
		t.Errorf("off says different things: %q", said)
	}
	var ps proto.PeerProjects
	if err := call(off, proto.MethodPeerProjects, nil, &ps); err != nil || len(ps.Projects) == 0 || slices.ContainsFunc(ps.Projects, func(p proto.PeerProject) bool { return p.Files }) {
		t.Errorf("off, peer.projects: %+v %v", ps, err)
	}

	on := linkedServersWith(t, f.ex, func(p *peer.Peer) { p.Files = []string{"web", "quiet"} })
	var res proto.PeerWorktreeRead
	if err := call(on, proto.MethodPeerWorktreeRead, proto.PeerWorktreeReadParams{Project: "web", Session: "s-thread", Path: "README.md"}, &res); err != nil || string(res.Data) != "one\nTHREE\n" {
		t.Errorf("on, read: %q %v", res.Data, err)
	}
	var list proto.PeerWorktreeList
	if err := call(on, proto.MethodPeerWorktreeList, proto.PeerWorktreeListParams{Project: "web", Session: "s-thread"}, &list); err != nil || len(list.Entries) == 0 {
		t.Errorf("on, list: %+v %v", list, err)
	}
	if err := call(on, proto.MethodPeerWorktreeRead, proto.PeerWorktreeReadParams{Project: "web", Session: "s-thread", Path: ".env"}, nil); code(err) != proto.ErrWithheld {
		t.Errorf("on, .env: %v", err)
	}
	if err := call(on, proto.MethodPeerWorktreeRead, proto.PeerWorktreeReadParams{Project: "quiet", Session: "s-quiet", Path: "README.md"}, nil); code(err) != proto.ErrNotShared {
		t.Errorf("on, not shared: %v", err)
	}
	ps = proto.PeerProjects{}
	if err := call(on, proto.MethodPeerProjects, nil, &ps); err != nil {
		t.Fatal(err)
	}
	for _, p := range ps.Projects {
		if p.Files != (p.Project == "web") {
			t.Errorf("on, peer.projects: %+v", p)
		}
	}
}
