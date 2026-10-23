package link

// Link requests across servers (docs/SPEC.md §16.11): the user of one
// server asks the user of a paired server to link one of its projects
// back. The request travels in the outbox like a message (kind
// link-request); there it is a link-request item that needs the user,
// who accepts it (choosing the direction and ask of that side's own
// entry) or declines it. The answer comes back the same way (kind
// link-answer). Each server writes only its own config.toml: the
// request writes the asking side's entry, the acceptance the other's.
// Requests are the human's, rate-limited by server, and expire.

import (
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// Inbox kinds of link requests.
const (
	// KindRequest is a paired server's project asking this one to link
	// back: the user's to accept or decline.
	KindRequest = "link-request"
	// KindAnswer tells the asking project its request was answered.
	KindAnswer = "link-answer"
)

// Request states. An outgoing request is queued, then waiting once that
// server took it; an incoming one waits from its arrival.
const (
	ReqQueued      = "queued"
	ReqWaiting     = "waiting"
	ReqAccepted    = "accepted"
	ReqDeclined    = "declined"
	ReqExpired     = "expired"
	ReqUndelivered = "undelivered"
)

// RequestExpiry is how long a request waits for its answer.
const RequestExpiry = 7 * 24 * time.Hour

// RequestsPerHour is how many requests one server sends to, or takes
// from, one paired server in an hour.
const RequestsPerHour = 5

// Request is a link request, kept in the store: the newest one per
// project, other project and direction.
type Request struct {
	ID string `json:"id"`
	// Out: this side asked; else the paired server did.
	Out bool `json:"out"`
	// Project is the project here, Peer the other ("mini/acme-api").
	Project string `json:"project"`
	Peer    string `json:"peer"`
	// Allow is the asking side's entry: send there is receive here.
	Allow   string    `json:"allow"`
	Created time.Time `json:"created"`
	State   string    `json:"state"`
	// Answer is the answering side's entry once answered ("off" when
	// declined), Ask its ask switch (this server's only); Code the
	// refusal of an undelivered request.
	Answer   string    `json:"answer,omitempty"`
	Ask      bool      `json:"ask,omitempty"`
	Answered time.Time `json:"answered,omitzero"`
	Code     string    `json:"code,omitempty"`
}

// Pending reports a request that waits for its answer.
func (r *Request) Pending() bool { return r.State == ReqQueued || r.State == ReqWaiting }

// Words is an outgoing request as the other side's column says it:
// "asked, no answer yet", "accepted: both ways", "declined".
func (r *Request) Words() string {
	switch r.State {
	case ReqQueued:
		return "asked, not sent yet"
	case ReqWaiting:
		return "asked, no answer yet"
	case ReqAccepted:
		return "accepted: " + allowWords(r.Answer)
	case ReqDeclined:
		return "declined"
	case ReqExpired:
		return "request expired"
	}
	return "request not delivered"
}

// allowWords is a direction in words: "both ways", "send only".
func allowWords(allow string) string {
	switch allow {
	case config.LinkBoth:
		return "both ways"
	case config.LinkSend:
		return "send only"
	case config.LinkReceive:
		return "receive only"
	}
	return "off"
}

// reqIDRE is a request's id: R-<the asking server's fingerprint>-<n>.
var reqIDRE = regexp.MustCompile(`^R-([0-9A-F]{4})-[0-9]{1,12}$`)

// ValidRequestID reports whether s is a request's id.
func ValidRequestID(s string) bool { return reqIDRE.MatchString(s) }

// RequestRefusalWords are the codes a request's delivery fails with,
// as the asking project's link-undelivered item says them.
var RequestRefusalWords = map[string]string{
	"not-shared":   "{to} isn't shared with this server: its user shares it (Settings) before a link can be asked",
	"bad-message":  "{to}'s server didn't take it: a tm before 0.28.0 doesn't take link requests; its user sets that side by hand (tm project link there)",
	"rate-limited": "{to}'s server takes at most 5 link requests an hour from this one; ask again later",
	"expired":      "{to}'s server didn't take it within 72 hours",
	"refused":      "{to}'s server refused it",
}

// pending finds the store's pending request of project slug with peer,
// out or in.
func (s *store) pending(slug, peer string, out bool) *Request {
	for _, r := range s.Requests {
		if r.Project == slug && r.Peer == peer && r.Out == out && r.Pending() {
			return r
		}
	}
	return nil
}

// keep puts r in the store in place of the older request of its
// project, peer and direction.
func (s *store) keep(r *Request) {
	s.Requests = slices.DeleteFunc(s.Requests, func(x *Request) bool {
		return x.Project == r.Project && x.Peer == r.Peer && x.Out == r.Out
	})
	s.Requests = append(s.Requests, r)
}

// find is request id of project slug.
func (s *store) find(slug, id string, out bool) *Request {
	for _, r := range s.Requests {
		if r.ID == id && r.Project == slug && r.Out == out {
			return r
		}
	}
	return nil
}

// spend counts a request to or from alias in the hour's stamps, and
// refuses one over RequestsPerHour.
func spend(stamps map[string][]time.Time, alias string, t time.Time) bool {
	stamps[alias] = slices.DeleteFunc(stamps[alias], func(x time.Time) bool { return t.Sub(x) >= time.Hour })
	if len(stamps[alias]) >= RequestsPerHour {
		return false
	}
	stamps[alias] = append(stamps[alias], t)
	return true
}

// Requests are p's requests, both ways, oldest first.
func Requests(p *project.Project) []Request {
	var out []Request
	readStore(func(s *store) {
		for _, r := range s.Requests {
			if r.Project == p.Slug {
				out = append(out, *r)
			}
		}
	})
	return out
}

// Ask sends project p's link request to key, a project on a paired
// server, as the human: p's entry for key is set to allow first (this
// side only, a new one asking first), then the request asks that
// server's user to set theirs. One request waits per pair.
func Ask(c caller.Caller, p *project.Project, key, allow string) (*Request, error) {
	if c.IsAgent() {
		return nil, refuse("human-only", "link requests are the user's (Settings › Linked projects)")
	}
	key = Canon(key)
	alias, slug, ok := config.Remote(key)
	if !ok {
		return nil, refuse("not-remote", "a request goes to a paired server's project (<server>/<project>); %s is set on both sides at once", key)
	}
	if !slices.Contains(config.LinkAllows, allow) {
		return nil, refuse("bad-direction", "a request asks for %s, %s or %s", config.LinkBoth, config.LinkSend, config.LinkReceive)
	}
	d, err := remoteTo(alias, slug)
	if err != nil {
		return nil, err
	}
	fp := d.(remote).fp
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if mine, _ := cfg.Links(p.Slug); mine[key].Allow != allow {
		if err := setRemote(c, p, key, alias, slug, allow, nil); err != nil {
			return nil, err
		}
	}
	t := now()
	var r *Request
	err = withStore(func(s *store) error {
		if old := s.pending(p.Slug, key, true); old != nil {
			return refuse("request-pending", "%s's request %s to %s waits for an answer since %s", p.Slug, old.ID, key, old.Created.Local().Format("2006-01-02 15:04"))
		}
		if !spend(s.ReqOut, alias, t) {
			return refuse("rate-limited", "at most %d link requests an hour go to %s; ask again later", RequestsPerHour, alias)
		}
		s.Next++
		r = &Request{ID: fmt.Sprintf("R-%s-%d", fp, s.Next), Out: true, Project: p.Slug, Peer: key, Allow: allow, Created: t.UTC().Truncate(time.Second), State: ReqQueued}
		s.keep(r)
		return s.queue(alias, proto.PeerMessage{Kind: proto.PeerLinkRequest, From: p.Slug, To: slug, ID: r.ID, Created: r.Created, By: "human", Allow: allow})
	})
	if err != nil {
		return nil, err
	}
	p.Journal(c, "link.request", key, fmt.Sprintf("%s (%s)", r.ID, allow))
	Kick()
	return r, nil
}

// Answer answers p's request id from a paired server's project, as the
// human: accepting sets p's own entry to allow with ask, declining sets
// nothing. Either way the answer goes back, and the item is done.
func Answer(c caller.Caller, p *project.Project, id string, accept bool, allow string, ask bool) (*Request, error) {
	if c.IsAgent() {
		return nil, refuse("human-only", "link requests are the user's to answer (the Messages tab, or tm link accept|decline)")
	}
	if accept && !slices.Contains(config.LinkAllows, allow) {
		return nil, refuse("bad-direction", "a link is %s, %s or %s", config.LinkBoth, config.LinkSend, config.LinkReceive)
	}
	var r Request
	readStore(func(s *store) {
		if x := s.find(p.Slug, id, false); x != nil {
			r = *x
		}
	})
	if r.ID == "" {
		return nil, refuse("unknown-request", "%s has no link request %s", p.Slug, id)
	}
	if !r.Pending() {
		return nil, refuse("not-pending", "%s was %s", id, r.State)
	}
	alias, slug, _ := config.Remote(r.Peer)
	answer := config.LinkOff
	if accept {
		answer = allow
	}
	err := withStore(func(s *store) error {
		x := s.find(p.Slug, id, false)
		if x == nil || !x.Pending() {
			return refuse("not-pending", "%s was answered meanwhile", id)
		}
		// This server's own entry only; the asking side set its own.
		if accept {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			mine, _ := cfg.Links(p.Slug)
			if err := config.SetLink(p.Slug, r.Peer, config.Link{Allow: allow, Ask: ask, Files: mine[r.Peer].Files}); err != nil {
				return err
			}
		}
		x.State, x.Answer, x.Ask, x.Answered = ReqDeclined, answer, ask && accept, now().UTC().Truncate(time.Second)
		if accept {
			x.State = ReqAccepted
		}
		r = *x
		// A server unpaired since keeps the answer here only.
		if _, err := remoteTo(alias, slug); err != nil {
			return nil
		}
		return s.queue(alias, proto.PeerMessage{Kind: proto.PeerLinkAnswer, From: p.Slug, To: slug, ID: id, By: "human", Allow: answer})
	})
	if err != nil {
		return nil, err
	}
	doneRequest(p, id)
	if accept {
		p.Journal(c, "link.accept", r.Peer, fmt.Sprintf("%s (%s)", id, linkWords(config.Link{Allow: allow, Ask: ask})))
	} else {
		p.Journal(c, "link.decline", r.Peer, id)
	}
	Kick()
	return &r, nil
}

// doneRequest moves the link-request items of request id to done.
func doneRequest(p *project.Project, id string) {
	items, _ := p.Inbox()
	for _, it := range items {
		if it.Kind == KindRequest && it.Message == id {
			p.DoneItem(it.ID)
		}
	}
}

// receiveRequest takes request m from project m.From on the server
// alias for project m.To here; the store is locked. A project that
// isn't there, is archived or isn't shared answers alike, not-shared.
func (s *store) receiveRequest(alias, fp string, m proto.PeerMessage) proto.PeerMessageResult {
	bad := proto.PeerMessageResult{Status: proto.PeerRefused, Code: "bad-message"}
	if g := reqIDRE.FindStringSubmatch(m.ID); g == nil || g[1] != fp {
		return bad
	}
	if m.By != "human" || !slices.Contains(config.LinkAllows, m.Allow) {
		return bad
	}
	cfg, err := config.Load()
	if err != nil {
		return proto.PeerMessageResult{Status: proto.PeerRetry, Code: "refused"}
	}
	notShared := proto.PeerMessageResult{Status: proto.PeerRefused, Code: "not-shared"}
	to, err := target(cfg, m.To)
	if err != nil {
		return notShared
	}
	if sf, err := cfg.Safety(to.Slug); err != nil || !sf.Share {
		return notShared
	}
	key := alias + "/" + m.From
	if s.find(to.Slug, m.ID, false) != nil {
		return proto.PeerMessageResult{Status: proto.PeerDuplicate}
	}
	t := now()
	if !spend(s.ReqIn, alias, t) {
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "rate-limited"}
	}
	// A newer request from the same project replaces one still waiting.
	if old := s.pending(to.Slug, key, false); old != nil {
		doneRequest(to, old.ID)
	}
	r := &Request{ID: m.ID, Project: to.Slug, Peer: key, Allow: m.Allow, Created: t.UTC().Truncate(time.Second), State: ReqWaiting}
	s.keep(r)
	to.AddMessageItem(KindRequest, key, RequestSummary(r), true, r.ID)
	to.Journal(caller.ForPeer(alias), "link.requested", key, fmt.Sprintf("%s (%s)", r.ID, r.Allow))
	return proto.PeerMessageResult{Status: proto.PeerDelivered}
}

