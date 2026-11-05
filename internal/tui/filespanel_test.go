package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"

	tmclient "github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// fakeFiles answers the files methods: folders by path, files by path.
type fakeFiles struct {
	dirs    map[string][]proto.FileEntry
	found   map[string][]proto.FileEntry // by query; "" the changed list
	texts   map[string]proto.FilesReadResult
	stamp   atomic.Value
	lists   atomic.Int32
	refusal error
	seen    []proto.FilesListParams
}

func (f *fakeFiles) install(t *testing.T) {
	t.Helper()
	f.stamp.Store("one")
	oldCall, oldGo, oldLib := filesCall, filesGo, filesLibrary
	filesGo = func(g func()) { g() }
	filesCall = func(_ tmclient.Paths, method string, params, res any) error {
		switch p := params.(type) {
		case proto.FilesStampParams:
			res.(*proto.FilesStampResult).Stamp = f.stamp.Load().(string)
		case proto.FilesListParams:
			f.lists.Add(1)
			f.seen = append(f.seen, p)
			if f.refusal != nil {
				return f.refusal
			}
			out := res.(*proto.FilesListResult)
			*out = proto.FilesListResult{Git: true, Files: 3, Added: 84, Deleted: 12, Path: p.Path}
			if p.Query != "" || p.Changed {
				out.Entries = f.found[p.Query]
			} else {
				out.Entries = f.dirs[p.Path]
			}
		case proto.FilesReadParams:
			r, ok := f.texts[p.Path]
			if !ok {
				return proto.Errorf(proto.ErrFileGone, "no longer there: %s", p.Path)
			}
			*res.(*proto.FilesReadResult) = r
		default:
			t.Fatalf("method %s", method)
		}
		return nil
	}
	filesLibrary = func(string) ([]thread.LibFile, error) {
		at := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
		return []thread.LibFile{
			{Thread: "t-0002", Task: "T5", Title: "Fix it", Name: "notes.md", Size: 3100, Time: at},
			{Thread: "t-0009", Task: "T9", Title: "Other", Name: "other.png", Size: 412000, Time: at},
		}, nil
	}
	t.Cleanup(func() {
		filesCall, filesGo, filesLibrary = oldCall, oldGo, oldLib
		filesKept.Store(nil)
	})
}

func sampleTree() map[string][]proto.FileEntry {
	return map[string][]proto.FileEntry{
		"": {
			{Name: "internal", Path: "internal", Kind: proto.FileKindDir, Changed: true},
			{Name: "node_modules", Path: "node_modules", Kind: proto.FileKindDir, Ignored: true},
			{Name: "old", Path: "old", Kind: proto.FileKindDir, Status: proto.FileDeleted, Changed: true},
			{Name: "Makefile", Path: "Makefile", Kind: proto.FileKindFile, Size: 9000},
			{Name: "README.md", Path: "README.md", Kind: proto.FileKindFile, Status: proto.FileModified, Size: 900},
			{Name: "evil\x1b[2J\x1b]52;c;x\x07name", Path: "evil", Kind: proto.FileKindFile, Status: proto.FileAdded},
			{Name: "gone.go", Path: "gone.go", Kind: proto.FileKindFile, Status: proto.FileDeleted},
			{Name: "out", Path: "out", Kind: proto.FileKindOther, Link: true, LinkOut: true},
			{Name: "here", Path: "here", Kind: proto.FileKindFile, Link: true},
		},
		"internal": {{Name: "types.go", Path: "internal/types.go", Kind: proto.FileKindFile, Status: proto.FileModified}},
	}
}

// filesClient is an info panel beside thread t-0005's session s-4, with
// the Files view open and the keyboard.
func filesClient(t *testing.T) (*client, *fakeFiles) {
	t.Helper()
	f := &fakeFiles{dirs: sampleTree()}
	f.install(t)
	c := infoClient(t, 140)
	c.info.data.session.ID = "s-4"
	c.kb = areaInfo
	c.filesToggle()
	if c.info.files == nil {
		t.Fatal("no Files view")
	}
	return c, f
}

func infoText(c *client) string {
	var b strings.Builder
	for _, l := range c.info.lines {
		b.WriteString(strings.TrimRight(ansi.Strip(l), " ") + "\n")
	}
	return b.String()
}

