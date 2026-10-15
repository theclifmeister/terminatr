package client

import "github.com/theclifmeister/terminatr/internal/proto"

// ProjectWatchStream is a project watch (project.watch).
type ProjectWatchStream struct{ c *Client }

// WatchProject watches project slug and returns the stream and the
// project's state now.
func WatchProject(p Paths, slug string) (*ProjectWatchStream, proto.ProjectWatch, error) {
	c, err := Dial(p, proto.KindControl)
	if err != nil {
		return nil, proto.ProjectWatch{}, err
	}
	var w proto.ProjectWatch
	if err := c.Call(proto.MethodProjectWatch, proto.ProjectWatchParams{Project: slug}, &w); err != nil {
		c.Close()
		return nil, proto.ProjectWatch{}, err
	}
	return &ProjectWatchStream{c: c}, w, nil
}

// Next blocks until the project's next state.
func (s *ProjectWatchStream) Next() (proto.ProjectWatch, error) {
	for {
		var ev proto.ProjectWatchEvent
		if err := s.c.ReadLine(&ev); err != nil {
			return proto.ProjectWatch{}, err
		}
		if ev.Event == proto.EventProjectChanged {
			return ev.Watch, nil
		}
	}
}

// Close ends the watch.
func (s *ProjectWatchStream) Close() error { return s.c.Close() }
