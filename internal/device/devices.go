// Package device pairs this tm server with the user's phones and keeps a
// link to each (docs/SPEC.md §16.15). A device is not a peer: it exports
// nothing, mirrors nothing and is never offered shared projects. It
// pairs by a QR code that carries the relay, a one-time secret and this
// server's key fingerprint, then meets the server in a link room on the
// relay, TLS 1.3 with both keys pinned inside, as peers do (§16.7). On
// its side of each link the server runs the app API (internal/appapi)
// as the device, filtered by the device's level.
package device

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/plat/flock"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/relay"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// DevicesFile is the paired devices' list in the device state directory.
const DevicesFile = "devices.json"

// Dir is the device state directory, <home>/state/device. The server
// owns it; no agent reads it. The server's key stays the peers'
// (peer.Dir): a phone pins the same key a paired server does.
func Dir() (string, error) {
	h, err := home.Dir()
	if err != nil {
		return "", err
	}
	return DirIn(h), nil
}

// DirIn is the device state directory of the home h.
func DirIn(h string) string { return filepath.Join(h, "state", "device") }

// The levels a device may have: what the app API does for it. Each
// takes in the one before.
const (
	// LevelView reads: the projects, sessions, conversations and panes,
	// and the notice tray.
	LevelView = "view"
	// LevelAnswer adds answering questions and approvals (never one that
	// runs a command outside the sandbox), accepting and sending back
	// tasks, and opening the coordinator's questions.
	LevelAnswer = "answer"
	// LevelWrite adds prompts, new tasks, delegating, starting a
	// coordinator and pane keys.
	LevelWrite = "write"
)

// Levels are the levels, narrowest first.
var Levels = []string{LevelView, LevelAnswer, LevelWrite}

// Enabled says whether this release lets a device have level: every
// level, since write came with the phone's composer (T821).
func Enabled(level string) bool { return ValidLevel(level) }

// Effective is the level d's link runs at: its own when this release
// enables it and, above view, d has a write key to sign its acts with;
// else view.
func (d Device) Effective() string {
	if Enabled(d.Level) && (d.Level == LevelView || d.WriteKey != nil) {
		return d.Level
	}
	return LevelView
}

// ValidLevel says whether s is a level.
func ValidLevel(s string) bool {
	for _, l := range Levels {
		if s == l {
			return true
		}
	}
	return false
}

// Device is a paired device: the name the user knows it by, the key
// pinned at pairing, its level, the relay both dial and its write key.
type Device struct {
	Name  string            `json:"name"`
	Key   ed25519.PublicKey `json:"key"` // base64
	Level string            `json:"level"`
	Relay string            `json:"relay"` // wss://host[:port][#sha256=<pin>]
	// WriteKey is the P-256 key the device signs each act with
	// (WriteKeySize bytes, uncompressed, base64), sent at pairing: on a
	// phone it lives in the Secure Enclave and only Face ID or the
	// passcode unlocks it. Without one the device acts on nothing.
	WriteKey []byte `json:"write_key,omitempty"`
}

// WriteKeySize is a write key's length: an uncompressed P-256 point
// (0x04, X, Y), as the Secure Enclave gives it (x963Representation).
const WriteKeySize = 65

// CheckWriteKey says why b isn't a write key.
func CheckWriteKey(b []byte) error {
	if len(b) != WriteKeySize {
		return fmt.Errorf("the write key is %d bytes, want %d (an uncompressed P-256 point)", len(b), WriteKeySize)
	}
	if _, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), b); err != nil {
		return fmt.Errorf("the write key isn't a P-256 point: %v", err)
	}
	return nil
}

// Verifier is d's write key, to check its signatures with; nil when it
// has none.
func (d Device) Verifier() *ecdsa.PublicKey {
	k, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), d.WriteKey)
	if err != nil {
		return nil
	}
	return k
}

// ID is the device's id: its key's SHA-256, as a server's.
func (d Device) ID() peer.ID { return peer.IDOf(d.Key) }

// MaxName bounds a device's name, in characters.
const MaxName = 40

