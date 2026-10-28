package appapi_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"io"
	"log"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/appapi"
	"github.com/theclifmeister/terminatr/internal/appapi/apptest"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/device"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
	"github.com/theclifmeister/terminatr/internal/server"
)

// TestAllowed: view reads, answer adds answers and the task decisions,
// write everything; the Mac app's bridge has every method.
func TestAllowed(t *testing.T) {
	for _, m := range appapi.Methods() {
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
	want := []string{proto.MethodQuestionsOpen, proto.MethodSessionAnswer, proto.MethodSessionApprove, proto.MethodTaskAccept, proto.MethodTaskSendBack}
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
	if _, err := device.Add(devDir, k.Public().(ed25519.PublicKey), wkb, "Clif's iPhone", url); err != nil {
		t.Fatal(err)
	}
	// The access is write, which this release runs at view.
	if _, err := device.SetLevel(devDir, "Clif's iPhone", device.LevelWrite); err != nil {
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
	if h := c.Hello; h.AppAPI != appapi.Version || h.Level != device.LevelView || h.Server == nil || slices.Contains(h.Methods, proto.MethodTaskAdd) {
		t.Fatalf("hello %+v", h)
	}
	var list appapi.SessionList
	if err := c.Call(proto.MethodSessionList, nil, &list); err != nil || list.Version == "" {
		t.Fatalf("session.list: %+v %v", list, err)
	}
	if err := c.Call(proto.MethodTaskAdd, appapi.TaskAddParams{Project: "web", Title: "x"}, nil); code(err) != appapi.ErrLevel {
		t.Fatalf("task.add at view: %v", err)
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
		st.Devices[0].Level != device.LevelWrite || st.Devices[0].Effective != device.LevelView {
		t.Fatalf("status %+v %v", st.Devices, err)
	}

	// Removed: the link closes at once.
	if _, err := device.Remove(devDir, "clif's iphone"); err != nil {
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
