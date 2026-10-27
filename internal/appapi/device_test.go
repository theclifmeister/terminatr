package appapi_test

import (
	"context"
	"crypto/ed25519"
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
		if !appapi.Allowed(m, "") || appapi.Allowed(m, device.LevelWrite) == (m.Name == appapi.MethodGuardDecide) {
			t.Errorf("%s: the Mac app's %v, write's %v", m.Name, appapi.Allowed(m, ""), appapi.Allowed(m, device.LevelWrite))
		}
		if m.Name == appapi.MethodGuardDecide {
			continue
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
		if h.Level != device.LevelAnswer || !slices.Contains(h.Methods, proto.MethodSessionApprove) || slices.Contains(h.Methods, proto.MethodSessionPrompt) {
			t.Fatalf("hello at answer %+v", h)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no hello after the level changed")
	}
	if err := c.Call(proto.MethodSessionApprove, appapi.ApproveParams{ID: "s-99"}, nil); code(err) != proto.ErrUnknownSession {
		t.Fatalf("approve at answer reaches the server: %v", err)
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
	if _, err := device.Add(devDir, k.Public().(ed25519.PublicKey), "Clif's iPhone", url); err != nil {
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
