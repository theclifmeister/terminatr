package peer

// Pairing: two servers meet in a pair room on a relay, prove they both
// know a one-time code the relay never sees, and pin each other's key
// once both people confirmed six digits (docs/SPEC.md §16.8).

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/relay"
)

// SecretSize is the pairing secret's size: 80 bits.
const SecretSize = 10

// Secret is a pairing code's secret, T.
type Secret [SecretSize]byte

// crockford is Crockford's base32 alphabet: no I, L, O or U.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// String is the secret as people read it: 16 digits in four groups.
func (s Secret) String() string {
	var b strings.Builder
	var acc uint64
	bits := 0
	n := 0
	for _, c := range s {
		acc = acc<<8 | uint64(c)
		bits += 8
		for bits >= 5 {
			bits -= 5
			if n > 0 && n%4 == 0 {
				b.WriteByte('-')
			}
			b.WriteByte(crockford[acc>>bits&31])
			n++
		}
	}
	return b.String()
}

// parseSecret reads 16 Crockford digits, ignoring case, hyphens and
// spaces, with O for 0 and I or L for 1.
func parseSecret(s string) (Secret, bool) {
	var out Secret
	var acc uint64
	bits, i, n := 0, 0, 0
	for _, r := range strings.ToUpper(s) {
		switch r {
		case '-', ' ':
			continue
		case 'O':
			r = '0'
		case 'I', 'L':
			r = '1'
		}
		v := strings.IndexRune(crockford, r)
		if v < 0 {
			return out, false
		}
		n++
		if n > 16 {
			return out, false
		}
		acc = acc<<5 | uint64(v)
		bits += 5
		if bits >= 8 {
			bits -= 8
			out[i] = byte(acc >> bits)
			i++
		}
	}
	return out, n == 16
}

// Code is a one-time pairing code: the relay both sides meet on and the
// secret.
type Code struct {
	Relay  string // the relay's URL, as RelayURL gives it
	Secret Secret
}

// NewCode is a fresh code for the relay at relayURL (RelayURL's form).
func NewCode(relayURL string) (Code, error) {
	c := Code{Relay: relayURL}
	_, err := rand.Read(c.Secret[:])
	return c, err
}

// String is the code to pass on: the relay's host when it is the usual
// wss://host/v1, else its whole URL, with its pin when it has one
// (#sha256=…), then a slash and the secret. tm before pinned relays
// refuses a code with a pin ("a host name, or a wss:// URL").
func (c Code) String() string {
	r := c.Relay
	if u, err := url.Parse(r); err == nil && u.Scheme == "wss" && (u.Path == "" || u.Path == "/v1") {
		r = u.Host
		if u.Fragment != "" {
			r += "#" + u.Fragment
		}
	}
	return r + "/" + c.Secret.String()
}

// Pinned: the code's relay is taken by its key's pin.
func (c Code) Pinned() bool { return strings.Contains(c.Relay, "#") }

// ParseCode reads a code back.
func ParseCode(s string) (Code, error) {
	s = strings.TrimSpace(s)
	i := strings.LastIndexByte(s, '/')
	if i <= 0 {
		return Code{}, errors.New("a code is the relay, a slash and 16 digits: relay.example.net/7Q4M-K2XD-9HVA-PT3C")
	}
	sec, ok := parseSecret(s[i+1:])
	if !ok {
		return Code{}, fmt.Errorf("%q: the code's last part is 16 digits and letters (7Q4M-K2XD-9HVA-PT3C)", s[i+1:])
	}
	r, err := RelayURL(s[:i])
	if err != nil {
		return Code{}, err
	}
	return Code{Relay: r, Secret: sec}, nil
}

// RelayURL is s as the URL peers.json keeps: wss:// when s has no
// scheme, without a trailing slash or the default /v1. A relay without
// a CA-signed certificate ends in its key's pin, #sha256=<64 hex digits>
// (relay.PinPrefix), kept in lower case.
func RelayURL(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "://") {
		s = "wss://" + s
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "wss" && u.Scheme != "ws") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery {
		return "", fmt.Errorf("relay %q: a host name, or a wss:// URL", strings.TrimPrefix(s, "wss://"))
	}
	pin, err := relay.ParsePin(u.Fragment)
	if err != nil {
		return "", fmt.Errorf("relay %s: %v", u.Host, err)
	}
	if pin != "" && u.Scheme != "wss" {
		return "", fmt.Errorf("relay %s: a pin goes with wss:// only", u.Host)
	}
	u.Fragment, u.RawFragment = "", ""
	if pin != "" {
		u.Fragment = relay.PinPrefix + pin
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	if u.Path == "/v1" {
		u.Path = ""
	}
	return u.String(), nil
}

