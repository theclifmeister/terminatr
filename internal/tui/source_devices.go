package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	tmclient "github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/device"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/version"
)

// ServerSource's DeviceSource: the paired phones (docs/SPEC.md §16.15).
// As tm device and the app's bridge do, it reads and changes the
// devices in this process, never through the server, and has a running
// server read them again after each change (device.reload); their
// links come from server.status.

// DeviceList is the paired devices, for the Network popup's phones
// (networkphones.go), oldest first. Running says a server answered, so
// each one's Link is its link (none yet: nil). Relay is the relay a
// pairing goes through, "" when none is known (none, or several, among
// the paired devices and servers).
type DeviceList struct {
	Devices []DeviceEntry
	Running bool
	Relay   string
}

// DeviceEntry is one paired device: its name, id (and short id), level
// and the one its link runs at (view without a write key), its relay,
// whether it has a write key, its link while a server runs it, and when
// its link was last up (zero before it ever linked).
type DeviceEntry struct {
	Name, ID, Short, Level, Effective, Relay string
	WriteKey                                 bool
	Link                                     *proto.DeviceLink
	LastUp                                   time.Time
}

// DevicePairEvents are what a phone's pairing shows on the way: Waiting
// the invite once it works, Confirm the phone that joined, returning
// the name to add it under ("" for no).
type DevicePairEvents struct {
	Waiting func(DeviceInvite)
	Confirm func(DeviceOffer) string
}

// DeviceInvite is a pairing's code as its QR code holds it
// (terminatr://pair?…), until when it works, this server's name and
// short id, the relay's host and a note on it (a private address).
type DeviceInvite struct {
	Invite, Name, Server, Relay, Note string
	Expires                           time.Time
}

// DeviceOffer is the phone that joined a pairing, as tm device pair
// shows it before asking: Name is what it would be added as, Phone the
// name it gave; Again the device's name when its key is paired already
// (it keeps Level, view when KeyChanged: it brings another write key);
// WriteKey false: it can only view. AnswerBy bounds the answer. Check
// says why a name can't be the one it is added as.
type DeviceOffer struct {
	Name, Phone, Short, Version, Again, Level string
	WriteKey, KeyChanged                      bool
	AnswerBy                                  time.Time
	Check                                     func(name string) error
}

// errRelayNeeded says no relay is known for a pairing: the user gives
// one.
var errRelayNeeded = errors.New("no relay is known yet: give the relay the phone and this server meet on")

func (s *ServerSource) Devices() (DeviceList, error) {
	var l DeviceList
	if s.Caller.IsAgent() {
		return l, errHumanOnly
	}
	dir, peerDir := device.DirIn(s.Paths.Home), peer.DirIn(s.Paths.Home)
	ds, err := device.Load(dir)
	if err != nil {
		return l, err
	}
	if r, ok, err := device.KnownRelay(dir, peerDir); err == nil && ok {
		l.Relay = r
	}
	links := map[string]proto.DeviceLink{}
	var st proto.ServerStatus
	if s.call(proto.MethodServerStatus, nil, &st) == nil {
		l.Running = true
		for _, dl := range st.Devices {
			links[dl.ID] = dl
		}
	}
	for _, d := range ds {
		e := DeviceEntry{Name: d.Name, ID: d.ID().String(), Short: d.ID().Short(), Level: d.Level, Effective: d.Effective(),
			Relay: d.Relay, WriteKey: d.WriteKey != nil, LastUp: d.LastUp}
		if dl, ok := links[e.ID]; ok {
			e.Link = &dl
			if dl.LastUp.After(e.LastUp) {
				e.LastUp = dl.LastUp
			}
		}
		l.Devices = append(l.Devices, e)
	}
	return l, nil
}

// devicesChanged has a running server read the devices again; one that
// isn't running reads them when it starts.
func (s *ServerSource) devicesChanged(done string) error {
	var links []proto.DeviceLink
	if err := s.call(proto.MethodDeviceReload, nil, &links); err != nil && !errors.Is(err, tmclient.ErrNotRunning) {
		return fmt.Errorf("%s, but the server didn't read the devices again (%v); tm server restart does", done, err)
	}
	return nil
}

func (s *ServerSource) SetDeviceLevel(id, level string) error {
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	if !device.Enabled(level) {
		return fmt.Errorf("level %s isn't in this release", level)
	}
	d, err := device.SetLevel(device.DirIn(s.Paths.Home), id, level)
	if err != nil {
		return err
	}
	return s.devicesChanged(d.Name + " is " + level)
}

