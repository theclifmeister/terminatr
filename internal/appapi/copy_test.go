package appapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// fill sets every field under v to a value that isn't zero: strings
// "SECRET", numbers 7, one element in each list and map.
func fill(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString("SECRET")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(7)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(7)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(7)
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fill(v.Elem())
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		fill(v.Index(0))
	case reflect.Array:
		for i := range v.Len() {
			fill(v.Index(i))
		}
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		k, e := reflect.New(v.Type().Key()).Elem(), reflect.New(v.Type().Elem()).Elem()
		fill(k)
		fill(e)
		m.SetMapIndex(k, e)
		v.Set(m)
	case reflect.Struct:
		if v.Type() == reflect.TypeFor[time.Time]() {
			v.Set(reflect.ValueOf(time.Unix(7, 0)))
			return
		}
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				fill(v.Field(i))
			}
		}
	case reflect.Interface:
		// Left nil: no record of the app API's sources holds one.
	default:
		panic("fill: " + v.Type().String())
	}
}

// node is a field under the source of a copy: a leaf (leaf >= 0, its
// index in walk order) or a record, list or map with children.
type node struct {
	path     string
	leaf     int
	children []*node
}

// walk visits the leaves under v in a fixed order, building the tree of
// fields once (tree non-nil). A map's one value is visited on a copy
// that is put back.
func walk(v reflect.Value, path string, tree *node, visit func(path string, v reflect.Value)) {
	child := func(p string) *node {
		if tree == nil {
			return nil
		}
		n := &node{path: p, leaf: -1}
		tree.children = append(tree.children, n)
		return n
	}
	leaf := func() {
		if tree != nil {
			tree.leaf = 0 // numbered by the caller
		}
		visit(path, v)
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			leaf()
			return
		}
		walk(v.Elem(), path, tree, visit)
	case reflect.Slice:
		if v.Len() == 0 {
			leaf()
			return
		}
		walk(v.Index(0), path+"[]", child(path+"[]"), visit)
	case reflect.Map:
		it := v.MapRange()
		for it.Next() {
			e := reflect.New(v.Type().Elem()).Elem()
			e.Set(it.Value())
			walk(e, path+"{}", child(path+"{}"), visit)
			v.SetMapIndex(it.Key(), e)
		}
	case reflect.Struct:
		if v.Type() == reflect.TypeFor[time.Time]() {
			leaf()
			return
		}
		for i := range v.NumField() {
			f := v.Type().Field(i)
			if f.IsExported() {
				walk(v.Field(i), path+"."+f.Name, child(path+"."+f.Name), visit)
			}
		}
	default:
		leaf()
	}
}

// homeOf is what of leaves home of an S with every field set: each leaf
// field is zeroed in turn, and one whose zero doesn't change the copy's
// JSON isn't copied. A record, list or map all of whose fields stay
// home is named alone.
func homeOf[S, A any](of func(S) A) []string {
	full := func() S {
		var s S
		fill(reflect.ValueOf(&s).Elem())
		return s
	}
	enc := func(s S) string {
		b, err := json.Marshal(of(s))
		if err != nil {
			panic(err)
		}
		return string(b)
	}
	base := enc(full())
	root := &node{leaf: -1}
	s := full()
	var leaves []*node
	// The tree: walk with a node per field, then number the leaves.
	walk(reflect.ValueOf(&s).Elem(), "", root, func(string, reflect.Value) {})
	var number func(n *node)
	number = func(n *node) {
		if n.leaf >= 0 {
			n.leaf = len(leaves)
			leaves = append(leaves, n)
		}
		for _, c := range n.children {
			number(c)
		}
	}
	number(root)
	home := make([]bool, len(leaves))
	for k := range leaves {
		s := full()
		i := 0
		walk(reflect.ValueOf(&s).Elem(), "", nil, func(_ string, v reflect.Value) {
			if i == k {
				v.Set(reflect.Zero(v.Type()))
			}
			i++
		})
		home[k] = enc(s) == base
	}
	var all func(n *node) bool
	all = func(n *node) bool {
		if n.leaf >= 0 {
			return home[n.leaf]
		}
		if len(n.children) == 0 {
			return false
		}
		for _, c := range n.children {
			if !all(c) {
				return false
			}
		}
		return true
	}
	var out []string
	var report func(n *node)
	report = func(n *node) {
		if n != root && all(n) {
			out = append(out, n.path)
			return
		}
		for _, c := range n.children {
			report(c)
		}
	}
	report(root)
	slices.Sort(out)
	return out
}

