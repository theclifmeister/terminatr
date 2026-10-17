package session

import "testing"

// TestInputQueueBounded: neither client input nor query answers grow the
// queue without limit while the program is not reading; answers keep some
// room past a full queue of input.
func TestInputQueueBounded(t *testing.T) {
	q := newInputQueue()
	chunk := make([]byte, 4<<10)
	for range 2 * maxInput / len(chunk) {
		q.push(chunk, false)
	}
	if len(q.buf) != maxInput {
		t.Fatalf("input queued %d, want %d", len(q.buf), maxInput)
	}
	answer := []byte("\x1b[24;80R")
	if !q.push(answer, true) || len(q.buf) != maxInput+len(answer) {
		t.Fatalf("answer not queued past full input: %d", len(q.buf))
	}
	for range 1 << 20 {
		q.push(answer, true)
	}
	if len(q.buf) > maxInput+answerRoom {
		t.Fatalf("answers queued %d, want at most %d", len(q.buf), maxInput+answerRoom)
	}
	if b, ok := q.pop(); !ok || len(b) == 0 {
		t.Fatal("pop")
	}
	q.close()
	if q.push(answer, true) {
		t.Fatal("push after close")
	}
}
