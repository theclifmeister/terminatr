package config

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/home"
)

// Socket rules (T306, T574 phase 4): a unix socket besides tm's that a
// thread's sandboxed commands may connect to (a database's, a dev
// server's, Docker's). One rule for both agents: Claude's
// sandbox.network.allowUnixSockets and Codex's network unix_sockets,
// both macOS only (Claude's Linux sandbox opens every socket or none,
// Codex's names none: on Linux a Codex thread's network is open
// anyway). A socket is one file, written whole: no pattern, no folder
// (a folder would open every socket below it), nor one in tm's folder or
// the threads' temp folders. The launch also refuses one in or holding
// tm's run folder or tm's folder, one in a folder a thread writes (it
// could swap a link in), and one whose link leads there: never the
// human socket, never another session's.

// CheckSocket checks one socket: absolute or from ~/, clean, without
// wildcards or variables, a file at least one folder below the root.
func CheckSocket(e string) error {
	if err := checkSandboxText(e); err != nil {
		return err
	}
	p := strings.ReplaceAll(e, `\`, "/")
	var rest string
	switch {
	case strings.HasPrefix(p, "~/"):
		rest = strings.TrimPrefix(p, "~/")
	case strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//"):
		rest = strings.TrimPrefix(p, "/")
	default:
		return fmt.Errorf("%q isn't a socket from the root or from ~/: write its path whole, e.g. ~/.colima/default/docker.sock", e)
	}
	parts := strings.Split(rest, "/")
	switch {
	case strings.ContainsAny(p, "*?[]{}"):
		return fmt.Errorf("%q has a pattern: a socket rule is one socket, its path written whole", e)
	case path.Clean(p) != p, slices.Contains(parts, ".."), slices.Contains(parts, "."), slices.Contains(parts, ""):
		return fmt.Errorf("%q isn't written plainly: no .., . or // in it, nor a / at its end", e)
	case strings.Contains(p, "$"):
		return fmt.Errorf("%q has a $: a socket is a plain path, without variables", e)
	case len(parts) < 2 && !strings.HasPrefix(p, "~/"):
		return fmt.Errorf("%q is at the disk's top level: name the socket's own path", e)
	}
	for _, x := range parts {
		if fold(x) == fold(".terminatr") {
			return fmt.Errorf("%q is in tm's folder (its sockets, the threads' worktrees): never a socket rule", e)
		}
	}
	// The threads' temp folders are theirs to write: a thread could put
	// a link to another socket there later.
	root := home.ThreadTempRoot()
	for _, r := range []string{root, strings.Replace(root, "/private/tmp/", "/tmp/", 1)} {
		if inFolder(p, filepath.ToSlash(r)) {
			return fmt.Errorf("%q is in the threads' temp folders, which they write: never a socket rule", e)
		}
	}
	return nil
}

// socketWarning is what a socket opens, said where it is added: the
// program listening there does for the thread's commands whatever it
// does for you, outside the sandbox; plainer for Docker's and an ssh
// agent's.
func socketWarning(e string) string {
	w := "the thread's sandboxed commands connect to it, on Claude and Codex threads alike: whatever the program listening there does for them, it does as you, outside the sandbox"
	base := strings.ToLower(path.Base(strings.ReplaceAll(e, `\`, "/")))
	switch {
	case strings.Contains(base, "docker") || strings.Contains(base, "podman") || strings.Contains(strings.ToLower(e), "colima"):
		w += "; Docker's socket runs any container with any of your folders mounted, which is everything outside the sandbox"
	case strings.Contains(base, "ssh") || strings.Contains(base, "agent") || strings.Contains(base, "gpg"):
		w += "; an ssh or gpg agent signs and logs in with your keys for whoever asks"
	}
	return w
}
