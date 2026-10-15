package server

import (
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// whoIs answers caller.who (docs/SPEC.md §11.1) for
// commands the CLI runs itself: the human's, and an agent's whose
// session variables are gone, which the CLI then can't forward.
func (s *Server) whoIs(c caller.Caller) proto.CallerInfo {
	return proto.CallerInfo{Kind: string(c.Kind), Project: c.Project, Thread: c.Thread}
}
