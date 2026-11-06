package peer

import (
	"context"
	"crypto/tls"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// hostileExport answers whatever a test says, as a broken or hostile tm on
// the far side might: nothing in the link stands in its way.
type hostileExport struct {
	*worktreeExport
	repos proto.PeerWorktreeRepos
	list  proto.PeerWorktreeList
	read  proto.PeerWorktreeRead
	stamp atomic.Value // string
	diff  proto.PeerWorktreeDiff
	asked atomic.Int32
}

func (f *hostileExport) WorktreeRepos(context.Context, caller.Caller, proto.PeerWorktreeParams) (proto.PeerWorktreeRepos, error) {
	f.asked.Add(1)
	return f.repos, f.refuse
}

func (f *hostileExport) WorktreeList(context.Context, caller.Caller, proto.PeerWorktreeListParams) (proto.PeerWorktreeList, error) {
	f.asked.Add(1)
	return f.list, f.refuse
}

func (f *hostileExport) WorktreeRead(context.Context, caller.Caller, proto.PeerWorktreeReadParams) (proto.PeerWorktreeRead, error) {
	f.asked.Add(1)
	return f.read, f.refuse
}

func (f *hostileExport) WorktreeStamp(ctx context.Context, _ caller.Caller, p proto.PeerWorktreeStampParams) (proto.PeerWorktreeStamp, error) {
	f.asked.Add(1)
	if len(p.Folders) > proto.PeerWorktreeStamped {
		return proto.PeerWorktreeStamp{}, proto.PeerWorktreeRefusal(proto.ErrBadParams)
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
		}
	}
	s, _ := f.stamp.Load().(string)
	if s == "" {
		return proto.PeerWorktreeStamp{}, proto.PeerWorktreeRefusal(proto.ErrFilesOff)
	}
	if s == "busy" {
		return proto.PeerWorktreeStamp{}, proto.PeerWorktreeRefusal(proto.ErrRateLimited)
	}
	return proto.PeerWorktreeStamp{Stamp: s}, nil
}

func (f *hostileExport) WorktreeDiff(context.Context, caller.Caller, proto.PeerWorktreeDiffParams) (proto.PeerWorktreeDiff, error) {
	f.asked.Add(1)
	return f.diff, f.refuse
}

// worktreeLinked is a Manager linked through a relay to one exporting ex,
// whose files switch for it is on for web.
func worktreeLinked(t *testing.T, ex Exporter) *Manager {
	t.Helper()
	_, _, url, tc := startRelay(t)
	a, b := newServer(t), newServer(t)
	a.pair(t, "mini", b, url)
	ps := []Peer{{Alias: "studio", Key: pub(a.id.Key), Relay: url, Files: []string{"web"}}}
	if err := SavePeers(b.dir, ps); err != nil {
		t.Fatal(err)
	}
	start := func(dir string, ex Exporter, tc *tls.Config) *Manager {
		m := Start(Options{Dir: dir, Version: "test", Logf: t.Logf, RelayTLS: tc, Export: ex,
			Backoff: func(int) time.Duration { return 20 * time.Millisecond }})
		t.Cleanup(m.Close)
		return m
	}
	start(b.dir, ex, tc)
	m := start(a.dir, nil, tc)
	waitLink(t, m, "mini", "up", func(l proto.PeerLink) bool { return l.State == proto.LinkUp })
	return m
}

func hostile() *hostileExport { return &hostileExport{worktreeExport: newWorktreeExport()} }

var (
	webSession  = proto.PeerWorktreeParams{Project: "web", Session: "s"}
	webList     = proto.PeerWorktreeListParams{Project: "web", Session: "s"}
	webRead     = proto.PeerWorktreeReadParams{Project: "web", Session: "s", Path: "a.go"}
	webDiff     = proto.PeerWorktreeDiffParams{Project: "web", Session: "s", Path: "a.go"}
	webStamp    = proto.PeerWorktreeStampParams{Project: "web", Session: "s"}
	escapeInSrc = "x\x1b]52;c;aGk=\x07\u202ey"
)

