package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// remoteFilesFix is a server reading mini's project web through a real link
// and relay, once its mirror knows mini's sessions.
type remoteFilesFix struct {
	f    *worktreeFix
	s    *Server
	mini linkedMini
}

func newRemoteFilesFix(t *testing.T, ex func(*worktreeFix) peer.Exporter) *remoteFilesFix {
	t.Helper()
	f := newWorktreeFix(t)
	var export peer.Exporter = f.ex
	if ex != nil {
		export = ex(f)
	}
	studio, mini := linkedServersDir(t, export, func(p *peer.Peer) { p.Files = []string{"web"} })
	mirror := peer.StartMirror(studio, peer.MirrorOptions{Logf: t.Logf})
	t.Cleanup(mirror.Close)
	s := &Server{peers: studio, mirror: mirror}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, perr := s.remoteFolder("mini/s-thread"); perr == nil {
			return &remoteFilesFix{f: f, s: s, mini: mini}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the mirror never knew the session: %+v", mirror.List())
	return nil
}

// busy repeats a call while the far side is over its rate: that isn't what
// these tests look at.
func busy[R any](t *testing.T, call func() (R, *proto.Error)) (R, *proto.Error) {
	t.Helper()
	for {
		r, perr := call()
		if perr == nil || perr.Code != proto.ErrRateLimited {
			return r, perr
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func wantCode(t *testing.T, what string, perr *proto.Error, code string) {
	t.Helper()
	if perr == nil || perr.Code != code {
		t.Errorf("%s: %v, want %s", what, perr, code)
	}
	if perr != nil && (strings.Contains(perr.Message, "/") || strings.Contains(perr.Message, "SECRET")) {
		t.Errorf("%s: its words could hold a path: %q", what, perr.Message)
	}
}

// TestRemoteFilesThroughRelay: through a real link and relay, the files
// methods on a paired server's session list, read, diff, stamp and name repos
// as that server allows; what stays there stays there, in a thread's and a
// coordinator's folder; and the mirror says who shares.
func TestRemoteFilesThroughRelay(t *testing.T) {
	x := newRemoteFilesFix(t, nil)
	s := x.s

	pl := s.mirror.List()
	if len(pl.Peers) != 1 || !pl.Peers[0].Worktree {
		t.Fatalf("peer.list: %+v", pl)
	}
	for _, pr := range pl.Peers[0].Projects {
		if pr.Files != (pr.Project == "web") {
			t.Errorf("peer.list says %s's files are %v", pr.Project, pr.Files)
		}
	}

	list := func(session, path string) (proto.FilesListResult, *proto.Error) {
		return busy(t, func() (proto.FilesListResult, *proto.Error) {
			r, perr := s.filesList(proto.FilesListParams{Session: session, Path: path, Ignored: true})
			if perr != nil {
				return proto.FilesListResult{}, perr
			}
			return r.(proto.FilesListResult), nil
		})
	}
	read := func(session, path string) (proto.FilesReadResult, *proto.Error) {
		return busy(t, func() (proto.FilesReadResult, *proto.Error) {
			r, perr := s.filesRead(proto.FilesReadParams{Session: session, Path: path, Ignored: true, Binary: true})
			if perr != nil {
				return proto.FilesReadResult{}, perr
			}
			return r.(proto.FilesReadResult), nil
		})
	}
	l, perr := list("mini/s-thread", "")
	if perr != nil {
		t.Fatal(perr)
	}
	if l.Root != "" || !l.Git || len(l.Entries) == 0 {
		t.Errorf("list: %+v", l)
	}
	names := map[string]proto.FileEntry{}
	for _, e := range l.Entries {
		names[e.Name] = e
	}
	if _, ok := names["build"]; ok {
		t.Errorf("an ignored folder crossed, though asked for: %+v", l.Entries)
	}
	if env, ok := names[".env"]; !ok || !env.Withheld || env.Size != 0 {
		t.Errorf(".env: %+v, %v", env, ok)
	}
	r, perr := read("mini/s-thread", "README.md")
	if perr != nil || string(r.Data) != "one\nTHREE\n" || r.Name != "README.md" {
		t.Errorf("read: %+v %v", r, perr)
	}
	// A binary's bytes don't come, though asked for.
	if r, perr := read("mini/s-thread", "bin.dat"); perr != nil || !r.Binary || r.Data != nil {
		t.Errorf("bin.dat: %+v %v", r, perr)
	}
	_, perr = read("mini/s-thread", ".env")
	wantCode(t, ".env", perr, proto.ErrWithheld)
	_, perr = read("mini/s-thread", "../../x")
	wantCode(t, "a parent", perr, proto.ErrInvalidPath)
	_, perr = read("mini/s-thread", "build/out.txt")
	wantCode(t, "an ignored file", perr, proto.ErrIgnored)

	d, perr := busy(t, func() (proto.FilesDiffResult, *proto.Error) {
		r, perr := s.filesDiff(proto.FilesDiffParams{Session: "mini/s-thread", Path: "README.md", Ignored: true})
		if perr != nil {
			return proto.FilesDiffResult{}, perr
		}
		return r.(proto.FilesDiffResult), nil
	})
	if perr != nil || d.Status != proto.FileModified || len(d.Hunks) == 0 {
		t.Errorf("diff: %+v %v", d, perr)
	}
	st, perr := busy(t, func() (proto.FilesStampResult, *proto.Error) {
		r, perr := s.filesStamp(proto.FilesStampParams{Session: "mini/s-thread", Folders: []string{"", "folder"}})
		if perr != nil {
			return proto.FilesStampResult{}, perr
		}
		return r.(proto.FilesStampResult), nil
	})
	if perr != nil || st.Stamp == "" {
		t.Errorf("stamp: %+v %v", st, perr)
	}

	// A coordinator's folder, by the name the repo goes by.
	repos, perr := busy(t, func() (proto.FilesReposResult, *proto.Error) {
		r, perr := s.filesRepos(proto.FilesReposParams{Session: "mini/s-coord"})
		if perr != nil {
			return proto.FilesReposResult{}, perr
		}
		return r.(proto.FilesReposResult), nil
	})
	if perr != nil || len(repos.Repos) != 2 {
		t.Fatalf("repos: %+v %v", repos, perr)
	}
	for _, repo := range repos.Repos {
		if repo.Path != repo.Name || strings.HasPrefix(repo.Path, "/") {
			t.Errorf("repo: %+v", repo)
		}
	}
	two, perr := busy(t, func() (proto.FilesListResult, *proto.Error) {
		r, perr := s.filesList(proto.FilesListParams{Session: "mini/s-coord", Repo: repos.Repos[1].Path})
		if perr != nil {
			return proto.FilesListResult{}, perr
		}
		return r.(proto.FilesListResult), nil
	})
	if perr != nil || two.Root != "" {
		t.Errorf("the second repo: %+v %v", two, perr)
	}
	if r, perr := busy(t, func() (proto.FilesReposResult, *proto.Error) {
		r, perr := s.filesRepos(proto.FilesReposParams{Session: "mini/s-thread"})
		if perr != nil {
			return proto.FilesReposResult{}, perr
		}
		return r.(proto.FilesReposResult), nil
	}); perr != nil || len(r.Repos) != 0 || r.Repos == nil {
		t.Errorf("a thread's repos: %+v %v", r, perr)
	}

	// Sessions that aren't one.
	_, perr = read("mini/s-none", "README.md")
	wantCode(t, "no such session", perr, proto.ErrUnknownSession)
	_, perr = read("nobody/s-thread", "README.md")
	wantCode(t, "no such server", perr, proto.ErrUnknownSession)
	_, perr = read("mini/mini/s-thread", "README.md")
	wantCode(t, "a chain", perr, proto.ErrUnknownSession)
}

// TestRemoteFilesOffMidView: the switch going off there is seen at the next
// answer, in this server's words, within the stamp's age; the link going down
// says offline and not off, so a view keeps what it has.
func TestRemoteFilesOffMidView(t *testing.T) {
	x := newRemoteFilesFix(t, nil)
	s := x.s
	stamp := func() *proto.Error {
		_, perr := s.filesStamp(proto.FilesStampParams{Session: "mini/s-thread"})
		return perr
	}
	if _, perr := busy(t, func() (any, *proto.Error) { return nil, stamp() }); perr != nil {
		t.Fatal(perr)
	}
	if _, perr := busy(t, func() (any, *proto.Error) { return s.filesList(proto.FilesListParams{Session: "mini/s-thread"}) }); perr != nil {
		t.Fatal(perr)
	}

	peers, err := peer.LoadPeers(x.mini.dir)
	if err != nil {
		t.Fatal(err)
	}
	peers[0].Files = nil
	if err := peer.SavePeers(x.mini.dir, peers); err != nil {
		t.Fatal(err)
	}
	_, perr := busy(t, func() (any, *proto.Error) { return s.filesList(proto.FilesListParams{Session: "mini/s-thread"}) })
	wantCode(t, "list, off", perr, proto.ErrFilesOff)
	if perr != nil && !strings.Contains(perr.Message, "mini doesn't share web's files") {
		t.Errorf("off says %q", perr.Message)
	}
	_, perr = busy(t, func() (any, *proto.Error) {
		return s.filesRead(proto.FilesReadParams{Session: "mini/s-thread", Path: "README.md"})
	})
	wantCode(t, "read, off", perr, proto.ErrFilesOff)
	_, perr = busy(t, func() (any, *proto.Error) { return s.filesRepos(proto.FilesReposParams{Session: "mini/s-coord"}) })
	wantCode(t, "repos, off", perr, proto.ErrFilesOff)
	// A stamp may be up to its age old, and no older.
	start := time.Now()
	for {
		perr := stamp()
		if perr != nil && perr.Code != proto.ErrRateLimited {
			wantCode(t, "stamp, off", perr, proto.ErrFilesOff)
			break
		}
		if time.Since(start) > 8*time.Second {
			t.Fatalf("the stamp still answers after %v", time.Since(start))
		}
		time.Sleep(200 * time.Millisecond)
	}

	// The peer goes: what the mirror remembers stays, and says offline.
	x.mini.m.Close()
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, perr := s.filesList(proto.FilesListParams{Session: "mini/s-thread"})
		if perr != nil && perr.Code == proto.ErrPeerOffline {
			if !strings.Contains(perr.Message, "mini is offline") {
				t.Errorf("offline says %q", perr.Message)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("still %v", perr)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// olderExport is an exporter from before folders crossed: its hello lacks the
// capability, whatever its switch says.
type olderExport struct{ peer.Exporter }

// TestRemoteFilesOlderPeer: a tm without the capability is never asked, and
// says why; its projects don't claim to share.
func TestRemoteFilesOlderPeer(t *testing.T) {
	x := newRemoteFilesFix(t, func(f *worktreeFix) peer.Exporter { return olderExport{f.ex} })
	pl := x.s.mirror.List()
	if len(pl.Peers) != 1 || pl.Peers[0].Worktree {
		t.Errorf("peer.list: %+v", pl.Peers)
	}
	for name, perr := range map[string]*proto.Error{
		"list":  second(x.s.filesList(proto.FilesListParams{Session: "mini/s-thread"})),
		"read":  second(x.s.filesRead(proto.FilesReadParams{Session: "mini/s-thread", Path: "README.md"})),
		"diff":  second(x.s.filesDiff(proto.FilesDiffParams{Session: "mini/s-thread", Path: "README.md"})),
		"stamp": second(x.s.filesStamp(proto.FilesStampParams{Session: "mini/s-thread"})),
		"repos": second(x.s.filesRepos(proto.FilesReposParams{Session: "mini/s-coord"})),
	} {
		wantCode(t, name, perr, proto.ErrNoPeerFiles)
		if perr != nil && !strings.Contains(perr.Message, "mini runs an older tm") {
			t.Errorf("%s says %q", name, perr.Message)
		}
	}
}

func second[A, B any](_ A, b B) B { return b }

// TestRemoteFilesWords: what crosses as a refusal reaches a console in this
// server's words, by its code: a hostile far side's sentence never does, and a
// code no method here has is only internal.
func TestRemoteFilesWords(t *testing.T) {
	evil := "/Users/me/secret \x1b[2J"
	for code, want := range map[string]string{
		proto.ErrFilesOff:       "mini doesn't share web's files",
		proto.ErrNotShared:      "mini doesn't share web any more",
		proto.ErrWithheld:       "its name says it may hold secrets, so it stays on mini",
		proto.ErrIgnored:        "git-ignored files stay on mini",
		proto.ErrRateLimited:    "mini is slow to answer",
		proto.ErrRefused:        "mini's answer doesn't add up",
		proto.ErrUnknownMethod:  "mini runs an older tm, which can't share files",
		proto.ErrUnknownSession: proto.PeerWorktreeRefusal(proto.ErrUnknownSession).Message,
		"made-up":               proto.PeerWorktreeRefusal(proto.ErrInternal).Message,
	} {
		perr := remoteFilesError("mini", "web", &proto.Error{Code: code, Message: evil})
		if perr.Message != want {
			t.Errorf("%s says %q, want %q", code, perr.Message, want)
		}
		if strings.Contains(perr.Message, "/Users") {
			t.Errorf("%s: their words crossed", code)
		}
	}
	if perr := remoteFilesError("mini", "web", context.DeadlineExceeded); perr.Code != proto.ErrRateLimited {
		t.Errorf("a slow answer: %v", perr)
	}
	if perr := remoteFilesError("mini", "web", context.Canceled); perr.Code != proto.ErrPeerOffline {
		t.Errorf("a dropped link: %v", perr)
	}
	// Words of a bad path don't name it.
	if perr := remoteFilesError("mini", "web", proto.Errorf(proto.ErrInvalidPath, "%s", evil)); strings.Contains(perr.Message, "/Users") {
		t.Errorf("a bad path: %q", perr.Message)
	}
}

// TestRemoteFilesNoServerLinks: without a link there is nothing to read, and
// nothing panics.
func TestRemoteFilesNoServerLinks(t *testing.T) {
	s := &Server{}
	_, perr := s.filesList(proto.FilesListParams{Session: "mini/s-1"})
	wantCode(t, "list", perr, proto.ErrUnknownSession)
	_, perr = s.filesRepos(proto.FilesReposParams{Session: "mini/s-1"})
	wantCode(t, "repos", perr, proto.ErrUnknownSession)
}
