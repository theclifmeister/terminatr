package tui

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	tmclient "github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/filetree"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/thumb"
)

// The Files view of the info panel (docs/SPEC.md §4): the panel's other
// face, F from its Info face, on a thread or a coordinator. Worktree is
// the session's folder as a tree (files.list, read only), with git's
// marks, a filter by name, Changed only (a tree built here from the changed
// result), and it refreshes while it shows (files.stamp);
// beside a coordinator that folder is a repo's main checkout, which the
// view says, with a picker when the project has more than one;
// Attached is the Library's files of that thread, of the whole project
// beside a coordinator, its own first; beside a director, which has no
// folder to list and no Worktree tab, its own files and then every member's
// by project, with the Library's filter and kinds so they can all be
// searched (library.list's program form, programFiles). Enter reads a file in the Library's viewer,
// drawn over the session, with its changes since the base marked
// (files.diff; filesdiff.go). Everything a folder holds is data: names go
// through oneLine and text through libText and textsafe, so no control
// character reaches the terminal, and the panel's draw takes styles only
// (plainUnder).
//
// On a paired server's session (docs/SPEC.md §16.18) the view asks the same
// methods, and that server holds every rule: it says "on studio · read only",
// has no ignored files to show, keeps nothing on disk and opens nothing
// elsewhere. Off, what showed goes; offline, it stays, marked.

const (
	// filesEvery is how often the view asks whether its folders changed.
	filesEvery = 2 * time.Second
	// filesFarEvery is how often it asks of a paired server's: that is a
	// request to the other machine.
	filesFarEvery = 5 * time.Second
)

// programFilesEvery is how often a director's view reads its program's
// library again.
const programFilesEvery = 10 * time.Second

// filesCall calls a files method of the server; a variable for tests.
var filesCall = func(paths tmclient.Paths, method string, params, res any) error {
	cl, err := tmclient.Connect(paths, false)
	if err != nil {
		return err
	}
	defer cl.Close()
	return cl.Call(method, params, res)
}

// filesLibrary is project slug's library, and filesLibRead reads a file
// of it; variables for tests.
var (
	filesLibrary = func(slug string) ([]thread.LibFile, error) { return (&ServerSource{}).Library(slug) }
	filesLibRead = func(slug, id, name string) ([]byte, bool, error) {
		return (&ServerSource{}).LibraryRead(slug, id, name, libMax)
	}
)

// filesGo runs a fetch off the keyboard's goroutine; tests run it in
// place.
var filesGo = func(f func()) { go f() }

// filesKept is the view left for the viewer over the session, taken back
// when the attach comes back to it (showInfo).
var filesKept atomic.Pointer[filesView]

// filesView is one console's Files view of one session. Guarded by
// client.mu.
type filesView struct {
	session string
	slug    string
	thread  string // "" beside a coordinator: the whole project's files
	program string // beside a director: its program, whose library is all this shows

	repos   []proto.FileRepo // a coordinator's choices (files.repos)
	repo    string           // the one chosen, "" for the first
	picking bool             // the repo picker shows, and has the keyboard
	pickSel int              // its selected row

	attached bool   // the Attached tab
	filter   string // by name
	typing   bool   // the keyboard is in the filter
	ignored  bool   // git-ignored files show
	changed  bool   // only what git marks

	open    map[string]bool              // the unfolded folders
	dirs    map[string][]proto.FileEntry // the folders as last listed
	found   []proto.FileEntry            // a filter's or changed's files
	tree    map[string][]proto.FileEntry // changed's files by folder
	shut    map[string]bool              // the folders changed's tree has folded
	sum     proto.FilesListResult        // the top's totals
	cut     bool                         // a listing was cut
	err     string                       // why the worktree doesn't show
	loaded  bool
	lib     []thread.LibFile
	pick    string // the kind a director's view is narrowed to
	libNote string // what a program's listing leaves out for now

	sel     int    // the selected row
	selPath string // its path, to find it again after a refresh
	lineOf  []int  // each row's line, as last laid out

	stamp  string
	polled time.Time
	gen    int // the newest fetch; an older one's answer is dropped

	// far is the paired server whose session this is, as the panel calls
	// it, "" for this server's; farAlias keys it. The rest only for a far one:
	farAlias string
	far      string
	done     bool            // the thread is resolved there, as the mirror has it
	gone     string          // it stopped sharing the files, in words: what showed left
	offline  bool            // the link is down: what shows is as of readAt
	slow     bool            // the last look got no answer in time
	readAt   time.Time       // when it last answered
	notText  map[string]bool // files a read found aren't text
}

func newFilesView(d *infoData) *filesView {
	fv := &filesView{session: d.session.ID, slug: d.slug, open: map[string]bool{}, dirs: map[string][]proto.FileEntry{}}
	if d.watch == nil {
		fv.thread = d.session.Thread
	}
	if r := d.remote; r != nil {
		fv.far, fv.farAlias, fv.slug, fv.thread = oneLine(r.shown), r.alias, d.session.Project, d.session.Thread
		if r.found {
			fv.slug, fv.thread = r.sess.Project, r.sess.Thread
		}
		if fv.slug == "" && r.project != nil {
			fv.slug = r.project.Project
		}
		if r.project != nil && r.project.Watch != nil && fv.thread != "" {
			fv.done = !slices.ContainsFunc(r.project.Watch.Threads, func(t proto.WatchThread) bool { return t.ID == fv.thread })
		}
	}
	if d.prog != nil {
		fv.program, fv.attached = d.slug, true
	}
	return fv
}

// every is how often the view asks whether its folders changed.
func (fv *filesView) every() time.Duration {
	if fv.program != "" {
		return programFilesEvery // each paired member's server is asked
	}
	if fv.far != "" {
		return filesFarEvery
	}
	return filesEvery
}

// farStopped says why a far session's folder can't be read: nothing runs it.
func (fv *filesView) farStopped() string {
	switch {
	case fv.thread == "":
		return fv.far + "'s coordinator isn't running, so its repos can't be read from here."
	case fv.done:
		return "This thread is done, so its worktree is gone from " + fv.far + "."
	}
	return "This thread's agent isn't running on " + fv.far + ", so its worktree can't be read from here."
}

// farGone says the far server's files switch went off, or its sharing did.
func (fv *filesView) farGone() string {
	return fv.far + " stopped sharing " + fv.slug + "'s files. What showed here is gone."
}

// clearTree drops all that shows of the folder.
func (fv *filesView) clearTree() {
	fv.dirs, fv.found, fv.tree = map[string][]proto.FileEntry{}, nil, nil
	fv.sum, fv.cut = proto.FilesListResult{}, false
	fv.open, fv.shut, fv.stamp = map[string]bool{}, nil, ""
	fv.repos, fv.repo, fv.picking = nil, "", false
}

// stopSharing is the far server's refusal to show its files any more. c.mu
// held.
func (fv *filesView) stopSharing() {
	fv.clearTree()
	fv.gone, fv.err, fv.offline, fv.slow = fv.farGone(), "", false, false
}

