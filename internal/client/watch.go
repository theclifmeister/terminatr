package client

import "github.com/theclifmeister/terminatr/internal/proto"

// OpenStream starts a stream method (view.subscribe, session.watch, …):
// it dials the control socket and calls method, whose answer goes into
// res; the stream's events then follow on the connection (NextEvent).
func OpenStream(p Paths, method string, params, res any) (*Client, error) {
	c, err := Dial(p, proto.KindControl)
	if err != nil {
		return nil, err
	}
	if err := c.Call(method, params, res); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// NextEvent reads a stream's lines, each into a fresh E, until keep says
// one is the caller's, and returns it.
func NextEvent[E any](c *Client, keep func(E) bool) (E, error) {
	for {
		var ev E
		if err := c.ReadLine(&ev); err != nil {
			var zero E
			return zero, err
		}
		if keep(ev) {
			return ev, nil
		}
	}
}

// ProjectWatchStream is a project watch (project.watch).
type ProjectWatchStream struct{ c *Client }

// WatchProject watches project slug and returns the stream and the
// project's state now.
func WatchProject(p Paths, slug string) (*ProjectWatchStream, proto.ProjectWatch, error) {
	var w proto.ProjectWatch
	c, err := OpenStream(p, proto.MethodProjectWatch, proto.ProjectWatchParams{Project: slug}, &w)
	if err != nil {
		return nil, proto.ProjectWatch{}, err
	}
	return &ProjectWatchStream{c: c}, w, nil
}

// Next blocks until the project's next state.
func (s *ProjectWatchStream) Next() (proto.ProjectWatch, error) {
	ev, err := NextEvent(s.c, func(ev proto.ProjectWatchEvent) bool { return ev.Event == proto.EventProjectChanged })
	return ev.Watch, err
}

// Close ends the watch.
func (s *ProjectWatchStream) Close() error { return s.c.Close() }