// pairRoom is the pair room T names; the relay can't get T from it.
func pairRoom(t Secret) [32]byte {
	return sha256.Sum256(append([]byte("tm-pair room v1\n"), t[:]...))
}

// Pairing roles.
const (
	rolePair = "pair"
	roleJoin = "join"
)

// pairProof is role's proof that it knows t, bound to both keys and to
// this TLS session (exp).
func pairProof(t Secret, role string, pairer, joiner ID, exp []byte) []byte {
	m := hmac.New(sha256.New, t[:])
	m.Write([]byte("tm-pair proof v1\n"))
	m.Write([]byte(role))
	m.Write([]byte{0})
	m.Write(pairer[:])
	m.Write(joiner[:])
	m.Write(exp)
	return m.Sum(nil)
}

// CheckDigits are the six digits both people compare: the same on both
// sides only when each holds the other's real key.
func CheckDigits(pairer, joiner ID) string {
	h := sha256.New()
	h.Write([]byte("tm-pair check v1\n"))
	h.Write(pairer[:])
	h.Write(joiner[:])
	n := binary.BigEndian.Uint64(h.Sum(nil)[:8]) % 1_000_000
	return fmt.Sprintf("%03d %03d", n/1000, n%1000)
}

// Offer is what a person confirms: the other server and the check.
type Offer struct {
	Name    string // its host name, as it said
	Version string // its tm release
	ID      ID
	Check   string
}

// Paired is the other server, both people having said yes.
type Paired struct {
	Name string
	Key  ed25519.PublicKey
}

// PairOptions configure a pairing.
type PairOptions struct {
	// Name and Version are this server's, sent to the other side.
	Name, Version string
	// RelayTLS is the outer TLS to the relay: nil trusts the system's CAs.
	RelayTLS *tls.Config
	// Confirm shows the offer and returns the person's answer.
	Confirm func(Offer) (bool, error)
	// Waiting is called once the pairer waits in the room: from then on
	// the code works, so that is when to show it.
	Waiting func()
}

// Pairing errors people see.
var (
	ErrNoPairer   = errors.New("nobody is pairing with this code: it was mistyped, has expired (10 minutes) or was used")
	ErrBadCode    = errors.New("the code doesn't match: the other side used another code, or someone in between doesn't know it")
	ErrDeclined   = errors.New("not paired: the digits weren't confirmed here")
	ErrPeerNo     = errors.New("not paired: the other side said no")
	ErrOwnKey     = errors.New("that's this server's own pairing")
	errPairFormat = errors.New("the other side sent something unexpected")
)

// pairTimeout bounds the exchange once mated: the relay's 2 minutes for
// a mated pair room.
const pairTimeout = 2 * time.Minute

// Pair waits in the pair room of code for the other side to join, then
// runs the pairing. ctx bounds the wait (the relay's 10 minutes, ^C).
func Pair(ctx context.Context, id *Identity, code Code, o PairOptions) (*Paired, error) {
	once := false
	c, err := dialPair(ctx, id, code, o, func() {
		if !once && o.Waiting != nil {
			o.Waiting()
		}
		once = true
	})
	if err != nil {
		return nil, err
	}
	return runPair(c, id, code.Secret, rolePair, o)
}

// Join joins the pairing code names. Nobody waiting there is
// ErrNoPairer, at once.
func Join(ctx context.Context, id *Identity, code Code, o PairOptions) (*Paired, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	waited := false
	c, err := dialPair(ctx, id, code, o, func() { waited = true; cancel() })
	if waited {
		return nil, ErrNoPairer
	}
	if err != nil {
		return nil, err
	}
	return runPair(c, id, code.Secret, roleJoin, o)
}

