package link

// Share requests (docs/SPEC.md §16.9, Asking for a share): the user of
// one server asks the user of a paired server to share a project with
// it. The request travels in the outbox like a link request (kind
// share-request), but it is the server's, not a project's: its For only
// says which of the asking server's projects it is for. Each side keeps
// it, its words and state in the store, never in a project's inbox: it
// is the user's, shown under its server (peer.list) and counted where
// the server's needs are, and no coordinator acts on it. There it waits
// for the user, who shares one of their projects (share = true, the
// usual safety setting) or declines. The answer comes back the same way (kind
// share-answer); which project was shared travels by the mirror, as any
// share does. A request asks; it never shares.

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// ShareSince is the release that takes share requests: an older server
// refuses the kind as bad-message.
const ShareSince = "0.50.0"

// ShareNoteMax is the most bytes of a request's note, after cleaning.
const ShareNoteMax = 120

// shareIDRE is a share request's id: S-<the asking server's
// fingerprint>-<n>.
var shareIDRE = regexp.MustCompile(`^S-([0-9A-F]{4})-[0-9]{1,12}$`)

// ValidShareID reports whether s is a share request's id.
func ValidShareID(s string) bool { return shareIDRE.MatchString(s) }

// ShareRefusalWords are the codes a share request's delivery fails
// with, as its share-undelivered words say them; {to} is the server.
var ShareRefusalWords = map[string]string{
	"bad-message":  "{to} runs a tm before " + ShareSince + ", which doesn't take share requests; ask its user yourself",
	"rate-limited": "{to} takes at most 5 share requests an hour from this server; ask again later",
	"expired":      "{to} didn't take it within 72 hours",
	"refused":      "{to} refused it",
}

// shareNote is a note as it is kept and shown: one line, cleaned.
func shareNote(s string) string { return textsafe.Line(strings.ToValidUTF8(s, ""), 0) }

// ShareSummary is an incoming request in fixed words, its note quoted as
// data: "lab-01 asks you to share a project with it, for its terminatr:
// "the deploy scripts"; answer in Network (N)".
func ShareSummary(r *proto.ShareRequest) string {
	s := peer.HomeNames().Show(r.Peer) + " asks you to share a project with it"
	if r.For != "" {
		s += ", for its " + r.For
	}
	if r.Note != "" {
		s += fmt.Sprintf(": %q", r.Note)
	}
	return s + "; answer in Network (N)"
}

// AskedWords is an outgoing request in fixed words: "asked lab-01 to
// share a project (S-4F2A-33, for terminatr)".
func AskedWords(r *proto.ShareRequest) string {
	s := "asked " + peer.HomeNames().Show(r.Peer) + " to share a project (" + r.ID
	if r.For != "" {
		s += ", for " + r.For
	}
	return s + ")"
}

// shareUndeliveredWords is an outgoing request that didn't reach its
// server, in fixed words for code.
func shareUndeliveredWords(r *proto.ShareRequest) string {
	words, ok := ShareRefusalWords[r.Code]
	if !ok {
		words = ShareRefusalWords["refused"]
	}
	to := peer.HomeNames().Show(r.Peer)
	return fmt.Sprintf("your share request %s to %s wasn't delivered: %s", r.ID, to, strings.ReplaceAll(words, "{to}", to))
}

// summarize sets r's Summary from its state.
func summarize(r *proto.ShareRequest) {
	to := peer.HomeNames().Show(r.Peer)
	switch {
	case !r.Out && r.Pending():
		r.Summary = ShareSummary(r)
	case !r.Out && r.State == proto.ShareIsAccepted:
		r.Summary = fmt.Sprintf("you shared %s for %s's share request %s", r.Project, to, r.ID)
	case !r.Out && r.State == proto.ShareIsDeclined:
		r.Summary = fmt.Sprintf("you declined %s's share request %s", to, r.ID)
	case !r.Out:
		r.Summary = fmt.Sprintf("%s's share request %s expired unanswered", to, r.ID)
	case r.State == proto.ShareQueued:
		r.Summary = AskedWords(r) + " · not sent yet"
	case r.State == proto.ShareWaiting:
		r.Summary = AskedWords(r) + " · waiting on " + to + "'s user"
	case r.State == proto.ShareIsAccepted:
		r.Summary = fmt.Sprintf("%s accepted your share request %s: the project it shared shows under it", to, r.ID)
	case r.State == proto.ShareIsDeclined:
		r.Summary = fmt.Sprintf("%s declined your share request %s; nothing was shared", to, r.ID)
	case r.State == proto.ShareExpired:
		r.Summary = fmt.Sprintf("your share request %s to %s expired unanswered after 7 days", r.ID, to)
	default:
		r.Summary = shareUndeliveredWords(r)
	}
}

