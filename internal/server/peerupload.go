package server

// Files a paired server attaches to a shared session's prompt (docs/SPEC.md
// §16.12). The calling server is only a caller: the link read the pane switch
// at this request (internal/peer); here, the caller, that the project is
// shared, the session, then session.upload's own checks and limits with the
// paired server's shares of them (upload.go). Nothing reaches the disk until
// the last byte is in, and the file goes into that project's uploads/ alone.

import (
	"context"
	"slices"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// uploadSession holds a request to the rules up to the file: a paired server,
// a shared project, and its coordinator or a thread running. Any other session
// answers as one that isn't there.
func (e peerExport) uploadSession(c caller.Caller, slug, id string) (proto.SessionInfo, error) {
	var none proto.SessionInfo
	if c.Kind != caller.Peer || c.Alias == "" {
		return none, proto.Errorf(proto.ErrRefused, "these are a paired server's")
	}
	if !slices.Contains(sharedProjects(), slug) {
		return none, errNotShared(slug)
	}
	// Nothing chains through to a third server.
	if _, _, remote := remoteID(id); remote {
		return none, proto.Errorf(proto.ErrUnknownSession, "no session %q in %s", id, slug)
	}
	info, ok := e.sessionInfo(id)
	if !ok || info.Project != slug || (info.Role != proto.RoleCoordinator && info.Role != proto.RoleThread) {
		return none, proto.Errorf(proto.ErrUnknownSession, "no session %q in %s", id, slug)
	}
	return info, nil
}

// SessionUpload takes a chunk of a file for a shared session's prompt.
func (e peerExport) SessionUpload(gone context.Context, c caller.Caller, p proto.PeerSessionUploadParams) (proto.PeerSessionUpload, error) {
	var none proto.PeerSessionUpload
	info, err := e.uploadSession(c, p.Project, p.Session)
	if err != nil {
		e.uploads.uploads.dropPeer(c.Alias)
		return none, err
	}
	r, perr := e.uploads.takeChunk(info, proto.SessionUploadParams{Session: p.Session, Upload: p.Upload, Name: p.Name, Size: p.Size, Offset: p.Offset, Data: p.Data}, c, c.Alias, gone)
	if perr != nil {
		return none, perr
	}
	return peerUploadOf(r), nil
}

// peerUploadOf is an upload's standing as a paired server may read it, field
// by field: a field added to SessionUploadResult stays home until it is added
// here (peerupload_test.go fails until it is decided).
func peerUploadOf(r proto.SessionUploadResult) proto.PeerSessionUpload {
	return proto.PeerSessionUpload{Upload: r.Upload, Received: r.Received, Ref: r.Ref, Type: r.Type, Size: r.Size}
}

// SessionFiles answers the paths of files in a shared session's uploads/,
// which its agent reads them by.
func (e peerExport) SessionFiles(c caller.Caller, p proto.PeerSessionFilesParams) (proto.PeerSessionFiles, error) {
	var none proto.PeerSessionFiles
	info, err := e.uploadSession(c, p.Project, p.Session)
	if err != nil {
		return none, err
	}
	dir, derr := sessionDir(info)
	if derr != nil {
		return none, proto.Errorf(proto.ErrRefused, "session %s takes no files", p.Session)
	}
	paths, perr := promptPaths(dir, p.Files)
	if perr != nil {
		return none, perr
	}
	return proto.PeerSessionFiles{Paths: paths}, nil
}

// DropUploads forgets what the paired server c has in memory.
func (e peerExport) DropUploads(c caller.Caller) { e.uploads.uploads.dropPeer(c.Alias) }
