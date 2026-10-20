package doctor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/plat/flock"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/keychain"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// Live is what the server check learned about a running server.
type Live struct {
	Running bool
	// Sessions are the ids of the server's sessions; nil when unknown.
	Sessions map[string]bool
	// CodeHost is the server's answer to server.codehost
	// (serverCodeHost); nil when it is not running or can't tell (an
	// older server).
	CodeHost *proto.CodeHostStatus
	// Mods is the server's answer to mod.hosts; nil when it is not
	// running or can't tell (an older server).
	Mods *proto.ModHostsResult
	// Link is the server's links to paired servers (server.status); nil
	// when it is not running or has no identity key.
	Link *proto.LinkStatus
	// Peers is the server's mirror of the paired servers (peer.list):
	// the panes level each allows, the projects each shares; nil when
	// it is not running or can't tell.
	Peers *proto.PeerList
}

// callTimeout bounds each of doctor's calls to the server; a server
// that holds the lock and answers the handshake may still hang on one.
var callTimeout = 15 * time.Second

// codeHostTimeout bounds server.codehost, which runs every code-host
// check in the server (several az and git calls per Azure repo, each up
// to codehost's execTimeout, a few repos at once); past it doctor shows
// this shell's checks instead.
var codeHostTimeout = 3 * time.Minute

// lockHeld reports whether some process holds the server lock. It never
// creates the lock file.
func lockHeld(path string) (bool, error) {
	return flock.Held(path)
}

