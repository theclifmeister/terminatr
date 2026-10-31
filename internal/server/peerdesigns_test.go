package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// TestPeerDesignsAllowlist: of a design with every field set, a paired
// server gets exactly the fields decided on: never its page's URL or
// its design task's open questions. A field added to project.Design or
// project.DesignBuild fails here until it is decided in
// peerDesignOf, and the proto types are pinned too.
func TestPeerDesignsAllowlist(t *testing.T) {
	var b project.DesignBuild
	fillAll(reflect.ValueOf(&b).Elem())
	b.Artifact = "https://claude.ai/artifact/SECRET-PAGE"
	b.Questions[0].Question, b.Questions[0].Header = "SECRET-QUESTION", "SECRET-HEADER"
	b.Build[0].Archived, b.Build[0].Status, b.Build[0].After = false, tasks.Done, []int{3}
	d := peerDesignOf(b, map[string]bool{b.Build[0].Ref(): true})
	if z := zeroFields(reflect.ValueOf(d)); len(z) > 0 {
		t.Errorf("design fields not copied: %v", z)
	}
	// Archived is off so that Working shows: an archived task never runs.
	if z := zeroFields(reflect.ValueOf(d.Build[0])); !slices.Equal(z, []string{"Archived"}) {
		t.Errorf("build task fields not copied: %v", z)
	}
	js, _ := json.Marshal(d)
	for _, never := range []string{"SECRET-PAGE", "claude.ai", "https", "SECRET-QUESTION", "SECRET-HEADER", `"questions"`, `"artifact"`} {
		if strings.Contains(string(js), never) {
			t.Errorf("%q crossed: %s", never, js)
		}
	}
	if !d.Page {
		t.Errorf("a design with a page doesn't say it has one: %s", js)
	}
	if want := []string{"Answers", "Archived", "Artifact", "Name", "Task", "Title"}; !slices.Equal(fieldNames(reflect.TypeFor[project.Design]()), want) {
		t.Errorf("project.Design has %v: decide what of a new field a paired server's designs see (peerDesignOf)", fieldNames(reflect.TypeFor[project.Design]()))
	}
	if want := []string{"Build", "Design", "DesignTask", "Questions"}; !slices.Equal(fieldNames(reflect.TypeFor[project.DesignBuild]()), want) {
		t.Errorf("project.DesignBuild has %v: decide what a paired server sees (peerDesignOf)", fieldNames(reflect.TypeFor[project.DesignBuild]()))
	}
	for typ, want := range map[reflect.Type][]string{
		reflect.TypeFor[proto.PeerDesigns]():    {"Designs", "Truncated"},
		reflect.TypeFor[proto.PeerDesign]():     {"Answers", "Archived", "Build", "Name", "Page", "Status", "Task", "TaskStatus", "TasksDone", "Title", "Working"},
		reflect.TypeFor[proto.PeerDesignTask](): {"After", "Archived", "ID", "Status", "Title", "Working"},
	} {
		if got := fieldNames(typ); !slices.Equal(got, want) {
			t.Errorf("%s crosses %v, decided %v: a field added to the designs is a decision (§16.17)", typ, got, want)
		}
	}
}

// designsExport is the test's peerExport of the projects on disk.
func designsExport(t *testing.T) peerExport {
	ticker := filepath.Join(t.TempDir(), "ticker.json")
	return peerExport{
		sessions: func() []proto.SessionInfo { return nil },
		state: func(slug string, ss []proto.SessionInfo) (proto.ProjectWatch, error) {
			p, err := project.Open(slug)
			if err != nil {
				return proto.ProjectWatch{}, err
			}
			return ProjectWatchOf(p, ss, ticker), nil
		},
		changes: func() (<-chan struct{}, func()) { return make(chan struct{}), func() {} },
	}
}