func filesTestKey(name string) uv.Key {
	switch name {
	case "enter":
		return uv.Key{Code: uv.KeyEnter}
	case "esc":
		return uv.Key{Code: uv.KeyEscape}
	case "up":
		return uv.Key{Code: uv.KeyUp}
	case "down":
		return uv.Key{Code: uv.KeyDown}
	case "left":
		return uv.Key{Code: uv.KeyLeft}
	case "right":
		return uv.Key{Code: uv.KeyRight}
	case "backspace":
		return uv.Key{Code: uv.KeyBackspace}
	}
	r := []rune(name)[0]
	return uv.Key{Code: r, Text: name}
}

func filesPress(c *client, names ...string) {
	for _, n := range names {
		c.mu.Lock()
		c.infoKeyboard(filesTestKey(n))
	}
}

// TestFilesPanelGolden: the view's faces, as testdata/files-panel.golden
// has them (go test -run TestFilesPanelGolden -update rewrites it): the
// tree with marks, ignored and link rows, a folder open, a filter, the
// changed list, a repo-less folder, no folder at all, and Attached.
func TestFilesPanelGolden(t *testing.T) {
	freshConsole(t)
	var b strings.Builder
	write := func(name string, fv *filesView, w int) {
		lines, _ := filesLay(fv, w, true)
		fmt.Fprintf(&b, "%s (%d cells)\n", name, w)
		for _, l := range lines {
			b.WriteString("│" + strings.TrimRight(ansi.Strip(l), " ") + "\n")
		}
		b.WriteString("\n")
	}
	base := func() *filesView {
		return &filesView{session: "s-4", thread: "t-0005", open: map[string]bool{}, dirs: sampleTree(), loaded: true,
			sum: proto.FilesListResult{Git: true, Files: 4, Added: 84, Deleted: 12}}
	}
	write("the top", base(), 39)
	fv := base()
	fv.open["internal"], fv.sel = true, 1
	write("a folder open, its file selected", fv, 39)
	write("narrow", fv, 23)
	fv = base()
	fv.typing, fv.filter, fv.ignored = true, "type", true
	fv.found = []proto.FileEntry{{Name: "types.go", Path: "internal/types.go", Kind: proto.FileKindFile, Status: proto.FileModified}}
	write("filtering, ignored too", fv, 39)
	fv = base()
	fv.changed, fv.cut = true, true
	fv.found = []proto.FileEntry{{Name: "README.md", Path: "README.md", Kind: proto.FileKindFile, Status: proto.FileModified}}
	write("changed only, cut", fv, 39)
	fv = base()
	fv.sum.Git = false
	fv.dirs = map[string][]proto.FileEntry{"": {{Name: "a.txt", Path: "a.txt", Kind: proto.FileKindFile}}}
	write("a plain folder", fv, 39)
	fv = base()
	fv.err, fv.dirs = "a director has no repo", nil
	write("no folder", fv, 39)
	fv = base()
	fv.attached = true
	at := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	fv.lib = []thread.LibFile{{Thread: "t-0005", Task: "T5", Title: "Fix it", Name: "notes.md", Size: 3100, Time: at}, {Thread: "t-0005", Task: "T5", Title: "Fix it", Name: "shot.png", Size: 412000, Time: at}}
	write("Attached", fv, 39)
	fv.lib = nil
	write("Attached, none", fv, 39)
	path := filepath.Join("testdata", "files-panel.golden")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if b.String() != string(want) {
		t.Errorf("the view differs from %s:\n%s", path, b.String())
	}
}

// TestFilesNoControlChars: a file's name and text from the worktree
// never bring an escape to the terminal.
func TestFilesNoControlChars(t *testing.T) {
	c, f := filesClient(t)
	if s := strings.Join(c.info.lines, ""); strings.Contains(s, "\x1b[2J") || strings.Contains(s, "\x1b]52") || strings.Contains(s, "\x07") {
		t.Errorf("control characters in the panel: %q", s)
	}
	f.texts = map[string]proto.FilesReadResult{"evil": {Name: "evil", Path: "evil", Size: 40,
		Data: []byte("one\x1b[2J\x1b]52;c;eA==\x07 \u009b31m two\u202e three\tfour\n")}}
	tv := worktreeViewer(tmclient.Paths{}, "s-4", proto.FileEntry{Name: "evil\x1b[2J", Path: "evil"}, false)
	for _, s := range []string{tv.body, tv.title, tv.note} {
		if strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 && r != '\n' || r == 0x7f || r >= 0x80 && r < 0xa0 || r == '\u202e' }) {
			t.Errorf("control characters in %q", s)
		}
	}
	if !strings.Contains(tv.body, "one") || !strings.Contains(tv.body, "three    four") {
		t.Errorf("the text went: %q", tv.body)
	}
}

