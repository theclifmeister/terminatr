package relay

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"
)

// Scanners on the relay's bare address fail the TLS handshake by the
// thousand (no SNI, plain HTTP, a hang-up), and net/http logs each. The
// errors of those kinds are only counted, and handshakeNoise logs them
// as one line an hour; an unknown handshake error (a certificate or
// ACME failure) is logged, up to unknownPerMinute, then counted too.
const (
	noiseEvery       = time.Hour
	unknownPerMinute = 10
	noiseMaxAddrs    = 4096 // distinct addresses counted, then "4096+"
)

const handshakePrefix = "http: TLS handshake error from "

// noiseKinds are the folded kinds (and "eof"): a substring of
// net/http's reason, and the kind's name in the summary.
var noiseKinds = []struct{ match, name string }{
	{"missing server name", "no server name"},
	{"client sent an HTTP request to an HTTPS server", "plain http"},
	{"connection reset by peer", "reset"},
	{"i/o timeout", "timeout"},
	{"unsupported versions", "old tls"},
	{"no supported versions", "old tls"},
	{"unsupported SSLv2", "old tls"},
	{"protocol version not supported", "old tls"},
	{"no cipher suite supported", "no shared cipher"},
}

// otherKind counts the unknown errors over the minute's limit.
const otherKind = "other"

type handshakeNoise struct {
	out func(string) // a line to the relay's log
	ips bool         // log addresses in what passes through
	now func() time.Time

	mu     sync.Mutex
	since  time.Time
	counts map[string]int
	addrs  map[netip.Addr]struct{}
	minute time.Time // the start of the minute unknown errors count in
	shown  int       // unknown errors logged in it
}

func newHandshakeNoise(out func(string), ips bool, now func() time.Time) *handshakeNoise {
	return &handshakeNoise{out: out, ips: ips, now: now, since: now(),
		counts: map[string]int{}, addrs: map[netip.Addr]struct{}{}}
}

// line takes one line of net/http's error log.
func (n *handshakeNoise) line(l string) {
	rest, ok := strings.CutPrefix(l, handshakePrefix)
	if !ok {
		n.pass(l)
		return
	}
	addr, reason, ok := strings.Cut(rest, ": ")
	if !ok {
		n.pass(l)
		return
	}
	kind := handshakeKind(reason)
	n.mu.Lock()
	if kind == "" {
		now := n.now()
		if now.Sub(n.minute) >= time.Minute {
			n.minute, n.shown = now, 0
		}
		if n.shown < unknownPerMinute {
			n.shown++
			n.mu.Unlock()
			n.pass(l)
			return
		}
		kind = otherKind
	}
	n.counts[kind]++
	if ap, err := netip.ParseAddrPort(addr); err == nil && len(n.addrs) < noiseMaxAddrs {
		n.addrs[ap.Addr().Unmap()] = struct{}{}
	}
	n.mu.Unlock()
}

// handshakeKind is the folded kind of a handshake error, "" for one to
// log.
func handshakeKind(reason string) string {
	if reason == "EOF" || reason == "unexpected EOF" || strings.HasSuffix(reason, ": EOF") {
		return "eof"
	}
	for _, k := range noiseKinds {
		if strings.Contains(reason, k.match) {
			return k.name
		}
	}
	return ""
}

func (n *handshakeNoise) pass(l string) {
	if !n.ips {
		l = hideAddrs(l)
	}
	n.out(l)
}

// flush logs what was counted since the last flush, if anything:
// "tls: 1043 handshakes refused in the last hour from 212 addresses
// (no server name 980, plain http 63)".
func (n *handshakeNoise) flush() {
	n.mu.Lock()
	now := n.now()
	counts, addrs, since := n.counts, len(n.addrs), n.since
	n.counts, n.addrs, n.since = map[string]int{}, map[netip.Addr]struct{}{}, now
	n.mu.Unlock()
	if len(counts) == 0 {
		return
	}
	total := 0
	kinds := make([]string, 0, len(counts))
	for k, c := range counts {
		total += c
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool {
		if counts[kinds[i]] != counts[kinds[j]] {
			return counts[kinds[i]] > counts[kinds[j]]
		}
		return kinds[i] < kinds[j]
	})
	parts := make([]string, len(kinds))
	for i, k := range kinds {
		parts[i] = fmt.Sprintf("%s %d", k, counts[k])
	}
	from := ""
	switch {
	case addrs >= noiseMaxAddrs:
		from = fmt.Sprintf(" from %d+ addresses", noiseMaxAddrs)
	case addrs == 1:
		from = " from 1 address"
	case addrs > 1:
		from = fmt.Sprintf(" from %d addresses", addrs)
	}
	plural := "s"
	if total == 1 {
		plural = ""
	}
	n.out(fmt.Sprintf("tls: %d handshake%s refused in the last %s%s (%s)",
		total, plural, period(now.Sub(since)), from, strings.Join(parts, ", ")))
}

// period is d for the summary: "hour", or minutes when it ends early.
func period(d time.Duration) string {
	m := max(int(d.Round(time.Minute)/time.Minute), 1)
	if m >= 60 {
		return "hour"
	}
	return fmt.Sprintf("%dm", m)
}

// run flushes every noiseEvery until stop closes, then once more.
func (n *handshakeNoise) run(stop <-chan struct{}) {
	t := time.NewTicker(noiseEvery)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			n.flush()
		case <-stop:
			n.flush()
			return
		}
	}
}
