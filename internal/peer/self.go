package peer

import (
	"cmp"
	"os"
	"slices"

	"github.com/theclifmeister/terminatr/internal/config"
)

// This server's own name (docs/SPEC.md §16.8): [server] name in
// config.toml, the user's, else the host name's first label. Screens
// show it for this server, pairing gives it the other side, and a link
// tells it a paired server, which shows it in the alias's place (Told).

// CapName says the hello carries the server's own name (Hello.Name)
// and peer.name tells a new one: a server whose hello lists it without
// a name has none set, and is shown by its alias.
const CapName = "name"

// hostName is the host name's first label in lower case (a variable
// for tests).
var hostName = func() string { h, _ := os.Hostname(); return HostLabel(h) }

// HostName is the host name's first label in lower case: this server's
// name while [server] name is unset.
func HostName() string { return hostName() }

// OwnName is [server] name when it is set and a valid name; "" else.
func OwnName() string {
	n := config.OwnServerName()
	if CheckName(n) != nil {
		return ""
	}
	return n
}

// SelfName is what this server calls itself: OwnName, else the host
// label.
func SelfName() string { return cmp.Or(OwnName(), hostName()) }

// SetSelfName sets this server's own name in config.toml, or clears it
// with "" (the host label shows again). It refuses a name a paired
// server in dir has as alias, name or told name, and one taken says is
// taken (a project here).
func SetSelfName(dir, name string, taken NameTaken) error {
	if name == "" {
		return config.Unset(config.ServerTable, config.ServerNameKey)
	}
	if err := CheckName(name); err != nil {
		return err
	}
	peers, err := LoadPeers(dir)
	if err != nil {
		return err
	}
	if i := slices.IndexFunc(peers, func(q Peer) bool { return q.Alias == name || q.Name == name || q.Told == name }); i >= 0 {
		return &NameError{Name: name, Why: "the paired server " + peers[i].Both() + " is called that"}
	}
	if taken != nil {
		if why := taken(name); why != "" {
			return &NameError{Name: name, Why: why}
		}
	}
	return config.Set(config.ServerTable, config.ServerNameKey, name)
}