// TestFilesKeys: the arrows move and fold, enter on a folder too; / keeps
// a filter and esc clears it; c and i ask the server again; a is Attached,
// esc gives the keyboard back, F the Info face.
func TestFilesKeys(t *testing.T) {
	c, f := filesClient(t)
	if got := infoText(c); !strings.Contains(got, "README.md") || !strings.Contains(got, "3 changed +84 −12") {
		t.Fatalf("the top:\n%s", got)
	}
	filesPress(c, "right")
	if got := infoText(c); !strings.Contains(got, "types.go") || !strings.Contains(got, "▾ internal/") {
		t.Fatalf("→ unfolded nothing:\n%s", got)
	}
	filesPress(c, "right")
	if c.info.files.selPath != "internal/types.go" {
		t.Fatalf("→ on an open folder: %q", c.info.files.selPath)
	}
	filesPress(c, "left")
	if c.info.files.selPath != "internal" {
		t.Fatalf("← on a file: %q", c.info.files.selPath)
	}
	filesPress(c, "left")
	if strings.Contains(infoText(c), "types.go") {
		t.Fatal("← folded nothing")
	}
	filesPress(c, "enter")
	if !strings.Contains(infoText(c), "types.go") {
		t.Fatal("enter unfolded nothing")
	}
	filesPress(c, "enter")

	f.found = map[string][]proto.FileEntry{"ty": {{Name: "types.go", Path: "internal/types.go", Kind: proto.FileKindFile}}}
	filesPress(c, "/", "t", "y", "x", "backspace")
	if got := f.seen[len(f.seen)-1]; got.Query != "ty" {
		t.Fatalf("query %+v", got)
	}
	if !c.info.files.typing || !strings.Contains(infoText(c), "internal/types.go") {
		t.Fatalf("typing:\n%s", infoText(c))
	}
	if hint := c.infoHintNow(); !strings.Contains(hint, "enter keep") {
		t.Errorf("hint %q", hint)
	}
	filesPress(c, "enter")
	if c.info.files.typing || c.info.files.filter != "ty" {
		t.Fatalf("enter dropped the filter: %+v", c.info.files.filter)
	}
	filesPress(c, "esc")
	if c.info.files.filter != "" || c.kb != areaInfo {
		t.Fatalf("esc: filter %q kb %v", c.info.files.filter, c.kb)
	}

	filesPress(c, "c")
	if got := f.seen[len(f.seen)-1]; !got.Changed {
		t.Fatalf("c: %+v", got)
	}
	filesPress(c, "c", "i")
	if got := f.seen[len(f.seen)-1]; !got.Ignored || got.Changed {
		t.Fatalf("i: %+v", got)
	}

	filesPress(c, "a")
	if got := infoText(c); !strings.Contains(got, "notes.md") || strings.Contains(got, "other.png") {
		t.Fatalf("Attached shows the thread's files alone:\n%s", got)
	}
	filesPress(c, "a")
	filesPress(c, "esc")
	if c.kb != areaMain || c.info.files == nil {
		t.Fatalf("esc: kb %v, view %v", c.kb, c.info.files)
	}
	c.kb = areaInfo
	filesPress(c, "F")
	if c.info.files != nil || !strings.Contains(infoText(c), "thread") {
		t.Fatalf("F: back to the Info face:\n%s", infoText(c))
	}
}

// TestFilesRefresh: the view asks for the stamp, not the listing, while
// nothing moved; a moved stamp lists again and keeps the selection.
func TestFilesRefresh(t *testing.T) {
	c, f := filesClient(t)
	filesPress(c, "down", "down", "down", "down")
	sel := c.info.files.selPath
	n := f.lists.Load()
	c.mu.Lock()
	c.info.files.polled = time.Time{}
	c.mu.Unlock()
	c.filesPoll()
	if f.lists.Load() != n {
		t.Fatal("listed again though the stamp stayed")
	}
	c.filesPoll() // within filesEvery: nothing asked
	f.stamp.Store("two")
	c.filesPoll()
	if f.lists.Load() != n {
		t.Fatal("polled within filesEvery")
	}
	c.mu.Lock()
	c.info.files.polled = time.Time{}
	c.mu.Unlock()
	f.dirs[""] = append([]proto.FileEntry{{Name: "a-new", Path: "a-new", Kind: proto.FileKindDir}}, f.dirs[""]...)
	c.filesPoll()
	if f.lists.Load() == n || !strings.Contains(infoText(c), "a-new") {
		t.Fatalf("no refresh:\n%s", infoText(c))
	}
	if c.info.files.selPath != sel || c.info.files.rows()[c.info.files.sel].id() != sel {
		t.Fatalf("selection moved: %q → %q", sel, c.info.files.selPath)
	}
}

