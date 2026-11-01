package search

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/theclifmeister/terminatr/internal/project"
)

// MaxFile is the largest file (or archive member) the index reads;
// bigger ones are left out.
const MaxFile = 1 << 20

// MaxArchive is the largest archive the index reads.
const MaxArchive = 64 << 20

// How the index reads a file: what the file is.
const (
	fileNote     = "note"     // memory/<name>.md
	fileContext  = "context"  // CONTEXT.md
	fileProject  = "project"  // PROJECT.md
	fileTasks    = "tasks"    // TASKS.md, tasks/ARCHIVE.md
	fileDesign   = "design"   // designs/<name>.md
	fileBrief    = "brief"    // threads/<id>/brief.md
	fileTaskMD   = "taskmd"   // threads/<id>/task.md
	fileReport   = "report"   // threads/<id>/REPORT.md, reports/<n>.md
	fileLibrary  = "library"  // threads/<id>/library/<name>
	fileInbox    = "inbox"    // inbox/<name>.md, inbox/done/<name>.md
	fileJournal  = "journal"  // JOURNAL.md
	fileJournalZ = "journalz" // journal/<yyyy-mm>.md.gz
	fileThreadZ  = "threadz"  // threads/archive/<id>.tar.gz
	fileInboxZ   = "inboxz"   // inbox/done/<yyyy-mm>.tar.gz
)

// source is one file of the project the index reads, as the walk found
// it.
type source struct {
	Rel   string // relative to the project folder, slash-separated
	Seg   string // the segment it is in
	File  string // how it is read: file*
	Size  int64
	MTime int64 // UnixNano
}

// libraryExts are the library files the index reads, as text.
var libraryExts = []string{".md", ".txt", ".html", ".htm", ".json", ".csv"}

var (
	monthGzRE  = regexp.MustCompile(`^([0-9]{4}-[0-9]{2})\.md\.gz$`)
	monthTarRE = regexp.MustCompile(`^([0-9]{4}-[0-9]{2})\.tar\.gz$`)
	threadIDRE = regexp.MustCompile(`^t-([0-9]{4,})$`)
	reportRE   = regexp.MustCompile(`^([1-9][0-9]*)\.md$`)
)

// threadSeg is the segment of a thread's files: a hundred threads each.
func threadSeg(id string) string {
	m := threadIDRE.FindStringSubmatch(id)
	n, _ := strconv.Atoi(m[1])
	return fmt.Sprintf("threads-%02d", n/100)
}

// walkWorkers is how many folders walk lists at once.
const walkWorkers = 8

