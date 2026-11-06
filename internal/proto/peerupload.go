package proto

// Attaching a file to a paired server's session (docs/SPEC.md §16.12): the
// server that runs the session stores the file as session.upload does for its
// own, under the same checks, while its pane switch for the caller is write,
// read at each request. The calling server only forwards. Nothing is on disk
// until the last byte is in, and a link that drops takes the bytes with it.

const (
	// MethodPeerSessionUpload takes a chunk of a file for a session's prompt.
	MethodPeerSessionUpload = "peer.session-upload"
	// MethodPeerSessionFiles answers the paths of files a prompt names, in
	// that session's project's uploads/ on the server that runs it: what its
	// agent reads them by.
	MethodPeerSessionFiles = "peer.session-files"
	// PeerUploadChunk bounds a chunk on a link: a line this short holds the
	// link's writer only briefly, so a pane's frames go between two chunks.
	PeerUploadChunk = 32 << 10
)

// PeerSessionUploadParams are a chunk of a file for the Session of the shared
// Project: as SessionUploadParams, at most PeerUploadChunk bytes of Data.
type PeerSessionUploadParams struct {
	Project string `json:"project"`
	Session string `json:"session"`
	Upload  string `json:"upload,omitempty"`
	Name    string `json:"name,omitempty"`
	Size    int64  `json:"size,omitempty"`
	Offset  int64  `json:"offset,omitempty"`
	Data    []byte `json:"data,omitempty"`
}

// PeerSessionUpload is where an upload stands, as SessionUploadResult: Ref,
// Type and Size once all its bytes are in and stored.
type PeerSessionUpload struct {
	Upload   string `json:"upload"`
	Received int64  `json:"received"`
	Ref      string `json:"ref,omitempty"`
	Type     string `json:"type,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// PeerSessionFilesParams name files of the Session's project's uploads/ by
// their refs.
type PeerSessionFilesParams struct {
	Project string   `json:"project"`
	Session string   `json:"session"`
	Files   []string `json:"files"`
}

// PeerSessionFiles are the refs' paths, in their order.
type PeerSessionFiles struct {
	Paths []string `json:"paths"`
}