// repoShown is the index in repos of the repo the view shows, -1 for none
// to name: a thread's, or a server that lists none.
func (fv *filesView) repoShown() int {
	if len(fv.repos) == 0 {
		return -1
	}
	return max(slices.IndexFunc(fv.repos, func(r proto.FileRepo) bool { return r.Path == fv.repo }), 0)
}

// choose shows repo: nothing of the last one's tree stays. The next fetch
// fills it in.
func (fv *filesView) choose(repo string) {
	fv.repo, fv.picking = repo, false
	fv.open, fv.dirs, fv.found, fv.tree, fv.shut = map[string]bool{}, map[string][]proto.FileEntry{}, nil, nil, nil
	fv.sum, fv.cut, fv.err, fv.stamp = proto.FilesListResult{}, false, "", ""
	fv.sel, fv.selPath, fv.loaded = 0, "", false
}

// searching says the worktree comes from one search of the whole folder,
// not from the folders' listings.
func (fv *filesView) searching() bool { return fv.filter != "" || fv.changed }

// flat says the worktree shows a filter's flat list. Changed only stays a
// tree, with a filter too: of the changed files it matches.
func (fv *filesView) flat() bool { return fv.filter != "" && !fv.changed }

// unfolded says folder p shows what's in it. Changed only's folders start
// open, so the changes show at once, and a new filter opens them again;
// they don't touch the tree's own.
func (fv *filesView) unfolded(p string) bool {
	if fv.changed {
		return len(fv.tree[p]) > 0 && !fv.shut[p]
	}
	return fv.open[p]
}

// fold folds or unfolds folder p, and says whether it has to be listed.
func (fv *filesView) fold(p string) bool {
	switch {
	case fv.changed && fv.unfolded(p):
		if fv.shut == nil {
			fv.shut = map[string]bool{}
		}
		fv.shut[p] = true
	case fv.changed:
		delete(fv.shut, p)
	case fv.open[p]:
		delete(fv.open, p)
	default:
		fv.open[p] = true
		return true
	}
	return false
}

// changedTree is files.list's changed result as folders, each listed as
// a folder is: folders first, by name. The folders come from the paths, not
// the disk, so a deleted file sits under a folder that's gone. A row the
// result has itself keeps its own marks: it comes before what's under it.
func changedTree(found []proto.FileEntry) map[string][]proto.FileEntry {
	tree := map[string][]proto.FileEntry{}
	seen := map[string]bool{}
	add := func(dir string, e proto.FileEntry) {
		if !seen[e.Path] {
			seen[e.Path] = true
			tree[dir] = append(tree[dir], e)
		}
	}
	for _, e := range found {
		names := strings.Split(e.Path, "/")
		dir := ""
		for _, name := range names[:len(names)-1] {
			p := path.Join(dir, name)
			add(dir, proto.FileEntry{Name: name, Path: p, Kind: proto.FileKindDir, Changed: true})
			dir = p
		}
		add(dir, e)
	}
	for _, list := range tree {
		slices.SortFunc(list, func(a, b proto.FileEntry) int {
			if ad, bd := a.Kind == proto.FileKindDir, b.Kind == proto.FileKindDir; ad != bd {
				if ad {
					return -1
				}
				return 1
			}
			return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		})
	}
	return tree
}

// fileRow is a row of the view: a worktree entry at depth, or an attached
// file.
type fileRow struct {
	e     proto.FileEntry
	depth int
	lib   *thread.LibFile
}

func (r fileRow) id() string {
	if r.lib != nil {
		return r.lib.Group() + "/" + r.lib.Name
	}
	return r.e.Path
}

// rows are the view's rows, as they list.
func (fv *filesView) rows() []fileRow {
	var out []fileRow
	if fv.program != "" {
		for _, f := range libraryOrder(libShown(fv.lib, fv.filter, fv.pick)) {
			out = append(out, fileRow{lib: &f})
		}
		return out
	}
	if fv.attached {
		for _, f := range libraryOrder(fv.lib) {
			if fv.filter == "" || strings.Contains(strings.ToLower(f.Name), strings.ToLower(fv.filter)) {
				out = append(out, fileRow{lib: &f})
			}
		}
		return out
	}
	if fv.flat() {
		for _, e := range fv.found {
			out = append(out, fileRow{e: e})
		}
		return out
	}
	dirs := fv.dirs
	if fv.changed {
		dirs = fv.tree
	}
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		for _, e := range dirs[dir] {
			out = append(out, fileRow{e: e, depth: depth})
			if fv.unfolded(e.Path) {
				walk(e.Path, depth+1)
			}
		}
	}
	walk("", 0)
	return out
}

// settle finds the selected row again after the rows changed.
func (fv *filesView) settle(rows []fileRow) {
	for i, r := range rows {
		if r.id() == fv.selPath {
			fv.sel = i
			return
		}
	}
	fv.sel = min(fv.sel, max(len(rows)-1, 0))
	fv.note(rows)
}

// note remembers which row is selected.
func (fv *filesView) note(rows []fileRow) {
	fv.selPath = ""
	if fv.sel < len(rows) {
		fv.selPath = rows[fv.sel].id()
	}
}

