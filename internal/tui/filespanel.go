package tui

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	tmclient "github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// The Files view of the info panel (docs/SPEC.md §4): the panel's other
// face, F from its Info face, on a thread or a coordinator. Worktree is
// the session's folder as a tree (files.list, read only), with git's
// marks, a filter by name, and it refreshes while it shows (files.stamp);
// Attached is the Library's files of that thread, of the whole project
// beside a coordinator. Enter reads a file in the Library's viewer,
// drawn over the session. Everything a folder holds is data: names go
// through oneLine and text through libText and textsafe, so no control
// character reaches the terminal, and the panel's draw takes styles only
// (plainUnder).

// filesEvery is how often the view asks whether its folders changed.
const filesEvery = 2 * time.Second

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

	attached bool   // the Attached tab
	filter   string // by name
	typing   bool   // the keyboard is in the filter
	ignored  bool   // git-ignored files show
	changed  bool   // only what git marks

	open   map[string]bool              // the unfolded folders
	dirs   map[string][]proto.FileEntry // the folders as last listed
	found  []proto.FileEntry            // a filter's or changed's files
	sum    proto.FilesListResult        // the top's totals
	cut    bool                         // a listing was cut
	err    string                       // why the worktree doesn't show
	loaded bool
	lib    []thread.LibFile

	sel     int    // the selected row
	selPath string // its path, to find it again after a refresh
	lineOf  []int  // each row's line, as last laid out

	stamp  string
	polled time.Time
	gen    int // the newest fetch; an older one's answer is dropped
}

func newFilesView(d *infoData) *filesView {
	fv := &filesView{session: d.session.ID, slug: d.slug, open: map[string]bool{}, dirs: map[string][]proto.FileEntry{}}
	if d.watch == nil {
		fv.thread = d.session.Thread
	}
	return fv
}

