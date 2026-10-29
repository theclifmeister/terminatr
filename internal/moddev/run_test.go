package moddev

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modbroker"
	"github.com/theclifmeister/terminatr/internal/modproto"
)

const manifest = `id = "age"
name = "Age"
version = "0.1.0"
api = "1.0"
[provides]
main = "main.ts"
[[provides.columns]]
id = "age"
title = "Age"
width = 4
where = ["sidebar.thread"]
[permissions]
project_read = true
inbox = true
[settings.warn]
type = "int"
default = 2
`

const script = `
$.on("mod.ready", async () => { $.log("ready " + $.settings().warn); });
$.on("task.changed", async (e: any) => {
  const n = ((await $.store.get("n")) ?? 0) + 1;
  await $.store.set("n", n);
  const tasks = await $.tasks.list(e.project);
  $.ui.cell.set("age", { project: e.project, task: e.id }, [{ text: String(tasks.length), role: n > $.settings().warn ? "bad" : "faint" }]);
  if (e.to === "boom") throw new Error("boom");
});
$.on("clock.minute", () => { setTimeout(() => $.ui.status.set([{ text: "tick" }]), 1); });
`

func writeMod(t *testing.T, fixtures map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "age")
	files := map[string]string{"mod.toml": manifest, "main.ts": script}
	for n, f := range fixtures {
		files["tests/"+n] = f
	}
	for n, text := range files {
		p := filepath.Join(dir, filepath.FromSlash(n))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestRunDir(t *testing.T) {
	dir := writeMod(t, map[string]string{
		"pass.json": `{
  "name": "a task moving paints its cell",
  "settings": {"warn": 1},
  "store": {"n": 1},
  "answers": [{"method": "tasks.list", "params": {"slug": "demo"}, "result": [{"id": "T1"}, {"id": "T2"}]}],
  "events": [
    {"name": "task.changed", "project": "demo", "data": {"project": "demo", "id": "T1", "from": "todo", "to": "review"}},
    {"name": "clock.minute", "data": {}, "wait_ms": 50}
  ],
  "expect": {
    "calls": [
      {"method": "store.get", "params": {"key": "n"}},
      {"method": "ui.cell.set", "params": {"column": "age", "target": {"project": "demo", "task": "T1"}, "spans": [{"text": "2", "role": "bad"}]}},
      {"method": "ui.status.set"}
    ],
    "no_calls": ["inbox.raise"],
    "logs": ["ready 1"],
    "store": {"n": 2, "gone": null}
  }
}`,
		"fail.json": `{
  "grants": [],
  "events": [{"name": "clock.minute", "data": {}}],
  "expect": {"calls": [{"method": "ui.toast"}]}
}`,
		"throws.json": `{
  "events": [{"name": "task.changed", "project": "demo", "data": {"project": "demo", "id": "T1", "to": "boom"}}],
  "expect": {"calls": [{"method": "tasks.list"}], "logs": ["nothing"]}
}`,
		"refused.json": `{
  "grants": ["inbox"],
  "events": [{"name": "clock.minute", "data": {}}]
}`,
		"typo.json": `{"events": [{"name": "clock.minute"}], "expect": {"cals": []}}`,
	})
	res, err := RunDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Result{}
	for _, r := range res {
		got[r.File] = r
	}
	if r := got["tests/pass.json"]; !r.OK || r.Name != "a task moving paints its cell" {
		b, _ := json.MarshalIndent(r, "", "  ")
		t.Fatalf("pass.json: %s", b)
	}
	want := map[string][]string{
		"tests/fail.json":    {"no call ui.toast()"},
		"tests/throws.json":  {"1 handlers failed; expected 0", "task.changed rejected: Error: boom", `no log line has "nothing"`},
		"tests/refused.json": {},
		"tests/typo.json":    {`unknown field "cals"`},
	}
	for file, problems := range want {
		r, ok := got[file]
		if !ok {
			t.Fatalf("no result for %s", file)
		}
		if r.OK != (len(problems) == 0) {
			t.Errorf("%s: ok %v, problems %q", file, r.OK, r.Problems)
		}
		all := strings.Join(r.Problems, "\n")
		for _, p := range problems {
			if !strings.Contains(all, p) {
				t.Errorf("%s: problems %q lack %q", file, r.Problems, p)
			}
		}
	}
}

// TestDesignsFixture (mod API 1.10): a fixture sends designs.changed
// and answers designs.list; unanswered, designs.list lists nothing.
func TestDesignsFixture(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "phases")
	files := map[string]string{
		"mod.toml": `id = "phases"
name = "Phases"
version = "0.1.0"
api = "1.10"
[provides]
main = "main.ts"
[permissions]
project_read = true
`,
		"main.ts": `
$.on("designs.changed", async (e: any) => {
  const ds = await $.designs.list(e.project);
  $.ui.status.set(ds.length ? ds.map((d: any) => ({ text: d.name + " " + d.status + " " + d.tasks_done + "/" + d.tasks.length })) : null);
});
`,
		"tests/phases.json": `{
  "answers": [{"method": "designs.list", "params": {"slug": "demo"}, "result": [
    {"name": "net-view", "title": "Network view", "status": "building", "task": "T1", "tasks": ["T2", "T3"], "tasks_done": 1, "answers": [], "questions": []}
  ]}],
  "events": [{"name": "designs.changed", "project": "demo", "data": {"project": "demo"}}],
  "expect": {"calls": [{"method": "designs.list", "params": {"slug": "demo"}}, {"method": "ui.status.set", "params": {"spans": [{"text": "net-view building 1/2"}]}}]}
}`,
		"tests/none.json": `{
  "events": [{"name": "designs.changed", "project": "demo", "data": {"project": "demo"}}],
  "expect": {"calls": [{"method": "designs.list"}, {"method": "ui.status.set", "params": {"spans": null}}]}
}`,
	}
	for n, text := range files {
		p := filepath.Join(dir, filepath.FromSlash(n))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := RunDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("results %+v", res)
	}
	for _, r := range res {
		if !r.OK {
			t.Errorf("%s: %q", r.File, r.Problems)
		}
	}
}

