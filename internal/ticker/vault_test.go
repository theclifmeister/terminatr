package ticker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// TestRenderVault (T718, T719): a sweep renders the thread's note and
// the day's daily note once the vault is on, and not before.
func TestRenderVault(t *testing.T) {
	r := newRig(t)
	mdfile.WriteYAML(true)
	t.Cleanup(func() { mdfile.WriteYAML(false) })
	note := thread.Path(r.p, "t-0001", "t-0001.md")
	mdfile.Append(r.p.Path("JOURNAL.md"), []byte(r.now.Format(time.RFC3339)+" coordinator task.add T1 First\n"))
	root, _ := home.ProjectsDir()
	daily := filepath.Join(root, "_tm", "daily", r.now.Local().Format(time.DateOnly)+".md")
	r.sweep(time.Second)
	if _, err := os.Stat(note); err == nil {
		t.Fatal("a note before the vault is on")
	}
	if _, err := os.Stat(daily); err == nil {
		t.Fatal("a daily note before the vault is on")
	}
	d, _ := home.Dir()
	os.WriteFile(filepath.Join(d, "state", "vault"), nil, 0o600)
	r.sweep(time.Second)
	if _, err := os.Stat(note); err != nil {
		t.Fatalf("no thread note: %v", err)
	}
	if b, err := os.ReadFile(daily); err != nil || !strings.Contains(string(b), "1 task added: [[demo/tasks/T1|T1]]") {
		t.Fatalf("daily note: %v\n%s", err, b)
	}
}