// removeIfNoServer removes path while holding the server lock, so it can
// never remove a live server's socket or pid file.
func removeIfNoServer(p client.Paths, path string) error {
	lk, err := flock.TryLock(p.Lock)
	if errors.Is(err, flock.ErrLocked) {
		return fmt.Errorf("a server started meanwhile; kept %s", path)
	}
	if err != nil {
		return err
	}
	defer lk.Unlock()
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Server checks the run dir and the server. It never starts a server and
// never removes anything itself.
func Server(d Deps) ([]Check, Live) {
	const g = "server"
	p := d.Paths
	var out []Check
	live := Live{}
	if err := fsx.CheckPrivate(p.RunDir); err == nil {
		out = append(out, Check{Group: g, Name: "run dir", Status: OK, Detail: p.RunDir})
	} else if !errors.Is(err, fs.ErrNotExist) {
		out = append(out, Check{Group: g, Name: "run dir", Status: Fail, Detail: err.Error() + "; the server needs a private run dir"})
	}
	held, err := lockHeld(p.Lock)
	if err != nil {
		return append(out, Check{Group: g, Name: "lock", Status: Fail, Detail: err.Error()}), live
	}
	if !held {
		out = append(out, Check{Group: g, Name: "server", Status: OK, Detail: "not running (any tm command starts it)"})
		for _, f := range []struct{ name, path string }{{"socket", p.Socket}, {"pid file", p.PID}} {
			if _, err := os.Lstat(f.path); err != nil {
				continue
			}
			path := f.path
			out = append(out, Check{Group: g, Name: "stale " + f.name, Status: Warn,
				Detail: path + " is left over from a server that is gone",
				Fix:    &Fix{Desc: "remove stale " + f.name + " " + path, Apply: func() error { return removeIfNoServer(p, path) }}})
		}
		live.Sessions = map[string]bool{}
		return append(out, runtimeDirs(p, live)...), live
	}
	live.Running = true
	pid := readPID(p.PID)
	c, err := client.Dial(p, proto.KindControl)
	if err != nil {
		var verr *proto.MismatchError
		if errors.As(err, &verr) {
			c := Check{Group: g, Name: "server", Status: Warn, Detail: fmt.Sprintf("pid %d: %s", pid, verr.Reason)}
			if d.Restart != nil {
				c.Fix = &Fix{Desc: fmt.Sprintf("restart the server (pid %d, protocol %d) with this tm; agents are resumed", pid, verr.Server.Protocol),
					Apply: d.Restart}
			}
			return append(out, c), live
		}
		return append(out, Check{Group: g, Name: "server", Status: Fail,
			Detail: fmt.Sprintf("holds the lock (pid %d) but doesn't answer (%v); see %s or run tm server stop --force", pid, err, p.Log)}), live
	}
	defer c.Close()
	var st proto.ServerStatus
	if err := c.CallWithin(callTimeout, proto.MethodServerStatus, nil, &st); err != nil {
		return append(out, Check{Group: g, Name: "server", Status: Fail, Detail: fmt.Sprintf("pid %d: server.status: %v", pid, err)}), live
	}
	live.Link = st.Link
	out = append(out, Check{Group: g, Name: "server", Status: OK,
		Detail: fmt.Sprintf("pid %d, up %s, protocol %d, %d sessions", st.PID, time.Since(st.Started).Round(time.Second), st.Protocol, st.Sessions)})
	if st.Build != d.Build {
		out = append(out, Check{Group: g, Name: "server build", Status: Warn,
			Detail: fmt.Sprintf("the server runs build %s, this tm is %s; tm server restart switches it (agents are resumed)", st.Build, d.Build)})
	}
	if d.GOOS == "darwin" {
		var ks proto.KeychainStatus
		err := c.CallWithin(callTimeout, proto.MethodServerKeychain, nil, &ks)
		out = append(out, keychainCheck(d, ks, err)...)
	}
	if st.PreviousShutdown == "crash" {
		detail := "the previous server crashed"
		if len(st.Lost) > 0 {
			detail += "; lost sessions: " + strings.Join(st.Lost, " ")
		}
		out = append(out, Check{Group: g, Name: "last shutdown", Status: Warn, Detail: detail + "; see " + p.Log})
	}
	var ms proto.ModHostsResult
	if err := c.CallWithin(callTimeout, proto.MethodModHosts, nil, &ms); err == nil {
		live.Mods = &ms
	}
	var pl proto.PeerList
	if st.Link != nil && len(st.Link.Peers) > 0 {
		if err := c.CallWithin(callTimeout, proto.MethodPeerList, nil, &pl); err == nil {
			live.Peers = &pl
		}
	}
	var list proto.SessionListResult
	if err := c.CallWithin(callTimeout, proto.MethodSessionList, nil, &list); err == nil {
		out = append(out, queueChecks(list.Sessions, time.Now())...)
		out = append(out, skippedChecks(list.Sessions)...)
		if live.Sessions = allSessions(list, st.Sessions); live.Sessions != nil {
			out = append(out, runtimeDirs(p, live)...)
		} else {
			out = append(out, Check{Group: g, Name: "runtime dirs", Status: OK,
				Detail: "not checked: from a session tm sees only its project's sessions, not every session of the server; run tm doctor in a terminal to check them"})
		}
	}
	return out, live
}

// allSessions are the ids of every session of the server, from its
// session.list answer and server.status's count; nil when the list holds
// only the sessions the caller sees (a session's connection, which an
// older server doesn't flag but lists fewer than it counts), so no
// runtime dir is taken for a leftover of another project's session.
func allSessions(list proto.SessionListResult, count int) map[string]bool {
	if list.Scoped || len(list.Sessions) < count {
		return nil
	}
	m := map[string]bool{}
	for _, s := range list.Sessions {
		m[s.ID] = true
	}
	return m
}

// queueChecks warns about sessions whose queued prompts are held while
// the agent is idle (a prompt box with text in it, a dialog): the prompt
// waits, and a coordinator's nudges with it (§7.5, §8.6).
func queueChecks(sessions []proto.SessionInfo, now time.Time) []Check {
	var out []Check
	for _, s := range sessions {
		n := s.QueueNote(now)
		if n == "" {
			continue
		}
		out = append(out, Check{Group: "server", Name: "prompt queue", Status: Warn,
			Detail: fmt.Sprintf("%s: %d queued prompt(s), %s; nothing is pasted until it clears, and a coordinator gets no nudges meanwhile. "+
				"Once it has been held for its bound the server drops it, or writes a tm prompt to the agent's socket, unconfirmed (journaled); tm agent explain %s shows the queue", sessionWho(s), s.Queued, n, s.ID)})
	}
	return out
}

// skippedChecks warns about agent sessions whose terminatr hooks another
// of the agent's mods skipped: tm's command hooks give the role's
// context instead, but the agent's other terminatr hooks on those events
// don't run either (docs/SPEC.md §7.8).
func skippedChecks(sessions []proto.SessionInfo) []Check {
	var out []Check
	for _, s := range sessions {
		if s.HooksSkipped == "" {
			continue
		}
		out = append(out, Check{Group: "server", Name: "agent mod", Status: Warn,
			Detail: fmt.Sprintf("%s: terminatr hooks skipped by %s; tm gives the role's context through its command hooks instead. "+
				"Claude Code's built-in cc-plugin-sec-default does this on managed and Team or Enterprise machines; only the organization's administrator can change it (managed prependPlugins without sec-default@builtin)", sessionWho(s), s.HooksSkipped)})
	}
	return out
}

// sessionWho names session s for a check: its id, project, role and thread.
func sessionWho(s proto.SessionInfo) string {
	who := s.ID
	if s.Project != "" {
		who += " (" + s.Project + " " + s.Role
		if s.Thread != "" {
			who += " " + s.Thread
		}
		who += ")"
	}
	return who
}

// keychainCheck reports whether the server's sessions can reach the
// macOS login keychain (server.keychain). A restart is offered when it
// starts the server through launchd's GUI domain, or when this tm can
// reach the keychain itself: a direct restart from an SSH login, or from a
// session of the same server, would start the server where it was.
func keychainCheck(d Deps, ks proto.KeychainStatus, err error) []Check {
	const g, name = "server", "keychain"
	var perr *proto.Error
	switch {
	case errors.As(err, &perr) && perr.Code == proto.ErrUnknownMethod:
		return nil // an older server; the build check says to restart it
	case err != nil:
		return []Check{{Group: g, Name: name, Status: Warn, Detail: "couldn't ask the server: " + err.Error()}}
	case !ks.Checked:
		return nil
	case ks.OK:
		return []Check{{Group: g, Name: name, Status: OK, Detail: "sessions can reach the login keychain"}}
	}
	c := Check{Group: g, Name: name, Status: Warn,
		Detail: ks.Detail + "; the PR CLI (gh, az) and git credential helpers fail in its sessions: " + keychain.Fix}
	switch {
	case d.Restart == nil:
	case d.Launchd != nil && d.Launchd():
		c.Fix = &Fix{Desc: "restart the server in the desktop's session (through launchd), so its sessions can reach the keychain; agents are resumed",
			Apply: d.Restart}
	case d.Keychain != nil:
		if self := d.Keychain(); self.OK && !self.OverSSH {
			c.Fix = &Fix{Desc: "restart the server from this terminal, so its sessions can reach the keychain; agents are resumed",
				Apply: d.Restart}
		}
	}
	return []Check{c}
}

// runtimeDirs finds session runtime dirs (<run dir>/s/<id>) of sessions
// that no longer exist. Only called when every live session is known
// (allSessions), never from a session's scoped view.
func runtimeDirs(p client.Paths, live Live) []Check {
	root := filepath.Join(p.RunDir, "s")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []Check
	for _, e := range entries {
		if !e.IsDir() || live.Sessions[e.Name()] {
			continue
		}
		dir := filepath.Join(root, e.Name())
		apply := func() error { return os.RemoveAll(dir) }
		if live.Running {
			// The server may have started that session since.
			id := e.Name()
			apply = func() error {
				if l := sessionsNow(p); l == nil || l[id] {
					return fmt.Errorf("kept %s: session %s may be running", dir, id)
				}
				return os.RemoveAll(dir)
			}
		}
		out = append(out, Check{Group: "server", Name: "runtime dir", Status: Warn,
			Detail: dir + " belongs to no running session",
			Fix:    &Fix{Desc: "remove " + dir, Apply: apply}})
	}
	return out
}

// serverCodeHost asks a running server for its code-host checks
// (server.codehost) on a connection of its own, so Each can run it
// beside the other checks; nil when no server answers.
func serverCodeHost(p client.Paths) *proto.CodeHostStatus {
	if held, _ := lockHeld(p.Lock); !held {
		return nil
	}
	c, err := client.Dial(p, proto.KindControl)
	if err != nil {
		return nil
	}
	defer c.Close()
	var ch proto.CodeHostStatus
	if err := c.CallWithin(codeHostTimeout, proto.MethodServerCodeHost, nil, &ch); err != nil {
		return nil
	}
	return &ch
}

// sessionsNow asks the server for its sessions (allSessions); nil if it
// can't tell.
func sessionsNow(p client.Paths) map[string]bool {
	c, err := client.Dial(p, proto.KindControl)
	if err != nil {
		if held, _ := lockHeld(p.Lock); !held {
			return map[string]bool{}
		}
		return nil
	}
	defer c.Close()
	var st proto.ServerStatus
	if err := c.CallWithin(callTimeout, proto.MethodServerStatus, nil, &st); err != nil {
		return nil
	}
	var list proto.SessionListResult
	if err := c.CallWithin(callTimeout, proto.MethodSessionList, nil, &list); err != nil {
		return nil
	}
	return allSessions(list, st.Sessions)
}

func readPID(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var pid int
	fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &pid)
	return pid
}
