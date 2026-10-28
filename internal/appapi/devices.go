package appapi

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"os"
	"time"

	"github.com/theclifmeister/terminatr/internal/device"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
	"github.com/theclifmeister/terminatr/internal/version"
)

// Settings › Devices (app API 26): the second exception to settings
// and access staying out of the app API (docs/SPEC.md §3.8). The paired
// devices are the user's own pairing, so the bridge lists, pairs,
// renames, removes and sets the level of them itself, in its own
// process as tm device does, only when the caller is the human
// (bridge.human), and never on a device's link (Allowed: no level has
// a method the bridge answers locally). Each change has a running
// server read the devices again (device.reload).
const (
	// MethodDeviceList lists the paired devices and their links.
	MethodDeviceList = "device.list"
	// MethodDevicePair starts a pairing (DevicePairParams): it answers
	// once the code works, with what the QR code holds.
	MethodDevicePair = "device.pair"
	// MethodDevicePairWait waits for the phone to join the pairing
	// (DevicePairingParams): it answers with its offer, or how the
	// pairing ended.
	MethodDevicePairWait = "device.pair.wait"
	// MethodDevicePairAnswer is the user's yes, under a name, or no to
	// the offer (DevicePairAnswerParams); a no before it cancels.
	MethodDevicePairAnswer = "device.pair.answer"
	// MethodDeviceAccess sets a device's level (DeviceAccessParams).
	MethodDeviceAccess = "device.access"
	// MethodDeviceName renames a device (DeviceNameParams).
	MethodDeviceName = "device.name"
	// MethodDeviceRemove removes a device (DeviceParams).
	MethodDeviceRemove = "device.remove"
)

// The device methods' error codes, besides ErrHumanOnly and tm device's
// own (unknown-device, name-taken, no-write-key, level-not-yet).
const (
	// ErrRelayNeeded: no relay is known (none or several among the
	// paired devices and servers); device.pair needs one.
	ErrRelayNeeded = "relay-needed"
	// ErrPairEnded: the pairing isn't open (another replaced it, it
	// ended, or the bridge never started it).
	ErrPairEnded = "pair-ended"
	// ErrPairExpired: the code's time ran out before the phone joined,
	// or the answer's before the user gave it.
	ErrPairExpired = "pair-expired"
	// ErrPairFailed: the pairing broke (the relay, the phone).
	ErrPairFailed = "pair-failed"
	// ErrDevices: the devices can't be read or written.
	ErrDevices = "devices"
)

type (
	// DeviceParams name a device: its id (or name).
	DeviceParams struct {
		Device string `json:"device"`
	}
	// DeviceAccessParams set a device's level: view or answer.
	DeviceAccessParams struct {
		Device string `json:"device"`
		Level  string `json:"level"`
	}
	// DeviceNameParams give a device a new name.
	DeviceNameParams struct {
		Device string `json:"device"`
		Name   string `json:"name"`
	}
	// DevicePairParams start a pairing through relay ("": the one the
	// paired devices and servers use).
	DevicePairParams struct {
		Relay string `json:"relay,omitempty"`
	}
	// DevicePairingParams name the pairing device.pair answered.
	DevicePairingParams struct {
		Pairing string `json:"pairing"`
	}
	// DevicePairAnswerParams answer a pairing's offer: accept under
	// name ("": the one the offer proposed), or not.
	DevicePairAnswerParams struct {
		Pairing string `json:"pairing"`
		Accept  bool   `json:"accept"`
		Name    string `json:"name,omitempty"`
	}
)

// Devices is device.list's result: the paired devices, oldest first;
// Running says a server answered, so each one's Link is its link's
// state (none yet: none); Levels are the levels this release enables;
// Relay is the relay a pairing goes through, "" when device.pair needs
// one.
type Devices struct {
	Devices []Device `json:"devices"`
	Running bool     `json:"running,omitempty"`
	Levels  []string `json:"levels"`
	Relay   string   `json:"relay,omitempty"`
}

