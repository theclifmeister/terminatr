package cli

import (
	"context"
	"log"
	"net"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/plat/proc"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/relay"
)

const relayUsage = `usage: tm relay serve [--listen ADDR] (--domain NAME [--state DIR] [--email ADDR] | --cert FILE --key FILE | --plain)
    [--trusted-proxy CIDR]... [--max-conns N] [--max-per-ip N] [--bandwidth MiB/s] [--log-ips]
       sudo tm relay install --domain NAME [--email ADDR]
       sudo tm relay update
       sudo tm relay uninstall`

func init() {
	commands["relay"] = runRelay
	checked["relay"] = true
}

// relayServe is set by tests to run the relay on their own listener.
var relayServe = func(ctx context.Context, addr string, s *relay.Server, t relay.TLS) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return relay.Serve(ctx, ln, s, t)
}

// runRelay is tm relay (docs/SPEC.md §16): serve runs the relay two
// linked servers meet through, on the user's own host; install, update
// and uninstall manage it as a systemd service there.
func runRelay(e *Env, args []string) error {
	if len(args) == 0 {
		return usagef("%s", relayUsage)
	}
	switch args[0] {
	case "serve":
		return relayServeCmd(e, args[1:])
	case "install", "update", "uninstall":
		return relayManage(e, args[0], args[1:], newRelayService(e))
	}
	return usagef("%s", relayUsage)
}

func relayServeCmd(e *Env, args []string) error {
	f := newFlags()
	listen, cert, key := f.String("listen"), f.String("cert"), f.String("key")
	domain, state, email, acmeDir := f.String("domain"), f.String("state"), f.String("email"), f.String("acme-directory")
	plain, logIPs := f.Bool("plain"), f.Bool("log-ips")
	proxies := f.List("trusted-proxy")
	maxConns, maxPerIP, bandwidth := f.String("max-conns"), f.String("max-per-ip"), f.String("bandwidth")
	pos, err := f.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("%s", relayUsage)
	}
	if e.Caller.Kind != caller.Human {
		return &project.Error{Code: "human-only", Msg: "the user runs a relay, on their own host"}
	}
	if (*cert == "") != (*key == "") {
		return usagef("--cert and --key go together")
	}
	modes := 0
	for _, on := range []bool{*domain != "", *cert != "", *plain} {
		if on {
			modes++
		}
	}
	if modes != 1 {
		return usagef("give --domain, --cert and --key, or --plain behind a TLS proxy\n%s", relayUsage)
	}
	if *domain == "" && (*state != "" || *email != "" || *acmeDir != "") {
		return usagef("--state, --email and --acme-directory go with --domain")
	}
	t := relay.TLS{CertFile: *cert, KeyFile: *key, Email: *email, Directory: *acmeDir}
	if *domain != "" {
		if err := checkDomain(*domain); err != nil {
			return err
		}
		if err := checkEmail(*email); err != nil {
			return err
		}
		t.Domain, t.CacheDir = *domain, *state
		if t.CacheDir == "" {
			t.CacheDir = relayStateDir(e)
		}
	}
	cfg := relay.DefaultConfig()
	cfg.LogIPs = *logIPs
	for _, p := range *proxies {
		pr, err := netip.ParsePrefix(p)
		if err != nil {
			a, aerr := netip.ParseAddr(p)
			if aerr != nil {
				return usagef("--trusted-proxy %s: an address or CIDR", p)
			}
			pr = netip.PrefixFrom(a, a.BitLen())
		}
		cfg.TrustedProxies = append(cfg.TrustedProxies, pr.Masked())
	}
	for _, n := range []struct {
		name string
		val  string
		set  func(float64)
	}{
		{"max-conns", *maxConns, func(v float64) { cfg.MaxConns = int(v) }},
		{"max-per-ip", *maxPerIP, func(v float64) { cfg.MaxPerIP = int(v) }},
		{"bandwidth", *bandwidth, func(v float64) { cfg.BytesRate, cfg.BytesBurst = v*(1<<20), 2*v*(1<<20) }},
	} {
		if n.val == "" {
			continue
		}
		v, err := strconv.ParseFloat(n.val, 64)
		if err != nil || v <= 0 {
			return usagef("--%s %s: a positive number", n.name, n.val)
		}
		n.set(v)
	}
	addr := *listen
	if addr == "" {
		addr = ":443"
	}
	cfg.Log = log.New(e.Stderr, "tm relay: ", log.LstdFlags)
	ctx, stop := proc.UntilStopped(context.Background())
	defer stop()
	mode := "TLS"
	switch {
	case *plain:
		mode = "plain HTTP, for a TLS proxy in front"
	case t.Domain != "":
		mode = "TLS for " + t.Domain + " from Let's Encrypt, kept in " + t.CacheDir
	}
	cfg.Log.Printf("serving on %s (%s)", addr, mode)
	if err := relayServe(ctx, addr, relay.New(cfg), t); err != nil {
		return err
	}
	cfg.Log.Printf("stopped")
	return nil
}

// relayStateDir is where --domain keeps its certificates without
// --state: systemd's StateDirectory for the service, else ~/.terminatr/relay.
func relayStateDir(e *Env) string {
	if d := e.Getenv("STATE_DIRECTORY"); d != "" {
		// systemd lists one per StateDirectory, separated by colons.
		d, _, _ = strings.Cut(d, ":")
		return d
	}
	h, err := home.Dir()
	if err != nil {
		return "relay"
	}
	return filepath.Join(h, "relay")
}
