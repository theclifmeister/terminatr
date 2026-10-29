package modsup

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/theclifmeister/terminatr/internal/modbus"
	"github.com/theclifmeister/terminatr/internal/modhost"
	"github.com/theclifmeister/terminatr/internal/modproto"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/plat/proc"
	"github.com/theclifmeister/terminatr/internal/plat/sandbox"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// startSandboxed is sandbox.Start; tests replace it.
var startSandboxed = sandbox.Start

// readRSS is proc.RSS; tests replace it.
var readRSS = proc.RSS

// helloFile is what a mod's last host said, in its cache folder: the
// bundle it ran and the events it handled, for the next lazy start.
const helloFile = "host.json"

type helloRecord struct {
	Bundle string   `json:"bundle"`
	Events []string `json:"events"`
	Tools  []string `json:"tools,omitempty"`
}

// runner supervises one mod: it starts its host, restarts it, and fails
// it, until stopped.
type runner struct {
	sup    *Supervisor
	spec   Spec
	ctx    context.Context
	cancel context.CancelFunc
	log    *modLog
	// done closes once run returned; a runner replacing this one waits
	// for it.
	done  chan struct{}
	after <-chan struct{}

	// wake starts an idle host now: an agent called one of its tools.
	wake chan struct{}

	mu   sync.Mutex
	st   Status
	host *host
	// client is the running host's connection, once it answered hello.
	client *modproto.Client
	// changed is closed, and replaced, whenever st.State or client
	// changes: a tool call waits on it.
	changed chan struct{}
	crashes []time.Time
	// streak counts the crashes since a host last ran CrashWindow: it
	// picks the backoff.
	streak int
}

func newRunner(s *Supervisor, sp Spec, after <-chan struct{}) *runner {
	r := &runner{sup: s, spec: sp, done: make(chan struct{}), after: after, wake: make(chan struct{}, 1),
		changed: make(chan struct{}), st: Status{ID: sp.ID, State: Starting, Since: time.Now()}}
	r.ctx, r.cancel = context.WithCancel(s.ctx)
	return r
}

// stop asks run to end: the host gets shutdown, then a kill.
func (r *runner) stop() { r.cancel() }

func (r *runner) status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.st
	st.Events = slices.Clone(st.Events)
	if r.host != nil {
		st.PID = r.host.pid
	}
	return st
}

func (r *runner) set(state State, why string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.st.State != state {
		r.st.State, r.st.Since = state, time.Now()
		r.signal()
	}
	if why != "" {
		r.st.Err = why
	}
	if state != Backoff {
		r.st.Next = time.Time{}
	}
}

// signal wakes the tool calls waiting on a change; r.mu held.
func (r *runner) signal() {
	close(r.changed)
	r.changed = make(chan struct{})
}

// setClient records the running host's connection, nil once it ends.
func (r *runner) setClient(c *modproto.Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.client = c
	r.signal()
}

// callTool runs one of the mod's agent tools in its host (call).
func (r *runner) callTool(ctx context.Context, call modproto.ToolCall) (res modproto.ToolResult, err error) {
	err = r.call(ctx, func(c *modproto.Client) (err error) {
		res, err = c.ToolCall(ctx, call)
		return err
	})
	return res, err
}

// call runs f with the host's connection: at once while it runs, after
// it starts when it is idle or starting. A mod in backoff or failed
// answers why, without waiting.
func (r *runner) call(ctx context.Context, f func(c *modproto.Client) error) error {
	for {
		r.mu.Lock()
		c, st, changed := r.client, r.st, r.changed
		r.mu.Unlock()
		switch {
		case c != nil && st.State == Running:
			err := f(c)
			if errors.Is(err, modproto.ErrClosed) {
				err = fmt.Errorf("mod %s's host ended during the call", r.spec.ID)
			}
			return err
		case st.State == Failed:
			return fmt.Errorf("mod %s failed: %s", r.spec.ID, st.Err)
		case st.State == Backoff:
			return fmt.Errorf("mod %s is restarting (%s)", r.spec.ID, st.Err)
		case st.State == Idle:
			select {
			case r.wake <- struct{}{}:
			default:
			}
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return fmt.Errorf("mod %s: %w", r.spec.ID, ctx.Err())
		case <-r.ctx.Done():
			return fmt.Errorf("mod %s stopped", r.spec.ID)
		}
	}
}