// Device is a paired device: its name, id (and short id), level and
// the one its link runs at (Effective: view without a write key, or for
// a level this release doesn't enable), its relay's host, whether it
// has a write key, and its link while a server runs it.
type Device struct {
	Name      string      `json:"name"`
	ID        string      `json:"id"`
	Short     string      `json:"short"`
	Level     string      `json:"level"`
	Effective string      `json:"effective"`
	Relay     string      `json:"relay"`
	WriteKey  bool        `json:"write_key,omitempty"`
	Link      *DeviceLink `json:"link,omitempty"`
}

// DeviceLink is a device's link: up, offline (Reason peer: the server
// waits for the phone to open) or connecting, since when, and the last
// failure.
type DeviceLink struct {
	State  string    `json:"state"`
	Reason string    `json:"reason,omitempty"`
	Since  time.Time `json:"since"`
	Error  string    `json:"error,omitempty"`
}

// DevicePairing is device.pair's result: the pairing's id, the invite
// its QR code holds (terminatr://pair?…, as tm device pair shows it),
// until when it works, this server's short id and name, the relay's
// host, and a note on it (a private address).
type DevicePairing struct {
	Pairing string    `json:"pairing"`
	Invite  string    `json:"invite"`
	Expires time.Time `json:"expires"`
	Server  string    `json:"server"`
	Name    string    `json:"name"`
	Relay   string    `json:"relay"`
	Note    string    `json:"note,omitempty"`
}

// DeviceOffer is device.pair.wait's result: the phone that joined, as
// tm device pair shows it before asking. Name is what it would be added
// as, Phone the name it gave; Again the device's name when its key is
// paired already (it keeps Level); WriteKey false: it can only view.
// AnswerBy bounds the user's answer.
type DeviceOffer struct {
	Name     string    `json:"name"`
	Phone    string    `json:"phone"`
	Short    string    `json:"short"`
	Version  string    `json:"version"`
	WriteKey bool      `json:"write_key,omitempty"`
	Again    string    `json:"again,omitempty"`
	Level    string    `json:"level"`
	AnswerBy time.Time `json:"answer_by"`
}

// DevicePaired is device.pair.answer's result: the device added, none
// for a no.
type DevicePaired struct {
	Device *Device `json:"device,omitempty"`
}

// pairing is the bridge's one open pairing.
type pairing struct {
	id     string
	relay  string
	cancel context.CancelFunc
	// waiting closes once the code works, offered once the phone's
	// offer is in (offer), done once the pairing ended (dev, err).
	waiting, offered, done chan struct{}
	offer                  device.Offer
	shown                  DeviceOffer
	// answer takes the user's answer to the offer: the name, "" for no.
	answer chan string
	dev    *Device
	err    *Error
}

// Variables for tests: the relay's outer TLS (nil trusts the system's
// CAs) and how long a pairing may take.
var (
	pairRelayTLS = func() *tls.Config { return nil }
	pairWait     = device.PairWindow + 3*time.Minute
)

func devicesHumanOnly() *Error {
	return &Error{Code: ErrHumanOnly, Message: "the paired devices are the user's: only they pair and change them"}
}

// deviceDirs are the devices' and the peers' folders.
func (b *bridge) deviceDirs() (devDir, peerDir string) {
	return device.DirIn(b.o.Paths.Home), peer.DirIn(b.o.Paths.Home)
}

