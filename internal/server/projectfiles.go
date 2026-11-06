package server

import (
	"errors"
	"slices"
	"time"

	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/thumb"
)

// A project's memory and library for a console off this machine's files
// (proto.CapProjectFiles): what the project popup's Memory and Library
// tabs read, by the same functions, so a path the project doesn't list
// never goes in or out.

func openProject(slug string) (*project.Project, *proto.Error) {
	p, err := project.Open(slug)
	if err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	return p, nil
}

func (s *Server) memoryList(pp proto.ProjectFilesParams) (any, *proto.Error) {
	p, perr := openProject(pp.Project)
	if perr != nil {
		return nil, perr
	}
	files, err := p.MemoryFiles()
	if err != nil {
		return nil, proto.Errorf(proto.ErrInternal, "%v", err)
	}
	out := proto.MemoryListResult{Files: []proto.MemoryFile{}}
	for _, f := range files {
		out.Files = append(out.Files, proto.MemoryFile{Path: f.Path, Title: f.Title, Bytes: f.Bytes, Touches: f.Touches})
	}
	return out, nil
}

func (s *Server) memoryRead(pp proto.MemoryReadParams) (any, *proto.Error) {
	p, perr := openProject(pp.Project)
	if perr != nil {
		return nil, perr
	}
	text, err := p.ReadMemoryFile(pp.Path)
	switch {
	case errors.Is(err, project.ErrNotMemory):
		return nil, proto.Errorf("not-memory", "%s: %v", pp.Path, err)
	case err != nil:
		return nil, proto.Errorf(proto.ErrRefused, "%v", err)
	}
	return proto.MemoryReadResult{Path: pp.Path, Text: text}, nil
}

// libraryList is the seat's and the threads' library files and the files
// the project's links brought, newest first; for a program, its director's
// and its members' (programlibrary.go).
func (s *Server) libraryList(pp proto.ProjectFilesParams) (any, *proto.Error) {
	if pp.Program != "" {
		return s.programLibrary(pp)
	}
	p, perr := openProject(pp.Project)
	if perr != nil {
		return nil, perr
	}
	files, err := projectLibrary(p)
	if err != nil {
		return nil, proto.Errorf(proto.ErrInternal, "%v", err)
	}
	out := proto.LibraryListResult{Files: []proto.LibraryFile{}}
	for _, f := range files {
		out.Files = append(out.Files, libraryFileOf(f))
	}
	return out, nil
}

// projectLibrary is project p's whole library, newest first.
func projectLibrary(p *project.Project) ([]thread.LibFile, error) {
	files, err := thread.Library(p)
	if err != nil {
		return nil, err
	}
	files = append(files, link.LibraryFiles(p)...)
	slices.SortStableFunc(files, func(a, b thread.LibFile) int { return b.Time.Compare(a.Time) })
	return files, nil
}

func libraryFileOf(f thread.LibFile) proto.LibraryFile {
	return proto.LibraryFile{Thread: f.Thread, Task: f.Task, TaskStatus: f.TaskStatus, Title: f.Title, Repo: f.Repo, State: f.State,
		Name: f.Name, DocTitle: f.DocTitle, Published: f.Published, Size: f.Size, Time: f.Time, Peer: f.Peer, Message: f.Message, Project: f.Project, Server: f.Server}
}

// libraryRead is a chunk of a thread's library file, or, for ID a
// message's, of the file it brought.
func (s *Server) libraryRead(pp proto.LibraryReadParams) (any, *proto.Error) {
	if pp.Offset < 0 {
		return nil, proto.Errorf(proto.ErrBadParams, "library.read: offset %d", pp.Offset)
	}
	if pp.Program != "" {
		return s.programLibraryRead(pp)
	}
	p, perr := openProject(pp.Project)
	if perr != nil {
		return nil, perr
	}
	read := thread.ReadLibraryChunk
	if link.ValidID(pp.ID) {
		read = link.ReadFileChunk
	}
	data, size, mod, err := read(p, pp.ID, pp.Name, pp.Offset, proto.LibraryChunk)
	if err != nil {
		return nil, taskError(err)
	}
	if size > proto.MaxLibraryRead {
		return nil, proto.Errorf("too-large", "%s is %d bytes, over the %d the app reads", pp.Name, size, proto.MaxLibraryRead)
	}
	return proto.LibraryReadResult{Name: pp.Name, Size: size, MTime: mod, Offset: pp.Offset, Data: data}, nil
}

// libraryThumbnail is library.thumbnail: for ID a message, of the file it
// brought, else of a thread's library file, packed or not.
func (s *Server) libraryThumbnail(pp proto.LibraryThumbnailParams) (any, *proto.Error) {
	if pp.Program != "" {
		return s.programLibraryThumbnail(pp)
	}
	p, perr := openProject(pp.Project)
	if perr != nil {
		return nil, perr
	}
	info, read := thread.LibraryFileInfo, thread.ReadLibraryChunk
	if link.ValidID(pp.ID) {
		read = link.ReadFileChunk
		info = func(p *project.Project, id, name string) (int64, time.Time, error) {
			_, size, mod, err := link.ReadFileChunk(p, id, name, 0, 0)
			return size, mod, err
		}
	}
	t, err := thumbnailOf(p, pp.ID, pp.Name, info, read)
	if err != nil {
		return nil, thumbnailError(pp.Name, err)
	}
	return thumbnailResult(pp.Name, t), nil
}

// thumbnailOf is a library file's thumbnail: cached, without reading the
// file, else made from it.
func thumbnailOf(p *project.Project, id, name string,
	info func(*project.Project, string, string) (int64, time.Time, error),
	read func(*project.Project, string, string, int64, int64) ([]byte, int64, time.Time, error)) (thumb.Thumb, error) {
	if !thumb.Supported(name) {
		return thumb.Thumb{}, thumb.ErrNoThumbnail
	}
	size, mod, err := info(p, id, name)
	if err != nil {
		return thumb.Thumb{}, err
	}
	return thumb.Get(thumb.Key{Project: p.Slug, ID: id, Name: name}, size, mod, func() ([]byte, error) {
		data, _, _, err := read(p, id, name, 0, thumb.MaxSource)
		return data, err
	})
}

func thumbnailResult(name string, t thumb.Thumb) proto.LibraryThumbnailResult {
	return proto.LibraryThumbnailResult{Name: name, Width: t.Width, Height: t.Height, Data: t.Data}
}

// thumbnailError is a thumbnail refused, with the thread library's codes
// for a bad name or thread.
func thumbnailError(name string, err error) *proto.Error {
	switch {
	case errors.Is(err, thumb.ErrNoThumbnail):
		return proto.Errorf(proto.ErrNoThumbnail, "%s has no thumbnail", name)
	case errors.Is(err, thumb.ErrBusy):
		return proto.Errorf(proto.ErrRefused, "%v; try again", err)
	}
	return taskError(err)
}
