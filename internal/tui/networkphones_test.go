package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/device"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/qr"
)

func (f *fakeSource) Devices() (DeviceList, error) { return f.devices, f.devErr }

func (f *fakeSource) devIndex(id string) int {
	return slices.IndexFunc(f.devices.Devices, func(e DeviceEntry) bool { return e.ID == id })
}

func (f *fakeSource) SetDeviceLevel(id, level string) error {
	f.devActs = append(f.devActs, "level "+id+" "+level)
	if i := f.devIndex(id); i >= 0 {
		f.devices.Devices[i].Level, f.devices.Devices[i].Effective = level, level
	}
	return nil
}

func (f *fakeSource) RenameDevice(id, name string) error {
	f.devActs = append(f.devActs, "name "+id+" "+name)
	if i := f.devIndex(id); i >= 0 {
		f.devices.Devices[i].Name = name
	}
	return nil
}

func (f *fakeSource) RemoveDevice(id string) error {
	f.devActs = append(f.devActs, "remove "+id)
	f.devices.Devices = slices.DeleteFunc(f.devices.Devices, func(e DeviceEntry) bool { return e.ID == id })
	return nil
}

func (f *fakeSource) PairDevice(ctx context.Context, relay string, ev DevicePairEvents) (DeviceEntry, error) {
	if f.pairDevice == nil {
		return DeviceEntry{}, errors.New("no pairing in this test")
	}
	return f.pairDevice(ctx, relay, ev)
}

// phonesList is two paired phones: Clif's iPhone at answer, connected;
// the iPad at view, away for two hours, with no write key.
func phonesList() DeviceList {
	return DeviceList{Running: true, Relay: "wss://relay.example.net", Devices: []DeviceEntry{
		{Name: "Clif's iPhone", ID: "aa11", Short: "AA11-0000", Level: device.LevelAnswer, Effective: device.LevelAnswer,
			Relay: "wss://relay.example.net", WriteKey: true, LastUp: peersAt,
			Link: &proto.DeviceLink{State: proto.LinkUp, Since: peersAt.Add(-time.Minute), LastUp: peersAt}},
		{Name: "iPad", ID: "bb22", Short: "BB22-0000", Level: device.LevelView, Effective: device.LevelView,
			Relay: "wss://relay.example.net", LastUp: peersAt.Add(-2 * time.Hour),
			Link: &proto.DeviceLink{State: proto.LinkOffline, Reason: proto.LinkReasonPeer, Since: peersAt}},
	}}
}

// phoneDash is netDash's dashboard with phonesList's phones.
func phoneDash(t *testing.T, w, h int) (*dash, *fakeSource) {
	t.Helper()
	m, src := netDash(t, w, h)
	src.devices = phonesList()
	return m, src
}

// TestNetworkPhones (T828): the paired phones follow the paired servers
// in the Network popup, each with its connection (or when it was last
// seen) and its level; the details panel spells out the level and why a
// phone can't be raised.
func TestNetworkPhones(t *testing.T) {
	m, _ := phoneDash(t, 160, 50)
	nv := netOpen(t, m)
	out := netBox(m)
	for _, want := range []string{
		"▾ phones · 2 paired",
		"D pairs a phone",
		"◉ Clif's iPhone · connected",
		"level answer",
		"○ iPad · last seen 2h ago",
		"level view",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("popup lacks %q:\n%s", want, out)
		}
	}
	nv.sel = "d:bb22"
	out = netBox(m)
	for _, want := range []string{
		"iPad · phone",
		"name (E)",
		"device id     BB22-0000",
		"relay         relay.example.net",
		"level (V)     view · answer · write",
		"No write key: it can only view.",
		"It reads projects, sessions, conversations and panes",
		"V level · E name · X remove · tab details",
	} {
		if !strings.Contains(strings.ReplaceAll(out, "[view]", "view"), want) && !strings.Contains(panelText(out), want) {
			t.Errorf("iPad's panel lacks %q:\n%s", want, out)
		}
	}
	// ← goes up to the phones' header; enter folds them.
	press(m, "left")
	if nv.sel != "d:" {
		t.Fatalf("left: %q", nv.sel)
	}
	press(m, "enter")
	if out := netBox(m); strings.Contains(out, "iPad") || !strings.Contains(out, "▸ phones · 2 paired") {
		t.Errorf("folded:\n%s", out)
	}
}