// deviceList answers device.list.
func deviceList(b *bridge, _ struct{}) (Devices, error) {
	if !b.human() {
		return Devices{}, devicesHumanOnly()
	}
	dir, peerDir := b.deviceDirs()
	ds, err := device.Load(dir)
	if err != nil {
		return Devices{}, devicesError(err)
	}
	out := Devices{Devices: []Device{}}
	for _, l := range device.Levels {
		if device.Enabled(l) {
			out.Levels = append(out.Levels, l)
		}
	}
	if r, ok, err := device.KnownRelay(dir, peerDir); err == nil && ok {
		out.Relay = peer.RelayHost(r)
	}
	var st proto.ServerStatus
	links := map[string]proto.DeviceLink{}
	if b.serverCall(proto.MethodServerStatus, nil, &st) == nil {
		out.Running = true
		for _, l := range st.Devices {
			links[l.ID] = l
		}
	}
	for _, d := range ds {
		ad := deviceOf(d)
		if l, ok := links[ad.ID]; ok {
			ad.Link = &DeviceLink{State: l.State, Reason: l.Reason, Since: l.Since, Error: l.Error}
		}
		out.Devices = append(out.Devices, ad)
	}
	return out, nil
}

func deviceOf(d device.Device) Device {
	return Device{Name: d.Name, ID: d.ID().String(), Short: d.ID().Short(), Level: d.Level, Effective: d.Effective(),
		Relay: peer.RelayHost(d.Relay), WriteKey: d.WriteKey != nil}
}

// devicesError is a registry error as the device methods answer it,
// with tm device's codes.
func devicesError(err error) *Error {
	var unknown *device.UnknownError
	var taken *device.TakenError
	var nokey *device.NoWriteKeyError
	switch {
	case errors.As(err, &nokey):
		return &Error{Code: "no-write-key", Message: nokey.Name + " has no write key to sign its acts with: pair it again from an app that makes one"}
	case errors.As(err, &unknown):
		return &Error{Code: "unknown-device", Message: "no device " + unknown.Name + ": it was removed"}
	case errors.As(err, &taken):
		return &Error{Code: "name-taken", Message: "a device is already called " + taken.Name}
	}
	return &Error{Code: ErrDevices, Message: err.Error()}
}

// devicesChanged has a running server read the devices again; one that isn't
// running reads them when it starts.
func (b *bridge) devicesChanged() {
	var links []proto.DeviceLink
	b.serverCall(proto.MethodDeviceReload, nil, &links)
}

// deviceAccess answers device.access, with tm device access's checks.
func deviceAccess(b *bridge, p DeviceAccessParams) (Device, error) {
	if !b.human() {
		return Device{}, devicesHumanOnly()
	}
	if !device.ValidLevel(p.Level) {
		return Device{}, &Error{Code: ErrBadParams, Message: `device.access: level is "view", "answer" or "write"`}
	}
	if !device.Enabled(p.Level) {
		return Device{}, &Error{Code: "level-not-yet", Message: "level " + p.Level + " isn't in this release"}
	}
	dir, _ := b.deviceDirs()
	d, err := device.SetLevel(dir, p.Device, p.Level)
	if err != nil {
		return Device{}, devicesError(err)
	}
	b.devicesChanged()
	return deviceOf(d), nil
}

// deviceName answers device.name.
func deviceName(b *bridge, p DeviceNameParams) (Device, error) {
	if !b.human() {
		return Device{}, devicesHumanOnly()
	}
	if err := device.CheckName(p.Name); err != nil {
		return Device{}, &Error{Code: ErrBadParams, Message: err.Error()}
	}
	dir, _ := b.deviceDirs()
	d, err := device.Rename(dir, p.Device, p.Name)
	if err != nil {
		return Device{}, devicesError(err)
	}
	b.devicesChanged()
	return deviceOf(d), nil
}

// deviceRemove answers device.remove: its link closes and its key is
// no longer taken.
func deviceRemove(b *bridge, p DeviceParams) (Empty, error) {
	if !b.human() {
		return Empty{}, devicesHumanOnly()
	}
	dir, _ := b.deviceDirs()
	if _, err := device.Remove(dir, p.Device); err != nil {
		return Empty{}, devicesError(err)
	}
	b.devicesChanged()
	return Empty{}, nil
}

