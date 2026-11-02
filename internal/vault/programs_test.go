package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

// fromObsidian lists the from-obsidian items of folder f, as
// "path|part", sorted.
func fromObsidian(t *testing.T, f *project.Folder) []string {
	t.Helper()
	items, err := f.Inbox()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, it := range items {
		if it.Kind == project.KindFromObsidian {
			out = append(out, it.Path+"|"+it.Part)
		}
	}
	slices.Sort(out)
	return out
}

// TestProgramRouting: _human/<slug>/, for: <slug>, #for-tm/<slug> and a
// card on a program's design canvas reach the program's inbox (the
// programs design §3.4) while programs are on; a bare #for-tm still goes
// to the vault's only project, never to a program. Off, nothing reaches
// a program, and a note for it is nobody's.
func TestProgramRouting(t *testing.T) {
	root, ps := vaultOn(t, "web")
	p := programOn(t, "acme", true)
	write(t, root, "_human/acme/idea.md", "# Idea\n")
	write(t, root, "_human/for.md", "---\nfor: acme\n---\nFor the program.\n")
	write(t, root, "Notes/plan.md", "- split the rollout #for-tm/acme\n- and cache #for-tm\n")
	write(t, root, "_programs/acme/designs/rollout.canvas", `{"nodes":[{"id":"tm-T1","type":"text","text":"#for-tm"},{"id":"h1","type":"text","text":"Reorder #for-tm"}]}`)
	write(t, root, "_programs/acme/memory/x.md", "- not read #for-tm/acme\n")
	n, err := ScanHuman(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Notes/plan.md|line", "_human/acme/idea.md|", "_human/for.md|", "_programs/acme/designs/rollout.canvas|card h1"}
	if got := fromObsidian(t, &p.Folder); n != 5 || !slices.Equal(got, want) {
		t.Fatalf("raised %d; acme has %q", n, got)
	}
	if got := fromObsidian(t, &ps[0].Folder); !slices.Equal(got, []string{"Notes/plan.md|line"}) {
		t.Fatalf("web has %q", got)
	}
	items, _ := p.Inbox()
	for _, it := range items {
		if raw, _ := os.ReadFile(p.Path("inbox", it.ID+".md")); strings.Contains(string(raw), "For the program") || strings.Contains(string(raw), "[[acme/") {
			t.Errorf("item file:\n%s", raw)
		}
	}

	// Off: a new note for acme raises nothing anywhere.
	programOff(t)
	write(t, root, "_human/acme/two.md", "# Two\n")
	write(t, root, "_human/for2.md", "---\nfor: acme\n---\nAgain.\n")
	if n, err := ScanHuman(time.Now()); err != nil || n != 0 {
		t.Fatalf("programs off, yet raised %d: %v", n, err)
	}
}

// programOff turns programs off in config.toml.
func programOff(t *testing.T) {
	t.Helper()
	d, _ := home.Dir()
	os.WriteFile(filepath.Join(d, "config.toml"), []byte("[features]\nprograms = false\n"), 0o600)
}
