package tui

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// remotePopupData is a popup mini serves of acme-api.
func remotePopupData() proto.PeerPopup {
	at := time.Date(2026, 10, 14, 15, 12, 0, 0, time.Local)
	return proto.PeerPopup{Project: "acme-api", Goal: "the billing API", Repos: 2,
		Tasks: []proto.PeerTask{
			{ID: 41, Title: "Billing", Status: "review", Thread: "t-0041", StepsDone: 3, StepsTotal: 3},
			{ID: 44, Title: "Refunds", Status: "ready"},
			{ID: 45, Title: "Invoices", Status: "started", Thread: "t-0045", StepsDone: 1, StepsTotal: 4},
			{ID: 40, Title: "Setup", Status: "done"},
		},
		Settings: proto.PeerSettings{StartThreads: "propose", ParallelThreads: 3, AutoClose: "merged", CompleteTasks: "user",
			PRFollowup: true, Protection: "sandboxed", Guard: true, GuardMode: "enforce", Merge: "coordinator", MergeMethod: "squash",
			Active: true, Share: true, ThreadAgent: "claude", CoordinatorAgent: "claude", LinkBurst: 3, LinkPerHour: 10, LinkMaxKB: 8, LinkFileMB: 25, LinkFileKbps: 1024,
			ArchiveTasksDays: 30, ArchiveThreadsDays: 30, ArchiveInboxDays: 30, ArchiveJournalDays: 30},
		Links: []proto.PeerPopupLink{{Project: "terminatr", Send: true, Receive: true, Unread: 1, Last: "M-AB12-2", LastAt: at,
			Messages: []proto.PeerLinkMessage{{ID: "M-AB12-1", Out: true, Created: at, State: "read", Bytes: 20},
				{ID: "M-9C1B-2", Created: at, State: "sent", Bytes: 40, Re: "M-AB12-1"}}}},
	}
}

// popupDash is the remote dashboard of mini's acme-api, mini serving its
// popup with its pane switch for this server at panes.
func popupDash(t *testing.T, panes string) (*dash, *fakeSource) {
	t.Helper()
	m, src := remoteDash(t, "@mini/acme-api")
	src.data.Peers[0].Popup, src.data.Peers[0].Pane, src.data.Peers[0].Panes = true, true, panes
	src.popup = remotePopupData()
	m.setData(src.data)
	return m, src
}

// bodyText is the open remote popup's body, plain.
func bodyText(m *dash) string {
	v := m.top().(*remotePopupView)
	var lines []string
	for _, l := range v.box(m).body {
		lines = append(lines, strings.TrimRight(ansi.Strip(l), " "))
	}
	return strings.Join(lines, "\n")
}