// RequestSummary is an incoming request's item: fixed words.
func RequestSummary(r *Request) string {
	return fmt.Sprintf("%s asks to link with %s: %s on its side, so %s here; accept or decline it in the Messages tab (or tm link accept|decline %s)",
		Shown(r.Peer), r.Project, allowWords(r.Allow), allowWords(config.Mirror(r.Allow)), r.ID)
}

// receiveAnswer takes the answer to request m.ID, which project m.To
// here sent to project m.From on the server alias; the store is locked.
// An answer to no request here goes quietly.
func (s *store) receiveAnswer(alias string, m proto.PeerMessage) proto.PeerMessageResult {
	ok := proto.PeerMessageResult{Status: proto.PeerDelivered}
	if !ValidRequestID(m.ID) || (m.Allow != config.LinkOff && !slices.Contains(config.LinkAllows, m.Allow)) {
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "bad-message"}
	}
	key := alias + "/" + m.From
	r := s.find(m.To, m.ID, true)
	if r == nil || r.Peer != key || !r.Pending() {
		return ok
	}
	r.State, r.Answer, r.Answered = ReqAccepted, m.Allow, now().UTC().Truncate(time.Second)
	if m.Allow == config.LinkOff {
		r.State = ReqDeclined
	}
	p, err := project.Open(m.To)
	if err != nil {
		return ok
	}
	if r.State == ReqAccepted {
		p.AddMessageItem(KindAnswer, key, fmt.Sprintf("%s accepted %s's link request %s: %s on its side", Shown(key), p.Slug, r.ID, allowWords(r.Answer)), false, r.ID)
		p.Journal(caller.ForPeer(alias), "link.accepted", key, fmt.Sprintf("%s (%s)", r.ID, r.Answer))
	} else {
		p.AddMessageItem(KindAnswer, key, fmt.Sprintf("%s declined %s's link request %s; this side's entry stays as it is", Shown(key), p.Slug, r.ID), false, r.ID)
		p.Journal(caller.ForPeer(alias), "link.declined", key, r.ID)
	}
	return ok
}

