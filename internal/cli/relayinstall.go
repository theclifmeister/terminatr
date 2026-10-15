package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/plat/systemd"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/relay"
	"github.com/theclifmeister/terminatr/internal/update"
	"github.com/theclifmeister/terminatr/internal/version"
)

// Where `tm relay install` puts things, below relayService.Root.
const (
	relayBin      = "/usr/local/bin/tm"
	relayUnitPath = "/etc/systemd/system/tm-relay.service"
	relayStateLib = "/var/lib/private/tm-relay" // StateDirectory with DynamicUser

	// The update units: a root oneshot running tm relay update, started
	// by a daily timer (--auto-update) and by a path unit when the relay
	// writes an update request into its state directory.
	relayUpdateService = "/etc/systemd/system/tm-relay-update.service"
	relayUpdateTimer   = "/etc/systemd/system/tm-relay-update.timer"
	relayUpdatePath    = "/etc/systemd/system/tm-relay-update.path"
	relayRequestName   = "update-request"
	relayRequestFile   = relayStateLib + "/" + relayRequestName
)

// relayService is everything tm relay install, update and uninstall touch
// outside their arguments, so tests can run them in a temp root.
type relayService struct {
	// Root prefixes every path written ("" is /).
	Root string
	// Check says why this host can't run a systemd service.
	Check  func() error
	IsRoot func() bool
	// Exe is the running tm, installed as relayBin.
	Exe       string
	Version   string
	GOARCH    string
	Systemctl func(args ...string) error
	// Health gets url once and says why it isn't the relay's "ok".
	Health func(ctx context.Context, url string) error
	// Wait is how long to keep asking /healthz (the first certificate
	// takes a few seconds), and Every how often.
	Wait, Every time.Duration
	Lookup      func(host string) ([]string, error)
	Client      *update.Client
}

// newRelayService is set by tests to a temp root and fakes.
var newRelayService = func(e *Env) *relayService {
	exe, _ := os.Executable()
	return &relayService{
		Check: systemd.Check, IsRoot: systemd.IsRoot,
		Exe: exe, Version: version.Version, GOARCH: runtime.GOARCH,
		Systemctl: systemd.Systemctl,
		Health:    relayHealth,
		Wait:      2 * time.Minute, Every: 3 * time.Second,
		Lookup: net.LookupHost,
		Client: update.NewClient(e.Getenv),
	}
}

func (h *relayService) path(p string) string { return filepath.Join(h.Root, filepath.FromSlash(p)) }

