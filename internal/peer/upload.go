package peer

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// Files for a shared session's prompt (docs/SPEC.md §16.12). The server that
// runs the session holds every rule (internal/server/peerupload.go); here,
// the pane switch read at each request, and on the side that calls, the pace
// that leaves the link to the panes.

// CapSessionUpload is the link capability of peer.session-upload and
// peer.session-files.
const CapSessionUpload = "session-upload"

// ErrNoUploads means the paired server's tm takes no files for its sessions
// (its hello doesn't list CapSessionUpload).
var ErrNoUploads = errors.New("the paired server's tm takes no files for its sessions")

// UploadExporter is an Exporter whose sessions take files from a paired
// server. The link checked the pane switch already.
type UploadExporter interface {
	// SessionUpload takes a chunk. gone ends with the link: an upload still
	// in memory then is dropped.
	SessionUpload(gone context.Context, c caller.Caller, p proto.PeerSessionUploadParams) (proto.PeerSessionUpload, error)
	// SessionFiles answers the paths of files in the session's uploads/.
	SessionFiles(c caller.Caller, p proto.PeerSessionFilesParams) (proto.PeerSessionFiles, error)
	// DropUploads forgets what c has in memory: its pane switch went off.
	DropUploads(c caller.Caller)
}

// errUploadsOff refuses while the pane switch isn't write.
var errUploadsOff = proto.Errorf(proto.ErrPanesOff, "this server doesn't let yours write to its sessions")

// uploadMethods are the link's methods of a session's files, when the
// Exporter takes them.
func (d *dispatcher) uploadMethods(m map[string]handler) {
	ex, ok := d.ex.(UploadExporter)
	if !ok || d.uploads == nil {
		return
	}
	m[proto.MethodPeerSessionUpload] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		if !d.uploads() {
			ex.DropUploads(d.caller)
			return nil, errUploadsOff
		}
		var p proto.PeerSessionUploadParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, proto.Errorf(proto.ErrBadParams, "peer.session-upload: bad params")
		}
		if len(p.Data) > proto.PeerUploadChunk {
			return nil, proto.Errorf(proto.ErrBadParams, "peer.session-upload: a chunk is at most %d bytes", proto.PeerUploadChunk)
		}
		return ex.SessionUpload(ctx, d.caller, p)
	}
	m[proto.MethodPeerSessionFiles] = func(_ context.Context, raw json.RawMessage) (any, error) {
		if !d.uploads() {
			ex.DropUploads(d.caller)
			return nil, errUploadsOff
		}
		var p proto.PeerSessionFilesParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, proto.Errorf(proto.ErrBadParams, "peer.session-files: bad params")
		}
		return ex.SessionFiles(d.caller, p)
	}
}

// panesWrite says whether this server's pane switch for peer id is write now,
// read from the file each time: a link's copy can lag a change, and off must
// hold at the next request. A file that can't be read is off.
func (m *Manager) panesWrite(id ID) bool {
	peers, err := LoadPeers(m.opts.Dir)
	if err != nil {
		return false
	}
	i := slices.IndexFunc(peers, func(q Peer) bool { return q.ID() == id })
	return i >= 0 && peers[i].PanesLevel() == proto.PanesType
}

// The pace of a link's uploads: a relay meters a room's bytes, and over its
// rate holds them all, the panes' too. Base64 makes 4 bytes of 3, so the
// fastest pace stays under a relay's default 2 MiB/s. It backs off as file
// streams do when answers take fileSlowRTT longer than the link's idle round
// trip.
var uploadRate float64 = 1 << 20

const (
	// uploadWait bounds one chunk's answer.
	uploadWait = 60 * time.Second
	// maxUploadPath bounds a path a paired server answers.
	maxUploadPath = 4 << 10
)

// uploadPace is a link's: every file going over it shares one pace. Its zero
// value is ready.
type uploadPace struct {
	mu   sync.Mutex
	rate float64
	next time.Time
}

