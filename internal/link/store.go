package link

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// store is <home>/state/links.json: the server-wide message counter,
// each direction's token bucket and each pair's breaker. It is read and
// written under its lock, so the server and a tm run without one agree.
type store struct {
	Next    int                `json:"next"`
	Buckets map[string]*bucket `json:"buckets,omitempty"` // "<from>><to>"
	Pairs   map[string]*pair   `json:"pairs,omitempty"`   // the two slugs, sorted, "a|b"
	// Held are the messages waiting for the user's OK, by "<from>><to>",
	// so a look at the links reads no message files.
	Held map[string][]string `json:"held,omitempty"`
	// Queued are the messages over their direction's rate, by
	// "<from>><to>" in the order they go (queue.go).
	Queued map[string][]queued `json:"queued,omitempty"`
	// Epoch, Out and In order the outboxes to paired servers (remote.go):
	// this store's random epoch, the last sequence queued to each alias,
	// and the highest taken from each.
	Epoch string            `json:"epoch,omitempty"`
	Out   map[string]int    `json:"out,omitempty"`
	In    map[string]*inSeq `json:"in,omitempty"`
	// Requests are the link requests to and from paired servers
	// (request.go), the newest per project, peer and direction; ReqOut
	// and ReqIn the last hour's, by alias, for the rate.
	Requests []*Request             `json:"requests,omitempty"`
	ReqOut   map[string][]time.Time `json:"req_out,omitempty"`
	ReqIn    map[string][]time.Time `json:"req_in,omitempty"`
	// Shares are the share requests to and from paired servers
	// (share.go), the newest per alias and direction; ShareOut and
	// ShareIn the last hour's, by alias, for their own rate; ShareRev
	// counts their changes.
	Shares   []*proto.ShareRequest  `json:"shares,omitempty"`
	ShareRev int                    `json:"share_rev,omitempty"`
	ShareOut map[string][]time.Time `json:"share_out,omitempty"`
	ShareIn  map[string][]time.Time `json:"share_in,omitempty"`
}

// bucket is one direction's token bucket (limits).
type bucket struct {
	Tokens float64   `json:"tokens"`
	At     time.Time `json:"at"`
}

// pair is the breaker of the link between two projects: the
// coordinators' messages since the user's last input on it, and whether
// it tripped.
type pair struct {
	Paused bool        `json:"paused,omitempty"`
	Human  time.Time   `json:"human,omitzero"`
	Sent   []sentStamp `json:"sent,omitempty"`
}

type sentStamp struct {
	From string    `json:"from"`
	At   time.Time `json:"at"`
}

func storePath() (string, error) {
	d, err := home.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "state", "links.json"), nil
}

func loadStore(file string) *store {
	s := &store{}
	if b, err := os.ReadFile(file); err == nil {
		json.Unmarshal(b, s)
	}
	if s.Buckets == nil {
		s.Buckets = map[string]*bucket{}
	}
	if s.Pairs == nil {
		s.Pairs = map[string]*pair{}
	}
	if s.Held == nil {
		s.Held = map[string][]string{}
	}
	if s.Queued == nil {
		s.Queued = map[string][]queued{}
	}
	if s.Out == nil {
		s.Out = map[string]int{}
	}
	if s.In == nil {
		s.In = map[string]*inSeq{}
	}
	if s.ReqOut == nil {
		s.ReqOut = map[string][]time.Time{}
	}
	if s.ReqIn == nil {
		s.ReqIn = map[string][]time.Time{}
	}
	if s.ShareOut == nil {
		s.ShareOut = map[string][]time.Time{}
	}
	if s.ShareIn == nil {
		s.ShareIn = map[string][]time.Time{}
	}
	return s
}

// unhold takes message id off from → to's held list.
func (s *store) unhold(from, to, id string) {
	k := from + ">" + to
	s.Held[k] = slices.DeleteFunc(s.Held[k], func(x string) bool { return x == id })
	if len(s.Held[k]) == 0 {
		delete(s.Held, k)
	}
}

// withStore runs fn on the store under its lock and saves it when fn
// returns nil.
func withStore(fn func(s *store) error) error {
	file, err := storePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	unlock, err := mdfile.Lock(file)
	if err != nil {
		return err
	}
	defer unlock()
	s := loadStore(file)
	if err := fn(s); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteAtomic(file, append(b, '\n'), 0o600)
}

// readStore runs fn on the store as it is, unlocked: for a look.
func readStore(fn func(s *store)) {
	file, err := storePath()
	if err != nil {
		return
	}
	if _, err := os.Stat(file); errors.Is(err, fs.ErrNotExist) {
		fn(loadStore(""))
		return
	}
	fn(loadStore(file))
}

func (s *store) nextID() string {
	s.Next++
	return fmt.Sprintf("M-%d", s.Next)
}

func pairKey(a, b string) string {
	if b < a {
		a, b = b, a
	}
	return a + "|" + b
}

func (s *store) pair(a, b string) *pair {
	k := pairKey(a, b)
	if s.Pairs[k] == nil {
		s.Pairs[k] = &pair{}
	}
	return s.Pairs[k]
}

// refill is the bucket from → to at t, under limits l.
func (s *store) refill(from, to string, l limits, t time.Time) *bucket {
	k := from + ">" + to
	b := s.Buckets[k]
	if b == nil {
		b = &bucket{Tokens: float64(l.burst), At: t}
		s.Buckets[k] = b
	}
	if el := t.Sub(b.At); el > 0 {
		// to a millionth, so a token's time brings a whole one back
		b.Tokens = math.Round((b.Tokens+el.Hours()*float64(l.perHour))*1e6) / 1e6
		b.At = t
	}
	// A burst set lower since holds at once.
	b.Tokens = min(float64(l.burst), b.Tokens)
	return b
}

// allow refuses a message from → to while the breaker holds the link.
func (s *store) allow(from, to string) error {
	if p := s.Pairs[pairKey(from, to)]; p != nil && p.Paused {
		return refuse("link-paused", "the link with %s is paused after %d exchanges in an hour without the user; the user resumes it", to, Exchanges)
	}
	return nil
}

// spend takes a token of from → to.
func (s *store) spend(from, to string, l limits, t time.Time) {
	s.refill(from, to, l, t).Tokens--
}

// human records the user's input on the link: the breaker counts again
// from here.
func (s *store) human(a, b string, t time.Time) {
	p := s.pair(a, b)
	p.Human, p.Sent = t, nil
}

// count records a coordinator's message from → to and reports whether
// the breaker tripped on it: Exchanges messages each way within an hour,
// since the user's last input.
func (s *store) count(from, to string, t time.Time) bool {
	p := s.pair(from, to)
	keep := p.Sent[:0]
	for _, x := range p.Sent {
		if t.Sub(x.At) < time.Hour && x.At.After(p.Human) {
			keep = append(keep, x)
		}
	}
	p.Sent = append(keep, sentStamp{From: from, At: t})
	n := map[string]int{}
	for _, x := range p.Sent {
		n[x.From]++
	}
	if min(n[from], n[to]) >= Exchanges && !p.Paused {
		p.Paused = true
		return true
	}
	return false
}
