package server

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/codehost"
	"github.com/theclifmeister/terminatr/internal/emu"
	"github.com/theclifmeister/terminatr/internal/keychain"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modnotify"
	"github.com/theclifmeister/terminatr/internal/modsup"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/pin"
	"github.com/theclifmeister/terminatr/internal/plat/caps"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/plat/ipc"
	"github.com/theclifmeister/terminatr/internal/plat/shell"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/ticker"
	"github.com/theclifmeister/terminatr/internal/version"
)

// StopGrace is how long sessions get between SIGHUP and SIGKILL.
const StopGrace = 5 * time.Second

// handlerGrace bounds how long Run waits, after closing every
// connection, for their goroutines to end.
const handlerGrace = 5 * time.Second

// handshakeTimeout bounds how long a new connection may take to say hello.
const handshakeTimeout = 5 * time.Second

// AlreadyRunningError is returned by Run when another server holds the lock.
type AlreadyRunningError struct{ PID int }

func (e *AlreadyRunningError) Error() string {
	if e.PID > 0 {
		return fmt.Sprintf("already running (pid %d)", e.PID)
	}
	return "already running"
}

// Options configure a server.
type Options struct {
	Paths client.Paths
	Log   *log.Logger
	// Bin is the absolute path of tm, exported to sessions as TERMINATR_BIN.
	Bin string
	// Source, when set, is the tm to pin in place of Bin: launchd runs
	// the pin itself (service.Config.Program), and the launch file names
	// the tm that started it. A missing Source pins Bin.
	Source string
	// Exec, when set, runs the pinned binary in place of this process
	// (proc.Exec), with Args, so the server itself runs from the pin
	// (pin.Binary). Nil keeps running the binary started.
	Exec func(bin string, argv, env []string) error
	// Args are the arguments Exec passes, after the binary.
	Args []string
	// Env is the base environment for sessions; nil means os.Environ().
	Env []string
	// RunCLI runs a project command for cli.run (set by package cli,
	// which imports this one). Nil refuses cli.run.
	RunCLI func(p proto.CLIRunParams, c caller.Caller) proto.CLIRunResult
}

// Server owns every session and the control socket.
type Server struct {
	opts    Options
	log     *log.Logger
	build   string
	started time.Time
	stopReq chan struct{}
	stopOne sync.Once

	// threadMu serialises the mirroring of thread state into files.
	threadMu sync.Mutex
	// taskOf caches each thread's task ref by "<project>/<thread>"
	// (taskRef); a thread's task never changes.
	taskOf sync.Map
	// ctxOf is each session's latest context use (setContext), by id.
	ctxOf sync.Map

	// latest is the newer release's tag and upgrade its install hint
	// ("" for none), from the daily update check (updatecheck.go).
	latest    atomic.Pointer[latestRelease]
	checkedAt atomic.Int64 // unix time update.CachedLatest was last consulted

	mu       sync.Mutex
	sessions map[string]*session.Session
	records  map[string]SessionRecord
	// runs are the approved commands running for a thread (approved.go),
	// by pid: the pid walk places what they start in that thread.
	runs map[int]SessionRecord
	// relaunch marks sessions stopped to be resumed at once under the
	// same id (a remote control change), instead of ending, with the
	// remote control state they get.
	relaunch map[string]bool
	// sleeping marks sessions stopped because their project was
	// deactivated: their records go to dormant, to resume at its
	// activation (active.go).
	sleeping map[string]bool
	dormant  map[string]SessionRecord
	blocked  map[string]bool            // sessions whose agent is blocked, for alerts
	told     map[string]map[string]bool // the consoles, by session, that raised its takeover (consoleTakeover)
	// adoptAsks are the human's go-aheads to adopt a session (by id)
	// into a project (session.adopt.ask), kept until it is adopted.
	adoptAsks map[string]adoptAsk
	nextID    int
	stopping  bool
	prevShut  string
	lost      []string
	resumed   []string
	conns     map[net.Conn]struct{}
	// handlers counts the connection goroutines: Run waits for them, so
	// nothing logs or writes state after it returned.
	handlers sync.WaitGroup
	// prevProject maps the previous server's session ids to their
	// project, for the restart inbox items.
	prevProject map[string]string

	// tick is the ticker (docs/SPEC.md §7.5); alerts counts the alerts
	// raised, so clients know when to ring.
	tick   *ticker.Ticker
	alerts atomic.Uint64

	agents    *agent.Registry
	agentErrs []string
	// scheme is the colour scheme a client last reported; new sessions
	// start with it.
	scheme emu.Scheme
	// colors are the terminal colours a client last reported; new
	// sessions start with them.
	colors emu.Colors
	// liveThemed are the agents whose live theme (agent.LiveThemer)
	// this server wrote for a launch: it rewrites them on every theme
	// or scheme change (themeChangedLocked).
	liveThemed map[string]bool

	// views are what consoles show (views.go).
	views *views
	// watch wakes the session watches (watch.go).
	watch watchers
	// projects is what project watches read from files, per project.
	projects projectCache
	// sides is what the projects watches send (sideprojects.go).
	sides sideHub
	// modEv is the user mods' event bus (modevents.go).
	modEv *modEvents
	// modSup runs the code mods' hosts (modhosts.go); set before Run
	// serves, nil in a test's bare Server.
	modSup atomic.Pointer[modsup.Supervisor]
	// noMods is safe mode (mod.EnvNoMods): no user mod runs.
	noMods bool
	// modBr is the user mods' broker (modbroker.go).
	modBr modBroker
	// modViews are the views user mods draw (modviews.go).
	modViewSt  *modview.Store
	modViewsMu sync.Mutex
	// presses are the press tokens of mod.action.check (modactions.go).
	presses pressTokens
	// modCl are the mods' cells (modcells.go).
	modCl modCells
	// notices are the mods' notifications (modnotify.go).
	notices *modnotify.Hub
	// toasts are the mods' toasts (modtoast.go).
	toasts modToasts
	// versions caches agent versions (launchconfig.go); nil
	// is the process's cache (agent.Version). Tests set their own.
	versions *agent.Versions
	// asks are the sessions' open questions (ask.go).
	asks asks
	// guard is the guard's memory of the sessions (guard.go).
	guard guardState
	// launched is the model each agent session was launched with ("" its
	// agent's own default), and refusedSeen the sessions whose refusal
	// was learnt, both under mu; modelsBusy are the agents being asked
	// for their models, under modelsMu (models.go).
	launched     map[string]string
	refusedSeen  map[string]bool
	modelsMu     sync.Mutex
	modelsBusy   map[string]bool
	modelUnknown map[string]string // session -> why its models were unknown when last logged, under mu
	// modelsCtx ends the agents' models probes when the server stops.
	modelsCtx  context.Context
	modelsStop context.CancelFunc
	modelsWG   sync.WaitGroup // the probes in flight; shutdown waits for them

	// peers are the links to paired servers (internal/peer); set before
	// Run serves.
	peers *peer.Manager
	// mirror is what the links last read of the paired servers' shared
	// projects (peer.list); set before Run serves.
	mirror *peer.Mirror
	// remotes are the paired servers' panes this server shows
	// (docs/SPEC.md §16.12).
	remotes *remotes

	// protocol is the protocol the hello claims, and deaf hangs up on
	// every hello: test hooks (testhooks.go).
	protocol int
	deaf     bool
}

