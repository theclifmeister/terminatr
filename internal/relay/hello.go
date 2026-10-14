package relay

// The hello: the relay sends a challenge, the client answers with its
// key, the room it wants and a signature over both (docs/SPEC.md §16.2).

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// Version is the relay wire version, sent in the challenge.
const Version = 1

// Room kinds.
const (
	// KindLink is a room of two paired servers: its id is LinkRoom of
	// their two keys, which the relay checks.
	KindLink = "link"
	// KindPair is a pairing room: its id comes from the one-time pairing
	// code, which the relay never sees, so it can't check it; pair rooms
	// are capped in time and bytes.
	KindPair = "pair"
)

// Message types (the "t" field).
const (
	msgChallenge = "challenge"
	msgHello     = "hello"
	msgWait      = "wait"
	msgMated     = "mated"
)

// Error words: the close reason the relay sends.
const (
	ErrBadHello     = "bad-hello"
	ErrBadSignature = "bad-signature"
	ErrBadRoom      = "bad-room"
	ErrTimeout      = "timeout"
	ErrReplaced     = "replaced"
	ErrRoomFull     = "room-full"
	ErrPeerGone     = "peer-gone"
	ErrRateLimited  = "rate-limited"
	ErrPairLimit    = "pair-limit"
	ErrShutdown     = "shutdown"
	ErrProtocol     = "protocol"
)

// challenge is the relay's first message.
type challenge struct {
	T     string `json:"t"`
	V     int    `json:"v"`
	Nonce []byte `json:"nonce"` // 32 random bytes, base64
	Max   int    `json:"max"`   // the largest message the relay takes
}

// hello is the client's answer.
type hello struct {
	T    string `json:"t"`
	Kind string `json:"kind"`
	Key  []byte `json:"key"`            // Ed25519 public key, base64
	Room string `json:"room"`           // 64 hex digits
	Peer string `json:"peer,omitempty"` // link: the peer's fingerprint, 64 hex digits
	Sig  []byte `json:"sig"`            // Ed25519 over helloSigned, base64
}

// status is the relay's "wait" or "mated".
type status struct {
	T string `json:"t"`
}

// Fingerprint is a key's id: SHA-256 of the Ed25519 public key.
func Fingerprint(pub ed25519.PublicKey) [32]byte { return sha256.Sum256(pub) }

// LinkRoom is the room two paired servers meet in: the same for both,
// whichever dials, and checkable by the relay from either key and the
// other's fingerprint.
func LinkRoom(a, b [32]byte) [32]byte {
	if bytes.Compare(a[:], b[:]) > 0 {
		a, b = b, a
	}
	h := sha256.New()
	h.Write([]byte("tm-relay room v1\n"))
	h.Write(a[:])
	h.Write(b[:])
	var r [32]byte
	h.Sum(r[:0])
	return r
}

// helloSigned is what a hello's signature covers: the relay's nonce, so
// a hello can't be replayed, and every field of the hello.
func helloSigned(nonce []byte, kind string, room, peer []byte) []byte {
	var b bytes.Buffer
	b.WriteString("tm-relay hello v1\n")
	b.Write(nonce)
	b.WriteString(kind)
	b.WriteByte(0)
	b.Write(room)
	b.Write(peer)
	return b.Bytes()
}

// checked is a hello the relay accepted.
type checked struct {
	kind string
	fp   [32]byte
	room [32]byte
}

// checkHello verifies h against the nonce the relay sent. It returns the
// error word to close with.
func checkHello(h *hello, nonce []byte) (checked, string) {
	var c checked
	if h.T != msgHello || (h.Kind != KindLink && h.Kind != KindPair) || len(h.Key) != ed25519.PublicKeySize {
		return c, ErrBadHello
	}
	room, ok := hex32(h.Room)
	if !ok {
		return c, ErrBadHello
	}
	var peer []byte
	if h.Kind == KindLink {
		p, ok := hex32(h.Peer)
		if !ok {
			return c, ErrBadHello
		}
		peer = p[:]
	} else if h.Peer != "" {
		return c, ErrBadHello
	}
	if !ed25519.Verify(h.Key, helloSigned(nonce, h.Kind, room[:], peer), h.Sig) {
		return c, ErrBadSignature
	}
	c.kind, c.fp, c.room = h.Kind, Fingerprint(h.Key), room
	if h.Kind == KindLink {
		p := [32]byte(peer)
		if p == c.fp || LinkRoom(c.fp, p) != room {
			return c, ErrBadRoom
		}
	}
	return c, ""
}

func hex32(s string) ([32]byte, bool) {
	var r [32]byte
	if len(s) != 64 {
		return r, false
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return r, false
	}
	return [32]byte(b), true
}

var errNotHello = errors.New("relay: expected a hello")
