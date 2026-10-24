package ticker

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// TestRenderVault (T718): a sweep renders the thread's note once the
// vault is on, and not before.
func TestRenderVault(t *testing.T) {
	r := newRig(t)
	mdfile.WriteYAML(true)
	t.Cleanup(func() { mdfile.WriteYAML(false) })
	note := thread.Path(r.p, "t-0001", "t-0001.md")
	r.sweep(time.Second)
	if _, err := os.Stat(note); err == nil {
		t.Fatal("a note before the vault is on")
	}
	d, _ := home.Dir()
	os.WriteFile(filepath.Join(d, "state", "vault"), nil, 0o600)
	r.sweep(time.Second)
	if _, err := os.Stat(note); err != nil {
		t.Fatalf("no thread note: %v", err)
	}
}
