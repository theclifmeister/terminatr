package link

// Links to projects on paired servers (docs/SPEC.md §16.11). The link's
// key is qualified, "mini/acme-api": the server's alias (tm peer) and the
// project's slug there. Each server checks its own entry: the sender
// before anything leaves, the receiver when the message arrives. The
// sender queues the message in its outbox for that server; the server's
// flusher delivers each peer's entries in sequence order over the link
// (peer.message) once it is up, and the answer is the delivered receipt.
// Handling the item sends a read receipt back the same way. An entry not
// taken within Expiry, or refused there, raises a link-undelivered item
// for the sender. Attachments stay on their server.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// Expiry is how long a message waits in the outbox for its server.
const Expiry = 72 * time.Hour

// RefusalWords are the codes a receiving server answers
// (proto.PeerMessageResult), and the sender's own, as its
// link-undelivered item says them; {to} is the receiving project, {from}
// the sending one. A code not here is shown as "refused".
var RefusalWords = map[string]string{
	"not-linked":   "{to} doesn't take messages from {from}: the user of its server sets that side",
	"link-paused":  "the link is paused on {to}'s server; its user resumes it there",
	"bad-message":  "{to}'s server refused it as malformed",
	"rate-limited": "{to}'s server is over the link's rate",
	"too-long":     "{to}'s server takes shorter messages: split it into parts",
	"expired":      "{to}'s server didn't take it within 72 hours",
	"refused":      "{to}'s server refused it",
}

// remote is a project on a paired server: its alias and slug, and this
// server's fingerprint for the ids.
type remote struct{ alias, slug, fp string }

// remoteTo checks that alias is a paired server and that this server has
// its key.
func remoteTo(alias, slug string) (deliverer, error) {
	h, err := home.Dir()
	if err != nil {
		return nil, err
	}
	dir := peer.DirIn(h)
	id, err := peer.LoadIdentity(dir)
	if err != nil {
		return nil, refuse("unknown-server", "no paired server named %s (tm peer list)", alias)
	}
	peers, _ := peer.LoadPeers(dir)
	for _, p := range peers {
		if p.Alias == alias {
			return remote{alias: alias, slug: slug, fp: fingerprint(id.ID)}, nil
		}
	}
	return nil, refuse("unknown-server", "no paired server named %s (tm peer list)", alias)
}

// fingerprint is a server id's first four hex digits, as tm peer list
// shows them: the middle of a remote message's id, M-4F2A-17.
func fingerprint(id peer.ID) string { return id.Short()[:4] }

func (r remote) newID(s *store) string {
	s.Next++
	return fmt.Sprintf("M-%s-%d", r.fp, s.Next)
}

// deliver keeps the sender's copy, queued, and puts the message in the
// outbox to the paired server; it counts for the rate and the breaker
// now, as a local one does.
func (r remote) deliver(s *store, c caller.Caller, from *project.Project, m *Message, _ []attachment, l limits) error {
	m.State = StateQueued
	if err := write(from, m); err != nil {
		return err
	}
	pm := proto.PeerMessage{Kind: proto.PeerMessageText, From: from.Slug, To: r.slug, ID: m.ID, Created: m.Created, Re: m.Re, By: m.By, Text: m.Text}
	if err := s.queue(r.alias, pm); err != nil {
		return err
	}
	from.Journal(c, "link.send", m.Peer, fmt.Sprintf("%s (%s, queued)", m.ID, KB(m.Bytes)))
	s.account(c.IsAgent(), from.Slug, m.Peer, l, now(), side{from, m.Peer})
	Kick()
	return nil
}

// entry is one outbox file: state/link/outbox/<alias>/<seq>.json.
type entry struct {
	Queued time.Time         `json:"queued"`
	Msg    proto.PeerMessage `json:"msg"`
}

func outboxDir() (string, error) {
	d, err := home.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "state", "link", "outbox"), nil
}

// queue numbers m in alias's sequence and writes it to the outbox; the
// store is locked.
func (s *store) queue(alias string, m proto.PeerMessage) error {
	root, err := outboxDir()
	if err != nil {
		return err
	}
	if s.Epoch == "" {
		b := make([]byte, 8)
		rand.Read(b)
		s.Epoch = hex.EncodeToString(b)
	}
	s.Out[alias]++
	m.Epoch, m.Seq = s.Epoch, s.Out[alias]
	d := filepath.Join(root, alias)
	if err := fsx.EnsurePrivateDir(d); err != nil {
		return err
	}
	b, err := json.Marshal(entry{Queued: now().UTC(), Msg: m})
	if err != nil {
		return err
	}
	return fsx.WriteAtomic(filepath.Join(d, fmt.Sprintf("%010d.json", m.Seq)), b, 0o600)
}

