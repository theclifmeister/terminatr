package peer

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// A link says which tm its relay runs, and either server of a live link
// can ask the relay to update, with its identity key.
func TestRelayVersionAndUpdate(t *testing.T) {
	file := filepath.Join(t.TempDir(), "update-request")
	cfg := relay.DefaultConfig()
	cfg.Version, cfg.UpdateFile = "v0.27.0", file
	cfg.UpdateAfter = 0 // the link may ask once it is up
	s := relay.New(cfg)
	hs := httptest.NewUnstartedServer(s)
	hs.StartTLS()
	t.Cleanup(func() { s.Shutdown(); hs.Close() })
	tc := hs.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	url := "wss" + strings.TrimPrefix(hs.URL, "https")

	a, b := newServer(t), newServer(t)
	a.pair(t, "mini", b, url)
	b.pair(t, "studio", a, url)
	ma := a.start(t, "0.27.0", tc)
	ctx := context.Background()
	// Not linked yet: the relay refuses.
	if _, res, err := RequestRelayUpdate(ctx, a.dir, "mini", tc); err != nil || res.Result != relay.UpdateNotLinked {
		t.Fatalf("alone: %+v %v", res, err)
	}
	b.start(t, "0.27.0", tc)
	pa := waitLink(t, ma, "mini", "up", isUp)
	if pa.RelayVersion != "v0.27.0" {
		t.Fatalf("relay version %q", pa.RelayVersion)
	}
	p, res, err := RequestRelayUpdate(ctx, b.dir, "studio", tc)
	if err != nil || res.Result != relay.UpdateQueued || p.Alias != "studio" {
		t.Fatalf("linked: %+v %v", res, err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal(err)
	}
	if words, ok := RelayUpdateWords("relay.example.net", res); !ok || !strings.Contains(words, "updates to the latest release") {
		t.Fatalf("words %q", words)
	}
	_, res, _ = RequestRelayUpdate(ctx, a.dir, "mini", tc)
	if words, ok := RelayUpdateWords("relay.example.net", res); ok || !strings.Contains(words, "it takes another after") {
		t.Fatalf("too soon: %+v %q", res, words)
	}
	// The link carried on.
	waitLink(t, ma, "mini", "still up", func(p proto.PeerLink) bool { return p.State == proto.LinkUp })
	var unknown *UnknownPeerError
	if _, _, err := RequestRelayUpdate(ctx, a.dir, "nas", tc); !errors.As(err, &unknown) {
		t.Fatalf("unknown: %v", err)
	}
}
