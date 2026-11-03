package peer

// Programs over the links (the programs design §3.5, §9.2): a server's
// hello lists CapProgram only while its [features] programs is on, so a
// paired server sends it program messages only then. The server's
// switch reaches the links at once (SetPrograms): each link that is up
// says its hello again, by dialling again.

// CapProgram is the link capability of the program message kinds
// (proto.IsProgramKind) on peer.message: listed only while the server's
// programs are on.
const CapProgram = "program"

// SetPrograms turns CapProgram on or off in the hellos. A link whose
// hello changes and is up dials again, so its peer reads the new one;
// one that isn't says it at its next dial. Saying the same again
// changes nothing.
func (m *Manager) SetPrograms(on bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.opts.Programs == on {
		return
	}
	m.opts.Programs = on
	caps := m.caps()
	for _, r := range m.links {
		r.l.setCaps(caps)
	}
}

// Programs says whether the hellos list CapProgram.
func (m *Manager) Programs() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.opts.Programs
}

// setCaps changes the capabilities the hello lists; a session that is
// up ends, so the link dials again and says them.
func (l *link) setCaps(caps []string) {
	l.mu.Lock()
	l.hello.Caps = caps
	s := l.sess
	l.mu.Unlock()
	if s != nil {
		s.c.Close()
	}
}
