package server

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/features"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/thumb"
)

// A program's library (docs/SPEC.md §7.2): what the director's Files view
// lists. The director's own files first, then each member's in the
// program's order, a paired member's only where that project's pages
// switch is on over there. A member's file is read as any file of its
// project is (library.read, peer.library-read); only the director's own
// need a program form, which names no path either. All of it stays off
// while programs are.

// openLibraryProgram is program slug, while programs are on.
func (s *Server) openLibraryProgram(slug string) (*program.Program, *proto.Error) {
	cfg := s.cfgNow()
	if !features.Programs(cfg) {
		return nil, proto.Errorf(features.OffCode, "%s", features.OffMsg)
	}
	g, err := program.Open(slug, cfg)
	if err != nil {
		return nil, taskError(err)
	}
	return g, nil
}

// LocalProgramLibrary is the director's own files and those of g's members
// on this server, and how many members are on paired servers.
func LocalProgramLibrary(g *program.Program) (files []thread.LibFile, paired int) {
	files = thread.SeatLibrary(&g.Folder)
	for _, m := range g.Members {
		if _, _, remote := config.Remote(m); remote {
			paired++
			continue
		}
		files = append(files, memberLibrary(m)...)
	}
	return files, paired
}

// memberLibrary is the files of member project slug on this server, none
// when it can't be read now.
func memberLibrary(slug string) []thread.LibFile {
	p, err := project.Open(slug)
	if err != nil {
		return nil
	}
	files, err := projectLibrary(p)
	if err != nil {
		return nil
	}
	for i := range files {
		files[i].Project = slug
	}
	return files
}

// programLibrary is library.list's program form: the members' answers in
// the program's order, the paired ones asked side by side. A paired member
// whose pages aren't shared stays out with no word; one that can't be read
// now says so in the Note.
func (s *Server) programLibrary(pp proto.ProjectFilesParams) (any, *proto.Error) {
	if pp.Project != "" {
		return nil, proto.Errorf(proto.ErrBadParams, "library.list names a project or a program, not both")
	}
	g, perr := s.openLibraryProgram(pp.Program)
	if perr != nil {
		return nil, perr
	}
	parts := make([][]thread.LibFile, len(g.Members))
	unread := make([]string, len(g.Members))
	var wg sync.WaitGroup
	for i, m := range g.Members {
		alias, slug, remote := config.Remote(m)
		if !remote {
			parts[i] = memberLibrary(m)
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			parts[i], unread[i] = s.pairedLibrary(alias, slug)
		}()
	}
	wg.Wait()
	files := thread.SeatLibrary(&g.Folder)
	for _, part := range parts {
		files = append(files, part...)
	}
	var notes []string
	if u := slices.DeleteFunc(unread, func(s string) bool { return s == "" }); len(u) > 0 {
		notes = append(notes, strings.Join(u, ", ")+" can't be read now")
	}
	if len(files) > proto.MaxProgramLibrary {
		files = newestFiles(files, proto.MaxProgramLibrary)
		notes = append(notes, fmt.Sprintf("the newest %d files show", proto.MaxProgramLibrary))
	}
	out := proto.LibraryListResult{Files: make([]proto.LibraryFile, 0, len(files)), Note: strings.Join(notes, "; ")}
	for _, f := range files {
		out.Files = append(out.Files, libraryFileOf(f))
	}
	return out, nil
}

// newestFiles is files without all but the newest n, in their order.
func newestFiles(files []thread.LibFile, n int) []thread.LibFile {
	byAge := slices.Clone(files)
	slices.SortStableFunc(byAge, func(a, b thread.LibFile) int { return b.Time.Compare(a.Time) })
	oldest := byAge[n-1].Time
	kept := 0
	return slices.DeleteFunc(slices.Clone(files), func(f thread.LibFile) bool {
		if f.Time.Before(oldest) || kept == n {
			return true
		}
		kept++
		return false
	})
}

// pairedLibrary is the files of project slug of the paired server alias,
// none where its pages aren't shared with this server; unread names the
// member when its server doesn't answer now.
func (s *Server) pairedLibrary(alias, slug string) (files []thread.LibFile, unread string) {
	lib, err := s.peers.Library(context.Background(), alias, slug)
	if err != nil {
		switch pagesError(alias, err).Code {
		case proto.ErrPagesOff, proto.ErrNoPages, proto.ErrNotShared:
			return nil, ""
		}
		return nil, slug + " on " + alias
	}
	for _, f := range lib.Files {
		files = append(files, thread.LibFile{Thread: f.Thread, Task: f.Task, Title: f.Title, State: f.State,
			Name: f.Name, Size: f.Size, Time: f.Time, Project: slug, Server: alias})
	}
	return files, ""
}

// checkProgramFile refuses a program form that names anything but the
// director's own file.
func checkProgramFile(method, projectName, id string) *proto.Error {
	if projectName != "" || id != thread.SeatID {
		return proto.Errorf(proto.ErrBadParams, "%s of a program names its director's own file: id %s, no project", method, thread.SeatID)
	}
	return nil
}

// programLibraryRead is library.read of a director's own file.
func (s *Server) programLibraryRead(pp proto.LibraryReadParams) (any, *proto.Error) {
	if perr := checkProgramFile(proto.MethodLibraryRead, pp.Project, pp.ID); perr != nil {
		return nil, perr
	}
	g, perr := s.openLibraryProgram(pp.Program)
	if perr != nil {
		return nil, perr
	}
	data, size, mod, err := thread.ReadSeatChunk(&g.Folder, pp.Name, pp.Offset, proto.LibraryChunk)
	if err != nil {
		return nil, taskError(err)
	}
	if size > proto.MaxLibraryRead {
		return nil, proto.Errorf("too-large", "%s is %d bytes, over the %d the app reads", pp.Name, size, proto.MaxLibraryRead)
	}
	return proto.LibraryReadResult{Name: pp.Name, Size: size, MTime: mod, Offset: pp.Offset, Data: data}, nil
}

// programLibraryThumbnail is library.thumbnail of a director's own file.
func (s *Server) programLibraryThumbnail(pp proto.LibraryThumbnailParams) (any, *proto.Error) {
	if perr := checkProgramFile(proto.MethodLibraryThumbnail, pp.Project, pp.ID); perr != nil {
		return nil, perr
	}
	g, perr := s.openLibraryProgram(pp.Program)
	if perr != nil {
		return nil, perr
	}
	t, err := programThumbnail(&g.Folder, pp.Name)
	if err != nil {
		return nil, thumbnailError(pp.Name, err)
	}
	return thumbnailResult(pp.Name, t), nil
}

func programThumbnail(f *project.Folder, name string) (thumb.Thumb, error) {
	if !thumb.Supported(name) {
		return thumb.Thumb{}, thumb.ErrNoThumbnail
	}
	size, mod, err := thread.SeatFileInfo(f, name)
	if err != nil {
		return thumb.Thumb{}, err
	}
	return thumb.Get(thumb.Key{Project: f.LinkSlug(), ID: thread.SeatID, Name: name}, size, mod, func() ([]byte, error) {
		data, _, _, err := thread.ReadSeatChunk(f, name, 0, thumb.MaxSource)
		return data, err
	})
}