// TestCopies: of each source the app API copies, every field is copied
// but those listed here as staying home. A field the server's records
// gain fails here until it is decided: copied in types.go, or listed.
func TestCopies(t *testing.T) {
	check := func(name string, got, want []string) {
		t.Helper()
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s: fields left home\n%s\nwant\n%s", name, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
	check("session.list", homeOf(sessionListOf), []string{
		// The server's platform, whether the list is a session's, the
		// mods' columns and cells: the TUI's.
		".OS", ".Scoped", ".Mods", ".Sessions[].Cells",
		// How tm runs a session, for tm session and tm doctor.
		".Sessions[].Argv", ".Sessions[].Cwd", ".Sessions[].PID", ".Sessions[].StateSources",
		".Sessions[].AgentSID", ".Sessions[].Identified",
		// The settings' side of remote control and the model.
		".Sessions[].RemoteHeld", ".Sessions[].ModelNext", ".Sessions[].ModelPending",
	})
	check("session.watch", homeOf(watchOf), nil)
	check("projects.watch", homeOf(projectsOf), append(settingsHome(".Projects[].Safety"),
		// Settings and mods are never in the app API, but for the
		// sidebar's three (settingsHome).
		".Projects[].Own", ".Projects[].Badges",
		// A task's archive flag: archived tasks aren't sent.
		".Projects[].NeedsYou[].Archived", ".Projects[].Threads[].Task.Archived",
		".Projects[].Board[].Archived",
		// For resume, and resolved threads aren't sent.
		".Projects[].Threads[].Record.AgentSID", ".Projects[].Threads[].Record.Prompted",
		".Projects[].Threads[].Record.ResolvedAt",
		// The status's own copies of its thread and task, and the
		// vault's properties (the file's only).
		".Projects[].Threads[].Status.Thread", ".Projects[].Threads[].Status.Task",
		".Projects[].Threads[].Status.Vault", ".Projects[].Threads[].Status.Tags",
		".Projects[].Threads[].Status.CSSClasses",
		// Where a vault item's text is, a link item's message.
		".Projects[].Items[].Message", ".Projects[].Items[].Path", ".Projects[].Items[].Hash",
		".Projects[].Items[].Part", ".Projects[].Items[].Source", ".Projects[].Items[].Vault",
		".Projects[].Items[].Tags", ".Projects[].Items[].CSSClasses",
		// Each side's link entry (settings), the newest message's id,
		// the link requests waiting (answered in the TUI): of the one
		// the paired server asks, State's words name only its id.
		".Projects[].Links[].Mine", ".Projects[].Links[].Theirs", ".Projects[].Links[].Last",
		".Projects[].Links[].Asked", ".Projects[].Links[].Asks.Allow", ".Projects[].Links[].Asks.Answer",
		".Projects[].Links[].Asks.Answered", ".Projects[].Links[].Asks.Ask", ".Projects[].Links[].Asks.Code",
		".Projects[].Links[].Asks.Created", ".Projects[].Links[].Asks.Out", ".Projects[].Links[].Asks.Peer",
		".Projects[].Links[].Asks.Project", ".Projects[].Links[].Asks.State",
	))
	check("peer.list", homeOf(peerListOf), []string{
		// The link's id, versions and ping: the Network popup's. Share
		// requests cross as their count; the clock's offset as the
		// sessions' times.
		".Peers[].ID", ".Peers[].Watch", ".Peers[].Popup",
		".Peers[].Version", ".Peers[].RTT", ".Peers[].Listed", ".Peers[].Shares",
		// A shared project's snapshot stays the TUI's remote
		// dashboard's, but for what the sidebar lists of its threads
		// (Watched). Its sessions cross as their panes, but only a
		// coordinator or a thread, which a filled role isn't: their
		// copy, and the clock's offset in it, are checked below.
		".Peers[].Offset", ".Peers[].Projects[].Rev", ".Peers[].Projects[].Sessions",
		".Peers[].Projects[].Watch.Project", ".Peers[].Projects[].Watch.NeedsYou", ".Peers[].Projects[].Watch.Inbox",
		".Peers[].Projects[].Watch.Ready", ".Peers[].Projects[].Watch.Context", ".Peers[].Projects[].Watch.Questions",
		".Peers[].Projects[].Watch.Ticker", ".Peers[].Projects[].Watch.Cells", ".Peers[].Projects[].Watch.Links",
		".Peers[].Projects[].Watch.Threads[].Reason", ".Peers[].Projects[].Watch.Threads[].Reports",
		".Peers[].Projects[].Watch.Threads[].PR", ".Peers[].Projects[].Watch.Threads[].PRURL",
		".Peers[].Projects[].Watch.Threads[].PRBad", ".Peers[].Projects[].Watch.Threads[].PRChecked",
		".Peers[].Projects[].Watch.Threads[].Task.Title", ".Peers[].Projects[].Watch.Threads[].Task.Status",
		".Peers[].Projects[].Watch.Threads[].Task.Current",
	})
	check("peer.list sessions", homeOf(func(s proto.PeerSession) PeerSession { return peerSessionOf("mini", time.Second, s) }), nil)
	check("session.prompt", homeOf(promptResultOf), nil)
	check("session.answer", homeOf(answerResultOf), nil)
	check("session.approve", homeOf(approveResultOf), nil)
	check("task.ask", homeOf(taskAskResultOf), nil)
	check("project.open", homeOf(projectOpenOf), nil)
	check("coordinator.clear", homeOf(coordinatorClearOf), nil)
	check("project.active", homeOf(projectActiveOf), nil)
	check("task.add", homeOf(taskAddedOf), nil)
	check("questions.open", homeOf(questionsOpenOf), nil)
	check("notify", homeOf(noticeResultOf), nil)
	// The window is the bridge's: it acks for the app.
	check("pane.vt", homeOf(paneResultOf), append(sessionHome(".Session"), ".Window"))
	check("pane.vt events", homeOf(paneEventOf), []string{".Event"})
	check("transcript.watch", homeOf(transcriptResultOf), append(sessionHome(".Session"), ".Window"))
	check("transcript.watch events", homeOf(transcriptEventOf), []string{".Event"})
	check("session.file", homeOf(sessionFileOf), nil)
	// The app's pane input, into the line: its id picks the stream
	// (TestParamsCopies).
	check("pane.input", homeOf(func(in PaneInputParams) proto.PaneVTInput {
		_, line := paneInput(in)
		return line.(proto.PaneVTInput)
	}), []string{".ID"})
}

// sessionHome is what of a SessionInfo at path stays home (session.list's
// list).
func sessionHome(path string) []string {
	var out []string
	for _, f := range []string{"Cells", "Argv", "Cwd", "PID", "StateSources", "AgentSID", "Identified", "RemoteHeld", "ModelNext", "ModelPending"} {
		out = append(out, path+"."+f)
	}
	return out
}

// settingsHome is what of a project's settings at path stays home: all
// but Active, Paused and Pinned, which the sidebar orders by.
func settingsHome(path string) []string {
	var out []string
	for f := range reflect.TypeFor[config.Safety]().Fields() {
		if f.IsExported() && !slices.Contains([]string{"Active", "Paused", "Pinned"}, f.Name) {
			out = append(out, path+"."+f.Name)
		}
	}
	return out
}

// TestParamsCopies: the app's params cross field by field and strictly:
// nothing it may not set (Relay) reaches the server, and an unknown
// field is refused.
func TestParamsCopies(t *testing.T) {
	for _, name := range []string{proto.MethodSessionKeys, proto.MethodSessionAnswer, proto.MethodSessionApprove} {
		m, _ := Lookup(name)
		if _, err := m.toServer(json.RawMessage(`{"id":"s-1","relay":"coordinator"}`)); err == nil {
			t.Errorf("%s took a relay", name)
		}
	}
	m, _ := Lookup(proto.MethodSessionApprove)
	p, err := m.toServer(json.RawMessage(`{"id":"s-1","choice":2,"escape":true}`))
	if want := (proto.SessionApproveParams{ID: "s-1", Choice: 2, Escape: true}); err != nil || p != want {
		t.Errorf("approve params %+v %v", p, err)
	}
	// The console a pane's takeover counts is the server's to name.
	m, _ = Lookup(proto.MethodPaneVT)
	if _, err := m.toServer(json.RawMessage(`{"session":"s-1","console":"c-1"}`)); err == nil {
		t.Error("pane.vt took a console")
	}
	m, _ = Lookup(MethodPaneInput)
	id, line, err := m.toStream(json.RawMessage(`{"id":3,"input":"aGk=","size":{"cols":80,"rows":24}}`))
	if in, ok := line.(proto.PaneVTInput); err != nil || id != 3 || !ok || string(in.Input) != "hi" || in.Size.Cols != 80 || in.Ack != 0 {
		t.Errorf("pane.input %d %+v %v", id, line, err)
	}
	if _, _, err := m.toStream(json.RawMessage(`{"id":3,"ack":5}`)); err == nil {
		t.Error("pane.input took an ack")
	}
	m, _ = Lookup(proto.MethodTaskAdd)
	p, err = m.toServer(json.RawMessage(`{"project":"web","title":"Ship it","notes":"soon"}`))
	if want := (proto.TaskAddParams{Project: "web", Title: "Ship it", Notes: "soon"}); err != nil || p != want {
		t.Errorf("task.add params %+v %v", p, err)
	}
	// Only a send-back takes a note.
	m, _ = Lookup(proto.MethodTaskAccept)
	if _, err := m.toServer(json.RawMessage(`{"project":"web","task":"T2","note":"x"}`)); err == nil {
		t.Error("task.accept took a note")
	}
	m, _ = Lookup(proto.MethodTaskSendBack)
	p, err = m.toServer(json.RawMessage(`{"project":"web","task":"T2","note":"x"}`))
	if want := (proto.TaskActionParams{Project: "web", Task: "T2", Note: "x"}); err != nil || p != want {
		t.Errorf("send-back params %+v %v", p, err)
	}
	m, _ = Lookup(proto.MethodProjectActive)
	p, err = m.toServer(json.RawMessage(`{"project":"web","active":true}`))
	if want := (proto.ProjectActiveParams{Project: "web", Active: true}); err != nil || p != want {
		t.Errorf("project.active params %+v %v", p, err)
	}
	m, _ = Lookup(proto.MethodCoordinatorClear)
	p, err = m.toServer(json.RawMessage(`{"project":"web"}`))
	if want := (proto.CoordinatorClearParams{Project: "web"}); err != nil || p != want {
		t.Errorf("coordinator.clear params %+v %v", p, err)
	}
	if _, err := m.toServer(json.RawMessage(`{"project":"web","session":"s-1"}`)); err == nil {
		t.Error("coordinator.clear took a session")
	}
	// session.file takes a ref, never a path.
	m, _ = Lookup(proto.MethodSessionFile)
	p, err = m.toServer(json.RawMessage(`{"session":"s-1","ref":"0.0123456789abcdef.1.0","offset":5,"stat":true}`))
	if want := (proto.SessionFileParams{Session: "s-1", Ref: "0.0123456789abcdef.1.0", Offset: 5, Stat: true}); err != nil || p != want {
		t.Errorf("session.file params %+v %v", p, err)
	}
	if _, err := m.toServer(json.RawMessage(`{"session":"s-1","path":"/etc/passwd"}`)); err == nil {
		t.Error("session.file took a path")
	}
	m, _ = Lookup(proto.MethodSessionList)
	for _, raw := range []string{"", "null", "{}"} {
		if _, err := m.toServer(json.RawMessage(raw)); err != nil {
			t.Errorf("session.list params %q: %v", raw, err)
		}
	}
	if _, err := m.toServer(json.RawMessage(`{"x":1}`)); err == nil {
		t.Error("session.list took a param")
	}
}

// TestAllowlist: the app API is the methods named here, no more: one
// joins on purpose. Settings, access, mods, views and the server's own
// controls never do.
func TestAllowlist(t *testing.T) {
	want := []string{proto.MethodNotifyClaim, proto.MethodNotifyDismiss, proto.MethodPeerList, proto.MethodProjectsWatch,
		proto.MethodSessionAnswer, proto.MethodSessionApprove, proto.MethodSessionKeys, proto.MethodSessionList,
		proto.MethodSessionPrompt, proto.MethodSessionWatch, proto.MethodTaskAsk,
		proto.MethodTaskAccept, proto.MethodTaskSendBack, proto.MethodTaskDelegate, proto.MethodProjectOpen,
		proto.MethodQuestionsOpen, proto.MethodTaskAdd, proto.MethodPaneVT, proto.MethodTranscriptWatch, MethodPaneInput,
		proto.MethodSessionFile, proto.MethodCoordinatorClear, proto.MethodProjectActive}
	slices.Sort(want)
	if got := MethodNames(); !slices.Equal(got, want) {
		t.Errorf("methods %v, want %v", got, want)
	}
	for _, m := range Methods() {
		if m.Stream != (len(m.Events) > 0 && m.Data != nil && m.event != nil) || m.Result == nil || m.result == nil ||
			(m.Target != "") != (m.toStream != nil) {
			t.Errorf("%s: incomplete %+v", m.Name, m)
		}
		if m.Target != "" {
			if tm, ok := Lookup(m.Target); !ok || !tm.Stream {
				t.Errorf("%s: target %s is no stream", m.Name, m.Target)
			}
		}
	}
	for name, write := range map[string]bool{MethodPaneInput: true, proto.MethodPaneVT: false, proto.MethodTranscriptWatch: false,
		proto.MethodSessionFile: false, proto.MethodCoordinatorClear: true, proto.MethodProjectActive: true} {
		if m, _ := Lookup(name); m.Write != write {
			t.Errorf("%s: write %v", name, m.Write)
		}
	}
}

// TestSafe: every string the app gets is plain text: escapes and bidi
// overrides go, newlines stay; map keys too.
func TestSafe(t *testing.T) {
	in := Projects{Projects: []Project{{Slug: "a\x1b]52;c;x\x07b", Goal: "line 1\nline 2",
		Checkouts: map[string]string{"/r\u202e": "behind\x1b[2J"},
		Threads:   []Thread{{Status: &ThreadStatus{NeedsYou: "ok?\u200b"}}}}}}
	out := safe(in).(Projects)
	p := out.Projects[0]
	if p.Slug != "a]52;c;xb" || p.Goal != "line 1\nline 2" || p.Checkouts["/r"] != "behind[2J" || p.Threads[0].Status.NeedsYou != "ok?" {
		t.Errorf("safe: %+v %+v", p, p.Threads[0].Status)
	}
	if in.Projects[0].Slug != "a\x1b]52;c;x\x07b" {
		t.Error("safe changed its argument's strings")
	}
}

// TestPeerListSessions: a paired server's coordinator and threads cross
// as their panes' ids here, on this server's clock; its other sessions
// don't.
func TestPeerListSessions(t *testing.T) {
	created := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	l := peerListOf(proto.PeerList{Peers: []proto.PeerMirror{{Alias: "mini", Pane: true, Panes: proto.PanesView, Offset: 2000,
		Projects: []proto.PeerMirrorProject{{PeerProject: proto.PeerProject{Project: "demo"}, Sessions: []proto.PeerSession{
			{ID: "s-1", Role: proto.RoleCoordinator, Project: "demo", Created: created},
			{ID: "s-2", Role: proto.RoleThread, Project: "demo", Thread: "t-0001", Asking: true},
			{ID: "s-3", Role: "shell", Project: "demo"},
		}}}}}})
	p := l.Peers[0]
	if !p.Pane || p.Panes != proto.PanesView {
		t.Fatalf("peer %+v", p)
	}
	ss := p.Projects[0].Sessions
	if len(ss) != 2 || ss[0].ID != "mini/s-1" || ss[1].ID != "mini/s-2" || !ss[1].Asking {
		t.Fatalf("sessions %+v", ss)
	}
	if want := created.Add(-2 * time.Second); !ss[0].Created.Equal(want) {
		t.Fatalf("created %v, want %v", ss[0].Created, want)
	}
	if w := p.Projects[0].Watched; w == nil || len(w) != 0 {
		t.Fatalf("watched %+v before a watch", w)
	}
}

// TestPeerListWatched: a paired server's watched threads cross, running
// or not, in its order, a running one with its pane's id here; whether
// one waits on that server's user, never its question.
func TestPeerListWatched(t *testing.T) {
	l := peerListOf(proto.PeerList{Peers: []proto.PeerMirror{{Alias: "mini",
		Projects: []proto.PeerMirrorProject{{PeerProject: proto.PeerProject{Project: "demo"}, Watch: &proto.ProjectWatch{
			Threads: []proto.WatchThread{
				{ID: "t-0001", Title: "first", Session: "s-2", State: "working", PRChecks: "fail",
					Task: &proto.WatchTask{ID: "T7", StepsDone: 1, StepsTotal: 3}},
				{ID: "t-0002", Title: "second", NeedsYou: proto.PeerQuestion, Done: true},
			}}}}}}})
	w := l.Peers[0].Projects[0].Watched
	want := []PeerThread{
		{ID: "t-0001", Title: "first", Task: "T7", StepsDone: 1, StepsTotal: 3, Session: "mini/s-2", State: "working", PRChecks: "fail"},
		{ID: "t-0002", Title: "second", NeedsYou: true, Done: true},
	}
	if !slices.Equal(w, want) {
		t.Fatalf("watched %+v", w)
	}
}

// TestSessionListUIHints: session.list carries [ui] needs_first and
// context_hint as the config says them now: false and the default hint
// without one, nothing for a hint that is off.
func TestSessionListUIHints(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TERMINATR_HOME", dir)
	if l := sessionListOf(proto.SessionListResult{}); l.NeedsFirst || l.ContextHint != config.DefaultContextHint {
		t.Fatalf("without a config: %+v", l)
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[ui]\nneeds_first = true\ncontext_hint = 60\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if l := sessionListOf(proto.SessionListResult{}); !l.NeedsFirst || l.ContextHint != 60 {
		t.Fatalf("set but not sent: %+v", l)
	}
	if err := os.WriteFile(path, []byte("[ui]\nneeds_first = false\ncontext_hint = 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l := sessionListOf(proto.SessionListResult{})
	if raw, _ := json.Marshal(l); l.NeedsFirst || strings.Contains(string(raw), "context_hint") {
		t.Fatalf("unset but sent: %s", raw)
	}
}