// filesLay is the view's lines w cells wide and what a click on each does.
func filesLay(fv *filesView, w int, focused bool) ([]string, []infoHit) {
	tab := func(name string, on bool) string {
		if on {
			return th().Accent.Render(name)
		}
		return th().Faint.Render(name)
	}
	lines := []string{th().Title.Render("FILES") + "  " + tab("Worktree", !fv.attached) + th().Faint.Render(" · ") + tab("Attached", fv.attached)}
	hits := []infoHit{{kind: hitFilesTab}}
	if fv.program != "" {
		// Nothing to switch to: a director has no folder here.
		lines, hits = []string{th().Title.Render("FILES") + "  " + tab("Attached", true)}, []infoHit{{}}
	}
	add := func(l string) {
		lines = append(lines, l)
		hits = append(hits, infoHit{})
	}
	if fv.far != "" {
		add(th().Faint.Render("on " + fv.far + " · read only"))
		if fv.gone != "" && !fv.attached {
			add("")
			for _, l := range wrapLines(fv.gone, max(w, 8)) {
				add(l)
			}
			add("")
			add(th().Faint.Render("a Attached · esc back"))
			fv.lineOf = nil
			return lines, hits
		}
	}
	if shown := fv.repoShown(); !fv.attached && shown >= 0 {
		const says = " · main checkout"
		many := len(fv.repos) > 1
		room := w - ansi.StringWidth(says)
		if many {
			room -= 2
		}
		l := strings.TrimRight(fit(oneLine(fv.repos[shown].Name), max(room, 4)), " ") + th().Faint.Render(says)
		h := infoHit{}
		if many {
			l += th().Accent.Render(" ▾")
			h.kind = hitFilesRepo
		}
		lines, hits = append(lines, l), append(hits, h)
		if fv.picking {
			for i, r := range fv.repos {
				mark := "  "
				if i == shown {
					mark = "✓ "
				}
				l := fit(mark+oneLine(r.Name), w)
				if i == fv.pickSel && focused {
					l = th().Sel.Render(l)
				}
				lines, hits = append(lines, l), append(hits, infoHit{kind: hitFilesRepoRow, task: i})
			}
			fv.lineOf = nil
			return lines, hits
		}
	}
	rows := fv.rows()
	fv.settle(rows)
	switch {
	case fv.program != "" && fv.err != "":
		add(th().Warn.Render(oneLine(fv.err)))
	case fv.program != "" && !fv.loaded:
		add(th().Faint.Render("loading…"))
	case fv.attached && len(rows) > 0:
		add(th().Faint.Render(fileCount(len(rows))))
	case fv.attached:
	case fv.err != "" && fv.far != "":
		for _, l := range wrapLines(oneLine(fv.err), max(w, 8)) {
			add(th().Warn.Render(l))
		}
	case fv.err != "":
		add(th().Warn.Render(oneLine(fv.err)))
	case fv.offline:
		add(th().Warn.Render(fv.far + " is offline: as of " + age(infoClock().Sub(fv.readAt)) + " ago"))
	case !fv.loaded:
		add(th().Faint.Render("loading…"))
	case fv.slow:
		add(th().Faint.Render(fv.far + " is slow to answer"))
	case fv.sum.Git:
		add(fileChanged(fv.sum.Files) + " " + th().Good.Render(fmt.Sprintf("+%d", fv.sum.Added)) + " " + th().Bad.Render(fmt.Sprintf("−%d", fv.sum.Deleted)))
	default:
		add(th().Faint.Render("no repo here: no marks"))
	}
	var flags []string
	if !fv.attached {
		if fv.changed {
			flags = append(flags, "changed only")
		}
		if fv.ignored {
			flags = append(flags, "ignored too")
		}
	}
	switch {
	case fv.typing:
		add(th().Accent.Render("/") + " " + oneLine(fv.filter) + th().Accent.Render("▏"))
	case fv.filter != "":
		add(th().Faint.Render("/ ") + oneLine(fv.filter))
	}
	if len(flags) > 0 {
		add(th().Faint.Render(strings.Join(flags, " · ")))
	}
	if fv.program != "" {
		if fv.libNote != "" {
			add(th().Warn.Render(oneLine(fv.libNote)))
		}
		for i, l := range libChipLinesOf(fv.lib, fv.filter, fv.pick, w) {
			lines, hits = append(lines, l.text), append(hits, infoHit{kind: hitFilesChip, task: i})
		}
	}
	fv.lineOf = make([]int, len(rows))
	group, place := "", ""
	groups := map[string]int{}
	if fv.attached {
		for _, r := range rows {
			groups[r.lib.Group()]++
		}
	}
	for i, r := range rows {
		if r.lib != nil && libPlace(*r.lib) != place {
			place = libPlace(*r.lib)
			add(th().Title.Render(fit(libPlaceWords(*r.lib), w)))
		}
		if r.lib != nil && r.lib.Group() != group {
			group = r.lib.Group()
			if fv.program != "" {
				title, meta := libHeading(groupFiles(rows, group), w)
				add(title)
				add(meta)
			} else {
				add(libGroup(*r.lib, groups[group], w))
			}
		}
		fv.lineOf[i] = len(lines)
		l := fileRowLine(r, w, fv.unfolded(r.e.Path), fv.far, fv.notText[r.e.Path])
		switch {
		case i == fv.sel && focused:
			l = th().Sel.Render(fit(ansi.Strip(l), w))
		case fv.offline:
			l = th().Faint.Render(ansi.Strip(l))
		}
		lines = append(lines, l)
		hits = append(hits, infoHit{kind: hitFileRow, task: i})
	}
	switch {
	case len(rows) == 0 && fv.far != "" && fv.attached && fv.libNote != "":
		for _, l := range wrapLines(oneLine(fv.libNote), max(w, 8)) {
			add(th().Faint.Render(l))
		}
	case len(rows) == 0 && fv.program != "" && len(fv.lib) > 0:
		add(th().Faint.Render("nothing matches: esc clears"))
	case len(rows) == 0 && fv.program != "":
		if fv.loaded {
			add(th().Faint.Render("no files: the director keeps its own (tm library add), threads attach theirs"))
		}
	case len(rows) == 0 && fv.attached && fv.thread == "":
		add(th().Faint.Render("no files: a thread's report attaches them, the coordinator keeps its own (tm library add)"))
	case len(rows) == 0 && fv.attached:
		add(th().Faint.Render("no files: a thread's report attaches them"))
	case len(rows) == 0 && fv.loaded && fv.err == "":
		add(th().Faint.Render("nothing here"))
	}
	if fv.cut && !fv.attached {
		add(th().Faint.Render("… more files than show here"))
	}
	if fv.sel < len(rows) && rows[fv.sel].lib == nil && rows[fv.sel].e.Withheld && focused {
		for _, l := range wrapLines(withheldWords(rows[fv.sel].e, fv.far), max(w, 8)) {
			add(th().Faint.Render(l))
		}
	}
	return lines, hits
}

// groupFiles are the files of group among rows.
func groupFiles(rows []fileRow, group string) []thread.LibFile {
	var out []thread.LibFile
	for _, r := range rows {
		if r.lib != nil && r.lib.Group() == group {
			out = append(out, *r.lib)
		}
	}
	return out
}

// libPlaceWords is the heading over a member's files in a program's list.
func libPlaceWords(f thread.LibFile) string {
	if f.Server != "" {
		return oneLine(f.Project) + " on " + oneLine(f.Server)
	}
	return oneLine(f.Project)
}

// fileCount is a number of files in words.
func fileCount(n int) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
}

// fileChanged is a number of changed files in words.
func fileChanged(n int) string { return fmt.Sprintf("%d changed", n) }

// withheldWords say why a far server's entry shows and doesn't open.
func withheldWords(e proto.FileEntry, far string) string {
	if e.Kind == proto.FileKindDir {
		return "it may hold secrets, or be a checkout of its own, so it stays on " + far
	}
	return "its name says it may hold secrets, so it stays on " + far
}

// fileRowLine is a row: its fold mark and name, its mark at the end. far is
// the paired server it is on, "" for this one's; notText says a read of it
// found no text, or its name does.
func fileRowLine(r fileRow, w int, open bool, far string, notText bool) string {
	if f := r.lib; f != nil {
		size := fmt.Sprintf("%9s", sizeWords(f.Size))
		return fit("  "+oneLine(f.Name), max(w-len(size), 4)) + size
	}
	e := r.e
	lead := strings.Repeat("  ", r.depth)
	name := oneLine(e.Name)
	if r.depth == 0 && strings.Contains(e.Path, "/") {
		name = oneLine(e.Path) // a flat list's
	}
	switch {
	case e.Kind == proto.FileKindDir:
		name += "/"
	case e.LinkOut:
		name += " ↗"
	case e.Link:
		name += " →"
	}
	mark, look := "", th().Faint
	switch {
	case e.Withheld:
		mark = "stays on " + far
	case e.Status == proto.FileModified:
		mark, look = "M", th().Warn
	case e.Status == proto.FileAdded:
		mark, look = "A", th().Good
	case e.Status == proto.FileDeleted:
		mark, look = "D", th().Bad
	case e.Changed:
		mark, look = "•", th().Warn
	case e.Ignored:
		mark = "ignored"
	case far != "" && e.Kind == proto.FileKindFile && (notText || thumb.Supported(e.Name)):
		mark = "not text"
	}
	width := max(w-ansi.StringWidth(lead)-2-ansi.StringWidth(mark)-1, 4)
	text := fit(name, width)
	if e.Ignored || e.Status == proto.FileDeleted || e.LinkOut || e.Withheld || mark == "not text" {
		text = th().Faint.Render(text)
	}
	fold := "  "
	switch {
	case e.Withheld:
	case e.Kind == proto.FileKindDir && open:
		fold = "▾ "
	case e.Kind == proto.FileKindDir:
		fold = "▸ "
	}
	return lead + fold + text + " " + look.Render(mark)
}

