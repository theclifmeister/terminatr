package appapi

// Signed writes (app API 23, docs/SPEC.md §16.15): on a paired device's
// link every write carries a signature by the device's write key, a
// P-256 key that on a phone lives in the Secure Enclave and signs with
// no prompt. The bridge checks it before the request goes any further,
// so malware holding the device's link key alone acts on nothing. Each signature covers
// the method, a nonce the bridge gave out (device.nonce, good once and
// for nonceLife) and the params exactly as the request line has them.

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/device"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// MethodDeviceNonce gives a device a fresh nonce to sign its next write
// with (DeviceNonce): a device's bridge's own method, at the levels
// that write.
const MethodDeviceNonce = "device.nonce"

// ErrSignature answers a device's write that came without a valid
// signature by its write key over a nonce it was given (app API 23).
const ErrSignature = "device-signature"

// Nonce limits.
const (
	// NonceSize is a nonce's length, in bytes.
	NonceSize = 32
	// nonceLife is how long a nonce may wait for its write: the Face ID
	// prompt comes between.
	nonceLife = 2 * time.Minute
	// maxNonces bounds the nonces given out and not yet used; a new one
	// past it drops the oldest.
	maxNonces = 16
)

// Auth is a device's signature on its write, the request's auth: the
// nonce device.nonce gave and an ECDSA P-256 SHA-256 signature (ASN.1
// DER, as the Secure Enclave makes it) of SignedMessage.
type Auth struct {
	Nonce []byte `json:"nonce"`
	Sig   []byte `json:"sig"`
}

// DeviceNonce is device.nonce's result.
type DeviceNonce struct {
	Nonce []byte `json:"nonce"`
}

// signPrefix starts every signed message, so a write key's signature
// means nothing anywhere else.
const signPrefix = "tm-device-write v1\n"

// SignedMessage is what a device signs for a write of method with nonce
// and params, the bytes of the request's params as the line has them
// (none for a write without params):
// "tm-device-write v1\n" method "\n" SHA-256(nonce) SHA-256(params).
func SignedMessage(method string, nonce, params []byte) []byte {
	n, p := sha256.Sum256(nonce), sha256.Sum256(params)
	out := make([]byte, 0, len(signPrefix)+len(method)+1+2*sha256.Size)
	out = append(out, signPrefix...)
	out = append(out, method...)
	out = append(out, '\n')
	out = append(out, n[:]...)
	return append(out, p[:]...)
}

// nonces are the nonces a device's bridge gave out, each good once.
type nonces struct {
	mu    sync.Mutex
	given map[string]time.Time
}

// next is a fresh nonce.
func (ns *nonces) next(now time.Time) []byte {
	b := make([]byte, NonceSize)
	rand.Read(b)
	ns.mu.Lock()
	defer ns.mu.Unlock()
	if ns.given == nil {
		ns.given = map[string]time.Time{}
	}
	ns.sweep(now)
	for len(ns.given) >= maxNonces {
		var oldest string
		var at time.Time
		for k, t := range ns.given {
			if oldest == "" || t.Before(at) {
				oldest, at = k, t
			}
		}
		delete(ns.given, oldest)
	}
	ns.given[string(b)] = now
	return b
}

// take uses nonce up: whether it was given and is still good.
func (ns *nonces) take(nonce []byte, now time.Time) bool {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	ns.sweep(now)
	_, ok := ns.given[string(nonce)]
	delete(ns.given, string(nonce))
	return ok
}

func (ns *nonces) sweep(now time.Time) {
	for k, t := range ns.given {
		if now.Sub(t) > nonceLife {
			delete(ns.given, k)
		}
	}
}

// deviceNonce answers device.nonce.
func deviceNonce(b *bridge, _ struct{}) (DeviceNonce, error) {
	return DeviceNonce{Nonce: b.nonces.next(time.Now())}, nil
}

// signs says whether a device bridge at level checks signatures: the
// levels that write.
func signs(level string) bool { return level == device.LevelAnswer || level == device.LevelWrite }

// readsOnly says whether a write's request only reads, so it needs no
// signature: session.approve {check}, which reads the dialog off the
// screen and answers nothing (a phone checks each approval it shows, and
// Face ID for each would be noise).
func readsOnly(m Method, raw []byte) bool {
	if m.Name != proto.MethodSessionApprove {
		return false
	}
	p, err := m.toServer(raw)
	sp, ok := p.(proto.SessionApproveParams)
	return err == nil && ok && sp.Check
}

// checkAuth is why req, a write of a device's bridge, can't go on: nil
// when its auth is the device's signature over a nonce the bridge gave.
// The nonce is used up either way.
func (b *bridge) checkAuth(req Request) *Error {
	a := req.Auth
	if a == nil || len(a.Nonce) == 0 || len(a.Sig) == 0 {
		return &Error{Code: ErrSignature, Message: req.Method + " is an act: it needs a signature by the device's write key over a nonce from " + MethodDeviceNonce}
	}
	if !b.nonces.take(a.Nonce, time.Now()) {
		return &Error{Code: ErrSignature, Message: req.Method + ": the nonce isn't one this link gave, or was used or is older than 2 minutes; ask " + MethodDeviceNonce + " for a fresh one"}
	}
	key := b.o.WriteKey
	if key == nil {
		return &Error{Code: ErrSignature, Message: req.Method + ": this device has no write key; pair it again"}
	}
	h := sha256.Sum256(SignedMessage(req.Method, a.Nonce, req.Params))
	if !ecdsa.VerifyASN1(key, h[:], a.Sig) {
		return &Error{Code: ErrSignature, Message: req.Method + ": the signature isn't the device's write key's over this request"}
	}
	return nil
}
