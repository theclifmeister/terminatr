package peer

import (
	"context"
	"encoding/json"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// Coordinator links across servers (docs/SPEC.md §16.11): a paired
// server delivers its projects' messages and receipts with
// peer.message, which the server hands to Messages. The message's text
// is data for the receiving coordinator, never for the link.

// CapMsg is the link capability of peer.message.
const CapMsg = "msg"

// Messages takes a paired server's messages. c is the paired server,
// caller kind peer; id its server id.
type Messages interface {
	Message(c caller.Caller, id ID, m proto.PeerMessage) (proto.PeerMessageResult, error)
}

func (d *dispatcher) message(ctx context.Context, raw json.RawMessage) (any, error) {
	var m proto.PeerMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "peer.message: %v", err)
	}
	return d.msgs.Message(d.caller, d.peer, m)
}