// queueRead queues the read receipt of message id, which project from
// here handled, for project to on the server alias.
func queueRead(alias, from, to, id string) {
	err := withStore(func(s *store) error {
		return s.queue(alias, proto.PeerMessage{Kind: proto.PeerMessageRead, From: from, To: to, ID: id})
	})
	if err == nil {
		Kick()
	}
}

// kick wakes the flusher: a message was queued in this process.
var kick = make(chan struct{}, 1)

// Kick asks the flusher to deliver now rather than at its next poll.
func Kick() {
	select {
	case kick <- struct{}{}:
	default:
	}
}

// Transport is the server's links to paired servers, as the flusher
// needs them.
type Transport interface {
	// Ready says the link to alias is up and takes messages (CapMsg).
	Ready(alias string) bool
	Call(ctx context.Context, alias, method string, params, result any) error
}

// Poll is how often the flusher looks at the outbox without a kick: a
// link that came up, or a message queued by a tm without the server.
var Poll = 3 * time.Second

// RunOutbox delivers the outbox until ctx ends: on each kick and every
// Poll.
func RunOutbox(ctx context.Context, t Transport) {
	tick := time.NewTicker(Poll)
	defer tick.Stop()
	for {
		Flush(ctx, t)
		select {
		case <-ctx.Done():
			return
		case <-kick:
		case <-tick.C:
		}
	}
}

// callTimeout bounds one delivery.
const callTimeout = 30 * time.Second

// holdUntil is, by alias, when the outbox tries a server again that
// answered retry with RetryAfter: its rate, not its link.
var (
	holdMu    sync.Mutex
	holdUntil = map[string]time.Time{}
)

// maxHold bounds a server's RetryAfter.
const maxHold = time.Hour

func holding(alias string) bool {
	holdMu.Lock()
	defer holdMu.Unlock()
	return now().Before(holdUntil[alias])
}

func holdOff(alias string, secs int) {
	holdMu.Lock()
	defer holdMu.Unlock()
	if secs <= 0 {
		delete(holdUntil, alias)
		return
	}
	holdUntil[alias] = now().Add(min(time.Duration(secs)*time.Second, maxHold))
}

// Flush sends what waits over the rate (Drain), expires the outbox's old
// entries, then delivers each ready peer's in sequence order, stopping
// at a peer's first entry that isn't taken now.
func Flush(ctx context.Context, t Transport) {
	Drain()
	ExpireRequests()
	root, err := outboxDir()
	if err != nil {
		return
	}
	peers, _ := os.ReadDir(root)
	for _, pd := range peers {
		if !pd.IsDir() || peer.CheckAlias(pd.Name()) != nil {
			continue
		}
		alias := pd.Name()
		files, _ := os.ReadDir(filepath.Join(root, alias))
		names := []string{}
		for _, f := range files {
			if strings.HasSuffix(f.Name(), ".json") && !f.IsDir() {
				names = append(names, f.Name())
			}
		}
		sort.Strings(names)
		ready := t != nil && t.Ready(alias) && !holding(alias)
		for _, name := range names {
			file := filepath.Join(root, alias, name)
			var e entry
			b, err := os.ReadFile(file)
			if err != nil || json.Unmarshal(b, &e) != nil {
				os.Remove(file)
				continue
			}
			if now().Sub(e.Queued) > Expiry {
				os.Remove(file)
				undelivered(alias, e.Msg, "expired")
				requestUndelivered(alias, e.Msg, "expired")
				continue
			}
			if !ready || ctx.Err() != nil {
				continue
			}
			var res proto.PeerMessageResult
			cctx, cancel := context.WithTimeout(ctx, callTimeout)
			err = t.Call(cctx, alias, proto.MethodPeerMessage, e.Msg, &res)
			cancel()
			switch {
			case err != nil, res.Status == proto.PeerRetry:
				ready = false // the rest wait, in order; expiry still runs
				if err == nil {
					holdOff(alias, res.RetryAfter)
				}
			case res.Status == proto.PeerDelivered, res.Status == proto.PeerDuplicate:
				os.Remove(file)
				delivered(alias, e.Msg)
				requestDelivered(alias, e.Msg)
			case res.Status == proto.PeerRefused:
				os.Remove(file)
				undelivered(alias, e.Msg, res.Code)
				requestUndelivered(alias, e.Msg, res.Code)
			default:
				ready = false
			}
		}
	}
}

