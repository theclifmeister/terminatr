package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/device"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/plat/proc"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/qr"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/version"
)

const deviceUsage = `usage: tm device pair [--relay URL] [--as NAME]
       tm device list [--json]
       tm device remove NAME
       tm device access NAME view|answer|write
       tm device name NAME NEW`

func init() {
	commands["device"] = runDevice
	checked["device"] = true
}

// Variables for tests: how a running server is told and asked about the
// devices, and whether the QR code is drawn in colour.
var (
	reloadDevices = serverReloadDevices
	deviceLinks   = serverDeviceLinks
	qrColour      = func() bool { return isTTY(os.Stdout) }
	// shownInvite hears the invite the QR code shows (a test's phone).
	shownInvite = func(device.Invite) {}
)

// runDevice is tm device (docs/SPEC.md §16.15): pairing a phone with this
// server through a relay, and the paired devices. All of it is the
// human's.
func runDevice(e *Env, args []string) error {
	if len(args) == 0 {
		return usagef("%s", deviceUsage)
	}
	f := newFlags()
	relayFlag, as := f.String("relay"), f.String("as")
	asJSON := f.Bool("json")
	pos, err := f.Parse(args[1:])
	if err != nil {
		return err
	}
	allowed := map[string][]string{"pair": {"relay", "as"}, "list": {"json"}}[args[0]]
	for _, name := range []string{"relay", "as", "json"} {
		if f.IsSet(name) && !slices.Contains(allowed, name) {
			return usagef("tm device %s takes no --%s\n%s", args[0], name, deviceUsage)
		}
	}
	npos := map[string]int{"pair": 0, "list": 0, "remove": 1, "access": 2, "name": 2}
	if n, ok := npos[args[0]]; !ok || len(pos) != n {
		return usagef("%s", deviceUsage)
	}
	if e.Caller.Kind != caller.Human {
		return &project.Error{Code: "human-only", Msg: "pairing devices is the user's, in their own terminal"}
	}
	if *as != "" {
		if err := device.CheckName(*as); err != nil {
			return usagef("--as: %v", err)
		}
	}
	peerDir, err := peer.Dir()
	if err != nil {
		return err
	}
	dir, err := device.Dir()
	if err != nil {
		return err
	}
	switch args[0] {
	case "pair":
		return e.devicePair(peerDir, dir, *relayFlag, *as)
	case "list":
		return e.deviceList(dir, *asJSON)
	case "remove":
		return e.deviceRemove(dir, pos[0])
	case "access":
		return e.deviceAccess(dir, pos[0], pos[1])
	default:
		return e.deviceName(dir, pos[0], pos[1])
	}
}

// deviceRelay is the relay a device pairs and links through: --relay,
// else the one every paired device or server uses.
func deviceRelay(peerDir, dir, flag string) (string, error) {
	if flag != "" {
		return pairRelay(peerDir, flag)
	}
	r, ok, err := device.KnownRelay(dir, peerDir)
	if err != nil || ok {
		return r, err
	}
	return "", usagef("--relay URL: the relay the phone and this server meet on (docs/OPERATIONS.md, Running a relay)\n%s", deviceUsage)
}

