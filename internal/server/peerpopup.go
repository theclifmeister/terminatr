package server

// The project popup of a shared project, for a paired server's console
// (docs/SPEC.md §16.9): read-only, beside peer.watch. Each record is
// copied field by field from this server's own, so a field added to a
// task, a message or the settings stays home until it is added here
// (peerpopup_test.go fails until it is decided).

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

func (e peerExport) Popup(c caller.Caller, slug string) (proto.PeerPopup, error) {
	if c.Kind != caller.Peer || c.Alias == "" {
		return proto.PeerPopup{}, proto.Errorf(proto.ErrRefused, "peer.popup is a paired server's")
	}
	if !slices.Contains(sharedProjects(), slug) {
		return proto.PeerPopup{}, errNotShared(slug)
	}
	p, err := project.Open(slug)
	if err != nil {
		return proto.PeerPopup{}, errNotShared(slug)
	}
	cfg, err := config.Load()
	if err != nil {
		return proto.PeerPopup{}, errNotShared(slug)
	}
	out := proto.PeerPopup{Project: slug, Goal: p.Meta.Goal, Repos: len(p.Repos),
		Tasks: []proto.PeerTask{}, Links: []proto.PeerPopupLink{}}
	if b, err := p.Tasks().Load(); err == nil {
		for _, t := range b.Tasks {
			if !t.Archived && len(out.Tasks) < proto.PeerPopupMaxTasks {
				out.Tasks = append(out.Tasks, peerTaskOf(t))
			}
		}
	}
	if s, err := cfg.Safety(slug); err == nil {
		out.Settings = peerSettingsOf(s)
	}
	// Nothing of the memory crosses, not even its notes' titles.
	out.Links = peerLinksOf(p, link.Rows(cfg, p), c.Alias)
	return out, nil
}

// peerTaskOf is t as a paired server may see it: its notes, owner, repo
// and steps' words stay home.
func peerTaskOf(t *tasks.Task) proto.PeerTask {
	return proto.PeerTask{ID: t.ID, Title: t.Title, Status: string(t.Status), Thread: t.Thread,
		StepsDone: t.StepsDone(), StepsTotal: len(t.Steps), Created: t.Created, Updated: t.Updated}
}

// peerSettingsOf is s as a paired server may see it (proto.PeerSettings).
func peerSettingsOf(s config.Safety) proto.PeerSettings {
	return proto.PeerSettings{StartThreads: s.StartThreads, Yolo: s.Yolo, CoordinatorApproves: s.CoordinatorApproves,
		ParallelThreads: s.ParallelThreads, AutoClose: s.AutoClose, AutoCloseDays: s.AutoCloseDays, PRFollowup: s.PRFollowup,
		PRPollSeconds: s.PRPollSeconds, CompleteTasks: s.CompleteTasks, CoordinatorRemoteControl: s.CoordinatorRemoteControl,
		AutoClear: s.AutoClear, CoordinatorMerges: s.CoordinatorMerges, FastForwardCheckout: s.FastForwardCheckout,
		Protection: s.Protection, CoordinatorAccess: s.CoordinatorAccess, ThreadAgent: s.ThreadAgent,
		CoordinatorAgent: s.CoordinatorAgent, Models: slices.Clone(s.Models), CoordinatorModel: s.CoordinatorModel,
		Active: s.Active, Paused: s.Paused, Share: s.Share, Merge: s.Merge, MergeMethod: s.MergeMethod, Guard: s.Guard,
		GuardMode: s.GuardMode, ArchiveTasksDays: s.ArchiveTasksDays, ArchiveThreadsDays: s.ArchiveThreadsDays,
		ArchiveInboxDays: s.ArchiveInboxDays, ArchiveJournalDays: s.ArchiveJournalDays, LinkBurst: s.LinkBurst,
		LinkPerHour: s.LinkPerHour, LinkMaxKB: s.LinkMaxKB}
}

// peerLinksOf are p's links (rows) with the projects of the paired
// server this one calls alias, each named by its slug there, with the
// headers of its messages that crossed (proto.PeerMessageStates): links
// with this server's other projects and with other servers' stay home,
// as do messages held for the user's OK, queued over the rate, dropped
// or undelivered, their counts, and a newest one among them.
func peerLinksOf(p *project.Project, rows []link.Row, alias string) []proto.PeerPopupLink {
	out := []proto.PeerPopupLink{}
	for _, r := range rows {
		a, slug, ok := config.Remote(r.Peer)
		if !ok || a != alias || len(out) == proto.PeerPopupMaxLinks {
			continue
		}
		l := proto.PeerPopupLink{Project: slug, Send: r.Send, Receive: r.Receive, Paused: r.Paused,
			Unread: r.Unread, Messages: []proto.PeerLinkMessage{}}
		msgs, _ := link.Log(p, r.Peer)
		msgs = slices.DeleteFunc(msgs, func(m *link.Message) bool { return !slices.Contains(proto.PeerMessageStates, m.State) })
		if n := len(msgs); n > proto.PeerPopupMaxMessages {
			msgs = msgs[n-proto.PeerPopupMaxMessages:]
		}
		for _, m := range msgs {
			l.Messages = append(l.Messages, peerMessageOf(m))
		}
		if n := len(msgs); n > 0 {
			l.Last, l.LastAt = msgs[n-1].ID, msgs[n-1].Created
		}
		out = append(out, l)
	}
	return out
}

// peerMessageOf is m's header: its text, attachments, refusal code and
// due time stay home.
func peerMessageOf(m *link.Message) proto.PeerLinkMessage {
	return proto.PeerLinkMessage{ID: m.ID, Out: m.Out, Created: m.Created, Re: m.Re, Bytes: m.Bytes, State: m.State, By: m.By}
}

// readPeerPopup is peer.popup-read: the paired server alias's popup of
// its project slug, for a console.
func (s *Server) readPeerPopup(p proto.PeerPopupReadParams) (any, *proto.Error) {
	if p.Alias == "" || strings.ContainsAny(p.Alias, "/@") || !project.ValidSlug(p.Project) {
		return nil, proto.Errorf(proto.ErrBadParams, "peer.popup-read needs a paired server and its project")
	}
	pp, err := s.peers.Popup(context.Background(), p.Alias, p.Project)
	if err != nil {
		return nil, popupError(p.Alias, err)
	}
	return pp, nil
}

// popupError is a failed peer.popup in this server's words, its code
// kept: the console words unknown-method (a tm without it) and
// panes-off its own way.
func popupError(alias string, err error) *proto.Error {
	var pe *proto.Error
	switch {
	case errors.As(err, &pe) && pe.Code == proto.ErrUnknownMethod:
		return proto.Errorf(proto.ErrUnknownMethod, "%s's tm serves no project popup; update it to open one", alias)
	case errors.As(err, &pe) && pe.Code == proto.ErrPanesOff:
		return proto.Errorf(proto.ErrPanesOff, "%s doesn't show its projects to this server: on %s, tm peer panes <this server> view", alias, alias)
	case errors.As(err, &pe) && pe.Code == proto.ErrNotShared:
		return proto.Errorf(proto.ErrNotShared, "%s doesn't share that project any more", alias)
	case errors.As(err, &pe):
		return pe
	}
	return proto.Errorf(proto.ErrRefused, "%s can't be reached now (%v)", alias, err)
}