// CheckName says why s can't be a device's name: it is one line of 1 to
// MaxName characters of plain text, without spaces at its ends.
func CheckName(s string) error {
	if s == "" || utf8.RuneCountInString(s) > MaxName || textsafe.Line(s, 0) != s {
		return fmt.Errorf("device name %q: 1 to %d characters on one line, plain text", s, MaxName)
	}
	return nil
}

// CleanName is the name a device gave itself, made a name: one line,
// cut to MaxName; "iPhone" when nothing is left.
func CleanName(s string) string {
	s = textsafe.Line(s, MaxName)
	if s == "" {
		return "iPhone"
	}
	return s
}

func (d Device) check() error {
	if err := CheckName(d.Name); err != nil {
		return err
	}
	if len(d.Key) != ed25519.PublicKeySize {
		return fmt.Errorf("device %s: the key is %d bytes, want %d", d.Name, len(d.Key), ed25519.PublicKeySize)
	}
	if !ValidLevel(d.Level) {
		return fmt.Errorf("device %s: level %q is not view, answer or write", d.Name, d.Level)
	}
	u, err := url.Parse(d.Relay)
	if err != nil || (u.Scheme != "wss" && u.Scheme != "ws") || u.Host == "" {
		return fmt.Errorf("device %s: relay %q is not a wss:// URL", d.Name, d.Relay)
	}
	if pin, err := relay.ParsePin(u.Fragment); err != nil || (pin != "" && u.Scheme != "wss") {
		return fmt.Errorf("device %s: relay %q: a pin is #sha256= and 64 hex digits, on a wss:// URL", d.Name, d.Relay)
	}
	if d.WriteKey != nil {
		if err := CheckWriteKey(d.WriteKey); err != nil {
			return fmt.Errorf("device %s: %w", d.Name, err)
		}
	}
	return nil
}

type devicesDoc struct {
	Devices []Device `json:"devices"`
}