func (e *Env) devicePair(peerDir, dir, relayFlag, as string) error {
	relayURL, err := deviceRelay(peerDir, dir, relayFlag)
	if err != nil {
		return err
	}
	id, err := e.pairIdentity(peerDir)
	if err != nil {
		return err
	}
	in, err := device.NewInvite(relayURL, id.ID)
	if err != nil {
		return err
	}
	code, err := qr.Encode([]byte(in.String()))
	if err != nil {
		return err
	}
	e.warnPrivate(relayURL)
	stdin := bufio.NewReader(e.Stdin)
	name := ""
	o := device.PairOptions{
		Name: pairName(), Version: version.Version, RelayTLS: pairRelayTLS(),
		Waiting: func() {
			fmt.Fprintf(e.Stdout, "Scan this with the terminatr app on the phone (it works once, for 10 minutes):\n\n%s\n", code.Terminal(qrColour()))
			fmt.Fprintf(e.Stdout, "This server: server id %s, through %s.\nWaiting…\n", id.ID.Short(), relayHost(relayURL))
			shownInvite(in)
		},
		Confirm: func(off device.Offer) (bool, error) {
			devices, err := device.Load(dir)
			if err != nil {
				return false, err
			}
			var again *device.Device
			if name, again, err = device.Propose(devices, off, as); err != nil {
				return false, err
			}
			level := device.LevelView
			if again != nil {
				level = again.Level
				fmt.Fprintf(e.Stdout, "\nThis device is paired already, as %q; pairing again keeps its level.\n", again.Name)
			}
			fmt.Fprintf(e.Stdout, "\nThe phone: %q, device id %s, app %s\n", off.Name, off.ID.Short(), textsafe.Line(off.Version, 40))
			if off.WriteKey == nil {
				fmt.Fprintln(e.Stdout, "Its app has no write key: it can only view, whatever its level.")
			}
			fmt.Fprintf(e.Stdout, "Add it as %q, a %s device? [y/N] ", name, levelWords(level))
			line, err := stdin.ReadString('\n')
			if err != nil && line == "" {
				fmt.Fprintln(e.Stdout)
				return false, nil
			}
			a := strings.ToLower(strings.TrimSpace(line))
			return a == "y" || a == "yes", nil
		},
	}
	ctx, stop := proc.UntilStopped(context.Background())
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, pairWait)
	defer cancel()
	p, err := device.Pair(ctx, id, in, o)
	if err != nil {
		return deviceError(pairFailed(err))
	}
	d, err := device.Add(dir, p.Key, p.WriteKey, name, relayURL)
	var taken *device.TakenError
	if errors.As(err, &taken) {
		return &project.Error{Code: "name-taken", Msg: err.Error()}
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "Paired %s (device id %s, %s) through %s.\n", d.Name, d.ID().Short(), levelWords(d.Level), relayHost(relayURL))
	e.reloadDevices()
	return nil
}

// levelWords is a level as the user reads it.
func levelWords(level string) string {
	switch level {
	case device.LevelAnswer:
		return "answer"
	case device.LevelWrite:
		return "write"
	}
	return "view-only"
}

// reloadDevices has a running server read the devices again.
func (e *Env) reloadDevices() {
	switch err := reloadDevices(); {
	case errors.Is(err, client.ErrNotRunning):
		fmt.Fprintln(e.Stdout, "The server isn't running; it reads the devices when it starts.")
	case err != nil:
		fmt.Fprintf(e.Stderr, "tm device: the server didn't read the devices again (%v); tm server restart does\n", err)
	}
}

func serverReloadDevices() error {
	c, _, err := connect(false)
	if err != nil {
		return err
	}
	defer c.Close()
	var st []proto.DeviceLink
	return c.Call(proto.MethodDeviceReload, nil, &st)
}

// serverDeviceLinks is a running server's device links; ok false when no
// server answers.
func serverDeviceLinks() (links []proto.DeviceLink, ok bool) {
	c, _, err := connect(false)
	if err != nil {
		return nil, false
	}
	defer c.Close()
	var s proto.ServerStatus
	if c.Call(proto.MethodServerStatus, nil, &s) != nil {
		return nil, false
	}
	return s.Devices, true
}

// deviceEntry is one paired device in tm device list --json.
type deviceEntry struct {
	Name  string `json:"name"`
	ID    string `json:"id"`
	Short string `json:"short"`
	// Level is the device's level; Effective the one its link runs at
	// (view for write in this release, or without a write key);
	// WriteKey says it has one.
	Level     string            `json:"level"`
	Effective string            `json:"effective"`
	Relay     string            `json:"relay"`
	WriteKey  bool              `json:"write_key"`
	Link      *proto.DeviceLink `json:"link,omitempty"`
}

func (e *Env) deviceList(dir string, asJSON bool) error {
	devices, err := device.Load(dir)
	if err != nil {
		return err
	}
	ls, running := deviceLinks()
	links := map[string]proto.DeviceLink{}
	for _, l := range ls {
		links[l.ID] = l
	}
	entries := []deviceEntry{}
	for _, d := range devices {
		de := deviceEntry{Name: d.Name, ID: d.ID().String(), Short: d.ID().Short(), Level: d.Level, Effective: d.Effective(), Relay: d.Relay, WriteKey: d.WriteKey != nil}
		if l, ok := links[de.ID]; ok {
			de.Link = &l
		}
		entries = append(entries, de)
	}
	if asJSON {
		return e.printJSON(map[string]any{"devices": entries})
	}
	if len(entries) == 0 {
		fmt.Fprintln(e.Stdout, "no devices: tm device pair")
		return nil
	}
	now := time.Now()
	tw := tabwriter.NewWriter(e.Stdout, 0, 0, 2, ' ', 0)
	for _, de := range entries {
		state := "link not running"
		switch {
		case de.Link != nil:
			state = deviceLinkWords(*de.Link, now)
		case running:
			state = "no link yet"
		}
		level := de.Level
		switch {
		case de.Effective == de.Level:
		case !de.WriteKey:
			level += " (runs at " + de.Effective + ": no write key)"
		default:
			level += " (runs at " + de.Effective + " in this release)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", de.Name, de.Short, level, relayHost(de.Relay), state)
	}
	return tw.Flush()
}

