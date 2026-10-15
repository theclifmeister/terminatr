package tui

import (
	"sync/atomic"

	"github.com/theclifmeister/terminatr/internal/update"
	"github.com/theclifmeister/terminatr/internal/version"
)

// The server's and this console's versions: the help's title and the
// hints to upgrade or restart an older console.

// versionTitle names the server's version, "terminatr v0.15.0", in the
// help and as the ≡ menu's title; "" from a server too old to say.
func versionTitle(s ServerInfo) string {
	if v := serverVersion(s); v != "" {
		return "terminatr " + v
	}
	return ""
}

// serverVersion is the help's and the ≡ menu's "v0.15.0": the server's version,
// and this tm's too when it differs (docs/SPEC.md §4). Empty from a
// server too old to say.
func serverVersion(s ServerInfo) string {
	if s.Version == "" {
		return ""
	}
	out := s.Version
	if version.Version != s.Version {
		out += " (tm " + version.Version + ")"
	}
	return out
}

// consoleOlder reports whether this console runs an older tm than the
// server: it was started before an upgrade and the server restarted on
// the new one since. Its old code misses what the server now expects of
// a console (the panes' theme, T344): only a restart of the console
// fixes that, not of the server.
func consoleOlder(s ServerInfo) bool {
	if s.Build == "" || s.Build == version.BuildID() {
		return false
	}
	// The sidebar asks several times a frame; the answer is the same
	// while the server's build and version are (PERF-08).
	k := olderKey{s.Build, s.Version}
	if c := olderMemo.Load(); c != nil && c.key == k {
		return c.older
	}
	newer, ok := update.Newer(s.Version, version.Version)
	olderMemo.Store(&olderAns{key: k, older: ok && newer})
	return ok && newer
}

type olderKey struct{ build, version string }

type olderAns struct {
	key   olderKey
	older bool
}

// olderMemo is consoleOlder's last answer.
var olderMemo atomic.Pointer[olderAns]

// oldConsoleSaid: this console said it is older than the server, once.
var oldConsoleSaid atomic.Bool

// oldConsoleNews is the note that this console is older than the server
// (consoleOlder), for the footer or the status bar, the first time it
// is; "" after.
func oldConsoleNews(s ServerInfo) string {
	if !consoleOlder(s) || oldConsoleSaid.Swap(true) {
		return ""
	}
	return versionHint(s)
}

// versionHint is the footer's faint line while it has no message: this
// console is older than the server (a warning), the installed tm is not
// the one the server runs, else a newer release is out. Empty for none.
func versionHint(s ServerInfo) string {
	switch {
	case s.Build == "":
		return ""
	case consoleOlder(s):
		return "this console runs tm " + version.Version + ", the server " + s.Version + ": quit it and run tm again"
	case s.Build != version.BuildID():
		return "tm " + version.Version + " is installed, the server runs " + s.Version + ": run tm server restart in a terminal"
	case s.Latest != "":
		return "tm " + s.Latest + " is out: " + s.Upgrade
	}
	return ""
}

// versionHintShort is versionHint for a narrow sidebar.
func versionHintShort(s ServerInfo) string {
	switch {
	case s.Build == "":
		return ""
	case consoleOlder(s):
		return "old console: quit, run tm again"
	case s.Build != version.BuildID():
		return "run tm server restart"
	case s.Latest != "":
		return s.Latest + " is out: " + s.Upgrade
	}
	return ""
}
