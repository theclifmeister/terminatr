package appapi

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// TestSignedMessageVector pins SignedMessage: the phone's
// SignedWrite.message (TerminatrKit, WriteSigningTests) checks the same
// digest, so the two can't drift apart.
func TestSignedMessageVector(t *testing.T) {
	nonce := make([]byte, NonceSize)
	for i := range nonce {
		nonce[i] = byte(i)
	}
	h := sha256.Sum256(SignedMessage("session.approve", nonce, []byte(`{"id":"s-1"}`)))
	if got := hex.EncodeToString(h[:]); got != signedVector {
		t.Fatalf("digest %s, want %s", got, signedVector)
	}
}

const signedVector = "a36611fd6e712e5976eb7e8d334133866a9f9b1ae21f59ad6dffe2c5e639e142"
