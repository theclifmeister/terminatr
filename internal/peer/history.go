package peer

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// A shared project's daily numbers (docs/SPEC.md §16.17), under the
// project pages switch, read-only: numbers and days, nothing named.

// CapPageHistory is the link capability of peer.pages.history: a server
// that lists pages but not this one is an older tm, and the feature
// hides for its projects.
const CapPageHistory = "page-history"

// HistoryExporter is a PagesExporter that also answers a shared
// project's daily numbers.
type HistoryExporter interface {
	PagesExporter
	History(c caller.Caller, project string) (proto.History, error)
}

// historyMethods are the link's method of the numbers, when the Exporter
// serves them.
func (d *dispatcher) historyMethods(m map[string]handler) {
	if _, ok := d.ex.(HistoryExporter); !ok || d.pages == nil {
		return
	}
	m[proto.MethodPeerPagesHistory] = d.pagesHistory
}

func (d *dispatcher) pagesHistory(_ context.Context, raw json.RawMessage) (any, error) {
	project, err := d.pagesProject(raw, proto.MethodPeerPagesHistory)
	if err != nil {
		return nil, err
	}
	return d.ex.(HistoryExporter).History(d.caller, project)
}

// ErrNoHistory means the peer's tm shares project pages but no numbers
// (its hello lists CapPages, not CapPageHistory).
var ErrNoHistory = proto.Errorf(proto.ErrNoPages, "the paired server's tm shares no history")

// History reads the numbers of the paired server alias's shared project
// slug, cleaned (cleanHistory): ErrNoPages for a tm without pages,
// ErrNoHistory for one without the numbers, else peer.pages.history's
// error.
func (m *Manager) History(ctx context.Context, alias, slug string) (proto.History, error) {
	l, err := m.pagesLink(alias)
	if err != nil {
		return proto.History{}, err
	}
	if st := l.status(); st.State == proto.LinkUp && !slices.Contains(st.Caps, CapPageHistory) {
		return proto.History{}, ErrNoHistory
	}
	ctx, cancel := context.WithTimeout(ctx, pagesWait)
	defer cancel()
	var h proto.History
	if err := l.call(ctx, proto.MethodPeerPagesHistory, proto.PeerPagesParams{Project: slug}, &h); err != nil {
		return proto.History{}, err
	}
	return cleanHistory(h), nil
}

var historyDay = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

// cleanHistory is a peer.pages.history answer as this server passes it
// on: a day that isn't one is left out, a count is never negative, the
// words are one short line, and at most PeerHistoryDays days stay.
func cleanHistory(in proto.History) proto.History {
	day := func(d proto.HistoryDay) (proto.HistoryDay, bool) {
		return proto.HistoryDay{Day: d.Day, Done: max(d.Done, 0)}, historyDay.MatchString(d.Day)
	}
	out := proto.History{Done: max(in.Done, 0), Open: max(in.Open, 0), Dropped: max(in.Dropped, 0), Days: []proto.HistoryDay{},
		Timed: max(in.Timed, 0), TypicalSeconds: max(in.TypicalSeconds, 0), Typical: textsafe.Line(in.Typical, 40), Truncated: in.Truncated}
	if historyDay.MatchString(in.FirstRecord) {
		out.FirstRecord = in.FirstRecord
	}
	if n := len(in.Days) - proto.PeerHistoryDays; n > 0 {
		in.Days, out.Truncated = in.Days[n:], true
	}
	for _, d := range in.Days {
		if c, ok := day(d); ok {
			out.Days = append(out.Days, c)
		}
	}
	if in.BestDay != nil {
		if c, ok := day(*in.BestDay); ok {
			out.BestDay = &c
		}
	}
	return out
}
