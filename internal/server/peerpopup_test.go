package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// fillAll sets every field under v to a value that isn't zero, strings
// to "SECRET".
func fillAll(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString("SECRET")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int64:
		v.SetInt(7)
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fillAll(v.Elem())
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		fillAll(v.Index(0))
	case reflect.Struct:
		if v.Type() == reflect.TypeFor[time.Time]() {
			v.Set(reflect.ValueOf(time.Unix(7, 0)))
			return
		}
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				fillAll(v.Field(i))
			}
		}
	default:
		panic("fillAll: " + v.Type().String())
	}
}

// fieldNames are t's exported fields' names.
func fieldNames(t reflect.Type) []string {
	var out []string
	for i := range t.NumField() {
		if t.Field(i).IsExported() {
			out = append(out, t.Field(i).Name)
		}
	}
	slices.Sort(out)
	return out
}

// zeroFields are the names of v's fields that are zero.
func zeroFields(v reflect.Value) []string {
	var out []string
	for i := range v.NumField() {
		if v.Field(i).IsZero() {
			out = append(out, v.Type().Field(i).Name)
		}
	}
	slices.Sort(out)
	return out
}

// TestPeerPopupAllowlist: of a task, a message and the settings with
// every field set, a paired server gets exactly the fields decided on.
// A field added to tasks.Task, link.Message or config.Safety fails here
// until it is decided: copied in peerpopup.go (and to the proto type),
// or listed below as staying home.
func TestPeerPopupAllowlist(t *testing.T) {
	var s config.Safety
	fillAll(reflect.ValueOf(&s).Elem())
	homeSettings := []string{"Archived", "BranchName", "GlobalParallelThreads", "GuardAllow", "GuardOff", "Pinned"}
	// Every field of the settings is either copied, by its name, or home.
	copied := fieldNames(reflect.TypeFor[proto.PeerSettings]())
	all := slices.Sorted(slices.Values(append(slices.Clone(copied), homeSettings...)))
	if want := fieldNames(reflect.TypeFor[config.Safety]()); !slices.Equal(all, want) {
		t.Errorf("settings: copied %v and home %v, but config.Safety has %v", copied, homeSettings, want)
	}
	ps := peerSettingsOf(s)
	if z := zeroFields(reflect.ValueOf(ps)); len(z) > 0 {
		t.Errorf("settings not copied: %v", z)
	}
	// Each copied field is the setting of its key (its JSON name).
	pt := reflect.TypeFor[proto.PeerSettings]()
	for i := range pt.NumField() {
		key, _, _ := strings.Cut(pt.Field(i).Tag.Get("json"), ",")
		if got, want := reflect.ValueOf(ps).Field(i).Interface(), s.Value(key); !reflect.DeepEqual(got, want) {
			t.Errorf("%s (%s): %v, the setting is %v", pt.Field(i).Name, key, got, want)
		}
	}

	var task tasks.Task
	fillAll(reflect.ValueOf(&task).Elem())
	task.Status = tasks.Ready
	pt2 := peerTaskOf(&task)
	if z := zeroFields(reflect.ValueOf(pt2)); len(z) > 0 {
		t.Errorf("task fields not copied: %v", z)
	}
	if b, _ := json.Marshal(pt2); strings.Count(string(b), "SECRET") != 4 { // title, thread, created, updated
		t.Errorf("a task's other words crossed: %s", b)
	}
	if want := []string{"After", "Archived", "Created", "Design", "ID", "Notes", "Owner", "Repo", "Status", "Steps", "Thread", "Title", "Updated"}; !slices.Equal(fieldNames(reflect.TypeFor[tasks.Task]()), want) {
		t.Errorf("tasks.Task has %v: decide what of a new field a paired server sees (peerTaskOf)", fieldNames(reflect.TypeFor[tasks.Task]()))
	}

	var m link.Message
	fillAll(reflect.ValueOf(&m).Elem())
	pm := peerMessageOf(&m)
	if z := zeroFields(reflect.ValueOf(pm)); len(z) > 0 {
		t.Errorf("message fields not copied: %v", z)
	}
	if want := []string{"Attachments", "By", "Bytes", "Created", "Due", "File", "FileSize", "ID", "Out", "Peer", "Re", "Refused", "Sender", "State", "Text", "Transfer"}; !slices.Equal(fieldNames(reflect.TypeFor[link.Message]()), want) {
		t.Errorf("link.Message has %v: decide what of a new field a paired server sees (peerMessageOf)", fieldNames(reflect.TypeFor[link.Message]()))
	}
}