// TestHostileListing: of a far listing, an entry that isn't a clean path of
// the folder asked for doesn't arrive, no string keeps an escape, words and
// numbers are checked, a name is its path's, a withheld entry is its name
// alone, and an answer about another folder is refused.
func TestHostileListing(t *testing.T) {
	ex := hostile()
	m := worktreeLinked(t, ex)
	ctx := context.Background()
	ex.list = proto.PeerWorktreeList{Path: "src", Files: -1, Added: -2, Deleted: -3, Entries: []proto.PeerFileEntry{
		{Name: "a.go", Path: "src/a.go", Kind: "file", Size: 3, Status: "M"},
		{Name: "evil" + escapeInSrc, Path: "src/b.go", Kind: "weird", Size: -9, Status: "no\nway"},
		{Name: "c", Path: "src/c" + escapeInSrc, Kind: "file"},
		{Name: "up", Path: "src/../../etc/passwd", Kind: "file"},
		{Name: "abs", Path: "/etc/passwd", Kind: "file"},
		{Name: "git", Path: "src/.git", Kind: "dir"},
		{Name: "git2", Path: "src/.git/config", Kind: "file"},
		{Name: "slash", Path: `src\d`, Kind: "file"},
		{Name: "elsewhere", Path: "other/e.go", Kind: "file"},
		{Name: "deep", Path: "src/deep/f.go", Kind: "file"},
		{Name: "top", Path: "", Kind: "dir"},
		{Name: "again", Path: "src/a.go", Kind: "file"},
		{Name: ".env", Path: "src/.env", Kind: "file", Size: 7, MTime: time.Now(), Withheld: true},
	}}
	p := webList
	p.Path = "src"
	l, err := m.WorktreeList(ctx, "mini", p)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, e := range l.Entries {
		paths = append(paths, e.Path)
	}
	if want := []string{"src/a.go", "src/b.go", "src/.env"}; !slices.Equal(paths, want) {
		t.Fatalf("entries %q, want %q", paths, want)
	}
	if hasEscape(t, l) || l.Root != "" || l.Files != 0 || l.Added != 0 || l.Deleted != 0 {
		t.Errorf("list: %+v", l)
	}
	if b := l.Entries[1]; b.Name != "b.go" || b.Kind != proto.FileKindOther || b.Size != 0 || b.Status != "" {
		t.Errorf("b.go: %+v", b)
	}
	if env := l.Entries[2]; !env.Withheld || env.Size != 0 || !env.MTime.IsZero() {
		t.Errorf(".env: %+v", env)
	}
	// Flat, anywhere under the folder, but still nothing outside it.
	p.Query = "go"
	l, err = m.WorktreeList(ctx, "mini", p)
	if err != nil || !slices.ContainsFunc(l.Entries, func(e proto.FileEntry) bool { return e.Path == "src/deep/f.go" }) ||
		slices.ContainsFunc(l.Entries, func(e proto.FileEntry) bool { return e.Path == "other/e.go" }) {
		t.Errorf("flat: %+v %v", l, err)
	}
	p.Path = "elsewhere"
	if _, err := m.WorktreeList(ctx, "mini", p); !isCode(err, proto.ErrRefused) {
		t.Errorf("another folder: %v", err)
	}
	// Too many: cut, and said to be.
	ex.list = proto.PeerWorktreeList{}
	for i := range proto.MaxFilesEntries + 5 {
		ex.list.Entries = append(ex.list.Entries, proto.PeerFileEntry{Path: strings.Repeat("x", 1+i/26) + string(rune('a'+i%26)), Kind: "file"})
	}
	l, err = m.WorktreeList(ctx, "mini", webList)
	if err != nil || len(l.Entries) != proto.MaxFilesEntries || !l.Truncated {
		t.Errorf("over the most: %d %v %v", len(l.Entries), l.Truncated, err)
	}
	// What a console asks that can't be a path never leaves.
	before := ex.asked.Load()
	for _, bad := range []string{"../x", "/etc", ".git", "a/../../b", "a\x1bb"} {
		p := webList
		p.Path = bad
		if _, err := m.WorktreeList(ctx, "mini", p); !isCode(err, proto.ErrInvalidPath) {
			t.Errorf("asking %q: %v", bad, err)
		}
	}
	if ex.asked.Load() != before {
		t.Error("a bad path crossed")
	}
}