// shareFind is the store's share request id, out or in.
func (s *store) shareFind(id string, out bool) *proto.ShareRequest {
	for _, r := range s.Shares {
		if r.ID == id && r.Out == out {
			return r
		}
	}
	return nil
}

// sharePending is the store's pending share request with alias, out or
// in.
func (s *store) sharePending(alias string, out bool) *proto.ShareRequest {
	for _, r := range s.Shares {
		if r.Peer == alias && r.Out == out && r.Pending() {
			return r
		}
	}
	return nil
}

// shareKeep puts r in the store in place of the older request with its
// server and direction.
func (s *store) shareKeep(r *proto.ShareRequest) {
	s.Shares = slices.DeleteFunc(s.Shares, func(x *proto.ShareRequest) bool { return x.Peer == r.Peer && x.Out == r.Out })
	s.Shares = append(s.Shares, r)
	s.ShareRev++
}

// ShareRequests are the share requests, both ways, oldest first, each
// with its Summary.
func ShareRequests() []proto.ShareRequest {
	var out []proto.ShareRequest
	readStore(func(s *store) {
		for _, r := range s.Shares {
			x := *r
			summarize(&x)
			out = append(out, x)
		}
	})
	return out
}

// ShareState is the share requests by alias, each with its Summary,
// and the store's count of their changes (peer.list's Gen adds it).
func ShareState() (byAlias map[string][]proto.ShareRequest, rev int) {
	byAlias = map[string][]proto.ShareRequest{}
	readStore(func(s *store) {
		rev = s.ShareRev
		for _, r := range s.Shares {
			x := *r
			summarize(&x)
			byAlias[x.Peer] = append(byAlias[x.Peer], x)
		}
	})
	return byAlias, rev
}

// AskShare sends a share request to the paired server alias (or its
// display name), as the human: for this server's project forSlug ("" for
// none in particular), with an optional one-line note. older says that
// server's hello shows a tm before ShareSince, which wouldn't take it.
// One waits per paired server, and at most RequestsPerHour go to one.
func AskShare(c caller.Caller, alias, forSlug, note string, older bool) (*proto.ShareRequest, error) {
	if c.Kind != caller.Human {
		return nil, refuse("human-only", "asking a paired server to share a project is the user's (Network, or tm peer ask-share)")
	}
	alias = peer.HomeNames().Alias(alias)
	d, err := remoteTo(alias, "")
	if err != nil {
		return nil, err
	}
	fp := d.(remote).fp
	if forSlug != "" {
		cfg, err := config.Load()
		if err != nil {
			return nil, err
		}
		if _, err := target(cfg, forSlug); err != nil {
			return nil, err
		}
	}
	note = shareNote(note)
	if len(note) > ShareNoteMax {
		return nil, refuse("too-long", "a share request's note is one line of at most %d bytes", ShareNoteMax)
	}
	if older {
		return nil, refuse("old-tm", "%s runs a tm before %s, which doesn't take share requests; ask its user yourself", peer.HomeNames().Show(alias), ShareSince)
	}
	t := now()
	var r *proto.ShareRequest
	err = withStore(func(s *store) error {
		if old := s.sharePending(alias, true); old != nil {
			return refuse("request-pending", "asked already (%s, since %s): one share request waits per paired server", old.ID, old.Created.Local().Format("2006-01-02 15:04"))
		}
		if !spend(s.ShareOut, alias, t) {
			return refuse("rate-limited", "at most %d share requests an hour go to %s; ask again later", RequestsPerHour, alias)
		}
		s.Next++
		r = &proto.ShareRequest{ID: fmt.Sprintf("S-%s-%d", fp, s.Next), Out: true, Peer: alias, For: forSlug, Note: note,
			Created: t.UTC().Truncate(time.Second), State: proto.ShareQueued}
		s.shareKeep(r)
		return s.queue(alias, proto.PeerMessage{Kind: proto.PeerShareRequest, ID: r.ID, For: forSlug, Text: note, Created: r.Created, By: "human"})
	})
	if err != nil {
		return nil, err
	}
	shareJournal(c, forSlug, "peer.share-request", alias, r.ID, forDetail(forSlug))
	Kick()
	x := *r
	summarize(&x)
	return &x, nil
}