// TestFilesNoFolder: a refusal says why, and Attached shows; a director
// and a paired server's pane say theirs and open nothing.
func TestFilesNoFolder(t *testing.T) {
	c, f := filesClient(t)
	filesPress(c, "F")
	f.refusal = proto.Errorf(proto.ErrNoFiles, "its worktree is gone")
	filesPress(c, "F")
	fv := c.info.files
	if fv == nil || !fv.attached || fv.err == "" || !strings.Contains(infoText(c), "notes.md") {
		t.Fatalf("a refusal: %+v\n%s", fv, infoText(c))
	}
	filesPress(c, "a")
	if !strings.Contains(infoText(c), "its worktree is gone") {
		t.Fatalf("no reason:\n%s", infoText(c))
	}
	filesPress(c, "F")
	c.info.data = &infoData{slug: "@pal/x", session: proto.SessionInfo{ID: "pal/s-1"}, remote: &remoteData{shown: "pal\x1b[2J"}}
	filesPress(c, "F")
	if c.info.files != nil || !strings.Contains(c.flash, "stay on it") || strings.Contains(c.flash, "\x1b") {
		t.Fatalf("paired: %v %q", c.info.files, c.flash)
	}
	c.info.data = &infoData{slug: "prog", session: proto.SessionInfo{ID: "s-9"}, prog: &programInfo{}}
	filesPress(c, "F")
	if c.info.files != nil || !strings.Contains(c.flash, "director") {
		t.Fatalf("director: %v %q", c.info.files, c.flash)
	}
}

// TestFilesCoordinator: beside a coordinator Attached is the whole
// project's.
func TestFilesCoordinator(t *testing.T) {
	f := &fakeFiles{dirs: sampleTree()}
	f.install(t)
	c := infoClient(t, 140)
	c.info.data = &infoData{slug: "beta", session: proto.SessionInfo{ID: "s-1", Role: proto.RoleCoordinator}, watch: &proto.ProjectWatch{Project: "beta"}}
	c.kb = areaInfo
	filesPress(c, "F", "a")
	if got := infoText(c); !strings.Contains(got, "notes.md") || !strings.Contains(got, "other.png") {
		t.Fatalf("Attached:\n%s", got)
	}
}

// TestFilesViewerOver: enter on a file reads it and leaves for the viewer
// over the session; what doesn't open says why and stays; the view comes
// back on the session afterwards, on no other.
func TestFilesViewerOver(t *testing.T) {
	c, f := filesClient(t)
	f.texts = map[string]proto.FilesReadResult{"Makefile": {Name: "Makefile", Path: "Makefile", Size: 4, Data: []byte("all:")}}
	selectRow := func(id string) {
		t.Helper()
		c.mu.Lock()
		fv := c.info.files
		for i, r := range fv.rows() {
			if r.id() == id {
				fv.sel, fv.selPath = i, id
			}
		}
		c.infoLayout()
		c.mu.Unlock()
	}
	for id, want := range map[string]string{"out": "doesn't open", "gone.go": "deleted"} {
		selectRow(id)
		filesPress(c, "enter")
		if !strings.Contains(c.flash, want) || c.over != nil {
			t.Errorf("%s: flash %q, over %+v", id, c.flash, c.over)
		}
	}
	tv := worktreeViewer(tmclient.Paths{}, "s-4", proto.FileEntry{Name: "Makefile", Path: "Makefile"}, false)
	if tv.body != "all:" || !strings.Contains(tv.title, "Makefile") {
		t.Errorf("viewer %+v", tv)
	}
	if strings.Contains(tv.title, "secrets") {
		t.Errorf("a plain file's title: %q", tv.title)
	}
	tv = worktreeViewer(tmclient.Paths{}, "s-4", proto.FileEntry{Name: "Makefile", Path: "Makefile", Ignored: true}, true)
	if tv.body != "all:" || !strings.Contains(tv.title, "git-ignored: may hold secrets") {
		t.Errorf("an ignored file's viewer %+v", tv)
	}
	f.texts["pic.png"] = proto.FilesReadResult{Name: "pic.png", Path: "pic.png", Size: 9, Binary: true}
	tv = worktreeViewer(tmclient.Paths{}, "s-4", proto.FileEntry{Name: "pic.png", Path: "pic.png"}, false)
	if tv.body != "" || !strings.Contains(tv.note, "not text") {
		t.Errorf("binary viewer %+v", tv)
	}
	tv = worktreeViewer(tmclient.Paths{}, "s-4", proto.FileEntry{Name: "pic.png", Path: "pic.png", Ignored: true}, true)
	if strings.Contains(tv.title, "secrets") {
		t.Errorf("no text shows, nothing to warn of: %q", tv.title)
	}
	tv = worktreeViewer(tmclient.Paths{}, "s-4", proto.FileEntry{Name: "nope", Path: "nope"}, false)
	if !strings.Contains(tv.note, "can't read it") {
		t.Errorf("gone viewer %+v", tv)
	}
	// Kept for the viewer: back on the same session, with the keyboard.
	kept := c.info.files
	filesKept.Store(kept)
	c.info.files, c.kb = nil, areaMain
	c.mu.Lock()
	c.filesFollow(&infoData{session: proto.SessionInfo{ID: "other"}})
	c.mu.Unlock()
	if c.info.files != nil {
		t.Fatal("came back on another session")
	}
	filesKept.Store(kept)
	c.mu.Lock()
	c.filesFollow(&infoData{session: proto.SessionInfo{ID: kept.session}})
	c.mu.Unlock()
	if c.info.files != kept || c.kb != areaInfo {
		t.Fatalf("not back: %v %v", c.info.files, c.kb)
	}
	// Another session in the pane closes it.
	c.mu.Lock()
	c.filesFollow(&infoData{session: proto.SessionInfo{ID: "other"}})
	c.mu.Unlock()
	if c.info.files != nil {
		t.Fatal("still open on another session")
	}
}