// sender finds the sending project's copy of an outgoing message m to
// the server alias.
func sender(alias string, m proto.PeerMessage) (*project.Project, string, bool) {
	if m.Kind != proto.PeerMessageText || !project.ValidSlug(m.From) || !ValidID(m.ID) {
		return nil, "", false
	}
	p, err := project.Open(m.From)
	if err != nil {
		return nil, "", false
	}
	return p, alias + "/" + m.To, true
}

// delivered marks the sender's copy sent: the delivered receipt.
func delivered(alias string, m proto.PeerMessage) {
	p, key, ok := sender(alias, m)
	if !ok {
		return
	}
	update(p, key, m.ID, func(x *Message) {
		if x.State == StateQueued {
			x.State = StateSent
		}
	})
	p.Journal(caller.Caller{Kind: caller.Ticker}, "link.delivered", key, m.ID)
}

// undelivered marks the sender's copy undelivered and tells its inbox,
// in fixed words for code. A receipt that isn't taken goes quietly.
func undelivered(alias string, m proto.PeerMessage, code string) {
	p, key, ok := sender(alias, m)
	if !ok {
		return
	}
	words, known := RefusalWords[code]
	if !known {
		code, words = "refused", RefusalWords["refused"]
	}
	update(p, key, m.ID, func(x *Message) { x.State, x.Refused = StateUndelivered, code })
	why := replaceWords(words, key, p.Slug)
	p.AddMessageItem(KindUndelivered, key, fmt.Sprintf("your message %s to %s wasn't delivered: %s", m.ID, key, why), false, m.ID)
	p.Journal(caller.Caller{Kind: caller.Ticker}, "link.undelivered", key, m.ID+" ("+code+")")
}

// replaceWords fills a refusal's {to} and {from}.
func replaceWords(words, to, from string) string {
	return strings.NewReplacer("{to}", to, "{from}", from).Replace(words)
}

// inSeq is the highest sequence taken from a paired server, in its
// epoch.
type inSeq struct {
	Epoch string `json:"epoch"`
	Seq   int    `json:"seq"`
}

// clockSkew is how far ahead of this server's clock a message's time
// may be; one further is stamped now.
const clockSkew = 5 * time.Minute

// Receive takes a message or receipt from the paired server alias, whose
// server id is id (the link proved it). The message's words go to the
// message file only; the item and journal say fixed words. Whatever the
// reason, a project that doesn't take the message answers not-linked,
// so the answer tells nothing of the projects here.
func Receive(alias string, id peer.ID, m proto.PeerMessage) proto.PeerMessageResult {
	res := proto.PeerMessageResult{Status: proto.PeerRefused, Code: "bad-message"}
	idOK := ValidID(m.ID)
	if m.Kind == proto.PeerLinkRequest || m.Kind == proto.PeerLinkAnswer {
		idOK = ValidRequestID(m.ID)
	}
	if m.Epoch == "" || m.Seq < 1 || !project.ValidSlug(m.From) || !project.ValidSlug(m.To) || !idOK {
		return res
	}
	err := withStore(func(s *store) error {
		if in := s.In[alias]; in != nil && in.Epoch == m.Epoch && m.Seq <= in.Seq {
			res = proto.PeerMessageResult{Status: proto.PeerDuplicate}
			return nil
		}
		switch m.Kind {
		case proto.PeerMessageText:
			res = s.receive(alias, fingerprint(id), m)
		case proto.PeerMessageRead:
			res = s.receipt(alias, m)
		case proto.PeerLinkRequest:
			res = s.receiveRequest(alias, fingerprint(id), m)
		case proto.PeerLinkAnswer:
			res = s.receiveAnswer(alias, m)
		default:
			res = proto.PeerMessageResult{Status: proto.PeerRefused, Code: "bad-message"}
		}
		if res.Status != proto.PeerRetry {
			s.In[alias] = &inSeq{Epoch: m.Epoch, Seq: m.Seq}
		}
		return nil
	})
	if err != nil {
		return proto.PeerMessageResult{Status: proto.PeerRetry, Code: "refused"}
	}
	return res
}

// remoteIDRE is a remote message's id; the middle must be the sending
// server's fingerprint.
var remoteIDRE = regexp.MustCompile(`^M-([0-9A-F]{4})-[0-9]{1,12}$`)

