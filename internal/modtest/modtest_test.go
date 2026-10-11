package modtest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestRunChecksTheFixture(t *testing.T) {
	m, err := Load(writeMod(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	no := []string{}
	r := m.Run(&Fixture{Grants: &no, Settings: map[string]any{"nope": 1},
		Events: []Step{{Name: "task.changed"}, {Name: "made.up"}}, Expect: Expect{Calls: []Call{{Method: "ui.nothing"}}}}, "f.json")
	all := strings.Join(r.Problems, "\n")
	for _, p := range []string{"nope: the mod has no such setting", "task.changed needs the project_read permission",
		`no event is called "made.up"`, `no method is called "ui.nothing"`} {
		if !strings.Contains(all, p) {
			t.Errorf("problems %q lack %q", r.Problems, p)
		}
	}
	if _, err := RunDir(writeMod(t, nil)); err == nil || !strings.Contains(err.Error(), "no fixtures") {
		t.Errorf("a mod without fixtures: %v", err)
	}
}

func TestBrokerRefusesWhatIsNotGranted(t *testing.T) {
	b := NewBroker(nil, nil, nil, map[string]string{"a.txt": "hi"})
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