// Run runs a server until ctx is cancelled or a client calls server.stop.
// It returns *AlreadyRunningError if another server is running.
func Run(ctx context.Context, opts Options) error {
	p := opts.Paths
	logger := opts.Log
	if logger == nil {
		logger = log.New(os.Stderr, "", log.LstdFlags)
	}
	for _, d := range []string{p.Home, filepath.Dir(p.Log), filepath.Dir(p.Sessions)} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	// The run dir holds the socket, the lock and the pid file. Bind before
	// anything else can start: a server that cannot listen must not own
	// processes nobody can reach.
	if err := fsx.EnsurePrivateDir(p.RunDir); err != nil {
		return fmt.Errorf("refusing to use the run dir: %w", err)
	}
	lock, err := takeLock(p)
	if err != nil {
		return err
	}
	defer lock.Unlock()

	// Run from the pin before anything else: macOS privacy settings know
	// the server (the responsible process of everything it starts) by the
	// path launchd started it from, which then stays the same across
	// upgrades.
	if opts.Bin != "" {
		if bin, again, err := pin.For(p.RunDir, opts.Bin, opts.Source, logger.Printf); err != nil {
			logger.Printf("pin: %v; an upgrade in place will break attach re-exec and hooks until restart", err)
		} else {
			if opts.Exec != nil && again && !lock.handedOver {
				logger.Printf("running from %s", bin)
				err := execPinned(lock, bin, opts.Args, opts.Exec)
				logger.Printf("exec %s: %v; running from %s", bin, err, opts.Bin)
			}
			opts.Bin = bin
			removeLegacyPins(p.Home)
		}
	}

	// Write folders are checked against the user's PATH, not only ours.
	useLoginPath()

	// We hold the lock, so any socket file left here is stale.
	os.Remove(p.Socket)
	ln, err := ipc.Listen(ipc.Addr(p.Socket))
	if err != nil {
		return fmt.Errorf("listen %s: %w", p.Socket, err)
	}
	// The human socket: the same server, which no agent's sandbox lets
	// connect, so the human-only methods answer there alone.
	human, err := ipc.HumanAddr(ipc.Addr(p.Socket))
	var hln net.Listener
	if err == nil {
		os.Remove(string(human))
		hln, err = ipc.Listen(human)
	}
	if err != nil {
		ln.Close()
		return fmt.Errorf("human socket: %w", err)
	}
	if err := os.WriteFile(p.PID, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		ln.Close()
		hln.Close()
		return err
	}

	s := &Server{
		opts:      opts,
		log:       logger,
		build:     version.BuildID(),
		started:   time.Now(),
		noMods:    mod.NoMods(os.Getenv),
		stopReq:   make(chan struct{}),
		sessions:  map[string]*session.Session{},
		records:   map[string]SessionRecord{},
		relaunch:  map[string]bool{},
		sleeping:  map[string]bool{},
		dormant:   map[string]SessionRecord{},
		blocked:   map[string]bool{},
		adoptAsks: map[string]adoptAsk{},
		nextID:    1,
		conns:     map[net.Conn]struct{}{},

		prevProject: map[string]string{},
		protocol:    proto.Protocol,
		modEv:       newModEvents(),
		notices:     modnotify.New(nil),
	}
	if n, deaf := testHello(); n > 0 || deaf {
		s.deaf = deaf
		if n > 0 {
			s.protocol = n
		}
		logger.Printf("test hook %s: protocol %d, deaf %v", testHelloEnv, s.protocol, s.deaf)
	}
	s.watchOwner()
	s.watchGone()
	s.views = newViews(s, filepath.Join(filepath.Dir(p.Sessions), "views.json"), logger.Printf)
	s.loadAgents()
	s.modelsCtx, s.modelsStop = context.WithCancel(context.Background())
	defer s.modelsStop()
	s.refreshModels()
	// Before any agent can run a command: the repo lists leave
	// PROJECT.md for config.toml, once (T335).
	if moved, err := project.MoveRepos(time.Now()); err != nil {
		logger.Printf("repo lists: %v", err)
	} else if len(moved) > 0 {
		logger.Printf("repo lists moved to config.toml: %s", strings.Join(moved, ", "))
	}
	// And config.toml's sandbox rules move to the access rules, once.
	migrateAccess(logger.Printf)
	// And an install from before protection keeps what it ran with.
	pinProtection(logger.Printf)
	tellYolo(logger.Printf)
	toResume, lost := s.loadPrevious()
	if err := s.saveLocked(""); err != nil {
		logger.Printf("sessions.json: %v", err)
	}
	// The links to paired servers: each its own goroutine, which no
	// local call waits on (docs/SPEC.md §16.7). They read the shared
	// projects through peerExport (§16.9), nothing else.
	s.peers = peer.Start(peer.Options{Dir: peer.DirIn(p.Home), Version: version.Version, Logf: logger.Printf, Export: s.peerExport(), Messages: peerMessages{}, Panes: peerPanes{s}})
	defer s.peers.Close()
	// What they share, for the consoles (§16.10); a peer's pane switch
	// reaches the remote panes open here at once (§16.12).
	s.remotes = newRemotes(s)
	s.mirror = peer.StartMirror(s.peers, peer.MirrorOptions{Logf: logger.Printf, Changed: s.watch.wake, Panes: s.remotes.setLevel})
	defer s.mirror.Close()
	defer s.remotes.closeAll()
	// Messages to linked projects on paired servers wait in the outbox
	// until their link is up (§16.11).
	outboxCtx, stopOutbox := context.WithCancel(context.Background())
	defer stopOutbox()
	go link.RunOutbox(outboxCtx, peerTransport{s.peers})
	logger.Printf("server pid %d %s protocol %d listening on %s", os.Getpid(), s.build, proto.Protocol, p.Socket)
	go func() {
		if st := keychain.Probe(runtime.GOOS, os.Getenv, keychain.Run); st.Checked && !st.OK {
			logger.Printf("keychain: %s; the PR CLI (gh, az) and git credential helpers will fail in sessions: %s", st.Detail, keychain.Fix)
		}
	}()

	var accepting sync.WaitGroup
	accept := func(ln net.Listener, human bool) {
		accepting.Add(1)
		go func() {
			defer accepting.Done()
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				s.handlers.Add(1)
				go func() {
					defer s.handlers.Done()
					s.handle(c, human)
				}()
			}
		}()
	}
	accept(ln, false)
	accept(hln, true)
	// Resume once hooks can be answered: a resumed agent fires
	// SessionStart at once.
	if outs := s.resume(toResume); s.prevShut != "" {
		s.logRestart(s.prevShut, append(lost, outs...))
	}
	// The views come back with the sessions that did.
	s.views.load()
	tctx, stopTicker := context.WithCancel(context.Background())
	tickerDone := s.startTicker(tctx)
	ectx, stopEvents := context.WithCancel(context.Background())
	defer stopEvents()
	go s.runModEvents(ectx)
	hostsDone := make(chan struct{})
	if sup := s.newModHosts(); sup != nil {
		s.modSup.Store(sup)
		codehost.Provide(s.codeHostMod)
		go func() {
			defer close(hostsDone)
			s.runModHosts(ectx, sup)
		}()
	} else {
		close(hostsDone)
	}

	select {
	case <-ctx.Done():
		logger.Printf("stopping: %v", context.Cause(ctx))
	case <-s.stopReq:
		logger.Printf("stopping: requested by a client")
	}
	s.modelsStop()
	s.mirror.Close()
	s.peers.Close()
	// The mod hosts go first: nothing waits on them.
	stopEvents()
	<-hostsDone
	// The cells they published last show again after a restart.
	if err := s.cells().Flush(); err != nil {
		logger.Printf("mods: cells: %v", err)
	}
	// Let a sweep in progress finish while the socket still answers.
	stopTicker()
	select {
	case <-tickerDone:
	case <-time.After(15 * time.Second):
		logger.Printf("ticker: still sweeping; stopping anyway")
	}
	ln.Close()
	hln.Close()
	accepting.Wait()
	s.shutdown()
	handled := make(chan struct{})
	go func() {
		s.handlers.Wait()
		close(handled)
	}()
	select {
	case <-handled:
	case <-time.After(handlerGrace):
		logger.Printf("connections still closing; stopping anyway")
	}
	os.Remove(p.Socket)
	os.Remove(string(human))
	os.Remove(p.PID)
	logger.Printf("server stopped")
	return nil
}

