package server

import (
	"errors"
	"slices"

	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
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

// libraryList is the threads' library files and the files the
// project's links brought, newest first.
func (s *Server) libraryList(pp proto.ProjectFilesParams) (any, *proto.Error) {
	p, perr := openProject(pp.Project)
	if perr != nil {
		return nil, perr
	}
	files, err := thread.Library(p)
	if err != nil {
		return nil, proto.Errorf(proto.ErrInternal, "%v", err)
	}
	files = append(files, link.LibraryFiles(p)...)
	slices.SortStableFunc(files, func(a, b thread.LibFile) int { return b.Time.Compare(a.Time) })
	out := proto.LibraryListResult{Files: []proto.LibraryFile{}}
	for _, f := range files {
		out.Files = append(out.Files, proto.LibraryFile{Thread: f.Thread, Task: f.Task, Title: f.Title, State: f.State,
			Name: f.Name, Size: f.Size, Time: f.Time, Peer: f.Peer, Message: f.Message})
	}
	return out, nil
}

// libraryRead is a chunk of a thread's library file, or, for ID a
// message's, of the file it brought.
func (s *Server) libraryRead(pp proto.LibraryReadParams) (any, *proto.Error) {
	p, perr := openProject(pp.Project)
	if perr != nil {
		return nil, perr
	}
	if pp.Offset < 0 {
		return nil, proto.Errorf(proto.ErrBadParams, "library.read: offset %d", pp.Offset)
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
