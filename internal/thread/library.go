package thread

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thumb"
)

// The library (docs/SPEC.md §7.2, §4 Project popup): the files threads
// attached to their reports, threads/<id>/library/, of every thread of
// a project, the resolved and archived ones too (an archived thread's
// files are in its tarball), and the coordinator's own (seatlibrary.go).

// LibFile is one file of a thread's library. It never carries a path:
// the thread and the name say where it is. A file a linked project sent
// (docs/SPEC.md §16.13) has no thread: Peer is that project and Message
// the message it came with (link.LibraryFiles). A seat's own file has
// SeatID for its thread. In a program's listing (the director's Files
// view) Project is the member project it is in, Server that project's
// paired server, both empty for the director's own.
type LibFile struct {
	Thread string `json:"thread"`
	Task   string `json:"task,omitempty"`
	// TaskStatus is that task's status, on the board or in its archive; ""
	// for a thread with no task, or a task no longer there.
	TaskStatus string `json:"task_status,omitempty"`
	Title      string `json:"title"`
	// Repo is the folder name of the thread's repository, in a project with
	// several (a filter finds by it); never a path.
	Repo string `json:"repo,omitempty"`
	// State is the thread's: running, stopped, resolved or archived;
	// received for a linked project's file.
	State string `json:"state"`
	Name  string `json:"name"`
	// DocTitle is the file's own title, a page's title or a note's first
	// heading (DocTitle); "" for none, and the file shows by its name.
	DocTitle string `json:"doc_title,omitempty"`
	// Published is where the designs list says this page is published:
	// only a page of a design task has it.
	Published string    `json:"published,omitempty"`
	Size      int64     `json:"size"`
	Time      time.Time `json:"time"`
	Peer      string    `json:"peer,omitempty"`
	Message   string    `json:"message,omitempty"`
	Project   string    `json:"project,omitempty"`
	Server    string    `json:"server,omitempty"`
}

// Group is the Library tab's group of f: its thread, or the linked
// project it came from, in its project when a program lists it.
func (f LibFile) Group() string {
	g := f.Thread
	if f.Peer != "" {
		g = "@" + f.Peer
	}
	if f.Project != "" {
		return f.Server + "/" + f.Project + "/" + g
	}
	return g
}

// Library lists every file in the library of every thread of p, newest
// file first (ties by thread, then name).
func Library(p *project.Project) ([]LibFile, error) {
	var out []LibFile
	several := len(p.Repos) > 1
	recs, err := List(p)
	if err != nil {
		return nil, err
	}
	status := taskStatuses(p)
	pages := publishedPages(p)
	live := map[string]bool{}
	for _, r := range recs {
		live[r.ID] = true
		ents, err := os.ReadDir(Path(p, r.ID, "library"))
		if err != nil {
			continue
		}
		for _, e := range ents {
			if !e.Type().IsRegular() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			fi, err := e.Info()
			if err != nil {
				continue
			}
			f := LibFile{Thread: r.ID, Task: r.Task, TaskStatus: status[r.Task], Title: r.Title, State: r.State,
				Name: e.Name(), DocTitle: FileDocTitle(Path(p, r.ID, "library", e.Name()), e.Name()),
				Published: publishedOf(pages, r.Task, e.Name()), Size: fi.Size(), Time: fi.ModTime()}
			if several {
				f.Repo = repoName(r.Repo)
			}
			out = append(out, f)
		}
	}
	arch, _ := ListArchived(p)
	for _, a := range arch {
		if live[a.ID] {
			continue // archived, then not yet removed: the folder is the truth
		}
		for _, f := range archivedLibrary(p, a.ID) {
			f.Task, f.TaskStatus, f.Title, f.State = a.Task, status[a.Task], a.Title, "archived"
			f.Published = publishedOf(pages, a.Task, f.Name)
			if !several {
				f.Repo = ""
			}
			out = append(out, f)
		}
	}
	out = append(out, SeatLibrary(&p.Folder)...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !a.Time.Equal(b.Time) {
			return a.Time.After(b.Time)
		}
		if a.Thread != b.Thread {
			return a.Thread < b.Thread
		}
		return a.Name < b.Name
	})
	return out, nil
}