// wait holds a chunk of n bytes for its place.
func (u *uploadPace) wait(ctx context.Context, n int) error {
	u.mu.Lock()
	now := time.Now()
	if u.rate == 0 {
		u.rate = uploadRate
	}
	at := u.next
	if at.Before(now) {
		at = now
	}
	u.next = at.Add(time.Duration(float64(n) / u.rate * float64(time.Second)))
	u.mu.Unlock()
	d := time.Until(at)
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// took takes a chunk's round trip, against the link's idle one: halved when
// slow, down to fileMinRate; up by a little on each that isn't.
func (u *uploadPace) took(rtt, idle time.Duration) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if rtt > idle+fileSlowRTT {
		u.rate = max(u.rate/2, fileMinRate)
		return
	}
	u.rate = min(u.rate*1.05, uploadRate)
}

// uploadIDPattern is an upload's id as a paired server may answer it.
var uploadIDPattern = regexp.MustCompile(`^[0-9a-f]{1,64}$`)

// uploadsLink is the link to alias, up and with uploads; the second result
// says why not.
func (m *Manager) uploadsLink(alias string) (*link, error) {
	if m == nil {
		return nil, errNotUp
	}
	l := m.link(alias)
	if l == nil {
		return nil, proto.Errorf(proto.ErrRefused, "no paired server %q", alias)
	}
	st := l.status()
	if st.State != proto.LinkUp {
		return nil, errNotUp
	}
	if !slices.Contains(st.Caps, CapSessionUpload) {
		return nil, ErrNoUploads
	}
	return l, nil
}

// SessionUpload sends p, one chunk of session.upload's, over the link to the
// paired server alias in chunks of at most proto.PeerUploadChunk, one at a
// time, and answers where the upload stands there. ErrNoUploads for a tm
// without them; an error that isn't a *proto.Error means the link dropped.
func (m *Manager) SessionUpload(ctx context.Context, alias string, p proto.PeerSessionUploadParams) (proto.PeerSessionUpload, error) {
	var res proto.PeerSessionUpload
	l, err := m.uploadsLink(alias)
	if err != nil {
		return res, err
	}
	rest := p.Data
	for {
		n := min(len(rest), proto.PeerUploadChunk)
		q := p
		q.Data = rest[:n]
		if err := l.uploads.wait(ctx, n); err != nil {
			return res, err
		}
		cctx, cancel := context.WithTimeout(ctx, uploadWait)
		at := time.Now()
		var r proto.PeerSessionUpload
		err := l.call(cctx, proto.MethodPeerSessionUpload, q, &r)
		cancel()
		if err != nil {
			return res, err
		}
		if s := l.sessionNow(); s != nil {
			l.uploads.took(time.Since(at), s.idleRTT())
		}
		rest = rest[n:]
		if !uploadIDPattern.MatchString(r.Upload) || r.Received != p.Offset+int64(n) || (r.Ref != "" && len(rest) > 0) {
			return res, proto.Errorf(proto.ErrRefused, "%s: its tm answered oddly: attach the file again", alias)
		}
		res = r
		if len(rest) == 0 {
			res.Type = textsafe.Line(res.Type, 40)
			return res, nil
		}
		p.Upload, p.Name, p.Size, p.Offset = r.Upload, "", 0, r.Received
	}
}

// SessionFiles asks the paired server alias for the paths of the files named
// in its session's uploads/, cleaned: one for each, plain text, ending in its
// ref.
func (m *Manager) SessionFiles(ctx context.Context, alias string, p proto.PeerSessionFilesParams) ([]string, error) {
	l, err := m.uploadsLink(alias)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, uploadWait)
	defer cancel()
	var r proto.PeerSessionFiles
	if err := l.call(ctx, proto.MethodPeerSessionFiles, p, &r); err != nil {
		return nil, err
	}
	if len(r.Paths) != len(p.Files) {
		return nil, proto.Errorf(proto.ErrRefused, "%s: its tm answered oddly: attach the files again", alias)
	}
	for i, path := range r.Paths {
		if !cleanUploadPath(path, p.Files[i]) {
			return nil, proto.Errorf(proto.ErrRefused, "%s: its tm answered oddly: attach the files again", alias)
		}
	}
	return r.Paths, nil
}

// cleanUploadPath says whether path, as a paired server answered it, is fit
// to type: one line of plain text, of a bounded length, ending in the ref.
func cleanUploadPath(path, ref string) bool {
	n := len(path) - len(ref) - 1
	return n > 0 && len(path) <= maxUploadPath && textsafe.Clean(path, false) == path &&
		path[n+1:] == ref && (path[n] == '/' || path[n] == '\\')
}
