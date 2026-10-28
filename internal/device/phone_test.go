package device

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/relay"
)

// The server's end of the phone's pairing and device link (T792,
// ios/TerminatrLink): a real device.Pair and a Manager's link, through
// tm relay serve --self-signed's code on a loopback port, to the phone
// running TerminatrLink's device tests on this Mac or in the iOS
// Simulator. It skips unless TM_PHONE_SPIKE_DIR is set, as
// ios/TerminatrLink/spike/run.sh device does; the invite goes through
// that directory (invite.txt). TM_PHONE_SPIKE_MODE=declined says no to
// the phone instead.
func TestPhoneDevice(t *testing.T) {
	dir := os.Getenv("TM_PHONE_SPIKE_DIR")
	if dir == "" {
		t.Skip("TM_PHONE_SPIKE_DIR not set")
	}
	declined := os.Getenv("TM_PHONE_SPIKE_MODE") == "declined"
	cert, key, pin, err := relay.SelfSigned(filepath.Join(dir, "relay"), []netip.Addr{netip.MustParseAddr("127.0.0.1")}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rctx, rcancel := context.WithCancel(context.Background())
	t.Cleanup(rcancel)
	cfg := relay.DefaultConfig()
	cfg.Log = log.New(os.Stderr, "relay: ", log.Lmicroseconds)
	go relay.Serve(rctx, ln, relay.New(cfg), relay.TLS{CertFile: cert, KeyFile: key})
	url := "wss://" + ln.Addr().String() + "#" + relay.PinPrefix + pin
	// The dialer takes the relay's pin from its URL.
	var tc *tls.Config

	peerDir, devDir, srv := testHome(t)
	in, err := NewInvite(url, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	paired, err := Pair(ctx, srv, in, PairOptions{Name: "studio", Version: "0.57.0", RelayTLS: tc,
		Waiting: func() {
			if err := os.WriteFile(filepath.Join(dir, "invite.txt"), []byte(in.String()), 0o644); err != nil {
				t.Error(err)
			}
			t.Logf("waiting: %s", in)
		},
		Confirm: func(o Offer) (bool, error) {
			t.Logf("offer: %+v", o)
			if o.Name != "tm phone test" || o.Version == "" {
				t.Errorf("offer %+v", o)
			}
			return !declined, nil
		}})
	if declined {
		if err == nil {
			t.Fatal("paired, though declined")
		}
		return
	}
	if err != nil {
		t.Fatalf("pair: %v", err)
	}
	if _, err := Add(devDir, paired.Key, paired.Name, url); err != nil {
		t.Fatal(err)
	}
	// The link: the device's name and level first, then each line back
	// (echo), until the phone leaves.
	served := make(chan error, 1)
	m := Start(Options{Dir: devDir, PeerDir: peerDir, Serve: func(ctx context.Context, c net.Conn, d Device, level *Level) error {
		err := echo(ctx, c, d, level)
		select {
		case served <- err:
		default:
		}
		return err
	}, Logf: t.Logf, RelayTLS: tc})
	defer m.Close()
	select {
	case err := <-served:
		t.Logf("the phone left: %v", err)
	case <-ctx.Done():
		t.Fatal("the phone never linked")
	}
}
