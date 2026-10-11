package modhost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/modproto"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// A TS mod with imports, types, private fields, async/await and an async
// generator (which ES2017 lowers for goja) loads and runs.
var tsMod = map[string]string{
	"main.ts": `
import { hoursSince, type Task } from "./lib/age";
import limits from "./limits.json";

declare const $: any;

class Counter {
  #n = 0;
  next(): number { return ++this.#n; }
}
const seen = new Counter();

async function* ids(tasks: Task[]) {
  for (const t of tasks) yield t.id;
}

$.on("task.changed", async (ev: { tasks: Task[]; now: number }) => {
  const got: string[] = [];
  for await (const id of ids(ev.tasks)) got.push(id);
  const old = ev.tasks.filter((t) => hoursSince(t, ev.now) > limits.warn).map((t) => t.id);
  $.log(seen.next(), got.join(","), old.join(","), ev.tasks[0]?.title ?? "none");
});
`,
	"lib/age.ts": `
export interface Task { id: string; title?: string; since: number }
export const hoursSince = (t: Task, now: number): number => (now - t.since) / 3600;
`,
	"limits.json": `{"warn": 24}`,
}

func TestTypeScriptModRuns(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, tsMod)
	code, inputs, err := Bundle(filepath.Join(dir, "main.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 3 || inputs["lib/age.ts"] == "" || inputs["limits.json"] == "" {
		t.Fatalf("inputs %v", inputs)
	}
	s := startHost(t, code, time.Second)
	s.hello()
	id := s.event("task.changed", map[string]any{"now": 100 * 3600, "tasks": []map[string]any{
		{"id": "T1", "since": 0}, {"id": "T2", "since": 90 * 3600},
	}})
	if got := logText(s.method(modproto.MethodLog)); got != "1 T1,T2 T1 none" {
		t.Fatalf("log %q", got)
	}
	if r := s.eventResult(id); r.Failed != 0 {
		t.Fatalf("result %+v", r)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"main.ts": "const x: number = 1;", "b.js": "var y = 2;", "c.py": ""})
	if code, err := Load(filepath.Join(dir, "main.ts")); err != nil || strings.Contains(code, ": number") {
		t.Fatalf("ts: %q %v", code, err)
	}
	if code, err := Load(filepath.Join(dir, "b.js")); err != nil || code != "var y = 2;" {
		t.Fatalf("js: %q %v", code, err)
	}
	if _, err := Load(filepath.Join(dir, "c.py")); err == nil {
		t.Fatal("loaded a .py")
	}
}

func TestImportsStayInTheModFolder(t *testing.T) {
	base := t.TempDir()
	mod := filepath.Join(base, "mod")
	writeFiles(t, base, map[string]string{
		"secret.ts":    "export const s = 'secret';",
		"mod/ok.ts":    "export const ok = 1;",
		"mod/note.txt": "hi",
	})
	if err := os.Symlink(filepath.Join(base, "secret.ts"), filepath.Join(mod, "link.ts")); err != nil {
		t.Skip("no symlinks:", err)
	}
	cases := map[string]string{
		"bare":     `import x from "lodash"; console.log(x);`,
		"outside":  `import { s } from "../secret"; console.log(s);`,
		"absolute": `import { s } from "` + filepath.ToSlash(filepath.Join(base, "secret.ts")) + `"; console.log(s);`,
		"symlink":  `import { s } from "./link"; console.log(s);`,
		"text":     `import n from "./note.txt"; console.log(n);`,
	}
	for name, src := range cases {
		writeFiles(t, mod, map[string]string{"main.ts": `import { ok } from "./ok"; ` + src})
		if _, _, err := Bundle(filepath.Join(mod, "main.ts")); err == nil {
			t.Errorf("%s: bundled", name)
		}
	}
	// A type-only import of the API's types is dropped, not resolved.
	writeFiles(t, mod, map[string]string{"main.ts": `import type { Api } from "tm"; declare const $: Api; $.log(1);`})
	if _, _, err := Bundle(filepath.Join(mod, "main.ts")); err != nil {
		t.Errorf("type-only import: %v", err)
	}
}

func TestBuildCachesByContentHash(t *testing.T) {
	dir, cache := t.TempDir(), t.TempDir()
	writeFiles(t, dir, tsMod)
	entry := filepath.Join(dir, "main.ts")
	p1, err := Build(entry, cache)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(p1, old, old)

	p2, err := Build(entry, cache)
	if err != nil || p2 != p1 {
		t.Fatalf("second build %q %v, want %q", p2, err, p1)
	}
	if st, _ := os.Stat(p2); !st.ModTime().Equal(old) {
		t.Fatal("an unchanged mod was bundled again")
	}

	// An import changed: a new bundle, and the old one removed.
	writeFiles(t, dir, map[string]string{"limits.json": `{"warn": 48}`})
	p3, err := Build(entry, cache)
	if err != nil || p3 == p1 {
		t.Fatalf("after a change %q %v", p3, err)
	}
	if _, err := os.Stat(p1); !os.IsNotExist(err) {
		t.Fatal("the old bundle is still there")
	}
	code, err := Load(p3)
	if err != nil || !strings.Contains(code, "48") {
		t.Fatalf("bundle %v", err)
	}

	// A broken bundle file in the cache is rebuilt, not run.
	_ = os.WriteFile(p3, []byte("tampered"), 0o644)
	p4, err := Build(entry, cache)
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := Load(p4); code == "tampered" {
		t.Fatal("ran a bundle whose hash doesn't match")
	}

	// A build error leaves the cache as it was.
	writeFiles(t, dir, map[string]string{"main.ts": "import x from 'nope'; $.log(x);"})
	if _, err := Build(entry, cache); err == nil {
		t.Fatal("built a bad mod")
	}
}
