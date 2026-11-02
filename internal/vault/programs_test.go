package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
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

// programOn makes program slug and turns programs on, or leaves them
// off, in config.toml.
func programOn(t *testing.T, slug string, on bool) *program.Program {
	t.Helper()
	d, err := home.Dir()
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(d, "config.toml"), []byte(fmt.Sprintf("[features]\nprograms = %v\n", on)), 0o600)
	p, err := program.New(program.Options{Slug: slug})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestProgramNotes: a program's board and designs get derived notes in
// _programs/<slug>/, linked there (the programs design §3.4), kept in the
// ledger apart from a project's, and only while programs are on.
func TestProgramNotes(t *testing.T) {
	root, ps := vaultOn(t, "acme-api")
	p := programOn(t, "acme", false)
	if got := Programs(nil); len(got) != 0 {
		t.Fatalf("programs off, yet the vault serves %v", got)
	}
	programOn(t, "studio", true)
	if got := Programs(nil); len(got) != 2 {
		t.Fatalf("programs on: %d", len(got))
	}
	who := caller.Caller{Kind: caller.Human}
	st := p.Tasks()
	if _, err := st.Add(who, []tasks.NewTask{{Title: "SSO everywhere"}, {Title: "sweep"}}); err != nil {
		t.Fatal(err)
	}
	title, design := "Rollout", "rollout"
	if _, _, err := p.SetDesign(design, project.DesignEdit{Title: &title}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetPlan(who, 2, &design, []string{"T1"}); err != nil {
		t.Fatal(err)
	}
	w, _, err := RenderProgram(p)
	if err != nil || w == 0 {
		t.Fatalf("rendered %d: %v", w, err)
	}
	stamp := ProgramStamp(p)
	if w, _, err := RenderProgram(p); err != nil || w != 0 || ProgramStamp(p) != stamp {
		t.Fatalf("a second render wrote %d (%v), or changed the stamp", w, err)
	}
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	t2 := read("_programs/acme/tasks/T2.md")
	for _, want := range []string{"[[_programs/acme/PROGRAM|acme]]", "[[_programs/acme/tasks/T1|T1]]", "[[_programs/acme/designs/rollout|rollout]]"} {
		if !strings.Contains(t2, want) {
			t.Errorf("T2's note has no %s:\n%s", want, t2)
		}
	}
	if strings.Contains(t2, "[[acme/") {
		t.Errorf("T2's note links a project acme:\n%s", t2)
	}
	if d := read("_programs/acme/designs/rollout.md"); !strings.Contains(d, "[[_programs/acme/tasks/T2|T2]]") {
		t.Errorf("design note:\n%s", d)
	}
	read("_programs/acme/designs/rollout.canvas")
	// A project's render leaves the program's notes alone, and the
	// program's ledger keys are its vault paths.
	if _, _, err := Render(ps[0]); err != nil {
		t.Fatal(err)
	}
	file, _ := ledgerPath()
	if l := readLedger(file); l.Files["_programs/acme/tasks/T1.md"] == "" {
		t.Fatalf("ledger %v", l.Files)
	}
}