// idle waits for the first event that starts a lazy host, or for a tool
// call (woke). sub stays open on either; an error is sub's or r.ctx's.
func (r *runner) idle(sub *modbus.Sub) (woke bool, err error) {
	ctx, cancel := context.WithCancel(r.ctx)
	defer cancel()
	called := make(chan bool, 1)
	go func() {
		select {
		case <-r.wake:
			called <- true
			cancel()
		case <-ctx.Done():
			called <- false
		}
	}()
	err = sub.Wait(ctx)
	cancel()
	if woke = <-called; woke && r.ctx.Err() == nil {
		return true, nil
	}
	return false, err
}

func (r *runner) pid() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.host == nil {
		return 0
	}
	return r.host.pid
}

// sampled records the RSS of host pid, killing it over the ceiling.
func (r *runner) sampled(pid int, n uint64) {
	r.mu.Lock()
	h := r.host
	if h == nil || h.pid != pid {
		r.mu.Unlock()
		return
	}
	r.st.RSS = n
	r.mu.Unlock()
	if limit := r.sup.o.RSSLimit; n > limit {
		h.kill(fmt.Sprintf("its RSS of %d MiB passed the %d MiB ceiling", n>>20, limit>>20))
	}
}

// logf writes to the mod's log and the server's.
func (r *runner) logf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	r.log.Printf("supervisor: %s", msg)
	r.sup.o.Logf("mod %s: %s", r.spec.ID, msg)
}

