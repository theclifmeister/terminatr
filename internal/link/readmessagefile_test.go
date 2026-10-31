package link

import (
	"bytes"
	"testing"
)

// TestReadMessageFile: a message's attachment reads on either side, by
// the message and the other end, cut at max; a file offered but not
// received, a name the message hasn't and an unknown message don't.
func TestReadMessageFile(t *testing.T) {
	a, b, _ := setup(t)
	ca := coord
	ca.Project = "a"
	shot := libFile(t, a, "t-0001", "shot.png", 2<<10)
	libFile(t, a, "t-0001", "report.pdf", 4<<10)
	m, err := Send(ca, a, "b", Opts{Text: "see", Attach: []string{"t-0001/shot.png"}, File: "t-0001/report.pdf"})
	if err != nil {
		t.Fatal(err)
	}
	got, cut, err := ReadMessageFile(b, "a", m.ID, "shot.png", 1<<20)
	if err != nil || cut || !bytes.Equal(got, shot) {
		t.Fatalf("receiver: %d bytes, cut %v, %v", len(got), cut, err)
	}
	if got, cut, err = ReadMessageFile(a, "b", m.ID, "shot.png", 1<<20); err != nil || cut || !bytes.Equal(got, shot) {
		t.Fatalf("sender: %d bytes, cut %v, %v", len(got), cut, err)
	}
	if got, cut, err = ReadMessageFile(b, "a", m.ID, "shot.png", 10); err != nil || !cut || !bytes.Equal(got, shot[:10]) {
		t.Fatalf("cut: %d bytes, cut %v, %v", len(got), cut, err)
	}
	if _, _, err := ReadMessageFile(b, "a", m.ID, "report.pdf", 1<<20); err == nil {
		t.Error("read a file only offered")
	}
	if _, _, err := ReadMessageFile(b, "a", m.ID, "other.png", 1<<20); err == nil {
		t.Error("read a name the message hasn't")
	}
	if _, _, err := ReadMessageFile(b, "a", "M-999", "shot.png", 1<<20); err == nil {
		t.Error("read a file of an unknown message")
	}
}