func (s *ServerSource) RenameDevice(id, name string) error {
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	d, err := device.Rename(device.DirIn(s.Paths.Home), id, name)
	if err != nil {
		return err
	}
	return s.devicesChanged("renamed " + d.Name)
}

func (s *ServerSource) RemoveDevice(id string) error {
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	d, err := device.Remove(device.DirIn(s.Paths.Home), id)
	if err != nil {
		return err
	}
	return s.devicesChanged("removed " + d.Name)
}

// devicePairWait bounds a phone's pairing: the code's 10 minutes, and
// the answer's time once the phone joined (as tm device pair).
const devicePairWait = device.PairWindow + 3*time.Minute

func (s *ServerSource) PairDevice(ctx context.Context, relayText string, ev DevicePairEvents) (DeviceEntry, error) {
	if s.Caller.IsAgent() {
		return DeviceEntry{}, errHumanOnly
	}
	dir, peerDir := device.DirIn(s.Paths.Home), peer.DirIn(s.Paths.Home)
	var relayURL string
	if relayText != "" {
		r, err := peer.RelayURL(relayText)
		if err != nil {
			return DeviceEntry{}, err
		}
		relayURL = r
	} else {
		r, ok, err := device.KnownRelay(dir, peerDir)
		if err != nil {
			return DeviceEntry{}, err
		}
		if !ok {
			return DeviceEntry{}, errRelayNeeded
		}
		relayURL = r
	}
	id, created, err := peer.EnsureIdentity(peerDir)
	if err != nil {
		return DeviceEntry{}, err
	}
	if created {
		// A server that runs links with its new key.
		s.devicesChanged("made this server's key")
	}
	in, err := device.NewInvite(relayURL, id.ID)
	if err != nil {
		return DeviceEntry{}, err
	}
	host, _ := os.Hostname()
	name := peer.HostLabel(host)
	var as string
	ctx, cancel := context.WithTimeout(ctx, devicePairWait)
	defer cancel()
	p, err := device.Pair(ctx, id, in, device.PairOptions{Name: name, Version: version.Version,
		Waiting: func() {
			ev.Waiting(DeviceInvite{Invite: in.String(), Name: name, Server: id.ID.Short(), Relay: peer.RelayHost(relayURL),
				Note: relay.PrivateWarning(relayURL), Expires: time.Now().Add(device.PairWindow)})
		},
		Confirm: func(off device.Offer) (bool, error) {
			ds, err := device.Load(dir)
			if err != nil {
				return false, err
			}
			proposed, again, _ := device.Propose(ds, off, "")
			o := DeviceOffer{Name: proposed, Phone: off.Name, Short: off.ID.Short(), Version: textsafe.Line(off.Version, 40),
				Level: device.LevelView, WriteKey: off.WriteKey != nil, AnswerBy: time.Now().Add(device.AnswerWindow),
				Check: func(as string) error {
					if err := device.CheckName(as); err != nil {
						return err
					}
					ds, err := device.Load(dir)
					if err != nil {
						return err
					}
					_, _, err = device.Propose(ds, off, as)
					return err
				}}
			if again != nil {
				o.Again = again.Name
				o.Level, o.KeyChanged = again.Again(off.WriteKey)
			}
			as = ev.Confirm(o)
			return as != "", nil
		}})
	if err != nil {
		return DeviceEntry{}, devicePairWords(err, relayURL)
	}
	d, err := device.Add(dir, p.Key, p.WriteKey, as, relayURL)
	if err != nil {
		return DeviceEntry{}, err
	}
	e := DeviceEntry{Name: d.Name, ID: d.ID().String(), Short: d.ID().Short(), Level: d.Level, Effective: d.Effective(),
		Relay: d.Relay, WriteKey: d.WriteKey != nil, LastUp: d.LastUp}
	return e, s.devicesChanged("paired " + d.Name)
}

// devicePairWords is a phone's pairing's error in plain words, as the
// Mac app says them.
func devicePairWords(err error, relayURL string) error {
	switch {
	case errors.Is(err, peer.ErrDeclined):
		return errors.New("not paired: you said no")
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, peer.ErrNoPairer):
		return errors.New("the code's time ran out before the phone joined: make a new code to try again")
	case errors.Is(err, peer.ErrPeerNo):
		return errors.New("not paired: the phone said no")
	case errors.Is(err, peer.ErrBadCode) || errors.Is(err, peer.ErrNotExpected):
		return errors.New("the phone and this server didn't agree on the code: someone in between, or another server; make a new code")
	}
	return pairWords(err, relayURL)
}
