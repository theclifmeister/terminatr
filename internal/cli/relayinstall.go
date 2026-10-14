package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/plat/systemd"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/update"
	"github.com/theclifmeister/terminatr/internal/version"
)

// Where `tm relay install` puts things, below relayService.Root.
const (
	relayBin      = "/usr/local/bin/tm"
	relayUnitPath = "/etc/systemd/system/tm-relay.service"
	relayStateLib = "/var/lib/private/tm-relay" // StateDirectory with DynamicUser
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

func relayHealth(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
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

// relayUnit is the systemd unit tm relay install writes, and
// deploy/relay/tm-relay.service for relay.example.net.
func relayUnit(domain, email string) string {
	exec := "ExecStart=" + relayBin + " relay serve --listen :443 --domain " + domain + " --state ${STATE_DIRECTORY}"
	if email != "" {
		exec += " --email " + email
	}
	return `# tm relay as a systemd service (docs/OPERATIONS.md, Running a relay).
#
# sudo tm relay install --domain <name> writes this file and does the
# rest. By hand: set your DNS name below, then
#   sudo install -m 755 tm /usr/local/bin/tm
#   sudo cp tm-relay.service /etc/systemd/system/
#   sudo systemctl daemon-reload && sudo systemctl enable --now tm-relay
#
# The relay runs as a throwaway user with only the right to bind 443. It
# gets its own certificate from Let's Encrypt (TLS-ALPN-01, on 443: no
# port 80, no certbot), keeps it in its state directory
# (/var/lib/private/tm-relay) and renews it before it expires.

[Unit]
Description=terminatr relay (tm relay serve)
Documentation=https://github.com/theclifmeister/terminatr/blob/main/docs/OPERATIONS.md
Wants=network-online.target
After=network-online.target

[Service]
` + exec + `
StateDirectory=tm-relay
StateDirectoryMode=0700
Restart=on-failure
RestartSec=2s

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
func unitDomain(unit string) string {
	for _, l := range strings.Split(unit, "\n") {
		if rest, ok := strings.CutPrefix(l, "ExecStart="); ok {
			f := strings.Fields(rest)
			for i := 0; i+1 < len(f); i++ {
				if f[i] == "--domain" {
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
	domain, email := f.String("domain"), f.String("email")
	pos, err := f.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) > 0 || (cmd != "install" && (*domain != "" || *email != "")) {
		return usagef("%s", relayUsage)
	}
	if cmd == "install" {
		if *domain == "" {
			return usagef("--domain NAME: the relay's DNS name, pointing at this host\n%s", relayUsage)
		}
		*domain = strings.ToLower(strings.TrimSuffix(*domain, "."))
		if err := checkDomain(*domain); err != nil {
			return err
		}
		if err := checkEmail(*email); err != nil {
			return err
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
		return h.install(ctx, e, *domain, *email)
	case "update":
		return h.update(ctx, e)
	}
	return h.uninstall(e)
}

func (h *relayService) install(ctx context.Context, e *Env, domain, email string) error {
	if err := h.installBinary(); err != nil {
		return err
	}
	unit := h.path(relayUnitPath)
	if err := os.MkdirAll(filepath.Dir(unit), 0o755); err != nil {
		return err
	}
	if err := writeFileAtomic(unit, []byte(relayUnit(domain, email)), 0o644); err != nil {
		return err
	}
	for _, a := range [][]string{{"daemon-reload"}, {"enable", "tm-relay"}, {"restart", "tm-relay"}} {
		if err := h.Systemctl(a...); err != nil {
			return err
		}
	}
	fmt.Fprintf(e.Stdout, "Installed %s and %s; tm-relay is starting.\nIt gets its certificate from Let's Encrypt, under their subscriber agreement (https://letsencrypt.org/repository/).\nWaiting for https://%s/healthz (the first certificate takes a few seconds)…\n", relayBin, relayUnitPath, domain)
	if err := h.waitHealthy(ctx, domain); err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, `The relay is up at wss://%[1]s. Pair two servers through it, on one of them:

    tm peer pair --relay wss://%[1]s

Logs: journalctl -u tm-relay · new release: sudo tm relay update · remove: sudo tm relay uninstall
`, domain)
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

// waitHealthy asks https://domain/healthz until it answers or h.Wait
// passes, then says what to check: tm never changes the firewall.
func (h *relayService) waitHealthy(ctx context.Context, domain string) error {
	url := "https://" + domain + "/healthz"
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

func (h *relayService) update(ctx context.Context, e *Env) error {
	rel, err := h.Client.Latest(ctx)
	if err != nil {
		return fmt.Errorf("the latest release: %w", err)
	}
	if newer, ok := update.Newer(rel.Tag, h.Version); ok && !newer {
		fmt.Fprintf(e.Stdout, "tm %s is the latest release; nothing to update.\n", h.Version)
		return nil
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
		fmt.Fprintln(e.Stdout, "tm-relay isn't installed here (sudo tm relay install --domain NAME); nothing to restart.")
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
		if err := h.waitHealthy(ctx, d); err != nil {
			return err
		}
		fmt.Fprintf(e.Stdout, "https://%s/healthz: ok\n", d)
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
	if err := h.Systemctl("daemon-reload"); err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, `Stopped tm-relay and removed %s.
Kept %s (other tm commands may use it) and the relay's certificates in %s (sudo rm -r %[3]s to drop them).
`, relayUnitPath, relayBin, relayStateLib)
	return nil
}
