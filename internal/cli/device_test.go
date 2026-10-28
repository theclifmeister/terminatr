package cli

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/device"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// stubDeviceServer stands in for the server: reloads are counted, and
// the links are links (nil: no server running).
func stubDeviceServer(t *testing.T, links []proto.DeviceLink) *int {
	t.Helper()
	n := 0
	oldReload, oldLinks := reloadDevices, deviceLinks
	t.Cleanup(func() { reloadDevices, deviceLinks = oldReload, oldLinks })
	reloadDevices = func() error { n++; return nil }
	deviceLinks = func() ([]proto.DeviceLink, bool) { return links, links != nil }
	return &n
}

func TestDeviceUsageAndGuard(t *testing.T) {
	h := newHarness(t)
	stubDeviceServer(t, nil)
	h.expect(2, "usage: tm device pair", human, "device")
	h.expect(2, "usage: tm device pair", human, "device", "nope")
	h.expect(2, "usage: tm device pair", human, "device", "remove")
	h.expect(2, "takes no --relay", human, "device", "list", "--relay", "r")
	h.expect(2, "--as: device name", human, "device", "pair", "--relay", "r.example.net", "--as", " x")
	h.expect(2, "--relay URL", human, "device", "pair")
	h.expect(2, "view|answer|write", human, "device", "access", "phone", "admin")
	for _, c := range [][]string{{"pair", "--relay", "r.example.net"}, {"list"}, {"remove", "phone"}, {"access", "phone", "view"}, {"name", "phone", "x"}} {
		h.expect(1, "human-only", coord, append([]string{"device"}, c...)...)
		h.expect(1, "human-only", thr, append([]string{"device"}, c...)...)
	}
}

// testWriteKey is a fresh write key's public half, as a phone sends it.
func testWriteKey(t *testing.T) []byte {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	b, err := k.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDeviceListAccessNameRemove(t *testing.T) {
	h := newHarness(t)
	if out := h.ok(human, "device", "list"); !strings.Contains(out, "no devices") {
		t.Fatalf("list before pairing: %q", out)
	}
	dir, _ := device.Dir()
	k1, _, _ := ed25519.GenerateKey(nil)
	k2, _, _ := ed25519.GenerateKey(nil)
	d1, err := device.Add(dir, k1, testWriteKey(t), "Clif's iPhone", "wss://r.example.net")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := device.Add(dir, k2, nil, "iPad", "wss://r.example.net"); err != nil {
		t.Fatal(err)
	}
	reloads := stubDeviceServer(t, []proto.DeviceLink{{Name: d1.Name, ID: d1.ID().String(), State: proto.LinkUp, Since: time.Now().Add(-2 * time.Minute)}})
	out := h.ok(human, "device", "list")
	for _, want := range []string{"Clif's iPhone  " + d1.ID().Short() + "  view  r.example.net  connected for 2m", "iPad", "no link yet"} {
		if !strings.Contains(out, want) {
			t.Errorf("list lacks %q:\n%s", want, out)
		}
	}
	var js struct {
		Devices []deviceEntry `json:"devices"`
	}
	if err := json.Unmarshal([]byte(h.ok(human, "device", "list", "--json")), &js); err != nil || len(js.Devices) != 2 || js.Devices[0].Link == nil || js.Devices[0].Effective != "view" {
		t.Fatalf("list --json: %+v, %v", js, err)
	}
	// Every level runs in this release; above view needs a write key.
	h.expect(1, "no-write-key", human, "device", "access", "ipad", "write")
	h.expect(1, "unknown-device", human, "device", "access", "watch", "view")
	h.expect(1, "no-write-key", human, "device", "access", "ipad", "answer")
	if out := h.ok(human, "device", "access", "ipad", "view"); !strings.Contains(out, "iPad is view-only") || *reloads != 1 {
		t.Fatalf("access: %q, %d reloads", out, *reloads)
	}
	if out := h.ok(human, "device", "access", "clif's iphone", "answer"); !strings.Contains(out, "Clif's iPhone is answer") || !strings.Contains(out, "Face ID") || *reloads != 2 {
		t.Fatalf("access answer: %q, %d reloads", out, *reloads)
	}
	if out := h.ok(human, "device", "list"); !strings.Contains(out, "answer") {
		t.Fatalf("list at answer: %q", out)
	}
	if out := h.ok(human, "device", "access", "clif's iphone", "write"); !strings.Contains(out, "Clif's iPhone is write") || !strings.Contains(out, "outside the sandbox") || *reloads != 3 {
		t.Fatalf("access write: %q, %d reloads", out, *reloads)
	}
	h.ok(human, "device", "access", "clif's iphone", "view")
	h.expect(1, "name-taken", human, "device", "name", "ipad", "CLIF'S IPHONE")
	h.expect(2, "device name", human, "device", "name", "ipad", "")
	if out := h.ok(human, "device", "name", "ipad", "Tablet"); !strings.Contains(out, "human (iphone: Tablet)") {
		t.Fatalf("name: %q", out)
	}
	h.expect(1, "unknown-device", human, "device", "remove", "ipad")
	if out := h.ok(human, "device", "remove", "tablet"); !strings.Contains(out, "Removed Tablet") || *reloads != 6 {
		t.Fatalf("remove: %q, %d reloads", out, *reloads)
	}
	if ds, _ := device.Load(dir); len(ds) != 1 {
		t.Fatalf("after remove: %+v", ds)
	}
}

// tm device pair shows a QR code, the phone joins with it, the user says
// yes and the device is pinned at view, its name made free.
func TestDevicePair(t *testing.T) {
	h := newHarness(t)
	reloads := stubDeviceServer(t, nil)
	s := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(s)
	t.Cleanup(func() { s.Shutdown(); hs.Close() })
	url := "ws" + strings.TrimPrefix(hs.URL, "http")
	dir, _ := device.Dir()
	k, _, _ := ed25519.GenerateKey(nil)
	if _, err := device.Add(dir, k, nil, "iPhone", url); err != nil {
		t.Fatal(err)
	}
	_, phoneKey, _ := ed25519.GenerateKey(nil)
	phone := peer.IdentityOf(phoneKey)
	wk := testWriteKey(t)
	joined := make(chan error, 1)
	old := shownInvite
	t.Cleanup(func() { shownInvite = old })
	shownInvite = func(in device.Invite) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_, err := device.Join(ctx, phone, in, "iPhone", "1.0", wk, nil)
			joined <- err
		}()
	}
	code, out, errs := h.run(human, "y\n", "device", "pair", "--relay", url)
	if code != 0 {
		t.Fatalf("pair: exit %d\n%s%s", code, out, errs)
	}
	if err := <-joined; err != nil {
		t.Fatalf("join: %v", err)
	}
	for _, want := range []string{"Scan this", "▀", `Add it as "iPhone 2", a view-only device? [y/N]`, "Paired iPhone 2"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	ds, _ := device.Load(dir)
	if len(ds) != 2 || ds[1].Name != "iPhone 2" || !ds[1].Key.Equal(phoneKey.Public().(ed25519.PublicKey)) || ds[1].Level != device.LevelView || ds[1].Relay != url || !bytes.Equal(ds[1].WriteKey, wk) {
		t.Fatalf("devices %+v", ds)
	}
	if *reloads != 1 {
		t.Fatalf("%d reloads", *reloads)
	}
}