// loadPrevious reads the last server's sessions.json: was its shutdown
// clean, which agent sessions to resume, which stay dormant (an inactive
// or archived project's), and which sessions are gone. Shell sessions
// and agents without a recorded agent session id are never restored
// (docs/SPEC.md §3.6, §5.1).
func (s *Server) loadPrevious() (resume []SessionRecord, lost []restartOutcome) {
	prev, err := loadState(s.opts.Paths.Sessions)
	if err != nil {
		s.log.Printf("sessions.json unreadable, starting fresh: %v", err)
		return nil, nil
	}
	if prev == nil {
		return nil, nil
	}
	if prev.NextID > s.nextID {
		s.nextID = prev.NextID
	}
	s.scheme = emu.Scheme(prev.Scheme)
	if b, err := hex.DecodeString(prev.Colors); err == nil {
		if c, ok := emu.DecodeColors(b); ok {
			s.colors = c
		}
	}
	if prev.Shutdown == "clean" {
		s.prevShut = "clean"
	} else {
		s.prevShut = "crash"
		s.log.Printf("previous server (pid %d) did not shut down cleanly", prev.ServerPID)
	}
	boots := projectBoots()
	for _, r := range prev.Dormant {
		switch why := dormantGone(r); {
		case why != "":
			s.log.Printf("session %s: dormant %s dropped: %s", r.ID, restartLabel(r), why)
		case boots(r.Project):
			resume = append(resume, r)
		default:
			s.dormant[r.ID] = r
		}
	}
	for _, r := range prev.Sessions {
		if r.Project != "" {
			s.prevProject[r.ID] = r.Project
		}
		if r.Agent != "" && r.AgentSessionID != "" {
			if dormantRole(r) && !boots(r.Project) {
				s.dormant[r.ID] = r
				continue
			}
			resume = append(resume, r)
			continue
		}
		s.lost = append(s.lost, r.ID)
		lost = append(lost, restartOutcome{rec: r, how: "lost"})
	}
	if len(s.lost) > 0 {
		s.log.Printf("sessions of the previous server not restored: %v", s.lost)
	}
	return resume, lost
}