// TestPeerDesignsNeverLeaks: through a real link, a paired server whose
// project pages switch here is on for web lists its designs (title,
// status, answers, build); never a design's page URL nor its design
// task's open questions. Off refuses with pages-off whatever the
// project; a project not shared answers not-shared with the switch on.
func TestPeerDesignsNeverLeaks(t *testing.T) {
	testPaths(t)
	for _, o := range []project.Options{{Slug: "web"}, {Slug: "secret-proj"}} {
		if _, err := project.New(o); err != nil {
			t.Fatal(err)
		}
	}
	if err := config.SetProject("web", "share", true); err != nil {
		t.Fatal(err)
	}
	web, _ := project.Open("web")
	if _, err := web.Tasks().Add(human, []tasks.NewTask{{Title: "Design the vault", Status: "review", Notes: "SECRET-NOTES"},
		{Title: "Build the vault", Status: "ready"}}); err != nil {
		t.Fatal(err)
	}
	design := "vault"
	if _, err := web.Tasks().SetPlan(human, 2, &design, nil); err != nil {
		t.Fatal(err)
	}
	title, task, page, answers := "Obsidian vault", "T1", "https://claude.ai/artifact/SECRETPAGE", []string{"YAML front matter"}
	if _, _, err := web.SetDesign("vault", project.DesignEdit{Title: &title, Task: &task, Artifact: &page, Answers: &answers}); err != nil {
		t.Fatal(err)
	}
	if _, err := web.AddQuestion(project.Question{Task: "T1", Header: "SECRET-HDR", Question: "SECRET-QUESTION?",
		Options: []project.QuestionOption{{Label: "a"}, {Label: "b"}}}); err != nil {
		t.Fatal(err)
	}
	ex := designsExport(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	off := linkedServers(t, ex)
	if !slices.Contains(off.Status().Peers[0].Caps, peer.CapPageDesigns) {
		t.Fatalf("mini serves designs, but its hello says %v", off.Status().Peers[0].Caps)
	}
	for _, slug := range []string{"web", "secret-proj", "nope"} {
		if _, err := off.Designs(ctx, "mini", slug); code(err) != proto.ErrPagesOff {
			t.Errorf("designs off, %s: %v, want pages-off", slug, err)
		}
	}

	on := linkedServersWith(t, ex, func(p *peer.Peer) { p.Pages = []string{"web", "secret-proj"} })
	var raw json.RawMessage
	if err := on.Call(ctx, "mini", proto.MethodPeerPagesDesigns, proto.PeerPagesParams{Project: "web"}, &raw); err != nil {
		t.Fatal(err)
	}
	ds, err := on.Designs(ctx, "mini", "web")
	if err != nil || len(ds.Designs) != 1 {
		t.Fatalf("designs: %+v %v", ds, err)
	}
	d := ds.Designs[0]
	if d.Name != "vault" || d.Title != title || d.Status != project.DesignPlanned || d.Task != "T1" || d.TaskStatus != "review" ||
		!d.Page || !slices.Equal(d.Answers, answers) || len(d.Build) != 1 || d.Build[0].ID != "T2" || d.Build[0].Title != "Build the vault" {
		t.Errorf("design: %+v", d)
	}
	if _, err := on.Designs(ctx, "mini", "secret-proj"); code(err) != proto.ErrNotShared {
		t.Errorf("secret-proj designs, pages on but not shared: %v, want not-shared", err)
	}
	for _, never := range []string{"SECRET", "claude.ai", "https", `"artifact"`, `"questions"`, "secret-proj"} {
		if strings.Contains(string(raw), never) {
			t.Errorf("%q crossed:\n%s", never, raw)
		}
	}
}

// TestPeerDesignsBudget: designs too many to cross whole drop their
// done and archived ones first, keep their order, and say so.
func TestPeerDesignsBudget(t *testing.T) {
	var bs []project.DesignBuild
	for i, name := range []string{"a", "b", "c", "d"} {
		b := project.DesignBuild{Design: project.Design{Name: name, Answers: []string{strings.Repeat("x", 200)}}}
		if i%2 == 0 {
			b.Build = []*tasks.Task{{ID: i + 1, Status: tasks.Done}}
		}
		if name == "c" {
			b.Archived, b.Build = true, nil
		}
		bs = append(bs, b)
	}
	all, cut := peerDesigns(bs, nil, 1<<20)
	if len(all) != 4 || cut {
		t.Fatalf("everything fits: %d designs, truncated %v", len(all), cut)
	}
	one := designBytes(all[1])
	got, cut := peerDesigns(bs, nil, 2*one+one/2)
	var names []string
	for _, d := range got {
		names = append(names, d.Name)
	}
	if !cut || !slices.Equal(names, []string{"b", "d"}) {
		t.Errorf("budget for two: %v (truncated %v), want the two not done nor archived", names, cut)
	}
}

// TestPeerDesignsOlderPeer: a paired server whose tm shares pages but
// no designs (a tm of T886: its hello lists pages, not page-designs) is
// never asked: no-pages, which the console words as "not shared".
func TestPeerDesignsOlderPeer(t *testing.T) {
	testPaths(t)
	if _, err := project.New(project.Options{Slug: "web"}); err != nil {
		t.Fatal(err)
	}
	config.SetProject("web", "share", true)
	// The Exporter's and the pages' methods only.
	ex := designsExport(t)
	old := struct {
		peer.Exporter
		peer.PagesExporter
	}{ex, ex}
	studio := linkedServersWith(t, old, func(p *peer.Peer) { p.Pages = []string{"web"} })
	caps := studio.Status().Peers[0].Caps
	if !slices.Contains(caps, peer.CapPages) || slices.Contains(caps, peer.CapPageDesigns) {
		t.Fatalf("a T886 exporter's hello says %v", caps)
	}
	ctx := context.Background()
	if _, err := studio.Designs(ctx, "mini", "web"); code(err) != proto.ErrNoPages {
		t.Errorf("designs of a T886 peer: %v, want no-pages", err)
	}
	if _, err := studio.Pages(ctx, "mini", "web"); err != nil {
		t.Errorf("its pages: %v", err)
	}
	if pe := pagesError("mini", peer.ErrNoDesigns); pe.Code != proto.ErrNoPages {
		t.Errorf("words: %+v", pe)
	}
}