// TestHostileRead: a chunk must be of the file asked for and add up; a
// chunk that isn't text arrives as binary without its bytes. One goes over
// the link; the rest are the same check, not worth the far side's rate.
func TestHostileRead(t *testing.T) {
	big := []byte(strings.Repeat("x", proto.PeerLibraryChunk))
	for name, c := range map[string]struct {
		r      proto.PeerWorktreeRead
		offset int64
		ok     bool
	}{
		"fine":           {r: proto.PeerWorktreeRead{Path: "a.go", Name: "evil\x1b", Size: 2, Data: []byte("hi")}, ok: true},
		"another path":   {r: proto.PeerWorktreeRead{Path: "b.go", Size: 2, Data: []byte("hi")}},
		"another offset": {r: proto.PeerWorktreeRead{Path: "a.go", Offset: 1, Size: 2, Data: []byte("i")}},
		"past its size":  {r: proto.PeerWorktreeRead{Path: "a.go", Size: 1, Data: []byte("hi")}},
		"negative size":  {r: proto.PeerWorktreeRead{Path: "a.go", Size: -1}},
		"past the most":  {r: proto.PeerWorktreeRead{Path: "a.go", Offset: proto.PeerWorktreeMaxRead, Size: proto.PeerWorktreeMaxRead * 2, Data: []byte("hi")}, offset: proto.PeerWorktreeMaxRead},
		"file shrank":    {r: proto.PeerWorktreeRead{Path: "a.go", Offset: 9, Size: 2}, offset: 9, ok: true},
		"a whole chunk":  {r: proto.PeerWorktreeRead{Path: "a.go", Size: int64(len(big)) * 2, Data: big}, ok: true},
		"over a chunk":   {r: proto.PeerWorktreeRead{Path: "a.go", Size: 1 << 20, Data: append(slices.Clone(big), 'x')}},
	} {
		p := webRead
		p.Offset = c.offset
		r, err := cleanWorktreeRead(p, c.r)
		if c.ok != (err == nil) {
			t.Errorf("%s: %v", name, err)
		} else if !c.ok && !isCode(err, proto.ErrRefused) {
			t.Errorf("%s: %v", name, err)
		} else if c.ok && (r.Name != "a.go" || r.Path != "a.go") {
			t.Errorf("%s: %+v", name, r)
		}
	}
	for name, data := range map[string][]byte{"a NUL": []byte("a\x00b"), "not UTF-8": {0xff, 0xfe, 'a', 'b', 'c'}} {
		r, err := cleanWorktreeRead(webRead, proto.PeerWorktreeRead{Path: "a.go", Size: int64(len(data)), Data: data})
		if err != nil || !r.Binary || r.Data != nil {
			t.Errorf("%s: %+v %v", name, r, err)
		}
	}
	// A character cut at either end of a chunk is still text.
	p := webRead
	p.Offset = 4
	if r, err := cleanWorktreeRead(p, proto.PeerWorktreeRead{Path: "a.go", Offset: 4, Size: 40, Data: []byte("\xa9 caf\xc3")}); err != nil || r.Binary || len(r.Data) == 0 {
		t.Errorf("a cut character: %+v %v", r, err)
	}
	// Binary said, bytes sent anyway; a file over the most is said to be.
	if r, err := cleanWorktreeRead(webRead, proto.PeerWorktreeRead{Path: "a.go", Size: proto.PeerWorktreeMaxRead + 1, Binary: true, Data: []byte("hi")}); err != nil || !r.Binary || r.Data != nil || !r.Truncated {
		t.Errorf("binary with bytes: %+v %v", r, err)
	}
	p = webRead
	p.Path = ""
	if _, err := cleanWorktreeRead(p, proto.PeerWorktreeRead{Size: 1, Data: []byte("x")}); !isCode(err, proto.ErrRefused) {
		t.Errorf("no file: %v", err)
	}

	ex := hostile()
	m := worktreeLinked(t, ex)
	ctx := context.Background()
	ex.read = proto.PeerWorktreeRead{Path: "a.go", Size: 3, Data: []byte("a\x00b")}
	if r, err := m.WorktreeRead(ctx, "mini", webRead); err != nil || !r.Binary || r.Data != nil {
		t.Errorf("over the link: %+v %v", r, err)
	}
	ex.read = proto.PeerWorktreeRead{Path: "elsewhere", Size: 1, Data: []byte("x")}
	if _, err := m.WorktreeRead(ctx, "mini", webRead); !isCode(err, proto.ErrRefused) {
		t.Errorf("over the link, another file: %v", err)
	}
	p = webRead
	p.Offset = -1
	before := ex.asked.Load()
	if _, err := m.WorktreeRead(ctx, "mini", p); !isCode(err, proto.ErrBadParams) || ex.asked.Load() != before {
		t.Errorf("a negative offset: %v", err)
	}
}

