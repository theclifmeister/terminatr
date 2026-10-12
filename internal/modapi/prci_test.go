package modapi

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modproto"
)

// prciBroker answers the PR/CI mod's calls: its store holds the targets
// it drew, as a JSON value; project.get answers project.
func prciBroker(stored *json.RawMessage, project string) func(m *modproto.Message) (any, *modproto.Error) {
	return func(m *modproto.Message) (any, *modproto.Error) {
		switch m.Method {
		case "store.get":
			return *stored, nil
		case "store.set":
			var p struct {
				Value json.RawMessage `json:"value"`
			}
			_ = json.Unmarshal(m.Params, &p)
			*stored = p.Value
		case "project.get":
			return json.RawMessage(project), nil
		}
		return nil, nil
	}
}

// cells reads the mod's calls until n cell sets came, and returns them
// as "<thread> <spans>", and the other methods called.
func (b *broker) cells(n int) (sets, other []string) {
	b.t.Helper()
	for len(sets) < n {
		m := b.call()
		if m.Method != "ui.cell.set" {
			other = append(other, m.Method)
			continue
		}
		var p struct {
			Column string `json:"column"`
			Target struct {
				Project, Thread string
			} `json:"target"`
			Spans json.RawMessage `json:"spans"`
		}
		_ = json.Unmarshal(m.Params, &p)
		if p.Column != "ci" {
			b.t.Fatalf("column %q", p.Column)
		}
		sets = append(sets, p.Target.Project+"/"+p.Target.Thread+" "+string(p.Spans))
	}
	slices.Sort(sets)
	return sets, other
}

// TestPRCIColumnMod: the bundled PR/CI column, as tm embeds it, on the
// bindings: mod.ready draws each thread's checks from the snapshot and
// stores what it drew; pr.changed redraws one thread, and clears it
// when its PR has no checks; thread.resolved clears it; a restart
// clears the cells of threads gone meanwhile (the server showed them
// from its cells file), and leaves other projects' alone.
func TestPRCIColumnMod(t *testing.T) {
	m := mod.Bundled(mod.BundledPRCI, config.Mod{})
	script, err := m.ReadFile(m.Manifest.Provides.Main)
	if err != nil {
		t.Fatal(err)
	}
	stored := json.RawMessage("null")
	b := start(t, string(script), prciBroker(&stored, `{"project":"tm","threads":[{"id":"t-1","title":"","pr_checks":"fail"}]}`))
	if r := b.hello(""); !slices.Equal(r.Events, []string{"mod.ready", "pr.changed", "thread.resolved"}) {
		t.Fatalf("events %v", r.Events)
	}
	if r := b.event("mod.ready", `{"projects":[{"project":"tm","threads":[
		{"id":"t-1","title":"a","pr_checks":"pass"},
		{"id":"t-2","title":"b","pr_checks":"fail"},
		{"id":"t-3","title":"c","pr_checks":"pending"},
		{"id":"t-4","title":"d"}]}]}`); r.Failed != 0 {
		t.Fatalf("mod.ready %+v", r)
	}
	sets, other := b.cells(3)
	want := []string{
		`tm/t-1 [{"icon":"ci.pass","role":"good"}]`,
		`tm/t-2 [{"icon":"ci.fail","role":"bad"}]`,
		`tm/t-3 [{"icon":"ci.run","role":"faint"}]`,
	}
	if !slices.Equal(sets, want) || !slices.Equal(other, []string{"store.get"}) {
		t.Fatalf("cells %v, other calls %v", sets, other)
	}
	if m := b.call(); m.Method != "store.set" || string(stored) != `["tm/t-1","tm/t-2","tm/t-3"]` {
		t.Fatalf("%s: stored %s", m.Method, stored)
	}

	b.event("pr.changed", `{"project":"tm","thread":"t-4","number":4,"state":"OPEN","checks":"pending"}`)
	if sets, _ := b.cells(1); sets[0] != `tm/t-4 [{"icon":"ci.run","role":"faint"}]` {
		t.Fatalf("pr.changed: %v", sets)
	}
	b.call() // store.set
	b.event("pr.changed", `{"project":"tm","thread":"t-3","number":3,"state":"OPEN"}`)
	if sets, _ := b.cells(1); sets[0] != `tm/t-3 null` {
		t.Fatalf("no checks: %v", sets)
	}
	b.call()
	b.event("thread.resolved", `{"project":"tm","thread":"t-2","agent":"claude"}`)
	if sets, _ := b.cells(1); sets[0] != `tm/t-2 null` {
		t.Fatalf("resolved: %v", sets)
	}
	b.call()
	if string(stored) != `["tm/t-1","tm/t-4"]` {
		t.Fatalf("stored %s", stored)
	}
	// A pr.changed after a drop looks the project up again.
	if r := b.eventDropped("pr.changed", `{"project":"tm","thread":"t-1","number":1,"state":"OPEN","checks":"fail"}`); r.Failed != 0 {
		t.Fatalf("dropped %+v", r)
	}
	sets, other = b.cells(2)
	if !slices.Equal(other, []string{"project.get"}) || !slices.Equal(sets, []string{`tm/t-1 [{"icon":"ci.fail","role":"bad"}]`, `tm/t-4 null`}) {
		t.Fatalf("after a drop: %v %v", sets, other)
	}

	// A new host: the store remembers t-1, and a project it no longer
	// runs for; the snapshot has t-5 only.
	stored = json.RawMessage(`["gone/t-9","tm/t-1"]`)
	b2 := start(t, string(script), prciBroker(&stored, `{}`))
	b2.hello("")
	b2.event("mod.ready", `{"projects":[{"project":"tm","threads":[{"id":"t-5","title":"e","pr_checks":"pass"}]}]}`)
	sets, _ = b2.cells(3)
	if want := []string{`gone/t-9 null`, `tm/t-1 null`, `tm/t-5 [{"icon":"ci.pass","role":"good"}]`}; !slices.Equal(sets, want) {
		t.Fatalf("restart: %v", sets)
	}
	b2.call()
	if string(stored) != `["tm/t-5"]` {
		t.Fatalf("stored after restart %s", stored)
	}
}

// eventDropped is event, after the queue dropped one.
func (b *broker) eventDropped(name, data string) modproto.EventResult {
	b.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := b.c.Event(ctx, modproto.Event{Name: name, Project: "tm", Dropped: 1, Data: json.RawMessage(data)})
	if err != nil {
		b.t.Fatal(err)
	}
	return r
}