// receive takes message m from project m.From on the server alias for
// project m.To here; the store is locked.
func (s *store) receive(alias, fp string, m proto.PeerMessage) proto.PeerMessageResult {
	bad := proto.PeerMessageResult{Status: proto.PeerRefused, Code: "bad-message"}
	if g := remoteIDRE.FindStringSubmatch(m.ID); g == nil || g[1] != fp {
		return bad
	}
	if m.By != "coordinator" && m.By != "human" {
		return bad
	}
	text := Clean(m.Text)
	if text == "" || len(text) > config.MaxLinkKB<<10 {
		return bad
	}
	key := alias + "/" + m.From
	notLinked := proto.PeerMessageResult{Status: proto.PeerRefused, Code: "not-linked"}
	cfg, err := config.Load()
	if err != nil {
		return proto.PeerMessageResult{Status: proto.PeerRetry, Code: "refused"}
	}
	to, err := target(cfg, m.To)
	if err != nil {
		return notLinked
	}
	if links, _ := cfg.Links(to.Slug); !links[key].Receives() {
		return notLinked
	}
	if _, err := os.Stat(path(to, key, m.ID)); err == nil {
		return proto.PeerMessageResult{Status: proto.PeerDuplicate}
	}
	t := now()
	if pr := s.Pairs[pairKey(key, to.Slug)]; pr != nil && pr.Paused {
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "link-paused"}
	}
	// This side's own limits: over the rate the sender's outbox keeps
	// it, in order, and tries again once a token is back here.
	l := limitsOf(cfg, to.Slug)
	if len(text) > l.maxText {
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "too-long"}
	}
	if b := s.refill(key, to.Slug, l, t); b.Tokens < 1 {
		wait := time.Duration((1 - b.Tokens) / float64(l.perHour) * float64(time.Hour))
		return proto.PeerMessageResult{Status: proto.PeerRetry, Code: "rate-limited", RetryAfter: int(wait.Seconds()) + 1}
	}
	created := m.Created.UTC().Truncate(time.Second)
	if created.IsZero() || created.After(t.Add(clockSkew)) {
		created = t.UTC().Truncate(time.Second)
	}
	re := ""
	if ValidID(m.Re) {
		if _, err := os.Stat(path(to, key, m.Re)); err == nil {
			re = m.Re
		}
	}
	in := &Message{ID: m.ID, Created: created, Re: re, Bytes: len(text), State: StateSent, By: m.By, Peer: key, Text: text}
	if err := write(to, in); err != nil {
		return proto.PeerMessageResult{Status: proto.PeerRetry, Code: "refused"}
	}
	to.AddMessageItem(KindMessage, key, Summary(key, in), false, in.ID)
	to.Journal(caller.ForPeer(alias), "link.receive", key, fmt.Sprintf("%s (%s)", in.ID, KB(in.Bytes)))
	s.account(m.By == "coordinator", key, to.Slug, l, t, side{to, key})
	return proto.PeerMessageResult{Status: proto.PeerDelivered}
}

// receipt marks message m.ID, which project m.To here sent to project
// m.From on the server alias, read; the store is locked.
func (s *store) receipt(alias string, m proto.PeerMessage) proto.PeerMessageResult {
	p, err := project.Open(m.To)
	if err != nil {
		return proto.PeerMessageResult{Status: proto.PeerDelivered}
	}
	err = update(p, alias+"/"+m.From, m.ID, func(x *Message) {
		if x.Out && (x.State == StateSent || x.State == StateQueued) {
			x.State = StateRead
		}
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return proto.PeerMessageResult{Status: proto.PeerRetry, Code: "refused"}
	}
	return proto.PeerMessageResult{Status: proto.PeerDelivered}
}

// Backlog is what waits for, or failed to reach, one paired server.
type Backlog struct {
	// Queued entries wait in the outbox for the server to take them.
	Queued int
	// Undelivered are messages of projects here that the server
	// refused, or didn't take within Expiry.
	Undelivered int
}

// Backlogs counts, by alias of paired server, the outbox's entries and
// the projects' undelivered messages; aliases with neither are left
// out. A tm doctor look: it reads every project's message folders.
func Backlogs() map[string]Backlog {
	out := map[string]Backlog{}
	if root, err := outboxDir(); err == nil {
		dirs, _ := os.ReadDir(root)
		for _, pd := range dirs {
			if !pd.IsDir() {
				continue
			}
			files, _ := os.ReadDir(filepath.Join(root, pd.Name()))
			for _, f := range files {
				if strings.HasSuffix(f.Name(), ".json") && !f.IsDir() {
					b := out[pd.Name()]
					b.Queued++
					out[pd.Name()] = b
				}
			}
		}
	}
	cfg, err := config.Load()
	if err != nil {
		return out
	}
	for _, slug := range cfg.ProjectSlugs() {
		p, err := project.Open(slug)
		if err != nil {
			continue
		}
		for _, other := range Peers(p) {
			alias, _, remote := config.Remote(other)
			if !remote {
				continue
			}
			msgs, _ := Log(p, other)
			for _, m := range msgs {
				if m.State == StateUndelivered {
					b := out[alias]
					b.Undelivered++
					out[alias] = b
				}
			}
		}
	}
	return out
}