// filesFetched is what a fetch brought.
type filesFetched struct {
	stamp   string
	sum     proto.FilesListResult
	dirs    map[string][]proto.FileEntry
	found   []proto.FileEntry
	cut     bool
	err     string
	noFiles bool
	lib     []thread.LibFile
	note    string           // a program's listing's
	repos   []proto.FileRepo // nil: not asked, or the server has no list
	offList bool             // the chosen repo left the project's list
	code    string           // a refusal's code
}

// fetchFiles asks the server for what the view shows. The stamp comes
// first: a change during the listing moves the next one.
func fetchFiles(paths tmclient.Paths, fv filesView, folders []string) filesFetched {
	var out filesFetched
	if fv.program != "" {
		lib, note, err := programFiles(paths, fv.program)
		out.lib, out.note = lib, note
		if err != nil {
			out.err, _ = filesErrText(err)
		}
		return out
	}
	if fv.far != "" {
		out.lib, out.note = attachedFar(paths, fv.farAlias, fv.slug, fv.thread)
		if fv.attached {
			// A request to the other machine for what doesn't show. The tab
			// back to Worktree asks.
			out.code = "attached"
			return out
		}
	} else {
		out.lib, _ = attachedFiles(fv.slug, fv.thread)
	}
	if fv.thread == "" {
		// A server without the method leaves the first repo, unnamed.
		var rs proto.FilesReposResult
		if filesCall(paths, proto.MethodFilesRepos, proto.FilesReposParams{Session: fv.session}, &rs) == nil {
			out.repos = rs.Repos
			if fv.repo != "" && !slices.ContainsFunc(rs.Repos, func(r proto.FileRepo) bool { return r.Path == fv.repo }) {
				fv.repo, out.offList = "", true
			}
		}
	}
	var st proto.FilesStampResult
	if filesCall(paths, proto.MethodFilesStamp, proto.FilesStampParams{Session: fv.session, Repo: fv.repo, Folders: folders}, &st) == nil {
		out.stamp = st.Stamp
	}
	list := func(p proto.FilesListParams) (proto.FilesListResult, error) {
		var res proto.FilesListResult
		p.Session, p.Repo, p.Ignored = fv.session, fv.repo, fv.ignored
		err := filesCall(paths, proto.MethodFilesList, p, &res)
		return res, err
	}
	if fv.searching() {
		res, err := list(proto.FilesListParams{Query: fv.filter, Changed: fv.changed})
		if err != nil {
			out.err, out.noFiles = filesErrText(err)
			out.code = filesErrCode(err)
			return out
		}
		out.sum, out.found, out.cut = res, res.Entries, res.Truncated
		return out
	}
	out.dirs = map[string][]proto.FileEntry{}
	for i, d := range folders {
		res, err := list(proto.FilesListParams{Path: d})
		if err != nil {
			code := filesErrCode(err)
			if i == 0 || fv.far != "" && farHolds(code) {
				out.err, out.noFiles = filesErrText(err)
				out.code = code
				return out
			}
			continue // gone since: it drops off the tree
		}
		if i == 0 {
			out.sum = res
		}
		out.dirs[d] = res.Entries
		out.cut = out.cut || res.Truncated
	}
	return out
}

// farHolds says a far server's refusal isn't about the folder asked for, so
// the rest of the tree mustn't drop for it.
func farHolds(code string) bool {
	switch code {
	case proto.ErrPeerOffline, proto.ErrRateLimited, proto.ErrFilesOff, proto.ErrNotShared, proto.ErrNoPeerFiles, proto.ErrUnknownSession:
		return true
	}
	return false
}

// attachedFar are the files of thread (all the project's for "") a paired
// server's project pages show, and why there are none when it says why.
func attachedFar(paths tmclient.Paths, alias, slug, id string) ([]thread.LibFile, string) {
	var res proto.PeerLibrary
	if err := filesCall(paths, proto.MethodPeerLibraryList, proto.PeerPagesReadParams{Alias: alias, Project: slug}, &res); err != nil {
		msg, _ := filesErrText(err)
		return nil, msg
	}
	var out []thread.LibFile
	for _, f := range res.Files {
		if id == "" || f.Thread == id {
			out = append(out, thread.LibFile{Thread: f.Thread, Task: f.Task, Title: f.Title, State: f.State, Name: f.Name, Size: f.Size, Time: f.Time})
		}
	}
	return out, ""
}

// attachedFiles are the files of thread (all the project's for "") in
// the Library.
func attachedFiles(slug, id string) ([]thread.LibFile, error) {
	all, err := filesLibrary(slug)
	if err != nil || id == "" {
		return all, err
	}
	var out []thread.LibFile
	for _, f := range all {
		if f.Thread == id {
			out = append(out, f)
		}
	}
	return out, nil
}

// programFiles is program slug's library as its director's view lists it:
// its own, then each member's, with what that leaves out for now.
func programFiles(paths tmclient.Paths, slug string) ([]thread.LibFile, string, error) {
	var res proto.LibraryListResult
	if err := filesCall(paths, proto.MethodLibraryList, proto.ProjectFilesParams{Program: slug}, &res); err != nil {
		return nil, "", err
	}
	out := make([]thread.LibFile, 0, len(res.Files))
	for _, f := range res.Files {
		out = append(out, thread.LibFile{Thread: f.Thread, Task: f.Task, TaskStatus: f.TaskStatus, Title: f.Title, Repo: f.Repo, State: f.State,
			Name: f.Name, Size: f.Size, Time: f.Time, Peer: f.Peer, Message: f.Message, Project: f.Project, Server: f.Server})
	}
	return out, res.Note, nil
}

// filesErrText is a refusal in words, and whether it means the session
// has no folder at all.
func filesErrText(err error) (string, bool) {
	var perr *proto.Error
	if errors.As(err, &perr) {
		return perr.Message, perr.Code == proto.ErrNoFiles
	}
	return err.Error(), false
}

// filesErrCode is a refusal's code, "" for an error that isn't one.
func filesErrCode(err error) string {
	var perr *proto.Error
	if errors.As(err, &perr) {
		return perr.Code
	}
	return ""
}

