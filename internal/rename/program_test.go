package rename

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// programSetup is setup with program acme of demo and web.
func programSetup(t *testing.T, cfg string) (root string, g *program.Program) {
	t.Helper()
	root = setup(t)
	if _, err := project.New(project.Options{Slug: "web"}); err != nil {
		t.Fatal(err)
	}
	if cfg == "" {
		cfg = "[programs.acme]\nprojects = [\"demo\", \"web\"]\nauthority = \"delegate\"\n"
	}
	os.WriteFile(filepath.Join(root, "config.toml"), []byte(cfg), 0o600)
	g, err := program.New(program.Options{Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	return root, g
}

func TestCheckProgramRefuses(t *testing.T) {
	for _, c := range []struct {
		name, from, to, config string
		taken                  string
		want                   string
	}{
		{name: "unknown", from: "nope", to: "x", want: "unknown-program"},
		{name: "invalid", from: "acme", to: "Big One", want: "invalid-program"},
		{name: "unchanged", from: "acme", to: "acme", want: "unchanged"},
		{name: "a project's", from: "acme", to: "web", want: "slug-taken"},
		{name: "a paired server's", from: "acme", to: "mini", taken: "mini", want: "slug-taken"},
		{name: "a program's table", from: "acme", to: "side", config: "[programs.acme]\nprojects = [\"demo\"]\n[programs.side]\n", want: "program-exists"},
		{name: "paired members", from: "acme", to: "studio", config: "[programs.acme]\nprojects = [\"demo\", \"mini/ios\"]\n", want: "paired-members"},
		{name: "dotted keys", from: "acme", to: "studio", config: "programs.acme.paused = true\n", want: "settings-form"},
		{name: "ok", from: "acme", to: "studio"},
	} {
		t.Run(c.name, func(t *testing.T) {
			programSetup(t, c.config)
			o := ProgramOptions{From: c.from, To: c.to, Caller: human, Taken: func(s string) string {
				if s == c.taken {
					return "a paired server is called that"
				}
				return ""
			}}
			err := CheckProgram(o)
			if code(err) != c.want && !(c.want == "" && err == nil) {
				t.Fatalf("CheckProgram: %v (code %q), want %q", err, code(err), c.want)
			}
		})
	}
}

// TestRenameProgram: the folder, the table, the links into the program,
// the members' channel folders and the director's words on the members'
// boards all take the new name; a refused rename moves nothing.
func TestRenameProgram(t *testing.T) {
	root, g := programSetup(t, "")
	if _, err := g.Tasks().Add(human, []tasks.NewTask{{Title: "Ship"}}); err != nil {
		t.Fatal(err)
	}
	web, _ := project.Open("web")
	if _, err := web.Tasks().Add(human, []tasks.NewTask{{Title: "UI", Notes: "filed by the director of acme (acme/T1)\n\nacme/T1 is the user's."}}); err != nil {
		t.Fatal(err)
	}
	board := web.Path("TASKS.md")
	data, _ := os.ReadFile(board)
	os.WriteFile(board, []byte(strings.Replace(string(data), "status: open", "status: open · owner: director:acme", 1)), 0o644)
	os.MkdirAll(web.Path("messages", "acme"), 0o755)
	os.WriteFile(web.Path("messages", "acme", "M-1.md"), []byte("---\nfrom: \"[[_programs/acme/PROGRAM|acme]]\"\nre: \"[[web/messages/acme/M-0|M-0]]\"\n---\nhi\n"), 0o644)
	os.WriteFile(web.Path("memory", "x.md"), []byte("---\nsource: \"[[_programs/acme/tasks/T1|acme/T1]]\"\n---\nbody\n"), 0o644)

	res, err := Program(ProgramOptions{From: "acme", To: "studio", Caller: human})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Notes) > 0 {
		t.Errorf("notes: %v", res.Notes)
	}
	if _, err := os.Stat(filepath.Join(root, "projects", "_programs", "acme")); !os.IsNotExist(err) {
		t.Errorf("old folder: %v", err)
	}
	ng, err := program.Open("studio", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ng.Members, []string{"demo", "web"}) {
		t.Errorf("members %v", ng.Members)
	}
	cfg, _ := config.Load()
	if p, _ := cfg.Program("studio"); p.Authority != config.AuthorityDelegate {
		t.Errorf("studio %+v", p)
	}
	if cfg.ProgramOf("web") != "studio" {
		t.Errorf("web in %q", cfg.ProgramOf("web"))
	}
	b, _ := os.ReadFile(board)
	for _, want := range []string{"owner: director:studio", "filed by the director of studio (studio/T1)", "acme/T1 is the user's."} {
		if !strings.Contains(string(b), want) {
			t.Errorf("web's board has no %q:\n%s", want, b)
		}
	}
	msg, err := os.ReadFile(web.Path("messages", "studio", "M-1.md"))
	if err != nil || !strings.Contains(string(msg), "[[_programs/studio/PROGRAM|studio]]") || !strings.Contains(string(msg), "[[web/messages/studio/M-0|M-0]]") {
		t.Errorf("web's channel: %s, %v", msg, err)
	}
	if mem, _ := os.ReadFile(web.Path("memory", "x.md")); !strings.Contains(string(mem), "[[_programs/studio/tasks/T1|acme/T1]]") {
		t.Errorf("memory link: %s", mem)
	}
	if j, _ := os.ReadFile(ng.Path("JOURNAL.md")); !strings.Contains(string(j), "program.rename") {
		t.Errorf("journal:\n%s", j)
	}
}