// saveLocked rewrites sessions.json; s.mu held (or no concurrency yet).
func (s *Server) saveLocked(shutdown string) error {
	st := &State{
		Version:   stateVersion,
		ServerPID: os.Getpid(),
		Started:   s.started,
		Shutdown:  shutdown,
		NextID:    s.nextID,
		Scheme:    uint8(s.scheme),
	}
	if s.colors.Known != 0 {
		st.Colors = hex.EncodeToString(s.colors.Encode())
	}
	for _, r := range s.records {
		if shutdown == "clean" {
			r.CleanExit = true
		}
		st.Sessions = append(st.Sessions, r)
	}
	sort.Slice(st.Sessions, func(i, j int) bool { return st.Sessions[i].Created.Before(st.Sessions[j].Created) })
	for _, r := range s.dormant {
		st.Dormant = append(st.Dormant, r)
	}
	sort.Slice(st.Dormant, func(i, j int) bool { return st.Dormant[i].Created.Before(st.Dormant[j].Created) })
	return saveState(s.opts.Paths.Sessions, st)
}

// saveColorsLocked keeps the colours and scheme a console reported for
// the next server (State.Colors); s.mu held.
func (s *Server) saveColorsLocked() {
	if s.stopping {
		return // shutdown saves them, with the clean mark
	}
	if err := s.saveLocked(""); err != nil {
		s.log.Printf("sessions.json: %v", err)
	}
}

func (s *Server) requestStop() { s.stopOne.Do(func() { close(s.stopReq) }) }

// shutdown stops every session (SIGHUP, then SIGKILL after StopGrace),
// records them for resume and marks the shutdown clean.
func (s *Server) shutdown() {
	s.mu.Lock()
	s.stopping = true
	sessions := make([]*session.Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	for c := range s.conns {
		c.Close()
	}
	s.mu.Unlock()

	var wg sync.WaitGroup
	for _, sess := range sessions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sess.Stop(StopGrace)
		}()
	}
	wg.Wait()
	if s.modelsStop != nil {
		s.modelsStop()
	}
	s.modelsWG.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.saveLocked("clean"); err != nil {
		s.log.Printf("sessions.json: %v", err)
	}
	os.RemoveAll(filepath.Join(s.opts.Paths.RunDir, "s"))
}

// handle runs one connection: peer check, handshake, then control or
// attach. human is the connection came in on the human socket, which
// no agent's sandbox lets connect: the human-only methods need it.
func (s *Server) handle(c net.Conn, human bool) {
	defer c.Close()
	peer, err := ipc.PeerOf(c)
	if err != nil {
		s.log.Printf("peer credentials: %v", err)
		return
	}
	pid := peer.PID
	if !peer.SameUser {
		s.log.Printf("rejected connection from another user (pid %d)", pid)
		return
	}
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
	if s.deaf {
		return
	}
	reply := s.hello()
	if err := proto.WriteLine(c, reply); err != nil {
		return
	}
	// Both sides check; the client says what went wrong, the server just
	// refuses to go on (docs/SPEC.md §3.3).
	if err := proto.Check(hello, reply); err != nil {
		s.log.Printf("refused %s connection from pid %d: %v", hello.Kind, pid, err)
		return
	}
	switch hello.Kind {
	case proto.KindControl, proto.KindHook:
		s.serveControl(c, br, pid, human)
	case proto.KindAttach:
		s.serveAttach(c, br, pid, human)
	default:
		s.log.Printf("refused connection of unknown kind %q from pid %d", hello.Kind, pid)
	}
}

// hello is the server's side of the handshake.
func (s *Server) hello() proto.Hello {
	return proto.Hello{
		Protocol: s.protocol,
		Version:  version.Version,
		Build:    s.build,
		Bin:      s.opts.Bin,
		PID:      os.Getpid(),
	}
}

