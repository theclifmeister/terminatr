package server

// Device links (docs/SPEC.md §16.15): on its side of each paired
// device's link the server runs the app API as the device, through
// Options.DeviceAPI. The app API's control connections are pipes of the
// server's own, never a socket: nothing else can open one, and their
// caller is the device, journaled "human (iphone: <name>)".

import (
	"bufio"
	"context"
	"net"
	"time"

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/device"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// onRun, when set, is given each server Run makes (a test's way to its
// device connections).
var onRun func(*Server)

// serveDeviceLink serves device d's link conn until it or ctx ends.
func (s *Server) serveDeviceLink(ctx context.Context, conn net.Conn, d device.Device, level *device.Level) error {
	if s.opts.DeviceAPI == nil {
		<-ctx.Done()
		return nil
	}
	return s.opts.DeviceAPI(ctx, conn, d, level, func() (*client.Client, error) { return s.deviceConn(d.Name) }, s.log.Printf)
}

// deviceConn is a control connection of the device name, served here.
func (s *Server) deviceConn(name string) (*client.Client, error) {
	ours, theirs := net.Pipe()
	go s.handleDevice(ours, name)
	c, err := client.Over(theirs, proto.ClientDevice)
	if err != nil {
		theirs.Close()
		return nil, err
	}
	return c, nil
}

// handleDevice runs one of device name's connections: the handshake,
// then its requests as a human control connection whose caller is the
// device. Its rights past the human's gates are the device's level's,
// which its app API enforces before a request gets here.
func (s *Server) handleDevice(c net.Conn, name string) {
	defer c.Close()
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return
	}
	s.conns[c] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.conns, c)
		s.mu.Unlock()
	}()
	br := bufio.NewReader(c)
	c.SetReadDeadline(time.Now().Add(handshakeTimeout))
	var hello proto.Hello
	if err := proto.ReadLine(br, &hello); err != nil {
		return
	}
	c.SetReadDeadline(time.Time{})
	reply := s.hello()
	if err := proto.WriteLine(c, reply); err != nil {
		return
	}
	if err := proto.Check(hello, reply); err != nil || hello.Kind != proto.KindControl {
		return
	}
	s.serveControl(c, br, 0, true, true, name)
}
