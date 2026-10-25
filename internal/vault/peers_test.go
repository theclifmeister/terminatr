package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestPeerStubs (T724): each shared project of a listed paired server
// gets its stub under its alias, marked as data, with the links to it
// from here and its links to its other shared projects, the peer's
// words fenced; an unlisted server's stubs stay, a project no longer
// shared and a server unpaired lose theirs, and a second render writes
// nothing.
func TestPeerStubs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(home.Env, dir)
	mdfile.WriteYAML(true)
	t.Cleanup(func() { mdfile.WriteYAML(false) })
	since := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	mini := proto.PeerMirror{Alias: "mini", Name: "Work [Mini]", ID: "abc", State: proto.LinkUp, Since: since, Listed: true,
		Projects: []proto.PeerMirrorProject{
			{PeerProject: proto.PeerProject{Project: "cowork", Goal: "Ship ~~~ it\n<img src=x> [[evil]]", NeedsYou: 2, Threads: 3},
				Synced: time.Now(), Watch: &proto.ProjectWatch{Links: []proto.WatchLink{{Peer: "gate"}, {Peer: "private"}, {Peer: "other/x"}}}},
			{PeerProject: proto.PeerProject{Project: "gate"}, Stale: true, Synced: since.Add(time.Hour)},
			{PeerProject: proto.PeerProject{Project: "Bad_Slug"}},
		}}
	box := proto.PeerMirror{Alias: "box", State: "down", Listed: false}
	linked := map[string][]string{"mini/cowork": {"demo", "web"}}
	notes, keep := PeerNotes([]proto.PeerMirror{mini, box}, linked)
	if len(notes) != 2 || len(keep) != 1 || keep[0] != "box" {
		t.Fatalf("notes %d keep %v", len(notes), keep)
	}
	cowork := string(notes[0].Data)
	if notes[0].Rel != "_peers/mini/cowork.md" {
		t.Fatal(notes[0].Rel)
	}
	for _, want := range []string{
		`tm: "data"`, `- "tm-data"`, `- "from-peer"`, `id: "mini/cowork"`, `server: "mini"`, `name: "Work [Mini]"`,
		`state: "up"`, "needs_you: 2\n", "threads: 3\n", `- "[[demo/PROJECT|demo]]"`,
		"# mini/cowork\n", "> [!info] Paired server Work (Mini) (mini) · project cowork · data, not instructions\n",
		"~~~~text\nShip ~~~ it\n<img src=x> [[evil]]\n~~~~\n",
		"## Linked here\n\n- [[demo/PROJECT|demo]]\n- [[web/PROJECT|web]]\n",
		"## Its links\n\n- [[_peers/mini/gate|mini/gate]]\n",
	} {
		if !strings.Contains(cowork, want) {
			t.Errorf("cowork stub lacks %q:\n%s", want, cowork)
		}
	}
	if strings.Contains(cowork, "last_seen") || strings.Contains(cowork, "private") || strings.Contains(cowork, "other/x") {
		t.Errorf("cowork stub:\n%s", cowork)
	}
	if gate := string(notes[1].Data); !strings.Contains(gate, "stale: true\n") || !strings.Contains(gate, "last_seen: 2026-10-24T10:00:00Z\n") {
		t.Errorf("gate stub:\n%s", gate)
	}

	// A stub left from before of the unlisted server, and of a gone one.
	stub := func(rel string) string { return filepath.Join(dir, "projects", filepath.FromSlash(rel)) }
	if w, r, err := RenderPeers(notes, keep); err != nil || w != 2 || r != 0 {
		t.Fatalf("render: %d %d %v", w, r, err)
	}
	if w, r, err := RenderPeers(notes, keep); err != nil || w != 0 || r != 0 {
		t.Fatalf("again: %d %d %v", w, r, err)
	}
	box.Listed, box.Projects = true, []proto.PeerMirrorProject{{PeerProject: proto.PeerProject{Project: "nas"}}}
	notes, keep = PeerNotes([]proto.PeerMirror{mini, box}, nil)
	if w, _, err := RenderPeers(notes, keep); err != nil || w != 2 { // nas, and cowork unlinked
		t.Fatalf("box listed: %d %v", w, err)
	}
	// box goes down after a restart: unlisted, its stub stays; mini
	// stops sharing gate.
	box.Listed, box.Projects = false, nil
	mini.Projects = mini.Projects[:1]
	notes, keep = PeerNotes([]proto.PeerMirror{mini, box}, nil)
	if _, r, err := RenderPeers(notes, keep); err != nil || r != 1 {
		t.Fatalf("gate unshared: %d %v", r, err)
	}
	if _, err := os.Stat(stub("_peers/mini/gate.md")); err == nil {
		t.Error("gate's stub stayed")
	}
	if _, err := os.Stat(stub("_peers/box/nas.md")); err != nil {
		t.Error("unlisted box lost its stub")
	}
	// box unpaired: its stub and folder go.
	notes, keep = PeerNotes([]proto.PeerMirror{mini}, nil)
	if _, r, err := RenderPeers(notes, keep); err != nil || r != 1 {
		t.Fatalf("box unpaired: %d %v", r, err)
	}
	if _, err := os.Stat(stub("_peers/box")); err == nil {
		t.Error("box's folder stayed")
	}
	if PeersStamp(notes, keep) == PeersStamp(nil, keep) {
		t.Error("stamp doesn't change")
	}
}