// TestHostileDiff: hunks that can't be placed are refused, not shown, words
// and paths are checked, a removed line stays one line, with no escape, and
// removed text stops at the most that crosses.
func TestHostileDiff(t *testing.T) {
	in := proto.PeerWorktreeDiff{Path: "a.go", Name: "x\x1b", Status: "Q", From: "../../etc/passwd", Base: "main\x1b[2J\nnow", Added: -1, Deleted: -1, Size: -1,
		Hunks: []proto.PeerFileHunk{{Old: 1, New: 1, Added: 2, Removed: []string{"one\ntwo" + escapeInSrc}}, {Old: 2, New: 3}}}
	d, err := cleanWorktreeDiff(webDiff, in)
	if err != nil {
		t.Fatal(err)
	}
	if hasEscape(t, d) || d.Name != "a.go" || d.Status != "" || d.From != "" || strings.Contains(d.Base, "\n") || d.Added != 0 || d.Deleted != 0 || d.Size != 0 {
		t.Errorf("diff: %+v", d)
	}
	if len(d.Hunks) != 2 || strings.Contains(d.Hunks[0].Removed[0], "\n") {
		t.Errorf("hunks: %+v", d.Hunks)
	}
	in.From = "old.go"
	if d, err := cleanWorktreeDiff(webDiff, in); err != nil || d.From != "old.go" {
		t.Errorf("a clean from: %+v %v", d, err)
	}
	for name, hunks := range map[string][]proto.PeerFileHunk{
		"line zero":       {{Old: 0, New: 1}},
		"negative":        {{Old: -5, New: 1}},
		"negative new":    {{Old: 1, New: -5}},
		"negative added":  {{Old: 1, New: 1, Added: -1}},
		"out of order":    {{Old: 9, New: 9}, {Old: 2, New: 10}},
		"new out of ord":  {{Old: 1, New: 9}, {Old: 2, New: 3}},
		"overlapping":     {{Old: 1, New: 1, Removed: []string{"a", "b"}}, {Old: 2, New: 2}},
		"overlapping new": {{Old: 1, New: 1, Added: 5}, {Old: 2, New: 3}},
		"past any file":   {{Old: proto.MaxFilesDiff + 2, New: 1}},
		"too many added":  {{Old: 1, New: 1, Added: proto.MaxFilesDiff + 2}},
	} {
		if _, err := cleanWorktreeDiff(webDiff, proto.PeerWorktreeDiff{Path: "a.go", Hunks: hunks}); !isCode(err, proto.ErrRefused) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := cleanWorktreeDiff(webDiff, proto.PeerWorktreeDiff{Path: "b.go"}); !isCode(err, proto.ErrRefused) {
		t.Errorf("another file: %v", err)
	}
	if d, err := cleanWorktreeDiff(webDiff, proto.PeerWorktreeDiff{Path: "a.go", Binary: true, Hunks: []proto.PeerFileHunk{{Old: 1, New: 1, Removed: []string{"bytes"}}}}); err != nil || d.Hunks != nil {
		t.Errorf("binary with hunks: %+v %v", d, err)
	}
	long := strings.Repeat("r", 400<<10)
	d, err = cleanWorktreeDiff(webDiff, proto.PeerWorktreeDiff{Path: "a.go", Hunks: []proto.PeerFileHunk{{Old: 1, New: 1, Removed: []string{long, long}}, {Old: 5, New: 5}}})
	if err != nil || !d.Truncated || len(d.Hunks) != 1 || len(d.Hunks[0].Removed) != 1 {
		t.Errorf("over the most: %d hunks, truncated %v, %v", len(d.Hunks), d.Truncated, err)
	}

	ex := hostile()
	m := worktreeLinked(t, ex)
	ex.diff = in
	if d, err := m.WorktreeDiff(context.Background(), "mini", webDiff); err != nil || hasEscape(t, d) || len(d.Hunks) != 2 {
		t.Errorf("over the link: %+v %v", d, err)
	}
	ex.diff = proto.PeerWorktreeDiff{Path: "a.go", Hunks: []proto.PeerFileHunk{{Old: -1, New: 1}}}
	if _, err := m.WorktreeDiff(context.Background(), "mini", webDiff); !isCode(err, proto.ErrRefused) {
		t.Errorf("over the link, a bad hunk: %v", err)
	}
}

// TestHostileRepos: a repo's name can't be a way out, and a client's echo of
// Path is that name, never a path of either machine.
func TestHostileRepos(t *testing.T) {
	ex := hostile()
	m := worktreeLinked(t, ex)
	ex.repos = proto.PeerWorktreeRepos{Repos: []string{"api", "/etc", "../up", "api", "", "group/web", "x\x1by", ".git"}}
	r, err := m.WorktreeRepos(context.Background(), "mini", webSession)
	if err != nil || len(r.Repos) != 2 || r.Repos[0] != (proto.FileRepo{Name: "api", Path: "api"}) || r.Repos[1].Path != "group/web" {
		t.Errorf("repos: %+v %v", r, err)
	}
	ex.repos = proto.PeerWorktreeRepos{}
	if r, err := m.WorktreeRepos(context.Background(), "mini", webSession); err != nil || r.Repos == nil {
		t.Errorf("none: %+v %v", r, err)
	}
}

// TestOlderPeerIsNeverAsked: a tm whose hello lacks the capability, and a link
// that isn't there, answer without a request.
func TestOlderPeerIsNeverAsked(t *testing.T) {
	m := worktreeLinked(t, newFakeExport())
	ctx := context.Background()
	check := func(name string, err error) {
		t.Helper()
		if !isCode(err, proto.ErrNoPeerFiles) {
			t.Errorf("%s: %v", name, err)
		}
	}
	_, err := m.WorktreeRepos(ctx, "mini", webSession)
	check("repos", err)
	_, err = m.WorktreeList(ctx, "mini", webList)
	check("list", err)
	_, err = m.WorktreeRead(ctx, "mini", webRead)
	check("read", err)
	_, err = m.WorktreeDiff(ctx, "mini", webDiff)
	check("diff", err)
	_, err = m.WorktreeStamp(ctx, "mini", webStamp)
	check("stamp", err)

	if _, err := m.WorktreeList(ctx, "nobody", webList); !errors.Is(err, errNotUp) {
		t.Errorf("no such peer: %v", err)
	}
	var none *Manager
	if _, err := none.WorktreeStamp(ctx, "mini", webStamp); !errors.Is(err, errNotUp) {
		t.Errorf("no manager: %v", err)
	}
}

// TestWorktreeWaits: a read or a stamp waits worktreeWait, a list or diff
// worktreeGitWait, and an answer that doesn't come is busy, not broken.
func TestWorktreeWaits(t *testing.T) {
	if worktreeWait != 15*time.Second || worktreeGitWait != 35*time.Second || worktreeStampFresh != 5*time.Second {
		t.Errorf("waits: %v %v %v", worktreeWait, worktreeGitWait, worktreeStampFresh)
	}
	wait, git := worktreeWait, worktreeGitWait
	worktreeWait, worktreeGitWait = 50*time.Millisecond, 150*time.Millisecond
	t.Cleanup(func() { worktreeWait, worktreeGitWait = wait, git })
	ex := newWorktreeExport()
	ex.block = make(chan struct{})
	t.Cleanup(func() { close(ex.block) })
	m := worktreeLinked(t, ex)
	ctx := context.Background()
	start := time.Now()
	_, err := m.WorktreeRead(ctx, "mini", webRead)
	if !WorktreeBusy(err) {
		t.Errorf("read: %v", err)
	}
	if took := time.Since(start); took < 50*time.Millisecond || took > 5*time.Second {
		t.Errorf("read waited %v", took)
	}
	// The first two still hold the far side's two places: a list waits too,
	// or is told it is over the rate. Neither is a broken link.
	if _, err := m.WorktreeList(ctx, "mini", webList); !WorktreeBusy(err) {
		t.Errorf("list: %v", err)
	}
	if WorktreeBusy(errNotUp) || WorktreeBusy(proto.PeerWorktreeRefusal(proto.ErrFilesOff)) || !WorktreeBusy(proto.PeerWorktreeRefusal(proto.ErrRateLimited)) {
		t.Error("WorktreeBusy says the wrong thing")
	}
}

// TestWorktreeStampShared: consoles looking at one folder set share one
// request in the window, whatever order they name them in and however many;
// past it the far side's answer shows, off included; a busy far side isn't a
// change or an error once there is a stamp.
func TestWorktreeStampShared(t *testing.T) {
	fresh := worktreeStampFresh
	worktreeStampFresh = 200 * time.Millisecond
	t.Cleanup(func() { worktreeStampFresh = fresh })
	ex := hostile()
	ex.stamp.Store("one" + escapeInSrc)
	m := worktreeLinked(t, ex)
	ctx := context.Background()
	stamp := func(folders ...string) (string, error) {
		p := webStamp
		p.Folders = folders
		r, err := m.WorktreeStamp(ctx, "mini", p)
		return r.Stamp, err
	}
	first, err := stamp("a", "b")
	if err != nil || first == "" || hasEscape(t, first) {
		t.Fatalf("first: %q %v", first, err)
	}
	for range 5 {
		if s, err := stamp("b", "a", "b", "../bad"); s != first || err != nil {
			t.Errorf("shared: %q %v", s, err)
		}
	}
	if got := ex.asked.Load(); got != 1 {
		t.Errorf("asked %d times", got)
	}
	// Other folders are another stamp.
	if _, err := stamp("c"); err != nil || ex.asked.Load() != 2 {
		t.Errorf("other folders: %v, asked %d", err, ex.asked.Load())
	}
	// More than the link takes: cut here, not refused there.
	many := make([]string, proto.PeerWorktreeStamped+50)
	for i := range many {
		many[i] = strings.Repeat("d", i+1)
	}
	if _, err := stamp(many...); err != nil {
		t.Errorf("many: %v", err)
	}
	// Busy: the stamp stands.
	ex.stamp.Store("busy")
	time.Sleep(worktreeStampFresh + 20*time.Millisecond)
	if s, err := stamp("a", "b"); s != first || err != nil {
		t.Errorf("busy: %q %v", s, err)
	}
	// A new one shows.
	ex.stamp.Store("two")
	time.Sleep(worktreeStampFresh + 20*time.Millisecond)
	if s, err := stamp("a", "b"); s != "two" || err != nil {
		t.Errorf("two: %q %v", s, err)
	}
	// Off shows at the next answer, and is shared as a stamp is.
	ex.stamp.Store("")
	time.Sleep(worktreeStampFresh + 20*time.Millisecond)
	if _, err := stamp("a", "b"); !isCode(err, proto.ErrFilesOff) {
		t.Errorf("off: %v", err)
	}
	before := ex.asked.Load()
	if _, err := stamp("a", "b"); !isCode(err, proto.ErrFilesOff) || ex.asked.Load() != before {
		t.Errorf("off, again: %v, asked %d more", err, ex.asked.Load()-before)
	}
	// Busy with nothing to stand on is an answer to pass on.
	ex.stamp.Store("busy")
	time.Sleep(worktreeStampFresh + 20*time.Millisecond)
	if _, err := stamp("a", "b"); !isCode(err, proto.ErrRateLimited) {
		t.Errorf("busy with no stamp: %v", err)
	}
}

// TestWorktreeStampWaitsTogether: consoles asking at once wait for one answer.
func TestWorktreeStampWaitsTogether(t *testing.T) {
	ex := hostile()
	ex.stamp.Store("s")
	ex.block = make(chan struct{})
	m := worktreeLinked(t, ex)
	got := make(chan error, 4)
	for range cap(got) {
		go func() {
			_, err := m.WorktreeStamp(context.Background(), "mini", webStamp)
			got <- err
		}()
	}
	deadline := time.Now().Add(5 * time.Second)
	for ex.asked.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	close(ex.block)
	for range cap(got) {
		if err := <-got; err != nil {
			t.Error(err)
		}
	}
	if n := ex.asked.Load(); n != 1 {
		t.Errorf("asked %d times", n)
	}
}

// TestStampShareBound: the stamps kept don't grow without end.
func TestStampShareBound(t *testing.T) {
	var s stampShare
	for i := range maxStampsSeen * 3 {
		s.entry(strings.Repeat("k", i+1))
	}
	if len(s.seen) > maxStampsSeen {
		t.Errorf("%d kept", len(s.seen))
	}
}

// isCode says err is a *proto.Error of that code.
func isCode(err error, code string) bool {
	var pe *proto.Error
	return errors.As(err, &pe) && pe.Code == code
}
