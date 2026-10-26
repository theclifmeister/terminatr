package server

// pane.vt (docs/SPEC.md §3.3, Pane VT): a pane for a local client of any
// build, as remote panes stream one (internal/peer, pane.go): a repaint
// in plain VT, then output and resizes, at most proto.PaneVTWindow past
// the client's acks, as NDJSON on the control connection. The client's
// keys are the human's, as an attach's are: typed text raises a
// thread's takeover once per console.

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
)

// maxPaneVTs bounds the pane.vt streams open at once: each holds up to
// session.MaxRemotePending of backlog besides its window.
const maxPaneVTs = 32

// paneVTChunk bounds one event's data, before base64.
const paneVTChunk = 64 << 10

// A size a pane.vt client claims is taken up to these, as a paired
// server's is (peer.maxPaneCols, maxPaneRows).
const (
	maxPaneVTCols = 1000
	maxPaneVTRows = 500
)

// paneVTWindow is proto.PaneVTWindow, a variable for tests.
var paneVTWindow = proto.PaneVTWindow

// errPaneStale: the subscriber resynced while a batch was being sent;
// the rest of it is left, and the repaint that replaced it follows.
var errPaneStale = errors.New("stale")

func (s *Server) servePaneVT(c net.Conn, br *bufio.Reader, req proto.Request, peerPID int) {
	var p proto.PaneVTParams
	if perr := decodeParams(req.Params, &p); perr != nil {
		refuse(c, req, perr)
		return
	}
	if p.Session == "" {
		refuse(c, req, proto.Errorf(proto.ErrBadParams, "pane.vt needs a session"))
		return
	}
	sess, perr := s.session(p.Session)
	if perr != nil {
		refuse(c, req, perr)
		return
	}
	if n := s.paneVTs.Add(1); n > maxPaneVTs {
		s.paneVTs.Add(-1)
		refuse(c, req, proto.Errorf(proto.ErrRefused, "at most %d panes at once", maxPaneVTs))
		return
	}
	defer s.paneVTs.Add(-1)
	sub, err := sess.AttachRemote()
	if err != nil {
		refuse(c, req, sessionError(p.Session, err))
		return
	}
	info := s.info(sess)
	window := paneVTWindow
	if !answer(c, req, proto.PaneVTResult{Session: info, Window: window}) {
		sub.Detach()
		return
	}
	console := p.Console
	if console == "" {
		console = fmt.Sprintf("pane.vt %d", peerPID)
	}
	who := s.whoFor(peerPID)
	s.log.Printf("session %s: pane.vt client attached", sess.ID())

	v := &paneVT{c: c, sub: sub, window: window}
	v.cond = sync.NewCond(&v.mu)
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		defer c.Close()
		v.run()
	}()

	reason := "eof"
	for {
		var in proto.PaneVTInput
		if err := proto.ReadLine(br, &in); err != nil {
			break
		}
		if len(in.Input) > 0 {
			sess.HumanInput(in.Input, s.consoleTakeover(info, console, who))
		}
		if in.Ack > 0 {
			v.acked(in.Ack)
		}
		if z := in.Size; z != nil && z.Cols > 0 && z.Rows > 0 {
			sess.RequestResize(min(z.Cols, maxPaneVTCols), min(z.Rows, maxPaneVTRows))
		}
		if in.Repaint {
			sub.Resync()
		}
		if in.Detach {
			reason = "detach"
			break
		}
	}
	v.stop()
	sub.Detach()
	<-writerDone
	s.log.Printf("session %s: pane.vt client left (%s, %d resyncs)", sess.ID(), reason, sub.Resyncs())
}

// paneVT is one pane.vt stream's writer side.
type paneVT struct {
	c      net.Conn
	sub    *session.Subscriber
	window int

	mu       sync.Mutex
	cond     *sync.Cond
	inflight int
	done     bool
}

// run sends the subscriber's frames as events until the pane ends or the
// client leaves, keeping at most the window unacked. A batch whose frames
// went stale (the backlog became a repaint) is left unsent.
func (v *paneVT) run() {
	for {
		if !v.wait() {
			return
		}
		b, ok := v.sub.Next()
		if !ok {
			return
		}
		r0 := v.sub.Resyncs()
		for len(b) >= 5 {
			typ := proto.FrameType(b[0])
			n := int(binary.BigEndian.Uint32(b[1:5]))
			if 5+n > len(b) {
				return
			}
			p := b[5 : 5+n]
			b = b[5+n:]
			var err error
			switch typ {
			case proto.FrameRepaint:
				cols, rows, perr := proto.ParseSize(p[:min(len(p), 4)])
				if perr != nil {
					return
				}
				err = v.data(proto.PaneVTEvent{Event: proto.EventPaneRepaint, Cols: cols, Rows: rows}, p[4:], r0)
			case proto.FrameOutput:
				err = v.data(proto.PaneVTEvent{Event: proto.EventPaneOutput}, p, r0)
			case proto.FrameResize:
				if cols, rows, perr := proto.ParseSize(p); perr == nil {
					err = proto.WriteLine(v.c, proto.PaneVTEvent{Event: proto.EventPaneResize, Cols: cols, Rows: rows})
				}
			case proto.FrameClosed:
				reason := string(p)
				if reason != proto.PaneStopping {
					reason = proto.PaneExited
				}
				proto.WriteLine(v.c, proto.PaneVTEvent{Event: proto.EventPaneClosed, Reason: reason})
				return
			}
			if err == errPaneStale {
				break
			}
			if err != nil {
				return
			}
		}
	}
}

// wait waits for room in the window; false once the stream is done.
func (v *paneVT) wait() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	for v.inflight >= v.window && !v.done {
		v.cond.Wait()
	}
	return !v.done
}

// data sends ev with the first chunk of data, then output events with
// the rest, each at most paneVTChunk, waiting for the window between
// them.
func (v *paneVT) data(ev proto.PaneVTEvent, data []byte, r0 int) error {
	for first := true; first || len(data) > 0; first = false {
		if !first {
			ev = proto.PaneVTEvent{Event: proto.EventPaneOutput}
			if !v.wait() {
				return net.ErrClosed
			}
		}
		if ev.Event == proto.EventPaneOutput && v.sub.Resyncs() != r0 {
			return errPaneStale
		}
		n := min(len(data), paneVTChunk)
		ev.Data = data[:n]
		if err := proto.WriteLine(v.c, ev); err != nil {
			return err
		}
		v.mu.Lock()
		v.inflight += n
		v.mu.Unlock()
		data = data[n:]
	}
	return nil
}

// acked takes n bytes off the window.
func (v *paneVT) acked(n int) {
	v.mu.Lock()
	v.inflight = max(0, v.inflight-n)
	v.cond.Broadcast()
	v.mu.Unlock()
}

// stop ends the writer's waits: the client left.
func (v *paneVT) stop() {
	v.mu.Lock()
	v.done = true
	v.cond.Broadcast()
	v.mu.Unlock()
}
