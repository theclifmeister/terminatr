//go:build unix

package session

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
)

// TestReadTailFIFO: a FIFO named as the transcript is never opened for
// a blocking read: setTail and readTail come back at once.
func TestReadTailFIFO(t *testing.T) {
	m, err := agent.ParseManifest([]byte(`manifest_version = 1
name = "a"
[launch]
command = "a"
[jsonl_tail]
path_field = "transcript_path"
[jsonl_tail.usage]
match = { type = "token_count" }
input = "n"
`))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "s1.jsonl")
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Skip(err)
	}
	rt, err := newAgentRT(AgentConfig{Agent: agent.FromManifest(m)}, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		rt.setTail(filepath.Dir(p), p)
		rt.readTail()
		// The open itself, past the Lstat: non-blocking, then refused.
		li, _ := os.Lstat(p)
		if f, _, err := openTail(filepath.Dir(p), p, li); err == nil {
			f.Close()
			t.Error("opened a FIFO")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("blocked on the FIFO")
	}
	_ = os.Remove(p)
}
