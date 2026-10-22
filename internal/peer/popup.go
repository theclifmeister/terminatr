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

// The project popup of a shared project (docs/SPEC.md §16.9): a paired
// server whose console opens the popup on one of this server's shared
// projects reads it with peer.popup, read-only. Only while this
// server's pane switch for it is view or type: the user lets it look
// at the panes, so it may look at the board and the settings too; off
// answers proto.ErrPanesOff.

// CapPopup is the link capability of peer.popup.
const CapPopup = "popup"

// PopupExporter is an Exporter that also serves the project popup.
type PopupExporter interface {
	// Popup is the shared project's popup for the paired server c, as
	// Watch answers only for a shared project.
	Popup(c caller.Caller, project string) (proto.PeerPopup, error)
}

// errPopupOff refuses peer.popup while the pane switch is off.
var errPopupOff = proto.Errorf(proto.ErrPanesOff, "this server doesn't show its projects' popups to yours")

func (d *dispatcher) popup(_ context.Context, raw json.RawMessage) (any, error) {
	if d.level == nil || (d.level() != proto.PanesView && d.level() != proto.PanesType) {
		return nil, errPopupOff
	}
	var p proto.PeerPopupParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, proto.Errorf(proto.ErrBadParams, "peer.popup: %v", err)
		}
	}
	if p.Project == "" {
		return nil, proto.Errorf(proto.ErrBadParams, "peer.popup needs a project")
	}
	pp, err := d.ex.(PopupExporter).Popup(d.caller, p.Project)
	if err != nil {
		return nil, err
	}
	pp.Now = time.Now()
	return pp, nil
}

// popupWait bounds a popup read: the console waits for it.
const popupWait = 15 * time.Second

// Popup reads the popup of the paired server alias's shared project
// slug, cleaned (cleanPopup): ErrNoPopup when that server's tm has none,
// else peer.popup's error (proto.ErrPanesOff, proto.ErrNotShared …).
func (m *Manager) Popup(ctx context.Context, alias, slug string) (proto.PeerPopup, error) {
	if m == nil {
		return proto.PeerPopup{}, errNotUp
	}
	l := m.link(alias)
	if l == nil {
		return proto.PeerPopup{}, proto.Errorf(proto.ErrRefused, "no paired server %q", alias)
	}
	if st := l.status(); st.State == proto.LinkUp && !slices.Contains(st.Caps, CapPopup) {
		return proto.PeerPopup{}, ErrNoPopup
	}
	ctx, cancel := context.WithTimeout(ctx, popupWait)
	defer cancel()
	var pp proto.PeerPopup
	if err := l.call(ctx, proto.MethodPeerPopup, proto.PeerPopupParams{Project: slug}, &pp); err != nil {
		return proto.PeerPopup{}, err
	}
	return cleanPopup(pp), nil
}

// ErrNoPopup means the peer's tm serves no project popup (its hello
// doesn't list CapPopup).
var ErrNoPopup = proto.Errorf(proto.ErrUnknownMethod, "the paired server's tm serves no project popup")

// cleanPopup is a peer.popup answer with every string cleaned, the words
// a row shows on one line, ids checked against their patterns (a task
// or message whose id isn't one is dropped, a thread's is blanked),
// messages only in the states that crossed (proto.PeerMessageStates),
// and each list cut to its most.
func cleanPopup(p proto.PeerPopup) proto.PeerPopup {
	cleanStrings(&p)
	p.Project = word(peerSlug, p.Project)
	p.Goal = textsafe.Line(p.Goal, 200)
	p.Repos = max(p.Repos, 0)
	ts := make([]proto.PeerTask, 0, min(len(p.Tasks), proto.PeerPopupMaxTasks))
	for _, t := range p.Tasks {
		if len(ts) == proto.PeerPopupMaxTasks {
			break
		}
		if t.ID <= 0 {
			continue
		}
		t.Title, t.Status = textsafe.Line(t.Title, 200), word(peerWord, t.Status)
		t.Thread = word(peerThread, t.Thread)
		t.StepsTotal = max(t.StepsTotal, 0)
		t.StepsDone = min(max(t.StepsDone, 0), t.StepsTotal)
		t.Created, t.Updated = textsafe.Line(t.Created, 40), textsafe.Line(t.Updated, 40)
		ts = append(ts, t)
	}
	p.Tasks = ts
	ls := make([]proto.PeerPopupLink, 0, min(len(p.Links), proto.PeerPopupMaxLinks))
	for _, l := range p.Links {
		if len(ls) == proto.PeerPopupMaxLinks {
			break
		}
		if !peerSlug.MatchString(l.Project) {
			continue
		}
		l.Unread = max(l.Unread, 0)
		l.Last = word(peerMessage, l.Last)
		if n := len(l.Messages); n > proto.PeerPopupMaxMessages {
			l.Messages = l.Messages[n-proto.PeerPopupMaxMessages:]
		}
		ms := make([]proto.PeerLinkMessage, 0, len(l.Messages))
		for _, msg := range l.Messages {
			// Only a message that crossed: a held or queued one a
			// server sends anyway isn't shown.
			if !peerMessage.MatchString(msg.ID) || !slices.Contains(proto.PeerMessageStates, msg.State) {
				continue
			}
			msg.Re, msg.By = word(peerMessage, msg.Re), word(peerWord, msg.By)
			msg.Bytes = max(msg.Bytes, 0)
			ms = append(ms, msg)
		}
		l.Messages = ms
		ls = append(ls, l)
	}
	p.Links = ls
	s := &p.Settings
	for _, w := range []*string{&s.StartThreads, &s.AutoClose, &s.CompleteTasks, &s.Protection, &s.ThreadAgent,
		&s.CoordinatorAgent, &s.CoordinatorModel, &s.Merge, &s.MergeMethod, &s.GuardMode} {
		*w = textsafe.Line(*w, 60)
	}
	if len(s.Models) > proto.PeerPopupMaxModels {
		s.Models = s.Models[:proto.PeerPopupMaxModels]
	}
	for i := range s.Models {
		s.Models[i] = textsafe.Line(s.Models[i], 60)
	}
	return p
}