// filesFolders are the folders the view lists, the top first.
func (fv *filesView) filesFolders() []string {
	out := []string{""}
	if fv.searching() {
		return out
	}
	var walk func(dir string)
	walk = func(dir string) {
		for _, e := range fv.dirs[dir] {
			if e.Kind == proto.FileKindDir && fv.open[e.Path] {
				if len(out) < proto.MaxFilesStamped {
					out = append(out, e.Path)
				}
				walk(e.Path)
			}
		}
	}
	walk("")
	// One just unfolded isn't listed yet, but is asked for.
	for d, on := range fv.open {
		if on && !slices.Contains(out, d) && len(out) < proto.MaxFilesStamped {
			out = append(out, d)
		}
	}
	return out
}

// filesFetch lists what the view shows, off the keyboard's goroutine. An
// answer outrun by a newer fetch is dropped.
func (c *client) filesFetch() {
	if !c.lock() {
		return
	}
	fv := c.info.files
	if fv == nil {
		c.mu.Unlock()
		return
	}
	fv.gen++
	snap, folders, paths := *fv, fv.filesFolders(), c.paths
	c.mu.Unlock()
	filesGo(func() {
		got := fetchFiles(paths, snap, folders)
		if !c.lock() {
			return
		}
		if c.info.files == fv && fv.gen == snap.gen {
			fv.take(got)
			c.infoLayout()
		}
		c.mu.Unlock()
		c.poke()
	})
}

// take shows what a fetch brought. With no folder at all, the view opens
// on Attached, and Worktree says why; a repo the user chose that can't
// open only says why. c.mu held.
func (fv *filesView) take(got filesFetched) {
	first := !fv.loaded
	fv.loaded = true
	fv.lib, fv.libNote = got.lib, got.note
	if fv.program != "" {
		fv.err = got.err
		return
	}
	if fv.far != "" {
		switch got.code {
		case "attached":
			return
		case proto.ErrPeerOffline:
			fv.offline, fv.slow, fv.err = true, false, ""
			if fv.readAt.IsZero() {
				fv.err = fv.far + " is offline, so its files can't be read now."
			}
			return
		case proto.ErrRateLimited:
			fv.slow, fv.offline = true, false
			return
		case proto.ErrFilesOff, proto.ErrNotShared:
			fv.stopSharing()
			return
		case proto.ErrUnknownSession:
			got.err = fv.farStopped()
		}
		fv.offline, fv.slow, fv.gone = false, false, ""
		if got.err == "" {
			fv.readAt = infoClock()
		}
	}
	if got.repos != nil {
		fv.repos = got.repos
	}
	if got.offList {
		fv.repo = ""
	}
	if got.stamp != "" {
		fv.stamp = got.stamp
	}
	fv.err = got.err
	if got.err != "" {
		fv.dirs, fv.found, fv.tree = map[string][]proto.FileEntry{}, nil, nil
		if got.noFiles && first && fv.repo == "" {
			fv.attached = true // a then shows why the worktree doesn't
		}
		return
	}
	fv.sum, fv.cut = got.sum, got.cut
	if got.dirs != nil {
		fv.dirs = got.dirs
		for d := range fv.open {
			if _, ok := got.dirs[d]; !ok {
				delete(fv.open, d)
			}
		}
	}
	fv.found, fv.tree = got.found, changedTree(got.found)
}

// filesPoll is the panel's poll: while the view shows, every filesEvery,
// has its folders' stamp moved? Off c.mu.
func (c *client) filesPoll() {
	if !c.lock() {
		return
	}
	fv := c.info.files
	if fv == nil || time.Since(fv.polled) < fv.every() {
		c.mu.Unlock()
		return
	}
	fv.polled = time.Now()
	session, repo, folders, paths, attached, had := fv.session, fv.repo, fv.filesFolders(), c.paths, fv.attached, fv.stamp
	coordinator, repos, far, gone := fv.thread == "", fv.repos, fv.far != "", fv.gone != ""
	c.mu.Unlock()
	if attached {
		c.filesFetch() // the Library's, a few stats
		return
	}
	if coordinator && !gone {
		// The project's list moves without the folder's stamp moving.
		var rs proto.FilesReposResult
		if filesCall(paths, proto.MethodFilesRepos, proto.FilesReposParams{Session: session}, &rs) == nil && !slices.Equal(rs.Repos, repos) {
			c.filesFetch()
			return
		}
	}
	var st proto.FilesStampResult
	if filesCall(paths, proto.MethodFilesStamp, proto.FilesStampParams{Session: session, Repo: repo, Folders: folders}, &st) != nil {
		// A far server's refusal says what the tree must do: the listing
		// finds out which. While the switch is off, nothing to ask.
		if far && !gone {
			c.filesFetch()
		}
		return
	}
	if st.Stamp != had || gone {
		c.filesFetch()
	}
}

// filesFollow keeps the Files view on the session the panel shows: it
// closes on another's, and the one left for the viewer comes back on its
// own, with the keyboard. It says whether the view changed. c.mu held.
func (c *client) filesFollow(d *infoData) bool {
	id := ""
	if d != nil {
		id = d.session.ID
	}
	if d != nil {
		if kept := filesKept.Swap(nil); kept != nil && kept.session == id {
			c.info.files, c.kb = kept, areaInfo
			return true
		}
	}
	if fv := c.info.files; fv != nil && fv.session != id {
		c.info.files = nil
		return true
	}
	// Unpaired, the link closes and takes what showed with it.
	if fv := c.info.files; fv != nil && fv.far != "" && d.remote != nil && d.remote.peer == nil && fv.gone == "" {
		fv.stopSharing()
		return true
	}
	return false
}

// filesToggle shows the panel's other face (F, a click on its worktree
// line). What has no folder here says why. Off c.mu.
func (c *client) filesToggle() {
	if !c.lock() {
		return
	}
	ip := c.info
	if ip.files != nil {
		ip.files = nil
		c.infoLayout()
		c.status()
		c.mu.Unlock()
		c.poke()
		return
	}
	d := ip.data
	switch {
	case d == nil:
		c.flash = "no files here"
	case d.remote != nil && filesFarRefused(d.remote) != "":
		c.flash = filesFarRefused(d.remote)
	default:
		ip.files = newFilesView(d)
		ip.top = 0
		c.infoLayout()
		c.status()
		c.mu.Unlock()
		c.poke()
		c.filesFetch()
		return
	}
	c.status()
	c.mu.Unlock()
	c.poke()
}

// filesFarRefused says why paired server r's session opens no Files view, ""
// when it opens: older tm, a project it doesn't share, or its files switch off.
func filesFarRefused(r *remoteData) string {
	name := oneLine(r.shown)
	switch {
	case r.peer == nil:
		return name + " isn't paired with this server"
	case !r.peer.Worktree && r.peer.State == proto.LinkUp:
		return name + " runs an older tm, which can't share files"
	case r.project == nil:
		return name + " doesn't share this project"
	case !r.project.Files:
		return name + " doesn't share " + oneLine(r.project.Project) + "'s files"
	}
	return ""
}

