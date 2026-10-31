package peer

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// A shared project's pages (docs/SPEC.md §16.17): its board, overview
// and library, for a paired server whose project pages switch here is on
// for the project. Read-only at every level: nothing here writes, and
// the memory has no method at all.

// CapPages is the link capability of peer.pages, peer.pages.library and
// peer.pages.library.read.
const CapPages = "pages"

// PagesExporter is an Exporter that also serves a shared project's
// pages. The link checked the pages switch already; each answers only
// for a shared project, as Watch.
type PagesExporter interface {
	// Pages are the project's board and overview.
	Pages(c caller.Caller, project string) (proto.PeerPages, error)
	// Library is the project's library: its threads' files only.
	Library(c caller.Caller, project string) (proto.PeerLibrary, error)
	// LibraryRead is a chunk of at most proto.PeerLibraryChunk bytes of
	// a file Library lists; any other is refused.
	LibraryRead(c caller.Caller, p proto.PeerLibraryReadParams) (proto.LibraryReadResult, error)
}

// pagesMethods are the link's methods of project pages, when the
// Exporter serves them.
func (d *dispatcher) pagesMethods(m map[string]handler) {
	if _, ok := d.ex.(PagesExporter); !ok || d.pages == nil {
		return
	}
	m[proto.MethodPeerPages] = d.pagesRead
	m[proto.MethodPeerPagesLibrary] = d.pagesLibrary
	m[proto.MethodPeerPagesLibraryRead] = d.pagesLibraryRead
}

// errPagesOff refuses a page while the switch is off for the project:
// the same words whether the project is shared or not.
func errPagesOff(project string) error {
	return proto.Errorf(proto.ErrPagesOff, "this server doesn't share %q's pages with yours", project)
}

// pagesProject is the project of a pages request, once the switch is on
// for it.
func (d *dispatcher) pagesProject(raw json.RawMessage, method string) (string, error) {
	var p proto.PeerPagesParams
	if err := json.Unmarshal(raw, &p); err != nil || p.Project == "" {
		return "", proto.Errorf(proto.ErrBadParams, "%s needs a project", method)
	}
	if !d.pages(p.Project) {
		return "", errPagesOff(p.Project)
	}
	return p.Project, nil
}

func (d *dispatcher) pagesRead(_ context.Context, raw json.RawMessage) (any, error) {
	project, err := d.pagesProject(raw, proto.MethodPeerPages)
	if err != nil {
		return nil, err
	}
	pp, err := d.ex.(PagesExporter).Pages(d.caller, project)
	if err != nil {
		return nil, err
	}
	pp.Now = time.Now()
	return pp, nil
}

func (d *dispatcher) pagesLibrary(_ context.Context, raw json.RawMessage) (any, error) {
	project, err := d.pagesProject(raw, proto.MethodPeerPagesLibrary)
	if err != nil {
		return nil, err
	}
	return d.ex.(PagesExporter).Library(d.caller, project)
}

func (d *dispatcher) pagesLibraryRead(_ context.Context, raw json.RawMessage) (any, error) {
	var p proto.PeerLibraryReadParams
	if err := json.Unmarshal(raw, &p); err != nil || p.Project == "" || p.Thread == "" || p.Name == "" {
		return nil, proto.Errorf(proto.ErrBadParams, "peer.pages.library.read needs a project, a thread and a name")
	}
	if p.Offset < 0 {
		return nil, proto.Errorf(proto.ErrBadParams, "peer.pages.library.read: offset %d", p.Offset)
	}
	if !d.pages(p.Project) {
		return nil, errPagesOff(p.Project)
	}
	return d.ex.(PagesExporter).LibraryRead(d.caller, p)
}

// pagesOn is this server's project pages switch for the peer now, for
// project.
func (l *link) pagesOn(project string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.peer.PagesOn(project)
}

// pagesList is the switch's projects now.
func (l *link) pagesList() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.peer.Pages)
}

// setPages changes the switch: the next request reads it, and the peer's
// next peer.projects says it.
func (l *link) setPages(pages []string) {
	l.mu.Lock()
	l.peer.Pages = slices.Clone(pages)
	l.mu.Unlock()
}

// pagesWait bounds a pages read: a screen waits for it.
const pagesWait = 15 * time.Second

// ErrNoPages means the peer's tm serves no project pages (its hello
// doesn't list CapPages).
var ErrNoPages = proto.Errorf(proto.ErrNoPages, "the paired server's tm shares no project pages")

// pagesLink is the up link to alias that serves pages: ErrNoPages for a
// tm without them.
func (m *Manager) pagesLink(alias string) (*link, error) {
	if m == nil {
		return nil, errNotUp
	}
	l := m.link(alias)
	if l == nil {
		return nil, proto.Errorf(proto.ErrRefused, "no paired server %q", alias)
	}
	if st := l.status(); st.State == proto.LinkUp && !slices.Contains(st.Caps, CapPages) {
		return nil, ErrNoPages
	}
	return l, nil
}

