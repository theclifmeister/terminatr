package project

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// noteTitle is a note's first heading, else name.
func noteTitle(text, name string) string {
	for _, l := range strings.Split(text, "\n") {
		if h, ok := strings.CutPrefix(strings.TrimSpace(l), "#"); ok {
			if h = strings.TrimSpace(strings.TrimLeft(h, "#")); h != "" {
				return h
			}
		}
	}
	return name
}

// MemoryFile is one file of the project's memory, as the Memory tab
// lists it: its path in the project folder (CONTEXT.md, MEMORY.md or
// memory/<name>.md), its title and its size.
type MemoryFile struct {
	Path  string `json:"path"`
	Title string `json:"title"`
	Bytes int64  `json:"bytes"`
}

// MaxMemoryRead is the largest memory file ReadMemoryFile reads.
const MaxMemoryRead = 1 << 20

// MemoryFiles lists the project's memory files: CONTEXT.md and MEMORY.md
// when they are there, then the notes by title. Only regular files
// count, never a link, so a link can't show a file from elsewhere.
func (p *Project) MemoryFiles() ([]MemoryFile, error) {
	var out []MemoryFile
	for _, f := range []struct{ name, title string }{{"CONTEXT.md", "Context"}, {"MEMORY.md", "Memory"}} {
		fi, err := os.Lstat(p.Path(f.name))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return nil, err
		case fi.Mode().IsRegular():
			out = append(out, MemoryFile{Path: f.name, Title: f.title, Bytes: fi.Size()})
		}
	}
	ents, err := os.ReadDir(p.Path("memory"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	var notes []MemoryFile
	for _, e := range ents {
		name, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok || !e.Type().IsRegular() {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue // gone since
		}
		title := name
		if fi.Size() <= MaxMemoryRead {
			if data, err := os.ReadFile(p.Path("memory", e.Name())); err == nil {
				title = noteTitle(string(data), name)
			}
		}
		notes = append(notes, MemoryFile{Path: "memory/" + e.Name(), Title: title, Bytes: fi.Size()})
	}
	slices.SortFunc(notes, func(a, b MemoryFile) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)), cmp.Compare(a.Path, b.Path))
	})
	return append(out, notes...), nil
}

// ErrNotMemory is ReadMemoryFile's error for a path that isn't one of
// MemoryFiles'.
var ErrNotMemory = errors.New("not a memory file of the project")

// ReadMemoryFile reads the memory file at path, one MemoryFiles lists;
// any other path is ErrNotMemory. A file over MaxMemoryRead is refused.
func (p *Project) ReadMemoryFile(path string) (string, error) {
	files, err := p.MemoryFiles()
	if err != nil {
		return "", err
	}
	i := slices.IndexFunc(files, func(f MemoryFile) bool { return f.Path == path })
	if i < 0 {
		return "", ErrNotMemory
	}
	if files[i].Bytes > MaxMemoryRead {
		return "", fmt.Errorf("%s is %d bytes, over the %d a mod may read", path, files[i].Bytes, MaxMemoryRead)
	}
	data, err := os.ReadFile(p.Path(filepath.FromSlash(path)))
	return string(data), err
}