// TestNetworkPhonesNone: with none paired the popup says how to pair
// one; a phone never linked waits for it, one the server doesn't run
// says so.
func TestNetworkPhonesNone(t *testing.T) {
	m, src := netDash(t, 160, 50)
	netOpen(t, m)
	if out := netBox(m); !strings.Contains(out, "No phone yet. D pairs one: a QR code to scan with the terminatr app") {
		t.Errorf("no phones:\n%s", out)
	}
	src.devices = DeviceList{Running: false, Devices: []DeviceEntry{{Name: "iPhone", ID: "cc", Level: device.LevelView, Effective: device.LevelView}}}
	m.stack = nil
	netOpen(t, m)
	if out := netBox(m); !strings.Contains(out, "iPhone · the server isn't running") {
		t.Errorf("not running:\n%s", out)
	}
	src.devices.Running = true
	src.devices.Devices[0].Link = &proto.DeviceLink{State: proto.LinkOffline, Reason: proto.LinkReasonPeer}
	m.stack = nil
	netOpen(t, m)
	if out := netBox(m); !strings.Contains(out, "iPhone · waiting for it") {
		t.Errorf("never linked:\n%s", out)
	}
	src.devErr = errors.New("devices.json: bad")
	m.stack = nil
	netOpen(t, m)
	if out := netBox(m); !strings.Contains(out, "The phones can't be read: devices.json: bad") {
		t.Errorf("error:\n%s", out)
	}
}