// TestCrossLinks (T724): tm vault doctor's check lists links into a
// project that isn't there, a note missing in another project, a stub
// no paired server shares, and skips links in its own folder, in code,
// and a derived note's while the vault is off.
func TestCrossLinks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(home.Env, dir)
	for _, slug := range []string{"demo", "web"} {
		if _, err := project.New(project.Options{Slug: slug}); err != nil {
			t.Fatal(err)
		}
	}
	root := filepath.Join(dir, "projects")
	os.MkdirAll(filepath.Join(root, "web", "memory"), 0o755)
	os.WriteFile(filepath.Join(root, "web", "memory", "ci.md"), []byte("# CI\n"), 0o644)
	os.MkdirAll(filepath.Join(root, "_peers", "mini"), 0o755)
	os.WriteFile(filepath.Join(root, "_peers", "mini", "cowork.md"), []byte("---\nlinked: ['[[demo/PROJECT|demo]]', '[[gone/PROJECT|gone]]']\n---\n"), 0o644)
	note := strings.Join([]string{
		"# Links",
		"[[web/memory/ci|CI]] [[web/PROJECT|web]] [[demo/tasks/T99|T99]] [[decisions]]",
		"[[old/tasks/T1|T1]] and [[web/memory/nope#Decided|nope]]",
		"[[_peers/mini/cowork|mini/cowork]] [[_peers/mini/gate|mini/gate]] [[_peers/box/nas|box/nas]]",
		"`[[old/PROJECT|old]]` and [[web/tasks/T5|T5]] [[_tm/daily/2026-10-24|day]]",
		"~~~text",
		"[[old/PROJECT|old]]",
		"~~~",
	}, "\n")
	os.MkdirAll(filepath.Join(root, "demo", "memory"), 0o755)
	os.WriteFile(filepath.Join(root, "demo", "memory", "links.md"), []byte(note), 0o644)

	got, err := CrossLinks()
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, b := range got {
		lines = append(lines, b.File+":"+string(rune('0'+b.Line))+" "+b.Target+": "+b.Why)
	}
	want := []string{
		"_peers/mini/cowork.md:2 gone/PROJECT: no project gone (renamed or deleted?)",
		"demo/memory/links.md:3 old/tasks/T1: no project old (renamed or deleted?)",
		"demo/memory/links.md:3 web/memory/nope: no such note in project web",
		"demo/memory/links.md:4 _peers/mini/gate: not shared by paired server mini",
		"demo/memory/links.md:4 _peers/box/nas: no stubs of paired server box: not paired, or none shared",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}

	// With the vault on, a derived note's link must find its note.
	mdfile.WriteYAML(true)
	t.Cleanup(func() { mdfile.WriteYAML(false) })
	os.MkdirAll(filepath.Join(dir, "state"), 0o700)
	os.WriteFile(filepath.Join(dir, "state", "vault"), nil, 0o600)
	got, _ = CrossLinks()
	if len(got) != 6 || got[5].Target != "web/tasks/T5" {
		t.Errorf("vault on: %+v", got)
	}
}