// deviceLinkWords is a device's link as tm device list says it: a device
// that isn't open is waited for, not failing.
func deviceLinkWords(l proto.DeviceLink, now time.Time) string {
	switch {
	case l.State == proto.LinkUp:
		return "connected " + sinceWords(l.Since, now)
	case l.State == proto.LinkOffline && l.Reason == proto.LinkReasonPeer:
		return "waiting for it"
	case l.State == proto.LinkOffline && l.Error != "":
		return "offline (" + l.Reason + "): " + l.Error
	case l.State == proto.LinkOffline:
		return "offline (" + l.Reason + ")"
	}
	return l.State
}

// sinceWords is how long ago t was, roughly.
func sinceWords(t, now time.Time) string {
	d := now.Sub(t).Round(time.Second)
	if d < time.Minute {
		return "just now"
	}
	return "for " + strings.TrimSuffix(d.Truncate(time.Minute).String(), "0s")
}

// deviceError is a registry error as tm device reports it.
func deviceError(err error) error {
	var unknown *device.UnknownError
	var taken *device.TakenError
	var nokey *device.NoWriteKeyError
	switch {
	case errors.As(err, &nokey):
		return &project.Error{Code: "no-write-key", Msg: err.Error()}
	case errors.As(err, &unknown):
		return &project.Error{Code: "unknown-device", Msg: err.Error()}
	case errors.As(err, &taken):
		return &project.Error{Code: "name-taken", Msg: err.Error()}
	}
	return err
}

func (e *Env) deviceRemove(dir, name string) error {
	d, err := device.Remove(dir, name)
	if err != nil {
		return deviceError(err)
	}
	fmt.Fprintf(e.Stdout, "Removed %s; its link closes now and its key is no longer taken. On the phone, Forget this server deletes its keys.\n", d.Name)
	e.reloadDevices()
	return nil
}

// deviceAccess sets what the device name may do through the app API.
// Every level runs in this release (write since the phone's composer,
// T821); a level a release doesn't enable is refused (level-not-yet)
// rather than stored. Above view the device needs a write key.
func (e *Env) deviceAccess(dir, name, level string) error {
	if !device.ValidLevel(level) {
		return usagef("tm device access NAME view|answer|write: %q isn't one", level)
	}
	if !device.Enabled(level) {
		return &project.Error{Code: "level-not-yet", Msg: fmt.Sprintf("level %s isn't in this release", level)}
	}
	d, err := device.SetLevel(dir, name, level)
	if err != nil {
		return deviceError(err)
	}
	switch d.Level {
	case device.LevelAnswer:
		fmt.Fprintf(e.Stdout, "%s is %s: it reads, answers questions and approvals (never one that runs a command outside the sandbox), and accepts or sends back tasks, each act behind Face ID.\n", d.Name, levelWords(d.Level))
	case device.LevelWrite:
		fmt.Fprintf(e.Stdout, "%s is %s: it does what the Mac app does (prompts, new tasks, delegating, starting a coordinator, pane keys and the rest), each act signed by its write key; never the guard's answers or the devices, and never an approval that runs a command outside the sandbox.\n", d.Name, levelWords(d.Level))
	default:
		fmt.Fprintf(e.Stdout, "%s is %s: it reads projects, sessions, conversations and panes, and acts on nothing.\n", d.Name, levelWords(d.Level))
	}
	e.reloadDevices()
	return nil
}

func (e *Env) deviceName(dir, name, to string) error {
	if err := device.CheckName(to); err != nil {
		return usagef("%v", err)
	}
	d, err := device.Rename(dir, name, to)
	if err != nil {
		return deviceError(err)
	}
	fmt.Fprintf(e.Stdout, "The device is now called %s; its journal lines say human (iphone: %s). Its link connects again.\n", d.Name, d.Name)
	e.reloadDevices()
	return nil
}
