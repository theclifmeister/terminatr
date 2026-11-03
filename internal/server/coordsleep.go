package server

// Coordinator sleep (docs/SPEC.md §7.5, Sleep): the ticker stops a
// coordinator idle for its project's coordinator_sleep_after, with
// nothing waiting on it and nobody watching, into a dormant record as a
// deactivation does, but marked Asleep and with its project still
// active. It stays asleep across server starts; enter on its row, an
// app opening it (project.open), tm program wake and a new inbox item
// (the ticker) resume it under its id, its conversation kept. A
// director never sleeps.

import (
	"fmt"
	"sort"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// sleepCoordinator stops coordinator session id into an Asleep dormant
// record, once still says yes to its info right before the stop (the
// ticker's conditions, checked again: a console may have attached).
// Refused for anything but a running, project's coordinator with a
// conversation to resume.
func (s *Server) sleepCoordinator(id string, still func(proto.SessionInfo) bool) error {
	s.mu.Lock()
	sess, ok := s.sessions[id]
	r := s.records[id]
	switch {
	case !ok:
		s.mu.Unlock()
		return fmt.Errorf("no session %s", id)
	case r.Role != proto.RoleCoordinator || r.Project == "":
		s.mu.Unlock()
		return fmt.Errorf("session %s is no project's coordinator", id)
	case r.Agent == "" || r.AgentSessionID == "":
		s.mu.Unlock()
		return fmt.Errorf("session %s has no conversation to resume", id)
	case s.stopping || sess.ExitStatus() != "" || s.sleeping[id]:
		s.mu.Unlock()
		return fmt.Errorf("session %s is stopping", id)
	}
	s.mu.Unlock()
	// info takes locks of its own.
	if still != nil && !still(s.info(sess)) {
		return fmt.Errorf("session %s is no longer idle and unwatched", id)
	}
	s.mu.Lock()
	if s.stopping || sess.ExitStatus() != "" || s.sleeping[id] {
		s.mu.Unlock()
		return fmt.Errorf("session %s is stopping", id)
	}
	s.sleeping[id], s.asleep[id] = true, true
	s.mu.Unlock()
	sess.Stop(StopGrace)
	s.log.Printf("session %s: coordinator of %s asleep", id, r.Project)
	return nil
}

// asleepList is the sleeping coordinators, by project.
func (s *Server) asleepList() []proto.AsleepCoordinator {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []proto.AsleepCoordinator
	for _, r := range s.dormant {
		if r.Asleep && r.Role == proto.RoleCoordinator {
			out = append(out, proto.AsleepCoordinator{Project: r.Project, ID: r.ID, Since: r.Dormant})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Project < out[j].Project })
	return out
}

// wakeCoordinator resumes project slug's sleeping coordinator under its
// id, its conversation kept, and journals coordinator.wake as c with
// why; ok is false when it has none asleep (or it couldn't be resumed:
// the caller starts one anew). A record found beside a running
// coordinator, or for an inactive project, isn't resumed: the first is
// dropped, the second kept for the project's activation.
func (s *Server) wakeCoordinator(slug string, c caller.Caller, why string) (id string, ok bool) {
	s.wakeMu.Lock()
	defer s.wakeMu.Unlock()
	s.mu.Lock()
	var rec SessionRecord
	for _, r := range s.dormant {
		if r.Asleep && r.Role == proto.RoleCoordinator && r.Project == slug {
			rec = r
			break
		}
	}
	if rec.ID == "" {
		s.mu.Unlock()
		return "", false
	}
	if !projectActive(slug) {
		s.mu.Unlock()
		return "", false
	}
	s.mu.Unlock()
	live := s.liveCoordinator(slug)
	s.mu.Lock()
	delete(s.dormant, rec.ID)
	if live != "" {
		s.log.Printf("session %s: asleep coordinator of %s dropped: %s runs", rec.ID, slug, live)
		if err := s.saveLocked(""); err != nil {
			s.log.Printf("sessions.json: %v", err)
		}
		s.mu.Unlock()
		return "", false
	}
	s.mu.Unlock()
	if gone := dormantGone(rec); gone != "" {
		s.log.Printf("session %s: asleep coordinator dropped: %s", rec.ID, gone)
		return "", false
	}
	o := s.resumeOne(rec)
	s.mu.Lock()
	if err := s.saveLocked(""); err != nil {
		s.log.Printf("sessions.json: %v", err)
	}
	s.mu.Unlock()
	if o.how == "lost" {
		s.log.Printf("session %s: asleep coordinator of %s not resumed", rec.ID, slug)
		return "", false
	}
	if p, err := project.Open(slug); err == nil {
		if err := p.Journal(c, "coordinator.wake", rec.ID, why); err != nil {
			s.log.Printf("coordinator.wake %s: journal: %v", slug, err)
		}
	}
	s.log.Printf("session %s: coordinator of %s woke (%s)", rec.ID, slug, why)
	s.watch.wake()
	return rec.ID, true
}

// startCoordinator is session.start for a project's coordinator: its
// sleeping one woken (as c, why "opened"), else one already running
// (a wake that raced the caller's list), else a new one.
func (s *Server) startCoordinator(p proto.SessionStartParams, c caller.Caller) (any, *proto.Error) {
	if p.ResumeSID == "" && len(p.Argv) == 0 && p.Project != "" {
		if id, ok := s.wakeCoordinator(p.Project, c, "opened"); ok {
			return s.startedResult(id)
		}
		if id := s.liveCoordinator(p.Project); id != "" {
			return s.startedResult(id)
		}
	}
	return s.startSession(p)
}

// liveCoordinator is the id of project slug's running coordinator, ""
// for none.
func (s *Server) liveCoordinator(slug string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, sess := range s.sessions {
		if r := s.records[id]; r.Role == proto.RoleCoordinator && r.Project == slug && sess.ExitStatus() == "" {
			return id
		}
	}
	return ""
}

// startedResult is session.start's answer for session id, already
// running.
func (s *Server) startedResult(id string) (any, *proto.Error) {
	s.mu.Lock()
	sess := s.sessions[id]
	s.mu.Unlock()
	if sess == nil {
		return nil, proto.Errorf(proto.ErrRefused, "session %s ended as it woke", id)
	}
	return proto.SessionStartResult{Session: s.info(sess)}, nil
}
