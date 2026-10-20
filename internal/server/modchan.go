package server

// The mod's channel (docs/SPEC.md §8.6, Mods): a session with terminatr's
// mod gets a small HTTP listener on a Unix socket of its own, in its
// runtime directory, and the mod reports the session's state there with
// $.http.fetch (socketPath). HTTP because that is all a mod can speak to
// a socket: tm.sock's NDJSON needs a long-lived writer, and a mod's
// child process gets its stdin once, then closed. One socket per
// session, so the path says which session reports, and nothing else on
// the machine listens on a TCP port.
//
// The other way, the mod pulls: nothing can push into a mod. It long-polls
// GET /v1/prompts for the head of the session's prompt queue and acks it
// with POST /v1/prompts/{id}/ack (session/modprompt.go). It fetches the
// guard's rules with GET /v1/rules and reports each refusal with POST
// /v1/denied (guard.go).
//
// GET /v1/tools lists the session's tools, and the mod's tools call POST /v1/tools/{name}
// (modtools.go).
//
// Only the session's own processes are answered: the peer's pid, walked
// up the process tree as for the control socket (callerOf), must lead to
// the session; anything else gets 403 (modGate).

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/plat/ipc"
	"github.com/theclifmeister/terminatr/internal/session"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// envModSocket names the mod's socket in the session's environment.
const envModSocket = "TERMINATR_MOD_SOCKET"

// modSocketName is the socket's name in the session's runtime dir.
const modSocketName = "mod.sock"

// maxModReport bounds a report's body.
const maxModReport = 4 << 10

// Long poll bounds for GET /v1/prompts?wait=<seconds>.
const (
	modPollDefault = 20 * time.Second
	modPollMax     = 25 * time.Second
)

// ModAck is the body of POST /v1/prompts/{id}/ack.
type ModAck struct {
	Result string `json:"result"` // session.ModTaken, ModSubmitted or ModRefused
	Error  string `json:"error,omitempty"`
}

// ModLog is the body of POST /v1/log: one line for the server's log,
// cut to maxModLog.
type ModLog struct {
	Text string `json:"text"`
}

// maxModLog bounds a logged line.
const maxModLog = 300

// ModReport is the body of POST /v1/state: the state the mod holds, and
// the event that moved it there ("beat" for a heartbeat).
type ModReport struct {
	State  agent.State `json:"state"`
	Reason string      `json:"reason,omitempty"`
	Event  string      `json:"event,omitempty"`
}

// ModUsage is the body of POST /v1/usage: what one turn used, numbers
// and the model's id only.
type ModUsage struct {
	Turn          string  `json:"turn,omitempty"`
	Model         string  `json:"model,omitempty"`
	Input         int64   `json:"input"`
	Output        int64   `json:"output"`
	CacheRead     int64   `json:"cache_read"`
	CacheCreation int64   `json:"cache_creation"`
	CostUSD       float64 `json:"cost_usd"`
	// Context is what the session's last request read as context (the
	// input and both cache counts, as the agent's status line counts
	// them; never turn.complete's usage, which sums the turn's
	// requests); 0 when unknown. ContextWindow is the window the agent
	// gives for its model; 0: the server judges it by the model's id.
	Context       int64 `json:"context,omitempty"`
	ContextWindow int64 `json:"context_window,omitempty"`
}

// listenMod opens session id's mod socket in its runtime dir rt; s.mu
// held. Any listener the id had is closed first.
func (s *Server) listenMod(id, rt string) (string, error) {
	s.closeModLocked(id)
	srv, path, err := s.openMod(id, rt)
	if err != nil {
		return "", err
	}
	s.setModLocked(id, srv)
	return path, nil
}

// setModLocked makes srv session id's mod server, closing any it had;
// s.mu held.
func (s *Server) setModLocked(id string, srv *http.Server) {
	s.closeModLocked(id)
	if s.mods == nil {
		s.mods = map[string]*http.Server{}
	}
	s.mods[id] = srv
}

// openMod opens session id's mod socket in its runtime dir rt and
// serves it; the launch's commit makes it the session's (s.mods), and
// abandoning the launch closes it. s.mu not needed.
func (s *Server) openMod(id, rt string) (*http.Server, string, error) {
	path := ipc.SessionAddr(rt, modSocketName)
	os.Remove(string(path))
	ln, err := ipc.Listen(path)
	if err != nil {
		return nil, "", err
	}
	srv := &http.Server{Handler: s.modGate(id, s.modHandler(id)), ReadHeaderTimeout: 5 * time.Second,
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			return context.WithValue(ctx, modPeerKey{}, &modPeer{conn: c})
		}}
	go srv.Serve(ln)
	return srv, string(path), nil
}