// TestRemotePopup: A on a paired server's project opens its popup, read
// from that server, as the local popup lays it out under "mini's
// acme-api"; each tab shows the far data, a tab change reads it again,
// and every key that would act says to change it on mini and acts on
// nothing here.
func TestRemotePopup(t *testing.T) {
	m, src := popupDash(t, proto.PanesView)
	drain(t, m, press(m, "A"))
	v, ok := m.top().(*remotePopupView)
	if !ok {
		t.Fatalf("A opened %T (msg %q)", m.top(), m.msg)
	}
	if v.alias != "mini" || v.slug != "acme-api" || v.tab != rtabOverview || len(src.popups) != 1 || src.popups[0] != "mini/acme-api" {
		t.Fatalf("popup %+v, read %v", v, src.popups)
	}
	out := ansi.Strip(screen(m))
	for _, want := range []string{"mini's acme-api", "1 overview", "2 inbox 1", "5 messages", "read-only", "change it on mini",
		"the billing API", "2 on mini; their paths stay there", "1 needs you", "1 in motion", "1 on deck", "1 done"} {
		if !strings.Contains(out, want) {
			t.Errorf("overview lacks %q:\n%s", want, out)
		}
	}
	tabs := []struct {
		key  string
		want []string
	}{
		{"2", []string{"report", "T41"}},
		{"3", []string{"Needs you 1", "T41    review   Billing · t-0041 · 3/3 steps", "In motion 1", "On deck 1", "Done 1"}},
		{"4", []string{"Start threads", "Parallel threads", "3", "Protection", "sandboxed", "Guard", "on · enforce", "Shared"}},
		{"5", []string{"project", "title", "→ terminatr  M-AB12-1", "terminatr    M-9C1B-2", "Oct 14 15:12"}},
	}
	for i, tab := range tabs {
		drain(t, m, press(m, tab.key))
		body := bodyText(m)
		for _, want := range tab.want {
			if !strings.Contains(body, want) {
				t.Errorf("tab %s lacks %q:\n%s", tab.key, want, body)
			}
		}
		if len(src.popups) != i+2 {
			t.Errorf("tab %s: read %d times", tab.key, len(src.popups))
		}
	}
	drain(t, m, press(m, "4"))
	for _, k := range []string{"enter", "D", "A", "X", "+", "E", "c"} {
		drain(t, m, press(m, k))
		if m.top() != v {
			t.Fatalf("%s opened %T", k, m.top())
		}
		if !strings.Contains(v.hint, "change it on mini") {
			t.Errorf("%s: hint %q", k, v.hint)
		}
	}
	if len(src.delegated)+len(src.asked)+len(src.settings)+len(src.repos)+len(src.lifecycle) > 0 {
		t.Errorf("acted here: %v %v %v %v %v", src.delegated, src.asked, src.settings, src.repos, src.lifecycle)
	}
	// The Messages tab: enter opens a message's dialog, its header
	// only, read-only.
	drain(t, m, press(m, "5"))
	keyPress(m, "down")
	drain(t, m, press(m, "enter"))
	mv, ok := m.top().(*msgView)
	if !ok || mv.e.msg.ID != "M-AB12-1" {
		t.Fatalf("enter on a message opened %T", m.top())
	}
	out = ansi.Strip(screen(m))
	for _, want := range []string{"M-AB12-1 · mini/acme-api → terminatr", "From      mini/acme-api", "To        terminatr", "State     read",
		"Only its header crosses; its text is in terminatr's Messages tab here.", "↑ ↓ scroll · esc close"} {
		if !strings.Contains(out, want) {
			t.Errorf("dialog lacks %q:\n%s", want, out)
		}
	}
	for _, k := range []string{"A", "X", "C"} {
		drain(t, m, press(m, k))
		if m.top() != mv || !strings.Contains(m.msg, "change it on mini") {
			t.Errorf("%s in the dialog: %T %q", k, m.top(), m.msg)
		}
	}
	drain(t, m, press(m, "esc"))
	// A click on a message opens it too.
	drain(t, m, v.click(m, 0, 4, false))
	if mv, ok := m.top().(*msgView); !ok || mv.e.msg.ID != "M-9C1B-2" {
		t.Fatalf("a click opened %T", m.top())
	}
	drain(t, m, press(m, "esc"))
	// No memory tab: nothing of the other server's memory crosses.
	if strings.Contains(ansi.Strip(screen(m)), "memory") {
		t.Errorf("a memory tab:\n%s", ansi.Strip(screen(m)))
	}
	drain(t, m, press(m, "6"))
	if v.tab != rtabMessages {
		t.Errorf("6 went to tab %d", v.tab)
	}
	drain(t, m, press(m, "esc"))
	if m.top() != nil {
		t.Errorf("esc left %T", m.top())
	}
}

// TestRemotePopupTabs: T opens the paired project's popup on its tasks,
// I on its inbox, plain or after the prefix; over a paired server's
// pane (Over, "@alias/slug") the same popup opens.
func TestRemotePopupTabs(t *testing.T) {
	for key, tab := range map[string]int{"A": rtabOverview, "T": rtabTasks, "I": rtabInbox} {
		for _, prefixed := range []bool{false, true} {
			m, _ := popupDash(t, proto.PanesType)
			if prefixed {
				drain(t, m, press(m, m.prefix))
			}
			drain(t, m, press(m, key))
			if v, ok := m.top().(*remotePopupView); !ok || v.tab != tab {
				t.Errorf("%s (prefix %v): %T %+v, msg %q", key, prefixed, m.top(), m.top(), m.msg)
			}
		}
	}
	_, _, peers := peerData()
	peers[0].Popup, peers[0].Pane, peers[0].Panes = true, true, proto.PanesView
	src := &fakeSource{data: Data{ServerOK: true, Peers: peers}, popup: remotePopupData()}
	m := newDash(DashOptions{Source: src, Width: 120, Height: 40, Over: &Over{Key: "T", Project: "@mini/acme-api"}})
	m.setData(src.data)
	drain(t, m, m.listKey("T"))
	if v, ok := m.top().(*remotePopupView); !ok || v.tab != rtabTasks || v.alias != "mini" || v.slug != "acme-api" {
		t.Errorf("over a paired pane, T: %T %+v", m.top(), m.top())
	}
}

