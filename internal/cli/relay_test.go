package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/relay"
)

func TestRelayServeFlags(t *testing.T) {
	h := newHarness(t)
	h.expect(2, "usage: tm relay serve", human, "relay")
	h.expect(2, "--cert and --key go together", human, "relay", "serve", "--cert", "c.pem")
	h.expect(2, "or --plain", human, "relay", "serve")
	h.expect(2, "or --plain", human, "relay", "serve", "--plain", "--cert", "c", "--key", "k")
	h.expect(2, "--trusted-proxy nope", human, "relay", "serve", "--plain", "--trusted-proxy", "nope")
	h.expect(2, "--max-per-ip 0", human, "relay", "serve", "--plain", "--max-per-ip", "0")
	h.expect(1, "human-only", coord, "relay", "serve", "--plain")
	h.expect(1, "human-only", thr, "relay", "serve", "--plain")

	var gotAddr, gotCert string
	defer func(old func(context.Context, string, *relay.Server, string, string) error) { relayServe = old }(relayServe)
	relayServe = func(ctx context.Context, addr string, s *relay.Server, cert, key string) error {
		gotAddr, gotCert = addr, cert
		return nil
	}
	h.ok(human, "relay", "serve", "--plain", "--listen", "127.0.0.1:8080",
		"--trusted-proxy", "172.16.0.0/12", "--trusted-proxy", "10.1.2.3", "--max-conns", "50", "--bandwidth", "1.5")
	if gotAddr != "127.0.0.1:8080" || gotCert != "" {
		t.Fatalf("addr %q cert %q", gotAddr, gotCert)
	}
	h.ok(human, "relay", "serve", "--cert", "c.pem", "--key", "k.pem")
	if gotAddr != ":443" || gotCert != "c.pem" {
		t.Fatalf("defaults: addr %q cert %q", gotAddr, gotCert)
	}
}

// The Dockerfile, the compose example and the systemd unit run tm relay
// serve with flags it takes.
func TestRelayDeployFilesUseRealFlags(t *testing.T) {
	h := newHarness(t)
	defer func(old func(context.Context, string, *relay.Server, string, string) error) { relayServe = old }(relayServe)
	relayServe = func(context.Context, string, *relay.Server, string, string) error { return nil }
	dir := filepath.Join("..", "..", "deploy", "relay")
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	jsonLine := func(text, prefix string) []string {
		for _, l := range strings.Split(text, "\n") {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(l), prefix); ok {
				var args []string
				if err := json.Unmarshal([]byte(rest), &args); err != nil {
					t.Fatalf("%s: %v", l, err)
				}
				return args
			}
		}
		t.Fatalf("no %q line", prefix)
		return nil
	}
	runs := map[string][]string{
		"Dockerfile":        jsonLine(read("Dockerfile"), "CMD "),
		"compose.acme.yaml": jsonLine(read("compose.acme.yaml"), "command: "),
	}
	for _, l := range strings.Split(read("tm-relay.service"), "\n") {
		if rest, ok := strings.CutPrefix(l, "ExecStart=/usr/local/bin/tm relay serve "); ok {
			runs["tm-relay.service"] = strings.Fields(rest)
		}
	}
	if len(runs["tm-relay.service"]) == 0 {
		t.Fatal("tm-relay.service: no ExecStart running tm relay serve")
	}
	for name, args := range runs {
		code, out, errs := h.run(human, "", append([]string{"relay", "serve"}, args...)...)
		if code != 0 {
			t.Errorf("%s: tm relay serve %v: exit %d %s%s", name, args, code, out, errs)
		}
	}
}