// taskStatuses is every task's status by its ref, the archive's too: a
// board that can't be read leaves the headings without one.
func taskStatuses(p *project.Project) map[string]string {
	out := map[string]string{}
	s := p.Tasks()
	for _, load := range []func() (*tasks.Board, error){s.LoadArchive, s.Load} {
		b, err := load()
		if err != nil {
			continue
		}
		for _, t := range b.Tasks {
			out[t.Ref()] = string(t.Status)
		}
	}
	return out
}

// archiveLibCache remembers an archive's library listing while its
// tarball is unchanged, since reading one means unpacking it.
var archiveLibCache sync.Map // path -> cachedLib

type cachedLib struct {
	mod   time.Time
	size  int64
	files []LibFile
}

// repoName is a repository path's folder name, "" for none.
func repoName(dir string) string {
	if dir == "" {
		return ""
	}
	return filepath.Base(filepath.Clean(dir))
}

// archivedLibrary lists the library files in thread id's tarball, each
// with the repository its thread.toml names.
func archivedLibrary(p *project.Project, id string) []LibFile {
	ap := archivePath(p, id)
	fi, err := os.Stat(ap)
	if err != nil {
		return nil
	}
	if c, ok := archiveLibCache.Load(ap); ok {
		if c := c.(cachedLib); c.mod.Equal(fi.ModTime()) && c.size == fi.Size() {
			return c.files
		}
	}
	var out []LibFile
	repo := ""
	_ = walkArchive(ap, func(h *tar.Header, body io.Reader) error {
		if name, ok := libraryName(id, h); ok {
			out = append(out, LibFile{Thread: id, Name: name, DocTitle: readDocTitle(name, body), Size: h.Size, Time: h.ModTime})
		} else if path.Clean(h.Name) == id+"/thread.toml" {
			var r Record
			if _, err := toml.NewDecoder(io.LimitReader(body, maxArchivedFile)).Decode(&r); err == nil {
				repo = repoName(r.Repo)
			}
		}
		return nil
	})
	for i := range out {
		out[i].Repo = repo
	}
	archiveLibCache.Store(ap, cachedLib{fi.ModTime(), fi.Size(), out})
	return out
}

// libraryName is the library file a tar entry of thread id's archive
// is: its name, else false.
func libraryName(id string, h *tar.Header) (string, bool) {
	if h.Typeflag != tar.TypeReg {
		return "", false
	}
	rest, ok := strings.CutPrefix(path.Clean(h.Name), id+"/library/")
	if !ok || rest == "" || strings.Contains(rest, "/") || strings.HasPrefix(rest, ".") {
		return "", false
	}
	return rest, true
}

func walkArchive(ap string, fn func(*tar.Header, io.Reader) error) error {
	f, err := os.Open(ap)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := fn(h, tr); err != nil {
			return err
		}
	}
}

// checkLibName refuses a name that isn't a plain file name: no path
// goes in or out of a library.
func checkLibName(name string) error {
	if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return refuse("invalid-file", "%q is not a library file name", name)
	}
	return nil
}