// filesHint is the status bar's note while the view has the keyboard.
func filesHint(fv *filesView) string {
	switch {
	case fv.typing:
		return "filter ▸ type a name · enter keep · esc clear"
	case fv.picking:
		return "repo ▸ ↑ ↓ · enter show · esc back"
	case fv.program != "":
		return "files ▸ ↑ ↓ · enter read · / filter · t kinds · F info · esc back"
	case fv.attached:
		return "files ▸ ↑ ↓ · enter read · / filter · a worktree · F info · esc back"
	case fv.far != "" && len(fv.repos) > 1:
		return "files ▸ ↑ ↓ ← → · enter read · r repo · / filter · c changed · a attached · F info · esc back"
	case fv.far != "":
		return "files ▸ ↑ ↓ ← → · enter read · / filter · c changed · a attached · F info · esc back"
	case len(fv.repos) > 1:
		return "files ▸ ↑ ↓ ← → · enter read · r repo · / filter · c changed · i ignored · a attached · F info · esc back"
	}
	return "files ▸ ↑ ↓ ← → · enter read · / filter · c changed · i ignored · a attached · F info · esc back"
}

// filesKeyboard runs a key while the view has the keyboard. c.mu held;
// released here.
func (c *client) filesKeyboard(k uv.Key) {
	c.flash = ""
	ip := c.info
	fv := ip.files
	rows := fv.rows()
	name := keyName(k)
	refetch, toggle, filter := false, false, fv.filter
	var read *fileRow
	move := func(to int) {
		fv.sel = min(max(to, 0), max(len(rows)-1, 0))
		fv.note(rows)
		c.filesReveal()
	}
	op, isOp := panelOp(name)
	switch {
	case fv.typing:
		switch name {
		case "enter":
			fv.typing = false
		case "esc":
			fv.typing, fv.filter, refetch = false, "", !fv.attached
		case "backspace":
			if r := []rune(fv.filter); len(r) > 0 {
				fv.filter, refetch = string(r[:len(r)-1]), !fv.attached
			}
		default:
			if t := textsafe.Clean(k.Text, false); t != "" && len(fv.filter)+len(t) <= proto.MaxFilesQuery {
				fv.filter, refetch = fv.filter+t, !fv.attached
			}
		}
	case name == "tab":
		c.kb = areaMain
	case name == "F":
		toggle = true
	case fv.picking:
		switch {
		case name == "r" || (isOp && op == panelBack):
			fv.picking = false
		case isOp && op == panelUp:
			fv.pickSel = max(fv.pickSel-1, 0)
		case isOp && op == panelDown:
			fv.pickSel = min(fv.pickSel+1, len(fv.repos)-1)
		case isOp && op == panelEnter:
			refetch = fv.pickRepo(fv.pickSel)
		}
	case name == "r" && !fv.attached && len(fv.repos) > 1:
		fv.picking, fv.pickSel, ip.top = true, fv.repoShown(), 0
	case name == "/":
		fv.typing = true
	case name == "a" && fv.program != "":
		c.flash = "a director has no worktree: its projects' threads have those"
	case name == "t" && fv.program != "":
		if chips := libChips(fv.lib, fv.filter, fv.pick); chips != nil {
			fv.narrow(libNextKind(chips, libPicked(fv.lib, fv.pick)))
		}
	case name == "a":
		fv.attached, fv.sel = !fv.attached, 0
		refetch = true
	case name == "i" && !fv.attached && fv.far == "":
		fv.ignored, refetch = !fv.ignored, true
	case name == "c" && !fv.attached:
		fv.changed, fv.shut, fv.sel, refetch = !fv.changed, nil, 0, true
	case !isOp:
	case op == panelUp:
		move(fv.sel - 1)
	case op == panelDown:
		move(fv.sel + 1)
	case op == panelPageUp:
		move(fv.sel - c.rows/2)
	case op == panelPageDown:
		move(fv.sel + c.rows/2)
	case op == panelFirst:
		move(0)
	case op == panelLast:
		move(len(rows) - 1)
	case op == panelBack && fv.program != "" && (fv.filter != "" || fv.pick != ""):
		fv.filter, fv.pick = "", ""
		fv.sel = 0
	case op == panelBack && fv.filter != "":
		fv.filter, refetch = "", !fv.attached
	case op == panelBack:
		c.kb = areaMain
	case fv.sel >= len(rows):
	case op == panelUnfold:
		if e := rows[fv.sel].e; e.Kind == proto.FileKindDir && !e.Withheld && !fv.flat() && rows[fv.sel].lib == nil {
			if fv.unfolded(e.Path) {
				move(fv.sel + 1)
			} else {
				refetch = fv.fold(e.Path)
			}
		}
	case op == panelFold:
		if rows[fv.sel].lib != nil || fv.flat() {
			break
		}
		e := rows[fv.sel].e
		if e.Kind == proto.FileKindDir && !e.Withheld && fv.unfolded(e.Path) {
			fv.fold(e.Path)
			break
		}
		for i, r := range rows {
			if r.e.Path == path.Dir(e.Path) {
				move(i)
				break
			}
		}
	case op == panelEnter:
		r := rows[fv.sel]
		switch {
		case r.lib == nil && r.e.Withheld:
			c.flash = withheldWords(r.e, fv.far)
		case r.lib == nil && r.e.Kind == proto.FileKindDir && !fv.flat():
			refetch = fv.fold(r.e.Path)
		case r.lib == nil && r.e.Kind == proto.FileKindDir:
			c.flash = "clear the filter to open a folder"
		default:
			read = &r
		}
	}
	if fv.filter != filter {
		fv.shut = nil // what it finds shows, not inside a folded folder
	}
	c.infoLayout()
	c.filesReveal()
	c.status()
	c.mu.Unlock()
	c.poke()
	if toggle {
		c.filesToggle()
	}
	if refetch {
		c.filesFetch()
	}
	if read != nil {
		filesGo(func() { c.filesRead(*read) })
	}
}

// narrow shows the kind: the selection stays on its file while that shows.
func (fv *filesView) narrow(kind string) {
	fv.pick, fv.sel = kind, 0
}

// pickRepo closes the picker on repos[i], and says whether that changed
// what the view shows.
func (fv *filesView) pickRepo(i int) bool {
	fv.picking = false
	if i < 0 || i >= len(fv.repos) || i == fv.repoShown() {
		return false
	}
	fv.choose(fv.repos[i].Path)
	return true
}

// filesReveal scrolls the panel so the selected row shows. c.mu held.
func (c *client) filesReveal() {
	ip, fv := c.info, c.info.files
	if fv == nil || fv.sel >= len(fv.lineOf) {
		return
	}
	l, rows := fv.lineOf[fv.sel], c.infoRows()
	switch {
	case l < ip.top+1 && l <= rows-1:
		ip.top = 0 // the head shows with the first rows
	case l < ip.top:
		ip.top = l
	case l >= ip.top+rows:
		ip.top = l - rows + 1
	}
	ip.top = min(max(ip.top, 0), max(len(ip.lines)-rows, 0))
}

