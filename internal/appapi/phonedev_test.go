package appapi_test

import (
	"context"
	"errors"
	"log"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/appapi"
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/device"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// TestPhoneDev is a dev server for the iPhone app in the Simulator
// (T792, ios/Makefile's sim-dev): a real tm server in a scratch home
// with a project that needs the user, its device links on, and tm relay
// serve --self-signed's code on a loopback port, both in this process
// (a thread may run neither tm relay serve nor tm device pair). It skips
// unless TM_PHONE_DEV_DIR is set. In that directory it writes
// invite.txt, a fresh pairing code each time one is used (the app's
// -pair launch argument takes it), and it watches two files: asleep,
// while it exists, stops the server (the phone shows it asleep or
// offline); stop ends the test. It runs at most TM_PHONE_DEV_MINUTES
// (default 20).
func TestPhoneDev(t *testing.T) {
	dir := os.Getenv("TM_PHONE_DEV_DIR")
	if dir == "" {
		t.Skip("TM_PHONE_DEV_DIR not set")
	}
	minutes := 20
	if m, err := time.ParseDuration(os.Getenv("TM_PHONE_DEV_MINUTES") + "m"); err == nil && m > 0 {
		minutes = int(m.Minutes())
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(minutes)*time.Minute)
	defer cancel()

	p := testPaths(t)
	pr, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	human := caller.Caller{Kind: caller.Human}
	if _, err := pr.Tasks().Add(human, []tasks.NewTask{{Title: "Ship the phone link"}, {Title: "Review the pairing screen"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := pr.AddQuestion(project.Question{Question: "Which relay should the phone use?",
		Options: []project.QuestionOption{{Label: "The VPS"}, {Label: "The Docker one"}}}); err != nil {
		t.Fatal(err)
	}

	cert, key, pin, err := relay.SelfSigned(filepath.Join(dir, "relay"), []netip.Addr{netip.MustParseAddr("127.0.0.1")}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := relay.DefaultConfig()
	cfg.Log = log.New(os.Stderr, "relay: ", log.Lmicroseconds)
	go relay.Serve(ctx, ln, relay.New(cfg), relay.TLS{CertFile: cert, KeyFile: key})
	url := "wss://" + ln.Addr().String() + "#" + relay.PinPrefix + pin
	t.Logf("relay %s, home %s", url, p.Home)

	srv, _, err := peer.EnsureIdentity(peer.DirIn(p.Home))
	if err != nil {
		t.Fatal(err)
	}
	devDir := device.DirIn(p.Home)
	logger := log.New(os.Stderr, "server: ", log.Lmicroseconds)
	run := func() (stop func()) {
		sctx, scancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() {
			done <- server.Run(sctx, server.Options{Paths: p, Log: logger, Bin: "/nonexistent/tm",
				Env: []string{"PATH=/usr/bin:/bin"}, DeviceAPI: appapi.ServeDevice})
		}()
		return func() {
			scancel()
			<-done
		}
	}
	stopServer := run()
	defer func() { stopServer() }()

	// Pairing, over and over: each code once, the user's yes given here.
	go func() {
		for ctx.Err() == nil {
			in, err := device.NewInvite(url, srv.ID)
			if err != nil {
				t.Error(err)
				return
			}
			paired, err := device.Pair(ctx, srv, in, device.PairOptions{Name: "studio", Version: "dev", Waiting: func() {
				os.WriteFile(filepath.Join(dir, "invite.txt"), []byte(in.String()), 0o644)
				t.Logf("pair: %s", in)
			}, Confirm: func(o device.Offer) (bool, error) {
				t.Logf("pair: yes to %q %s (%s)", o.Name, o.Version, o.ID)
				return true, nil
			}})
			if err != nil {
				t.Logf("pair: %v", err)
				continue
			}
			d, err := device.Add(devDir, paired.Key, paired.Name, url)
			if err != nil {
				t.Logf("pair: %v", err)
				continue
			}
			t.Logf("paired %s", d.Name)
			if c, err := client.Connect(p, false); err == nil {
				c.Call(proto.MethodDeviceReload, nil, nil)
				c.Close()
			}
		}
	}()

	asleep := false
	for ctx.Err() == nil {
		if _, err := os.Stat(filepath.Join(dir, "stop")); err == nil {
			return
		}
		_, err := os.Stat(filepath.Join(dir, "asleep"))
		switch want := err == nil; {
		case want && !asleep:
			t.Log("server: asleep")
			stopServer()
			stopServer = func() {}
			asleep = true
		case !want && asleep:
			if !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			t.Log("server: awake")
			stopServer = run()
			asleep = false
		}
		time.Sleep(200 * time.Millisecond)
	}
}
