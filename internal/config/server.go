package config

import (
	"os"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

// ServerTable is the [server] table: this server's own settings.
const ServerTable = "server"

// ServerNameKey is [server] name, this server's own name.
const ServerNameKey = "name"

// serverName caches OwnServerName by the file's size and time: screens
// ask for the name on every draw.
var serverName struct {
	sync.Mutex
	path string
	size int64
	mod  time.Time
	name string
}

// OwnServerName is [server] name from config.toml as written, "" when
// unset or the file can't be read. It reads only that table, so a
// safety error elsewhere in the file doesn't hide it (peer.SelfName
// checks it).
func OwnServerName() string {
	path, err := Path()
	if err != nil {
		return ""
	}
	fi, err := os.Stat(path)
	if err != nil {
		return ""
	}
	c := &serverName
	c.Lock()
	defer c.Unlock()
	if c.path == path && c.size == fi.Size() && c.mod.Equal(fi.ModTime()) {
		return c.name
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var raw struct {
		Server struct {
			Name string `toml:"name"`
		} `toml:"server"`
	}
	if _, err := toml.Decode(string(data), &raw); err != nil {
		raw.Server.Name = ""
	}
	c.path, c.size, c.mod, c.name = path, fi.Size(), fi.ModTime(), raw.Server.Name
	return c.name
}