// filesClick is a click on the view's line with hit h: a tab switches, a
// row selects, a folder then folds or unfolds, and a selected file
// reads. c.mu held; released here.
func (c *client) filesClick(h infoHit, col int) {
	fv := c.info.files
	refetch := false
	var read *fileRow
	switch h.kind {
	case hitFilesChip:
		if ls := libChipLinesOf(fv.lib, fv.filter, fv.pick, max(c.infoW-1, 1)); h.task < len(ls) {
			for _, s := range ls[h.task].spans {
				if col >= s.from && col < s.to {
					fv.narrow(s.kind)
				}
			}
		}
	case hitFilesTab:
		fv.attached, fv.sel, fv.picking, refetch = !fv.attached, 0, false, true
	case hitFilesRepo:
		fv.picking, fv.pickSel = !fv.picking, fv.repoShown()
	case hitFilesRepoRow:
		refetch = fv.pickRepo(h.task)
	case hitFileRow:
		rows := fv.rows()
		if h.task < len(rows) {
			r := rows[h.task]
			again := fv.sel == h.task
			fv.sel = h.task
			fv.note(rows)
			switch {
			case r.lib == nil && r.e.Withheld:
				c.flash = withheldWords(r.e, fv.far)
			case r.lib == nil && r.e.Kind == proto.FileKindDir && !fv.flat():
				refetch = fv.fold(r.e.Path)
			case again:
				read = &r
			}
		}
	}
	c.infoLayout()
	c.status()
	c.mu.Unlock()
	c.poke()
	if refetch {
		c.filesFetch()
	}
	if read != nil {
		filesGo(func() { c.filesRead(*read) })
	}
}

// filesRead reads row r's file in the Library's viewer, over the session:
// the first libMax bytes, a chunk at a time. What can't open says why.
// Off c.mu.
func (c *client) filesRead(r fileRow) {
	if !c.lock() {
		return
	}
	fv := c.info.files
	if fv == nil {
		c.mu.Unlock()
		return
	}
	flash := ""
	switch {
	case !c.dashboard:
		flash = "tm reads files on the dashboard"
	case r.lib != nil && fv.far != "" && fv.offline:
		flash = fv.far + " is offline, so that can't be read now"
	case r.lib != nil:
	case r.e.Withheld:
		flash = withheldWords(r.e, fv.far)
	case fv.far != "" && fv.offline:
		flash = fv.far + " is offline, so that can't be read now"
	case r.e.LinkOut:
		flash = oneLine(r.e.Name) + " is a link that doesn't open: it leads out of the folder, or nowhere"
	case r.e.Kind != proto.FileKindFile:
		flash = oneLine(r.e.Name) + " is no file to read"
	}
	if flash != "" {
		c.flash = flash
		c.status()
		c.mu.Unlock()
		c.poke()
		return
	}
	session, repo, slug, ignored, paths, program, far, alias := fv.session, fv.repo, fv.slug, fv.ignored, c.paths, fv.program, fv.far, fv.farAlias
	gone := fv.farGone()
	c.mu.Unlock()
	var tv *textView
	switch {
	case r.lib != nil && program != "":
		tv = programFileViewer(paths, program, *r.lib)
	case r.lib != nil && far != "":
		tv = attachedFarViewer(paths, alias, slug, *r.lib)
	case r.lib != nil:
		tv = attachedViewer(paths, slug, *r.lib)
	case far != "":
		tv = farViewer(paths, session, repo, r.e, gone)
	default:
		tv = worktreeViewer(paths, session, repo, r.e, ignored)
	}
	if !c.lock() {
		return
	}
	if c.info.files != fv {
		c.mu.Unlock()
		return
	}
	if far != "" && r.lib == nil && tv.note == noTextNote {
		if fv.notText == nil {
			fv.notText = map[string]bool{}
		}
		fv.notText[r.e.Path] = true
		c.infoLayout()
	}
	filesKept.Store(fv)
	c.goOver(&Over{Viewer: tv})
}

// noTextNote is what a viewer on a file without text says.
const noTextNote = "not text: only its name and size show"

// farViewer is the viewer on a file of a paired server's session: its text
// with its changes marked, and nothing else. Its bytes never cross if it
// isn't text, so there is no picture and nothing to open elsewhere. It looks
// while it shows whether the switch went off (textView.keep), and then what
// shows goes, in the words gone.
func farViewer(paths tmclient.Paths, session, repo string, e proto.FileEntry, gone string) *textView {
	tv := worktreeViewer(paths, session, repo, e, false)
	tv.image, tv.open = nil, nil
	tv.keep = func() string {
		var st proto.FilesStampResult
		err := filesCall(paths, proto.MethodFilesStamp, proto.FilesStampParams{Session: session, Repo: repo}, &st)
		switch filesErrCode(err) {
		case proto.ErrFilesOff, proto.ErrNotShared:
			return gone
		}
		return ""
	}
	return tv
}

// attachedFarViewer is the viewer on a file a paired server's project pages
// show: text, as far as there is any, and nothing to open elsewhere.
func attachedFarViewer(paths tmclient.Paths, alias, slug string, f thread.LibFile) *textView {
	tv := &textView{title: oneLine(f.Name) + " · " + sizeWords(f.Size)}
	var data []byte
	for {
		var res proto.LibraryReadResult
		err := filesCall(paths, proto.MethodPeerLibraryRead, proto.PeerLibraryReadReadParams{Alias: alias, Project: slug, Thread: f.Thread, Name: f.Name, Offset: int64(len(data))}, &res)
		if err != nil {
			msg, _ := filesErrText(err)
			tv.note = "can't read it: " + oneLine(msg)
			return tv
		}
		data = append(data, res.Data...)
		if len(res.Data) == 0 || int64(len(data)) >= min(res.Size, libMax) {
			tv.body, tv.note = libText(f.Name, data[:min(len(data), libMax)], res.Size > libMax)
			tv.body = textsafe.Text(tv.body)
			return tv
		}
	}
}

// attachedViewer is the viewer on a Library file.
func attachedViewer(paths tmclient.Paths, slug string, f thread.LibFile) *textView {
	tv := &textView{title: oneLine(f.Name) + " · " + sizeWords(f.Size)}
	id := libFileID(f)
	if f.Peer != "" {
		tv.title += " · from " + oneLine(f.Peer)
	}
	tv.open = func(m *dash) tea.Cmd { return m.openLibCopy(slug, id, f.Name) }
	if previewable() && thumb.Supported(f.Name) {
		var res proto.LibraryThumbnailResult
		if filesCall(paths, proto.MethodLibraryThumbnail, proto.LibraryThumbnailParams{Project: slug, ID: id, Name: f.Name}, &res) == nil {
			if tv.image = newImagePreview(res.Data); tv.image != nil {
				return tv
			}
		}
	}
	data, truncated, err := filesLibRead(slug, id, f.Name)
	if err != nil {
		tv.note = "can't read it: " + oneLine(err.Error())
		return tv
	}
	tv.body, tv.note = libText(f.Name, data, truncated)
	tv.body = textsafe.Text(tv.body)
	return tv
}

// programFileViewer is the viewer on a file of a program's list: the
// director's own, or a member's here or on its paired server, each read as
// a console off this machine's files reads it, so a path never crosses.
func programFileViewer(paths tmclient.Paths, program string, f thread.LibFile) *textView {
	tv := &textView{title: oneLine(f.Name) + " · " + sizeWords(f.Size)}
	if f.Project != "" {
		tv.title += " · " + libPlaceWords(f)
	}
	tv.open = func(m *dash) tea.Cmd {
		return m.openCopy(f.Name, func() ([]byte, bool, error) { return programFileBytes(paths, program, f, openMax) })
	}
	if previewable() && thumb.Supported(f.Name) {
		var res proto.LibraryThumbnailResult
		if programFileThumb(paths, program, f, &res) == nil {
			if tv.image = newImagePreview(res.Data); tv.image != nil {
				return tv
			}
		}
	}
	data, truncated, err := programFileBytes(paths, program, f, libMax)
	if err != nil {
		tv.note = "can't read it: " + oneLine(err.Error())
		return tv
	}
	tv.body, tv.note = libText(f.Name, data, truncated)
	tv.body = textsafe.Text(tv.body)
	return tv
}