// modPeerKey keys a mod connection's modPeer in its requests' context.
type modPeerKey struct{}

// modPeer is one connection to a mod socket; ok, once true, says its
// peer is in the socket's session.
type modPeer struct {
	conn net.Conn
	mu   sync.Mutex
	ok   bool
}

// modGate answers with h only a connection whose peer is a process of
// session id (modPeerIn); the rest get 403. A peer found in it is
// trusted for the connection's later requests; one not found is looked
// at again with each request (a hook that came before the session was
// known).
func (s *Server) modGate(id string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := r.Context().Value(modPeerKey{}).(*modPeer)
		ok := false
		if p != nil {
			p.mu.Lock()
			if !p.ok {
				p.ok = s.modPeerIn(id, p.conn)
			}
			ok = p.ok
			p.mu.Unlock()
		}
		if !ok {
			http.Error(w, "only session "+id+"'s processes use this socket", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// modPeerIn says whether conn's peer is the same user and one of
// session id's processes.
func (s *Server) modPeerIn(id string, conn net.Conn) bool {
	peer, err := ipc.PeerOf(conn)
	if err != nil {
		s.log.Printf("session %s: mod socket: peer credentials: %v", id, err)
		return false
	}
	if !peer.SameUser {
		return false
	}
	if s.inSession != nil {
		return s.inSession(id, peer.PID)
	}
	return s.callerOf(peer.PID).Session == id
}

// closeModLocked closes session id's mod listener, if any; s.mu held.
func (s *Server) closeModLocked(id string) {
	if srv := s.mods[id]; srv != nil {
		srv.Close()
		delete(s.mods, id)
	}
}

// closeMods closes every mod listener (server stop).
func (s *Server) closeMods() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.mods {
		s.closeModLocked(id)
	}
}

func (s *Server) modHandler(id string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/state", func(w http.ResponseWriter, r *http.Request) {
		var rep ModReport
		b, err := io.ReadAll(io.LimitReader(r.Body, maxModReport))
		if err == nil {
			err = json.Unmarshal(b, &rep)
		}
		if err == nil {
			err = checkModReport(rep)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		sess := s.sessions[id]
		s.mu.Unlock()
		if sess == nil {
			http.Error(w, "session "+id+" is gone", http.StatusGone)
			return
		}
		s.modHeard(id)
		if err := sess.ModState(rep.State, rep.Reason, rep.Event); err != nil {
			code := http.StatusInternalServerError
			if errors.Is(err, session.ErrNoAgent) {
				code = http.StatusConflict
			}
			http.Error(w, err.Error(), code)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	// What a turn used, added to its thread's totals.
	mux.HandleFunc("POST /v1/usage", func(w http.ResponseWriter, r *http.Request) {
		var u ModUsage
		b, err := io.ReadAll(io.LimitReader(r.Body, maxModReport))
		if err == nil {
			err = json.Unmarshal(b, &u)
		}
		if err == nil {
			err = checkModUsage(u)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if s.modSession(w, id) == nil {
			return
		}
		s.setContext(id, u.Model, u.Context, u.ContextWindow)
		s.addUsage(id, thread.Usage{Turns: 1, Input: u.Input, Output: u.Output,
			CacheRead: u.CacheRead, CacheCreation: u.CacheCreation, CostUSD: u.CostUSD})
		w.WriteHeader(http.StatusNoContent)
	})
	// /clear: the context use is gone with the conversation; session.end
	// starts no session.start, so no turn would say so.
	mux.HandleFunc("POST /v1/context/clear", func(w http.ResponseWriter, r *http.Request) {
		if s.modSession(w, id) == nil {
			return
		}
		s.clearContext(id)
		w.WriteHeader(http.StatusNoContent)
	})
	// The head of the prompt queue once the mod delivers it, or 204 after
	// the wait.
	mux.HandleFunc("GET /v1/prompts", func(w http.ResponseWriter, r *http.Request) {
		wait := modPollDefault
		if n, err := strconv.Atoi(r.URL.Query().Get("wait")); err == nil && n >= 0 {
			wait = min(time.Duration(n)*time.Second, modPollMax)
		}
		sess := s.modSession(w, id)
		if sess == nil {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), wait)
		defer cancel()
		p, ok, err := sess.NextModPrompt(ctx)
		if err != nil {
			modError(w, id, err)
			return
		}
		if !ok {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(p)
	})
	mux.HandleFunc("POST /v1/prompts/{pid}/ack", func(w http.ResponseWriter, r *http.Request) {
		var ack ModAck
		b, err := io.ReadAll(io.LimitReader(r.Body, maxModReport))
		if err == nil {
			err = json.Unmarshal(b, &ack)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		sess := s.modSession(w, id)
		if sess == nil {
			return
		}
		if len(ack.Error) > 200 {
			ack.Error = ack.Error[:200]
		}
		if err := sess.AckModPrompt(r.PathValue("pid"), ack.Result, ack.Error); err != nil {
			modError(w, id, err)
			return
		}
		if ack.Result == session.ModSubmitted {
			s.watch.wake()
		}
		w.WriteHeader(http.StatusNoContent)
	})
	// A line for the server's log: the mod's prompt loop failing or
	// started again (T82), which nothing else would show.
	mux.HandleFunc("POST /v1/log", func(w http.ResponseWriter, r *http.Request) {
		var l ModLog
		b, err := io.ReadAll(io.LimitReader(r.Body, maxModReport))
		if err == nil {
			err = json.Unmarshal(b, &l)
		}
		if err == nil && strings.TrimSpace(l.Text) == "" {
			err = errors.New("text is required")
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if s.modSession(w, id) == nil {
			return
		}
		s.log.Printf("session %s: mod: %s", id, oneLine(l.Text, maxModLog))
		w.WriteHeader(http.StatusNoContent)
	})
	// The role's context, fresh (§7.8): the mod adds it to the
	// conversation's context, re-read after /clear and compaction. 204
	// for a session outside a project, and for a conversation a command
	// hook gave it to (convcontext.go).
	mux.HandleFunc("GET /v1/context", func(w http.ResponseWriter, r *http.Request) {
		if s.modSession(w, id) == nil {
			return
		}
		b, err := s.contextOf(id)()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if len(b) == 0 || !s.blockContext(id) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		s.servedContext(id)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(b)
	})
	// The mod's classic.SessionStart ran: the SessionStart command hook,
	// which runs after it, then only says where the context is.
	mux.HandleFunc("POST /v1/context/started", func(w http.ResponseWriter, r *http.Request) {
		if s.modSession(w, id) == nil {
			return
		}
		s.markedStart(id)
		w.WriteHeader(http.StatusNoContent)
	})
	// The guard's rules and refusals (guard.go).
	s.guardRoutes(mux, id)
	mux.HandleFunc("GET /v1/tools", func(w http.ResponseWriter, r *http.Request) {
		s.modToolList(w, id)
	})
	mux.HandleFunc("POST /v1/tools/{name}", func(w http.ResponseWriter, r *http.Request) {
		s.modTool(w, r, id)
	})
	return mux
}

// modSession is session id, or nil after a 410 when it is gone.
func (s *Server) modSession(w http.ResponseWriter, id string) *session.Session {
	s.mu.Lock()
	sess := s.sessions[id]
	s.mu.Unlock()
	if sess == nil {
		http.Error(w, "session "+id+" is gone", http.StatusGone)
	}
	return sess
}

func modError(w http.ResponseWriter, id string, err error) {
	code := http.StatusInternalServerError
	switch {
	case errors.Is(err, session.ErrNoAgent):
		code = http.StatusConflict
	case errors.Is(err, session.ErrExited):
		code = http.StatusGone
	case errors.Is(err, session.ErrNoModPrompt):
		code = http.StatusNotFound
	case errors.Is(err, session.ErrModAck):
		code = http.StatusBadRequest
	}
	http.Error(w, err.Error(), code)
}

func checkModReport(r ModReport) error {
	switch r.State {
	case agent.StateIdle, agent.StateWorking, agent.StateBlocked, agent.StateExited:
	default:
		return errors.New("state must be idle, working, blocked or exited")
	}
	if len(r.Reason) > 64 || len(r.Event) > 64 {
		return errors.New("reason and event are short words")
	}
	return nil
}

// maxModTokens bounds a count: a turn can't use more than a model reads.
const maxModTokens = 1 << 40

func checkModUsage(u ModUsage) error {
	for _, n := range []int64{u.Input, u.Output, u.CacheRead, u.CacheCreation, u.Context, u.ContextWindow} {
		if n < 0 || n > maxModTokens {
			return errors.New("token counts are small non-negative numbers")
		}
	}
	if u.CostUSD < 0 || u.CostUSD > 1e6 || u.CostUSD != u.CostUSD {
		return errors.New("cost_usd is a small non-negative number")
	}
	if len(u.Turn) > 128 || len(u.Model) > 64 {
		return errors.New("turn and model are short words")
	}
	return nil
}
