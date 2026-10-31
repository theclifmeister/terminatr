package server

// A shared project's designs, for a paired server whose project pages
// switch here is on for it (docs/SPEC.md §16.17, T888): each design's
// record and build, read-only. Copied field by field from this server's
// own, so a field added to a design stays home until it is added here
// (peerdesigns_test.go fails until it is decided). A design's page (an
// https URL to a third server) and its design task's open questions
// stay home.

import (
	"cmp"
	"context"
	"slices"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

func (e peerExport) Designs(c caller.Caller, slug string) (proto.PeerDesigns, error) {
	p, err := sharedProject(c, slug, proto.MethodPeerPagesDesigns)
	if err != nil {
		return proto.PeerDesigns{}, err
	}
	out := proto.PeerDesigns{Designs: []proto.PeerDesign{}}
	builds, err := p.BoardDesigns()
	if err != nil {
		return out, nil
	}
	var sessions []proto.SessionInfo
	if e.sessions != nil {
		sessions = e.sessions()
	}
	out.Designs, out.Truncated = peerDesigns(builds, designWorking(slug, sessions), proto.PeerPagesBudget)
	return out, nil
}

// peerDesigns are builds as a paired server may see them, by name, as
// many as fit budget bytes of text: done and archived ones go first
// when they don't all fit. truncated says some were left out.
func peerDesigns(builds []project.DesignBuild, working map[string]bool, budget int) (out []proto.PeerDesign, truncated bool) {
	var live, closed []proto.PeerDesign
	for _, b := range builds {
		d := peerDesignOf(b, working)
		if d.Archived || d.Status == project.DesignDone {
			closed = append(closed, d)
		} else {
			live = append(live, d)
		}
	}
	keep := map[string]bool{}
	for _, d := range slices.Concat(live, closed) {
		n := designBytes(d)
		if len(keep) == proto.PeerDesignsMax || n > budget {
			truncated = true
			continue
		}
		budget -= n
		keep[d.Name] = true
	}
	out = []proto.PeerDesign{}
	for _, b := range builds {
		if keep[b.Name] {
			out = append(out, peerDesignOf(b, working))
		}
	}
	return out, truncated
}

// designBytes is about what d's JSON takes.
func designBytes(d proto.PeerDesign) int {
	n := 160 + len(d.Name) + len(d.Title) + len(d.Status) + len(d.Task) + len(d.TaskStatus)
	for _, a := range d.Answers {
		n += 4 + len(a)
	}
	for _, t := range d.Build {
		n += 80 + len(t.ID) + len(t.Title) + len(t.Status)
		for _, a := range t.After {
			n += 4 + len(a)
		}
	}
	return n
}

// peerDesignOf is b as a paired server may see it: its page (Artifact)
// stays home, Page says it has one; its design task's open questions
// stay home; its title, answers and build are cut to their most.
func peerDesignOf(b project.DesignBuild, working map[string]bool) proto.PeerDesign {
	d := proto.PeerDesign{Name: b.Name, Title: cutRunes(cmp.Or(b.Title, b.Name), proto.PeerDesignTitle), Status: b.Status(),
		Task: b.Task, Page: b.Artifact != "", TasksDone: b.Done(), Answers: []string{}, Build: []proto.PeerDesignTask{},
		Archived: b.Archived}
	if b.DesignTask != nil {
		d.TaskStatus = string(b.DesignTask.Status)
	}
	for i, a := range b.Answers {
		if i == proto.PeerDesignAnswers {
			break
		}
		d.Answers = append(d.Answers, cutRunes(a, proto.PeerDesignAnswer))
	}
	for i, t := range b.Ordered() {
		if i == proto.PeerDesignBuild {
			break
		}
		after := taskRefs(t.After)
		if len(after) > proto.PeerDesignAfterMax {
			after = after[:proto.PeerDesignAfterMax]
		}
		dt := proto.PeerDesignTask{ID: t.Ref(), Title: cutRunes(t.Title, proto.PeerPagesTitle), Status: string(t.Status),
			After: after, Archived: t.Archived, Working: !t.Archived && working[t.Ref()]}
		d.Working = d.Working || dt.Working
		d.Build = append(d.Build, dt)
	}
	return d
}

// listPeerDesigns is peer.design-list: the paired server alias's designs
// of its project slug, for a console.
func (s *Server) listPeerDesigns(p proto.PeerPagesReadParams, _ callCtx) (any, *proto.Error) {
	if perr := checkPeerRead(proto.MethodPeerDesignList, p.Alias, p.Project); perr != nil {
		return nil, perr
	}
	ds, err := s.peers.Designs(context.Background(), p.Alias, p.Project)
	if err != nil {
		return nil, pagesError(p.Alias, err)
	}
	return ds, nil
}
