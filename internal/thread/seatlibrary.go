package thread

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/thumb"
)

// A seat's own library (docs/SPEC.md §7.2): the files a coordinator or a
// director keeps with tm library add, library/ of its project's or
// program's folder. Only tm writes there (project.Project.Owned,
// program.Program.Owned), and the id SeatID names them wherever a thread's
// id names a thread's files, so a project's listing, its reads, a paired
// server's pages and tm library rm carry them as they do a thread's. A
// file never carries a path, here either.

// SeatID is the library id of a seat's own files. No thread has it: a
// thread's id is t- and digits.
const SeatID = "seat"

// seatState is the state a seat's own file lists with.
const seatState = "kept"

// ValidLibraryID says whether id names a library of a project: a thread's,
// or its coordinator's own.
func ValidLibraryID(id string) bool { return id == SeatID || ValidID(id) }

// IsSeat says whether f is a seat's own file.
func (f LibFile) IsSeat() bool { return f.Thread == SeatID }

// SeatTitle is what a folder's own files are called in a list.
func SeatTitle(f *project.Folder) string {
	if f.IsProgram() {
		return "Director's files"
	}
	return "Coordinator's files"
}

// SeatLibrary lists the files in folder f's own library, newest first
// (ties by name).
func SeatLibrary(f *project.Folder) []LibFile {
	ents, err := os.ReadDir(f.Path("library"))
	if err != nil {
		return nil
	}
	var out []LibFile
	for _, e := range ents {
		if !e.Type().IsRegular() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, LibFile{Thread: SeatID, Title: SeatTitle(f), State: seatState, Name: e.Name(), Size: fi.Size(), Time: fi.ModTime()})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Time.Equal(out[j].Time) {
			return out[i].Time.After(out[j].Time)
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// AddSeatFile copies source into folder f's own library under its base
// name, as a report's attachment is copied, opening it with open (the
// caller's rules for which files it may name, cli.openArg), and journals
// it as library.add. A file of that name is replaced.
func AddSeatFile(f *project.Folder, c caller.Caller, source string, open func(string) (*os.File, error)) (LibFile, error) {
	name := filepath.Base(source)
	if !textsafe.Safe(name) {
		return LibFile{}, refuse("invalid-attachment", "%q: a library file's name is one line of plain text; rename it", name)
	}
	if err := checkLibName(name); err != nil {
		return LibFile{}, err
	}
	in, err := open(source)
	if err != nil {
		return LibFile{}, refuse("invalid-attachment", "%v", err)
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return LibFile{}, refuse("invalid-attachment", "%v", err)
	}
	if !fi.Mode().IsRegular() {
		return LibFile{}, refuse("invalid-attachment", "%s is not a regular file", source)
	}
	dst := f.Path("library", name)
	// Copying a library file onto itself would empty it.
	if have, err := os.Lstat(dst); err == nil && os.SameFile(fi, have) {
		return LibFile{}, refuse("invalid-attachment", "%s is already in the library", name)
	}
	if err := copyFile(in, dst); err != nil {
		return LibFile{}, err
	}
	out, err := os.Lstat(dst)
	if err != nil {
		return LibFile{}, err
	}
	_ = thumb.Forget(f.LinkSlug(), SeatID, name)
	if err := f.Journal(c, "library.add", SeatID, name); err != nil {
		return LibFile{}, err
	}
	return LibFile{Thread: SeatID, Title: SeatTitle(f), State: seatState, Name: name, Size: out.Size(), Time: out.ModTime()}, nil
}

// seatFile opens folder f's own library file, never through a link.
func seatFile(f *project.Folder, name string) (*os.File, fs.FileInfo, error) {
	if err := checkLibName(name); err != nil {
		return nil, nil, err
	}
	none := refuse("unknown-file", "%s has no library file %s", f.Slug, name)
	in, err := fsx.OpenNoLink(f.Path("library", name), os.O_RDONLY, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, none
	}
	if err != nil {
		return nil, nil, err
	}
	fi, err := in.Stat()
	if err != nil {
		in.Close()
		return nil, nil, err
	}
	if !fi.Mode().IsRegular() {
		in.Close()
		return nil, nil, none
	}
	return in, fi, nil
}

// ReadSeatChunk reads at most n bytes of folder f's own library file from
// off, and says the file's size and modification time.
func ReadSeatChunk(f *project.Folder, name string, off, n int64) (data []byte, size int64, mod time.Time, err error) {
	in, fi, err := seatFile(f, name)
	if err != nil {
		return nil, 0, time.Time{}, err
	}
	defer in.Close()
	data, err = readChunk(in, fi.Size(), off, n)
	return data, fi.Size(), fi.ModTime(), err
}

// SeatFileInfo says folder f's own library file's size and modification
// time.
func SeatFileInfo(f *project.Folder, name string) (int64, time.Time, error) {
	in, fi, err := seatFile(f, name)
	if err != nil {
		return 0, time.Time{}, err
	}
	in.Close()
	return fi.Size(), fi.ModTime(), nil
}

// RemoveSeatFiles deletes folder f's own library files: the named one, or
// every one when name is "". It journals each as library.rm and returns
// how many it removed.
func RemoveSeatFiles(f *project.Folder, c caller.Caller, name string) (int, error) {
	if name != "" {
		if err := checkLibName(name); err != nil {
			return 0, err
		}
	}
	gone := 0
	for _, g := range SeatLibrary(f) {
		if name != "" && g.Name != name {
			continue
		}
		if err := os.Remove(f.Path("library", g.Name)); err != nil {
			return gone, err
		}
		gone++
		_ = thumb.Forget(f.LinkSlug(), SeatID, g.Name)
		if err := f.Journal(c, "library.rm", SeatID, g.Name); err != nil {
			return gone, err
		}
	}
	if name != "" && gone == 0 {
		return 0, refuse("unknown-file", "%s has no library file %s", f.Slug, name)
	}
	return gone, nil
}
