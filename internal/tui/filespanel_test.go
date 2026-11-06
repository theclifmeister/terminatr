package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	thumbs  map[string][]byte // "t-0001/name" -> its thumbnail
	dirs    map[string][]proto.FileEntry
	found   map[string][]proto.FileEntry // by query; "" the changed list
	texts   map[string]proto.FilesReadResult
	stamp   atomic.Value
	lists   atomic.Int32
	refusal error
	seen    []proto.FilesListParams
	repos   []proto.FileRepo
	noRepos bool // a server without files.repos
	stamped []proto.FilesStampParams
	read    []proto.FilesReadParams
	diffed  []proto.FilesDiffParams
	diffs   map[string]proto.FilesDiffResult
}

func (f *fakeFiles) install(t *testing.T) {
	t.Helper()
	f.stamp.Store("one")
	oldCall, oldGo, oldLib := filesCall, filesGo, filesLibrary
	filesGo = func(g func()) { g() }
	filesCall = func(_ tmclient.Paths, method string, params, res any) error {
		switch p := params.(type) {
		case proto.FilesReposParams:
			if f.noRepos {
				return proto.Errorf(proto.ErrBadParams, "unknown method")
			}
			res.(*proto.FilesReposResult).Repos = f.repos
		case proto.FilesStampParams:
			f.stamped = append(f.stamped, p)
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
			f.read = append(f.read, p)
			r, ok := f.texts[p.Path]
			if !ok {
				return proto.Errorf(proto.ErrFileGone, "no longer there: %s", p.Path)
			}
			*res.(*proto.FilesReadResult) = r
		case proto.LibraryThumbnailParams:
			data, ok := f.thumbs[p.ID+"/"+p.Name]
			if !ok {
				return proto.Errorf(proto.ErrNoThumbnail, "%s has none", p.Name)
			}
			res.(*proto.LibraryThumbnailResult).Data = data
		case proto.FilesDiffParams:
			f.diffed = append(f.diffed, p)
			*res.(*proto.FilesDiffResult) = f.diffs[p.Path]
		case proto.ProjectFilesParams:
			res.(*proto.LibraryListResult).Files = nil
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
	fv.found = []proto.FileEntry{
		{Name: "README.md", Path: "README.md", Kind: proto.FileKindFile, Status: proto.FileModified},
		{Name: "types.go", Path: "internal/tui/types.go", Kind: proto.FileKindFile, Status: proto.FileAdded},
		{Name: "only.go", Path: "old/only.go", Kind: proto.FileKindFile, Status: proto.FileDeleted},
	}
	fv.tree = changedTree(fv.found)
	write("changed only, cut", fv, 39)
	fv.shut = map[string]bool{"internal/tui": true, "old": true}
	write("changed only, folders folded", fv, 39)
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
	tv := worktreeViewer(tmclient.Paths{}, "s-4", "", proto.FileEntry{Name: "evil\x1b[2J", Path: "evil"}, false)
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

// changedFound is a changed result as the server sends it: whole paths,
// sorted, a deleted file under a folder that's gone.
func changedFound() []proto.FileEntry {
	return []proto.FileEntry{
		{Name: "README.md", Path: "README.md", Kind: proto.FileKindFile, Status: proto.FileModified},
		{Name: "a.go", Path: "internal/tui/a.go", Kind: proto.FileKindFile, Status: proto.FileModified},
		{Name: "z.go", Path: "internal/tui/widgets/z.go", Kind: proto.FileKindFile, Status: proto.FileAdded},
		{Name: "only.go", Path: "old/only.go", Kind: proto.FileKindFile, Status: proto.FileDeleted},
	}
}

// TestChangedTree: the changed files as folders, listed as a folder is;
// the folders that hold them come from the paths, a gone one too.
func TestChangedTree(t *testing.T) {
	tree := changedTree(changedFound())
	names := func(dir string) []string {
		var out []string
		for _, e := range tree[dir] {
			out = append(out, e.Path+":"+e.Kind+":"+e.Status)
		}
		return out
	}
	for dir, want := range map[string][]string{
		"":                     {"internal:dir:", "old:dir:", "README.md:file:M"},
		"internal":             {"internal/tui:dir:"},
		"internal/tui":         {"internal/tui/widgets:dir:", "internal/tui/a.go:file:M"},
		"internal/tui/widgets": {"internal/tui/widgets/z.go:file:A"},
		"old":                  {"old/only.go:file:D"},
	} {
		if got := names(dir); !slices.Equal(got, want) {
			t.Errorf("%q: %v, want %v", dir, got, want)
		}
	}
	for _, e := range tree[""] {
		if e.Kind == proto.FileKindDir && (!e.Changed || e.Name == "" || strings.Contains(e.Name, "/")) {
			t.Errorf("a made folder: %+v", e)
		}
	}
	// A row the result has itself keeps its mark: a file deleted where a
	// folder now is. And what git says is under a file still shows under it.
	tree = changedTree([]proto.FileEntry{
		{Name: "both", Path: "both", Kind: proto.FileKindDir, Status: proto.FileDeleted, Changed: true},
		{Name: "new.go", Path: "both/new.go", Kind: proto.FileKindFile, Status: proto.FileAdded},
		{Name: "was", Path: "was", Kind: proto.FileKindFile, Status: proto.FileAdded, Changed: true},
		{Name: "dir.go", Path: "was/dir.go", Kind: proto.FileKindFile, Status: proto.FileDeleted},
	})
	if got := names(""); !slices.Equal(got, []string{"both:dir:D", "was:file:A"}) {
		t.Errorf("top: %v", got)
	}
	fv := &filesView{changed: true, tree: tree}
	var ids []string
	for _, r := range fv.rows() {
		ids = append(ids, r.id())
	}
	if want := []string{"both", "both/new.go", "was", "was/dir.go"}; !slices.Equal(ids, want) {
		t.Errorf("rows %v, want %v", ids, want)
	}
}

// TestFilesChangedKeys: Changed only is a tree whose folders start open;
// the keys and a click fold it as in the tree, without listing anything or
// touching the tree's own folding, and a refresh keeps both. A filter alone
// stays flat; with Changed only it stays a tree of what it matches.
func TestFilesChangedKeys(t *testing.T) {
	c, f := filesClient(t)
	f.found = map[string][]proto.FileEntry{"": changedFound(), "a.go": changedFound()[1:2]}
	filesPress(c, "right") // the tree's own: internal, unfolded
	fv := c.info.files
	sel := func() string { return fv.selPath }
	ids := func() []string {
		var out []string
		for _, r := range fv.rows() {
			out = append(out, strings.Repeat(" ", r.depth)+r.id())
		}
		return out
	}
	all := []string{"internal", " internal/tui", "  internal/tui/widgets", "   internal/tui/widgets/z.go", "  internal/tui/a.go", "old", " old/only.go", "README.md"}
	filesPress(c, "c")
	if got := ids(); !slices.Equal(got, all) {
		t.Fatalf("changed only: %q", got)
	}
	if got := infoText(c); !strings.Contains(got, "▾ internal/") || !strings.Contains(got, "only.go") || strings.Contains(got, "internal/tui/a.go") {
		t.Fatalf("changed only, as a tree of names:\n%s", got)
	}
	n := f.lists.Load()
	filesPress(c, "right") // on internal, open: steps in
	if sel() != "internal/tui" {
		t.Fatalf("→ on an open folder: %q", sel())
	}
	filesPress(c, "left") // folds it
	if got := ids(); !slices.Equal(got, []string{"internal", " internal/tui", "old", " old/only.go", "README.md"}) {
		t.Fatalf("← folded: %q", got)
	}
	filesPress(c, "left")
	if sel() != "internal" {
		t.Fatalf("← up: %q", sel())
	}
	filesPress(c, "down", "right") // unfolds again
	if got := ids(); !slices.Equal(got, all) {
		t.Fatalf("→ unfolded: %q", got)
	}
	filesPress(c, "enter") // folds, as in the tree
	if strings.Contains(infoText(c), "a.go") {
		t.Fatalf("enter folded nothing:\n%s", infoText(c))
	}
	if f.lists.Load() != n {
		t.Fatal("folding a changed tree listed folders")
	}
	if !fv.open["internal"] || len(fv.open) != 1 {
		t.Fatalf("the tree's own folding moved: %v", fv.open)
	}

	// A refresh keeps the folded folder and the selection.
	c.filesFetch()
	if strings.Contains(infoText(c), "a.go") || sel() != "internal/tui" {
		t.Fatalf("refresh: %q\n%s", sel(), infoText(c))
	}

	// With a filter too: a tree of what it matches, opened again so what it
	// finds shows.
	filesPress(c, "/", "a", ".", "g", "o", "enter")
	if got := f.seen[len(f.seen)-1]; !got.Changed || got.Query != "a.go" {
		t.Fatalf("both: %+v", got)
	}
	if got := ids(); !slices.Equal(got, []string{"internal", " internal/tui", "  internal/tui/a.go"}) {
		t.Fatalf("both: %q", got)
	}
	filesPress(c, "esc")

	// Off and on: the folders start open again, and the tree's are back.
	filesPress(c, "left") // shuts something
	filesPress(c, "c")
	if !fv.open["internal"] || fv.shut != nil || !slices.Contains(ids(), " internal/types.go") {
		t.Fatalf("off: %v %v %q", fv.open, fv.shut, ids())
	}
	filesPress(c, "c")
	if got := ids(); !slices.Equal(got, all) {
		t.Fatalf("on again: %q", got)
	}
	filesPress(c, "c")

	// A filter alone stays the flat search.
	f.found["a.go"] = changedFound()[1:2]
	filesPress(c, "/", "a", ".", "g", "o", "enter")
	if rows := fv.rows(); len(rows) != 1 || rows[0].depth != 0 || !strings.Contains(infoText(c), "internal/tui/a.go") {
		t.Fatalf("a filter: %+v\n%s", rows, infoText(c))
	}
}

// TestFilesChangedClick: a click folds a changed tree's folder, and a
// deleted file under a gone folder selects and reads as a file does.
func TestFilesChangedClick(t *testing.T) {
	c, f := filesClient(t)
	f.found = map[string][]proto.FileEntry{"": changedFound()}
	filesPress(c, "c")
	fv := c.info.files
	at := func(id string) int {
		return slices.IndexFunc(fv.rows(), func(r fileRow) bool { return r.id() == id })
	}
	c.mu.Lock()
	c.filesClick(infoHit{kind: hitFileRow, task: at("internal")}, 0)
	if strings.Contains(infoText(c), "a.go") || !fv.shut["internal"] {
		t.Fatalf("a click folded nothing:\n%s", infoText(c))
	}
	c.mu.Lock()
	c.filesClick(infoHit{kind: hitFileRow, task: at("old/only.go")}, 0)
	if fv.selPath != "old/only.go" {
		t.Fatalf("a click selects: %q", fv.selPath)
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
	if c.info.files != nil || !strings.Contains(c.flash, "isn't paired") || strings.Contains(c.flash, "\x1b") {
		t.Fatalf("paired: %v %q", c.info.files, c.flash)
	}
	// A director has no folder, but has its Attached.
	c.flash = ""
	c.info.data = &infoData{slug: "prog", session: proto.SessionInfo{ID: "s-9"}, prog: &programInfo{}}
	filesPress(c, "F")
	if fv := c.info.files; fv == nil || fv.program != "prog" || !fv.attached {
		t.Fatalf("director: %+v %q", fv, c.flash)
	}
	filesPress(c, "a")
	if !c.info.files.attached || !strings.Contains(c.flash, "no worktree") {
		t.Fatalf("a on a director's: %+v %q", c.info.files, c.flash)
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
	for id, want := range map[string]string{"out": "doesn't open"} {
		selectRow(id)
		filesPress(c, "enter")
		if !strings.Contains(c.flash, want) || c.over != nil {
			t.Errorf("%s: flash %q, over %+v", id, c.flash, c.over)
		}
	}
	tv := worktreeViewer(tmclient.Paths{}, "s-4", "", proto.FileEntry{Name: "Makefile", Path: "Makefile"}, false)
	if tv.body != "all:" || !strings.Contains(tv.title, "Makefile") {
		t.Errorf("viewer %+v", tv)
	}
	if strings.Contains(tv.title, "secrets") {
		t.Errorf("a plain file's title: %q", tv.title)
	}
	tv = worktreeViewer(tmclient.Paths{}, "s-4", "", proto.FileEntry{Name: "Makefile", Path: "Makefile", Ignored: true}, true)
	if tv.body != "all:" || !strings.Contains(tv.title, "git-ignored: may hold secrets") {
		t.Errorf("an ignored file's viewer %+v", tv)
	}
	f.texts["pic.png"] = proto.FilesReadResult{Name: "pic.png", Path: "pic.png", Size: 9, Binary: true}
	tv = worktreeViewer(tmclient.Paths{}, "s-4", "", proto.FileEntry{Name: "pic.png", Path: "pic.png"}, false)
	if tv.body != "" || !strings.Contains(tv.note, "not text") {
		t.Errorf("binary viewer %+v", tv)
	}
	tv = worktreeViewer(tmclient.Paths{}, "s-4", "", proto.FileEntry{Name: "pic.png", Path: "pic.png", Ignored: true}, true)
	if strings.Contains(tv.title, "secrets") {
		t.Errorf("no text shows, nothing to warn of: %q", tv.title)
	}
	tv = worktreeViewer(tmclient.Paths{}, "s-4", "", proto.FileEntry{Name: "nope", Path: "nope"}, false)
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
	tv := worktreeViewer(tmclient.Paths{}, "s-4", "", proto.FileEntry{Name: "big", Path: "big"}, false)
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

// repoView is the Files view of a coordinator's project with repos.
func repoView(t *testing.T, f *fakeFiles) (*filesView, func() string) {
	t.Helper()
	f.install(t)
	fv := &filesView{session: "s-1", slug: "demo", open: map[string]bool{}, dirs: map[string][]proto.FileEntry{}}
	fv.take(fetchFiles(tmclient.Paths{}, *fv, fv.filesFolders()))
	return fv, func() string {
		lines, _ := filesLay(fv, 48, true)
		return ansi.Strip(strings.Join(lines, "\n"))
	}
}

// TestFilesRepoLabel: a coordinator's view names the repo and says it's the
// main checkout; the picker's mark comes only with a choice to make; a
// thread's view and a server without the list stay as they were.
func TestFilesRepoLabel(t *testing.T) {
	_, one := repoView(t, &fakeFiles{dirs: sampleTree(), repos: []proto.FileRepo{{Name: "app", Path: "/r/app"}}})
	if s := one(); !strings.Contains(s, "app · main checkout") || strings.Contains(s, "▾") {
		t.Errorf("one repo:\n%s", s)
	}
	_, two := repoView(t, &fakeFiles{dirs: sampleTree(), repos: []proto.FileRepo{{Name: "app", Path: "/r/app"}, {Name: "site", Path: "/r/site"}}})
	if s := two(); !strings.Contains(s, "app · main checkout ▾") {
		t.Errorf("two repos:\n%s", s)
	}
	old := &fakeFiles{dirs: sampleTree(), noRepos: true}
	fv, older := repoView(t, old)
	if s := older(); strings.Contains(s, "main checkout") {
		t.Errorf("an older server:\n%s", s)
	}
	if fv.loaded != true || old.seen[0].Repo != "" {
		t.Errorf("an older server is asked for no repo: %+v", old.seen)
	}
	own := &fakeFiles{dirs: sampleTree(), repos: []proto.FileRepo{{Name: "app", Path: "/r/app"}, {Name: "site", Path: "/r/site"}}}
	own.install(t)
	tv := &filesView{session: "s-4", slug: "demo", thread: "t-0005", open: map[string]bool{}, dirs: map[string][]proto.FileEntry{}}
	tv.take(fetchFiles(tmclient.Paths{}, *tv, tv.filesFolders()))
	lines, _ := filesLay(tv, 48, true)
	if s := ansi.Strip(strings.Join(lines, "\n")); strings.Contains(s, "main checkout") || len(tv.repos) != 0 {
		t.Errorf("a thread:\n%s", s)
	}
}

// TestFilesRepoPicker: r lists the repos, enter shows one: every later call
// names it, nothing of the last tree stays; esc changes nothing; a repo off
// the list falls back to the first; one that can't open says why and stays.
func TestFilesRepoPicker(t *testing.T) {
	f := &fakeFiles{dirs: sampleTree(), repos: []proto.FileRepo{{Name: "app", Path: "/r/app"}, {Name: "site", Path: "/r/site"}},
		texts: map[string]proto.FilesReadResult{"a": {Name: "a", Path: "a", Size: 2, Data: []byte("x\n")}}}
	fv, show := repoView(t, f)
	fv.open["internal"], fv.sel = true, 3
	if f.seen[0].Repo != "" || f.stamped[0].Repo != "" {
		t.Errorf("the default names no repo: %+v %+v", f.seen[0], f.stamped[0])
	}

	fv.picking, fv.pickSel = true, fv.repoShown()
	s := show()
	if !strings.Contains(s, "✓ app") || !strings.Contains(s, "  site") || strings.Contains(s, "Makefile") {
		t.Errorf("the picker:\n%s", s)
	}
	if lines, hits := filesLay(fv, 48, true); len(lines) != len(hits) || hits[3].kind != hitFilesRepoRow || hits[3].task != 1 || hits[1].kind != hitFilesRepo {
		t.Errorf("the picker's hits: %+v", hits)
	}
	if fv.pickRepo(0) || fv.repo != "" || fv.picking || !fv.open["internal"] {
		t.Errorf("the repo shown again changed the view: %+v", fv)
	}

	if !fv.pickRepo(1) || fv.repo != "/r/site" || fv.loaded || fv.sel != 0 || len(fv.open) != 0 || len(fv.dirs) != 0 {
		t.Errorf("chose the second: %+v", fv)
	}
	if s := show(); !strings.Contains(s, "site · main checkout") || !strings.Contains(s, "loading") {
		t.Errorf("while it loads:\n%s", s)
	}
	f.seen, f.stamped = nil, nil
	fv.take(fetchFiles(tmclient.Paths{}, *fv, fv.filesFolders()))
	if f.seen[0].Repo != "/r/site" || f.stamped[0].Repo != "/r/site" {
		t.Errorf("the calls name it: %+v %+v", f.seen, f.stamped)
	}
	if tv := worktreeViewer(tmclient.Paths{}, fv.session, fv.repo, proto.FileEntry{Name: "a", Path: "a"}, false); tv == nil || f.read[0].Repo != "/r/site" {
		t.Errorf("a read names it: %+v", f.read)
	}
	if len(f.diffed) == 0 || f.diffed[0].Repo != "/r/site" {
		t.Errorf("a diff names it: %+v", f.diffed)
	}
	f.diffed = nil
	worktreeViewer(tmclient.Paths{}, fv.session, fv.repo, proto.FileEntry{Name: "a", Path: "a", Status: proto.FileDeleted}, false)
	if len(f.diffed) == 0 || f.diffed[0].Repo != "/r/site" {
		t.Errorf("a deleted file's diff names it: %+v", f.diffed)
	}

	// A repo that can't open: it says why, stays chosen, and the choice
	// stays open, rather than falling to Attached.
	f.refusal = proto.Errorf(proto.ErrNoFiles, "gone")
	fv.choose("/r/site")
	fv.take(fetchFiles(tmclient.Paths{}, *fv, fv.filesFolders()))
	if fv.attached || fv.err != "gone" || fv.repo != "/r/site" {
		t.Errorf("a chosen repo refused: %+v", fv)
	}
	if s := show(); !strings.Contains(s, "site · main checkout ▾") || !strings.Contains(s, "gone") {
		t.Errorf("a chosen repo refused:\n%s", s)
	}
	f.refusal = nil

	// Taken off the project's list since: back to the first.
	f.repos = f.repos[:1]
	f.seen = nil
	fv.take(fetchFiles(tmclient.Paths{}, *fv, fv.filesFolders()))
	if fv.repo != "" || f.seen[0].Repo != "" {
		t.Errorf("off the list: %q %+v", fv.repo, f.seen)
	}
	if s := show(); !strings.Contains(s, "app · main checkout") || strings.Contains(s, "▾") {
		t.Errorf("off the list:\n%s", s)
	}
}

// TestFilesRepoKeys: the picker through the keyboard.
func TestFilesRepoKeys(t *testing.T) {
	c, f := filesClient(t)
	fv := c.info.files
	press := func(name string) {
		t.Helper()
		filesPress(c, name)
	}
	press("r")
	if fv.picking {
		t.Error("r on a thread opened a picker")
	}
	f.repos = []proto.FileRepo{{Name: "app", Path: "/r/app"}, {Name: "site", Path: "/r/site"}}
	fv.repos = f.repos
	press("r")
	if !fv.picking || fv.pickSel != 0 || !strings.Contains(filesHint(fv), "repo ▸") {
		t.Fatalf("r: %+v", fv)
	}
	press("esc")
	if fv.picking || c.kb != areaInfo {
		t.Errorf("esc closes the picker alone: %+v", fv)
	}
	press("r")
	press("down")
	press("enter")
	if fv.picking || fv.repo != "/r/site" {
		t.Errorf("down, enter: %+v", fv)
	}
}

// TestFilesDiffGolden: how the viewer shows each kind of file's changes, as
// testdata/files-diff.golden has them (-update rewrites it): no changes, a
// change and a rename, a new file, a deleted one, binary, over-size, and a
// server without the method.
func TestFilesDiffGolden(t *testing.T) {
	freshConsole(t)
	f := &fakeFiles{}
	f.install(t)
	f.texts = map[string]proto.FilesReadResult{
		"same.go":  {Name: "same.go", Path: "same.go", Size: 8, Data: []byte("a\nb\nc\n")},
		"edit.go":  {Name: "edit.go", Path: "edit.go", Size: 12, Data: []byte("one\nTWO\nthree\nsix\n" + strings.Repeat("y", 50) + "\n")},
		"moved.go": {Name: "moved.go", Path: "moved.go", Size: 4, Data: []byte("a\nB\n")},
		"new.go":   {Name: "new.go", Path: "new.go", Size: 8, Data: []byte("package new\n\nfunc N() {}\n")},
		"pic.png":  {Name: "pic.png", Path: "pic.png", Size: 900, Binary: true},
		"big.go":   {Name: "big.go", Path: "big.go", Size: 3 << 20, Truncated: false, Data: []byte(strings.Repeat("big\n", proto.LibraryChunk/4))},
	}
	for name, r := range f.texts {
		if name != "big.go" && !r.Binary {
			r.Size = int64(len(r.Data))
			f.texts[name] = r
		}
	}
	f.diffs = map[string]proto.FilesDiffResult{
		"edit.go": {Status: "M", Base: "origin/main", Added: 2, Deleted: 3, Hunks: []proto.FilesHunk{
			{Old: 2, New: 2, Added: 1, Removed: []string{"two"}},
			{Old: 4, New: 4, Removed: []string{"four", "five"}},
			{Old: 8, New: 5, Added: 1},
		}},
		"moved.go": {Status: "M", From: "old.go", Base: "origin/main", Added: 1, Deleted: 1, Hunks: []proto.FilesHunk{{Old: 2, New: 2, Added: 1, Removed: []string{"b"}}}},
		"new.go":   {Status: "A", Base: "origin/main", Added: 3},
		"gone.go":  {Status: "D", Base: "origin/main", Size: 20, Deleted: 2, Hunks: []proto.FilesHunk{{Old: 1, New: 1, Removed: []string{"package gone", "func G() {}"}}}},
		"gone.png": {Status: "D", Base: "origin/main", Size: 900, Binary: true},
		"gone.log": {Status: "D", Base: "origin/main", Size: 3 << 20, Deleted: 9000, TooBig: true},
		"pic.png":  {Status: "M", Base: "origin/main", Binary: true},
		"big.go":   {Status: "M", Base: "origin/main", Added: 12, Deleted: 3, TooBig: true},
	}
	var b strings.Builder
	show := func(what, path, status string) {
		tv := worktreeViewer(tmclient.Paths{}, "s-4", "", proto.FileEntry{Name: path, Path: path, Status: status}, false)
		fmt.Fprintf(&b, "%s: %s\n", what, tv.title)
		lines := tv.lines(40)
		for _, l := range lines[:min(len(lines), 12)] {
			b.WriteString("│" + strings.TrimRight(ansi.Strip(l), " ") + "\n")
		}
		if len(lines) > 12 {
			fmt.Fprintf(&b, "│… %s\n", ansi.Strip(lines[len(lines)-1]))
		}
		fmt.Fprintf(&b, "opens at row %d\n\n", tv.changeAt)
	}
	show("no changes", "same.go", "")
	show("modified", "edit.go", "M")
	show("renamed", "moved.go", "A")
	show("new", "new.go", "A")
	show("deleted", "gone.go", "D")
	show("deleted, binary", "gone.png", "D")
	show("deleted, over-size", "gone.log", "D")
	show("binary", "pic.png", "M")
	show("over-size", "big.go", "M")
	path := filepath.Join("testdata", "files-diff.golden")
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
		t.Errorf("the viewer differs from %s:\n%s", path, b.String())
	}
}

// TestFilesDiffOpensAtChange: the viewer opens at the first change, a few
// lines above it, and scrolls on from there; colour goes on changed lines
// only, and a JSON file with changes isn't reindented under its hunks.
func TestFilesDiffOpensAtChange(t *testing.T) {
	var body strings.Builder
	for i := range 200 {
		fmt.Fprintf(&body, "line %d\n", i+1)
	}
	tv := &textView{title: "f", body: body.String()}
	tv.mark(proto.FilesDiffResult{Status: "M", Hunks: []proto.FilesHunk{{Old: 100, New: 100, Added: 1}}})
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 100, Height: 30, Over: &Over{Session: "s-4", Screen: []string{"", "row"}, Viewer: tv}})
	m.setData(src.data)
	tv.render(m)
	if tv.scroll != 99-diffLeadIn {
		t.Errorf("opens at %d", tv.scroll)
	}
	before := tv.scroll
	tv.key(m, arrow(1))
	tv.render(m)
	if tv.scroll != before+1 {
		t.Errorf("scrolled to %d", tv.scroll)
	}

	f := &fakeFiles{}
	f.install(t)
	f.texts = map[string]proto.FilesReadResult{"a.json": {Name: "a.json", Path: "a.json", Size: 7, Data: []byte(`{"a":1}`)}}
	f.diffs = map[string]proto.FilesDiffResult{"a.json": {Status: "M", Hunks: []proto.FilesHunk{{Old: 1, New: 1, Added: 1}}}}
	got := worktreeViewer(tmclient.Paths{}, "s", "", proto.FileEntry{Name: "a.json", Path: "a.json"}, false)
	if len(got.diff) != 1 || got.diff[0] != (diffLine{lineAdded, `{"a":1}`}) {
		t.Errorf("%+v", got.diff)
	}
	f.diffs = nil
	if got = worktreeViewer(tmclient.Paths{}, "s", "", proto.FileEntry{Name: "a.json", Path: "a.json"}, false); got.diff != nil || !strings.Contains(got.body, "  \"a\"") {
		t.Errorf("no changes, indented as before: %q %+v", got.body, got.diff)
	}
}

// TestMergeDiff: hunks in the text, at its ends and past them.
func TestMergeDiff(t *testing.T) {
	d := proto.FilesDiffResult{Status: "M", Hunks: []proto.FilesHunk{
		{Old: 1, New: 1, Removed: []string{"gone"}},
		{Old: 3, New: 2, Added: 1},
		{Old: 9, New: 9, Added: 2, Removed: []string{"past"}},
	}}
	got := mergeDiff("a\nb\nc\n", d)
	want := []diffLine{{lineRemoved, "gone"}, {lineSame, "a"}, {lineAdded, "b"}, {lineSame, "c"}, {lineRemoved, "past"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("%v, want %v", got, want)
	}
	if got := mergeDiff("", proto.FilesDiffResult{Status: "A"}); len(got) != 0 {
		t.Errorf("an empty new file: %v", got)
	}
}

// TestFilesRepoListMoves: a repo added while the view shows comes in on the
// poll, though nothing in the folder changed.
func TestFilesRepoListMoves(t *testing.T) {
	c, f := filesClient(t)
	fv := c.info.files
	fv.thread = ""
	f.repos = []proto.FileRepo{{Name: "app", Path: "/r/app"}, {Name: "site", Path: "/r/site"}}
	fv.polled = time.Time{}
	c.filesPoll()
	if len(fv.repos) != 2 {
		t.Errorf("the list after a poll: %+v", fv.repos)
	}
	lists := f.lists.Load()
	fv.polled = time.Time{}
	c.filesPoll()
	if f.lists.Load() != lists {
		t.Error("a poll with nothing changed listed the folder again")
	}
}

// farClient is filesClient's view of a paired server's session: F opens it
// where the switch is on, and says why not otherwise.
func farClient(t *testing.T) (*client, *fakeFiles) {
	t.Helper()
	f := &fakeFiles{dirs: map[string][]proto.FileEntry{"": {
		{Name: "internal", Path: "internal", Kind: proto.FileKindDir, Changed: true},
		{Name: ".env", Path: ".env", Kind: proto.FileKindFile, Status: proto.FileAdded, Withheld: true},
		{Name: "Makefile", Path: "Makefile", Kind: proto.FileKindFile},
		{Name: "logo.png", Path: "logo.png", Kind: proto.FileKindFile, Size: 9},
	}, "internal": {{Name: "types.go", Path: "internal/types.go", Kind: proto.FileKindFile, Status: proto.FileModified}}}}
	f.install(t)
	old := filesCall
	t.Cleanup(func() { filesCall = old })
	filesCall = func(paths tmclient.Paths, method string, params, res any) error {
		if method == proto.MethodPeerLibraryList {
			return proto.Errorf(proto.ErrPagesOff, "studio doesn't share this project's pages")
		}
		return old(paths, method, params, res)
	}
	c := infoClient(t, 140)
	c.kb = areaInfo
	pm := proto.PeerMirror{Alias: "studio", State: proto.LinkUp, Worktree: true}
	pr := proto.PeerMirrorProject{PeerProject: proto.PeerProject{Project: "acme", Files: true}}
	c.info.data = &infoData{slug: "@studio/acme", session: proto.SessionInfo{ID: "studio/s-1", Project: "acme", Thread: "t-0301"},
		remote: &remoteData{alias: "studio", shown: "studio", peer: &pm, project: &pr, sess: proto.PeerSession{ID: "s-1", Project: "acme", Thread: "t-0301"}, found: true}}
	return c, f
}

// TestFilesFarRefused: a session whose server can't or won't share says so
// and opens nothing.
func TestFilesFarRefused(t *testing.T) {
	c, _ := farClient(t)
	r := c.info.data.remote
	r.project.Files = false
	filesPress(c, "F")
	if c.info.files != nil || c.flash != "studio doesn't share acme's files" {
		t.Fatalf("off: %v %q", c.info.files, c.flash)
	}
	r.project.Files, r.peer.Worktree = true, false
	filesPress(c, "F")
	if c.info.files != nil || !strings.Contains(c.flash, "older tm") {
		t.Fatalf("older: %v %q", c.info.files, c.flash)
	}
}

// TestFilesFar: the view on a paired server's session: on that server, read
// only, withheld rows that don't open, no ignored files to ask for, no
// bytes of what isn't text; off clears it, offline keeps it, marked.
func TestFilesFar(t *testing.T) {
	c, f := farClient(t)
	filesPress(c, "F")
	fv := c.info.files
	if fv == nil || fv.far != "studio" || fv.slug != "acme" || fv.thread != "t-0301" {
		t.Fatalf("view: %+v", fv)
	}
	out := infoText(c)
	for _, want := range []string{"on studio · read only", ".env", "stays on studio", "logo.png", "not text", "Makefile"} {
		if !strings.Contains(out, want) {
			t.Fatalf("no %q:\n%s", want, out)
		}
	}
	if strings.Contains(filesHint(fv), "ignored") {
		t.Errorf("hint offers ignored: %q", filesHint(fv))
	}
	n := len(f.seen)
	filesPress(c, "i")
	if fv.ignored || len(f.seen) != n {
		t.Errorf("i asked for ignored files: %v", f.seen[n:])
	}
	// The withheld row says why, and enter doesn't read it.
	filesPress(c, "down")
	if !strings.Contains(infoText(c), "it may hold secrets") {
		t.Fatalf("no reason under .env:\n%s", infoText(c))
	}
	filesPress(c, "enter")
	if len(f.read) != 0 || !strings.Contains(c.flash, "stays on studio") {
		t.Fatalf("read %v, flash %q", f.read, c.flash)
	}
	// Offline: what shows stays, marked, and no new read.
	f.refusal = proto.Errorf(proto.ErrPeerOffline, "studio is offline")
	c.filesFetch()
	out = infoText(c)
	if !fv.offline || !strings.Contains(out, "studio is offline: as of") || !strings.Contains(out, "Makefile") {
		t.Fatalf("offline: %+v\n%s", fv, out)
	}
	// Slow: it keeps what it has.
	f.refusal = proto.Errorf(proto.ErrRateLimited, "studio is slow to answer")
	c.filesFetch()
	if out = infoText(c); !strings.Contains(out, "studio is slow to answer") || !strings.Contains(out, "Makefile") {
		t.Fatalf("slow:\n%s", out)
	}
	// Off: tree and all go, and it says so.
	f.refusal = proto.Errorf(proto.ErrFilesOff, "studio doesn't share acme's files")
	c.filesFetch()
	out = infoText(c)
	if !strings.Contains(out, "studio stopped sharing acme's") || strings.Contains(out, "Makefile") || strings.Contains(out, ".env") {
		t.Fatalf("off:\n%s", out)
	}
	if len(fv.dirs) != 0 || fv.stamp != "" {
		t.Errorf("kept: %+v", fv)
	}
	// On again: it comes back at the next look.
	f.refusal = nil
	fv.polled = time.Time{}
	c.filesPoll()
	if out = infoText(c); fv.gone != "" || !strings.Contains(out, "Makefile") {
		t.Fatalf("back on: %q\n%s", fv.gone, out)
	}
	// A session that stopped says so, in the design's words.
	f.refusal = proto.Errorf(proto.ErrUnknownSession, "studio isn't running a session s-1")
	c.filesFetch()
	if out = infoText(c); !strings.Contains(out, "agent isn't running on") {
		t.Fatalf("stopped:\n%s", out)
	}
}

// TestFilesFarViewer: a far file has no bytes to open elsewhere, and what
// shows goes when the switch does.
func TestFilesFarViewer(t *testing.T) {
	f := &fakeFiles{texts: map[string]proto.FilesReadResult{
		"a.go":  {Name: "a.go", Path: "a.go", Size: 5, Data: []byte("hello")},
		"p.png": {Name: "p.png", Path: "p.png", Size: 9, Binary: true},
	}}
	f.install(t)
	tv := farViewer(tmclient.Paths{}, "studio/s-1", "", proto.FileEntry{Name: "p.png", Path: "p.png"}, "gone")
	if tv.open != nil || tv.image != nil || tv.note != noTextNote {
		t.Fatalf("binary: %+v", tv)
	}
	for _, r := range f.read {
		if r.Binary {
			t.Fatalf("asked for bytes: %+v", r)
		}
	}
	tv = farViewer(tmclient.Paths{}, "studio/s-1", "", proto.FileEntry{Name: "a.go", Path: "a.go"}, "it went")
	if !strings.Contains(tv.body, "hello") || tv.keep == nil || tv.keep() != "" {
		t.Fatalf("text: %+v", tv)
	}
	old := filesCall
	defer func() { filesCall = old }()
	filesCall = func(tmclient.Paths, string, any, any) error { return proto.Errorf(proto.ErrFilesOff, "off") }
	if w := tv.keep(); w != "it went" {
		t.Fatalf("keep says %q", w)
	}
	tv.goes("it went")
	if tv.body != "" || tv.note != "it went" || tv.keep != nil {
		t.Fatalf("after: %+v", tv)
	}
}

// programFilesFake answers a program's library: the director's own, a
// member here and one on a paired server, and reads each by the form it
// is named by; calls says what was asked.
func programFilesFake(t *testing.T) *[]any {
	t.Helper()
	at := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	files := []proto.LibraryFile{
		{Thread: "seat", Title: "Director's files", State: "kept", Name: "plan.md", Size: 5, Time: at},
		{Thread: "seat", Title: "Coordinator's files", State: "kept", Name: "site.html", Size: 6, Time: at, Project: "web"},
		{Thread: "t-0002", Task: "T5", Title: "Fix it", State: "running", Name: "shot.png", Size: 7, Time: at, Project: "web"},
		{Thread: "t-0003", Task: "T6", Title: "Far work", State: "running", Name: "far.md", Size: 8, Time: at, Project: "far", Server: "mini"},
	}
	var calls []any
	old, oldGo := filesCall, filesGo
	filesGo = func(g func()) { g() }
	filesCall = func(_ tmclient.Paths, method string, params, res any) error {
		calls = append(calls, params)
		switch p := params.(type) {
		case proto.ProjectFilesParams:
			if p.Program != "acme" || p.Project != "" {
				t.Errorf("listed %+v", p)
			}
			*res.(*proto.LibraryListResult) = proto.LibraryListResult{Files: files, Note: "gone on away can't be read now"}
		case proto.LibraryReadParams:
			*res.(*proto.LibraryReadResult) = proto.LibraryReadResult{Name: p.Name, Size: 4, Data: []byte("text")}
		case proto.PeerLibraryReadReadParams:
			*res.(*proto.LibraryReadResult) = proto.LibraryReadResult{Name: p.Name, Size: 4, Data: []byte("text")}
		case proto.LibraryThumbnailParams, proto.PeerLibraryThumbnailReadParams:
			return proto.Errorf(proto.ErrNoThumbnail, "none")
		default:
			t.Fatalf("method %s: a director's view has no folder to list", method)
		}
		return nil
	}
	t.Cleanup(func() {
		filesCall, filesGo = old, oldGo
		filesKept.Store(nil)
	})
	return &calls
}

// TestProgramFilesView: beside a director F opens on Attached with no
// Worktree tab and no folder asked for: its own files first, then each
// member's under its project, with the note, the filter across all of them,
// the kind chips by t and by a click, and esc letting both go. A file is read
// by the form it is named by: the director's by program, a member's by
// project, a paired member's over the link.
func TestProgramFilesView(t *testing.T) {
	calls := programFilesFake(t)
	fv := &filesView{session: "s-1", slug: "acme", program: "acme", attached: true, open: map[string]bool{}, dirs: map[string][]proto.FileEntry{}}
	fv.take(fetchFiles(tmclient.Paths{}, *fv, nil))
	text := func() string {
		lines, _ := filesLay(fv, 60, true)
		return ansi.Strip(strings.Join(lines, "\n"))
	}
	got := text()
	if strings.Contains(got, "Worktree") || !strings.Contains(got, "Attached") {
		t.Fatalf("tabs:\n%s", got)
	}
	last := -1
	for _, w := range []string{"gone on away", "Director's files", "plan.md", "\nweb ", "Coordinator's files", "site.html", "shot.png", "far on mini", "far.md"} {
		i := strings.Index(got, w)
		if i <= last {
			t.Fatalf("%q out of order:\n%s", w, got)
		}
		last = i
	}
	if !strings.Contains(got, " all 4 ") || !strings.Contains(got, " images 1 ") {
		t.Fatalf("no chips:\n%s", got)
	}

	fv.filter = "mini"
	if rows := fv.rows(); len(rows) != 1 || rows[0].lib.Name != "far.md" {
		t.Fatalf("filter by server: %+v", rows)
	}
	fv.filter = ""
	fv.pick = "images"
	if rows := fv.rows(); len(rows) != 1 || rows[0].lib.Name != "shot.png" {
		t.Fatalf("by kind: %+v", rows)
	}
	fv.pick = ""

	if tv := programFileViewer(tmclient.Paths{}, "acme", *fv.rows()[0].lib); tv.body != "text" {
		t.Fatalf("the director's: %+v", tv)
	}
	if tv := programFileViewer(tmclient.Paths{}, "acme", *fv.rows()[3].lib); tv.body != "text" {
		t.Fatalf("the paired member's: %+v", tv)
	}
	var sawProgram, sawPeer bool
	for _, c := range *calls {
		switch p := c.(type) {
		case proto.LibraryReadParams:
			sawProgram = sawProgram || p.Program == "acme" && p.ID == "seat" && p.Project == ""
		case proto.PeerLibraryReadReadParams:
			sawPeer = p.Alias == "mini" && p.Project == "far" && p.Thread == "t-0003"
		}
	}
	if !sawProgram || !sawPeer {
		t.Fatalf("read forms: %+v", *calls)
	}
}

// TestCoordinatorAttachedOwnFirst: a coordinator's Attached lists its own
// files above its threads', whichever is newer.
func TestCoordinatorAttachedOwnFirst(t *testing.T) {
	at := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	fv := &filesView{attached: true, lib: []thread.LibFile{
		{Thread: "t-0002", Task: "T5", Title: "Fix it", Name: "notes.md", Size: 3, Time: at},
		{Thread: "seat", Title: "Coordinator's files", Name: "site.html", Size: 6, Time: at.Add(-time.Hour)},
	}}
	rows := fv.rows()
	if len(rows) != 2 || rows[0].lib.Name != "site.html" {
		t.Fatalf("rows %+v", rows)
	}
	lines, _ := filesLay(fv, 60, false)
	got := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(got, "Coordinator's files · 1 file") || strings.Contains(got, "· seat") {
		t.Fatalf("heading:\n%s", got)
	}
}
