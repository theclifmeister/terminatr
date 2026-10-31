package appapi

// The app API on a paired device's link (docs/SPEC.md §16.15): the
// server runs this bridge on its side of each link, as the device, so
// the trust boundary is the server's, never the phone's. A device's
// level (device.LevelView …) says which methods it has; its hello lists
// only those, and a request for another answers ErrLevel.

import (
	"context"
	"net"
	"slices"

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/device"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// answerMethods are what the answer level adds to view's reads: answers,
// approvals (never an escape's: the server refuses those to devices),
// a task's accept and send-back, the coordinator's questions, and the
// same on a paired server's needs (peer.need-act). Each is signed
// (sign.go).
var answerMethods = []string{proto.MethodSessionAnswer, proto.MethodSessionApprove, proto.MethodTaskAccept,
	proto.MethodTaskSendBack, proto.MethodQuestionsOpen, proto.MethodPeerNeedAct}

// Allowed says whether a bridge at device level may call m; "" is the
// Mac app's, which may call every method but the devices' own
// (device.nonce, which only the levels that sign have). No level has a
// method the bridge answers in its own process (Local: the guard's
// refusals, whose log and answers are the human's on a computer, and
// change the access rules): on a device's link that process is the
// server. Otherwise view reads (every method that isn't Write, the
// notice tray's too), answer adds answerMethods, write everything; an
// unknown level is view's.
func Allowed(m Method, level string) bool {
	switch {
	case m.Name == MethodDeviceBye:
		return level != ""
	case m.device:
		return signs(level)
	case level == "":
		return true
	case m.Local():
		return false
	case level == device.LevelWrite || !m.Write:
		return true
	case level == device.LevelAnswer:
		return slices.Contains(answerMethods, m.Name)
	}
	return false
}

// allowsWrite says whether level lists CapWrite: the Mac app's and
// write.
func allowsWrite(level string) bool { return level == "" || level == device.LevelWrite }

// ServeDevice is the app API for device d over conn, its link past TLS,
// at level, until conn or ctx ends: the server's Options.DeviceAPI.
// dial opens the server's own control connections as the device. Its
// writes need d's write key's signature (sign.go). Its lines are at
// most MaxLine bytes, what the phone reads (Options.MaxOut); what
// wasn't sent for it goes to logf, as "device <name>: …". It ends with
// why: the device closed the link (a ByeError, with the reason
// device.bye gave), or what broke.
func ServeDevice(ctx context.Context, conn net.Conn, d device.Device, level *device.Level, dial func() (*client.Client, error), logf func(string, ...any)) error {
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	defer conn.Close()
	var bye *string
	o := Options{Dial: dial, Level: level.Get, MaxStreams: MaxDeviceStreams, WriteKey: d.Verifier(), MaxOut: MaxLine, onBye: func(r string) { bye = &r }}
	if logf != nil {
		o.Logf = func(format string, a ...any) { logf("device %s: "+format, append([]any{d.Name}, a...)...) }
	}
	err := Serve(conn, conn, o)
	if ctx.Err() != nil {
		return err
	}
	switch {
	case bye != nil:
		return &ByeError{Reason: *bye}
	case err == nil:
		// in ended without a word: the phone's TLS closed, or the relay
		// ended the room
		return &ByeError{}
	}
	return err
}

// MethodDeviceBye tells the server why the device closes its link, just
// before it does (ByeParams; app API 37): the server logs it with the
// close. A device's bridge's own, at every level.
const MethodDeviceBye = "device.bye"

// ByeParams are device.bye's: Reason in the device's words (the app
// went to the back, the network changed), at most 200 characters.
type ByeParams struct {
	Reason string `json:"reason"`
}

// ByeError is how a device's link ended when the device closed it:
// with the reason device.bye gave, if it gave one.
type ByeError struct{ Reason string }

func (e *ByeError) Error() string {
	if e.Reason == "" {
		return "the device closed the link"
	}
	return "the device closed the link: " + e.Reason
}

// deviceBye answers device.bye: the reason is kept for the close.
func deviceBye(b *bridge, p ByeParams) (Empty, error) {
	r := textsafe.Line(p.Reason, 200)
	if b.o.onBye != nil {
		b.o.onBye(r)
	}
	return Empty{}, nil
}