// walk lists the files of the project at dir the index reads, with
// their sizes and times. Only regular files count, never a link. The
// threads' folders and the inbox are listed by walkWorkers at once.
func walk(dir string) ([]source, error) {
	w := walker{dir: dir}
	w.add("CONTEXT.md", "notes", fileContext, w.lstat("CONTEXT.md"))
	w.add("PROJECT.md", "notes", fileProject, w.lstat("PROJECT.md"))
	w.each("memory", nil, func(name string, fi fs.FileInfo) {
		if strings.HasSuffix(name, ".md") {
			w.add("memory/"+name, "notes", fileNote, fi)
		}
	})
	w.add("TASKS.md", "tasks", fileTasks, w.lstat("TASKS.md"))
	w.add("tasks/ARCHIVE.md", "tasks", fileTasks, w.lstat("tasks/ARCHIVE.md"))
	w.each("designs", nil, func(name string, fi fs.FileInfo) {
		if strings.HasSuffix(name, ".md") {
			w.add("designs/"+name, "designs", fileDesign, fi)
		}
	})
	w.add("JOURNAL.md", "journal", fileJournal, w.lstat("JOURNAL.md"))
	w.each("journal", nil, func(name string, fi fs.FileInfo) {
		if m := monthGzRE.FindStringSubmatch(name); m != nil {
			w.add("journal/"+name, "journal-"+m[1], fileJournalZ, fi)
		}
	})
	w.each("threads/archive", nil, func(name string, fi fs.FileInfo) {
		if id, ok := strings.CutSuffix(name, ".tar.gz"); ok && threadIDRE.MatchString(id) && fi.Size() <= MaxArchive {
			w.add("threads/archive/"+name, threadSeg(id), fileThreadZ, fi)
		}
	})
	w.each("inbox", nil, func(name string, fi fs.FileInfo) {
		if strings.HasSuffix(name, ".md") {
			w.add("inbox/"+name, "inbox", fileInbox, fi)
		}
	})
	// The rest in parallel: inbox/done, then each thread's folder.
	jobs := []func(w *walker){func(w *walker) {
		w.each("inbox/done", nil, func(name string, fi fs.FileInfo) {
			switch m := monthTarRE.FindStringSubmatch(name); {
			case m != nil && fi.Size() <= MaxArchive:
				w.add("inbox/done/"+name, "inbox-"+m[1], fileInboxZ, fi)
			case strings.HasSuffix(name, ".md"):
				w.add("inbox/done/"+name, "inbox", fileInbox, fi)
			}
		})
	}}
	if ents, err := os.ReadDir(filepath.Join(dir, "threads")); err == nil {
		for _, e := range ents {
			if id := e.Name(); e.IsDir() && threadIDRE.MatchString(id) {
				jobs = append(jobs, func(w *walker) { w.thread(id) })
			}
		}
	}
	parts := make([]walker, len(jobs))
	var wg sync.WaitGroup
	next := make(chan int)
	for range min(walkWorkers, len(jobs)) {
		wg.Go(func() {
			for i := range next {
				parts[i].dir = dir
				jobs[i](&parts[i])
			}
		})
	}
	for i := range jobs {
		next <- i
	}
	close(next)
	wg.Wait()
	for _, p := range parts {
		w.out = append(w.out, p.out...)
	}
	return w.out, nil
}

// walker gathers the sources walk finds.
type walker struct {
	dir string
	out []source
}

func (w *walker) add(rel, seg, file string, fi fs.FileInfo) {
	if fi == nil || !fi.Mode().IsRegular() {
		return
	}
	w.out = append(w.out, source{Rel: rel, Seg: seg, File: file, Size: fi.Size(), MTime: fi.ModTime().UnixNano()})
}

func (w *walker) lstat(rel string) fs.FileInfo {
	fi, err := os.Lstat(filepath.Join(w.dir, filepath.FromSlash(rel)))
	if err != nil {
		return nil
	}
	return fi
}

// each lists the regular files of a folder that want takes (all with
// want nil), and answers the names of its folders.
func (w *walker) each(rel string, want func(name string) bool, fn func(name string, fi fs.FileInfo)) (dirs []string) {
	ents, err := os.ReadDir(filepath.Join(w.dir, filepath.FromSlash(rel)))
	if err != nil {
		return nil
	}
	for _, e := range ents {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
		if !e.Type().IsRegular() || strings.HasPrefix(e.Name(), ".") || want != nil && !want(e.Name()) {
			continue
		}
		if fi, err := e.Info(); err == nil {
			fn(e.Name(), fi)
		}
	}
	return dirs
}

// thread lists a thread's files: brief, task, reports, library.
func (w *walker) thread(id string) {
	seg, base := threadSeg(id), "threads/"+id+"/"
	dirs := w.each("threads/"+id, threadFileName, func(name string, fi fs.FileInfo) {
		switch name {
		case "brief.md":
			w.add(base+name, seg, fileBrief, fi)
		case "task.md":
			w.add(base+name, seg, fileTaskMD, fi)
		case "REPORT.md":
			w.add(base+name, seg, fileReport, fi)
		}
	})
	if slices.Contains(dirs, "reports") {
		w.each(base+"reports", nil, func(name string, fi fs.FileInfo) {
			if reportRE.MatchString(name) {
				w.add(base+"reports/"+name, seg, fileReport, fi)
			}
		})
	}
	if slices.Contains(dirs, "library") {
		w.each(base+"library", nil, func(name string, fi fs.FileInfo) {
			if libraryFile(name) && fi.Size() <= MaxFile {
				w.add(base+"library/"+name, "library", fileLibrary, fi)
			}
		})
	}
}

