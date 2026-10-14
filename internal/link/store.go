package link

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
)

// store is <home>/state/links.json: the server-wide message counter,
// each direction's token bucket and each pair's breaker. It is read and
// written under its lock, so the server and a tm run without one agree.
type store struct {
	Next    int                `json:"next"`
	Buckets map[string]*bucket `json:"buckets,omitempty"` // "<from>><to>"
	Pairs   map[string]*pair   `json:"pairs,omitempty"`   // the two slugs, sorted, "a|b"
}

// bucket is one direction's token bucket (PerHour, Burst).
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
	return s
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

// refill is the bucket from → to at t.
func (s *store) refill(from, to string, t time.Time) *bucket {
	k := from + ">" + to
	b := s.Buckets[k]
	if b == nil {
		b = &bucket{Tokens: Burst, At: t}
		s.Buckets[k] = b
	}
	if el := t.Sub(b.At); el > 0 {
		b.Tokens = min(Burst, b.Tokens+el.Hours()*PerHour)
		b.At = t
	}
	return b
}

// every is how long one token takes to come back.
const every = time.Hour / PerHour

// allow refuses a message from → to at t while the breaker holds the
// link, and with rate, while the direction's bucket is empty.
func (s *store) allow(from, to string, rate bool, t time.Time) error {
	if p := s.Pairs[pairKey(from, to)]; p != nil && p.Paused {
		return refuse("link-paused", "the link with %s is paused after %d exchanges in an hour without the user; the user resumes it", to, Exchanges)
	}
	if !rate {
		return nil
	}
	if b := s.refill(from, to, t); b.Tokens < 1 {
		next := t.Add(time.Duration((1 - b.Tokens) * float64(every)))
		return refuse("rate-limited", "the link to %s takes %d messages an hour (%d at once); the next fits at %s", to, PerHour, Burst, next.Local().Format("15:04"))
	}
	return nil
}

// spend takes a token of from → to.
func (s *store) spend(from, to string, t time.Time) {
	s.refill(from, to, t).Tokens--
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