// TestRemotePopupRefused: a paired server whose tm serves no popup, or
// whose pane switch for this server is off, or a link that is down,
// keep the remote dashboard's message and read nothing; one that
// refuses when asked shows why in the popup.
func TestRemotePopupRefused(t *testing.T) {
	msg := "acme-api is mini's: no project popup here; your messages with it are in the Messages tab of your project linked to it"
	for name, set := range map[string]func(p *proto.PeerMirror){
		"older tm":   func(p *proto.PeerMirror) { p.Popup = false },
		"switch off": func(p *proto.PeerMirror) { p.Panes = proto.PanesOff },
		"offline":    func(p *proto.PeerMirror) { p.State = proto.LinkOffline },
	} {
		m, src := popupDash(t, proto.PanesView)
		set(&src.data.Peers[0])
		m.setData(src.data)
		drain(t, m, press(m, "A"))
		if m.top() != nil || m.msg != msg || len(src.popups) > 0 {
			t.Errorf("%s: %T %q, read %v", name, m.top(), m.msg, src.popups)
		}
	}
	m, src := popupDash(t, proto.PanesView)
	src.popupErr = proto.Errorf(proto.ErrPanesOff, "mini doesn't show its projects to this server: on mini, tm peer panes <this server> view")
	drain(t, m, press(m, "A"))
	if body := bodyText(m); !strings.Contains(body, "doesn't show its projects") {
		t.Errorf("refused: %s", body)
	}
	src.popupErr = errors.New("link down")
	m.top().(*remotePopupView).took(remotePopupMsg{alias: "mini", slug: "acme-api", pp: remotePopupData()})
	if body := bodyText(m); !strings.Contains(body, "the billing API") {
		t.Errorf("after a read: %s", body)
	}
}

// TestSafetyOfPeer: every setting a paired server sends lands in its
// key's place in config.Safety, as the Settings tab's rows read it.
func TestSafetyOfPeer(t *testing.T) {
	var ps proto.PeerSettings
	v := reflect.ValueOf(&ps).Elem()
	for i := range v.NumField() {
		switch f := v.Field(i); f.Kind() {
		case reflect.String:
			f.SetString("x" + v.Type().Field(i).Name)
		case reflect.Bool:
			f.SetBool(true)
		case reflect.Int:
			f.SetInt(int64(i + 1))
		case reflect.Slice:
			f.Set(reflect.ValueOf([]string{"m"}))
		}
	}
	s := safetyOfPeer(ps)
	for i := range v.NumField() {
		key, _, _ := strings.Cut(v.Type().Field(i).Tag.Get("json"), ",")
		if got := s.Value(key); !reflect.DeepEqual(got, v.Field(i).Interface()) {
			t.Errorf("%s: %v, sent %v", key, got, v.Field(i).Interface())
		}
	}
}

// TestRemotePopupGolden: the remote popup's tabs as they read.
func TestRemotePopupGolden(t *testing.T) {
	m, _ := popupDash(t, proto.PanesView)
	drain(t, m, press(m, "A"))
	var b strings.Builder
	for i := range rtabCount {
		m.top().(*remotePopupView).tab = i
		b.WriteString("== " + rtabNames[i] + "\n" + bodyText(m) + "\n")
	}
	got := b.String()
	path := filepath.Join("testdata", "remote-popup.golden")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (go test -run TestRemotePopupGolden -update writes it)", err)
	}
	if got != string(want) {
		t.Errorf("the remote popup differs from %s:\n%s", path, got)
	}
}