// ReadLibraryFile reads up to max bytes of a library file, from the
// folder or, for an archived thread, from the tarball. truncated says
// the file was longer.
func ReadLibraryFile(p *project.Project, id, name string, max int64) (data []byte, truncated bool, err error) {
	if id == SeatID {
		data, size, _, err := ReadSeatChunk(&p.Folder, name, 0, max)
		return data, size > max, err
	}
	if !ValidID(id) {
		return nil, false, refuse("unknown-thread", "%q is not a thread id like t-0003", id)
	}
	if err := checkLibName(name); err != nil {
		return nil, false, err
	}
	f, err := os.Open(Path(p, id, "library", name))
	if err == nil {
		defer f.Close()
		data, err = io.ReadAll(io.LimitReader(f, max+1))
		if err != nil {
			return nil, false, err
		}
		if int64(len(data)) > max {
			return data[:max], true, nil
		}
		return data, false, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, false, err
	}
	found := false
	werr := walkArchive(archivePath(p, id), func(h *tar.Header, r io.Reader) error {
		if n, ok := libraryName(id, h); ok && n == name && !found {
			found = true
			data, err = io.ReadAll(io.LimitReader(r, max+1))
			if err == nil && int64(len(data)) > max {
				data, truncated = data[:max], true
			}
			return err
		}
		return nil
	})
	if werr != nil && !errors.Is(werr, fs.ErrNotExist) {
		return nil, false, werr
	}
	if !found {
		return nil, false, refuse("unknown-file", "thread %s has no library file %s", id, name)
	}
	return data, truncated, err
}

// LibraryFileInfo says a library file's size and modification time, from
// the folder (never through a link) or, for an archived thread, from the
// tarball's listing, kept while the tarball is unchanged, so it doesn't
// unpack the thread.
func LibraryFileInfo(p *project.Project, id, name string) (size int64, mod time.Time, err error) {
	if id == SeatID {
		return SeatFileInfo(&p.Folder, name)
	}
	if !ValidID(id) {
		return 0, time.Time{}, refuse("unknown-thread", "%q is not a thread id like t-0003", id)
	}
	if err := checkLibName(name); err != nil {
		return 0, time.Time{}, err
	}
	fi, err := os.Lstat(Path(p, id, "library", name))
	switch {
	case err == nil && fi.Mode().IsRegular():
		return fi.Size(), fi.ModTime(), nil
	case err == nil:
		return 0, time.Time{}, refuse("unknown-file", "thread %s has no library file %s", id, name)
	case !errors.Is(err, fs.ErrNotExist):
		return 0, time.Time{}, err
	}
	for _, f := range archivedLibrary(p, id) {
		if f.Name == name {
			return f.Size, f.Time, nil
		}
	}
	return 0, time.Time{}, refuse("unknown-file", "thread %s has no library file %s", id, name)
}

// ReadLibraryChunk reads at most n bytes of a library file from off,
// from the folder (never through a link) or, for an archived thread,
// from the tarball, and says the file's size and modification time.
func ReadLibraryChunk(p *project.Project, id, name string, off, n int64) (data []byte, size int64, mod time.Time, err error) {
	if id == SeatID {
		return ReadSeatChunk(&p.Folder, name, off, n)
	}
	if !ValidID(id) {
		return nil, 0, time.Time{}, refuse("unknown-thread", "%q is not a thread id like t-0003", id)
	}
	if err := checkLibName(name); err != nil {
		return nil, 0, time.Time{}, err
	}
	f, err := fsx.OpenNoLink(Path(p, id, "library", name), os.O_RDONLY, 0)
	if err == nil {
		defer f.Close()
		fi, err := f.Stat()
		if err != nil {
			return nil, 0, time.Time{}, err
		}
		if !fi.Mode().IsRegular() {
			return nil, 0, time.Time{}, refuse("unknown-file", "thread %s has no library file %s", id, name)
		}
		data, err := readChunk(io.NewSectionReader(f, 0, fi.Size()), fi.Size(), off, n)
		return data, fi.Size(), fi.ModTime(), err
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, 0, time.Time{}, err
	}
	found := false
	werr := walkArchive(archivePath(p, id), func(h *tar.Header, r io.Reader) error {
		if n2, ok := libraryName(id, h); ok && n2 == name && !found {
			found, size, mod = true, h.Size, h.ModTime
			data, err = readChunk(r, h.Size, off, n)
			return err
		}
		return nil
	})
	if werr != nil && !errors.Is(werr, fs.ErrNotExist) {
		return nil, 0, time.Time{}, werr
	}
	if !found {
		return nil, 0, time.Time{}, refuse("unknown-file", "thread %s has no library file %s", id, name)
	}
	return data, size, mod, err
}

