package proto

// The sidebar's and the sessions' acts a console makes as the human
// beside the TUI's own keys (docs/SPEC.md §3.8): a project pinned (P), a
// project made (+), an outside agent session offered to a coordinator
// to adopt (O), a message written on a project's link (M). Each is the
// human's, with the checks the TUI's make, journaled as the caller.
// CapSideActs says the server answers them.
const (
	// MethodProjectPin pins a project to the top of the sidebar's
	// active projects, or unpins it (ProjectPinParams): its order only,
	// kept in its settings. Result ProjectPinResult.
	MethodProjectPin = "project.pin"
	// MethodProjectNew makes a project (ProjectNewParams), as tm
	// project new does: refusals carry the project store's codes
	// ("project-exists", "invalid-slug", "invalid-repo"). Result
	// ProjectNewResult.
	MethodProjectNew = "project.new"
	// MethodSessionAdoptRequest asks a project's coordinator to adopt an
	// agent session outside the projects as a thread (O on the
	// dashboard): the human's go-ahead (session.adopt.ask) and the
	// inbox item that tells the coordinator, or with Withdraw both taken
	// back. Params SessionAdoptRequestParams, result
	// SessionAdoptAskResult (false when an item asks it already).
	MethodSessionAdoptRequest = "session.adopt.request"
	// MethodLinkSend writes a message as the user on one of a
	// project's links (LinkSendParams), with tm link send's checks.
	// Result LinkActResult.
	MethodLinkSend = "link.send"
)

// CapSideActs says the server answers project.pin, project.new,
// session.adopt.request and link.send.
const CapSideActs = "side-acts"

// ProjectPinParams are the params of project.pin.
type ProjectPinParams struct {
	Project string `json:"project"`
	Pinned  bool   `json:"pinned"`
}

// ProjectPinResult says whether the setting changed.
type ProjectPinResult struct {
	Changed bool `json:"changed"`
}

// ProjectNewParams are the params of project.new: its name, which
// becomes a lower-case slug, and the folders of its repos, if any.
type ProjectNewParams struct {
	Project string   `json:"project"`
	Repos   []string `json:"repos,omitempty"`
}

// ProjectNewResult is the project's slug, and whether no agent is
// installed to run its coordinator.
type ProjectNewResult struct {
	Project string `json:"project"`
	NoAgent bool   `json:"no_agent,omitempty"`
}

// SessionAdoptRequestParams are the params of session.adopt.request:
// the agent session and the project to adopt it into.
type SessionAdoptRequestParams struct {
	ID       string `json:"id"`
	Project  string `json:"project"`
	Withdraw bool   `json:"withdraw,omitempty"`
}

// LinkSendParams are the params of link.send: project's message to
// Peer, a project it is linked with (a paired server's as
// "<alias>/<slug>").
type LinkSendParams struct {
	Project string `json:"project"`
	Peer    string `json:"peer"`
	Text    string `json:"text"`
}