// devicePair answers device.pair: it ends the bridge's open pairing,
// if any, starts a new one and answers once the code works.
func devicePair(b *bridge, p DevicePairParams) (DevicePairing, error) {
	if !b.human() {
		return DevicePairing{}, devicesHumanOnly()
	}
	dir, peerDir := b.deviceDirs()
	relayURL := ""
	if p.Relay != "" {
		r, err := peer.RelayURL(p.Relay)
		if err != nil {
			return DevicePairing{}, &Error{Code: ErrBadParams, Message: err.Error()}
		}
		relayURL = r
	} else {
		r, ok, err := device.KnownRelay(dir, peerDir)
		if err != nil {
			return DevicePairing{}, devicesError(err)
		}
		if !ok {
			return DevicePairing{}, &Error{Code: ErrRelayNeeded, Message: "no relay is known yet: give the relay the phone and this server meet on (wss://host)"}
		}
		relayURL = r
	}
	id, created, err := peer.EnsureIdentity(peerDir)
	if err != nil {
		return DevicePairing{}, &Error{Code: ErrDevices, Message: err.Error()}
	}
	if created {
		b.devicesChanged()
	}
	in, err := device.NewInvite(relayURL, id.ID)
	if err != nil {
		return DevicePairing{}, &Error{Code: ErrDevices, Message: err.Error()}
	}
	ctx, cancel := context.WithTimeout(b.ctx, pairWait)
	pg := &pairing{id: newPairingID(), relay: relayURL, cancel: cancel, waiting: make(chan struct{}), offered: make(chan struct{}),
		done: make(chan struct{}), answer: make(chan string, 1)}
	b.mu.Lock()
	old := b.pairing
	b.pairing = pg
	b.mu.Unlock()
	if old != nil {
		old.cancel()
	}
	name := pairName()
	go b.runPairing(ctx, pg, id, in, name, dir)
	select {
	case <-pg.waiting:
	case <-pg.done:
		return DevicePairing{}, pg.err
	}
	return DevicePairing{Pairing: pg.id, Invite: in.String(), Expires: time.Now().Add(device.PairWindow), Server: id.ID.Short(),
		Name: name, Relay: peer.RelayHost(relayURL), Note: relay.PrivateWarning(relayURL)}, nil
}

// runPairing runs pairing pg until it ends: the phone's offer waits for
// the user's answer (device.pair.answer), and a yes adds the device.
func (b *bridge) runPairing(ctx context.Context, pg *pairing, id *peer.Identity, in device.Invite, name, dir string) {
	defer pg.cancel()
	defer close(pg.done)
	var as string
	p, err := device.Pair(ctx, id, in, device.PairOptions{Name: name, Version: version.Version, RelayTLS: pairRelayTLS(),
		Waiting: func() { close(pg.waiting) },
		Confirm: func(off device.Offer) (bool, error) {
			ds, err := device.Load(dir)
			if err != nil {
				return false, err
			}
			proposed, again, _ := device.Propose(ds, off, "")
			pg.offer = off
			pg.shown = DeviceOffer{Name: proposed, Phone: off.Name, Short: off.ID.Short(), Version: off.Version,
				WriteKey: off.WriteKey != nil, Level: device.LevelView, AnswerBy: time.Now().Add(device.AnswerWindow)}
			if again != nil {
				pg.shown.Again, pg.shown.Level = again.Name, again.Level
			}
			close(pg.offered)
			select {
			case as = <-pg.answer:
				return as != "", nil
			case <-ctx.Done():
				return false, ctx.Err()
			}
		}})
	if err != nil {
		pg.err = pairError(ctx, err)
		return
	}
	d, err := device.Add(dir, p.Key, p.WriteKey, as, pg.relay)
	if err != nil {
		pg.err = devicesError(err)
		return
	}
	b.devicesChanged()
	ad := deviceOf(d)
	pg.dev = &ad
}