// programFileThumb asks for the thumbnail of a file of a program's list.
func programFileThumb(paths tmclient.Paths, program string, f thread.LibFile, res *proto.LibraryThumbnailResult) error {
	switch {
	case f.Server != "":
		return filesCall(paths, proto.MethodPeerLibraryThumbnail, proto.PeerLibraryThumbnailReadParams{Alias: f.Server, Project: f.Project, Thread: f.Thread, Name: f.Name}, res)
	case f.Project != "":
		return filesCall(paths, proto.MethodLibraryThumbnail, proto.LibraryThumbnailParams{Project: f.Project, ID: libFileID(f), Name: f.Name}, res)
	}
	return filesCall(paths, proto.MethodLibraryThumbnail, proto.LibraryThumbnailParams{Program: program, ID: libFileID(f), Name: f.Name}, res)
}

// programFileBytes reads up to most bytes of a file of a program's list, a
// chunk at a time; truncated says it was longer.
func programFileBytes(paths tmclient.Paths, program string, f thread.LibFile, most int64) (data []byte, truncated bool, err error) {
	for {
		var res proto.LibraryReadResult
		off := int64(len(data))
		switch {
		case f.Server != "":
			err = filesCall(paths, proto.MethodPeerLibraryRead, proto.PeerLibraryReadReadParams{Alias: f.Server, Project: f.Project, Thread: f.Thread, Name: f.Name, Offset: off}, &res)
		case f.Project != "":
			err = filesCall(paths, proto.MethodLibraryRead, proto.LibraryReadParams{Project: f.Project, ID: libFileID(f), Name: f.Name, Offset: off}, &res)
		default:
			err = filesCall(paths, proto.MethodLibraryRead, proto.LibraryReadParams{Program: program, ID: libFileID(f), Name: f.Name, Offset: off}, &res)
		}
		if err != nil {
			return nil, false, err
		}
		data = append(data, res.Data...)
		switch {
		case int64(len(data)) >= res.Size:
			return data, false, nil
		case int64(len(data)) >= most:
			return data[:most], true, nil
		case len(res.Data) == 0:
			return nil, false, errors.New("it changed while it was read")
		}
	}
}

// worktreeViewer is the viewer on a file of the session's folder, its
// changes marked; a deleted file shows what it held.
func worktreeViewer(paths tmclient.Paths, session, repo string, e proto.FileEntry, ignored bool) *textView {
	tv := &textView{title: oneLine(e.Path)}
	if e.Status == proto.FileDeleted {
		deletedViewer(tv, paths, session, repo, e, ignored)
		return tv
	}
	var data []byte
	truncated := false
	for {
		var res proto.FilesReadResult
		err := filesCall(paths, proto.MethodFilesRead, proto.FilesReadParams{Session: session, Repo: repo, Path: e.Path, Offset: int64(len(data)), Ignored: ignored}, &res)
		if err != nil {
			msg, _ := filesErrText(err)
			tv.note = "can't read it: " + oneLine(msg)
			return tv
		}
		tv.title = oneLine(e.Path) + " · " + sizeWords(res.Size)
		if res.Binary {
			tv.note = noTextNote
			if d := fileDiff(paths, session, repo, e.Path, ignored); d.Status != "" {
				tv.title += " · " + changeWords(d)
			}
			if thumb.Supported(e.Name) && !strings.Contains(session, "/") { // a paired server's id has one: no bytes cross
				tv.open = func(m *dash) tea.Cmd {
					return m.openCopy(e.Name, func() ([]byte, bool, error) { return worktreeBytes(paths, session, repo, e.Path, ignored) })
				}
				if tv.image = worktreeImage(paths, session, repo, e, ignored, res.Size); tv.image != nil {
					tv.note = ""
					if c := fileCaution(e); c != "" {
						tv.title += " · " + c
					}
				}
			}
			return tv
		}
		data = append(data, res.Data...)
		if int64(len(data)) >= libMax {
			data, truncated = data[:libMax], res.Size > libMax
			break
		}
		if len(res.Data) == 0 || int64(len(data)) >= res.Size {
			break
		}
	}
	d := fileDiff(paths, session, repo, e.Path, ignored)
	name := e.Name
	if len(d.Hunks) > 0 {
		name = "" // as it is: its lines are what the hunks count
	}
	tv.body, tv.note = libText(name, data, truncated)
	tv.body = textsafe.Text(tv.body)
	tv.mark(d)
	if c := fileCaution(e); c != "" && tv.body != "" {
		tv.title += " · " + c
	}
	return tv
}

// worktreeBytes reads a file of the session's folder whatever it holds,
// the first proto.MaxFilesRead bytes; truncated says it was longer.
func worktreeBytes(paths tmclient.Paths, session, repo, path string, ignored bool) ([]byte, bool, error) {
	var data []byte
	for {
		var res proto.FilesReadResult
		err := filesCall(paths, proto.MethodFilesRead, proto.FilesReadParams{Session: session, Repo: repo, Path: path, Offset: int64(len(data)), Ignored: ignored, Binary: true}, &res)
		if err != nil {
			return nil, false, err
		}
		if res.Binary && len(res.Data) == 0 && len(data) == 0 && res.Size > 0 {
			return nil, false, errors.New("this server's tm doesn't send a file that isn't text")
		}
		data = append(data, res.Data...)
		if len(res.Data) == 0 || int64(len(data)) >= min(res.Size, proto.MaxFilesRead) {
			return data, res.Truncated, nil
		}
	}
}

// worktreeImage is the preview of an image in the session's folder, nil
// for none: a worktree's files have no thumbnails on the server, so this
// console makes one, under thumb's limits, when the viewer opens.
func worktreeImage(paths tmclient.Paths, session, repo string, e proto.FileEntry, ignored bool, size int64) *imagePreview {
	if !previewable() || size > thumb.MaxSource {
		return nil
	}
	data, truncated, err := worktreeBytes(paths, session, repo, e.Path, ignored)
	if err != nil || truncated {
		return nil
	}
	t, err := thumb.Make(data)
	if err != nil {
		return nil
	}
	return newImagePreview(t.Data)
}

// fileCaution is what the viewer's title says of a file that may hold a
// secret: said, not asked, since it opens only for the human who picked it,
// on this machine, and ignored files were asked for first. The title
// stays in view while the text scrolls, which matters on a shared screen.
func fileCaution(e proto.FileEntry) string {
	switch {
	case e.Ignored:
		return "git-ignored: may hold secrets"
	case filetree.SecretName(e.Name):
		return "its name says it may hold secrets"
	}
	return ""
}