func dialPair(ctx context.Context, id *Identity, code Code, o PairOptions, onWait func()) (net.Conn, error) {
	d := relay.Dialer{Key: id.Key, Kind: relay.KindPair, Room: pairRoom(code.Secret), TLS: o.RelayTLS, OnWait: onWait}
	c, err := d.Dial(ctx, code.Relay)
	if err != nil {
		var ce *relay.CloseError
		if errors.As(err, &ce) && ce.Reason == relay.ErrTimeout {
			return nil, errors.New("nobody joined within 10 minutes; make a new code")
		}
		return nil, err
	}
	return c, nil
}

// pairMsg is one NDJSON line of the pairing.
type pairMsg struct {
	Proof   []byte `json:"proof,omitempty"`
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
	OK      *bool  `json:"ok,omitempty"`
}

func runPair(raw net.Conn, id *Identity, t Secret, role string, o PairOptions) (*Paired, error) {
	defer raw.Close()
	raw.SetDeadline(time.Now().Add(pairTimeout))
	cert, err := certificate(id)
	if err != nil {
		return nil, err
	}
	var other ed25519.PublicKey
	cfg := &tls.Config{
		MinVersion:             tls.VersionTLS13,
		MaxVersion:             tls.VersionTLS13,
		Certificates:           []tls.Certificate{cert},
		InsecureSkipVerify:     true, // the proof below is the verification
		VerifyPeerCertificate:  anyKey(&other),
		ClientAuth:             tls.RequireAnyClientCert,
		SessionTicketsDisabled: true,
	}
	var c *tls.Conn
	if role == roleJoin {
		c = tls.Client(raw, cfg)
	} else {
		c = tls.Server(raw, cfg)
	}
	if err := c.Handshake(); err != nil {
		return nil, fmt.Errorf("pairing: TLS: %w", err)
	}
	otherID := IDOf(other)
	if otherID == id.ID {
		return nil, ErrOwnKey
	}
	pairer, joiner, otherRole := id.ID, otherID, roleJoin
	if role == roleJoin {
		pairer, joiner, otherRole = otherID, id.ID, rolePair
	}
	cs := c.ConnectionState()
	exp, err := cs.ExportKeyingMaterial("tm-pair v1", nil, 32)
	if err != nil {
		return nil, err
	}
	enc := json.NewEncoder(c)
	dec := json.NewDecoder(bufio.NewReader(c))
	recv := func() (pairMsg, error) {
		var m pairMsg
		if err := dec.Decode(&m); err != nil {
			return m, fmt.Errorf("pairing: %w", err)
		}
		return m, nil
	}
	if err := enc.Encode(pairMsg{Proof: pairProof(t, role, pairer, joiner, exp)}); err != nil {
		return nil, err
	}
	m, err := recv()
	if err != nil {
		return nil, err
	}
	if !hmac.Equal(m.Proof, pairProof(t, otherRole, pairer, joiner, exp)) {
		return nil, ErrBadCode
	}
	if err := enc.Encode(pairMsg{Name: o.Name, Version: o.Version}); err != nil {
		return nil, err
	}
	if m, err = recv(); err != nil {
		return nil, err
	}
	if m.Proof != nil || m.OK != nil {
		return nil, errPairFormat
	}
	offer := Offer{Name: m.Name, Version: m.Version, ID: otherID, Check: CheckDigits(pairer, joiner)}
	yes, err := o.Confirm(offer)
	if err != nil {
		return nil, err
	}
	if err := enc.Encode(pairMsg{OK: &yes}); err != nil && yes {
		return nil, err
	}
	if !yes {
		return nil, ErrDeclined
	}
	if m, err = recv(); err != nil {
		return nil, err
	}
	if m.OK == nil {
		return nil, errPairFormat
	}
	if !*m.OK {
		return nil, ErrPeerNo
	}
	return &Paired{Name: offer.Name, Key: other}, nil
}

// anyKey takes the other end's one Ed25519 certificate, whatever its key,
// and records the key: pairing proves it with the code instead.
func anyKey(dst *ed25519.PublicKey) func([][]byte, [][]*x509.Certificate) error {
	return func(raw [][]byte, _ [][]*x509.Certificate) error {
		if len(raw) != 1 {
			return errNotPinned
		}
		c, err := x509.ParseCertificate(raw[0])
		if err != nil {
			return err
		}
		k, ok := c.PublicKey.(ed25519.PublicKey)
		if !ok {
			return errors.New("not an Ed25519 key")
		}
		*dst = k
		return nil
	}
}