// readChunk reads at most n bytes from off of r, size bytes long.
func readChunk(r io.Reader, size, off, n int64) ([]byte, error) {
	if off < 0 || off > size {
		return nil, fmt.Errorf("offset %d is past the end (%d)", off, size)
	}
	if _, err := io.CopyN(io.Discard, r, off); err != nil {
		return nil, err
	}
	buf := make([]byte, min(size-off, n))
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// RemoveLibrary deletes library files of thread id: the named one, or
// every one when name is "". It journals each deletion as library.rm and
// returns how many it removed. A resolved thread's archive is rewritten
// without them.
func RemoveLibrary(p *project.Project, c caller.Caller, id, name string) (int, error) {
	if id == SeatID {
		return RemoveSeatFiles(&p.Folder, c, name)
	}
	if !ValidID(id) {
		return 0, refuse("unknown-thread", "%q is not a thread id like t-0003", id)
	}
	if name != "" {
		if err := checkLibName(name); err != nil {
			return 0, err
		}
	}
	var gone []string
	dir := Path(p, id, "library")
	if _, err := os.Stat(Dir(p, id)); err == nil {
		ents, err := os.ReadDir(dir)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return 0, err
		}
		for _, e := range ents {
			if !e.Type().IsRegular() || strings.HasPrefix(e.Name(), ".") || name != "" && e.Name() != name {
				continue
			}
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				return len(gone), err
			}
			gone = append(gone, e.Name())
		}
		_ = os.Remove(dir) // when it is empty
	} else {
		var err error
		if gone, err = removeArchived(p, id, name); err != nil {
			return 0, err
		}
	}
	if name != "" && len(gone) == 0 {
		return 0, refuse("unknown-file", "thread %s has no library file %s", id, name)
	}
	for _, g := range gone {
		_ = thumb.Forget(p.Slug, id, g)
		if err := p.Journal(c, "library.rm", id, g); err != nil {
			return len(gone), err
		}
	}
	return len(gone), nil
}

// errNoneGone keeps an archive that removeArchived would not change.
var errNoneGone = errors.New("no library file to remove")

// removeArchived rewrites thread id's tarball without the library file
// name (every one when ""), returning the names it dropped. The new
// tarball replaces the old atomically, under the lock Archive takes.
func removeArchived(p *project.Project, id, name string) ([]string, error) {
	ap := archivePath(p, id)
	if _, err := os.Stat(ap); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, refuse("unknown-thread", "no thread %s in project %s (tm thread list --all)", id, p.Slug)
		}
		return nil, err
	}
	unlock, err := mdfile.Lock(p.Path("threads", "ids"))
	if err != nil {
		return nil, err
	}
	defer unlock()
	var gone []string
	err = fsx.Stream(ap, 0o644, func(w io.Writer) error {
		zw := gzip.NewWriter(w)
		tw := tar.NewWriter(zw)
		err := walkArchive(ap, func(h *tar.Header, r io.Reader) error {
			if n, ok := libraryName(id, h); ok && (name == "" || n == name) {
				gone = append(gone, n)
				return nil
			}
			if err := tw.WriteHeader(h); err != nil {
				return err
			}
			_, err := io.Copy(tw, r)
			return err
		})
		if err == nil {
			err = tw.Close()
		}
		if err == nil {
			err = zw.Close()
		}
		if err != nil {
			return fmt.Errorf("%s: %w", ap, err)
		}
		if len(gone) == 0 {
			return errNoneGone
		}
		return nil
	})
	if errors.Is(err, errNoneGone) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	archiveLibCache.Delete(ap)
	return gone, nil
}