// forDetail is a request's For in a journal line: " (for acme)".
func forDetail(slug string) string {
	if slug == "" {
		return ""
	}
	return " (for " + slug + ")"
}

// shareJournal journals a share request's action: in the server's log,
// and in project slug's journal when there is one. Ids and slugs only.
func shareJournal(c caller.Caller, slug, action, alias, id, detail string) {
	logf("%s %s %s%s", action, alias, id, detail)
	if slug == "" {
		return
	}
	if p, err := project.Open(slug); err == nil {
		p.Journal(c, action, alias, id+detail)
	}
}

// AnswerShare answers share request id from a paired server, as the
// human: accepting shares project slug here (share = true, the setting
// the Settings tab writes) and then says so; declining shares nothing.
// Either way the answer goes back and the request is done. Sharing is a
// safety setting the console writes on a keypress, so only the console
// accepts; the CLI declines.
func AnswerShare(c caller.Caller, id string, accept bool, slug string) (*proto.ShareRequest, error) {
	if c.Kind != caller.Human {
		return nil, refuse("human-only", "share requests are the user's to answer (Network)")
	}
	var r proto.ShareRequest
	readStore(func(s *store) {
		if x := s.shareFind(id, false); x != nil {
			r = *x
		}
	})
	if r.ID == "" {
		return nil, refuse("unknown-request", "no share request %s (tm peer requests)", id)
	}
	if !r.Pending() {
		return nil, refuse("not-pending", "%s was %s", id, r.State)
	}
	if accept {
		cfg, err := config.Load()
		if err != nil {
			return nil, err
		}
		p, err := target(cfg, slug)
		if err != nil {
			return nil, err
		}
		if sf, err := cfg.Safety(p.Slug); err == nil && !sf.Active {
			return nil, refuse("inactive", "%s isn't active: only an active project is shared", p.Slug)
		}
		if err := config.SetProject(p.Slug, "share", true); err != nil {
			return nil, err
		}
		p.Journal(c, "settings.share", p.Slug, "true")
	}
	err := withStore(func(s *store) error {
		x := s.shareFind(id, false)
		if x == nil || !x.Pending() {
			return refuse("not-pending", "%s was answered meanwhile", id)
		}
		x.State, x.Answered = proto.ShareIsDeclined, now().UTC().Truncate(time.Second)
		s.ShareRev++
		allow := proto.ShareDeclined
		if accept {
			x.State, x.Project, allow = proto.ShareIsAccepted, slug, proto.ShareAccepted
		}
		r = *x
		// A server unpaired since keeps the answer here only.
		if _, err := remoteTo(r.Peer, ""); err != nil {
			return nil
		}
		return s.queue(r.Peer, proto.PeerMessage{Kind: proto.PeerShareAnswer, Re: id, By: "human", Allow: allow})
	})
	if err != nil {
		return nil, err
	}
	if accept {
		shareJournal(c, slug, "peer.share-accepted", r.Peer, id, " ("+slug+")")
	} else {
		shareJournal(c, "", "peer.share-declined", r.Peer, id, "")
	}
	Kick()
	summarize(&r)
	return &r, nil
}