// requestDelivered marks the asking side's request waiting: that
// server took it.
func requestDelivered(alias string, m proto.PeerMessage) {
	if m.Kind != proto.PeerLinkRequest {
		return
	}
	withStore(func(s *store) error {
		if r := s.find(m.From, m.ID, true); r != nil && r.State == ReqQueued {
			r.State = ReqWaiting
		}
		return nil
	})
}

// requestUndelivered marks the asking side's request undelivered and
// tells its inbox, in fixed words for code. An answer that isn't taken
// goes quietly.
func requestUndelivered(alias string, m proto.PeerMessage, code string) {
	if m.Kind != proto.PeerLinkRequest {
		return
	}
	words, known := RequestRefusalWords[code]
	if !known {
		code, words = "refused", RequestRefusalWords["refused"]
	}
	key := alias + "/" + m.To
	found := false
	withStore(func(s *store) error {
		if r := s.find(m.From, m.ID, true); r != nil && r.Pending() {
			r.State, r.Code, found = ReqUndelivered, code, true
		}
		return nil
	})
	p, err := project.Open(m.From)
	if !found || err != nil {
		return
	}
	why := replaceWords(words, key, p.Slug)
	p.AddMessageItem(KindUndelivered, key, fmt.Sprintf("your link request %s to %s wasn't delivered: %s", m.ID, Shown(key), why), false, m.ID)
	p.Journal(caller.Caller{Kind: caller.Ticker}, "link.undelivered", key, m.ID+" ("+code+")")
}

// ExpireRequests ends the requests that waited RequestExpiry for their
// answer: an incoming one's item is done. It writes the store only when
// one expired.
func ExpireRequests() {
	t := now()
	old := func(r *Request) bool { return r.State == ReqWaiting && t.Sub(r.Created) > RequestExpiry }
	some := false
	readStore(func(s *store) { some = slices.ContainsFunc(s.Requests, old) })
	if !some {
		return
	}
	var gone []Request
	withStore(func(s *store) error {
		for _, r := range s.Requests {
			if old(r) {
				r.State = ReqExpired
				gone = append(gone, *r)
			}
		}
		return nil
	})
	for _, r := range gone {
		p, err := project.Open(r.Project)
		if err != nil {
			continue
		}
		if !r.Out {
			doneRequest(p, r.ID)
		}
		p.Journal(caller.Caller{Kind: caller.Ticker}, "link.request-expired", r.Peer, r.ID)
	}
}