// threadFileName reports whether a thread folder's file is one the
// index reads.
func threadFileName(name string) bool {
	return name == "brief.md" || name == "task.md" || name == "REPORT.md"
}

func libraryFile(name string) bool {
	return slices.Contains(libraryExts, strings.ToLower(path.Ext(name)))
}

// readSource reads a source's bytes: a plain file whole, a .gz
// journal decompressed. A file over MaxFile (an archive over
// MaxArchive) is refused.
func readSource(dir string, s source) ([]byte, error) {
	full := filepath.Join(dir, filepath.FromSlash(s.Rel))
	f, err := os.Open(full)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	limit := int64(MaxFile)
	if s.File == fileJournalZ || s.File == fileThreadZ || s.File == fileInboxZ || s.File == fileJournal || s.File == fileTasks {
		limit = MaxArchive
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is over %d bytes", s.Rel, limit)
	}
	if s.File == fileJournalZ {
		return gunzip(data)
	}
	return data, nil
}

func gunzip(data []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	out, err := io.ReadAll(io.LimitReader(zr, MaxArchive+1))
	if err != nil {
		return nil, err
	}
	if len(out) > MaxArchive {
		return nil, fmt.Errorf("over %d bytes unpacked", MaxArchive)
	}
	return out, nil
}

// member is one file of a tar.gz archive.
type member struct {
	name  string
	data  []byte
	mtime time.Time
}

// untar reads a tar.gz archive's regular files of at most MaxFile
// bytes, MaxArchive in all.
func untar(data []byte) ([]member, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(zr)
	var out []member
	total := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		if h.Typeflag != tar.TypeReg || h.Size > MaxFile {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(tr, MaxFile))
		if err != nil {
			return out, err
		}
		if total += len(b); total > MaxArchive {
			return out, fmt.Errorf("over %d bytes unpacked", MaxArchive)
		}
		out = append(out, member{name: path.Clean(h.Name), data: b, mtime: h.ModTime})
	}
}

// textOf is the text of a file (or member) as the index reads it, the
// text a chunk's Off and Len place it in: an HTML library file's text
// without its tags, any other as it is.
func textOf(name string, data []byte) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".html", ".htm":
		return htmlText(string(data))
	}
	return string(data)
}

// threadMeta is what a thread's chunks carry from its record.
type threadMeta struct {
	Task  string `toml:"task"`
	Title string `toml:"title"`
}

func parseThreadMeta(data []byte) threadMeta {
	var m threadMeta
	_, _ = toml.Decode(string(data), &m)
	m.Title = strings.TrimSpace(m.Title)
	return m
}

// parse reads a source into its chunks, each with the text it is in;
// it answers the source's bytes too.
func parse(dir string, s source) ([]piece, []byte, error) {
	data, err := readSource(dir, s)
	if err != nil {
		return nil, nil, err
	}
	ps, err := parseData(dir, s, data)
	return ps, data, err
}

// withText sets the pieces' text.
func withText(ps []piece, text string) []piece {
	for i := range ps {
		ps[i].text = text
	}
	return ps
}