// run starts the host (lazily unless eager) and restarts it until the
// runner is stopped or the mod fails.
func (r *runner) run(eager bool) {
	defer close(r.done)
	if r.after != nil {
		select {
		case <-r.after:
		case <-r.ctx.Done():
			return
		}
	}
	r.log = openLog(r.sup.logPath(r.spec.ID))
	defer r.log.Close()
	lazy := !eager
	for r.ctx.Err() == nil {
		var sub *modbus.Sub
		if lazy {
			lazy = false
			if events, tools := r.lazyEvents(); slices.ContainsFunc(events, wakes) || len(tools) > 0 {
				sub = r.subscribe(events)
				r.mu.Lock()
				r.st.Events = events
				r.mu.Unlock()
				r.set(Idle, "")
				if _, err := r.idle(sub); err != nil {
					sub.Close()
					if r.ctx.Err() != nil {
						return
					}
					sub = nil // replaced: start now
				}
			}
		}
		began := time.Now()
		code, why := r.once(sub)
		if r.ctx.Err() != nil {
			return
		}
		r.mu.Lock()
		if time.Since(began) >= r.sup.o.CrashWindow {
			r.streak = 0
		}
		r.st.Restarts++
		if code == modproto.ExitInterrupted {
			r.st.Interrupts++
			n := r.st.Interrupts
			r.mu.Unlock()
			if n >= InterruptLimit {
				r.fail(fmt.Sprintf("%d handlers in a row ran over the CPU budget", n))
				return
			}
			r.logf("%s; restarting", why)
			continue
		}
		now := time.Now()
		r.crashes = append(r.crashes, now)
		r.crashes = slices.DeleteFunc(r.crashes, func(t time.Time) bool { return now.Sub(t) > r.sup.o.CrashWindow })
		n := len(r.crashes)
		r.st.Crashes = n
		r.streak++
		wait := r.sup.o.Backoff[min(r.streak, len(r.sup.o.Backoff))-1]
		r.mu.Unlock()
		if n >= CrashLimit {
			r.fail(fmt.Sprintf("%s; %d crashes within %s", why, n, r.sup.o.CrashWindow))
			return
		}
		r.mu.Lock()
		r.st.Next = now.Add(wait)
		r.mu.Unlock()
		r.set(Backoff, why)
		r.logf("%s; restarting in %s", why, wait)
		select {
		case <-r.ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// fail leaves the mod failed until it is changed or restarted.
func (r *runner) fail(why string) {
	r.set(Failed, why)
	r.logf("failed: %s", why)
	<-r.ctx.Done()
}

// subscribe opens the mod's event queue, letting events through.
func (r *runner) subscribe(events []string) *modbus.Sub {
	var projects func(string) bool
	if len(r.spec.Projects) > 0 {
		projects = func(slug string) bool { return slices.Contains(r.spec.Projects, slug) }
	}
	sub := r.sup.o.Subscribe(r.spec.ID, r.spec.Grants, projects)
	sub.SetEvents(events)
	return sub
}

// wakes says whether event name can start a lazy host: mod.ready is
// given to every host as it starts, never queued.
func wakes(name string) bool { return name != modbus.ModReady }

// lazyEvents are the events and agent tools the last host handled, when
// it ran the bundle a start now would run: the host then waits for the
// first of those events, or a call of one of its tools. A mod that
// handled none of either but mod.ready (it runs on timers) starts at
// once.
func (r *runner) lazyEvents() (events, tools []string) {
	b, err := os.ReadFile(filepath.Join(r.sup.cacheDir(r.spec.ID), helloFile))
	if err != nil {
		return nil, nil
	}
	var rec helloRecord
	if json.Unmarshal(b, &rec) != nil {
		return nil, nil
	}
	bundle, err := r.prepare()
	if err != nil || filepath.Base(bundle) != rec.Bundle {
		return nil, nil
	}
	return rec.Events, rec.Tools
}

// prepare returns the bundle to run: a bundled mod's precompiled script,
// written to its cache folder once, or the user mod's main built there
// (cached while its files are unchanged).
func (r *runner) prepare() (string, error) {
	dir := r.sup.cacheDir(r.spec.ID)
	if r.spec.Bundle == nil {
		return modhost.Build(r.spec.Entry, dir)
	}
	sum := sha256.Sum256(r.spec.Bundle)
	path := filepath.Join(dir, hex.EncodeToString(sum[:16])+".js")
	if b, err := os.ReadFile(path); err == nil && bytes.Equal(b, r.spec.Bundle) {
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return path, writeAtomic(path, r.spec.Bundle)
}

// host is one running `tm mod host`.
type host struct {
	pid  int
	proc *os.Process

	mu     sync.Mutex
	reason string // why the supervisor killed it
}

// kill ends the host at once; the first reason is kept.
func (h *host) kill(reason string) {
	h.mu.Lock()
	if h.reason == "" {
		h.reason = reason
	}
	h.mu.Unlock()
	_ = h.proc.Kill()
}

func (h *host) why() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.reason
}

// hostEvents are the events a host handles: its hello's, and those it
// subscribed to since. sub is the queue they filter, once open. A
// subscribe arrives on the client's read loop, maybe before once has
// recorded the hello: mu guards all but r and bundle.
type hostEvents struct {
	r      *runner
	bundle string

	mu     sync.Mutex
	tools  []string // the tools its hello said it handles
	events []string
	sub    *modbus.Sub
}

// add merges names in, filters the queue with them, and records them in
// the status and the cache folder, in order with any other add.
func (e *hostEvents) add(names []string, sub *modbus.Sub) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.addLocked(names, sub)
}

// hello records the host's hello: its tools, and its events as add does.
func (e *hostEvents) hello(tools, names []string, sub *modbus.Sub) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tools = tools
	return e.addLocked(names, sub)
}

func (e *hostEvents) addLocked(names []string, sub *modbus.Sub) []string {
	for _, n := range names {
		if !slices.Contains(e.events, n) {
			e.events = append(e.events, n)
		}
	}
	slices.Sort(e.events)
	if e.events == nil {
		e.events = []string{}
	}
	if sub != nil {
		e.sub = sub
	}
	if e.sub != nil {
		e.sub.SetEvents(e.events)
	}
	e.r.saveHello(e.bundle, e.events, e.tools)
	e.r.mu.Lock()
	e.r.st.Events = slices.Clone(e.events)
	e.r.mu.Unlock()
	return slices.Clone(e.events)
}

// once runs one host until it exits or the runner stops, delivering sub's
// events (opened after hello when nil). It returns the host's exit code
// and why it ended.
func (r *runner) once(sub *modbus.Sub) (code int, why string) {
	defer func() {
		if sub != nil {
			sub.Close()
		}
	}()
	r.set(Starting, "")
	started := time.Now()
	bundle, err := r.prepare()
	if err != nil {
		return modproto.ExitLoad, fmt.Sprintf("bundling %s: %v", r.spec.Entry, err)
	}
	dir := r.sup.stateDir(r.spec.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return modproto.ExitLoad, fmt.Sprintf("its data folder: %v", err)
	}
	cmd := exec.Command(r.sup.o.Bin, "mod", "host", "--budget", r.sup.o.Budget.String(), bundle)
	cmd.Dir, cmd.Env = dir, hostEnv(os.Getenv)
	stdin, err1 := cmd.StdinPipe()
	stdout, err2 := cmd.StdoutPipe()
	stderr, err3 := cmd.StderrPipe()
	if err := errors.Join(err1, err2, err3); err != nil {
		return modproto.ExitLoad, fmt.Sprintf("starting its host: %v", err)
	}
	sb, mode, err := r.start(cmd, bundle)
	if err != nil {
		return modproto.ExitLoad, fmt.Sprintf("starting its host: %v", err)
	}
	h := &host{pid: cmd.Process.Pid, proc: cmd.Process}
	r.mu.Lock()
	r.host, r.st.RSS, r.st.Sandbox = h, 0, mode
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.host = nil
		r.mu.Unlock()
	}()

	ev := hostEvents{r: r, bundle: bundle}
	var cl atomic.Pointer[modproto.Client]
	// The host's calls end with it (or the runner): their answers have
	// nowhere to go.
	cctx, ccancel := context.WithCancel(r.ctx)
	defer ccancel()
	onRequest := func(m *modproto.Message) {
		c := cl.Load()
		switch {
		case c == nil:
		case r.sup.o.Broker != nil:
			r.sup.o.Broker(cctx, r.spec.ID, c.Conn(), m)
		default:
			_ = c.Conn().ReplyError(*m.ID, modproto.CodeMethodNotFound, "the $ API isn't served yet: "+m.Method)
		}
	}
	onNotify := func(m *modproto.Message) { r.notified(m, &ev) }
	client := modproto.NewClient(stdout, stdin, onRequest, onNotify)
	cl.Store(client)
	go func() {
		<-client.Done()
		ccancel()
	}()
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 4096), 64<<10)
		for sc.Scan() {
			r.log.Printf("stderr: %s", clip(sc.Text()))
		}
		_, _ = io.Copy(io.Discard, stderr)
	}()
	exited := make(chan struct{})
	go func() {
		// Wait closes the pipes: read what the host wrote first.
		<-client.Done()
		<-stderrDone
		_ = cmd.Wait()
		sb.Release()
		close(exited)
	}()
	end := func() (int, string) {
		<-exited
		code := cmd.ProcessState.ExitCode()
		if why := h.why(); why != "" {
			return code, "killed: " + why
		}
		return code, exitWhy(code)
	}

	hctx, hcancel := context.WithTimeout(r.ctx, HelloLimit)
	res, err := client.Hello(hctx, modproto.Hello{API: r.sup.o.API, TM: r.sup.o.TMVersion, Mod: r.spec.ID,
		Grants: r.spec.Grants, Settings: settingsJSON(r.spec.Settings), Projects: projectSettingsJSON(r.spec.ProjectSettings)})
	hcancel()
	if err != nil {
		if r.ctx.Err() != nil {
			r.shutdown(client, h, exited)
			return 0, "stopped"
		}
		var perr *modproto.Error
		if errors.As(err, &perr) {
			r.log.Printf("supervisor: hello: %v", perr)
		} else if !errors.Is(err, modproto.ErrClosed) {
			h.kill(fmt.Sprintf("no hello answer within %s", HelloLimit))
		}
		return end()
	}
	if sub == nil {
		sub = r.subscribe(nil)
	}
	events := ev.hello(res.Tools, res.Events, sub)
	r.setClient(client)
	defer r.setClient(nil)
	r.set(Running, "")
	r.logf("started: pid %d in %s (script %.0f ms); handles %s", h.pid, time.Since(started).Round(time.Millisecond),
		res.LoadMS, cmp.Or(strings.Join(events, ", "), "no events"))

	dctx, dcancel := context.WithCancel(r.ctx)
	defer dcancel()
	delivered := make(chan struct{})
	go func() {
		defer close(delivered)
		_ = modbus.Deliver(dctx, sub, r.handler(client), r.log.Printf)
	}()
	go r.ping(dctx, client, h)

	select {
	case <-exited:
	case <-r.ctx.Done():
		dcancel()
		<-delivered
		r.shutdown(client, h, exited)
		return 0, "stopped"
	}
	dcancel()
	<-delivered
	return end()
}