// TestNetworkPhoneLevel: V picks a phone's level from the three, each
// saying what it allows; enter sets it, without a write key only view
// takes. The panel's switch moves it with ← →, asking first when it
// lets the phone do more, at once when less.
func TestNetworkPhoneLevel(t *testing.T) {
	m, src := phoneDash(t, 160, 50)
	nv := netOpen(t, m)
	nv.sel = "d:aa11"
	press(m, "V")
	lv, ok := m.top().(*levelPickView)
	if !ok || lv.sel != 1 {
		t.Fatalf("V: %T", m.top())
	}
	out := netBox(m)
	for _, want := range []string{"Level for Clif's iPhone", "( ) view", "(•) answer", "( ) write", "It does what the Mac app does", "enter set"} {
		if !strings.Contains(out, want) {
			t.Errorf("picker lacks %q:\n%s", want, out)
		}
	}
	press(m, "down")
	drain(t, m, press(m, "enter"))
	if !slices.Equal(src.devActs, []string{"level aa11 write"}) || m.msg != "Clif's iPhone is write now" {
		t.Fatalf("picked write: %v %q", src.devActs, m.msg)
	}

	// The iPad has no write key: only view.
	nv.sel = "d:bb22"
	press(m, "V", "down")
	press(m, "enter")
	if len(src.devActs) != 1 || !strings.Contains(m.msg, "no write key") {
		t.Fatalf("iPad to answer: %v %q", src.devActs, m.msg)
	}
	if _, ok := m.top().(*levelPickView); !ok {
		t.Fatalf("the picker closed: %T", m.top())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	// The panel's switch: down asks, then is set on Y; down again at once.
	nv.sel = "d:aa11"
	src.devices.Devices[0].Level, src.devices.Devices[0].Effective = device.LevelView, device.LevelView
	nv.load(m)
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if !nv.panel {
		t.Fatal("tab: the panel hasn't the keyboard")
	}
	for items := panelItems(nv.panelLines(m, netRow{kind: netDevice, dev: 0}, true, 60)); items[nv.psel] != itemLevel; {
		press(m, "down")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	cv, ok := m.top().(*confirmView)
	if !ok || !strings.HasPrefix(cv.question, "Let Clif's iPhone answer?") {
		t.Fatalf("right: %T", m.top())
	}
	drain(t, m, press(m, "Y"))
	if src.devActs[len(src.devActs)-1] != "level aa11 answer" {
		t.Fatalf("after Y: %v", src.devActs)
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	drain(t, m, cmd)
	if src.devActs[len(src.devActs)-1] != "level aa11 view" {
		t.Fatalf("left: %v", src.devActs)
	}
}

// TestNetworkPhoneRenameRemove: E renames a phone; X removes it after
// asking, N keeping it.
func TestNetworkPhoneRenameRemove(t *testing.T) {
	m, src := phoneDash(t, 160, 50)
	nv := netOpen(t, m)
	nv.sel = "d:bb22"
	press(m, "E")
	in, ok := m.top().(*inputView)
	if !ok || in.title != "Name for iPad" {
		t.Fatalf("E: %T", m.top())
	}
	in.lineEdit = newLine("Studio iPad")
	drain(t, m, press(m, "enter"))
	if !slices.Equal(src.devActs, []string{"name bb22 Studio iPad"}) || m.msg != "iPad is called Studio iPad now" {
		t.Fatalf("rename: %v %q", src.devActs, m.msg)
	}
	press(m, "X")
	cv, ok := m.top().(*confirmView)
	if !ok || cv.title != "Remove Studio iPad" || !strings.Contains(cv.question, "Its link closes now") {
		t.Fatalf("X: %T", m.top())
	}
	press(m, "N")
	if len(src.devActs) != 1 || m.msg != "Studio iPad kept" {
		t.Fatalf("N: %v %q", src.devActs, m.msg)
	}
	press(m, "X")
	drain(t, m, press(m, "Y"))
	if src.devActs[1] != "remove bb22" || !strings.HasPrefix(m.msg, "removed Studio iPad") {
		t.Fatalf("remove: %v %q", src.devActs, m.msg)
	}
	if out := netBox(m); strings.Contains(out, "Studio iPad ·") || !strings.Contains(out, "phones · 1 paired") {
		t.Errorf("after remove:\n%s", out)
	}
}

// panelText is the details panel's words in out, joined across lines.
func panelText(out string) string {
	var words []string
	for _, l := range strings.Split(out, "\n") {
		if _, right, ok := strings.Cut(l, " │ "); ok {
			words = append(words, strings.Fields(strings.TrimSuffix(right, "│"))...)
		}
	}
	return strings.Join(words, " ")
}

// phoneStep runs the pairing popup's pending command: its next step.
func phoneStep(t *testing.T, m *dash, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	msg, ok := cmd().(phonePairMsg)
	if !ok {
		t.Fatalf("not a pairing step")
	}
	_, next := m.Update(msg)
	return next
}

// TestNetworkPhonePair (T828): D shows the invite as a QR code in half
// blocks with its quiet zone, with the code as text under it, the time
// it works and this server; once the phone joins it asks as tm device
// pair does, E names it first, Y adds it and the popup closes with the
// outcome, the new phone selected.
func TestNetworkPhonePair(t *testing.T) {
	m, src := phoneDash(t, 160, 60)
	nv := netOpen(t, m)
	invite := "terminatr://pair?v=1&relay=wss%3A%2F%2Frelay.example.net&secret=7Q4MK2XD9HVAPT3C&server=" + strings.Repeat("ab", 32)
	got := make(chan string, 1)
	src.pairDevice = func(ctx context.Context, relay string, ev DevicePairEvents) (DeviceEntry, error) {
		ev.Waiting(DeviceInvite{Invite: invite, Name: "studio", Server: "4F2A-91C3", Relay: "relay.example.net", Expires: time.Now().Add(10 * time.Minute)})
		name := ev.Confirm(DeviceOffer{Name: "iPhone", Phone: "iPhone", Short: "CC33-0000", Version: "1.4", WriteKey: true,
			Level: device.LevelView, AnswerBy: time.Now().Add(2 * time.Minute),
			Check: func(s string) error {
				if s == "iPad" {
					return &device.TakenError{Name: s}
				}
				return nil
			}})
		got <- name
		return DeviceEntry{Name: name, ID: "cc33", Short: "CC33-0000", Level: device.LevelView, Effective: device.LevelView, WriteKey: true}, nil
	}
	cmd := press(m, "D")
	if _, ok := m.top().(*phonePairView); !ok {
		t.Fatalf("D: %T", m.top())
	}
	cmd = phoneStep(t, m, cmd)
	out := netBox(m)
	code, _ := qr.Encode([]byte(invite))
	want := strings.Split(strings.TrimSuffix(ansi.Strip(code.Terminal(true)), "\n"), "\n")
	for _, l := range want {
		if !strings.Contains(out, l) {
			t.Fatalf("the QR code's line %q isn't drawn:\n%s", l, out)
		}
	}
	if len(want[0]) != code.Size+2*qr.Quiet || strings.TrimSpace(want[0]) != "" {
		t.Errorf("no quiet zone: %q", want[0])
	}
	flat := strings.Join(strings.Fields(out), " ")
	for _, w := range []string{"Scan this with the terminatr app on the iPhone", "Works for 10:00 · waiting for the phone…",
		"This server: studio, id 4F2A-91C3.", "Through relay.example.net.", "Or paste this code in the app:"} {
		if !strings.Contains(flat, w) {
			t.Errorf("waiting lacks %q:\n%s", w, out)
		}
	}
	if !strings.Contains(strings.ReplaceAll(out, "\n", ""), "terminatr://pair?v=1") {
		t.Errorf("no code as text:\n%s", out)
	}

	// The phone joins: a taken name is refused, another one taken.
	cmd = phoneStep(t, m, cmd)
	out = netBox(m)
	for _, w := range []string{"A phone joined", `"iPhone", device id CC33-0000, app 1.4`, `Add it as "iPhone", a view-only device?`, "Y add · N don't add · E another name"} {
		if !strings.Contains(out, w) {
			t.Errorf("offer lacks %q:\n%s", w, out)
		}
	}
	press(m, "E")
	m.top().(*inputView).lineEdit = newLine("iPad")
	press(m, "enter")
	press(m, "Y")
	if out := netBox(m); !strings.Contains(out, "a device is already called iPad") {
		t.Fatalf("taken name:\n%s", out)
	}
	press(m, "E")
	m.top().(*inputView).lineEdit = newLine("Work iPhone")
	press(m, "enter")
	press(m, "Y")
	if n := <-got; n != "Work iPhone" {
		t.Fatalf("added as %q", n)
	}
	src.devices.Devices = append(src.devices.Devices, DeviceEntry{Name: "Work iPhone", ID: "cc33", Short: "CC33-0000", Level: device.LevelView, Effective: device.LevelView, WriteKey: true})
	phoneStep(t, m, cmd)
	if m.top() != overlay(nv) || nv.sel != "d:cc33" || m.msg != "paired Work iPhone (device id CC33-0000), view only; V on it sets its level" {
		t.Fatalf("after pairing: %T %q %q", m.top(), nv.sel, m.msg)
	}
}

// TestNetworkPhonePairRelay: with no relay known D asks for one; a
// failure says why and enter makes a new code; a window too small for
// the QR code says how big it must be, with the code as text.
func TestNetworkPhonePairRelay(t *testing.T) {
	m, src := phoneDash(t, 100, 30)
	netOpen(t, m)
	var relays []string
	src.pairDevice = func(ctx context.Context, relay string, ev DevicePairEvents) (DeviceEntry, error) {
		relays = append(relays, relay)
		if relay == "" {
			return DeviceEntry{}, errRelayNeeded
		}
		if len(relays) == 2 {
			ev.Waiting(DeviceInvite{Invite: "terminatr://pair?v=1&relay=" + strings.Repeat("x", 120), Name: "studio", Server: "4F2A-91C3",
				Relay: "relay.example.net", Expires: time.Now().Add(10 * time.Minute)})
		}
		return DeviceEntry{}, errors.New("not paired: the phone said no")
	}
	cmd := press(m, "D")
	phoneStep(t, m, cmd)
	in, ok := m.top().(*inputView)
	if !ok || !strings.Contains(in.label, "none is set up yet") {
		t.Fatalf("no relay: %T", m.top())
	}
	in.lineEdit = newLine("relay.example.net")
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	cmd = phoneStep(t, m, cmd)
	out := netBox(m)
	if !strings.Contains(out, "The QR code needs a window at least") || !strings.Contains(out, "Or paste this code in the app:") {
		t.Errorf("small window:\n%s", out)
	}
	phoneStep(t, m, cmd)
	if out := netBox(m); !strings.Contains(out, "Not paired: the phone said no") || !strings.Contains(out, "enter new code") {
		t.Fatalf("failed:\n%s", out)
	}
	cmd = press(m, "enter")
	phoneStep(t, m, cmd)
	if !slices.Equal(relays, []string{"", "relay.example.net", "relay.example.net"}) {
		t.Fatalf("relays %q", relays)
	}
}

// TestNetworkPhonesGolden: the phones at 80×24 (two detail lines) and
// 160×36 (the details panel), on their header and on each phone,
// matching testdata/network-phones-<w>x<h>.golden (go test -run
// TestNetworkPhonesGolden -update rewrites them).
func TestNetworkPhonesGolden(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {160, 36}} {
		m, _ := phoneDash(t, size[0], size[1])
		nv := netOpen(t, m)
		var b strings.Builder
		for _, sel := range []string{"d:", "d:aa11", "d:bb22"} {
			nv.sel = sel
			b.WriteString("== " + sel + "\n" + ansi.Strip(netBox(m)) + "\n")
		}
		got := b.String()
		path := filepath.Join("testdata", fmt.Sprintf("network-phones-%dx%d.golden", size[0], size[1]))
		if *updateGolden {
			if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (go test -run TestNetworkPhonesGolden -update writes it)", err)
		}
		if got != string(want) {
			t.Errorf("the Network popup's phones differ from %s:\n%s", path, got)
		}
	}
}

// TestNetworkPhonePairAgain (T832): a phone paired already says so, and
// with another write key that it comes back view-only.
func TestNetworkPhonePairAgain(t *testing.T) {
	m, _ := phoneDash(t, 160, 60)
	for _, c := range []struct {
		changed bool
		level   string
		want    []string
	}{
		{false, device.LevelAnswer, []string{`This phone is paired already, as "Old phone": pairing again keeps its level.`, `Add it as "iPhone", an answer device?`}},
		{true, device.LevelView, []string{`This phone is paired already, as "Old phone", but its write key changed: pairing again makes it view-only; set its level again after.`,
			`Add it as "iPhone", a view-only device?`}},
	} {
		pv := &phonePairView{stage: phoneOffer, name: "iPhone", offer: DeviceOffer{Name: "iPhone", Phone: "iPhone", Short: "CC33-0000",
			Version: "1.4", WriteKey: true, Again: "Old phone", KeyChanged: c.changed, Level: c.level, AnswerBy: time.Now().Add(time.Minute)}}
		out := ansi.Strip(pv.render(m))
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("key changed %v, offer lacks %q:\n%s", c.changed, w, out)
			}
		}
	}
}