// searching says the worktree shows a flat list.
func (fv *filesView) searching() bool { return fv.filter != "" || fv.changed }

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
	if fv.attached {
		for _, f := range libraryOrder(fv.lib) {
			if fv.filter == "" || strings.Contains(strings.ToLower(f.Name), strings.ToLower(fv.filter)) {
				out = append(out, fileRow{lib: &f})
			}
		}
		return out
	}
	if fv.searching() {
		for _, e := range fv.found {
			out = append(out, fileRow{e: e})
		}
		return out
	}
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		for _, e := range fv.dirs[dir] {
			out = append(out, fileRow{e: e, depth: depth})
			if e.Kind == proto.FileKindDir && fv.open[e.Path] {
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
	add := func(l string) {
		lines = append(lines, l)
		hits = append(hits, infoHit{})
	}
	rows := fv.rows()
	fv.settle(rows)
	switch {
	case fv.attached && len(rows) > 0:
		add(th().Faint.Render(fileCount(len(rows))))
	case fv.attached:
	case fv.err != "":
		add(th().Warn.Render(oneLine(fv.err)))
	case !fv.loaded:
		add(th().Faint.Render("loading…"))
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
	fv.lineOf = make([]int, len(rows))
	group := ""
	groups := map[string]int{}
	if fv.attached {
		for _, r := range rows {
			groups[r.lib.Group()]++
		}
	}
	for i, r := range rows {
		if r.lib != nil && r.lib.Group() != group {
			group = r.lib.Group()
			add(libGroup(*r.lib, groups[group], w))
		}
		fv.lineOf[i] = len(lines)
		l := fileRowLine(r, w, fv.open[r.e.Path])
		if i == fv.sel && focused {
			l = th().Sel.Render(fit(ansi.Strip(l), w))
		}
		lines = append(lines, l)
		hits = append(hits, infoHit{kind: hitFileRow, task: i})
	}
	switch {
	case len(rows) == 0 && fv.attached:
		add(th().Faint.Render("no files: a thread's report attaches them"))
	case len(rows) == 0 && fv.loaded && fv.err == "":
		add(th().Faint.Render("nothing here"))
	}
	if fv.cut && !fv.attached {
		add(th().Faint.Render("… more files than show here"))
	}
	return lines, hits
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

// fileRowLine is a row: its fold mark and name, its mark at the end.
func fileRowLine(r fileRow, w int, open bool) string {
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
	}
	width := max(w-ansi.StringWidth(lead)-2-ansi.StringWidth(mark)-1, 4)
	text := fit(name, width)
	if e.Ignored || e.Status == proto.FileDeleted || e.LinkOut {
		text = th().Faint.Render(text)
	}
	fold := "  "
	switch {
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
}

// fetchFiles asks the server for what the view shows. The stamp comes
// first: a change during the listing moves the next one.
func fetchFiles(paths tmclient.Paths, fv filesView, folders []string) filesFetched {
	var out filesFetched
	out.lib, _ = attachedFiles(fv.slug, fv.thread)
	var st proto.FilesStampResult
	if filesCall(paths, proto.MethodFilesStamp, proto.FilesStampParams{Session: fv.session, Folders: folders}, &st) == nil {
		out.stamp = st.Stamp
	}
	list := func(p proto.FilesListParams) (proto.FilesListResult, error) {
		var res proto.FilesListResult
		p.Session, p.Ignored = fv.session, fv.ignored
		err := filesCall(paths, proto.MethodFilesList, p, &res)
		return res, err
	}
	if fv.searching() {
		res, err := list(proto.FilesListParams{Query: fv.filter, Changed: fv.changed})
		if err != nil {
			out.err, out.noFiles = filesErrText(err)
			return out
		}
		out.sum, out.found, out.cut = res, res.Entries, res.Truncated
		return out
	}
	out.dirs = map[string][]proto.FileEntry{}
	for i, d := range folders {
		res, err := list(proto.FilesListParams{Path: d})
		if err != nil {
			if i == 0 {
				out.err, out.noFiles = filesErrText(err)
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

// filesErrText is a refusal in words, and whether it means the session
// has no folder at all.
func filesErrText(err error) (string, bool) {
	var perr *proto.Error
	if errors.As(err, &perr) {
		return perr.Message, perr.Code == proto.ErrNoFiles
	}
	return err.Error(), false
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
// on Attached, and Worktree says why. c.mu held.
func (fv *filesView) take(got filesFetched) {
	first := !fv.loaded
	fv.loaded = true
	fv.lib = got.lib
	if got.stamp != "" {
		fv.stamp = got.stamp
	}
	fv.err = got.err
	if got.err != "" {
		fv.dirs, fv.found = map[string][]proto.FileEntry{}, nil
		if got.noFiles && first {
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
	fv.found = got.found
}

// filesPoll is the panel's poll: while the view shows, every filesEvery,
// has its folders' stamp moved? Off c.mu.
func (c *client) filesPoll() {
	if !c.lock() {
		return
	}
	fv := c.info.files
	if fv == nil || time.Since(fv.polled) < filesEvery {
		c.mu.Unlock()
		return
	}
	fv.polled = time.Now()
	session, folders, paths, attached, had := fv.session, fv.filesFolders(), c.paths, fv.attached, fv.stamp
	c.mu.Unlock()
	if attached {
		c.filesFetch() // the Library's, a few stats
		return
	}
	var st proto.FilesStampResult
	if filesCall(paths, proto.MethodFilesStamp, proto.FilesStampParams{Session: session, Folders: folders}, &st) != nil {
		return
	}
	if st.Stamp != had {
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
	if fv := c.info.files; fv != nil && (fv.session != id || d.remote != nil || d.prog != nil) {
		c.info.files = nil
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
	case d.remote != nil:
		c.flash = oneLine(d.remote.shown) + "'s files stay on it"
	case d.prog != nil:
		c.flash = "a director has no repo: its projects' threads have the files"
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

// filesHint is the status bar's note while the view has the keyboard.
func filesHint(fv *filesView) string {
	switch {
	case fv.typing:
		return "filter ▸ type a name · enter keep · esc clear"
	case fv.attached:
		return "files ▸ ↑ ↓ · enter read · / filter · a worktree · F info · esc back"
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
	refetch, toggle := false, false
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
	case name == "/":
		fv.typing = true
	case name == "a":
		fv.attached, fv.sel = !fv.attached, 0
		refetch = true
	case name == "i" && !fv.attached:
		fv.ignored, refetch = !fv.ignored, true
	case name == "c" && !fv.attached:
		fv.changed, fv.sel, refetch = !fv.changed, 0, true
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
	case op == panelBack && fv.filter != "":
		fv.filter, refetch = "", !fv.attached
	case op == panelBack:
		c.kb = areaMain
	case fv.sel >= len(rows):
	case op == panelUnfold:
		if e := rows[fv.sel].e; e.Kind == proto.FileKindDir && !fv.searching() && rows[fv.sel].lib == nil {
			if fv.open[e.Path] {
				move(fv.sel + 1)
			} else {
				fv.open[e.Path], refetch = true, true
			}
		}
	case op == panelFold:
		if rows[fv.sel].lib != nil || fv.searching() {
			break
		}
		e := rows[fv.sel].e
		if e.Kind == proto.FileKindDir && fv.open[e.Path] {
			delete(fv.open, e.Path)
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
		case r.lib == nil && r.e.Kind == proto.FileKindDir && !fv.searching():
			if fv.open[r.e.Path] {
				delete(fv.open, r.e.Path)
			} else {
				fv.open[r.e.Path], refetch = true, true
			}
		case r.lib == nil && r.e.Kind == proto.FileKindDir:
			c.flash = "clear the filter to open a folder"
		default:
			read = &r
		}
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
		c.filesRead(*read)
	}
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
func (c *client) filesClick(h infoHit) {
	fv := c.info.files
	refetch := false
	var read *fileRow
	switch h.kind {
	case hitFilesTab:
		fv.attached, fv.sel, refetch = !fv.attached, 0, true
	case hitFileRow:
		rows := fv.rows()
		if h.task < len(rows) {
			r := rows[h.task]
			again := fv.sel == h.task
			fv.sel = h.task
			fv.note(rows)
			switch {
			case r.lib == nil && r.e.Kind == proto.FileKindDir && !fv.searching():
				if fv.open[r.e.Path] {
					delete(fv.open, r.e.Path)
				} else {
					fv.open[r.e.Path], refetch = true, true
				}
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
		c.filesRead(*read)
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
	case r.lib != nil:
	case r.e.LinkOut:
		flash = oneLine(r.e.Name) + " is a link that doesn't open: it leads out of the folder, or nowhere"
	case r.e.Status == proto.FileDeleted:
		flash = oneLine(r.e.Name) + " is deleted"
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
	session, slug, ignored, paths := fv.session, fv.slug, fv.ignored, c.paths
	c.mu.Unlock()
	var tv *textView
	if r.lib != nil {
		tv = attachedViewer(slug, *r.lib)
	} else {
		tv = worktreeViewer(paths, session, r.e, ignored)
	}
	if !c.lock() {
		return
	}
	if c.info.files != fv {
		c.mu.Unlock()
		return
	}
	filesKept.Store(fv)
	c.goOver(&Over{Viewer: tv})
}

// attachedViewer is the viewer on a Library file.
func attachedViewer(slug string, f thread.LibFile) *textView {
	tv := &textView{title: oneLine(f.Name) + " · " + sizeWords(f.Size)}
	id := f.Thread
	if f.Peer != "" {
		id, tv.title = f.Message, tv.title+" · from "+oneLine(f.Peer)
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

// worktreeViewer is the viewer on a file of the session's folder.
func worktreeViewer(paths tmclient.Paths, session string, e proto.FileEntry, ignored bool) *textView {
	tv := &textView{title: oneLine(e.Path)}
	var data []byte
	truncated := false
	for {
		var res proto.FilesReadResult
		err := filesCall(paths, proto.MethodFilesRead, proto.FilesReadParams{Session: session, Path: e.Path, Offset: int64(len(data)), Ignored: ignored}, &res)
		if err != nil {
			msg, _ := filesErrText(err)
			tv.note = "can't read it: " + oneLine(msg)
			return tv
		}
		tv.title = oneLine(e.Path) + " · " + sizeWords(res.Size)
		if res.Binary {
			tv.note = "not text: only its name and size show"
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
	tv.body, tv.note = libText(e.Name, data, truncated)
	tv.body = textsafe.Text(tv.body)
	return tv
}
