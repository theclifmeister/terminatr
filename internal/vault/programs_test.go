package vault

import (
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/project"
)

// TestProgramsFolderIsTms: _programs/ is tm's folder (the programs
// design §3.4): a #for-tm line in it raises nothing, while the same line
// in the human's folder does, and a program's folder is never counted
// as a project.
func TestProgramsFolderIsTms(t *testing.T) {
	root, ps := vaultOn(t, "web")
	write(t, root, "_programs/acme/memory/plan.md", "- cache it #for-tm/web\n- or #for-tm\n")
	write(t, root, "_programs/acme/PROJECT.md", "# not a project\n")
	if got := vaultProjects(root); len(got) != 1 || got[0] != "web" {
		t.Fatalf("projects %v", got)
	}
	now := time.Now().Add(Settle + time.Second)
	if n, err := ScanHuman(now); err != nil || n != 0 {
		t.Fatalf("raised %d from _programs/: %v", n, err)
	}
	write(t, root, "Notes/plan.md", "- cache it #for-tm/web\n")
	if n, err := ScanHuman(now.Add(Settle + time.Second)); err != nil || n != 1 {
		t.Fatalf("raised %d from Notes/: %v", n, err)
	}
	if len(vaultItems(t, ps[0], project.KindFromObsidian)) != 1 {
		t.Fatal("no item in web")
	}
}
