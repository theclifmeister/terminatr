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
)

// answerMethods are what the answer level adds to view's reads: answers,
// approvals (never an escape's: the server refuses those to devices),
// a task's accept and send-back, the coordinator's questions.
var answerMethods = []string{proto.MethodSessionAnswer, proto.MethodSessionApprove, proto.MethodTaskAccept,
	proto.MethodTaskSendBack, proto.MethodQuestionsOpen}

// Allowed says whether a bridge at device level may call m; "" is the
// Mac app's, which may call every method. view reads (every method
// that isn't Write, the notice tray's too), answer adds answerMethods,
// write everything; an unknown level is view's.
func Allowed(m Method, level string) bool {
	switch {
	case level == "" || level == device.LevelWrite || !m.Write:
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
// dial opens the server's own control connections as the device.
func ServeDevice(ctx context.Context, conn net.Conn, d device.Device, level *device.Level, dial func() (*client.Client, error)) error {
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	defer conn.Close()
	return Serve(conn, conn, Options{Dial: dial, Level: level.Get, MaxStreams: MaxDeviceStreams})
}