func (s *Server) serveControl(c net.Conn, br *bufio.Reader, peerPID int, human bool) {
	for {
		var req proto.Request
		if err := proto.ReadLine(br, &req); err != nil {
			return
		}
		who := s.whoFor(peerPID)
		m, known := methodOf(req.Method)
		perr := mayCall(req.Method, m.gate, human, who)
		var params any
		if perr == nil {
			params, perr = s.mayAddress(req, m, human, who)
		}
		if perr != nil {
			if req.Method == proto.MethodHookEvent {
				s.log.Printf("hook event from pid %d (caller %s) refused: %s", peerPID, who(), perr.Message)
			}
			if err := proto.WriteLine(c, proto.Response{ID: req.ID, Error: perr}); err != nil {
				return
			}
			continue
		}
		if m.stream != nil {
			m.stream(s, c, br, req, peerPID)
			return
		}
		var result any
		if known {
			result, perr = m.call(s, req, callCtx{method: req.Method, params: params, peerPID: peerPID, human: human, who: who})
		} else {
			perr = proto.Errorf(proto.ErrUnknownMethod, "unknown method %q", req.Method)
		}
		resp := proto.Response{ID: req.ID, Error: perr}
		if perr == nil {
			b, err := json.Marshal(result)
			if err != nil {
				resp.Error = proto.Errorf(proto.ErrInternal, "%v", err)
			} else {
				resp.Result = b
			}
		}
		if err := proto.WriteLine(c, resp); err != nil {
			return
		}
		if req.Method == proto.MethodServerStop && perr == nil {
			s.log.Printf("server.stop from pid %d", peerPID)
			s.requestStop()
		}
	}
}

// mayAnswer refuses session.answer on session in to caller c unless c is
// the human on the human socket, or the coordinator of the session's
// own project answering one of its threads (tm thread answer, docs/SPEC.md
// §11.2), on either socket. A coordinator's own questions are the
// human's to answer.
func (s *Server) mayAnswer(in proto.SessionInfo, c caller.Caller, human bool) *proto.Error {
	switch {
	case c.Kind == caller.Human && human:
		return nil
	case c.Kind == caller.Coordinator && in.Project != "" && c.Project == in.Project && in.Role != proto.RoleCoordinator:
		return nil
	}
	return proto.Errorf("human-only", "only the human, on the human socket (%s), or project %s's coordinator answers session %s's questions", ipc.HumanName, in.Project, in.ID)
}

// mayCall is gate g of method m before it runs (method.gate), who
// telling the caller from the peer pid (asked only when needed: it walks
// the process tree). A human-only method answers only on the human
// socket, and there only to a caller the pid walk doesn't place in an
// agent's session (an agent without an OS sandbox can reach the socket).
func mayCall(m string, g gate, human bool, who func() caller.Caller) *proto.Error {
	switch {
	case g == gateHuman && !human:
		return humanSocketOnly(m)
	case (g == gateHuman || g == gateNotAgent) && who().IsAgent():
		return proto.Errorf("human-only", "%s is the human's; an agent's session doesn't call it", m)
	case g == gateAgentOrHuman && !human && !who().IsAgent():
		return proto.Errorf("human-only", "%s from outside every agent's session answers only on the human socket (%s)", m, ipc.HumanName)
	}
	return nil
}

// whoFor is callerOf(pid), deferred and asked once: one request's gates
// and handler share one walk. Not a connection's: a session.adopt can
// make a shell session an agent's while a connection from it stays open.
func (s *Server) whoFor(pid int) func() caller.Caller {
	return sync.OnceValue(func() caller.Caller { return s.callerOf(pid) })
}

// attachMethod names an attach connection in mayCall and its errors.
const attachMethod = "attach"

// humanSocketOnly refuses human-only method m on the server socket:
// every agent's sandbox allows that one, and the pid walk that tells an
// agent from the human (callerOf) ends where a process left its
// session's tree, so only the human socket vouches for the human.
func humanSocketOnly(m string) *proto.Error {
	return proto.Errorf("human-only", "%s answers only on the human socket (%s), which no agent's sandbox allows", m, ipc.HumanName)
}

// read is session.read's answer.
func (s *Server) read(p proto.SessionReadParams) (any, *proto.Error) {
	sess, perr := s.session(p.ID)
	if perr != nil {
		return nil, perr
	}
	r, err := sess.Read(p.Scrollback)
	if err != nil {
		return nil, sessionError(p.ID, err)
	}
	return r, nil
}

// keys is session.keys's answer.
func (s *Server) keys(p proto.SessionKeysParams) (any, *proto.Error) {
	sess, perr := s.session(p.ID)
	if perr != nil {
		return nil, perr
	}
	// The coordinator's keys (tm thread approve, answer: the only way
	// they reach a session) never answer an escape's prompt, checked
	// here against the session's state as the keys go (T574 case 6).
	if p.Relay == proto.RoleCoordinator && sess.Info().Escape {
		return nil, proto.Errorf(proto.ErrRefused, "session %s asks to run a command outside its sandbox: only the user answers that prompt", p.ID)
	}
	if err := sess.Input([]byte(p.Data)); err != nil {
		return nil, sessionError(p.ID, err)
	}
	return struct{}{}, nil
}

