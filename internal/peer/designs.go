package peer

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// A shared project's designs (docs/SPEC.md §16.17, T888), under the
// project pages switch: each design's record and build, read-only. A
// design's page and its open questions stay home.

// CapPageDesigns is the link capability of peer.pages.designs: a server
// that lists pages but not this one (a tm of T886) has no designs to
// read.
const CapPageDesigns = "page-designs"

// DesignsExporter is a PagesExporter that also lists a shared project's
// designs.
type DesignsExporter interface {
	PagesExporter
	// Designs are the project's designs, by name.
	Designs(c caller.Caller, project string) (proto.PeerDesigns, error)
}

// designsMethods are the link's method of designs, when the Exporter
// serves them.
func (d *dispatcher) designsMethods(m map[string]handler) {
	if _, ok := d.ex.(DesignsExporter); !ok || d.pages == nil {
		return
	}
	m[proto.MethodPeerPagesDesigns] = d.pagesDesigns
}

func (d *dispatcher) pagesDesigns(_ context.Context, raw json.RawMessage) (any, error) {
	project, err := d.pagesProject(raw, proto.MethodPeerPagesDesigns)
	if err != nil {
		return nil, err
	}
	return d.ex.(DesignsExporter).Designs(d.caller, project)
}

// ErrNoDesigns means the peer's tm shares project pages but no designs
// (its hello lists CapPages, not CapPageDesigns): the console says "not
// shared" all the same.
var ErrNoDesigns = proto.Errorf(proto.ErrNoPages, "the paired server's tm shares no designs")

// Designs lists the designs of the paired server alias's shared project
// slug, cleaned (cleanDesigns): ErrNoPages for a tm without pages,
// ErrNoDesigns for one without designs, else peer.pages.designs's error.
func (m *Manager) Designs(ctx context.Context, alias, slug string) (proto.PeerDesigns, error) {
	l, err := m.pagesLink(alias)
	if err != nil {
		return proto.PeerDesigns{}, err
	}
	if st := l.status(); st.State == proto.LinkUp && !slices.Contains(st.Caps, CapPageDesigns) {
		return proto.PeerDesigns{}, ErrNoDesigns
	}
	ctx, cancel := context.WithTimeout(ctx, pagesWait)
	defer cancel()
	var ds proto.PeerDesigns
	if err := l.call(ctx, proto.MethodPeerPagesDesigns, proto.PeerPagesParams{Project: slug}, &ds); err != nil {
		return proto.PeerDesigns{}, err
	}
	return cleanDesigns(ds), nil
}

// cleanDesigns is a peer.pages.designs answer with every string cleaned
// and on one line, names and ids checked (a design whose name isn't one
// is dropped, a build task whose id isn't one too), a page never
// carried, and each list cut to its most.
func cleanDesigns(in proto.PeerDesigns) proto.PeerDesigns {
	cleanStrings(&in)
	out := proto.PeerDesigns{Designs: make([]proto.PeerDesign, 0, min(len(in.Designs), proto.PeerDesignsMax)), Truncated: in.Truncated}
	seen := map[string]bool{}
	for _, d := range in.Designs {
		if len(out.Designs) == proto.PeerDesignsMax {
			out.Truncated = true
			break
		}
		if !tasks.ValidDesign(d.Name) || len(d.Name) > 64 || seen[d.Name] {
			continue
		}
		seen[d.Name] = true
		cd := proto.PeerDesign{Name: d.Name, Title: textsafe.Line(d.Title, proto.PeerDesignTitle), Status: word(peerWord, d.Status),
			Task: taskRef(d.Task), TaskStatus: word(peerWord, d.TaskStatus), Page: d.Page, TasksDone: max(d.TasksDone, 0),
			Answers: []string{}, Build: []proto.PeerDesignTask{}, Archived: d.Archived}
		if cd.Title == "" {
			cd.Title = cd.Name
		}
		for _, a := range d.Answers {
			if len(cd.Answers) == proto.PeerDesignAnswers {
				break
			}
			if a = textsafe.Line(a, proto.PeerDesignAnswer); a != "" {
				cd.Answers = append(cd.Answers, a)
			}
		}
		for _, t := range d.Build {
			if len(cd.Build) == proto.PeerDesignBuild {
				break
			}
			if !tasks.ValidRef(t.ID) {
				continue
			}
			ct := proto.PeerDesignTask{ID: t.ID, Title: textsafe.Line(t.Title, proto.PeerPagesTitle), Status: word(peerWord, t.Status),
				Archived: t.Archived, Working: t.Working && !t.Archived}
			for _, a := range t.After {
				if len(ct.After) == proto.PeerDesignAfterMax {
					break
				}
				if tasks.ValidRef(a) {
					ct.After = append(ct.After, a)
				}
			}
			cd.Working = cd.Working || ct.Working
			cd.Build = append(cd.Build, ct)
		}
		out.Designs = append(out.Designs, cd)
	}
	return out
}

// taskRef is s when it is a task's id ("T12"), else "".
func taskRef(s string) string {
	if tasks.ValidRef(s) {
		return s
	}
	return ""
}
