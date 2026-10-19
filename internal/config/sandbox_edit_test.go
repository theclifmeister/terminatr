package config

import (
	"reflect"
	"strings"
	"testing"
)

// TestEditSandboxRule: an entry changes in place, keeping its place and
// its ask unless told; the new value is checked as an added one; all
// projects' entry carries the projects' drops of it along (beside it
// while another list keeps the old value); a project's new value that
// all projects give is refused.
func TestEditSandboxRule(t *testing.T) {
	write(t, accessMarker+`[defaults.sandbox]
excluded_commands = ["make a", { command = "docker compose *", ask = false }, "make c"]
hosts = ["a.dev"]
approved_commands = ["make c"]
guides = [{ title = "Old", body = "Text." }]

[projects.web.sandbox]
drop = ["docker compose *", "make c"]
hosts = ["web.dev"]

[projects.app.sandbox]
drop = ["a.dev"]
`)
	moved, err := EditSandboxRule(DefaultsTable, SandboxEdit{Key: SandboxExcluded, Old: "docker compose *", New: "docker compose up *"})
	if err != nil || !reflect.DeepEqual(moved, []string{"web"}) {
		t.Fatalf("moved %q, %v", moved, err)
	}
	c, _ := Load()
	all := c.SandboxOf("")
	if got := all.Lists[SandboxExcluded]; !reflect.DeepEqual(got, []string{"make a", "docker compose up *", "make c"}) || !reflect.DeepEqual(all.NoAsk, []string{"docker compose up *"}) {
		t.Errorf("all projects': %q, no ask %q", got, all.NoAsk)
	}
	if got := c.SandboxOf("web").Lists[SandboxDrop]; !reflect.DeepEqual(got, []string{"docker compose up *", "make c"}) {
		t.Errorf("web drops %q", got)
	}
	// make c is an approved command too: web drops both values.
	ask := false
	if _, err := EditSandboxRule(DefaultsTable, SandboxEdit{Key: SandboxExcluded, Old: "make c", New: "make d", Ask: &ask}); err != nil {
		t.Fatal(err)
	}
	c, _ = Load()
	if got := c.SandboxOf("web").Lists[SandboxDrop]; !reflect.DeepEqual(got, []string{"docker compose up *", "make c", "make d"}) {
		t.Errorf("web drops %q", got)
	}
	if got := c.ThreadSandbox("").WithoutAsking(); !reflect.DeepEqual(got, []string{"docker compose up *", "make d"}) {
		t.Errorf("without asking %q", got)
	}
	// A guide's title and text.
	body := "New text."
	if _, err := EditSandboxRule(DefaultsTable, SandboxEdit{Key: SandboxGuides, Old: "Old", New: "New", Body: &body}); err != nil {
		t.Fatal(err)
	}
	c, _ = Load()
	if got := c.ThreadSandbox("").Guides(); !reflect.DeepEqual(got, []Guide{{"New", "New text."}}) {
		t.Errorf("guides %q", got)
	}
	for _, bad := range []struct {
		table string
		e     SandboxEdit
		want  string
	}{
		{DefaultsTable, SandboxEdit{Key: SandboxExcluded, Old: "make a", New: "*"}, "every command"},
		{DefaultsTable, SandboxEdit{Key: SandboxExcluded, Old: "make a", New: "make d"}, "listed already"},
		{DefaultsTable, SandboxEdit{Key: SandboxExcluded, Old: "make z", New: "make y"}, "isn't listed"},
		{DefaultsTable, SandboxEdit{Key: SandboxHosts, Old: "a.dev", New: "https://b.dev"}, "host name"},
		{"projects.web", SandboxEdit{Key: SandboxHosts, Old: "web.dev", New: "a.dev"}, "all projects give a.dev already"},
		{"projects.app", SandboxEdit{Key: SandboxHosts, Old: "x.dev", New: "a.dev"}, "isn't listed"},
	} {
		if _, err := EditSandboxRule(bad.table, bad.e); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("%s %+v: %v", bad.table, bad.e, err)
		}
	}
	// app dropped a.dev, which is no other list's: the drop moves.
	if moved, err := EditSandboxRule(DefaultsTable, SandboxEdit{Key: SandboxHosts, Old: "a.dev", New: "b.dev"}); err != nil || !reflect.DeepEqual(moved, []string{"app"}) {
		t.Fatalf("moved %q, %v", moved, err)
	}
	c, _ = Load()
	if got := c.SandboxOf("app").Lists[SandboxDrop]; !reflect.DeepEqual(got, []string{"b.dev"}) {
		t.Errorf("app drops %q", got)
	}
}