// answer is session.answer's, gated by mayAnswer.
func (s *Server) answer(p proto.SessionAnswerParams, cc callCtx) (any, *proto.Error) {
	sess, perr := s.session(p.ID)
	if perr != nil {
		return nil, perr
	}
	c := cc.who()
	if perr := s.mayAnswer(sess.Info(), c, cc.human); perr != nil {
		return nil, perr
	}
	// Only the human answers while an escape's prompt is open: not an
	// agent, nor tm's commands answering for the coordinator.
	byHuman := cc.human && p.Relay != proto.RoleCoordinator && !c.IsAgent()
	if !byHuman && sess.Info().Escape {
		return nil, proto.Errorf(proto.ErrRefused, "session %s asks to run a command outside its sandbox: only the user answers that prompt", p.ID)
	}
	res, perr := s.asks.answer(p.ID, p.Index, p.Answer, p.Question, byHuman)
	if perr == nil && p.Question != "" {
		s.journalAnswer(p, c)
	}
	s.watch.wake()
	s.modEv.wake()
	return res, perr
}

func (s *Server) status() proto.ServerStatus {
	link := s.peers.Status()
	s.mu.Lock()
	defer s.mu.Unlock()
	return proto.ServerStatus{
		PID:              os.Getpid(),
		Version:          version.Version,
		Build:            s.build,
		Protocol:         proto.Protocol,
		Started:          s.started,
		Sessions:         len(s.sessions),
		Socket:           s.opts.Paths.Socket,
		Home:             s.opts.Paths.Home,
		PreviousShutdown: s.prevShut,
		Lost:             s.lost,
		Resumed:          s.resumed,
		Link:             link,
	}
}

func (s *Server) stop(p proto.ServerStopParams) (any, *proto.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	agents := 0
	for id, sess := range s.sessions {
		if sess.Config().Role != proto.RoleShell || s.records[id].Agent != "" {
			agents++
		}
	}
	if agents > 0 && !p.Yes {
		return nil, proto.Errorf(proto.ErrRefused, "%d agent session(s) running; pass --yes to stop them", agents)
	}
	return map[string]int{"sessions": len(s.sessions)}, nil
}

