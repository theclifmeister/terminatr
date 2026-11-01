package project

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/vaultlink"
)

// Changing the memory notes (docs/SPEC.md §7.6, Memory notes): tm
// memory new, edit and rm, and the Memory tab's keys, which run them.
// Only a regular file directly in the project's memory/ folder is a
// note to change, never a link, nor the folder when it is one; after
// every change tm writes MEMORY.md's list of the notes again
// (RebuildIndex), and a change the human made is an inbox item for the
// coordinator, so it re-reads the note and doesn't write back what the
// human removed.

// KindMemory is the inbox item that tells the coordinator the user
// added, edited or removed a memory note.
const KindMemory = "memory"

// The changes NoteChanged records.
const (
	NoteNew  = "new"
	NoteEdit = "edit"
	NoteRm   = "rm"
)

var noteNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// errNotNote refuses arg as a note of the project.
func errNotNote(arg string) error {
	return refuse("not-memory", "%q is not a note in the project's memory/ folder (memory/<name>.md)", arg)
}

// NotePath is the note arg names, as memory/<name>.md: <name>,
// <name>.md, memory/<name>.md, or the note's absolute path. Anything
// else, a path in a folder below memory/ or with a .. step included,
// is refused (not-memory). It only checks the name; the file may not
// exist.
func (p *Project) NotePath(arg string) (string, error) {
	name := arg
	if filepath.IsAbs(arg) {
		rel, ok := fsx.Within(p.Path("memory"), filepath.Clean(arg))
		if !ok {
			return "", errNotNote(arg)
		}
		name = rel
	} else if rest, ok := strings.CutPrefix(filepath.ToSlash(arg), "memory/"); ok {
		name = rest
	}
	name = strings.TrimSuffix(name, ".md")
	if !noteNameRE.MatchString(name) || strings.Contains(name, "..") {
		return "", errNotNote(arg)
	}
	return "memory/" + name + ".md", nil
}

// NewNotePath is the path of a new note named name: NotePath's, or for
// words that aren't a file name ("Retries for the poller") their slug.
// A name with a folder in it is refused.
func (p *Project) NewNotePath(name string) (string, error) {
	if rel, err := p.NotePath(name); err == nil {
		return rel, nil
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return "", errNotNote(name)
	}
	s := Slugify(strings.TrimSuffix(name, ".md"))
	if s == "" {
		return "", errNotNote(name)
	}
	return p.NotePath(s)
}

// notesDir is the memory/ folder, refused when it is a link (it would
// lead a write out of the project); made when make is set.
func (p *Project) notesDir(make bool) error {
	dir := p.Path("memory")
	if make {
		if err := fsx.MkdirNoLink(dir, 0o755); err != nil {
			return refuse("link", "%v: tm doesn't change memory through a link", err)
		}
		return nil
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if fsx.IsLink(fi) || !fi.IsDir() {
		return refuse("link", "%s is not a folder: tm doesn't change memory through a link", dir)
	}
	return nil
}

// noteFile checks that note rel (NotePath's) is a regular file, not a
// link; fs.ErrNotExist when there is none.
func (p *Project) noteFile(rel string) (fs.FileInfo, error) {
	if err := p.notesDir(false); err != nil {
		return nil, err
	}
	fi, err := os.Lstat(p.Path(filepath.FromSlash(rel)))
	if err != nil {
		return nil, err
	}
	if fsx.IsLink(fi) || !fi.Mode().IsRegular() {
		return nil, refuse("link", "%s is not a regular file: tm doesn't change memory through a link", rel)
	}
	return fi, nil
}

// HasNote says whether path is a note of the project: NotePath's form
// of itself, and a regular file in memory/.
func (p *Project) HasNote(path string) bool {
	rel, err := p.NotePath(path)
	if err != nil || rel != path {
		return false
	}
	_, err = p.noteFile(rel)
	return err == nil
}

// ReadNote reads note rel (NotePath's), never through a link, at most
// MaxMemoryRead bytes.
func (p *Project) ReadNote(rel string) ([]byte, error) {
	if _, err := p.noteFile(rel); err != nil {
		return nil, err
	}
	f, err := fsx.OpenNoLink(p.Path(filepath.FromSlash(rel)), os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxMemoryRead+1))
	if err == nil && len(data) > MaxMemoryRead {
		return nil, refuse("too-big", "%s is over %d bytes", rel, MaxMemoryRead)
	}
	return data, err
}