// Load reads the paired devices in dir; none when the file isn't there.
// Names (ignoring case) and keys are unique.
func Load(dir string) ([]Device, error) {
	b, err := os.ReadFile(filepath.Join(dir, DevicesFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var d devicesDoc
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("%s: %w", DevicesFile, err)
	}
	if err := checkDevices(d.Devices); err != nil {
		return nil, fmt.Errorf("%s: %w", DevicesFile, err)
	}
	return d.Devices, nil
}

// Save replaces the paired devices in dir.
func Save(dir string, devices []Device) error {
	if err := checkDevices(devices); err != nil {
		return err
	}
	if err := fsx.EnsurePrivateDir(dir); err != nil {
		return err
	}
	if devices == nil {
		devices = []Device{}
	}
	b, err := json.MarshalIndent(devicesDoc{Devices: devices}, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteAtomic(filepath.Join(dir, DevicesFile), append(b, '\n'), 0o600)
}

func checkDevices(devices []Device) error {
	names, keys := map[string]bool{}, map[peer.ID]bool{}
	for _, d := range devices {
		if err := d.check(); err != nil {
			return err
		}
		if names[strings.ToLower(d.Name)] {
			return fmt.Errorf("two devices named %s", d.Name)
		}
		if keys[d.ID()] {
			return fmt.Errorf("device %s: the key is paired twice", d.Name)
		}
		names[strings.ToLower(d.Name)], keys[d.ID()] = true, true
	}
	return nil
}

// lockFile serializes changes to the devices.
const lockFile = "devices.lock"

// change runs f on the devices in dir under the lock and saves what it
// returns.
func change(dir string, f func([]Device) ([]Device, error)) error {
	if err := fsx.EnsurePrivateDir(dir); err != nil {
		return err
	}
	lk, err := flock.Wait(filepath.Join(dir, lockFile))
	if err != nil {
		return err
	}
	defer lk.Unlock()
	devices, err := Load(dir)
	if err != nil {
		return err
	}
	if devices, err = f(devices); err != nil {
		return err
	}
	return Save(dir, devices)
}

// find is the index of the device named name (ignoring case) or whose
// id starts with name's hex digits (its short id, 4F2A-91C3); -1 for
// none.
func find(devices []Device, name string) int {
	for i, d := range devices {
		if strings.EqualFold(d.Name, name) {
			return i
		}
	}
	short := strings.ToLower(strings.ReplaceAll(name, "-", ""))
	if len(short) < 8 {
		return -1
	}
	at := -1
	for i, d := range devices {
		if strings.HasPrefix(d.ID().String(), short) {
			if at >= 0 {
				return -1
			}
			at = i
		}
	}
	return at
}

// UnknownError says no device has that name.
type UnknownError struct{ Name string }

func (e *UnknownError) Error() string { return fmt.Sprintf("no device %q (tm device list)", e.Name) }

// TakenError says another device has that name.
type TakenError struct{ Name string }

func (e *TakenError) Error() string {
	return fmt.Sprintf("a device is already called %s (tm device name changes it)", e.Name)
}

// FreeName is name, or name with " 2", " 3" … when a device other than
// the one with key has it.
func FreeName(devices []Device, name string, key ed25519.PublicKey) string {
	taken := func(n string) bool {
		i := find(devices, n)
		return i >= 0 && strings.EqualFold(devices[i].Name, n) && !devices[i].Key.Equal(key)
	}
	out := name
	for n := 2; taken(out); n++ {
		suffix := fmt.Sprintf(" %d", n)
		r := []rune(name)
		if len(r)+len(suffix) > MaxName {
			r = r[:MaxName-len(suffix)]
		}
		out = strings.TrimSpace(string(r)) + suffix
	}
	return out
}

// Add keeps key as the device called name through relayURL, with its
// write key (nil for none), at view. A key already paired keeps its
// level and takes the new name, relay and write key; a name another
// device has is a TakenError.
func Add(dir string, key ed25519.PublicKey, writeKey []byte, name, relayURL string) (Device, error) {
	d := Device{Name: name, Key: key, Level: LevelView, Relay: relayURL, WriteKey: writeKey}
	if err := d.check(); err != nil {
		return Device{}, err
	}
	err := change(dir, func(devices []Device) ([]Device, error) {
		var kept []Device
		for _, q := range devices {
			switch {
			case q.Key.Equal(key):
				d.Level = q.Level
			case strings.EqualFold(q.Name, name):
				return nil, &TakenError{name}
			default:
				kept = append(kept, q)
			}
		}
		return append(kept, d), nil
	})
	return d, err
}

// Remove drops the device called name; it returns the one it dropped.
func Remove(dir, name string) (Device, error) {
	var gone Device
	err := change(dir, func(devices []Device) ([]Device, error) {
		i := find(devices, name)
		if i < 0 {
			return nil, &UnknownError{name}
		}
		gone = devices[i]
		return append(devices[:i], devices[i+1:]...), nil
	})
	return gone, err
}

// NoWriteKeyError says a device paired without a write key can't be
// raised above view: it has nothing to sign its acts with.
type NoWriteKeyError struct{ Name string }

func (e *NoWriteKeyError) Error() string {
	return fmt.Sprintf("%s has no write key to sign its acts with: pair it again from an app that has one (tm device pair)", e.Name)
}

// SetLevel sets the level of the device called name; above view it
// needs a write key (NoWriteKeyError).
func SetLevel(dir, name, level string) (Device, error) {
	if !ValidLevel(level) {
		return Device{}, fmt.Errorf("level %q: view, answer or write", level)
	}
	var d Device
	err := change(dir, func(devices []Device) ([]Device, error) {
		i := find(devices, name)
		if i < 0 {
			return nil, &UnknownError{name}
		}
		if level != LevelView && devices[i].WriteKey == nil {
			return nil, &NoWriteKeyError{devices[i].Name}
		}
		devices[i].Level = level
		d = devices[i]
		return devices, nil
	})
	return d, err
}

// Rename gives the device called name the name to.
func Rename(dir, name, to string) (Device, error) {
	if err := CheckName(to); err != nil {
		return Device{}, err
	}
	var d Device
	err := change(dir, func(devices []Device) ([]Device, error) {
		i := find(devices, name)
		if i < 0 {
			return nil, &UnknownError{name}
		}
		for j, q := range devices {
			if j != i && strings.EqualFold(q.Name, to) {
				return nil, &TakenError{to}
			}
		}
		devices[i].Name = to
		d = devices[i]
		return devices, nil
	})
	return d, err
}
