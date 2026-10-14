package cli

import (
	"context"
	"log"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/relay"
)

const relayUsage = `usage: tm relay serve [--listen ADDR] (--cert FILE --key FILE | --plain)
    [--trusted-proxy CIDR]... [--max-conns N] [--max-per-ip N] [--bandwidth MiB/s] [--log-ips]`

func init() {
	commands["relay"] = runRelay
	checked["relay"] = true
}

// relayServe is set by tests to run the relay on their own listener.
var relayServe = func(ctx context.Context, addr string, s *relay.Server, cert, key string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return relay.Serve(ctx, ln, s, cert, key)
}

// runRelay is tm relay serve (docs/SPEC.md §16): the relay two linked
// servers meet through, run by the user on their own host.
func runRelay(e *Env, args []string) error {
	if len(args) == 0 || args[0] != "serve" {
		return usagef("%s", relayUsage)
	}
	f := newFlags()
	listen, cert, key := f.String("listen"), f.String("cert"), f.String("key")
	plain, logIPs := f.Bool("plain"), f.Bool("log-ips")
	proxies := f.List("trusted-proxy")
	maxConns, maxPerIP, bandwidth := f.String("max-conns"), f.String("max-per-ip"), f.String("bandwidth")
	pos, err := f.Parse(args[1:])
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
	if *plain == (*cert != "") {
		return usagef("give --cert and --key, or --plain behind a TLS proxy\n%s", relayUsage)
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	mode := "TLS"
	if *plain {
		mode = "plain HTTP, for a TLS proxy in front"
	}
	cfg.Log.Printf("serving on %s (%s)", addr, mode)
	if err := relayServe(ctx, addr, relay.New(cfg), *cert, *key); err != nil {
		return err
	}
	cfg.Log.Printf("stopped")
	return nil
}
