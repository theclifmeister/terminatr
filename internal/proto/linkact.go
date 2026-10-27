package proto

// MethodLinkAct answers what one of a project's links waits on the
// user for (LinkActParams), as the human, with the checks tm link
// approve, drop, accept-file, decline-file, cancel-file, accept and
// decline make (docs/SPEC.md §7.9, §16.13): a LinkActResult. A message
// is never written through it. CapLinkAct says the server answers it.
const MethodLinkAct = "link.act"

// CapLinkAct says the server answers link.act.
const CapLinkAct = "link-act"

// link.act's actions: on a held message (its M- id), a file (its X- id)
// and a paired server's link request (its id).
const (
	LinkApprove     = "approve"
	LinkDrop        = "drop"
	LinkAcceptFile  = "accept-file"
	LinkDeclineFile = "decline-file"
	LinkCancelFile  = "cancel-file"
	LinkAccept      = "accept"
	LinkDecline     = "decline"
)

// LinkActs are link.act's actions.
var LinkActs = []string{LinkApprove, LinkDrop, LinkAcceptFile, LinkDeclineFile, LinkCancelFile, LinkAccept, LinkDecline}

// LinkActParams take Action on ID of project Project's links. Accepting
// a link request sets this side's entry to Allow (both, send, receive)
// with Ask, its ask switch; no other action takes them.
type LinkActParams struct {
	Project string `json:"project"`
	Action  string `json:"action"`
	ID      string `json:"id"`
	Allow   string `json:"allow,omitempty"`
	Ask     bool   `json:"ask,omitempty"`
}

// LinkActResult says what came of it, as the TUI's footer does.
type LinkActResult struct {
	Words string `json:"words"`
}