// start starts cmd, a host running bundle, in the OS sandbox (mods
// design §7): it may read the tm binary and the bundle, nothing else, and
// has no network. Where the sandbox can't apply (an old kernel, a server
// that already runs in a sandbox) the host starts without it, said in the
// mod's log once, and mode says why. On success the caller releases sb
// once cmd has exited.
func (r *runner) start(cmd *exec.Cmd, bundle string) (sb *sandbox.Proc, mode string, err error) {
	sb, err = startSandboxed(cmd, sandbox.Policy{Exe: r.sup.o.Bin, Read: []string{bundle}})
	if err == nil {
		return sb, sb.Mode, nil
	}
	if !errors.Is(err, sandbox.ErrUnavailable) {
		return nil, "", err
	}
	mode = "none: " + err.Error()
	r.mu.Lock()
	warned := r.st.Sandbox == mode
	r.mu.Unlock()
	if !warned {
		r.logf("runs without the OS sandbox: %v", err)
	}
	return nil, mode, cmd.Start()
}

// handler gives one event to the host; a handler that ran clears the run
// of interrupts.
func (r *runner) handler(c *modproto.Client) modbus.Handler {
	return func(ctx context.Context, ev modbus.Event) error {
		data, err := json.Marshal(ev.Data)
		if err != nil {
			return err
		}
		res, err := c.Event(ctx, modproto.Event{Name: ev.Name, Project: ev.Project, Seq: ev.Seq, Time: ev.Time,
			Dropped: ev.Dropped, Data: data})
		if errors.Is(err, modproto.ErrClosed) {
			return fmt.Errorf("%w: %v", modbus.ErrGone, err)
		}
		if err == nil && ev.Name != modbus.ModReady && res.Handlers > 0 && res.Failed == 0 {
			r.mu.Lock()
			r.st.Interrupts = 0
			r.mu.Unlock()
		}
		return err
	}
}

