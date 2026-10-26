package appapi

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

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
	check("projects.watch", homeOf(projectsOf), []string{
		// Settings and mods are never in the app API.
		".Projects[].Safety", ".Projects[].Own", ".Projects[].Badges",
		// A task's archive flag: archived tasks aren't sent.
		".Projects[].NeedsYou[].Archived", ".Projects[].Threads[].Task.Archived",
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
	})
	check("peer.list", homeOf(peerListOf), []string{
		// The link's id, versions, clock and pane switches: the
		// Network popup's. Share requests cross as their count.
		".Peers[].ID", ".Peers[].Watch", ".Peers[].Popup", ".Peers[].Pane", ".Peers[].Panes",
		".Peers[].Version", ".Peers[].Offset", ".Peers[].RTT", ".Peers[].Listed", ".Peers[].Shares",
		// A shared project's snapshot: its sessions and watch stay the
		// TUI's remote dashboard's.
		".Peers[].Projects[].Rev", ".Peers[].Projects[].Watch", ".Peers[].Projects[].Sessions",
	})
	check("session.prompt", homeOf(promptResultOf), nil)
	check("session.answer", homeOf(answerResultOf), nil)
	check("session.approve", homeOf(approveResultOf), nil)
	check("task.ask", homeOf(taskAskResultOf), nil)
	check("notify", homeOf(noticeResultOf), nil)
	// The window is the bridge's: it acks for the app.
	check("pane.vt", homeOf(paneResultOf), append(sessionHome(".Session"), ".Window"))
	check("pane.vt events", homeOf(paneEventOf), []string{".Event"})
	check("transcript.watch", homeOf(transcriptResultOf), append(sessionHome(".Session"), ".Window"))
	check("transcript.watch events", homeOf(transcriptEventOf), []string{".Event"})
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
		proto.MethodPaneVT, proto.MethodTranscriptWatch, MethodPaneInput}
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
	for name, write := range map[string]bool{MethodPaneInput: true, proto.MethodPaneVT: false, proto.MethodTranscriptWatch: false} {
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