// Pages reads the board and overview of the paired server alias's shared
// project slug, cleaned (cleanPages): ErrNoPages when that server's tm
// has none, else peer.pages's error (proto.ErrPagesOff,
// proto.ErrNotShared …).
func (m *Manager) Pages(ctx context.Context, alias, slug string) (proto.PeerPages, error) {
	l, err := m.pagesLink(alias)
	if err != nil {
		return proto.PeerPages{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, pagesWait)
	defer cancel()
	var pp proto.PeerPages
	if err := l.call(ctx, proto.MethodPeerPages, proto.PeerPagesParams{Project: slug}, &pp); err != nil {
		return proto.PeerPages{}, err
	}
	return cleanPages(pp), nil
}

// Library lists the library of the paired server alias's shared project
// slug, cleaned (cleanLibrary).
func (m *Manager) Library(ctx context.Context, alias, slug string) (proto.PeerLibrary, error) {
	l, err := m.pagesLink(alias)
	if err != nil {
		return proto.PeerLibrary{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, pagesWait)
	defer cancel()
	var lib proto.PeerLibrary
	if err := l.call(ctx, proto.MethodPeerPagesLibrary, proto.PeerPagesParams{Project: slug}, &lib); err != nil {
		return proto.PeerLibrary{}, err
	}
	return cleanLibrary(lib), nil
}

// LibraryRead reads a chunk of a library file of the paired server
// alias's shared project: its bytes as they came, its name and numbers
// checked (a chunk over proto.PeerLibraryChunk, or a file over
// proto.PeerLibraryMaxRead, is refused).
func (m *Manager) LibraryRead(ctx context.Context, alias string, p proto.PeerLibraryReadParams) (proto.LibraryReadResult, error) {
	l, err := m.pagesLink(alias)
	if err != nil {
		return proto.LibraryReadResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, pagesWait)
	defer cancel()
	var r proto.LibraryReadResult
	if err := l.call(ctx, proto.MethodPeerPagesLibraryRead, p, &r); err != nil {
		return proto.LibraryReadResult{}, err
	}
	if len(r.Data) > proto.PeerLibraryChunk || r.Size < 0 || r.Size > proto.PeerLibraryMaxRead || r.Offset != p.Offset ||
		r.Offset+int64(len(r.Data)) > r.Size {
		return proto.LibraryReadResult{}, proto.Errorf(proto.ErrRefused, "the paired server's chunk of %s doesn't add up", textsafe.Line(p.Name, 80))
	}
	r.Name = p.Name
	return r, nil
}

// cleanPages is a peer.pages answer with every string cleaned, a task's
// title and steps on one line each, ids and words checked (a task whose
// id isn't one is dropped, a thread's is blanked), the body cut to
// proto.PeerPagesMaxBody and each list to its most.
func cleanPages(p proto.PeerPages) proto.PeerPages {
	body := p.Overview.Body
	cleanStrings(&p)
	p.Project = word(peerSlug, p.Project)
	p.Overview.Goal = textsafe.Line(p.Overview.Goal, 200)
	p.Overview.Body = textsafe.Cut(textsafe.Clean(body, true), proto.PeerPagesMaxBody)
	p.Overview.Repos = max(p.Overview.Repos, 0)
	ts := make([]proto.PeerPageTask, 0, min(len(p.Tasks), proto.PeerPagesMaxTasks))
	for _, t := range p.Tasks {
		if len(ts) == proto.PeerPagesMaxTasks {
			break
		}
		if t.ID <= 0 {
			continue
		}
		t.Title, t.Status = textsafe.Line(t.Title, proto.PeerPagesTitle), word(peerWord, t.Status)
		t.Thread = word(peerThread, t.Thread)
		t.Created, t.Updated = textsafe.Line(t.Created, 40), textsafe.Line(t.Updated, 40)
		if len(t.Steps) > proto.PeerPagesMaxSteps {
			t.Steps = t.Steps[:proto.PeerPagesMaxSteps]
		}
		steps := make([]proto.PeerStep, 0, len(t.Steps))
		for _, s := range t.Steps {
			steps = append(steps, proto.PeerStep{Text: textsafe.Line(s.Text, proto.PeerPagesStep), Done: s.Done})
		}
		t.Steps = steps
		ts = append(ts, t)
	}
	if len(ts) < len(p.Tasks) {
		p.Truncated = true
	}
	p.Tasks = ts
	return p
}

// cleanLibrary is a peer.pages.library answer with every string
// cleaned, names on one line, a file whose thread isn't one dropped, and
// the list cut to its most.
func cleanLibrary(lib proto.PeerLibrary) proto.PeerLibrary {
	cleanStrings(&lib)
	fs := make([]proto.PeerLibraryFile, 0, min(len(lib.Files), proto.PeerPagesMaxFiles))
	for _, f := range lib.Files {
		if len(fs) == proto.PeerPagesMaxFiles {
			break
		}
		if !peerThread.MatchString(f.Thread) || f.Name == "" {
			continue
		}
		f.Name = textsafe.Line(f.Name, 200)
		f.Title = textsafe.Line(f.Title, 200)
		f.Task, f.State = word(peerWord, f.Task), word(peerWord, f.State)
		f.Size = max(f.Size, 0)
		fs = append(fs, f)
	}
	lib.Files = fs
	return lib
}