// ping asks the host for stats every Ping; one that doesn't answer within
// PingLimit is stuck outside its script, and is killed.
func (r *runner) ping(ctx context.Context, c *modproto.Client, h *host) {
	t := time.NewTicker(r.sup.o.Ping)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		pctx, cancel := context.WithTimeout(ctx, r.sup.o.PingLimit)
		_, err := c.Call(pctx, modproto.MethodStats, nil)
		cancel()
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			h.kill(fmt.Sprintf("no stats answer within %s", r.sup.o.PingLimit))
			return
		}
	}
}

// shutdown asks the host to exit, killing it after StopGrace.
func (r *runner) shutdown(c *modproto.Client, h *host, exited <-chan struct{}) {
	grace := r.sup.o.StopGrace
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	_ = c.Shutdown(ctx)
	select {
	case <-exited:
	case <-ctx.Done():
		h.kill("no exit after shutdown")
		<-exited
	}
	r.log.Printf("supervisor: stopped")
}

// notified handles a host's notification, on its read loop.
func (r *runner) notified(m *modproto.Message, ev *hostEvents) {
	switch m.Method {
	case modproto.MethodLog:
		var l modproto.Log
		if json.Unmarshal(m.Params, &l) == nil {
			r.log.Printf("%s: %s", cleanLevel(l.Level), clip(l.Text))
		}
	case modproto.MethodHandlerFailed:
		var f modproto.HandlerFailed
		if json.Unmarshal(m.Params, &f) == nil {
			r.log.Printf("handler %s %s after %.0f ms: %s", clip(f.Where), clip(f.Kind), f.AfterMS, clip(f.Error))
			if f.Stack != "" {
				// Each line of the stack its own, indented, so none
				// passes for a line of tm's.
				for _, line := range strings.Split(textsafe.Cut(f.Stack, maxText), "\n") {
					r.log.Printf("  %s", textsafe.Escape(line))
				}
			}
		}
	case modproto.MethodSubscribe:
		var s modproto.Subscribe
		if json.Unmarshal(m.Params, &s) == nil {
			ev.add(s.Events, nil)
		}
	}
}

