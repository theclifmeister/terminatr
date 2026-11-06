package e2e

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/appapi"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// TestSmokeProgramLibrary: a real director keeps a page with tm library add,
// and its Files view shows it first, with a member's files here and a paired
// member's under that project's pages switch; the filter and the kinds work
// across them, and the bridge lists and reads the same for the Mac. A file of
// the paired member reads through the view. Pages off, it goes.
func TestSmokeProgramLibrary(t *testing.T) {
	studio, mini := New(t), New(t)
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	pairDirect(t, studio, mini, "studio", "mini", "ws"+strings.TrimPrefix(hs.URL, "http"))

	studio.FakeClaude()
	tm := `"$TERMINATR_BIN" `
	studio.scripts(map[string]string{"keep": `
[[step]]
do = "run"
cmd = 'printf "<h1>The site</h1>" > site.html; ` + tm + `library add site.html; echo "add=$?"; ` + tm + `library add site.html --project web; echo "other=$?"; ` + tm + `library rm seat site.html; echo "rm=$?"; ` + tm + `library list; echo "list=$?"; echo KEPT""DONE'
`})
	webSlug, webDir := newProject(studio, "web")
	mini.MustCLI("project", "new", "far")
	writeConfig(t, studio, "[features]\nprograms = true\n[programs.acme]\nprojects = [\"web\", \"mini/far\"]\n")
	writeConfig(t, mini, "[projects.far]\nshare = true\n")
	studio.MustCLI("program", "new", "acme")
	studio.MustCLI("server", "start")
	mini.MustCLI("server", "start")
	waitPeerState(t, studio, "mini", "up", "")
	mini.MustCLI("peer", "pages", "studio", "far", "on")

	// A coordinator's own, here and on the paired server, and a stray in the
	// program folder's root, which stays out of the library.
	page := filepath.Join(t.TempDir(), "web-notes.md")
	os.WriteFile(page, []byte("# Web notes\n"), 0o644)
	studio.MustCLI("library", "add", page, "--project", webSlug)
	farPage := filepath.Join(t.TempDir(), "far-report.md")
	os.WriteFile(farPage, []byte("# Far report: quince\n"), 0o644)
	mini.MustCLI("library", "add", farPage, "--project", "far")

	t.Setenv("TERMINATR_HOME", studio.Home)
	g, err := program.Open("acme", nil)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(g.Path("stray.html"), []byte("<p>stray</p>"), 0o644)
	studio.Trust(g.Dir, webDir)
	id := strings.TrimSpace(studio.MustCLI("program", "open", "acme"))
	d := &Session{ID: id}
	info, _ := studio.Info(d)
	d.PID = info.PID
	studio.track(info.PID, "director "+id)
	studio.WaitState(d, "idle", agentWait)
	studio.Prompt(d, "run keep")
	studio.WaitFor(d, "KEPTDONE", agentWait)
	sc := studio.Screen(d)
	for _, want := range []string{"other-project", "human-only", "rm=1", "list=0", "seat  site.html"} {
		if !strings.Contains(sc, want) {
			t.Errorf("the director's screen lacks %q:\n%s", want, sc)
		}
	}
	if got, err := os.ReadFile(g.Path("library", "site.html")); err != nil || string(got) != "<h1>The site</h1>" {
		t.Fatalf("the library's copy: %q %v", got, err)
	}

	// What the Mac reads.
	type file struct{ Thread, Name, Project, Server string }
	bridge := runBridge(t, studio)
	listed := func() []file {
		var res appapi.Library
		if err := bridge.Call(proto.MethodLibraryList, appapi.LibraryListParams{Program: &appapi.LibraryProgram{Slug: "acme"}}, &res); err != nil {
			t.Fatal(err)
		}
		var out []file
		for _, f := range res.Files {
			out = append(out, file{f.Thread, f.Name, f.Project, f.Server})
		}
		return out
	}
	var files []file
	if !Poll(30*time.Second, func() bool { files = listed(); return len(files) == 3 }) {
		t.Fatalf("the program's library: %+v", files)
	}
	if files[0] != (file{"seat", "site.html", "", ""}) || files[1] != (file{"seat", "web-notes.md", "web", ""}) || files[2] != (file{"seat", "far-report.md", "far", "mini"}) {
		t.Fatalf("own first, then the members': %+v", files)
	}
	var chunk appapi.LibraryChunk
	if err := bridge.Call(proto.MethodLibraryRead, appapi.LibraryReadParams{Program: &appapi.LibraryProgram{Slug: "acme"}, ID: "seat", Name: "site.html"}, &chunk); err != nil || string(chunk.Data) != "<h1>The site</h1>" {
		t.Fatalf("the director's own: %+v %v", chunk, err)
	}
	if err := bridge.Call(proto.MethodPeerLibraryRead, appapi.PeerLibraryReadParams{Alias: "mini", Project: "far", ID: "seat", Name: "far-report.md"}, &chunk); err != nil || !strings.Contains(string(chunk.Data), "quince") {
		t.Fatalf("the paired member's: %+v %v", chunk, err)
	}

	// The TUI beside the director.
	const wait = 30 * time.Second
	w := studio.Window(160, 40)
	t.Cleanup(w.CloseWindow)
	w.WaitFor("SESSIONS", wait)
	y := -1
	w.WaitUntil("the director's row", wait, func(sc string) bool {
		y = slices.IndexFunc(strings.Split(sc, "\n"), func(l string) bool { return strings.Contains(l, "─ director") })
		return y >= 0
	})
	w.Click(8, y)
	w.WaitFor("F files", wait)
	w.Click(150, 37)
	w.Type("F")
	got := w.WaitUntil("the director's Attached", wait, func(s string) bool {
		return strings.Contains(s, "FILES") && strings.Contains(s, "site.html") && strings.Contains(s, "far-report.md")
	})
	for _, want := range []string{"Attached", "Director's files", "web-notes.md", "far on mini"} {
		if !strings.Contains(got, want) {
			t.Errorf("the view lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Worktree") || strings.Contains(got, "stray.html") {
		t.Errorf("a Worktree tab, or the stray:\n%s", got)
	}

	// Pages off there: the paired member goes, the rest stay.
	mini.MustCLI("peer", "pages", "studio", "far", "off")
	if !Poll(30*time.Second, func() bool { files = listed(); return len(files) == 2 }) {
		t.Fatalf("pages off: %+v", files)
	}
	w.WaitUntil("the paired member gone", wait, func(s string) bool {
		return strings.Contains(s, "site.html") && !strings.Contains(s, "far-report.md")
	})
}