// pairError is how a pairing ended, as the device methods answer it.
func pairError(ctx context.Context, err error) *Error {
	switch {
	case errors.Is(err, peer.ErrDeclined):
		return &Error{Code: "declined", Message: "not paired: you said no"}
	case errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled):
		return &Error{Code: ErrPairEnded, Message: "the pairing was cancelled"}
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, peer.ErrNoPairer):
		return &Error{Code: ErrPairExpired, Message: "the code's time ran out: pair again for a new one"}
	case errors.Is(err, peer.ErrPeerNo):
		return &Error{Code: "declined", Message: "not paired: the phone said no"}
	case errors.Is(err, peer.ErrBadCode) || errors.Is(err, peer.ErrNotExpected):
		return &Error{Code: "bad-code", Message: err.Error()}
	}
	return &Error{Code: ErrPairFailed, Message: err.Error()}
}

// openPairing is the bridge's pairing called id, while it is open.
func (b *bridge) openPairing(id string) (*pairing, *Error) {
	b.mu.Lock()
	pg := b.pairing
	b.mu.Unlock()
	if pg == nil || pg.id != id {
		return nil, &Error{Code: ErrPairEnded, Message: "that pairing isn't open any more: pair again"}
	}
	return pg, nil
}

// devicePairWait answers device.pair.wait: the phone's offer once it
// joined, or how the pairing ended before.
func devicePairWait(b *bridge, p DevicePairingParams) (DeviceOffer, error) {
	if !b.human() {
		return DeviceOffer{}, devicesHumanOnly()
	}
	pg, e := b.openPairing(p.Pairing)
	if e != nil {
		return DeviceOffer{}, e
	}
	select {
	case <-pg.done:
		return DeviceOffer{}, pg.ended()
	default:
	}
	select {
	case <-pg.offered:
		return pg.shown, nil
	case <-pg.done:
		return DeviceOffer{}, pg.ended()
	}
}

// ended is how the ended pairing pg ended, for a wait.
func (pg *pairing) ended() *Error {
	if pg.err != nil {
		return pg.err
	}
	return &Error{Code: ErrPairEnded, Message: "the pairing ended: the phone is paired"}
}

// devicePairAnswer answers device.pair.answer: a yes to the offer, under
// a name the devices don't have, waits for the pairing to end and
// answers the device added; a no ends the pairing, offer or not.
func devicePairAnswer(b *bridge, p DevicePairAnswerParams) (DevicePaired, error) {
	if !b.human() {
		return DevicePaired{}, devicesHumanOnly()
	}
	pg, e := b.openPairing(p.Pairing)
	if e != nil {
		return DevicePaired{}, e
	}
	if !p.Accept {
		select {
		case <-pg.offered:
			select {
			case pg.answer <- "":
			default:
			}
		default:
			pg.cancel()
		}
		<-pg.done
		return DevicePaired{}, nil
	}
	select {
	case <-pg.offered:
	default:
		return DevicePaired{}, &Error{Code: ErrBadParams, Message: "no phone has joined the pairing yet"}
	}
	name := pg.shown.Name
	if p.Name != "" {
		if err := device.CheckName(p.Name); err != nil {
			return DevicePaired{}, &Error{Code: ErrBadParams, Message: err.Error()}
		}
		dir, _ := b.deviceDirs()
		ds, err := device.Load(dir)
		if err != nil {
			return DevicePaired{}, devicesError(err)
		}
		if _, _, err := device.Propose(ds, pg.offer, p.Name); err != nil {
			return DevicePaired{}, devicesError(err)
		}
		name = p.Name
	}
	select {
	case pg.answer <- name:
	default:
		return DevicePaired{}, &Error{Code: ErrPairEnded, Message: "the pairing was answered already"}
	}
	<-pg.done
	if pg.err != nil {
		return DevicePaired{}, pg.err
	}
	return DevicePaired{Device: pg.dev}, nil
}

func newPairingID() string {
	var b [8]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// pairName is the name this server gives the phone.
func pairName() string {
	h, _ := os.Hostname()
	return peer.HostLabel(h)
}