// saveHello records the events and tools the host for bundle handles.
func (r *runner) saveHello(bundle string, events, tools []string) {
	b, _ := json.Marshal(helloRecord{Bundle: filepath.Base(bundle), Events: events, Tools: tools})
	if err := writeAtomic(filepath.Join(r.sup.cacheDir(r.spec.ID), helloFile), b); err != nil {
		r.log.Printf("supervisor: %s: %v", helloFile, err)
	}
}

// exitWhy says what a host's exit code means (modproto.Exit*).
func exitWhy(code int) string {
	switch code {
	case modproto.ExitOK:
		return "its host exited unasked"
	case modproto.ExitLoad:
		return "its script failed to load (exit 1)"
	case modproto.ExitUsage:
		return "its host refused the command line or hello (exit 2)"
	case modproto.ExitInterrupted:
		return "a handler ran over the CPU budget (exit 3)"
	case -1:
		return "its host was killed"
	}
	return fmt.Sprintf("its host exited with %d", code)
}

func settingsJSON(s map[string]any) json.RawMessage {
	if len(s) == 0 {
		return nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		return nil
	}
	return b
}

func projectSettingsJSON(ps map[string]map[string]any) map[string]json.RawMessage {
	if len(ps) == 0 {
		return nil
	}
	out := make(map[string]json.RawMessage, len(ps))
	for slug, s := range ps {
		if b, err := json.Marshal(s); err == nil {
			out[slug] = b
		}
	}
	return out
}

func cleanLevel(l string) string {
	switch l {
	case "info", "warn", "error":
		return l
	}
	return "info"
}

// maxText caps a line a host gives the log.
const maxText = 8 << 10

// clip is text a host gives the log as one line of at most maxText
// bytes: what would break the line or reach a terminal as other than
// text escaped (textsafe.Escape), so a mod's line can't pass for tm's
// nor move the cursor of whoever reads the log.
func clip(s string) string {
	return textsafe.Escape(textsafe.Cut(s, maxText))
}

// writeAtomic replaces path with b, mode 0600, making its folder first.
func writeAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return fsx.WriteAtomic(path, b, 0o600)
}
