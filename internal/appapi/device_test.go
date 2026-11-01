package appapi_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/appapi"
	"github.com/theclifmeister/terminatr/internal/appapi/apptest"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/device"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
	"github.com/theclifmeister/terminatr/internal/server"
)

// TestAllowed: view reads, answer adds answers and the task decisions,
// write everything; the Mac app's bridge has every method.
func TestAllowed(t *testing.T) {
	for _, m := range appapi.Methods() {
		if m.Name == appapi.MethodDeviceBye {
			if appapi.Allowed(m, "") || !appapi.Allowed(m, device.LevelView) || !appapi.Allowed(m, device.LevelAnswer) || !appapi.Allowed(m, device.LevelWrite) {
				t.Errorf("%s: devices' own, at every level", m.Name)
			}
			continue
		}
		if m.DeviceOnly() {
			if appapi.Allowed(m, "") || appapi.Allowed(m, device.LevelView) || !appapi.Allowed(m, device.LevelAnswer) || !appapi.Allowed(m, device.LevelWrite) {
				t.Errorf("%s: devices' own, at the levels that sign", m.Name)
			}
			continue
		}
		if !appapi.Allowed(m, "") {
			t.Errorf("%s: not the Mac app's", m.Name)
		}
		if m.Local() {
			for _, l := range append(slices.Clone(device.Levels), "bogus") {
				if appapi.Allowed(m, l) {
					t.Errorf("%s answers in the bridge's process, and level %s has it", m.Name, l)
				}
			}
			continue
		}
		if !appapi.Allowed(m, device.LevelWrite) {
			t.Errorf("%s: not write's", m.Name)
		}
		if appapi.Allowed(m, device.LevelView) == m.Write {
			t.Errorf("%s: view %v, write %v", m.Name, appapi.Allowed(m, device.LevelView), m.Write)
		}
		if appapi.Allowed(m, "bogus") != appapi.Allowed(m, device.LevelView) {
			t.Errorf("%s: an unknown level isn't view's", m.Name)
		}
	}
	answer := map[string]bool{}
	for _, m := range appapi.Methods() {
		if m.Write && appapi.Allowed(m, device.LevelAnswer) {
			answer[m.Name] = true
		}
	}
	want := []string{proto.MethodPeerNeedAct, proto.MethodQuestionsOpen, proto.MethodSessionAnswer, proto.MethodSessionApprove, proto.MethodTaskAccept, proto.MethodTaskSendBack}
	if got := slices.Sorted(func(yield func(string) bool) {
		for k := range answer {
			if !yield(k) {
				return
			}
		}
	}); !slices.Equal(got, want) {
		t.Errorf("answer adds %v, want %v", got, want)
	}
}

