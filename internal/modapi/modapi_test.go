package modapi

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/modbus"
	"github.com/theclifmeister/terminatr/internal/modhost"
	"github.com/theclifmeister/terminatr/internal/modproto"
)

var update = flag.Bool("update", false, "rewrite tm.d.ts from the schema")

// tm.d.ts is TS(): `go test ./internal/modapi -update` rewrites it.
func TestTSCurrent(t *testing.T) {
	got := TS()
	if *update {
		if err := os.WriteFile("tm.d.ts", []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile("tm.d.ts")
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatal("tm.d.ts is not the schema's: run go test ./internal/modapi -update")
	}
}

// The schema's events are the bus's, with the bus's grants.
func TestEventsMatchBus(t *testing.T) {
	bus := []string{
		modbus.TaskChanged, modbus.ThreadState, modbus.ThreadStarted, modbus.ThreadResolved,
		modbus.ReportStored, modbus.InboxItem, modbus.PRChanged, modbus.SessionState, modbus.ClockMinute,
		modbus.UIClick, modbus.UIKey, modbus.ActionDone, modbus.ViewOpened, modbus.ViewClosed,
		modbus.CommandRun, modbus.ModSettings, modbus.ModReady,
	}
	var names []string
	for _, e := range Events {
		names = append(names, e.Name)
		if e.Needs != modbus.Needs(e.Name) {
			t.Errorf("%s needs %q here and %q on the bus", e.Name, e.Needs, modbus.Needs(e.Name))
		}
	}
	slices.Sort(bus)
	slices.Sort(names)
	if !slices.Equal(bus, names) {
		t.Errorf("events\n%v\nbus\n%v", names, bus)
	}
}

func TestSchema(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range Methods {
		if seen[m.Name] || m.Name == "on" || m.Name == "log" {
			t.Errorf("%s twice", m.Name)
		}
		seen[m.Name] = true
		if m.Doc == "" {
			t.Errorf("%s has no doc", m.Name)
		}
		optional := false
		for _, p := range m.Params {
			if optional && !p.Optional {
				t.Errorf("%s: %s is required after an optional param", m.Name, p.Name)
			}
			optional = p.Optional
		}
	}
	for _, n := range []string{"on", "log", "tool.handle", "project.get", "ui.view.set", "view.opened"} {
		if !Has(n) {
			t.Errorf("Has(%q) false", n)
		}
	}
	if Has("ui.section.set") || Has("ui") {
		t.Error("Has is too loose")
	}
}

// broker is the server's side for a test: it records the host's calls
// and answers them from answers, by method.
type broker struct {
	t      *testing.T
	c      *modproto.Client
	calls  chan *modproto.Message
	logs   chan string
	fails  chan string
	answer func(m *modproto.Message) (any, *modproto.Error)
	code   chan int
}

func start(t *testing.T, script string, answer func(m *modproto.Message) (any, *modproto.Error)) *broker {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	b := &broker{t: t, calls: make(chan *modproto.Message, 100), logs: make(chan string, 100),
		fails: make(chan string, 100), answer: answer, code: make(chan int, 1)}
	go func() {
		b.code <- modhost.Run(modhost.Options{Name: "main.js", Script: script, Budget: 2 * time.Second, In: inR, Out: outW, Bind: Bind})
		outW.Close()
	}()
	b.c = modproto.NewClient(outR, inW, func(m *modproto.Message) {
		b.calls <- m
		res, e := b.answer(m)
		if e != nil {
			_ = b.c.Conn().ReplyError(*m.ID, e.Code, e.Message)
			return
		}
		_ = b.c.Conn().Reply(*m.ID, res)
	}, func(m *modproto.Message) {
		switch m.Method {
		case modproto.MethodLog:
			var l modproto.Log
			_ = json.Unmarshal(m.Params, &l)
			b.logs <- l.Text
		case modproto.MethodHandlerFailed:
			var f modproto.HandlerFailed
			_ = json.Unmarshal(m.Params, &f)
			b.fails <- f.Error
		}
	})
	t.Cleanup(func() { inW.Close() })
	return b
}

func (b *broker) hello(settings string) modproto.HelloResult {
	b.t.Helper()
	h := modproto.Hello{API: "1.0", Mod: "test", Grants: []string{"project_read"}}
	if settings != "" {
		h.Settings = json.RawMessage(settings)
	}
	r, err := b.c.Hello(context.Background(), h)
	if err != nil {
		b.t.Fatal(err)
	}
	return r
}

func (b *broker) event(name, data string) modproto.EventResult {
	b.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := b.c.Event(ctx, modproto.Event{Name: name, Project: "tm", Data: json.RawMessage(data)})
	if err != nil {
		b.t.Fatal(err)
	}
	return r
}

func (b *broker) log() string {
	b.t.Helper()
	select {
	case s := <-b.logs:
		return s
	case f := <-b.fails:
		b.t.Fatalf("handler failed: %s", f)
	case <-time.After(5 * time.Second):
		b.t.Fatal("no log")
	}
	return ""
}

func (b *broker) call() *modproto.Message {
	b.t.Helper()
	select {
	case m := <-b.calls:
		return m
	case <-time.After(5 * time.Second):
		b.t.Fatal("no call")
	}
	return nil
}

func echo(m *modproto.Message) (any, *modproto.Error) {
	switch m.Method {
	case "project.get":
		return map[string]any{"project": "tm", "threads": []any{}}, nil
	case "store.get":
		return nil, nil
	case "fs.read":
		return nil, &modproto.Error{Code: -32010, Message: "outside the mod's folder"}
	}
	return nil, nil
}

// Each binding sends its wire method with its params named, the local
// ones answer from hello, and a broker error rejects with its code.
func TestBindings(t *testing.T) {
	b := start(t, `
		$.on("go", async () => {
			const p = await $.project.get("tm");
			$.log("project", p.project, Array.isArray(p.threads));
			$.log("store", await $.store.get("k"));
			await $.ui.cell.set("age", {project: "tm", task: "T1"}, null);
			await $.ui.toast("hi");
			await $.projects.list();
			try { await $.fs.read("/etc/passwd"); } catch (e) { $.log("fs", e.message, e.code); }
			$.log("settings", $.settings().warn, typeof $.settings);
			$.log("has", $.api.has("ui.view.set"), $.api.has("ui.section.set"), $.api.has("view.opened"));
			try { $.project.get(); } catch (e) { $.log("missing", e instanceof TypeError, e.message); }
			try { $.tasks.list(7); } catch (e) { $.log("type", e.message); }
		});
	`, echo)
	if r := b.hello(`{"warn": 48}`); !slices.Equal(r.Events, []string{"go"}) {
		t.Fatalf("hello %+v", r)
	}
	if r := b.event("go", `{}`); r.Handlers != 1 || r.Failed != 0 {
		t.Fatalf("go %+v", r)
	}
	want := []struct{ method, params string }{
		{"project.get", `{"slug":"tm"}`},
		{"store.get", `{"key":"k"}`},
		{"ui.cell.set", `{"column":"age","target":{"project":"tm","task":"T1"},"spans":null}`},
		{"ui.toast", `{"text":"hi"}`},
		{"projects.list", ``},
		{"fs.read", `{"path":"/etc/passwd"}`},
	}
	for _, w := range want {
		m := b.call()
		if m.Method != w.method || string(m.Params) != w.params {
			t.Errorf("call %s %s, want %s %s", m.Method, m.Params, w.method, w.params)
		}
	}
	for _, w := range []string{
		"project tm true",
		"store null",
		"fs outside the mod's folder -32010",
		"settings 48 function",
		"has true false true",
		"missing true $.project.get(slug): slug is missing",
		"type $.tasks.list(slug): slug is not a string",
	} {
		if got := b.log(); got != w {
			t.Errorf("log %q, want %q", got, w)
		}
	}
	select {
	case m := <-b.calls:
		t.Errorf("extra call %s", m.Method)
	default:
	}
}

// settings() is {} without settings, and a fresh copy each time.
func TestSettingsEmpty(t *testing.T) {
	b := start(t, `$.on("go", () => { const s = $.settings(); s.x = 1; $.log(JSON.stringify($.settings())); });`, echo)
	b.hello("")
	b.event("go", `{}`)
	if got := b.log(); got != "{}" {
		t.Fatalf("settings %q", got)
	}
}

// Bind refuses a $ path that runs into a built-in that isn't an object.
func TestBindClash(t *testing.T) {
	saved := Methods
	t.Cleanup(func() { Methods = saved })
	Methods = append(slices.Clone(saved), Method{Name: "log.extra", Doc: "x"})
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	code := make(chan int, 1)
	go func() {
		code <- modhost.Run(modhost.Options{Name: "main.js", Script: ``, In: inR, Out: outW, Bind: Bind})
		outW.Close()
	}()
	c := modproto.NewClient(outR, inW, nil, nil)
	defer inW.Close()
	_, err := c.Hello(context.Background(), modproto.Hello{Mod: "test"})
	if err == nil || !strings.Contains(err.Error(), "$.log is not an object") {
		t.Fatalf("hello: %v", err)
	}
	if got := <-code; got != modproto.ExitLoad {
		t.Fatalf("exit %d", got)
	}
}

// bundle copies a testdata mod and tm.d.ts to a temp folder and bundles
// it as tm mod host would.
func bundle(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	src, err := os.ReadFile(filepath.Join("testdata", name, "main.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "types"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "types", "tm.d.ts"), []byte(TS()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.ts"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	code, err := modhost.Load(filepath.Join(dir, "main.ts"))
	if err != nil {
		t.Fatal(err)
	}
	return code
}

// The design's example B, in TypeScript against tm.d.ts, runs on the
// bindings: mod.ready reads the board and paints a cell per task in
// review.
func TestReviewAgeExample(t *testing.T) {
	b := start(t, bundle(t, "review-age"), func(m *modproto.Message) (any, *modproto.Error) {
		switch m.Method {
		case "projects.list":
			return []Project{{Slug: "tm"}}, nil
		case "tasks.list":
			return []Task{{ID: "T1", Status: "review"}, {ID: "T2", Status: "ready"}}, nil
		case "store.get":
			return time.Now().Add(-3 * time.Hour).UnixMilli(), nil
		}
		return nil, nil
	})
	r := b.hello(`{"warn_after_hours": 2}`)
	if !slices.Equal(r.Events, []string{"clock.minute", "mod.ready", "task.changed"}) {
		t.Fatalf("events %v", r.Events)
	}
	if r := b.event("mod.ready", `{"projects":[]}`); r.Failed != 0 {
		t.Fatalf("mod.ready %+v", r)
	}
	var methods []string
	var cell *modproto.Message
	for cell == nil {
		m := b.call()
		methods = append(methods, m.Method)
		if m.Method == "ui.cell.set" {
			cell = m
		}
	}
	if !slices.Equal(methods, []string{"projects.list", "tasks.list", "store.get", "ui.cell.set"}) {
		t.Fatalf("calls %v", methods)
	}
	if want := `{"column":"age","target":{"project":"tm","task":"T1"},"spans":[{"text":"3h","role":"bad"}]}`; string(cell.Params) != want {
		t.Fatalf("cell %s", cell.Params)
	}
}

// The design's example C bundles against tm.d.ts and loads.
func TestAutoLabellerExample(t *testing.T) {
	b := start(t, bundle(t, "auto-labeller"), echo)
	if r := b.hello(""); !slices.Equal(r.Events, []string{"report.stored"}) {
		t.Fatalf("events %v", r.Events)
	}
	b.event("report.stored", `{"project":"tm","thread":"t-1","task":"T1","report":1,"sections":{"report":"see README","next":[]},"pr":"https://x/pull/1"}`)
	m := b.call()
	if want := `{"target":{"project":"tm","task":"T1"},"key":"labels","spans":[{"text":"docs","role":"info"},{"text":"no-check","role":"warn"}]}`; m.Method != "annotate" || string(m.Params) != want {
		t.Fatalf("call %s %s", m.Method, m.Params)
	}
	if m := b.call(); m.Method != "inbox.raise" || !strings.Contains(string(m.Params), `"subject":"T1"`) {
		t.Fatalf("call %s %s", m.Method, m.Params)
	}
}
