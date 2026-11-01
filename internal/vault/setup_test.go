package vault

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// yamlHome is a temp home whose front matter has moved to YAML, with
// project demo.
func yamlHome(t *testing.T) (string, *project.Project) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(home.Env, dir)
	mdfile.WriteYAML(true)
	t.Cleanup(func() { mdfile.WriteYAML(false) })
	if _, err := Migrate(time.Now()); err != nil {
		t.Fatal(err)
	}
	p, err := project.New(project.Options{Slug: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	return dir, p
}

func readObj(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return m
}

func finding(fs []Finding, name string) Finding {
	for _, f := range fs {
		if f.Name == name {
			return f
		}
	}
	return Finding{}
}

// TestInit (T720): init merges tm's keys into the settings Obsidian
// already has, keeping the user's other keys and Sync as it is, writes
// the Bases, Home.md, the snippet and _human/, guards a report stored
// before the guard (backup first), turns the derived notes on and
// renders them; a later init keeps what the user changed since, and
// doctor says so. Off removes the notes and _tm/ and leaves .obsidian/.
func TestInit(t *testing.T) {
	dir, p := yamlHome(t)
	human := caller.Caller{Kind: caller.Human}
	if _, err := p.Tasks().Add(human, []tasks.NewTask{{Title: "One"}}); err != nil {
		t.Fatal(err)
	}
	r, _ := thread.Create(p, thread.Record{Title: "x", Task: "T1", State: thread.Running, Agent: "claude"})
	raw := "## Report\n<img src=\"https://x.y/a.png\">\n## Next\nMerge\n"
	os.WriteFile(thread.Path(p, r.ID, "REPORT.md"), []byte(raw), 0o644)
	root := filepath.Join(dir, "projects")
	obs := filepath.Join(root, ".obsidian")
	os.MkdirAll(obs, 0o755)
	os.WriteFile(filepath.Join(obs, "app.json"), []byte(`{"foo": 1, "alwaysUpdateLinks": true, "userIgnoreFilters": ["mine/"]}`), 0o644)
	os.WriteFile(filepath.Join(obs, "core-plugins.json"), []byte(`{"sync": true, "graph": false, "daily-notes": false}`), 0o644)

	if h := Health(); finding(h, "sync and publish").Status != "warn" || finding(h, "link auto-update").Status != "warn" || finding(h, "tm's settings").Status != "warn" || finding(h, "reports").Status != "warn" {
		t.Errorf("before init: %+v", h)
	}
	pl, err := NewPlan()
	if err != nil {
		t.Fatal(err)
	}
	var rels []string
	for _, c := range pl.Changes {
		rels = append(rels, c.Rel+": "+c.What)
	}
	for _, want := range []string{
		".obsidian/app.json: alwaysUpdateLinks, newFileFolderPath, newFileLocation, newLinkFormat, showUnsupportedFiles, useMarkdownLinks, userIgnoreFilters",
		".obsidian/core-plugins.json: backlink, bases, canvas, file-explorer, global-search, graph, outgoing-link, properties, tag-pane",
		".obsidian/graph.json: new", ".obsidian/types.json: new", ".obsidian/appearance.json: new", ".obsidian/snippets/tm.css: new",
		"_tm/bases/Tasks.base: new", "_tm/bases/Needs you.base: new", "_tm/Home.md: new",
	} {
		if !slices.Contains(rels, want) {
			t.Errorf("plan lacks %q:\n%s", want, strings.Join(rels, "\n"))
		}
	}
	if len(pl.Reports) != 1 || pl.Reports[0].Rel != "threads/"+r.ID+"/REPORT.md" {
		t.Fatalf("reports: %+v", pl.Reports)
	}
	n, err := Init(pl, human, time.Now())
	if err != nil || n != 3 || !On() { // the task's, the thread's and today's
		t.Fatalf("init: %d notes, %v, on %v", n, err, On())
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(DailyNote(time.Now().Format(time.DateOnly))))); err != nil {
		t.Errorf("no daily note: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "_tm", "Home.md")); !strings.Contains(string(b), "```base\nfilters:\n  and:\n    - file.inFolder(\"_tm/daily\")") {
		t.Errorf("Home.md has no days:\n%s", b)
	}
	app := readObj(t, filepath.Join(obs, "app.json"))
	if app["foo"] != float64(1) || app["alwaysUpdateLinks"] != false || app["newLinkFormat"] != "absolute" || app["newFileFolderPath"] != "_human" {
		t.Errorf("app.json: %v", app)
	}
	if f := app["userIgnoreFilters"].([]any); f[0] != "mine/" || len(f) != 1+len(IgnoreFilters) {
		t.Errorf("ignore filters: %v", f)
	}
	core := readObj(t, filepath.Join(obs, "core-plugins.json"))
	if core["sync"] != true || core["daily-notes"] != false || core["bases"] != true || core["graph"] != true || core["publish"] != nil {
		t.Errorf("core plugins: %v", core)
	}
	if types := readObj(t, filepath.Join(obs, "types.json"))["types"].(map[string]any); types["created"] != "datetime" || types["after"] != "multitext" {
		t.Errorf("types: %v", types)
	}
	graph := readObj(t, filepath.Join(obs, "graph.json"))
	if g := graph["colorGroups"].([]any); len(g) != len(GraphGroups) || g[1].(map[string]any)["query"] != "tag:#task" {
		t.Errorf("graph: %v", graph)
	}
	if a := readObj(t, filepath.Join(obs, "appearance.json")); !slices.Contains(a["enabledCssSnippets"].([]any), any("tm")) {
		t.Errorf("appearance: %v", a)
	}
	for _, rel := range []string{"_human", "_tm/Home.md", "_tm/bases/Messages.base", "demo/tasks/T1.md", "demo/threads/" + r.ID + "/" + r.ID + ".md"} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("%s: %v", rel, err)
		}
	}
	if b, _ := os.ReadFile(thread.Path(p, r.ID, "REPORT.md")); strings.Contains(string(b), "<img") {
		t.Errorf("report not guarded:\n%s", b)
	}
	if tars, _ := filepath.Glob(filepath.Join(dir, "state", "migrate", "report-guard-*.tar.gz")); len(tars) != 1 {
		t.Errorf("backup: %v", tars)
	}
	if lines, _, _ := p.JournalTail(5); !strings.Contains(strings.Join(lines, "\n"), "vault.guard demo 1 reports") {
		t.Errorf("journal:\n%s", strings.Join(lines, "\n"))
	}
	h := Health()
	for _, name := range []string{"derived notes", "link auto-update", "tm's settings", "tm's notes", "front matter", "reports"} {
		if f := finding(h, name); f.Status != "ok" {
			t.Errorf("after init, %s: %+v", name, f)
		}
	}
	if finding(h, "sync and publish").Status != "warn" {
		t.Error("Sync on, no warning")
	}

	// The user changes a key and a Base, and edits a task's note.
	app["newLinkFormat"] = "shortest"
	b, _ := json.Marshal(app)
	os.WriteFile(filepath.Join(obs, "app.json"), b, 0o644)
	os.WriteFile(filepath.Join(root, "_tm", "bases", "Tasks.base"), []byte("views: []\n"), 0o644)
	os.Chmod(filepath.Join(root, "demo", "tasks", "T1.md"), 0o644) // tm wrote it read-only
	os.WriteFile(filepath.Join(root, "demo", "tasks", "T1.md"), []byte("mine\n"), 0o644)
	os.WriteFile(filepath.Join(obs, "community-plugins.json"), []byte(`["obsidian-linter", "dataview"]`), 0o644)
	h = Health()
	if f := finding(h, "tm's settings"); f.Status != "warn" || !strings.Contains(f.Detail, "app.json newLinkFormat") {
		t.Errorf("drift: %+v", f)
	}
	if f := finding(h, "tm's notes"); f.Status != "warn" || !slices.Equal(f.Files, []string{"demo/tasks/T1.md"}) {
		t.Errorf("foreign edit: %+v", f)
	}
	if f := finding(h, "plugins"); f.Status != "warn" || !strings.Contains(f.Detail, "Linter") {
		t.Errorf("plugins: %+v", f)
	}
	pl, err = NewPlan()
	if err != nil {
		t.Fatal(err)
	}
	kept := map[string]string{}
	for _, c := range pl.Changes {
		kept[c.Rel] = c.What + "|" + c.Kept
	}
	if kept[".obsidian/app.json"] != "|your newLinkFormat" || !strings.HasPrefix(kept["_tm/bases/Tasks.base"], "|your edit") || len(kept) != 2 {
		t.Errorf("second plan: %v", kept)
	}
	if _, err := Init(pl, human, time.Now()); err != nil {
		t.Fatal(err)
	}
	if readObj(t, filepath.Join(obs, "app.json"))["newLinkFormat"] != "shortest" {
		t.Error("init overwrote the user's key")
	}

	removed, err := Off(human)
	if err != nil || removed != 2 || On() {
		t.Fatalf("off: %d removed, %v", removed, err)
	}
	// A render that waited for the ledger's lock writes nothing now, and
	// does again once the mark is back.
	if w, _, err := Render(p); err != nil || w != 0 {
		t.Fatalf("render after off: %d, %v", w, err)
	}
	os.WriteFile(filepath.Join(dir, "state", "vault"), nil, 0o600)
	if w, _, err := Render(p); err != nil || w != 2 {
		t.Fatalf("render with the mark back: %d, %v", w, err)
	}
	Off(human)
	for _, rel := range []string{"_tm", "demo/tasks/T1.md", "demo/tasks/.T1.md.lock"} {
		if _, err := os.Lstat(filepath.Join(root, rel)); err == nil {
			t.Errorf("%s left after off", rel)
		}
	}
	for _, rel := range []string{".obsidian/app.json", "_human", "demo/TASKS.md"} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("%s gone after off", rel)
		}
	}
	if st := readState(root); len(st.Files) != 1 { // the snippet
		t.Errorf("tm.json after off: %+v", st.Files)
	}
}

// TestBasesParse checks every Base against the rules Obsidian's own
// parser applies (1.14.4): a view has a string type and name, a groupBy
// has property and direction ASC or DESC (else the whole file fails),
// order and sort are lists, summaries name a summary per property; the
// filters are a string or one and/or/not of them.
func TestBasesParse(t *testing.T) {
	home := strings.TrimSuffix(strings.SplitN(recentDays, "```base\n", 2)[1], "```\n")
	for _, b := range append(bases([]string{"demo", "o'brien"}), base{"Home days", home}) {
		var doc struct {
			Filters  any               `yaml:"filters"`
			Formulas map[string]string `yaml:"formulas"`
			Views    []map[string]any  `yaml:"views"`
		}
		if err := yaml.Unmarshal([]byte(b.text), &doc); err != nil {
			t.Fatalf("%s: %v\n%s", b.name, err, b.text)
		}
		checkFilter(t, b.name, doc.Filters)
		if len(doc.Views) == 0 {
			t.Errorf("%s: no view", b.name)
		}
		names := map[string]bool{}
		for _, v := range doc.Views {
			name, _ := v["name"].(string)
			if typ, _ := v["type"].(string); name == "" || (typ != "table" && typ != "cards") || names[name] {
				t.Errorf("%s: view %v", b.name, v)
			}
			names[name] = true
			for k, val := range v {
				switch k {
				case "type", "name", "limit":
				case "filters":
					checkFilter(t, b.name+"/"+name, val)
				case "groupBy":
					g, _ := val.(map[string]any)
					if d := g["direction"]; g["property"] == nil || d != "ASC" && d != "DESC" {
						t.Errorf("%s/%s: groupBy %v", b.name, name, val)
					}
				case "order", "groupOrder":
					if _, ok := val.([]any); !ok {
						t.Errorf("%s/%s: %s not a list", b.name, name, k)
					}
				case "sort":
					for _, s := range val.([]any) {
						s := s.(map[string]any)
						if d := s["direction"]; s["property"] == nil || d != "ASC" && d != "DESC" {
							t.Errorf("%s/%s: sort %v", b.name, name, s)
						}
					}
				case "summaries":
					for p, s := range val.(map[string]any) {
						if _, ok := s.(string); !ok || p == "" {
							t.Errorf("%s/%s: summaries %v", b.name, name, val)
						}
					}
				default:
					t.Errorf("%s/%s: key %s", b.name, name, k)
				}
			}
		}
		if b.name == "Tasks" && !names["demo"] || b.name == "Memory" && !names["o'brien"] {
			t.Errorf("%s: no per-project view: %v", b.name, names)
		}
	}
}

// TestTasksBaseArchived (T872): every view of the Tasks Base leaves out
// archived tasks but Archived, which asks for them; the board's views
// leave out dropped ones, which have their own.
func TestTasksBaseArchived(t *testing.T) {
	var doc struct {
		Views []struct {
			Name    string `yaml:"name"`
			Filters any    `yaml:"filters"`
		} `yaml:"views"`
	}
	if err := yaml.Unmarshal([]byte(bases([]string{"demo"})[0].text), &doc); err != nil {
		t.Fatal(err)
	}
	flat := func(f any) string {
		if m, ok := f.(map[string]any); ok {
			var out []string
			for _, x := range m["and"].([]any) {
				out = append(out, x.(string))
			}
			return strings.Join(out, " && ")
		}
		s, _ := f.(string)
		return s
	}
	seen := map[string]string{}
	for _, v := range doc.Views {
		seen[v.Name] = flat(v.Filters)
	}
	for name, f := range seen {
		if want := name != "Archived"; strings.Contains(f, "archived != true") != want {
			t.Errorf("%s: %q", name, f)
		}
	}
	for name, want := range map[string]string{"Archived": "archived == true", "Board": `status != "dropped"`, "Cards": `status != "dropped"`, "Dropped": `status == "dropped"`} {
		if !strings.Contains(seen[name], want) {
			t.Errorf("%s: %q, want %s", name, seen[name], want)
		}
	}
}

func checkFilter(t *testing.T, where string, f any) {
	t.Helper()
	switch f := f.(type) {
	case nil, string:
	case map[string]any:
		if len(f) != 1 {
			t.Errorf("%s: filter %v", where, f)
		}
		for k, v := range f {
			list, ok := v.([]any)
			if k != "and" && k != "or" && k != "not" || !ok {
				t.Errorf("%s: filter %v", where, f)
			}
			for _, x := range list {
				checkFilter(t, where, x)
			}
		}
	default:
		t.Errorf("%s: filter %T", where, f)
	}
}

// TestLinksInside (T720): Links checks the links inside a project too,
// and names a task's note where a link from before T729 looks for it in
// tasks/archive; CrossLinks still doesn't.
func TestLinksInside(t *testing.T) {
	dir, p := yamlHome(t)
	os.WriteFile(filepath.Join(dir, "state", "vault"), nil, 0o600)
	os.MkdirAll(p.Path("memory"), 0o755)
	os.MkdirAll(p.Path("tasks"), 0o755)
	os.WriteFile(p.Path("tasks", "T5.md"), nil, 0o644)
	os.WriteFile(p.Path("memory", "x.md"), []byte("[[demo/memory/gone|gone]] [[demo/tasks/archive/T5|T5]] [[demo/tasks/T5|T5]] [[demo/PROJECT|demo]] [[bare]]\n"), 0o644)
	broken, err := Links()
	if err != nil || len(broken) != 2 || broken[0].Why != "no such note in project demo" || broken[1].Why != "task note moved: its note is demo/tasks/T5" {
		t.Fatalf("%v %+v", err, broken)
	}
	if cross, _ := CrossLinks(); len(cross) != 0 {
		t.Fatalf("cross: %+v", cross)
	}
}

// TestOffKeepsCanvases (T720): off removes a design's note and a paired
// server's stub with the other derived notes, and keeps a design's
// canvas, which holds the human's nodes and places too.
func TestOffKeepsCanvases(t *testing.T) {
	dir, _ := yamlHome(t)
	os.WriteFile(filepath.Join(dir, "state", "vault"), nil, 0o600)
	root := filepath.Join(dir, "projects")
	files := []string{"demo/designs/net.md", "demo/designs/net.canvas", "_peers/mini/acme-api.md"}
	l := ledger{Files: map[string]string{}}
	for _, rel := range files {
		os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755)
		os.WriteFile(filepath.Join(root, rel), []byte("x"), 0o644)
		l.Files[rel] = hash([]byte("x"))
	}
	b, _ := json.Marshal(&l)
	os.WriteFile(filepath.Join(dir, "state", "vault.json"), b, 0o600)
	if n, err := Off(caller.Caller{Kind: caller.Human}); err != nil || n != 2 {
		t.Fatalf("off: %d, %v", n, err)
	}
	for _, rel := range []string{"demo/designs/net.md", "_peers"} {
		if _, err := os.Lstat(filepath.Join(root, rel)); err == nil {
			t.Errorf("%s left", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "demo/designs/net.canvas")); err != nil {
		t.Errorf("canvas removed: %v", err)
	}
}

// TestTasksBasePriority (T927): the Tasks Base shows a task's priority
// and its Board and Cards sort by it, must first, through a rank
// formula (priority is text, which sorts could, must, should).
func TestTasksBasePriority(t *testing.T) {
	var doc struct {
		Formulas map[string]string `yaml:"formulas"`
		Views    []struct {
			Name  string   `yaml:"name"`
			Order []string `yaml:"order"`
			Sort  []struct {
				Property  string `yaml:"property"`
				Direction string `yaml:"direction"`
			} `yaml:"sort"`
		} `yaml:"views"`
	}
	if err := yaml.Unmarshal([]byte(bases([]string{"demo"})[0].text), &doc); err != nil {
		t.Fatal(err)
	}
	if f := doc.Formulas["rank"]; !strings.Contains(f, `priority == "must", 1`) || !strings.Contains(f, `priority == "could", 3, 4`) {
		t.Errorf("rank formula %q", f)
	}
	for _, v := range doc.Views {
		if v.Name != "Board" && v.Name != "Cards" {
			continue
		}
		if len(v.Sort) == 0 || v.Sort[0].Property != "formula.rank" || v.Sort[0].Direction != "ASC" || !slices.Contains(v.Order, "priority") {
			t.Errorf("%s: sort %+v, order %v", v.Name, v.Sort, v.Order)
		}
	}
}