// CreateNote writes note rel (NotePath's), which must not exist yet
// (exists), its source linked (LinkSource).
func (p *Project) CreateNote(rel string, data []byte) error {
	if err := p.notesDir(true); err != nil {
		return err
	}
	data = p.LinkSource(data)
	f, err := fsx.OpenNoLink(p.Path(filepath.FromSlash(rel)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return refuse("exists", "%s exists: tm memory edit %s changes it", rel, rel)
	}
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	return errors.Join(err, f.Close())
}

// WriteNote replaces note rel (NotePath's), which must be there, with
// data, its source linked (LinkSource). The new file takes the old one's place by a rename, so a link
// planted meanwhile is replaced, never followed.
func (p *Project) WriteNote(rel string, data []byte) error {
	if _, err := p.noteFile(rel); err != nil {
		return err
	}
	return fsx.WriteAtomic(p.Path(filepath.FromSlash(rel)), p.LinkSource(data), 0o644)
}

// RemoveNote deletes note rel (NotePath's).
func (p *Project) RemoveNote(rel string) error {
	if _, err := p.noteFile(rel); err != nil {
		return err
	}
	return os.Remove(p.Path(filepath.FromSlash(rel)))
}

// NoteStamp is what a note held when an edit of it began: its time,
// size and hash, to tell afterwards whether someone else changed it.
type NoteStamp struct {
	Gone bool
	Mod  time.Time
	Size int64
	Hash [sha256.Size]byte
}

// StampNote is note rel's NoteStamp now, and its text.
func (p *Project) StampNote(rel string) (NoteStamp, []byte, error) {
	fi, err := p.noteFile(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return NoteStamp{Gone: true}, nil, nil
	}
	if err != nil {
		return NoteStamp{}, nil, err
	}
	data, err := p.ReadNote(rel)
	if err != nil {
		return NoteStamp{}, nil, err
	}
	return NoteStamp{Mod: fi.ModTime(), Size: fi.Size(), Hash: sha256.Sum256(data)}, data, nil
}

// Changed says whether note rel differs from s: removed, created, or
// its text changed (the same time and size are taken as unchanged; an
// other time is checked by the hash).
func (s NoteStamp) Changed(p *Project, rel string) (bool, error) {
	now, err := p.noteFile(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return !s.Gone, nil
	}
	if err != nil {
		return false, err
	}
	if s.Gone {
		return true, nil
	}
	if now.ModTime().Equal(s.Mod) && now.Size() == s.Size {
		return false, nil
	}
	data, err := p.ReadNote(rel)
	if err != nil {
		return false, err
	}
	return sha256.Sum256(data) != s.Hash, nil
}

// ConflictPath is where an edit of note rel that someone else changed
// meanwhile keeps the editor's copy: memory/<name>.md.conflict, beside
// the note and never itself a note.
func ConflictPath(rel string) string { return rel + ".conflict" }

// WriteConflict writes data to note rel's ConflictPath (replacing an
// older one), never through a link.
func (p *Project) WriteConflict(rel string, data []byte) error {
	if err := p.notesDir(false); err != nil {
		return err
	}
	path := p.Path(filepath.FromSlash(ConflictPath(rel)))
	if fi, err := os.Lstat(path); err == nil && (fsx.IsLink(fi) || !fi.Mode().IsRegular()) {
		return refuse("link", "%s is not a regular file", ConflictPath(rel))
	}
	return fsx.WriteAtomic(path, data, 0o644)
}

// NoteChanged records change op (NoteNew, NoteEdit, NoteRm) of note
// rel by c: MEMORY.md's list is written again, a journal line
// memory.<op> <rel>, and, when the human made it, a memory item for the
// coordinator (one per note and change while unhandled).
func (p *Project) NoteChanged(c caller.Caller, op, rel string) error {
	_, ierr := p.RebuildIndex()
	jerr := p.Journal(c, "memory."+op, rel, "")
	if c.Kind != caller.Human {
		return errors.Join(ierr, jerr)
	}
	var summary string
	switch op {
	case NoteNew:
		summary = "the user added " + rel + ": read it"
	case NoteEdit:
		summary = "the user edited " + rel + ": read it again before you change it"
	default:
		summary = "the user removed " + rel + ": don't write its content back"
	}
	items, err := p.Inbox()
	if err == nil && !slices.ContainsFunc(items, func(it Item) bool {
		return it.Kind == KindMemory && it.Subject == rel && it.Summary == summary
	}) {
		_, err = p.AddItem(KindMemory, rel, summary, false)
	}
	return errors.Join(ierr, jerr, err)
}

// indexDesc is the most an index line says of a note, in runes.
const indexDesc = 120

// indexTitle is the title note n's line in MEMORY.md shows: its front
// matter's title, else keptTitle, the one its line had already ("" for
// none), else its first heading (a note with front matter) or its file
// name (a free-form note).
func indexTitle(n Note, keptTitle string) string {
	title := n.Title
	switch {
	case n.Front && n.titled:
	case textsafe.Line(keptTitle, 0) != "":
		title = keptTitle
	case !n.Front:
		title = strings.TrimSuffix(path.Base(n.Path), ".md")
	}
	return strings.NewReplacer("[", "(", "]", ")").Replace(textsafe.Line(title, indexDesc))
}

// IndexLine is note n's line in MEMORY.md: "- [[<slug>/memory/file|title]]:
// what it holds (touches: a, b)", a link to the note (vaultlink), or
// "- [title](memory/file.md): …" while links aren't written. The title
// is indexTitle's; what it holds is kept, the description the note's
// line had already ("" for none), else the note's first line of text (a
// heading's text, but none of the schema's section headings; for a
// free-form note, no front matter, its first line whatever it is).
func IndexLine(slug string, n Note, keptTitle, kept string) string {
	title := indexTitle(n, keptTitle)
	desc := firstLine(n.Body, n.Front)
	line := fmt.Sprintf("- [%s](%s)", title, n.Path)
	if l := vaultlink.Link(slug, vaultlink.Memory, n.Path, title); strings.HasPrefix(l, "[[") {
		line = "- " + l
	}
	if kept = textsafe.Line(kept, 0); kept != "" {
		line += ": " + kept
	} else if desc = textsafe.Line(desc, indexDesc); desc != "" && desc != title {
		line += ": " + desc
	}
	if len(n.Touches) > 0 {
		line += " (touches: " + textsafe.Line(strings.Join(n.Touches, ", "), 0) + ")"
	}
	return line
}

// firstLine is text's first non-blank line, a heading's # marks cut;
// skipHeads leaves the headings out.
func firstLine(text string, skipHeads bool) string {
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || l == "+++" || l == "---" {
			continue
		}
		if h := strings.TrimLeft(l, "#"); len(h) < len(l) {
			if skipHeads {
				continue
			}
			l = strings.TrimSpace(h)
		}
		if l != "" {
			return l
		}
	}
	return ""
}