func parseData(dir string, s source, data []byte) ([]piece, error) {
	switch s.File {
	case fileThreadZ:
		ms, err := untar(data)
		if err != nil && len(ms) == 0 {
			return nil, err
		}
		var meta threadMeta
		var reports []int
		for _, m := range ms {
			_, rest, _ := strings.Cut(m.name, "/")
			if rest == "thread.toml" {
				meta = parseThreadMeta(m.data)
			}
			if r := reportRE.FindStringSubmatch(strings.TrimPrefix(rest, "reports/")); r != nil && strings.HasPrefix(rest, "reports/") {
				n, _ := strconv.Atoi(r[1])
				reports = append(reports, n)
			}
		}
		var out []piece
		for _, m := range ms {
			id, rest, _ := strings.Cut(m.name, "/")
			if !threadIDRE.MatchString(id) {
				continue
			}
			file := ""
			switch {
			case rest == "brief.md":
				file = fileBrief
			case rest == "task.md":
				file = fileTaskMD
			case rest == "REPORT.md" || strings.HasPrefix(rest, "reports/") && reportRE.MatchString(path.Base(rest)):
				file = fileReport
			case strings.HasPrefix(rest, "library/") && libraryFile(rest):
				file = fileLibrary
			default:
				continue
			}
			text := textOf(m.name, m.data)
			c := Chunk{Path: s.Rel, Member: m.name, Thread: id, Task: meta.Task, Time: m.mtime.UTC()}
			out = append(out, withText(threadFile(file, id, rest, text, meta, reports, c), text)...)
		}
		return out, nil
	case fileInboxZ:
		ms, err := untar(data)
		if err != nil && len(ms) == 0 {
			return nil, err
		}
		var out []piece
		for _, m := range ms {
			if !strings.HasSuffix(m.name, ".md") || strings.Contains(m.name, "/") {
				continue
			}
			c := Chunk{Path: s.Rel, Member: m.name, Time: m.mtime.UTC()}
			text := string(m.data)
			out = append(out, withText(inboxItem(m.name, text, c), text)...)
		}
		return out, nil
	}
	text := textOf(s.Rel, data)
	ps, err := parseText(dir, s, text)
	return withText(ps, text), err
}

// parseText cuts a plain file's text into chunks.
func parseText(dir string, s source, text string) ([]piece, error) {
	c := Chunk{Path: s.Rel, Time: time.Unix(0, s.MTime).UTC()}
	switch s.File {
	case fileNote:
		name := strings.TrimSuffix(path.Base(s.Rel), ".md")
		n := project.ParseNote(s.Rel, text)
		start := 0
		if n.Front && strings.HasSuffix(text, n.Body) {
			start = len(text) - len(n.Body)
		}
		c.Kind, c.Coordinator = KindNote, !n.ForThreads()
		return markdown(text, start, 3, "note:"+name, n.Title, c), nil
	case fileContext, fileProject:
		c.Kind = KindContext
		title := map[string]string{fileContext: "Context", fileProject: "Project"}[s.File]
		return markdown(text, frontEnd(text), 3, s.File, title, c), nil
	case fileTasks:
		return tasksFile(text, c), nil
	case fileDesign:
		c.Kind = KindDesign
		fe := frontEnd(text)
		c.Task = frontTask(text[:fe])
		title := firstHeading(text, fe)
		name := strings.TrimSuffix(path.Base(s.Rel), ".md")
		if title == "" {
			title = name
		}
		return markdown(text, fe, 3, "design:"+name, title, c), nil
	case fileBrief, fileTaskMD, fileReport, fileLibrary:
		parts := strings.SplitN(s.Rel, "/", 3) // threads, <id>, rest
		id, rest := parts[1], parts[2]
		meta := threadMeta{}
		if b, err := os.ReadFile(filepath.Join(dir, "threads", id, "thread.toml")); err == nil {
			meta = parseThreadMeta(b)
		}
		var reports []int
		if s.File == fileReport && rest == "REPORT.md" {
			ents, _ := os.ReadDir(filepath.Join(dir, "threads", id, "reports"))
			for _, e := range ents {
				if r := reportRE.FindStringSubmatch(e.Name()); r != nil {
					n, _ := strconv.Atoi(r[1])
					reports = append(reports, n)
				}
			}
		}
		c.Thread, c.Task = id, meta.Task
		return threadFile(s.File, id, rest, text, meta, reports, c), nil
	case fileInbox:
		return inboxItem(path.Base(s.Rel), text, c), nil
	case fileJournal, fileJournalZ:
		return journalLines(text, 0, nil, c), nil
	}
	return nil, fmt.Errorf("%s: unknown file kind %q", s.Rel, s.File)
}