// TestBridgeLevel: a bridge at a device's level lists only what the
// level allows and refuses the rest; a change of level reaches it at
// once, in a new hello.
func TestBridgeLevel(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	level := device.NewLevel(device.LevelView)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go func() {
		appapi.Serve(inR, outW, appapi.Options{Level: level.Get, MaxStreams: appapi.MaxDeviceStreams,
			Dial: func() (*client.Client, error) { return client.ConnectAs(p, false, proto.ClientApp) }})
		outW.Close()
	}()
	t.Cleanup(func() { inW.Close() })
	c, err := apptest.New(outR, inW)
	if err != nil {
		t.Fatal(err)
	}
	h := c.Hello
	if h.Level != device.LevelView || slices.Contains(h.Capabilities, appapi.CapWrite) || slices.Contains(h.Methods, proto.MethodSessionPrompt) ||
		!slices.Contains(h.Methods, proto.MethodTranscriptWatch) || h.Server == nil {
		t.Fatalf("hello %+v", h)
	}
	if err := c.Call(proto.MethodTaskAsk, appapi.TaskAskParams{Project: "web", Task: "T1", Kind: "accept"}, nil); code(err) != appapi.ErrLevel {
		t.Fatalf("task.ask at view: %v", err)
	}
	if err := c.Call(proto.MethodSessionList, nil, nil); err != nil {
		t.Fatalf("session.list at view: %v", err)
	}
	level.Set(device.LevelAnswer)
	select {
	case h := <-c.Hellos:
		if h.Level != device.LevelAnswer || !h.Sign || !slices.Contains(h.Methods, proto.MethodSessionApprove) || slices.Contains(h.Methods, proto.MethodSessionPrompt) {
			t.Fatalf("hello at answer %+v", h)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no hello after the level changed")
	}
	// Unsigned, a write is refused; this bridge has no write key, so
	// signed ones are too.
	if err := c.Call(proto.MethodSessionApprove, appapi.ApproveParams{ID: "s-99"}, nil); code(err) != appapi.ErrSignature {
		t.Fatalf("unsigned approve at answer: %v", err)
	}
	// More streams than a device's bound are refused.
	var streams []*apptest.Stream
	for range appapi.MaxDeviceStreams {
		st, err := c.Stream(proto.MethodProjectsWatch, appapi.ProjectsWatchParams{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		streams = append(streams, st)
	}
	if _, err := c.Stream(proto.MethodProjectsWatch, appapi.ProjectsWatchParams{}, nil); code(err) != appapi.ErrTooMany {
		t.Fatalf("stream %d: %v", appapi.MaxDeviceStreams+1, err)
	}
	for _, st := range streams {
		st.Cancel()
	}
}

// TestDeviceLink: a paired phone meets the server in its room on the
// relay, gets the app API at view over the pinned link, and is cut off
// at once when it is removed.
func TestDeviceLink(t *testing.T) {
	p := testPaths(t)
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	url := "ws" + strings.TrimPrefix(hs.URL, "http")
	srv, _, err := peer.EnsureIdentity(peer.DirIn(p.Home))
	if err != nil {
		t.Fatal(err)
	}
	_, k, _ := ed25519.GenerateKey(nil)
	phone := peer.IdentityOf(k)
	devDir := device.DirIn(p.Home)
	wk, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	wkb, _ := wk.PublicKey.Bytes()
	if _, err := device.Add(devDir, k.Public().(ed25519.PublicKey), wkb, "Sam's iPhone", url); err != nil {
		t.Fatal(err)
	}
	// The access is write: everything the Mac app's bridge has but the
	// bridge's own methods, each write signed.
	if _, err := device.SetLevel(devDir, "Sam's iPhone", device.LevelWrite); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.Run(ctx, server.Options{Paths: p, Log: log.New(io.Discard, "", 0), Bin: "/nonexistent/tm",
			Env: []string{"PATH=/usr/bin:/bin"}, DeviceAPI: appapi.ServeDevice})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("server did not stop")
		}
	})
	dctx, dcancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer dcancel()
	conn, err := device.Dial(dctx, phone, srv.Key.Public().(ed25519.PublicKey), url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	c, err := apptest.New(conn, conn)
	if err != nil {
		t.Fatal(err)
	}
	if h := c.Hello; h.AppAPI != appapi.Version || h.Level != device.LevelWrite || !h.Sign || h.Server == nil {
		t.Fatalf("hello %+v", h)
	}
	for _, m := range appapi.Methods() {
		if want := m.DeviceOnly() || !m.Local(); slices.Contains(c.Hello.Methods, m.Name) != want {
			t.Errorf("the write device's hello has %s: %v, want %v", m.Name, !want, want)
		}
	}
	var list appapi.SessionList
	if err := c.Call(proto.MethodSessionList, nil, &list); err != nil || list.Version == "" {
		t.Fatalf("session.list: %+v %v", list, err)
	}
	if err := c.Call(proto.MethodTaskAdd, appapi.TaskAddParams{Project: "web", Title: "x"}, nil); code(err) != appapi.ErrSignature {
		t.Fatalf("unsigned task.add at write: %v", err)
	}
	// The methods the bridge answers in its own process (the guard's
	// refusals) are the human's at a computer: on a device's link that
	// process is the server, so the hello lacks them and a call answers
	// device-level, whatever the params.
	locals := 0
	for _, m := range appapi.Methods() {
		if !m.Local() || m.DeviceOnly() {
			continue
		}
		locals++
		if slices.Contains(c.Hello.Methods, m.Name) {
			t.Errorf("the device's hello lists %s", m.Name)
		}
		for _, params := range []any{nil, map[string]string{"id": "G1", "how": "all"}} {
			if err := c.Call(m.Name, params, nil); code(err) != appapi.ErrLevel && code(err) != appapi.ErrHumanOnly {
				t.Errorf("%s from a device: %v", m.Name, err)
			}
		}
	}
	if locals < 2 || !slices.Contains(c.Hello.Methods, proto.MethodSessionList) {
		t.Fatalf("%d local methods; hello %v", locals, c.Hello.Methods)
	}
	// The status shows the link up, as the device.
	sc, err := client.Connect(p, false)
	if err != nil {
		t.Fatal(err)
	}
	defer sc.Close()
	var st proto.ServerStatus
	if err := sc.Call(proto.MethodServerStatus, nil, &st); err != nil || len(st.Devices) != 1 || st.Devices[0].State != proto.LinkUp ||
		st.Devices[0].Level != device.LevelWrite || st.Devices[0].Effective != device.LevelWrite {
		t.Fatalf("status %+v %v", st.Devices, err)
	}

	// Removed: the link closes at once.
	if _, err := device.Remove(devDir, "sam's iphone"); err != nil {
		t.Fatal(err)
	}
	if err := sc.Call(proto.MethodDeviceReload, nil, nil); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := c.Call(proto.MethodSessionList, nil, nil); err == nil {
		t.Fatal("the removed device's link still answers")
	}
}

// TestDeviceBridgeNeverHuman: the second check behind Allowed. A bridge
// that serves a device (Options.Level set) never counts its caller as
// the human for the methods it answers in its own process, even with a
// level that would let a call through.
func TestDeviceBridgeNeverHuman(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go func() {
		appapi.Serve(inR, outW, appapi.Options{Level: func() (string, <-chan struct{}) { return "", nil },
			Dial: func() (*client.Client, error) { return client.ConnectAs(p, false, proto.ClientApp) }})
		outW.Close()
	}()
	t.Cleanup(func() { inW.Close() })
	c, err := apptest.New(outR, inW)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range appapi.Methods() {
		if !m.Local() || m.DeviceOnly() {
			continue
		}
		// a write is stopped before, at its signature
		if err := c.Call(m.Name, nil, nil); code(err) != appapi.ErrHumanOnly && !(m.Write && code(err) == appapi.ErrSignature) {
			t.Errorf("%s: %v", m.Name, err)
		}
	}
}

// deviceBridge is a bridge as a device's link runs it, at level with
// the write key wk (nil for none), on the server at p.
func deviceBridge(t *testing.T, p client.Paths, level *device.Level, wk *ecdsa.PublicKey) *apptest.Client {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go func() {
		appapi.Serve(inR, outW, appapi.Options{Level: level.Get, MaxStreams: appapi.MaxDeviceStreams, WriteKey: wk,
			Dial: func() (*client.Client, error) { return client.ConnectAs(p, false, proto.ClientApp) }})
		outW.Close()
	}()
	t.Cleanup(func() { inW.Close() })
	c, err := apptest.New(outR, inW)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestSignedWrites: at answer a device's hello says writes are signed
// and lists device.nonce; a write goes on only with the write key's
// signature over its method, params and a nonce the bridge gave, each
// nonce once. Reads need nothing.
func TestSignedWrites(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	wk, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	c := deviceBridge(t, p, device.NewLevel(device.LevelAnswer), &wk.PublicKey)
	if h := c.Hello; !h.Sign || !slices.Contains(h.Methods, appapi.MethodDeviceNonce) {
		t.Fatalf("hello at answer %+v", h)
	}
	if err := c.Call(proto.MethodSessionList, nil, nil); err != nil {
		t.Fatalf("a read: %v", err)
	}
	approve := appapi.ApproveParams{ID: "s-99"}
	if err := c.Call(proto.MethodSessionApprove, approve, nil); code(err) != appapi.ErrSignature {
		t.Fatalf("unsigned: %v", err)
	}
	// A check only reads the dialog: it needs no signature.
	if err := c.Call(proto.MethodSessionApprove, appapi.ApproveParams{ID: "s-99", Check: true}, nil); code(err) != proto.ErrUnknownSession {
		t.Fatalf("unsigned check: %v", err)
	}
	// Signed by the write key: it reaches the server.
	c.Sign = apptest.Signer(wk)
	if err := c.Call(proto.MethodSessionApprove, approve, nil); code(err) != proto.ErrUnknownSession {
		t.Fatalf("signed: %v", err)
	}
	c.Sign = nil

	var n appapi.DeviceNonce
	if err := c.Call(appapi.MethodDeviceNonce, nil, &n); err != nil || len(n.Nonce) != appapi.NonceSize {
		t.Fatalf("nonce: %v %v", n, err)
	}
	raw, _ := json.Marshal(approve)
	sign := func(k *ecdsa.PrivateKey, method string, nonce, params []byte) []byte {
		sig, err := apptest.Signer(k)(appapi.SignedMessage(method, nonce, params))
		if err != nil {
			t.Fatal(err)
		}
		return sig
	}
	send := c.Raw
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	for _, bad := range []struct {
		what string
		req  appapi.Request
	}{
		{"another key's signature", appapi.Request{Method: proto.MethodSessionApprove, Params: raw,
			Auth: &appapi.Auth{Nonce: n.Nonce, Sig: sign(other, proto.MethodSessionApprove, n.Nonce, raw)}}},
		{"the nonce again", appapi.Request{Method: proto.MethodSessionApprove, Params: raw,
			Auth: &appapi.Auth{Nonce: n.Nonce, Sig: sign(wk, proto.MethodSessionApprove, n.Nonce, raw)}}},
	} {
		if err := send(bad.req); code(err) != appapi.ErrSignature {
			t.Errorf("%s: %v", bad.what, err)
		}
	}
	// A signature over other params, or another method, doesn't carry.
	for _, m := range []string{proto.MethodSessionApprove, proto.MethodSessionAnswer} {
		var n appapi.DeviceNonce
		c.Call(appapi.MethodDeviceNonce, nil, &n)
		sig := sign(wk, proto.MethodSessionApprove, n.Nonce, []byte(`{"id":"s-1"}`))
		if m == proto.MethodSessionAnswer {
			sig = sign(wk, proto.MethodSessionApprove, n.Nonce, raw)
		}
		if err := send(appapi.Request{Method: m, Params: raw, Auth: &appapi.Auth{Nonce: n.Nonce, Sig: sig}}); code(err) != appapi.ErrSignature {
			t.Errorf("%s with a signature over something else: %v", m, err)
		}
	}
	// A nonce the bridge never gave.
	if err := send(appapi.Request{Method: proto.MethodSessionApprove, Params: raw,
		Auth: &appapi.Auth{Nonce: make([]byte, appapi.NonceSize), Sig: sign(wk, proto.MethodSessionApprove, make([]byte, appapi.NonceSize), raw)}}); code(err) != appapi.ErrSignature {
		t.Errorf("a made-up nonce: %v", err)
	}

	// A bridge without a write key refuses every write, signed or not;
	// at view there is no nonce to sign with.
	nokey := deviceBridge(t, p, device.NewLevel(device.LevelAnswer), nil)
	nokey.Sign = apptest.Signer(wk)
	if err := nokey.Call(proto.MethodSessionApprove, approve, nil); code(err) != appapi.ErrSignature {
		t.Errorf("no write key: %v", err)
	}
	view := deviceBridge(t, p, device.NewLevel(device.LevelView), &wk.PublicKey)
	if view.Hello.Sign || slices.Contains(view.Hello.Methods, appapi.MethodDeviceNonce) {
		t.Errorf("hello at view %+v", view.Hello)
	}
	if err := view.Call(appapi.MethodDeviceNonce, nil, nil); code(err) != appapi.ErrLevel {
		t.Errorf("nonce at view: %v", err)
	}
}

// TestDeviceBye (T892): device.bye's reason is how ServeDevice says the
// link ended, which the device manager logs ("device X: closed: the
// device closed the link: …"); a link that just ends says the device
// closed it. It is the device's at every level, never the Mac app's.
func TestDeviceBye(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	d := device.Device{Name: "Clif's iPhone"}
	serve := func() (*apptest.Client, net.Conn, chan error) {
		ours, theirs := net.Pipe()
		done := make(chan error, 1)
		go func() {
			done <- appapi.ServeDevice(context.Background(), ours, d, device.NewLevel(device.LevelView),
				func() (*client.Client, error) { return client.ConnectAs(p, false, proto.ClientApp) }, nil)
		}()
		c, err := apptest.New(theirs, theirs)
		if err != nil {
			t.Fatal(err)
		}
		return c, theirs, done
	}
	c, conn, done := serve()
	if !slices.Contains(c.Hello.Methods, appapi.MethodDeviceBye) {
		t.Fatalf("view's hello lacks device.bye: %v", c.Hello.Methods)
	}
	if err := c.Call(appapi.MethodDeviceBye, appapi.ByeParams{Reason: "the app went to the back\x1b[2J"}, nil); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	var bye *appapi.ByeError
	if err := <-done; !errors.As(err, &bye) || err.Error() != "the device closed the link: the app went to the back[2J" {
		t.Fatalf("ended: %v", err)
	}
	_, conn, done = serve()
	conn.Close()
	if err := <-done; !errors.As(err, &bye) || err.Error() != "the device closed the link" {
		t.Fatalf("ended without a word: %v", err)
	}
	mac, err := bridge(t, p, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := mac.Call(appapi.MethodDeviceBye, appapi.ByeParams{}, nil); code(err) != appapi.ErrLevel {
		t.Fatalf("the Mac app's device.bye: %v", err)
	}
}

// TestDeviceMods: a device reads a project's mods at view; turning one
// on or off and its settings wait for write, signed, as every write.
func TestDeviceMods(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	if _, err := project.New(project.Options{Slug: "web"}); err != nil {
		t.Fatal(err)
	}
	wk, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	set := appapi.ModsSetParams{Project: "web", Mod: "nope", On: true}
	setting := appapi.ModsSettingParams{Project: "web", Mod: "nope", Key: "k"}
	for _, level := range []string{device.LevelView, device.LevelAnswer} {
		c := deviceBridge(t, p, device.NewLevel(level), &wk.PublicKey)
		c.Sign = apptest.Signer(wk)
		var mods appapi.Mods
		if err := c.Call(proto.MethodModsList, appapi.ModsListParams{Project: "web"}, &mods); err != nil || mods.Project != "web" {
			t.Fatalf("mods.list at %s: %+v %v", level, mods, err)
		}
		if slices.Contains(c.Hello.Methods, proto.MethodModsSet) || !slices.Contains(c.Hello.Methods, proto.MethodModsList) {
			t.Errorf("hello at %s: %v", level, c.Hello.Methods)
		}
		if err := c.Call(proto.MethodModsSet, set, nil); code(err) != appapi.ErrLevel {
			t.Errorf("mods.set at %s: %v", level, err)
		}
		if err := c.Call(proto.MethodModsSetting, setting, nil); code(err) != appapi.ErrLevel {
			t.Errorf("mods.setting at %s: %v", level, err)
		}
	}
	c := deviceBridge(t, p, device.NewLevel(device.LevelWrite), &wk.PublicKey)
	if err := c.Call(proto.MethodModsSet, set, nil); code(err) != appapi.ErrSignature {
		t.Errorf("unsigned mods.set at write: %v", err)
	}
	c.Sign = apptest.Signer(wk)
	// Signed at write, it reaches the server, which knows no such mod.
	if err := c.Call(proto.MethodModsSet, set, nil); code(err) != proto.ErrBadParams {
		t.Errorf("mods.set at write: %v", err)
	}
	if err := c.Call(proto.MethodModsSetting, setting, nil); code(err) != proto.ErrBadParams {
		t.Errorf("mods.setting at write: %v", err)
	}
}

// TestDeviceSearch: search reads reach a device at every level, with the
// human's scope (the coordinator's notes and the inbox too), through
// the app's own params (no reindex) and types (no folder path).
func TestDeviceSearch(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	pr, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"memory/relay.md":                         "## Decided\n- The wombat relay runs on port 4433.\n",
		"memory/coord.md":                         "---\naudience: coordinator\n---\n## Decided\n- Wombat reviews on Fridays.\n",
		"inbox/20261031T100000Z-report-t-0001.md": "The wombat report.\n",
	} {
		path := pr.Path(filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, level := range []string{device.LevelView, device.LevelAnswer, device.LevelWrite} {
		c := deviceBridge(t, p, device.NewLevel(level), nil)
		for _, m := range []string{proto.MethodSearchQuery, proto.MethodSearchTimeline, proto.MethodSearchShow} {
			if !slices.Contains(c.Hello.Methods, m) {
				t.Errorf("hello at %s lacks %s", level, m)
			}
		}
		var r appapi.SearchResult
		if err := c.Call(proto.MethodSearchQuery, appapi.SearchQueryParams{Project: "web", Words: "wombat"}, &r); err != nil {
			t.Fatalf("search.query at %s: %v", level, err)
		}
		var ids []string
		for _, h := range r.Hits {
			ids = append(ids, h.ID)
		}
		for _, want := range []string{"note:relay#decided", "note:coord#decided", "inbox:20261031T100000Z-report-t-0001"} {
			if !slices.Contains(ids, want) {
				t.Errorf("search.query at %s: %v lacks %s", level, ids, want)
			}
		}
		var tl appapi.SearchTimeline
		if err := c.Call(proto.MethodSearchTimeline, appapi.SearchTimelineParams{Project: "web", ID: "note:coord#decided"}, &tl); err != nil || tl.At.ID != "note:coord#decided" {
			t.Errorf("search.timeline at %s: %+v %v", level, tl, err)
		}
		var sh appapi.SearchShown
		if err := c.Call(proto.MethodSearchShow, appapi.SearchShowParams{Project: "web", IDs: []string{"note:coord#decided", "nope"}}, &sh); err != nil ||
			len(sh.Items) != 2 || !strings.Contains(sh.Items[0].Text, "Fridays") || sh.Items[0].Hit.Path != "memory/coord.md" || sh.Items[1].Error == "" {
			t.Errorf("search.show at %s: %+v %v", level, sh, err)
		}
	}
	c := deviceBridge(t, p, device.NewLevel(device.LevelView), nil)
	raw := json.RawMessage(`{"project":"web","words":"wombat","reindex":true}`)
	if err := c.Call(proto.MethodSearchQuery, raw, nil); code(err) != appapi.ErrBadParams {
		t.Errorf("search.query took a reindex: %v", err)
	}
}