// indexLineRE reads a note's line in MEMORY.md: its title, its path,
// then the rest after the link; wikiLineRE one with a wikilink
// (IndexLine): the note's name, its title, the rest.
var (
	indexLineRE = regexp.MustCompile(`^- \[(.*?)\]\((memory/[^)\s]+)\)(.*)$`)
	wikiLineRE  = regexp.MustCompile(`^- \[\[[^\[\]|]*?/memory/([^\[\]|/]+)\|([^\[\]]*?)\]\](.*)$`)
)

// keptLine is what index line l says of its note: its path, its title
// and its description, the text after the link and its ":" (or a dash),
// without the touches tm adds. ok is false for a line that isn't a
// note's.
func keptLine(l string) (path, title, desc string, ok bool) {
	if m := wikiLineRE.FindStringSubmatch(l); m != nil {
		title, path, desc = strings.TrimSpace(m[2]), "memory/"+m[1]+".md", strings.TrimSpace(m[3])
	} else if m := indexLineRE.FindStringSubmatch(l); m != nil {
		title, path, desc = strings.TrimSpace(m[1]), m[2], strings.TrimSpace(m[3])
	} else {
		return "", "", "", false
	}
	if i := strings.LastIndex(desc, "(touches: "); i >= 0 && strings.HasSuffix(desc, ")") {
		desc = strings.TrimSpace(desc[:i])
	}
	for _, sep := range []string{":", "—", "–", "-"} {
		if rest, cut := strings.CutPrefix(desc, sep); cut {
			desc = strings.TrimSpace(rest)
			break
		}
	}
	return path, title, desc, true
}

