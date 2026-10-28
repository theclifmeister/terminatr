package link

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// The Library tab's groups of files from linked projects (docs/SPEC.md
// §16.13): each file a linked project sent and p received, beside its
// message. Listed, read and deleted like a thread's library files, by
// the message's id and the name.

// LibraryFiles are the files p received over its links, newest first.
func LibraryFiles(p *project.Project) []thread.LibFile {
	var out []thread.LibFile
	for _, other := range Peers(p) {
		msgs, _ := Log(p, other)
		for _, m := range msgs {
			if m.Out || m.File == "" {
				continue
			}
			fi, err := os.Lstat(filepath.Join(fileDir(p, other, m.ID), m.File))
			if err != nil || !fi.Mode().IsRegular() {
				continue
			}
			out = append(out, thread.LibFile{Peer: other, Message: m.ID, Title: "from " + other, State: FileReceived,
				Name: m.File, Size: fi.Size(), Time: fi.ModTime()})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out
}

// received is the path of the file name that came with p's message id.
func received(p *project.Project, id, name string) (string, error) {
	m, err := Read(p, id)
	if err != nil {
		return "", err
	}
	if m.Out || m.File == "" || m.File != name {
		return "", refuse("unknown-file", "%s brought no file %s", id, name)
	}
	return FilePath(p, m, name)
}

// ReadFile reads up to max bytes of the file name that came with p's
// message id; truncated says it was longer.
func ReadFile(p *project.Project, id, name string, max int64) (data []byte, truncated bool, err error) {
	path, err := received(p, id, name)
	if err != nil {
		return nil, false, err
	}
	f, err := fsx.OpenNoLink(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	data, err = io.ReadAll(io.LimitReader(f, max+1))
	if int64(len(data)) > max {
		return data[:max], true, err
	}
	return data, false, err
}

// ReadFileChunk reads at most n bytes from off of the file name that
// came with p's message id, never through a link, and says its size and
// modification time.
func ReadFileChunk(p *project.Project, id, name string, off, n int64) (data []byte, size int64, mod time.Time, err error) {
	path, err := received(p, id, name)
	if err != nil {
		return nil, 0, time.Time{}, err
	}
	f, err := fsx.OpenNoLink(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, 0, time.Time{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, 0, time.Time{}, err
	}
	if !fi.Mode().IsRegular() {
		return nil, 0, time.Time{}, refuse("unknown-file", "%s brought no file %s", id, name)
	}
	if off < 0 || off > fi.Size() {
		return nil, 0, time.Time{}, fmt.Errorf("offset %d is past the end (%d)", off, fi.Size())
	}
	data = make([]byte, min(fi.Size()-off, n))
	if _, err := f.ReadAt(data, off); err != nil && !errors.Is(err, io.EOF) {
		return nil, 0, time.Time{}, err
	}
	return data, fi.Size(), fi.ModTime(), nil
}

// RemoveFile deletes the file name that came with p's message id, as
// the human or the coordinator on the user's word (as tm library rm);
// journaled as library.rm.
func RemoveFile(p *project.Project, c caller.Caller, id, name string) error {
	path, err := received(p, id, name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	os.Remove(filepath.Dir(path)) // when it is empty
	return p.Journal(c, "library.rm", id, name)
}