// TestPeerPopupNeverLeaks: through a real link, a paired server whose
// pane switch here is view reads a shared project's popup: its board
// without notes, owners, repos or steps' words, its settings without
// the branch template or the guard's allowed calls, how many
// repositories but not where, its links with that server's projects
// only and the headers of the messages that crossed (sent, read), without
// text: never one held for the user's OK, queued over the rate, dropped
// or undelivered, nor their counts. Nothing of the memory crosses, not
// even a note's title. Off refuses it, as an unshared project is.
func TestPeerPopupNeverLeaks(t *testing.T) {
	testPaths(t)
	root, _ := home.Dir()
	for _, o := range []project.Options{{Slug: "web", Goal: "the web shop"}, {Slug: "secret-proj"}} {
		if _, err := project.New(o); err != nil {
			t.Fatal(err)
		}
	}
	if err := config.SetProject("web", "share", true); err != nil {
		t.Fatal(err)
	}
	if err := config.SetProject("web", "branch_name", "users/SECRET-USER/{id}"); err != nil {
		t.Fatal(err)
	}
	web, _ := project.Open("web")
	if _, err := web.Tasks().Add(human, []tasks.NewTask{
		{Title: "Ship it", Status: "ready", Notes: "SECRET-NOTES", Steps: []string{"SECRET-STEP", "two"}, Owner: "SECRET-OWNER"},
	}); err != nil {
		t.Fatal(err)
	}
	// Links: one with studio's project notes (this server calls the
	// reader studio), one with another server's, one with a project here.
	for other, allow := range map[string]string{"studio/notes": config.LinkBoth, "other/x": config.LinkBoth, "secret-proj": config.LinkSend} {
		if err := config.SetLink("web", other, config.Link{Allow: allow}); err != nil {
			t.Fatal(err)
		}
	}
	msg := func(other, id string, out bool, state string) {
		alias, slug, _ := config.Remote(other)
		dir := web.Path("messages", "@"+alias, slug)
		if alias == "" {
			dir = web.Path("messages", other)
		}
		os.MkdirAll(dir, 0o755)
		m := &link.Message{ID: id, Out: out, Created: time.Now(), State: state, By: "coordinator", Bytes: 20, Attachments: []string{"SECRET-FILE"},
			File: "SECRET-OFFERED.pdf", FileSize: 4 << 20, Transfer: "X-AB12-99"}
		if err := mdfile.Write(filepath.Join(dir, id+".md"), m, []byte("SECRET-TEXT\n")); err != nil {
			t.Fatal(err)
		}
	}
	// Two that crossed, then the newest four that didn't.
	msg("studio/notes", "M-AB12-1", true, link.StateSent)
	msg("studio/notes", "M-9C1B-8", false, link.StateRead)
	msg("studio/notes", "M-AB12-2", true, link.StateHeld)
	msg("studio/notes", "M-AB12-5", true, link.StateQueued)
	msg("studio/notes", "M-AB12-6", true, link.StateDropped)
	msg("studio/notes", "M-AB12-7", true, link.StateUndelivered)
	msg("other/x", "M-AB12-3", true, link.StateSent)
	msg("secret-proj", "M-4", true, link.StateSent)
	for name, text := range map[string]string{
		"build.md": "+++\ntitle = \"SECRET-NOTE-TITLE\"\n+++\nSECRET-BODY\n",
		"coord.md": "+++\ntitle = \"SECRET-COORD-NOTE\"\naudience = \"coordinator\"\n+++\nx\n",
	} {
		os.MkdirAll(web.Path("memory"), 0o755)
		if err := os.WriteFile(web.Path("memory", name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ticker := filepath.Join(t.TempDir(), "ticker.json")
	ex := peerExport{
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
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	off := linkedServers(t, ex)
	if !slices.Contains(off.Status().Peers[0].Caps, peer.CapPopup) {
		t.Fatalf("mini serves popups, but its hello says %v", off.Status().Peers[0].Caps)
	}
	if _, err := off.Popup(ctx, "mini", "web"); code(err) != proto.ErrPanesOff {
		t.Errorf("panes off: %v, want panes-off", err)
	}

	studio := linkedServersPanes(t, ex, proto.PanesView)
	var raw json.RawMessage
	if err := studio.Call(ctx, "mini", proto.MethodPeerPopup, proto.PeerPopupParams{Project: "web"}, &raw); err != nil {
		t.Fatal(err)
	}
	var pp proto.PeerPopup
	json.Unmarshal(raw, &pp)
	if pp.Project != "web" || pp.Goal != "the web shop" || len(pp.Tasks) != 1 || pp.Tasks[0].StepsTotal != 2 || pp.Tasks[0].Title != "Ship it" {
		t.Errorf("popup: %+v", pp)
	}
	if !pp.Settings.Share || pp.Settings.ParallelThreads == 0 {
		t.Errorf("settings: %+v", pp.Settings)
	}
	if len(pp.Links) != 1 || pp.Links[0].Project != "notes" || pp.Links[0].Last != "M-9C1B-8" {
		t.Errorf("links: %+v", pp.Links)
	}
	var ids []string
	for _, l := range pp.Links {
		for _, m := range l.Messages {
			ids = append(ids, m.ID+" "+m.State)
		}
	}
	if !slices.Equal(ids, []string{"M-AB12-1 sent", "M-9C1B-8 read"}) {
		t.Errorf("message headers: %q, want only the sent and the read one", ids)
	}
	for _, slug := range []string{"secret-proj", "nope"} {
		if _, err := studio.Popup(ctx, "mini", slug); code(err) != proto.ErrNotShared {
			t.Errorf("%s: %v, want not-shared", slug, err)
		}
	}
	cfg, _ := os.ReadFile(filepath.Join(root, "config.toml"))
	for _, never := range []string{"SECRET", "secret-proj", "other/x", "M-4\"", root, "/Users/", "config.toml", "users/", `"notes":`, `"text"`, `"attachments"`, `"owner"`, `"repo"`,
		"M-AB12-2", "M-AB12-5", "M-AB12-6", "M-AB12-7", "X-AB12-99", `"file`, "4194304", `"held"`, `"queued"`, "dropped", "undelivered", `"memory"`} {
		if strings.Contains(string(raw), never) {
			t.Errorf("%q crossed:\n%s", never, raw)
		}
	}
	if len(cfg) == 0 || strings.Contains(string(raw), string(cfg)) {
		t.Error("the settings file crossed, or the test wrote none")
	}
}

// code is err's protocol code, "" for another error.
func code(err error) string {
	if pe, ok := err.(*proto.Error); ok {
		return pe.Code
	}
	return ""
}