// isIndexLine says whether l is one of MEMORY.md's lines for a note,
// in either form IndexLine writes.
func isIndexLine(l string) bool {
	return strings.HasPrefix(l, "- [") && strings.Contains(l, "](memory/") || wikiLineRE.MatchString(l)
}

// PlainIndex is MEMORY.md's text as an agent or a terminal is shown it:
// each note's wikilink (IndexLine) as a Markdown link to its file,
// "- [title](memory/file.md): …", the rest as it is.
func PlainIndex(text string) string {
	if !strings.Contains(text, "[[") {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if m := wikiLineRE.FindStringSubmatch(strings.TrimRight(l, "\r")); m != nil {
			lines[i] = "- [" + m[2] + "](memory/" + m[1] + ".md)" + m[3]
		}
	}
	return strings.Join(lines, "\n")
}

// RebuildIndex writes MEMORY.md's list of the notes again, one
// IndexLine per note in memory/ (a regular .md file; one too big to
// read shows by its file name), by title; a note that had a line keeps
// that line's description. The rest of the file stays as it is: its heading and text, the list where its first line was (else
// at the end). A missing MEMORY.md starts from the new project's. It
// says whether the file changed.
func (p *Project) RebuildIndex() (bool, error) {
	notes, err := p.indexNotes()
	if err != nil {
		return false, err
	}
	file := p.Path("MEMORY.md")
	var old []byte
	missing := false
	switch fi, err := os.Lstat(file); {
	case errors.Is(err, fs.ErrNotExist):
		old, missing = []byte(memoryTemplate), true
	case err != nil:
		return false, err
	case fsx.IsLink(fi) || !fi.Mode().IsRegular():
		return false, refuse("link", "MEMORY.md is not a regular file: tm doesn't write it through a link")
	default:
		f, err := fsx.OpenNoLink(file, os.O_RDONLY, 0)
		if err != nil {
			return false, err
		}
		old, err = io.ReadAll(f)
		f.Close()
		if err != nil {
			return false, err
		}
	}
	lines := strings.Split(strings.ReplaceAll(string(old), "\r\n", "\n"), "\n")
	entries := p.indexEntries(notes, lines)
	list := make([]string, len(entries))
	for i, en := range entries {
		list[i] = en.Line
	}
	var out []string
	placed := false
	for _, l := range lines {
		if !isIndexLine(l) {
			out = append(out, l)
			continue
		}
		if !placed {
			out = append(out, list...)
			placed = true
		}
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	if !placed && len(list) > 0 {
		out = append(append(out, ""), list...)
	}
	data := []byte(strings.Join(out, "\n") + "\n")
	if !missing && bytes.Equal(data, old) {
		return false, nil
	}
	return true, fsx.WriteAtomic(file, data, 0o644)
}

// IndexEntry is one note's line in MEMORY.md (IndexLine), with the
// note it lists.
type IndexEntry struct {
	Note Note
	Line string
}

// indexEntries are the notes' lines in MEMORY.md, by the title each
// shows: a note that has a line among lines keeps that line's title
// and description.
func (p *Project) indexEntries(notes []Note, lines []string) []IndexEntry {
	type said struct{ title, desc string }
	kept := map[string]said{}
	for _, l := range lines {
		if isIndexLine(l) {
			if path, title, desc, ok := keptLine(l); ok {
				if _, seen := kept[path]; !seen {
					kept[path] = said{title, desc}
				}
			}
		}
	}
	notes = slices.Clone(notes)
	slices.SortFunc(notes, func(a, b Note) int {
		ta, tb := indexTitle(a, kept[a.Path].title), indexTitle(b, kept[b.Path].title)
		return cmp.Or(cmp.Compare(strings.ToLower(ta), strings.ToLower(tb)), cmp.Compare(a.Path, b.Path))
	})
	out := make([]IndexEntry, len(notes))
	for i, n := range notes {
		out[i] = IndexEntry{Note: n, Line: IndexLine(p.Slug, n, kept[n.Path].title, kept[n.Path].desc)}
	}
	return out
}

// MemoryIndex is MEMORY.md's list of the notes as RebuildIndex would
// write it, without writing it: one IndexEntry per note in memory/,
// each line in PlainIndex's form (a Markdown link to the note's file),
// by title. forThreads leaves out the notes threads aren't shown
// (ForThreads).
func (p *Project) MemoryIndex(forThreads bool) ([]IndexEntry, error) {
	notes, err := p.indexNotes()
	if err != nil {
		return nil, err
	}
	if forThreads {
		notes = ThreadNotes(notes)
	}
	var lines []string
	if fi, err := os.Lstat(p.Path("MEMORY.md")); err == nil && fi.Mode().IsRegular() {
		if f, err := fsx.OpenNoLink(p.Path("MEMORY.md"), os.O_RDONLY, 0); err == nil {
			data, _ := io.ReadAll(io.LimitReader(f, MaxMemoryRead))
			f.Close()
			lines = strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		}
	}
	entries := p.indexEntries(notes, lines)
	for i := range entries {
		entries[i].Line = PlainIndex(entries[i].Line)
	}
	return entries, nil
}

// indexStamps are SyncIndex's stamps of what it found in sync, by
// MEMORY.md's path.
var indexStamps sync.Map

// indexStamp is the stat stamp of MEMORY.md, memory/ and the entries in
// it: what a rebuild reads.
func (p *Project) indexStamp() uint64 {
	paths := []string{p.Path("MEMORY.md"), p.Path("memory")}
	if ents, err := os.ReadDir(p.Path("memory")); err == nil {
		for _, e := range ents {
			paths = append(paths, p.Path("memory", e.Name()))
		}
	}
	return fsx.StatStamp(paths...)
}

// SyncIndex brings MEMORY.md's list in line with memory/ when it isn't
// (RebuildIndex), so a note written by hand, or an index an older tm
// left, needs no command: tm context and the ticker's upkeep run it. It
// is a few stats while nothing changed since it last found the list in
// sync; it writes only when the list differs, and journals that as the
// ticker's memory.index. It says whether it wrote.
func (p *Project) SyncIndex() (bool, error) {
	key := p.Path("MEMORY.md")
	st := p.indexStamp()
	if v, ok := indexStamps.Load(key); ok && v.(uint64) == st {
		return false, nil
	}
	if _, err := os.Lstat(key); errors.Is(err, fs.ErrNotExist) {
		if ents, _ := os.ReadDir(p.Path("memory")); len(ents) == 0 {
			return false, nil // nothing to list, nor a file to keep it in
		}
	}
	changed, err := p.RebuildIndex()
	if err != nil {
		return false, err
	}
	indexStamps.Store(key, p.indexStamp())
	if changed {
		err = p.Journal(caller.Caller{Kind: caller.Ticker}, "memory.index", "MEMORY.md", "synced with memory/")
	}
	return changed, err
}

// indexNotes are the notes RebuildIndex lists.
func (p *Project) indexNotes() ([]Note, error) {
	ents, err := os.ReadDir(p.Path("memory"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var notes []Note
	for _, e := range ents {
		name, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok || !e.Type().IsRegular() {
			continue
		}
		rel := "memory/" + e.Name()
		data, err := p.ReadNote(rel)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue // gone since
			}
			notes = append(notes, Note{Path: rel, NoteFront: NoteFront{Title: name}})
			continue
		}
		notes = append(notes, ParseNote(rel, string(data)))
	}
	return notes, nil
}