// relayHealth gets url; one ending in #sha256=<pin> takes the relay by
// its key's pin (relay.PinnedTLS).
func relayHealth(ctx context.Context, rawURL string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	url, pin, err := relay.SplitPin(rawURL)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := http.DefaultClient
	if pin != "" {
		client = &http.Client{Transport: &http.Transport{TLSClientConfig: relay.PinnedTLS(nil, req.URL.Host, pin)}}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "ok" {
		return fmt.Errorf("%s: %q", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// domainRE is a DNS name of at least two labels.
var domainRE = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{0,61}[a-z0-9]$`)

func checkDomain(d string) error {
	if len(d) > 253 || !domainRE.MatchString(d) {
		return usagef("--domain %s: the relay's DNS name, like relay.example.net (no scheme, port or path)", d)
	}
	return nil
}

func checkEmail(m string) error {
	if m != "" && (!strings.Contains(m, "@") || strings.ContainsAny(m, " \t\r\n\"'\\$%;")) {
		return usagef("--email %s: an e-mail address", m)
	}
	return nil
}

// relayUnit is the systemd unit tm relay install --domain writes, and
// deploy/relay/tm-relay.service for relay.example.net.
func relayUnit(domain, email string, auto bool) string {
	exec := "ExecStart=" + relayBin + " relay serve --listen :443 --domain " + domain + " --state ${STATE_DIRECTORY}"
	if email != "" {
		exec += " --email " + email
	}
	exec += relayRequestFlag + relayAutoFlag(auto)
	return relayUnitFor(exec, `# sudo tm relay install --domain <name> writes this file and does the
# rest. By hand: set your DNS name below, then
#   sudo install -m 755 tm /usr/local/bin/tm
#   sudo cp tm-relay.service tm-relay-update.* /etc/systemd/system/
#   sudo systemctl daemon-reload && sudo systemctl enable --now tm-relay
#   sudo systemctl enable --now tm-relay-update.path tm-relay-update.timer
# (the timer updates the relay daily; the path unit when a linked
# server asks: drop either to do without).
#
# The relay runs as a throwaway user with only the right to bind 443. It
# gets its own certificate from Let's Encrypt (TLS-ALPN-01, on 443: no
# port 80, no certbot), keeps it in its state directory
# (/var/lib/private/tm-relay) and renews it before it expires.
`)
}

// relayIPUnit is the systemd unit tm relay install --ip writes.
func relayIPUnit(ip string, auto bool) string {
	return relayUnitFor("ExecStart="+relayBin+" relay serve --listen :443 --self-signed --ip "+ip+" --state ${STATE_DIRECTORY}"+relayRequestFlag+relayAutoFlag(auto),
		`# sudo tm relay install --ip <address> writes this file and does the rest.
#
# The relay runs as a throwaway user with only the right to bind 443. It
# serves a self-signed certificate for its address; the key is in its
# state directory (/var/lib/private/tm-relay) for good: the servers
# paired through it pin that key (wss://<address>#sha256=<pin>), so back
# the directory up.
`)
}

// relayRequestFlag has the relay write update requests where
// tm-relay-update.path looks.
const relayRequestFlag = " --update-request ${STATE_DIRECTORY}/" + relayRequestName

// relayAutoFlag tells the relay whether the update timer is on, for
// /healthz's Tm-Auto-Update header (tm doctor shows it).
func relayAutoFlag(auto bool) string {
	if auto {
		return " --auto-update on"
	}
	return " --auto-update off"
}

// relayUpdateServiceUnit is tm-relay-update.service: tm relay update as
// root, started by the timer and the path unit.
const relayUpdateServiceUnit = `# tm relay install writes this file (docs/OPERATIONS.md, Running a relay).
#
# tm relay update, as root: it removes the relay's update request, gets
# the latest official release, checks its sha256 against the release's
# checksums.txt and restarts tm-relay only when the release is newer.
# Started by tm-relay-update.timer (daily, unless installed with
# --auto-update=off) and by tm-relay-update.path (a linked server asked).

[Unit]
Description=terminatr relay update (tm relay update)
Documentation=https://github.com/theclifmeister/terminatr/blob/main/docs/OPERATIONS.md
Wants=network-online.target
After=network-online.target

[Service]
Type=oneshot
ExecStart=` + relayBin + ` relay update
TimeoutStartSec=15min
`

// relayUpdateTimerUnit is tm-relay-update.timer: the daily update.
const relayUpdateTimerUnit = `# tm relay install writes this file; --auto-update=off leaves it disabled.

[Unit]
Description=terminatr relay update, daily

[Timer]
OnCalendar=daily
RandomizedDelaySec=1h
Persistent=true

[Install]
WantedBy=timers.target
`

// relayUpdatePathUnit is tm-relay-update.path: the relay, unprivileged,
// writes an update request into its state directory when a linked
// server asks (at most once an hour); this starts tm relay update.
const relayUpdatePathUnit = `# tm relay install writes this file.
#
# A server linked through the relay may ask it to update itself to the
# latest official release (the Peers tab's u, tm peer relay-update). The
# relay runs unprivileged and only writes this request file; tm relay
# update, as root, removes it and does the update.

[Unit]
Description=terminatr relay update, when a linked server asks

[Path]
PathExists=` + relayRequestFile + `

[Install]
WantedBy=paths.target
`

func relayUnitFor(exec, about string) string {
	return `# tm relay as a systemd service (docs/OPERATIONS.md, Running a relay).
#
` + about + `
[Unit]
Description=terminatr relay (tm relay serve)
Documentation=https://github.com/theclifmeister/terminatr/blob/main/docs/OPERATIONS.md
Wants=network-online.target
After=network-online.target
# Restart however often it fails: never left stopped by a start limit.
StartLimitIntervalSec=0

[Service]
` + exec + `
StateDirectory=tm-relay
StateDirectoryMode=0700
Restart=on-failure
RestartSec=2s
# The Go GC works harder near this, well before MemoryMax kills it.
Environment=GOMEMLIMIT=200MiB

DynamicUser=yes
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
ProtectClock=yes
ProtectHostname=yes
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
RestrictNamespaces=yes
RestrictRealtime=yes
LockPersonality=yes
MemoryDenyWriteExecute=yes
SystemCallArchitectures=native
SystemCallFilter=@system-service
LimitNOFILE=8192
MemoryMax=256M

[Install]
WantedBy=multi-user.target
`
}

// unitDomain is the --domain of an installed unit's ExecStart, or "".
func unitDomain(unit string) string { return unitFlag(unit, "--domain") }

// unitFlag is the value of flag in an installed unit's ExecStart, or "".
func unitFlag(unit, flag string) string {
	for _, l := range strings.Split(unit, "\n") {
		if rest, ok := strings.CutPrefix(l, "ExecStart="); ok {
			f := strings.Fields(rest)
			for i := 0; i+1 < len(f); i++ {
				if f[i] == flag {
					return f[i+1]
				}
			}
		}
	}
	return ""
}

// relayManage is tm relay install, update and uninstall: the relay as a
// systemd service on this Linux host, run as root by the user.
func relayManage(e *Env, cmd string, args []string, h *relayService) error {
	f := newFlags()
	domain, email, ipMode := f.String("domain"), f.String("email"), f.Bool("ip")
	autoUpdate := f.String("auto-update")
	force := f.Bool("force")
	// --ip takes its address as the next argument, or none.
	var flagArgs []string
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, "--ip="); ok {
			flagArgs = append(flagArgs, "--ip", v)
			continue
		}
		flagArgs = append(flagArgs, a)
	}
	pos, err := f.Parse(flagArgs)
	if err != nil {
		return err
	}
	if len(pos) > 1 || (len(pos) == 1 && !*ipMode) || (cmd != "update" && *force) || (cmd != "install" && (*domain != "" || *email != "" || *ipMode || f.IsSet("auto-update"))) {
		return usagef("%s", relayUsage)
	}
	auto := true
	switch *autoUpdate {
	case "", "on":
	case "off":
		auto = false
	default:
		return usagef("--auto-update=on or --auto-update=off, not %q", *autoUpdate)
	}
	var ip netip.Addr
	if cmd == "install" {
		switch {
		case *ipMode && (*domain != "" || *email != ""):
			return usagef("--domain or --ip, not both\n%s", relayUsage)
		case *ipMode:
			if ip, err = relayIP(strings.Join(pos, ""), ""); err != nil {
				return err
			}
		case *domain == "":
			return usagef("--domain NAME: the relay's DNS name, pointing at this host (or --ip [ADDR] for a relay without a domain)\n%s", relayUsage)
		}
		if *domain != "" {
			*domain = strings.ToLower(strings.TrimSuffix(*domain, "."))
			if err := checkDomain(*domain); err != nil {
				return err
			}
			if err := checkEmail(*email); err != nil {
				return err
			}
		}
	}
	if e.Caller.Kind != caller.Human {
		return &project.Error{Code: "human-only", Msg: "the user runs a relay, on their own host"}
	}
	if err := h.Check(); err != nil {
		return fmt.Errorf("%s: %v", cmd, err)
	}
	if !h.IsRoot() {
		return fmt.Errorf("%s writes system files: run it as root (sudo tm relay %s)", cmd, strings.Join(append([]string{cmd}, args...), " "))
	}
	ctx := context.Background()
	switch cmd {
	case "install":
		if ip.IsValid() {
			return h.installIP(ctx, e, ip, len(pos) == 0, auto)
		}
		return h.install(ctx, e, *domain, *email, auto)
	case "update":
		return h.update(ctx, e, *force)
	}
	return h.uninstall(e)
}

func (h *relayService) install(ctx context.Context, e *Env, domain, email string, auto bool) error {
	if err := h.installBinary(); err != nil {
		return err
	}
	if err := h.installUnits(relayUnit(domain, email, auto), auto); err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "Installed %s and %s; tm-relay is starting.\nIt gets its certificate from Let's Encrypt, under their subscriber agreement (https://letsencrypt.org/repository/).\nWaiting for https://%s/healthz (the first certificate takes a few seconds)…\n", relayBin, relayUnitPath, domain)
	if err := h.waitHealthy(ctx, domain, ""); err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, `The relay is up at wss://%[1]s. Pair two servers through it, on one of them:

    tm peer pair --relay wss://%[1]s

Logs: journalctl -u tm-relay · new release: sudo tm relay update · remove: sudo tm relay uninstall
`, domain)
	autoUpdateNote(e, auto)
	return nil
}

// installUnits writes the relay's unit and its update units, then
// starts the relay and the path unit, and enables the daily timer when
// auto (else disables it: install again with --auto-update=on).
func (h *relayService) installUnits(relay string, auto bool) error {
	if err := os.MkdirAll(h.path(filepath.Dir(relayUnitPath)), 0o755); err != nil {
		return err
	}
	for _, u := range []struct{ path, text string }{
		{relayUnitPath, relay},
		{relayUpdateService, relayUpdateServiceUnit},
		{relayUpdateTimer, relayUpdateTimerUnit},
		{relayUpdatePath, relayUpdatePathUnit},
	} {
		if err := writeFileAtomic(h.path(u.path), []byte(u.text), 0o644); err != nil {
			return err
		}
	}
	timer := []string{"disable", "--now", "tm-relay-update.timer"}
	if auto {
		timer = []string{"enable", "--now", "tm-relay-update.timer"}
	}
	for _, a := range [][]string{{"daemon-reload"}, {"enable", "tm-relay"}, {"restart", "tm-relay"},
		{"enable", "--now", "tm-relay-update.path"}, timer} {
		if err := h.Systemctl(a...); err != nil {
			return err
		}
	}
	return nil
}

// autoUpdateNote says how the relay keeps up with releases.
func autoUpdateNote(e *Env, auto bool) {
	if auto {
		fmt.Fprintln(e.Stdout, "Auto-update: on. tm-relay-update.timer runs tm relay update daily (checksums checked; a restart only for a newer release); install again with --auto-update=off to stop it.")
	} else {
		fmt.Fprintln(e.Stdout, "Auto-update: off. Update with sudo tm relay update, or install again with --auto-update=on.")
	}
	fmt.Fprintln(e.Stdout, "Any server linked through the relay may also ask it to update to the latest release (the Peers tab's u, tm peer relay-update), at most once an hour.")
}

// installIP installs a relay without a domain: its key and self-signed
// certificate are made here, in its state directory, before it starts,
// so the address printed holds the pin of the key it serves. detected:
// ip is this host's own, not given.
func (h *relayService) installIP(ctx context.Context, e *Env, ip netip.Addr, detected, auto bool) error {
	if err := h.installBinary(); err != nil {
		return err
	}
	// systemd hands the state directory, and what is in it, to the
	// service's user when it starts.
	_, _, pin, err := relay.SelfSigned(h.path(relayStateLib), []netip.Addr{ip}, time.Now())
	if err != nil {
		return err
	}
	if err := h.installUnits(relayIPUnit(ip.String(), auto), auto); err != nil {
		return err
	}
	address := relayAddress(ip, "", pin)
	fmt.Fprintf(e.Stdout, "Installed %s and %s; tm-relay is starting, with a self-signed certificate for %s.\n", relayBin, relayUnitPath, ip)
	if err := h.waitHealthy(ctx, ip.String(), pin); err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, `The relay is up. Its address, with its key's pin:

    %[1]s

Pair two servers through it, on one of them:

    tm peer pair --relay %[1]s

Both servers must reach %[2]s on TCP port 443.`, address, ip)
	if detected {
		fmt.Fprintf(e.Stdout, " %s is this host's own address; if the servers reach it at another (a public address in front of it, a tailnet's), use that one with the same #sha256=…, or install again with --ip ADDR.", ip)
	}
	if w := relay.PrivateWarning(address); w != "" {
		fmt.Fprintf(e.Stdout, "\nNote: %s.", w)
	}
	fmt.Fprintf(e.Stdout, `
Back up %s: the paired servers pin the key in it, and a new key means giving both the new address (tm peer relay ALIAS ADDRESS).
Logs: journalctl -u tm-relay · new release: sudo tm relay update · remove: sudo tm relay uninstall
`, relayStateLib)
	autoUpdateNote(e, auto)
	return nil
}

// installBinary copies the running tm to relayBin, unless it is that file.
func (h *relayService) installBinary() error {
	dst := h.path(relayBin)
	src, err := filepath.EvalSymlinks(h.Exe)
	if err != nil {
		return fmt.Errorf("this tm's binary: %w", err)
	}
	if cur, err := filepath.EvalSymlinks(dst); err == nil && cur == src {
		return nil
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(dst, b, 0o755)
}

// writeFileAtomic writes a new file next to path and renames it over.
func writeFileAtomic(path string, b []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".tm-relay-*")
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Chmod(mode)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}

// waitHealthy asks https://host/healthz until it answers or h.Wait
// passes, then says what to check: tm never changes the firewall. With
// a pin, host is an IP address and the relay is taken by its key.
func (h *relayService) waitHealthy(ctx context.Context, host, pin string) error {
	url := "https://" + host + "/healthz"
	if pin != "" {
		if ip, err := netip.ParseAddr(host); err == nil && ip.Is6() {
			url = "https://[" + host + "]/healthz"
		}
		url += "#" + relay.PinPrefix + pin
	}
	deadline := time.Now().Add(h.Wait)
	var err error
	for {
		if err = h.Health(ctx, url); err == nil {
			return nil
		}
		if !time.Now().Add(h.Every).Before(deadline) {
			break
		}
		time.Sleep(h.Every)
	}
	if h.Systemctl("is-active", "--quiet", "tm-relay") != nil {
		return fmt.Errorf("tm-relay didn't stay up; its log says why: journalctl -u tm-relay")
	}
	if pin != "" {
		var pe *relay.PinError
		if errors.As(err, &pe) {
			return fmt.Errorf("something at %s answers with another key than tm-relay's: %v", host, err)
		}
		return fmt.Errorf(`tm-relay is running, but %s doesn't answer: %v
Both servers reach the relay on TCP port 443, so:
  - open port 443 (TCP, inbound) to this host: your cloud provider's firewall, and ufw or firewalld if this host runs one (sudo ufw allow 443/tcp); tm doesn't change firewalls
  - check %s is this host's address as the servers see it (else install again with --ip ADDR)
journalctl -u tm-relay shows how it goes`, strings.TrimSuffix(url, "#"+relay.PinPrefix+pin), err, host)
	}
	domain := host
	where := "doesn't resolve"
	if addrs, lerr := h.Lookup(domain); lerr == nil && len(addrs) > 0 {
		where = "resolves to " + strings.Join(addrs, ", ")
	}
	return fmt.Errorf(`tm-relay is running, but %s doesn't answer: %v
Let's Encrypt and both servers reach the relay on TCP port 443, so:
  - open port 443 (TCP, inbound) to this host: your cloud provider's firewall, and ufw or firewalld if this host runs one (sudo ufw allow 443/tcp); tm doesn't change firewalls
  - check %s's DNS points at this host (it %s)
Then: curl %s (the relay keeps trying for a certificate by itself; journalctl -u tm-relay shows how it goes)`, url, err, domain, where, url)
}

func (h *relayService) update(ctx context.Context, e *Env, force bool) error {
	// A linked server's request (tm-relay-update.path started this): it
	// is done once this runs, whatever comes of it, so a failing update
	// isn't started again and again.
	switch err := os.Remove(h.path(relayRequestFile)); {
	case err == nil:
		fmt.Fprintln(e.Stdout, "A linked server asked the relay to update to the latest release.")
	case !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("the relay's update request: %w", err)
	}
	rel, err := h.Client.Latest(ctx)
	if err != nil {
		return fmt.Errorf("the latest release: %w", err)
	}
	newer, ok := update.Newer(rel.Tag, h.Version)
	switch {
	case ok && !newer:
		// Equal, or this build is ahead of the release.
		if ahead, _ := update.Newer(h.Version, rel.Tag); !ahead {
			fmt.Fprintf(e.Stdout, "tm %s is the latest release; nothing to update.\n", h.Version)
			return nil
		}
		if !force {
			return fmt.Errorf("this tm (%s) is newer than the latest release (%s): installing it would be a downgrade, and the relay's unit may use flags the release lacks; sudo tm relay update --force to do it anyway", h.Version, rel.Tag)
		}
	case !ok && !force:
		return fmt.Errorf("this tm (%s) isn't a release version, so it can't be compared with the latest release (%s); sudo tm relay update --force to install the release anyway", h.Version, rel.Tag)
	}
	bin := h.path(relayBin)
	dir := filepath.Dir(bin)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := h.Client.Download(ctx, rel, "linux", h.GOARCH, dir)
	if err != nil {
		return err
	}
	defer b.Remove()
	if err := b.Install(dir); err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "Installed tm %s as %s (sha256 checked against the release's checksums.txt).\n", rel.Tag, relayBin)
	unit, err := os.ReadFile(h.path(relayUnitPath))
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(e.Stdout, "tm-relay isn't installed here (sudo tm relay install --domain NAME, or --ip); nothing to restart.")
		return nil
	}
	if err != nil {
		return err
	}
	if err := h.Systemctl("restart", "tm-relay"); err != nil {
		return err
	}
	fmt.Fprintln(e.Stdout, "Restarted tm-relay; linked servers reconnect by themselves.")
	if d := unitDomain(string(unit)); d != "" {
		if err := h.waitHealthy(ctx, d, ""); err != nil {
			return err
		}
		fmt.Fprintf(e.Stdout, "https://%s/healthz: ok\n", d)
	}
	if a := unitFlag(string(unit), "--ip"); a != "" && strings.Contains(string(unit), " --self-signed ") {
		ip, err := netip.ParseAddr(a)
		if err != nil {
			return fmt.Errorf("%s: --ip %s: %v", relayUnitPath, a, err)
		}
		pin, err := relay.SelfSignedPin(h.path(relayStateLib))
		if err != nil {
			return fmt.Errorf("the relay's key: %w", err)
		}
		if err := h.waitHealthy(ctx, a, pin); err != nil {
			return err
		}
		fmt.Fprintf(e.Stdout, "The relay is up with the same key: %s\n", relayAddress(ip, "", pin))
	}
	return nil
}

func (h *relayService) uninstall(e *Env) error {
	unit := h.path(relayUnitPath)
	if _, err := os.Stat(unit); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(e.Stdout, "tm-relay isn't installed here (no %s).\n", relayUnitPath)
		return nil
	}
	if err := h.Systemctl("disable", "--now", "tm-relay"); err != nil {
		return err
	}
	if err := os.Remove(unit); err != nil {
		return err
	}
	// An install from before the update units has none.
	for _, u := range []string{relayUpdateTimer, relayUpdatePath, relayUpdateService} {
		if _, err := os.Stat(h.path(u)); err != nil {
			continue
		}
		if u != relayUpdateService {
			if err := h.Systemctl("disable", "--now", filepath.Base(u)); err != nil {
				return err
			}
		}
		if err := os.Remove(h.path(u)); err != nil {
			return err
		}
	}
	if err := h.Systemctl("daemon-reload"); err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, `Stopped tm-relay and removed %s and its update units.
Kept %s (other tm commands may use it) and the relay's keys and certificates in %s: a self-signed relay's key is what the paired servers pin, so keep it to install again (sudo rm -r %[3]s drops them).
`, relayUnitPath, relayBin, relayStateLib)
	return nil
}
