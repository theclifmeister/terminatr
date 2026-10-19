//go:build unix

package main

import "github.com/theclifmeister/terminatr/internal/plat/ipc"

// sockDir holds the messaging sockets: the temp folder when it is short
// enough for a socket path (macOS's per-user one isn't), else /tmp.
var sockDir = ipc.ShortTemp()

// shell is the shell that runs hook commands and the Bash tool's.
func shell() string { return "/bin/sh" }

// codexHookShell is how Codex runs a hook command: in sh.
func codexHookShell(command string) (string, []string) { return shell(), []string{"-c", command} }
