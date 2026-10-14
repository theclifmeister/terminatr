package cli

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/plat/proc"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/relay"
	"github.com/theclifmeister/terminatr/internal/version"
)

const relayUsage = `usage: tm relay serve [--listen ADDR] (--domain NAME [--state DIR] [--email ADDR] | --self-signed [--ip ADDR] [--state DIR]
    | --cert FILE --key FILE | --plain)
    [--trusted-proxy CIDR]... [--max-conns N] [--max-per-ip N] [--bandwidth MiB/s] [--log-ips] [--update-request FILE]
       sudo tm relay install (--domain NAME [--email ADDR] | --ip [ADDR]) [--auto-update=on|off]
       sudo tm relay update [--force]
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
	plain, logIPs, selfSigned, ipFlag := f.Bool("plain"), f.Bool("log-ips"), f.Bool("self-signed"), f.String("ip")
	proxies, updateFile := f.List("trusted-proxy"), f.String("update-request")
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
	for _, on := range []bool{*domain != "", *cert != "", *plain, *selfSigned} {
		if on {
			modes++
		}
	}
	if modes != 1 {
		return usagef("give --domain, --self-signed, --cert and --key, or --plain behind a TLS proxy\n%s", relayUsage)
	}
	if *domain == "" && (*email != "" || *acmeDir != "" || (*state != "" && !*selfSigned)) {
		return usagef("--state, --email and --acme-directory go with --domain (--state with --self-signed too)")
	}
	if *ipFlag != "" && !*selfSigned {
		return usagef("--ip goes with --self-signed")
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
	cfg.LogIPs, cfg.Version = *logIPs, version.Version
	// Linked servers' update requests go here, for tm-relay-update.path.
	cfg.UpdateFile = *updateFile
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
	var address string
	if *selfSigned {
		ip, err := relayIP(*ipFlag, addr)
		if err != nil {
			return err
		}
		dir := *state
		if dir == "" {
			dir = relayStateDir(e)
		}
		var pin string
		t.CertFile, t.KeyFile, pin, err = relay.SelfSigned(dir, []netip.Addr{ip}, time.Now())
		if err != nil {
			return err
		}
		_, port, _ := net.SplitHostPort(addr)
		address = relayAddress(ip, port, pin)
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
	case *selfSigned:
		mode = "TLS with a self-signed certificate, its key kept in " + filepath.Dir(t.KeyFile)
	}
	cfg.Log.Printf("serving on %s (%s)", addr, mode)
	if address != "" {
		cfg.Log.Printf("the relay's address, with its key's pin: %s", address)
		if w := relay.PrivateWarning(address); w != "" {
			cfg.Log.Print(w)
		}
	}
	if err := relayServe(ctx, addr, relay.New(cfg), t); err != nil {
		return err
	}
	cfg.Log.Printf("stopped")
	return nil
}

// relayIP is the address a self-signed relay names: --ip, else the one
// it listens on, else this host's own (localIP).
func relayIP(flag, listen string) (netip.Addr, error) {
	if flag != "" {
		ip, err := netip.ParseAddr(flag)
		if err != nil || ip.Zone() != "" {
			return netip.Addr{}, usagef("--ip %s: the IP address both servers reach the relay at, like 203.0.113.5", flag)
		}
		return ip.Unmap(), nil
	}
	if h, _, err := net.SplitHostPort(listen); err == nil {
		if ip, err := netip.ParseAddr(h); err == nil && !ip.IsUnspecified() {
			return ip.Unmap(), nil
		}
	}
	ip, err := localIP()
	if err != nil {
		return netip.Addr{}, fmt.Errorf("this host's address: %v; give it with --ip ADDR", err)
	}
	return ip, nil
}

// localIP is the address this host reaches the internet from (no packet
// goes out), else its first global one; tests set it.
var localIP = func() (netip.Addr, error) {
	if c, err := net.Dial("udp4", "192.0.2.1:9"); err == nil {
		defer c.Close()
		if ap, err := netip.ParseAddrPort(c.LocalAddr().String()); err == nil && !ap.Addr().IsLoopback() {
			return ap.Addr().Unmap(), nil
		}
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return netip.Addr{}, err
	}
	for _, a := range addrs {
		if pr, err := netip.ParsePrefix(a.String()); err == nil && pr.Addr().IsGlobalUnicast() {
			return pr.Addr().Unmap(), nil
		}
	}
	return netip.Addr{}, errors.New("no global address")
}

// relayAddress is a self-signed relay's address, as servers pair
// through it: wss://ip[:port]#sha256=pin.
func relayAddress(ip netip.Addr, port, pin string) string {
	host := ip.String()
	if ip.Is6() {
		host = "[" + host + "]"
	}
	if port != "" && port != "443" {
		host = net.JoinHostPort(ip.String(), port)
	}
	return "wss://" + host + "#" + relay.PinPrefix + pin
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