// receiveShare takes share request m from the server alias, whose
// fingerprint is fp; the store is locked.
func (s *store) receiveShare(alias, fp string, m proto.PeerMessage) proto.PeerMessageResult {
	bad := proto.PeerMessageResult{Status: proto.PeerRefused, Code: "bad-message"}
	if g := shareIDRE.FindStringSubmatch(m.ID); g == nil || g[1] != fp {
		return bad
	}
	if m.By != "human" || (m.For != "" && !project.ValidSlug(m.For)) {
		return bad
	}
	note := shareNote(m.Text)
	if len(note) > ShareNoteMax {
		return bad
	}
	if s.shareFind(m.ID, false) != nil {
		return proto.PeerMessageResult{Status: proto.PeerDuplicate}
	}
	t := now()
	if !spend(s.ShareIn, alias, t) {
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "rate-limited"}
	}
	// The newest from a server replaces one still waiting.
	r := &proto.ShareRequest{ID: m.ID, Peer: alias, For: m.For, Note: note, Created: t.UTC().Truncate(time.Second), State: proto.ShareWaiting}
	s.shareKeep(r)
	logf("peer.share-requested %s %s%s", alias, r.ID, forDetail(r.For))
	return proto.PeerMessageResult{Status: proto.PeerDelivered}
}

// receiveShareAnswer takes the answer to this server's share request
// m.Re from the server alias; the store is locked. An answer to no
// request here goes quietly.
func (s *store) receiveShareAnswer(alias string, m proto.PeerMessage) proto.PeerMessageResult {
	ok := proto.PeerMessageResult{Status: proto.PeerDelivered}
	if m.Allow != proto.ShareAccepted && m.Allow != proto.ShareDeclined {
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "bad-message"}
	}
	r := s.shareFind(m.Re, true)
	if r == nil || r.Peer != alias || !r.Pending() {
		return ok
	}
	r.State, r.Answered = proto.ShareIsAccepted, now().UTC().Truncate(time.Second)
	s.ShareRev++
	action := "peer.share-accepted"
	if m.Allow == proto.ShareDeclined {
		r.State, action = proto.ShareIsDeclined, "peer.share-declined"
	}
	shareEnded(r, action)
	return ok
}

// shareEnded journals the end of request r: in the server's log, and
// an outgoing one in the asking project's journal when it is for one
// (an incoming one's For is the other server's). Its words stay with
// the request; no inbox hears of it.
func shareEnded(r *proto.ShareRequest, action string) {
	slug := r.For
	if !r.Out {
		slug = ""
	}
	shareJournal(caller.Caller{Kind: caller.Ticker}, slug, action, r.Peer, r.ID, "")
}

// shareDelivered marks the asking side's request waiting: that server
// took it.
func shareDelivered(alias string, m proto.PeerMessage) {
	if m.Kind != proto.PeerShareRequest {
		return
	}
	withStore(func(s *store) error {
		if r := s.shareFind(m.ID, true); r != nil && r.Peer == alias && r.State == proto.ShareQueued {
			r.State = proto.ShareWaiting
			s.ShareRev++
		}
		return nil
	})
}

// shareUndelivered marks the asking side's request undelivered, code
// why: an older server's bad-message among them. An answer that isn't
// taken goes quietly.
func shareUndelivered(alias string, m proto.PeerMessage, code string) {
	if m.Kind != proto.PeerShareRequest {
		return
	}
	if _, known := ShareRefusalWords[code]; !known {
		code = "refused"
	}
	var x proto.ShareRequest
	withStore(func(s *store) error {
		if r := s.shareFind(m.ID, true); r != nil && r.Peer == alias && r.Pending() {
			r.State, r.Code = proto.ShareUndelivered, code
			s.ShareRev++
			x = *r
		}
		return nil
	})
	if x.ID != "" {
		shareEnded(&x, "peer.share-undelivered")
	}
}

// ExpireShares ends the share requests that waited RequestExpiry for
// their answer, on either side. It writes the store only when one
// expired.
func ExpireShares() {
	t := now()
	old := func(r *proto.ShareRequest) bool { return r.Pending() && t.Sub(r.Created) > RequestExpiry }
	some := false
	readStore(func(s *store) { some = slices.ContainsFunc(s.Shares, old) })
	if !some {
		return
	}
	var gone []proto.ShareRequest
	withStore(func(s *store) error {
		for _, r := range s.Shares {
			if old(r) {
				r.State = proto.ShareExpired
				s.ShareRev++
				gone = append(gone, *r)
			}
		}
		return nil
	})
	for i := range gone {
		shareEnded(&gone[i], "peer.share-expired")
	}
}