// frontTask is the task a front matter's task property names.
func frontTask(front string) string {
	for _, l := range strings.Split(front, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(l), "task:"); ok {
			return taskRefRE.FindString(v)
		}
	}
	return ""
}

// taskHeadRE is a task's heading in TASKS.md: "T914 Title".
var taskHeadRE = regexp.MustCompile(`^(T[1-9][0-9]*)\b\s*(.*)$`)

// updatedRE is a task's updated date on its status line.
var updatedRE = regexp.MustCompile(`updated: ([0-9]{4}-[0-9]{2}-[0-9]{2})`)

// tasksFile cuts TASKS.md (or the archive) into its tasks, one chunk
// each (more for a long one), id T<n>.
func tasksFile(text string, c Chunk) []piece {
	c.Kind = KindTask
	var out []piece
	for _, s := range sections(text, frontEnd(text), 3) {
		m := taskHeadRE.FindStringSubmatch(s.heading)
		if m == nil {
			continue
		}
		t := c
		t.Task = m[1]
		body := text[s.headingEnd:s.end]
		status, _, _ := strings.Cut(strings.TrimLeft(body, "\n"), "\n")
		if strings.HasPrefix(status, "status:") {
			t.Thread = threadRefRE.FindString(status)
			if u := updatedRE.FindStringSubmatch(status); u != nil {
				if at, err := time.Parse("2006-01-02", u[1]); err == nil {
					t.Time = at
				}
			}
		}
		// The board's group headings (## Backlog) end the task before.
		end := s.end
		for _, g := range sections(text[:s.end], s.headingEnd, 2) {
			if g.heading != "" {
				end = g.off
				break
			}
		}
		for k, pr := range pieces(text, s.off, end) {
			p := piece{Chunk: t, head: s.heading}
			p.ID, p.Title, p.Off, p.Len = m[1], s.heading, pr[0], pr[1]-pr[0]
			if k > 0 {
				p.ID = m[1] + "#" + strconv.Itoa(k+1)
			}
			out = append(out, p)
		}
	}
	return out
}

// threadFile cuts one file of a thread (rest is its path in the
// thread's folder) into chunks: <id>/brief, <id>/task and
// <id>/followup.<n>, <id>/report.<n>, lib:<id>/<name>.
// The thread's title is in each chunk's Title, to show, but weighs as
// a head in none: it would match every chunk of the thread.
func threadFile(file, id, rest, text string, meta threadMeta, reports []int, c Chunk) []piece {
	ps := threadPieces(file, id, rest, text, meta, reports, c)
	if file != fileLibrary {
		for i := range ps {
			if meta.Title != "" && strings.HasSuffix(ps[i].head, meta.Title) || strings.HasSuffix(ps[i].head, ": "+id) {
				ps[i].head = ""
			}
		}
	}
	return ps
}

func threadPieces(file, id, rest, text string, meta threadMeta, reports []int, c Chunk) []piece {
	title := meta.Title
	if title == "" {
		title = id
	}
	c.Kind = KindThread
	switch file {
	case fileBrief:
		return markdown(text, 0, 3, id+"/brief", "Brief: "+title, c)
	case fileTaskMD:
		return taskMD(text, id, title, c)
	case fileReport:
		n := 1
		if rest == "REPORT.md" {
			if len(reports) > 0 {
				n = slices.Max(reports) + 1
			}
		} else if r := reportRE.FindStringSubmatch(path.Base(rest)); r != nil {
			n, _ = strconv.Atoi(r[1])
		}
		return markdown(text, 0, 3, fmt.Sprintf("%s/report.%d", id, n), fmt.Sprintf("Report %d: %s", n, title), c)
	case fileLibrary:
		c.Kind = KindLibrary
		name := strings.TrimPrefix(rest, "library/")
		base := "lib:" + id + "/" + name
		if strings.EqualFold(path.Ext(name), ".md") {
			return markdown(text, frontEnd(text), 3, base, name, c)
		}
		return plain(text, base, name, c)
	}
	return nil
}

