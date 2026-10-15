package relay

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
)

const evilEsc = "\x1b]52;c;cHduZWQ=\x07\x1b[2J"

func TestCleanVersion(t *testing.T) {
	for _, v := range []string{"v0.28.0", "0.28.0", "v1.2.3-rc.1"} {
		if CleanVersion(v) != v {
			t.Errorf("%q dropped", v)
		}
	}
	for _, v := range []string{"", "dev", "v9.9.9" + evilEsc, "v0.28.0\n", "v0.28.0 x", strings.Repeat("1", 200)} {
		if got := CleanVersion(v); got != "" {
			t.Errorf("%q kept as %q", v, got)
		}
	}
}

func TestCleanReason(t *testing.T) {
	if cleanReason(ErrPeerGone) != ErrPeerGone || cleanReason("") != "" {
		t.Fatal("known words must pass")
	}
	if got := cleanReason("\x1b]0;owned\x07\x1b[31mfake"); got != ReasonUnknown {
		t.Fatalf("got %q", got)
	}
}

func TestRelayChallengeVersionIsChecked(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Version = "v9.9.9" + evilEsc
	_, url, tc := relayFor(t, cfg)
	ca, _, _, _ := mate(t, url, tc)
	if v := ca.RelayVersion(); v != "" {
		t.Fatalf("version %q", v)
	}
}

func TestRelayCloseReasonIsMapped(t *testing.T) {
	hs := http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrade(w, r, maxMessage)
		if err != nil {
			return
		}
		ws.CloseWith(4400, "\x1b]0;owned\x07\x1b[31mfake")
	})}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go hs.Serve(ln)
	defer hs.Close()
	_, err = linkDialer(newKey(t), newKey(t), nil).Dial(context.Background(), "ws://"+ln.Addr().String())
	if err == nil || strings.ContainsAny(err.Error(), "\x1b\x07") || !strings.Contains(err.Error(), ReasonUnknown) {
		t.Fatalf("err %q", err)
	}
}
