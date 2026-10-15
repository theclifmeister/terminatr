package relay

// An update request (docs/SPEC.md §16.2): a server whose link through
// the relay is live asks it to update itself to the latest official
// release. The relay stays unprivileged: it only writes a request file
// in its state directory, at most once per UpdateEvery from any key and
// once per UpdateKeyEvery from one key, and only for a key whose link
// room has been mated UpdateAfter with bytes sent both ways; a root systemd
// path unit (tm-relay-update.path, from tm relay install) sees the file
// and runs tm relay update, which removes it, checks the release's
// checksums and restarts the relay only for a newer release. A request
// carries nothing but the wish: no version, no URL.

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Update results, the "result" of the relay's answer.
const (
	UpdateQueued    = "queued"     // the request file is written
	UpdateOff       = "off"        // this relay takes no requests (no --update-request)
	UpdateNotLinked = "not-linked" // the key has no live link through this relay
	UpdateTooSoon   = "too-soon"   // asked too soon (see update); Next says when
	UpdateFailed    = "failed"     // the request file couldn't be written
)

// updateReply is the relay's answer to an update hello, before it
// closes the connection.
type updateReply struct {
	T      string    `json:"t"`
	Result string    `json:"result"`
	Next   time.Time `json:"next,omitzero"` // too-soon: the first time another is taken
}

// updateLast is the file next to the request file whose time is the
// last request taken: the limit holds across the restart it causes.
func updateLast(file string) string { return file + ".last" }

// lastUpdate is when the last request was taken, as the file says.
func lastUpdate(file string) time.Time {
	if file == "" {
		return time.Time{}
	}
	fi, err := os.Stat(updateLast(file))
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

// update answers an update hello: the key must be one side of a link
// room that is mated now, the room the one it signed, with bytes sent
// both ways. It is too soon until the room has been mated UpdateAfter,
// UpdateKeyEvery after this key's last request taken and UpdateEvery
// after anyone's.
func (s *Server) update(c *conn) string {
	r := updateReply{T: msgUpdate}
	k := roomKey{KindLink, c.chk.room}
	now := s.now()
	s.mu.Lock()
	m := s.busy[k]
	linked := m != nil && (m.a.chk.fp == c.chk.fp || m.b.chk.fp == c.chk.fp) &&
		m.a.in.Load() > 0 && m.b.in.Load() > 0
	var next time.Time
	if linked {
		next = m.since.Add(s.cfg.UpdateAfter)
		if t, ok := s.keyUpd[c.chk.fp]; ok {
			next = later(next, t.Add(s.cfg.UpdateKeyEvery))
		}
		if !s.updated.IsZero() {
			next = later(next, s.updated.Add(s.cfg.UpdateEvery))
		}
	}
	switch {
	case !linked:
		r.Result = UpdateNotLinked
	case s.cfg.UpdateFile == "":
		r.Result = UpdateOff
	case now.Before(next):
		r.Result, r.Next = UpdateTooSoon, next
	default:
		if err := writeUpdateRequest(s.cfg.UpdateFile, now); err != nil {
			s.logf("update request: %v", err)
			r.Result = UpdateFailed
		} else {
			s.updated, r.Result = now, UpdateQueued
			s.keyUpd[c.chk.fp] = now
		}
	}
	s.mu.Unlock()
	s.logf("%supdate request from key %s: %s", c.who(), hex.EncodeToString(c.chk.fp[:4]), r.Result)
	c.send(r)
	c.closeWith(CloseNormal, "")
	return ""
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// writeUpdateRequest writes the request file (the path unit's trigger)
// and stamps the last-request file.
func writeUpdateRequest(file string, now time.Time) error {
	b := []byte(fmt.Sprintf("update to the latest release\nrequested %s\n", now.UTC().Format(time.RFC3339)))
	tmp, err := os.CreateTemp(filepath.Dir(file), ".update-*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(b)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), file)
	}
	if err != nil {
		os.Remove(tmp.Name())
		return err
	}
	last := updateLast(file)
	if err := os.WriteFile(last, nil, 0o600); err != nil {
		return err
	}
	return os.Chtimes(last, now, now)
}

// parseUpdateReply reads the relay's answer to an update hello.
func parseUpdateReply(op byte, msg []byte) (updateReply, bool) {
	var r updateReply
	if op != opText || json.Unmarshal(msg, &r) != nil || r.T != msgUpdate || r.Result == "" {
		return r, false
	}
	return r, true
}