// TestFilesViewerLong: a file over the viewer's first MiB says so.
func TestFilesViewerLong(t *testing.T) {
	f := &fakeFiles{}
	f.install(t)
	chunk := []byte(strings.Repeat("x", proto.LibraryChunk))
	f.texts = map[string]proto.FilesReadResult{"big": {Size: 3 << 20, Data: chunk}}
	tv := worktreeViewer(tmclient.Paths{}, "s-4", proto.FileEntry{Name: "big", Path: "big"}, false)
	if len(tv.body) != libMax || !strings.Contains(tv.note, "shows the first") {
		t.Errorf("len %d, note %q", len(tv.body), tv.note)
	}
}

// TestFilesDashViewer: the viewer the attach left for is the dashboard's
// popup, and closing it attaches again.
func TestFilesDashViewer(t *testing.T) {
	src := &fakeSource{data: testData()}
	tv := &textView{title: "Makefile · 4 B", body: "all:"}
	m := newDash(DashOptions{Source: src, Width: 100, Height: 30,
		Over: &Over{Session: "s-4", Screen: []string{"", "row"}, Viewer: tv}})
	m.setData(src.data)
	if m.top() != any(tv) {
		t.Fatalf("top %T", m.top())
	}
	if m.overDone() {
		t.Fatal("back to the session under the viewer")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.overDone() || m.result.Attach != "s-4" {
		t.Fatalf("closed: done %v, attach %q", m.overDone(), m.result.Attach)
	}
}

// TestFileCaution: the viewer's title says so of an ignored file and of a
// tracked one named like a secret; other files, and what shows no text, say
// nothing.
func TestFileCaution(t *testing.T) {
	for _, c := range []struct {
		e    proto.FileEntry
		want string
	}{
		{proto.FileEntry{Name: "out.log", Ignored: true}, "git-ignored: may hold secrets"},
		{proto.FileEntry{Name: ".env", Ignored: true}, "git-ignored: may hold secrets"},
		{proto.FileEntry{Name: ".env"}, "its name says it may hold secrets"},
		{proto.FileEntry{Name: ".env.production"}, "its name says it may hold secrets"},
		{proto.FileEntry{Name: "server.PEM"}, "its name says it may hold secrets"},
		{proto.FileEntry{Name: "id_ed25519"}, "its name says it may hold secrets"},
		{proto.FileEntry{Name: "secrets.yaml"}, "its name says it may hold secrets"},
		{proto.FileEntry{Name: ".env.example"}, ""},
		{proto.FileEntry{Name: "id_ed25519.pub"}, ""},
		{proto.FileEntry{Name: "keyboard.go"}, ""},
		{proto.FileEntry{Name: "README.md"}, ""},
	} {
		if got := fileCaution(c.e); got != c.want {
			t.Errorf("%s: %q, want %q", c.e.Name, got, c.want)
		}
	}
}
