package server

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

const prAgeManifest = `id = "pr-age"
name = "PR age"
version = "0.1.0"
api = "1.11"
description = "How old each pull request is."
[provides]
main = "main.js"
[permissions]
project_read = true
[settings.days]
type = "int"
default = 3
min = 1
max = 9
label = "Days before a PR is old"
[settings.loud]
type = "bool"
default = false
`

// projectModsServer is a bare server with mod pr-age installed, off,
// the built-in mods off, and projects demo and web; what it logs goes
// to logs.
func projectModsServer(t *testing.T) (*Server, *project.Project, string, *bytes.Buffer) {
	t.Helper()
	tmHome := t.TempDir()
	t.Setenv("TERMINATR_HOME", tmHome)
	dir := filepath.Join(tmHome, "mods", "pr-age")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"mod.toml": prAgeManifest, "main.js": ""} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := "[mods.memory-tab]\nenabled = false\n[mods.tasks-tab]\nenabled = false\n[mods.ticker]\nenabled = false\n"
	if err := os.WriteFile(filepath.Join(tmHome, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	demo := newWatchProject(t)
	if _, err := project.New(project.Options{Slug: "web"}); err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	return &Server{log: log.New(logs, "", 0), modEv: newModEvents()}, demo, dir, logs
}

// prAge is pr-age as mods.list has it for project slug, with its
// consent's lines.
func prAge(t *testing.T, s *Server, slug string) proto.ProjectMod {
	t.Helper()
	res, perr := s.modsList(proto.ModsListParams{Project: slug, Consent: "pr-age"})
	if perr != nil {
		t.Fatal(perr)
	}
	for _, m := range res.(proto.ModsListResult).Mods {
		if m.ID == "pr-age" {
			return m
		}
	}
	t.Fatalf("no pr-age in %+v", res)
	return proto.ProjectMod{}
}

func perrCode(perr *proto.Error) string {
	if perr == nil {
		return ""
	}
	return perr.Code
}

// TestProjectMods: mods.list says where a mod runs for the project and
// what turning it on asks, the consent in the TUI's lines; mods.set
// turns it on only for the key of what was shown (none, another, or one
// from before the mod asked for more is refused), journals the change as
// its caller and logs a device's; turning it off for the project and its
// settings for the project follow the Mods tab; an agent is refused.
func TestProjectMods(t *testing.T) {
	s, demo, dir, logs := projectModsServer(t)
	phone := callCtx{device: "Sam's iPhone", app: true, who: func() caller.Caller { return caller.ForDevice("Sam's iPhone") }}

	m := prAge(t, s, "demo")
	if m.Runs || m.Where != "follows all projects: off" || m.Toggle.Do != "consent" || m.Toggle.Question != "Enable PR age for demo?" ||
		m.Toggle.Key == "" || m.Description != "How old each pull request is." || m.Own != "settings: all projects" || len(m.Settings) != 2 {
		t.Fatalf("pr-age off: %+v", m)
	}
	var text []string
	for _, l := range m.Toggle.Consent {
		var b strings.Builder
		for _, sp := range l.Spans {
			b.WriteString(sp.Text)
		}
		text = append(text, b.String())
	}
	if all := strings.Join(text, "\n"); !strings.HasPrefix(all, "PR age 0.1.0 (pr-age)") || !strings.Contains(all, "It may:") ||
		!strings.HasSuffix(all, "\nEnable PR age for demo?") {
		t.Fatalf("consent:\n%s", all)
	}
	// Without a list's consent, its lines stay home.
	res, _ := s.modsList(proto.ModsListParams{Project: "demo"})
	for _, o := range res.(proto.ModsListResult).Mods {
		if o.ID == "pr-age" && (len(o.Toggle.Consent) != 0 || o.Toggle.Key != m.Toggle.Key) {
			t.Fatalf("listed without consent: %+v", o.Toggle)
		}
	}

	for _, bad := range []proto.ModsSetParams{
		{Project: "demo", Mod: "pr-age", On: true},
		{Project: "demo", Mod: "pr-age", On: true, Key: "0123"},
		{Project: "demo", Mod: "pr-age", On: false, Key: m.Toggle.Key},
	} {
		if _, perr := s.modsSet(bad, phone); perrCode(perr) != proto.ErrModChanged {
			t.Errorf("mods.set %+v: %v", bad, perr)
		}
	}
	if _, perr := s.modsSet(proto.ModsSetParams{Project: "nope", Mod: "pr-age", On: true}, phone); perrCode(perr) != proto.ErrBadParams {
		t.Errorf("another project: %v", perr)
	}
	agent := callCtx{who: func() caller.Caller { return caller.Caller{Kind: caller.Coordinator, Project: "demo"} }}
	if _, perr := s.modsSet(proto.ModsSetParams{Project: "demo", Mod: "pr-age", On: true, Key: m.Toggle.Key}, agent); perr == nil ||
		!strings.Contains(perr.Message, "human") {
		t.Fatalf("an agent's mods.set: %v", perr)
	}
	if prAge(t, s, "demo").Runs {
		t.Fatal("refused, but it runs")
	}

	got, perr := s.modsSet(proto.ModsSetParams{Project: "demo", Mod: "pr-age", On: true, Key: m.Toggle.Key}, phone)
	if perr != nil || got.(proto.ModsActResult).Words != "PR age on for demo" {
		t.Fatalf("mods.set on: %+v %v", got, perr)
	}
	m = prAge(t, s, "demo")
	if !m.Runs || m.Where != "on for this project" || m.Toggle.Do != "off" || m.Toggle.Key != "" {
		t.Fatalf("pr-age on: %+v", m)
	}
	journal, _ := os.ReadFile(demo.Path("JOURNAL.md"))
	if !strings.Contains(string(journal), " human (iphone: Sam's iPhone) mod.enable pr-age on for demo after its consent") {
		t.Errorf("journal:\n%s", journal)
	}
	if !strings.Contains(logs.String(), "device Sam's iPhone: mod.enable pr-age for project demo: on for demo") {
		t.Errorf("log:\n%s", logs)
	}

	// For web it runs for one more project: nothing new, nothing asked.
	web := prAge(t, s, "web")
	if web.Toggle.Do != "on" || web.Toggle.Key != "" || web.Where != "off for this project · runs for demo" {
		t.Fatalf("pr-age for web: %+v", web)
	}
	// The mod updates to ask for more before the phone's tap: what the
	// phone saw (nothing to ask) is stale.
	more := strings.Replace(prAgeManifest, "project_read = true", "project_read = true\nnotify = true", 1)
	if err := os.WriteFile(filepath.Join(dir, "mod.toml"), []byte(more), 0o600); err != nil {
		t.Fatal(err)
	}
	s.forgetMods()
	if _, perr := s.modsSet(proto.ModsSetParams{Project: "web", Mod: "pr-age", On: true}, phone); perrCode(perr) != proto.ErrModChanged {
		t.Fatalf("stale: %v", perr)
	}
	web = prAge(t, s, "web")
	if web.Toggle.Do != "consent" || web.Toggle.Question != "Grant PR age what it wants more?" || web.Toggle.Key == "" {
		t.Fatalf("pr-age wants more: %+v", web.Toggle)
	}
	if _, perr := s.modsSet(proto.ModsSetParams{Project: "web", Mod: "pr-age", On: true, Key: web.Toggle.Key}, phone); perr != nil {
		t.Fatal(perr)
	}
	if m := prAge(t, s, "web"); !m.Runs {
		t.Fatalf("web: %+v", m)
	}

	// Its settings for demo.
	if got, perr := s.modsSetting(proto.ModsSettingParams{Project: "demo", Mod: "pr-age", Key: "days", Value: &proto.ModValue{Type: "int", Int: 5}}, phone); perr != nil ||
		got.(proto.ModsActResult).Words != "days: 5" {
		t.Fatalf("days 5: %+v %v", got, perr)
	}
	m = prAge(t, s, "demo")
	if m.Own != "settings: 1 of its own" || m.Settings[0].Key != "days" || !m.Settings[0].Own || m.Settings[0].Value.Int != 5 ||
		m.Settings[0].Default.Int != 3 || m.Settings[1].Own {
		t.Fatalf("settings: %+v", m.Settings)
	}
	if web := prAge(t, s, "web"); web.Settings[0].Own || web.Settings[0].Value.Int != 3 {
		t.Fatalf("web follows all projects: %+v", web.Settings)
	}
	for _, bad := range []*proto.ModValue{{Type: "int", Int: 42}, {Type: "bool", Bool: true}, {}, {Type: "number", Int: 5}} {
		if _, perr := s.modsSetting(proto.ModsSettingParams{Project: "demo", Mod: "pr-age", Key: "days", Value: bad}, phone); perrCode(perr) != proto.ErrBadParams {
			t.Errorf("days %+v: %v", bad, perr)
		}
	}
	if _, perr := s.modsSetting(proto.ModsSettingParams{Project: "demo", Mod: "pr-age", Key: "days"}, phone); perr != nil {
		t.Fatal(perr)
	}
	if m := prAge(t, s, "demo"); m.Settings[0].Own || m.Own != "settings: all projects" {
		t.Fatalf("days follows all projects again: %+v", m.Settings[0])
	}
	if _, perr := s.modsSetting(proto.ModsSettingParams{Project: "demo", Mod: "pr-age", Key: "loud", Value: &proto.ModValue{Type: "bool", Bool: true}}, agent); perr == nil {
		t.Fatal("an agent set a setting")
	}
	journal, _ = os.ReadFile(demo.Path("JOURNAL.md"))
	if !strings.Contains(string(journal), " human (iphone: Sam's iPhone) mod.setting pr-age days: 5") {
		t.Errorf("journal:\n%s", journal)
	}

	// Off for demo: it runs only for web then.
	if got, perr := s.modsSet(proto.ModsSetParams{Project: "demo", Mod: "pr-age", On: false}, phone); perr != nil ||
		got.(proto.ModsActResult).Words != "PR age off for demo" {
		t.Fatalf("off: %+v %v", got, perr)
	}
	if m := prAge(t, s, "demo"); m.Runs || m.Where != "off for this project · runs for web" {
		t.Fatalf("off for demo: %+v", m)
	}
}

// TestProjectModsEveryProject: a mod that runs for every project is
// limited to the others, after the question whose key mods.set takes.
func TestProjectModsEveryProject(t *testing.T) {
	s, _, _, _ := projectModsServer(t)
	cfg := "[mods.pr-age]\nenabled = true\ngrants = [\"project_read\"]\n[mods.memory-tab]\nenabled = false\n[mods.tasks-tab]\nenabled = false\n[mods.ticker]\nenabled = false\n"
	if err := os.WriteFile(filepath.Join(os.Getenv("TERMINATR_HOME"), "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	mac := callCtx{app: true, who: func() caller.Caller { return caller.Caller{Kind: caller.Human, Via: caller.ViaApp} }}
	m := prAge(t, s, "demo")
	if m.Toggle.Do != "limit" || len(m.Toggle.Others) != 1 || m.Toggle.Others[0] != "web" || m.Toggle.Key == "" ||
		m.Toggle.Question != "PR age runs for every project. Run it only for web instead? A project made later won't run it until you turn it on there." {
		t.Fatalf("every project: %+v", m.Toggle)
	}
	if _, perr := s.modsSet(proto.ModsSetParams{Project: "demo", Mod: "pr-age"}, mac); perrCode(perr) != proto.ErrModChanged {
		t.Fatalf("unasked: %v", perr)
	}
	if _, perr := s.modsSet(proto.ModsSetParams{Project: "demo", Mod: "pr-age", Key: m.Toggle.Key}, mac); perr != nil {
		t.Fatal(perr)
	}
	if m := prAge(t, s, "demo"); m.Runs || m.Where != "off for this project · runs for web" {
		t.Fatalf("limited: %+v", m)
	}
}