func TestRunChecksTheFixture(t *testing.T) {
	m, err := Load(writeMod(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	no := []string{}
	r := m.Run(&Fixture{Grants: &no, Settings: map[string]any{"nope": 1},
		Events: []Step{{Name: "task.changed"}, {Name: "made.up"}, {Name: ToolStep, Data: json.RawMessage(`{"tool":"lint"}`)}},
		Expect: Expect{Calls: []Call{{Method: "ui.nothing"}}, Tools: []string{"a", "b"}}}, "f.json")
	all := strings.Join(r.Problems, "\n")
	for _, p := range []string{"nope: age has no such setting", "task.changed needs the project_read permission",
		`no event is called "made.up"`, `no method is called "ui.nothing"`, `the mod offers no tool "lint"`,
		"2 answers for 1 tool calls"} {
		if !strings.Contains(all, p) {
			t.Errorf("problems %q lack %q", r.Problems, p)
		}
	}
	if _, err := RunDir(writeMod(t, nil)); err == nil || !strings.Contains(err.Error(), "no fixtures") {
		t.Errorf("a mod without fixtures: %v", err)
	}
}

func TestBrokerRefusesWhatIsNotGranted(t *testing.T) {
	b := NewBroker(nil, mod.Permissions{}, nil, nil, map[string]string{"a.txt": "hi"})
	if _, e := b.Answer("tasks.list", json.RawMessage(`{"slug":"x"}`)); e == nil || !strings.Contains(e.Message, "project_read") {
		t.Errorf("tasks.list without project_read: %v", e)
	}
	if res, e := b.Answer("fs.read", json.RawMessage(`{"path":"a.txt"}`)); e != nil || string(res) != `"hi"` {
		t.Errorf("fs.read: %s %v", res, e)
	}
	if _, e := b.Answer("settings", nil); e == nil {
		t.Error("a local method reached the broker")
	}
	if n := len(b.Calls()); n != 3 {
		t.Errorf("%d calls recorded", n)
	}
}

func TestMatch(t *testing.T) {
	for _, c := range []struct {
		want, got string
		ok        bool
	}{
		{`{"a":1}`, `{"a":1,"b":2}`, true},
		{`{"a":1}`, `{"a":2}`, false},
		{`{"a":null}`, `{}`, true},
		{`[1,{"x":1}]`, `[1,{"x":1,"y":2}]`, true},
		{`[1]`, `[1,2]`, false},
		{`"s"`, `"s"`, true},
		{``, `{"a":1}`, true},
		{`{"a":1}`, ``, false},
	} {
		if got := Match(json.RawMessage(c.want), json.RawMessage(c.got)); got != c.ok {
			t.Errorf("Match(%s, %s) = %v", c.want, c.got, got)
		}
	}
}

// TestExamples: every code mod of examples/mods that has fixtures passes
// them (mods design §13: kept honest by CI).
func TestExamples(t *testing.T) {
	root := filepath.Join("..", "..", "examples", "mods")
	dirs, _ := filepath.Glob(filepath.Join(root, "*", "tests"))
	if len(dirs) == 0 {
		t.Fatal("no example has fixtures")
	}
	for _, d := range dirs {
		res, err := RunDir(filepath.Dir(d))
		if err != nil {
			t.Fatalf("%s: %v", d, err)
		}
		for _, r := range res {
			if !r.OK {
				t.Errorf("%s %s: %q", filepath.Base(filepath.Dir(d)), r.File, r.Problems)
			}
		}
	}
}

// TestBrokerToast: the fake takes ui.toast as the server does, and
// refuses what the server refuses.
func TestBrokerToast(t *testing.T) {
	b := NewBroker(nil, mod.Permissions{}, nil, nil, nil)
	for params, ok := range map[string]bool{
		`{"text":"hi"}`:                true,
		`{"text":"hi","level":"warn"}`: true,
		`{"text":"  "}`:                false,
		`{"text":"hi","level":"loud"}`: false,
	} {
		if _, err := b.Answer("ui.toast", json.RawMessage(params)); (err == nil) != ok {
			t.Errorf("%s: %v", params, err)
		}
	}
}

// TestBrokerRun: the fake refuses a process.run the broker refuses,
// even when the fixture answers it.
func TestBrokerRun(t *testing.T) {
	perms := mod.Permissions{Commands: []string{"git", "cat"}, Files: mod.Files{Read: []string{"~/notes"}}}
	answers := []Answer{{Method: "process.run", Result: json.RawMessage(`{"code":0}`)}}
	b := NewBroker([]string{mod.PermCommands, mod.PermFiles}, perms, answers, nil, nil)
	for params, ok := range map[string]bool{
		`{"argv":["git","status"]}`:                                       true,
		`{"argv":["git","log","-n5"],"options":{"cwd":"~/notes"}}`:        true,
		`{"argv":["cat","~/notes/a.txt"]}`:                                true,
		`{"argv":["cat","a.txt","../notes"],"cwd":"~/notes/sub"}`:         true,
		`{"argv":["sh","-c","git status"]}`:                               false,
		`{"argv":["git","-C","../../../../notes","status"]}`:              false,
		`{"argv":["git","--git-dir=x","status"]}`:                         false,
		`{"argv":["git","-c","core.pager=x","log"]}`:                      false,
		`{"argv":["git","fetch","--upload-pack=x"]}`:                      false,
		`{"argv":["git","config","core.pager","x"]}`:                      false,
		`{"argv":["cat","~/.ssh/id_rsa"]}`:                                false,
		`{"argv":["cat","/etc/passwd"]}`:                                  false,
		`{"argv":["cat","../../../../elsewhere/x"]}`:                      false,
		`{"argv":["cat","--file=~/elsewhere"]}`:                           false,
		`{"argv":["cat","a.txt"],"cwd":"~/elsewhere"}`:                    false,
		`{"argv":["cat","../../.terminatr/config.toml"],"cwd":"~/notes"}`: false,
		`{"argv":["cat","x/.git/config"],"cwd":"~/notes"}`:                false,
	} {
		if _, err := b.Answer("process.run", json.RawMessage(params)); (err == nil) != ok {
			t.Errorf("%s: %v", params, err)
		}
	}
	b = NewBroker([]string{mod.PermCommands}, perms, answers, nil, nil)
	if _, err := b.Answer("process.run", json.RawMessage(`{"argv":["cat","~/notes/a.txt"]}`)); err == nil {
		t.Error("a files root without the files grant")
	}
}

const clockScript = `
$.on("clock.minute", async (e: any, meta: any) => { $.log("tick " + e.now + " meta " + meta.time); });
$.on("task.changed", async (_e: any, meta: any) => { $.log("opened " + meta.time + " date " + new Date().toISOString() + " " + Date.now()); });
`

func TestPinnedNow(t *testing.T) {
	dir := writeMod(t, map[string]string{
		"pinned.json": `{
  "now": "2026-10-13T12:00:30Z",
  "events": [
    {"name": "clock.minute"},
    {"name": "clock.minute", "data": {"now": "2020-01-01T00:00:00Z"}},
    {"name": "task.changed", "project": "demo", "data": {"project": "demo", "id": "T1"}}
  ],
  "expect": {"logs": [
    "tick 2026-10-13T12:00:00Z meta 2026-10-13T12:00:30Z",
    "tick 2020-01-01T00:00:00Z meta 2026-10-13T12:00:30Z",
    "opened 2026-10-13T12:00:30Z date 2026-10-13T12:00:30.000Z 1791892830000"
  ]}
}`,
		"bad.json": `{"now": "yesterday", "events": [{"name": "clock.minute"}]}`,
	})
	if err := os.WriteFile(filepath.Join(dir, "main.ts"), []byte(clockScript), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := RunDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		switch r.File {
		case "tests/pinned.json":
			if !r.OK {
				t.Errorf("pinned.json: %q, logs %q", r.Problems, r.Logs)
			}
		case "tests/bad.json":
			if r.OK || !strings.Contains(strings.Join(r.Problems, "\n"), `now "yesterday" is not an RFC 3339 time`) {
				t.Errorf("bad.json: ok %v, problems %q", r.OK, r.Problems)
			}
		}
	}
}

// refuses runs one call and reports the code it was refused with, 0 for
// none.
func refuses(b *Broker, method, params string) int {
	if _, e := b.Answer(method, json.RawMessage(params)); e != nil {
		return e.Code
	}
	return 0
}

// TestBrokerFetch: the fake refuses an http.fetch the broker refuses,
// even when the fixture answers it.
func TestBrokerFetch(t *testing.T) {
	perms := mod.Permissions{Net: []string{"api.example.com", "*.cdn.example.org"}}
	answers := []Answer{{Method: "http.fetch", Result: json.RawMessage(`{"status":200}`)}}
	b := NewBroker([]string{mod.PermNet}, perms, answers, nil, nil)
	for _, c := range []struct {
		params string
		code   int
	}{
		{`{"url":"https://api.example.com/v1?q=1"}`, 0},
		{`{"url":"https://api.example.com:443/v1"}`, 0},
		{`{"url":"https://a.cdn.example.org/x","options":{"method":"GET"}}`, 0},
		{`{"url":"http://api.example.com/"}`, modproto.CodeDenied},
		{`{"url":"https://api.example.com:8443/"}`, modproto.CodeDenied},
		{`{"url":"https://other.example.com/"}`, modproto.CodeDenied},
		{`{"url":"https://cdn.example.org/"}`, modproto.CodeDenied},
		{`{"url":"https://user:pw@api.example.com/"}`, modproto.CodeDenied},
		{`{"url":"https://127.0.0.1/"}`, modproto.CodeDenied},
		{`{"url":"file:///etc/passwd"}`, modproto.CodeDenied},
		{`{"url":"https://api.example.com/` + strings.Repeat("a", 9000) + `"}`, modproto.CodeInvalidParams},
	} {
		if got := refuses(b, "http.fetch", c.params); got != c.code {
			t.Errorf("%.80s: code %d, want %d", c.params, got, c.code)
		}
	}
	// Without an answer a fetch the broker allows is the fixture's to say.
	b = NewBroker([]string{mod.PermNet}, perms, nil, nil, nil)
	if got := refuses(b, "http.fetch", `{"url":"https://api.example.com/"}`); got != modproto.CodeInternal {
		t.Errorf("no answer: code %d", got)
	}
}

// TestBrokerFiles: the fake checks fs.* paths as the broker does, and a
// file the fixture lacks is NotFound.
func TestBrokerFiles(t *testing.T) {
	perms := mod.Permissions{Files: mod.Files{Read: []string{"~/notes"}, Write: []string{"~/out"}}}
	files := map[string]string{"a.txt": "own", "~/notes/n.txt": "note"}
	b := NewBroker([]string{mod.PermFiles}, perms, nil, nil, files)
	for _, c := range []struct {
		method, params string
		code           int
	}{
		{"fs.read", `{"path":"a.txt"}`, 0},
		{"fs.read", `{"path":"~/notes/n.txt"}`, 0},
		{"fs.read", `{"path":"~/out/x"}`, modproto.CodeNotFound},
		{"fs.read", `{"path":"missing.txt"}`, modproto.CodeNotFound},
		{"fs.read", `{"path":"~/notes/missing.txt"}`, modproto.CodeNotFound},
		{"fs.read", `{"path":""}`, modproto.CodeInvalidParams},
		{"fs.read", `{"path":"~/elsewhere/x"}`, modproto.CodeDenied},
		{"fs.read", `{"path":"/etc/passwd"}`, modproto.CodeDenied},
		{"fs.read", `{"path":"../../../../x"}`, modproto.CodeDenied},
		{"fs.read", `{"path":"~/.ssh/id_rsa"}`, modproto.CodeDenied},
		{"fs.read", `{"path":"~/notes/.env"}`, modproto.CodeDenied},
		{"fs.read", `{"path":"~/notes/r/.git/config"}`, modproto.CodeDenied},
		{"fs.read", `{"path":"~/.terminatr/config.toml"}`, modproto.CodeDenied},
		{"fs.write", `{"path":"b.txt","text":"x"}`, 0},
		{"fs.write", `{"path":"~/out/o.txt","text":"x"}`, 0},
		{"fs.write", `{"path":"~/notes/n.txt","text":"x"}`, modproto.CodeDenied},
		{"fs.write", `{"path":"~/out/.env","text":"x"}`, modproto.CodeDenied},
		{"fs.write", `{"path":"~/.terminatr/config.toml","text":"x"}`, modproto.CodeDenied},
	} {
		if got := refuses(b, c.method, c.params); got != c.code {
			t.Errorf("%s %s: code %d, want %d", c.method, c.params, got, c.code)
		}
	}
	// What was written reads back, by any name for the same file.
	if res, e := b.Answer("fs.read", json.RawMessage(`{"path":"~/out/o.txt"}`)); e != nil || string(res) != `"x"` {
		t.Errorf("read back: %s %v", res, e)
	}
	// A files root is the grant's: without it only the own folder.
	b = NewBroker(nil, perms, nil, nil, files)
	if got := refuses(b, "fs.read", `{"path":"~/notes/n.txt"}`); got != modproto.CodeDenied {
		t.Errorf("a root without the files grant: code %d", got)
	}
	if got := refuses(b, "fs.read", `{"path":"a.txt"}`); got != 0 {
		t.Errorf("the own folder: code %d", got)
	}
}

// TestBrokerStore: the fake holds a store to the broker's key and size
// limits.
func TestBrokerStore(t *testing.T) {
	b := NewBroker(nil, mod.Permissions{}, nil, nil, nil)
	key := func(n int) string { return strings.Repeat("k", n) }
	for _, c := range []struct {
		method, params string
		code           int
	}{
		{"store.set", `{"key":"a","value":1}`, 0},
		{"store.get", `{"key":"a"}`, 0},
		{"store.del", `{"key":"a"}`, 0},
		{"store.set", `{"key":"` + key(256) + `","value":1}`, 0},
		{"store.set", `{"key":"` + key(257) + `","value":1}`, modproto.CodeInvalidParams},
		{"store.set", `{"key":"","value":1}`, modproto.CodeInvalidParams},
		{"store.set", `{"value":1}`, modproto.CodeInvalidParams},
		{"store.get", `{"key":"` + key(257) + `"}`, modproto.CodeInvalidParams},
		{"store.del", `{"key":""}`, modproto.CodeInvalidParams},
		{"store.set", `{"key":"big","value":"` + strings.Repeat("x", modbroker.MaxStore) + `"}`, modproto.CodeDenied},
	} {
		if got := refuses(b, c.method, c.params); got != c.code {
			t.Errorf("%s %.60s: code %d, want %d", c.method, c.params, got, c.code)
		}
	}
	if _, ok := b.Store()["big"]; ok {
		t.Error("a refused set was stored")
	}
	// The store as a whole is held to the limit: a second value that
	// tips it over is refused, and the first stays.
	half := `"` + strings.Repeat("x", modbroker.MaxStore/2) + `"`
	b = NewBroker(nil, mod.Permissions{}, nil, nil, nil)
	if got := refuses(b, "store.set", `{"key":"one","value":`+half+`}`); got != 0 {
		t.Fatalf("first half: code %d", got)
	}
	if got := refuses(b, "store.set", `{"key":"two","value":`+half+`}`); got != modproto.CodeDenied {
		t.Errorf("second half: code %d", got)
	}
	if got := refuses(b, "store.set", `{"key":"one","value":`+half+`}`); got != 0 {
		t.Errorf("replacing a value: code %d", got)
	}
}

// TestRunProjectSettings: a fixture's projects reach hello, so
// $.settings(project) is that project's settings over all projects'
// (mod API 1.11); a bad project value fails the fixture.
func TestRunProjectSettings(t *testing.T) {
	dir := writeMod(t, nil)
	if err := os.WriteFile(filepath.Join(dir, "main.ts"), []byte(`$.on("mod.ready", () => { $.log("warn " + $.settings().warn + " " + $.settings("demo").warn + " " + $.settings("other").warn); });`), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	f := &Fixture{Settings: map[string]any{"warn": float64(3)}, Projects: map[string]map[string]any{"demo": {"warn": float64(9)}},
		Events: []Step{{Name: "mod.ready", Data: json.RawMessage(`{"projects":[]}`)}}, Expect: Expect{Logs: []string{"warn 3 9 3"}}}
	if r := m.Run(f, "p.json"); !r.OK {
		t.Fatalf("%q %q", r.Problems, r.Logs)
	}
	f.Projects["demo"]["warn"] = "lots"
	if r := m.Run(f, "p.json"); r.OK || !strings.Contains(strings.Join(r.Problems, "\n"), "projects.demo: warn must be a whole number") {
		t.Errorf("a bad project value: %q", r.Problems)
	}
}

// TestRunRulesFixtures: a rules mod's fixtures check its text with its
// sections resolved for a project; a key it doesn't provide, or events
// for a data mod, fail.
func TestRunRulesFixtures(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gates")
	files := map[string]string{
		"mod.toml": "id = \"gates\"\nname = \"Gates\"\nversion = \"0.1.0\"\napi = \"1.11\"\n[provides]\nrules = { thread = \"t.md\" }\n[settings.gates]\ntype = \"bool\"\ndefault = false\n",
		"t.md":     "Work.\n<!-- tm:if gates -->\nStop at the gate.\n<!-- tm:end -->\n",
		"tests/a.json": `{"projects": {"g": {"gates": true}}, "rules": {
  "thread": {"project": "g", "contains": ["Work.", "Stop at the gate."]}}}`,
		"tests/b.json": `{"rules": {"thread": {"lacks": ["Stop"]}, "coordinator": {}}}`,
		"tests/c.json": `{"events": [{"name": "mod.ready"}]}`,
	}
	for n, text := range files {
		p := filepath.Join(dir, filepath.FromSlash(n))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := RunDir(dir)
	if err != nil || len(res) != 3 {
		t.Fatalf("%v %+v", err, res)
	}
	if !res[0].OK {
		t.Errorf("a: %q", res[0].Problems)
	}
	if res[1].OK || !strings.Contains(strings.Join(res[1].Problems, "\n"), "rules.coordinator: the mod provides no such rules") {
		t.Errorf("b: %q", res[1].Problems)
	}
	if res[2].OK || !strings.Contains(strings.Join(res[2].Problems, "\n"), "a data mod") {
		t.Errorf("c: %q", res[2].Problems)
	}
}