func (s *Server) list() proto.SessionListResult {
	s.mu.Lock()
	sessions := make([]*session.Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.mu.Unlock()
	res := proto.SessionListResult{Sessions: []proto.SessionInfo{}, Alerts: s.alerts.Load(),
		Version: version.Version, Build: s.build, OS: caps.OS}
	if l := s.latest.Load(); l != nil {
		res.Latest, res.Upgrade = l.Tag, l.Upgrade
	}
	res.ModNotices = s.modNotices()
	s.checkForUpdate()
	for _, sess := range sessions {
		res.Sessions = append(res.Sessions, s.info(sess))
	}
	sort.Slice(res.Sessions, func(i, j int) bool { return res.Sessions[i].Created.Before(res.Sessions[j].Created) })
	return res
}

// sessionList is session.list's answer: list's, with the mods' cells.
func (s *Server) sessionList() proto.SessionListResult {
	res := s.list()
	res.Mods = s.modCellsView()
	for i := range res.Sessions {
		res.Sessions[i].Cells = s.sessionCells(res.Sessions[i].ID)
	}
	return res
}

// info is sess's info with its thread's task.
func (s *Server) info(sess *session.Session) proto.SessionInfo {
	info := sess.Info()
	info.Question = s.asks.of(info.ID)
	if v, ok := s.ctxOf.Load(info.ID); ok {
		c := v.(ctxUse)
		info.Context, info.ContextWindow = c.tokens, c.window
	}
	if info.Role == proto.RoleThread && thread.ValidID(info.Thread) {
		info.Task = s.taskRef(info.Project, info.Thread)
	}
	return info
}

// taskRef is thread id's task ref in project slug, "" for none.
func (s *Server) taskRef(slug, id string) string {
	key := slug + "/" + id
	if v, ok := s.taskOf.Load(key); ok {
		return v.(string)
	}
	p, err := project.Open(slug)
	if err != nil {
		return ""
	}
	rec, err := thread.Load(p, id)
	if err != nil {
		return ""
	}
	s.taskOf.Store(key, rec.Task)
	return rec.Task
}

func (s *Server) session(id string) (*session.Session, *proto.Error) {
	if _, _, ok := remoteID(id); ok {
		return nil, remoteRefusal(id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, proto.Errorf(proto.ErrUnknownSession, "no session %q", id)
	}
	return sess, nil
}

func sessionError(id string, err error) *proto.Error {
	if errors.Is(err, session.ErrExited) {
		return proto.Errorf(proto.ErrUnknownSession, "session %q has exited", id)
	}
	return proto.Errorf(proto.ErrInternal, "%v", err)
}

func (s *Server) startSession(p proto.SessionStartParams) (any, *proto.Error) {
	argv := p.Argv
	if len(argv) == 0 && p.Agent == "" {
		argv = shell.Interactive()
	}
	if p.Agent != "" && len(argv) > 0 {
		return nil, proto.Errorf(proto.ErrBadParams, "pass an agent or a command, not both")
	}
	cwd := p.Cwd
	if cwd == "" {
		cwd, _ = os.UserHomeDir()
	}
	if !filepath.IsAbs(cwd) {
		return nil, proto.Errorf(proto.ErrBadParams, "cwd must be absolute: %q", cwd)
	}
	if fi, err := os.Stat(cwd); err != nil || !fi.IsDir() {
		return nil, proto.Errorf(proto.ErrBadParams, "cwd is not a directory: %s", cwd)
	}
	role := p.Role
	switch role {
	case "":
		role = proto.RoleShell
	case proto.RoleShell:
	case proto.RoleCoordinator, proto.RoleThread:
		if p.Agent == "" || p.Project == "" {
			return nil, proto.Errorf(proto.ErrBadParams, "role %s needs an agent and a project", role)
		}
		if role == proto.RoleThread && p.Thread == "" {
			return nil, proto.Errorf(proto.ErrBadParams, "role thread needs a thread id")
		}
	default:
		return nil, proto.Errorf(proto.ErrBadParams, "unknown role %q", role)
	}
	if (role == proto.RoleCoordinator || role == proto.RoleThread) && !projectActive(p.Project) {
		return nil, inactiveErr(p.Project)
	}
	if p.Brief != "" && !filepath.IsAbs(p.Brief) {
		return nil, proto.Errorf(proto.ErrBadParams, "brief must be an absolute path: %q", p.Brief)
	}
	cols, rows := p.Cols, p.Rows
	if cols == 0 || rows == 0 {
		cols, rows = 80, 24
	}

	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return nil, proto.Errorf(proto.ErrRefused, "server is stopping")
	}
	id := fmt.Sprintf("s-%d", s.nextID)
	s.nextID++
	created := time.Now()
	rec := SessionRecord{ID: id, Role: role, Project: p.Project, Thread: p.Thread, Argv: argv, Cwd: cwd,
		Created: created, Cols: cols, Rows: rows}
	if p.Agent != "" {
		rec.Agent, rec.AgentSessionID, rec.Brief, rec.Model, rec.Yolo = p.Agent, newUUID(), p.Brief, p.Model, p.Yolo
		rec.Kickoff, rec.RemoteControl, rec.NoSandboxEscapes = p.Kickoff, p.RemoteControl, p.NoSandboxEscapes
		if role == proto.RoleThread && p.Protection != "" {
			if !knownProtection(p.Protection) {
				s.mu.Unlock()
				return nil, proto.Errorf(proto.ErrBadParams, "unknown protection %q", p.Protection)
			}
			rec.Protection = p.Protection
			rec.NoSandbox, rec.NoSandboxEscapes = protectFlags(p.Protection, p.Agent)
		}
		if p.RemoteControl && role != proto.RoleCoordinator {
			s.mu.Unlock()
			return nil, proto.Errorf(proto.ErrRefused, "remote control is for coordinators; threads are reached through theirs")
		}
		if p.RemoteControl && agent.RemoteControlOf(s.agentOr(p.Agent)) == nil {
			// The project's setting asks for it, but this agent has
			// none: start without it rather than not at all.
			s.log.Printf("agent %s has no remote control; starting %s without it", p.Agent, id)
			rec.RemoteControl = false
		}
		l := agentLaunch{rec: rec, kick: p.Kickoff, cols: cols, rows: rows}
		if p.ResumeSID != "" {
			l.rec.AgentSessionID, l.rec.Prompted, l.resume, l.kick = p.ResumeSID, true, true, ""
		}
		s.mu.Unlock()
		// The id is new: only a stop since can keep it from starting.
		sess, warns, perr := s.launchAgent(l, s.notStoppingLocked)
		if perr != nil {
			return nil, perr
		}
		return proto.SessionStartResult{Session: s.info(sess), Warnings: warns}, nil
	}
	defer s.mu.Unlock()
	env := sessionEnv(s.baseEnv(), s.terminatrEnv(rec))
	home, _ := os.UserHomeDir()
	reg := s.agents
	cfg := session.Config{
		ID: id, Role: role, Argv: argv, Cwd: cwd, Env: env,
		Cols: cols, Rows: rows, Created: created,
		Xtversion: "terminatr " + version.Version,
		Scheme:    s.scheme,
		Colors:    s.colors,
		Logf:      s.log.Printf,
		OnExit:    s.sessionExited,
		// A shell gets agent state while an agent the user started by
		// hand runs in its foreground (docs/SPEC.md §8.1 Identify).
		ObservedAgent: session.AgentConfig{Home: home, OnChange: s.agentChanged, OnUsage: s.tailUsage,
			PromptHold: envDuration(envPromptHold), OnPromptResolved: s.promptResolved},
	}
	if reg != nil {
		cfg.Identify = func(pi agent.ProcessInfo) agent.Agent {
			a, _ := reg.Identify(pi)
			return a
		}
	}
	sess, err := session.Start(cfg)
	if err != nil {
		return nil, proto.Errorf(proto.ErrRefused, "%v", err)
	}
	s.sessions[id] = sess
	s.records[id] = rec
	if err := s.saveLocked(""); err != nil {
		s.log.Printf("sessions.json: %v", err)
	}
	s.log.Printf("session %s: started pid %d %q in %s", id, sess.PID(), argv, cwd)
	return proto.SessionStartResult{Session: s.info(sess)}, nil
}

func (s *Server) sessionExited(sess *session.Session) {
	s.mu.Lock()
	// The views keep the panes of a stopping server, and of a session
	// relaunched under its id: they come back with it.
	gone := !s.stopping
	relaunch, remote := false, false
	defer func() {
		s.mu.Unlock()
		if relaunch {
			// Without s.mu: a launch takes long (launchAgent).
			gone = s.relaunchSession(sess, remote)
		}
		if gone {
			s.views.sessionGone(sess.ID())
		}
	}()
	delete(s.sessions, sess.ID())
	delete(s.blocked, sess.ID())
	delete(s.adoptAsks, sess.ID())
	delete(s.told, sess.ID())
	s.guard.forget(sess.ID())
	if s.tick != nil {
		s.tick.Kick()
	}
	s.watch.wake()
	s.modEv.wake()
	if s.stopping {
		return // keep the record: shutdown writes it for resume
	}
	if s.sleeping[sess.ID()] {
		s.sleepLocked(sess.ID())
		return
	}
	if on, ok := s.relaunch[sess.ID()]; ok {
		delete(s.relaunch, sess.ID())
		relaunch, remote = true, on
		return
	}
	os.RemoveAll(s.runtimeDir(sess.ID()))
	delete(s.records, sess.ID())
	delete(s.launched, sess.ID())
	delete(s.refusedSeen, sess.ID())
	if err := s.saveLocked(""); err != nil {
		s.log.Printf("sessions.json: %v", err)
	}
}

// notStoppingLocked refuses a launch's commit once the server stops;
// s.mu held.
func (s *Server) notStoppingLocked() *proto.Error {
	if s.stopping {
		return proto.Errorf(proto.ErrRefused, "server is stopping")
	}
	return nil
}

func (s *Server) stopSession(id string) (any, *proto.Error) {
	sess, perr := s.session(id)
	if perr != nil {
		return nil, perr
	}
	sess.Stop(StopGrace)
	return map[string]string{"status": sess.ExitStatus()}, nil
}

// serveAttach streams one session to a client: the snapshot, then output
// and resizes in order, while it reads the client's input and requests.
// The client leaving (DETACH, EOF, a dead socket) changes nothing else.
func (s *Server) serveAttach(c net.Conn, br *bufio.Reader, peerPID int, human bool) {
	c.SetReadDeadline(time.Now().Add(handshakeTimeout))
	var req proto.AttachRequest
	if err := proto.ReadLine(br, &req); err != nil {
		return
	}
	c.SetReadDeadline(time.Time{})
	// Attach types into a session as the human would: the human's alone,
	// like session.keys.
	who := s.whoFor(peerPID)
	if perr := mayCall(attachMethod, gateHuman, human, who); perr != nil {
		proto.WriteLine(c, proto.AttachReply{Error: perr})
		return
	}
	if _, _, ok := remoteID(req.Attach.Session); ok {
		s.serveRemoteAttach(c, br, req.Attach.Session)
		return
	}
	sess, perr := s.session(req.Attach.Session)
	if perr != nil {
		proto.WriteLine(c, proto.AttachReply{Error: perr})
		return
	}
	sub, err := sess.Attach()
	if err != nil {
		proto.WriteLine(c, proto.AttachReply{Error: sessionError(req.Attach.Session, err)})
		return
	}
	info := s.info(sess)
	if err := proto.WriteLine(c, proto.AttachReply{Attached: &info}); err != nil {
		sub.Detach()
		return
	}
	s.log.Printf("session %s: client attached", sess.ID())

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		defer c.Close()
		for {
			b, ok := sub.Next()
			if !ok {
				return
			}
			if _, err := c.Write(b); err != nil {
				return
			}
		}
	}()

	reason := "eof"
	var buf []byte
loop:
	for {
		typ, payload, err := proto.ReadFrame(br, buf)
		if err != nil {
			break
		}
		buf = payload
		switch typ {
		case proto.FrameInput:
			sess.HumanInput(payload, s.consoleTakeover(info, req.Attach.Console, who))
		case proto.FrameDigestReq:
			sess.RequestDigest(sub)
		case proto.FrameColorScheme:
			if len(payload) == 1 {
				scheme := emu.Scheme(payload[0])
				if err := sess.SetColorScheme(scheme); err == nil {
					s.mu.Lock()
					if s.scheme != scheme {
						s.log.Printf("session %s: console's colour scheme %d (1 dark, 2 light)", sess.ID(), scheme)
						s.scheme = scheme
						s.saveColorsLocked()
						s.themeChangedLocked()
					}
					s.mu.Unlock()
				}
			}
		case proto.FrameColors:
			if colors, ok := emu.DecodeColors(payload); ok {
				if err := sess.SetColors(colors); err == nil {
					s.mu.Lock()
					if s.colors != colors {
						s.colors = colors
						s.saveColorsLocked()
						s.themeChangedLocked()
					}
					s.mu.Unlock()
				}
			}
		case proto.FrameDetach:
			reason = "detach"
			break loop
		}
	}
	sub.Detach()
	<-writerDone
	s.log.Printf("session %s: client left (%s, %d resyncs)", sess.ID(), reason, sub.Resyncs())
}

func decodeParams(raw json.RawMessage, v any) *proto.Error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	return nil
}

// keychainStatus is server.keychain's answer: whether the server's
// sessions reach the login keychain.
func keychainStatus() proto.KeychainStatus {
	return keychain.Probe(runtime.GOOS, os.Getenv, keychain.Run)
}

// codeHostStatus runs the code-host checks (codehost.Checks) for the
// repos in use, in the server's own context: its environment, its
// session of the login keychain. That is the context its sessions' gh,
// az and git have, which a tm doctor run from another shell (over SSH)
// may not share.
func codeHostStatus() proto.CodeHostStatus {
	res := proto.CodeHostStatus{Checks: []proto.CodeHostCheck{}}
	for _, c := range codehost.Checks(codehost.SystemDeps(), project.CodeHosts()) {
		res.Checks = append(res.Checks, proto.CodeHostCheck{Name: c.Name, OK: c.OK, Detail: c.Detail})
	}
	return res
}