// taskMD cuts a thread's task.md into the task as given (<id>/task) and
// the coordinator's follow-ups (<id>/followup.<n>, from 1).
func taskMD(text, id, title string, c Chunk) []piece {
	var out []piece
	n := 0
	for _, s := range sections(text, 0, 2) {
		at, isFollowUp := strings.CutPrefix(s.heading, "Follow-up")
		t := c
		base, ttl := id+"/task", "Task: "+title
		if isFollowUp {
			n++
			base, ttl = fmt.Sprintf("%s/followup.%d", id, n), fmt.Sprintf("Follow-up %d: %s", n, title)
			if ts, err := time.Parse(time.RFC3339, strings.TrimSpace(at)); err == nil {
				t.Time = ts.UTC()
			}
		} else if s.heading != "" && len(out) > 0 {
			// A heading inside the task text stays with it.
			last := &out[len(out)-1]
			if last.ID == id+"/task" || strings.HasPrefix(last.ID, id+"/task#") {
				if last.Off+last.Len == s.off && last.Len+(s.end-s.off) <= MaxChunk {
					last.Len += s.end - s.off
					continue
				}
			}
		}
		if blank(text[s.off:s.end]) {
			continue
		}
		for k, pr := range pieces(text, s.off, s.end) {
			p := piece{Chunk: t, head: ttl}
			p.ID, p.Title, p.Off, p.Len = base, ttl, pr[0], pr[1]-pr[0]
			if k > 0 {
				p.ID = base + "#" + strconv.Itoa(k+1)
			}
			out = append(out, p)
		}
	}
	return out
}

// inboxItem is an inbox item (<stamp>-<kind>-<who>.md) as one chunk (more
// for a long one), id inbox:<name>, the coordinator's only.
func inboxItem(name, text string, c Chunk) []piece {
	c.Kind, c.Coordinator = KindInbox, true
	base := strings.TrimSuffix(name, ".md")
	stamp, rest, _ := strings.Cut(base, "-")
	if at, err := time.Parse("20060102T150405Z", stamp); err == nil {
		c.Time = at
	}
	c.Thread = threadRefRE.FindString(rest)
	c.Task = taskRefRE.FindString(text)
	title := firstHeading(text, frontEnd(text))
	if title == "" {
		title = strings.ReplaceAll(rest, "-", " ")
	}
	return plain(text, "inbox:"+base, title, c)
}

// journalLines makes each journal line from off a chunk, id
// j:<yyyymmddThhmmssZ>, then -2, -3… for more lines in the same second;
// seen holds the ids taken already (by lines before off). Lines
// without a timestamp are left out.
func journalLines(text string, off int, seen map[string]bool, c Chunk) []piece {
	c.Kind = KindJournal
	if seen == nil {
		seen = map[string]bool{}
	}
	var out []piece
	for i := off; i < len(text); {
		j := strings.IndexByte(text[i:], '\n')
		next := len(text)
		if j >= 0 {
			next = i + j + 1
		}
		line := strings.TrimRight(text[i:next], "\r\n")
		ts, rest, _ := strings.Cut(line, " ")
		if at, err := time.Parse(time.RFC3339, ts); err == nil && strings.TrimSpace(rest) != "" {
			id := "j:" + at.UTC().Format("20060102T150405Z")
			for k := 2; seen[id]; k++ {
				id = "j:" + at.UTC().Format("20060102T150405Z") + "-" + strconv.Itoa(k)
			}
			seen[id] = true
			p := piece{Chunk: c}
			p.ID, p.Title, p.Time, p.Off, p.Len = id, unlink(rest), at.UTC(), i, len(line)
			p.Task = taskRefRE.FindString(rest)
			p.Thread = threadRefRE.FindString(rest)
			out = append(out, p)
		}
		i = next
	}
	return out
}
