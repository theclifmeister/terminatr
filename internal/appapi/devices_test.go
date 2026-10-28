package appapi_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/appapi"
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/device"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// TestBridgeDevices: Settings › Devices (app API 26) is the human's: an
// agent's bridge is refused; the human's lists the devices, pairs a
// phone through the QR code's invite with tm device pair's
// confirmation, and sets a device's level, renames and removes it with
// tm device's checks.
func TestBridgeDevices(t *testing.T) {
	p := testPaths(t)
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	url := "ws" + strings.TrimPrefix(hs.URL, "http")
	devDir := device.DirIn(p.Home)
	old, _, _ := ed25519.GenerateKey(nil)
	if _, err := device.Add(devDir, old, nil, "iPhone", url); err != nil {
		t.Fatal(err)
	}
	// It was linked before (app API 27's last_up, kept in the devices).
	seen := time.Date(2026, 10, 28, 9, 30, 0, 0, time.UTC)
	if err := device.SetLastUp(devDir, peer.IDOf(old), seen); err != nil {
		t.Fatal(err)
	}
	startServer(t, p)

	agent, err := bridgeWith(t, appapi.Options{Paths: p, Caller: caller.Caller{Kind: caller.Thread, Project: "web", Thread: "t-0001"}})
	if err != nil {
		t.Fatal(err)
	}
	for m, params := range map[string]any{appapi.MethodDeviceList: nil, appapi.MethodDevicePair: appapi.DevicePairParams{Relay: url},
		appapi.MethodDeviceRemove: appapi.DeviceParams{Device: "iPhone"}, appapi.MethodDeviceAccess: appapi.DeviceAccessParams{Device: "iPhone", Level: device.LevelView}} {
		if err := agent.Call(m, params, nil); code(err) != appapi.ErrHumanOnly {
			t.Errorf("an agent's %s: %v", m, err)
		}
	}

	c, err := bridge(t, p, false)
	if err != nil {
		t.Fatal(err)
	}
	var list appapi.Devices
	if err := c.Call(appapi.MethodDeviceList, nil, &list); err != nil || len(list.Devices) != 1 || !list.Running ||
		!slices.Equal(list.Levels, device.Levels) || list.Relay == "" {
		t.Fatalf("device.list: %+v %v", list, err)
	}
	if d := list.Devices[0]; d.Name != "iPhone" || d.Level != device.LevelView || d.WriteKey || len(d.Short) != 9 || !d.LastUp.Equal(seen) {
		t.Errorf("device %+v", d)
	}

	// The phone joins with what the QR code holds; the user answers
	// under a name another device has, then under a free one.
	var pg appapi.DevicePairing
	if err := c.Call(appapi.MethodDevicePair, appapi.DevicePairParams{Relay: url}, &pg); err != nil || pg.Pairing == "" ||
		time.Until(pg.Expires) < 9*time.Minute || pg.Server == "" {
		t.Fatalf("device.pair: %+v %v", pg, err)
	}
	in, err := device.ParseInvite(pg.Invite)
	if err != nil {
		t.Fatalf("invite %q: %v", pg.Invite, err)
	}
	_, phoneKey, _ := ed25519.GenerateKey(nil)
	wk, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	wkb, _ := wk.PublicKey.Bytes()
	joined := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := device.Join(ctx, peer.IdentityOf(phoneKey), in, "iPhone", "1.0", wkb, nil)
		joined <- err
	}()
	var offer appapi.DeviceOffer
	if err := c.Call(appapi.MethodDevicePairWait, appapi.DevicePairingParams{Pairing: pg.Pairing}, &offer); err != nil {
		t.Fatalf("device.pair.wait: %v", err)
	}
	if offer.Name != "iPhone 2" || offer.Phone != "iPhone" || offer.Version != "1.0" || !offer.WriteKey || offer.Again != "" ||
		offer.Level != device.LevelView || offer.AnswerBy.IsZero() {
		t.Errorf("offer %+v", offer)
	}
	if err := c.Call(appapi.MethodDevicePairAnswer, appapi.DevicePairAnswerParams{Pairing: "nope", Accept: true}, nil); code(err) != appapi.ErrPairEnded {
		t.Errorf("another pairing's answer: %v", err)
	}
	if err := c.Call(appapi.MethodDevicePairAnswer, appapi.DevicePairAnswerParams{Pairing: pg.Pairing, Accept: true, Name: "IPHONE"}, nil); code(err) != "name-taken" {
		t.Errorf("a taken name: %v", err)
	}
	var paired appapi.DevicePaired
	if err := c.Call(appapi.MethodDevicePairAnswer, appapi.DevicePairAnswerParams{Pairing: pg.Pairing, Accept: true, Name: "Clif's iPhone"}, &paired); err != nil ||
		paired.Device == nil || paired.Device.Name != "Clif's iPhone" || !paired.Device.WriteKey {
		t.Fatalf("device.pair.answer: %+v %v", paired, err)
	}
	if err := <-joined; err != nil {
		t.Fatalf("join: %v", err)
	}
	ds, _ := device.Load(devDir)
	if len(ds) != 2 || ds[1].Name != "Clif's iPhone" || !ds[1].Key.Equal(phoneKey.Public()) || ds[1].Relay != url || !bytes.Equal(ds[1].WriteKey, wkb) {
		t.Fatalf("devices %+v", ds)
	}
	if err := c.Call(appapi.MethodDevicePairWait, appapi.DevicePairingParams{Pairing: pg.Pairing}, nil); code(err) != appapi.ErrPairEnded {
		t.Errorf("device.pair.wait after the pairing ended: %v", err)
	}

	id := paired.Device.ID

	// A no before any phone joined ends the pairing.
	paired = appapi.DevicePaired{}
	if err := c.Call(appapi.MethodDevicePair, appapi.DevicePairParams{}, &pg); err != nil {
		t.Fatalf("device.pair with the known relay: %v", err)
	}
	if err := c.Call(appapi.MethodDevicePairAnswer, appapi.DevicePairAnswerParams{Pairing: pg.Pairing}, &paired); err != nil || paired.Device != nil {
		t.Fatalf("device.pair.answer no: %+v %v", paired, err)
	}
	if err := c.Call(appapi.MethodDevicePairWait, appapi.DevicePairingParams{Pairing: pg.Pairing}, nil); code(err) != appapi.ErrPairEnded {
		t.Errorf("device.pair.wait after a no: %v", err)
	}

	// The level: above view needs a write key; every level runs.
	var d appapi.Device
	for _, bad := range []struct {
		params appapi.DeviceAccessParams
		code   string
	}{
		{appapi.DeviceAccessParams{Device: "iPhone", Level: device.LevelAnswer}, "no-write-key"},
		{appapi.DeviceAccessParams{Device: "iPhone", Level: device.LevelWrite}, "no-write-key"},
		{appapi.DeviceAccessParams{Device: id, Level: "admin"}, appapi.ErrBadParams},
		{appapi.DeviceAccessParams{Device: "Android", Level: device.LevelView}, "unknown-device"},
	} {
		if err := c.Call(appapi.MethodDeviceAccess, bad.params, nil); code(err) != bad.code {
			t.Errorf("device.access %+v: %v, want %s", bad.params, err, bad.code)
		}
	}
	if err := c.Call(appapi.MethodDeviceAccess, appapi.DeviceAccessParams{Device: id, Level: device.LevelAnswer}, &d); err != nil ||
		d.Level != device.LevelAnswer || d.Effective != device.LevelAnswer {
		t.Errorf("device.access answer: %+v %v", d, err)
	}
	if err := c.Call(appapi.MethodDeviceAccess, appapi.DeviceAccessParams{Device: id, Level: device.LevelWrite}, &d); err != nil ||
		d.Level != device.LevelWrite || d.Effective != device.LevelWrite {
		t.Errorf("device.access write: %+v %v", d, err)
	}

	// Pairing it again with another write key (T832): the offer says so
	// and it comes back at view.
	wk2, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	wkb2, _ := wk2.PublicKey.Bytes()
	if err := c.Call(appapi.MethodDevicePair, appapi.DevicePairParams{}, &pg); err != nil {
		t.Fatalf("device.pair again: %v", err)
	}
	in, err = device.ParseInvite(pg.Invite)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := device.Join(ctx, peer.IdentityOf(phoneKey), in, "iPhone", "1.1", wkb2, nil)
		joined <- err
	}()
	offer = appapi.DeviceOffer{}
	if err := c.Call(appapi.MethodDevicePairWait, appapi.DevicePairingParams{Pairing: pg.Pairing}, &offer); err != nil ||
		offer.Again != "Clif's iPhone" || !offer.KeyChanged || offer.Level != device.LevelView || offer.Name != "iPhone 2" {
		t.Fatalf("offer again: %+v %v", offer, err)
	}
	paired = appapi.DevicePaired{}
	if err := c.Call(appapi.MethodDevicePairAnswer, appapi.DevicePairAnswerParams{Pairing: pg.Pairing, Accept: true, Name: "Clif's iPhone"}, &paired); err != nil ||
		paired.Device == nil || paired.Device.Level != device.LevelView || paired.Device.ID != id {
		t.Fatalf("device.pair.answer again: %+v %v", paired, err)
	}
	if err := <-joined; err != nil {
		t.Fatalf("join again: %v", err)
	}
	if err := c.Call(appapi.MethodDeviceName, appapi.DeviceNameParams{Device: id, Name: "iphone"}, nil); code(err) != "name-taken" {
		t.Errorf("device.name to a taken name: %v", err)
	}
	if err := c.Call(appapi.MethodDeviceName, appapi.DeviceNameParams{Device: id, Name: "line\none"}, nil); code(err) != appapi.ErrBadParams {
		t.Errorf("device.name to two lines: %v", err)
	}
	if err := c.Call(appapi.MethodDeviceName, appapi.DeviceNameParams{Device: id, Name: "Work phone"}, &d); err != nil || d.Name != "Work phone" || d.ID != id {
		t.Errorf("device.name: %+v %v", d, err)
	}
	if err := c.Call(appapi.MethodDeviceRemove, appapi.DeviceParams{Device: id}, nil); err != nil {
		t.Errorf("device.remove: %v", err)
	}
	if err := c.Call(appapi.MethodDeviceList, nil, &list); err != nil || len(list.Devices) != 1 || list.Devices[0].Name != "iPhone" {
		t.Errorf("device.list after: %+v %v", list, err)
	}
}
