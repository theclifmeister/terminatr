package modhost

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/dop251/goja"

	"github.com/theclifmeister/terminatr/internal/modproto"
)

// server is the test's side of a host running in-process.
type server struct {
	t    *testing.T
	conn *modproto.Conn
	in   *io.PipeWriter
	msgs chan *modproto.Message
	code chan int
	id   int64
	// held are messages until skipped, for later next and until calls.
	held []*modproto.Message
}

// testBind gives scripts $.call, the generic channel the T251 bindings
// are built on.
func testBind(h *Host) error {
	return h.Dollar().Set("call", func(method string, params goja.Value) *goja.Promise { return h.Call(method, params) })
}

func startHost(t *testing.T, script string, budget time.Duration) *server {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	s := &server{t: t, conn: modproto.NewConn(outR, inW), in: inW, msgs: make(chan *modproto.Message, 100), code: make(chan int, 1)}
	go func() {
		s.code <- Run(Options{Name: "main.js", Script: script, Budget: budget, In: inR, Out: outW, Bind: testBind})
		outW.Close()
	}()
	go func() {
		for {
			m, err := s.conn.Read()
			if err != nil {
				close(s.msgs)
				return
			}
			s.msgs <- m
		}
	}()
	t.Cleanup(func() { inW.Close() })
	return s
}

func (s *server) request(method string, params any) int64 {
	s.t.Helper()
	s.id++
	if err := s.conn.Request(s.id, method, params); err != nil {
		s.t.Fatal(err)
	}
	return s.id
}

func (s *server) hello() modproto.HelloResult {
	s.t.Helper()
	id := s.request(modproto.MethodHello, modproto.Hello{Protocol: modproto.Protocol, API: "1.0", Mod: "test", Grants: []string{"project_read"}})
	m := s.reply(id)
	if m.Error != nil {
		s.t.Fatalf("hello: %v", m.Error)
	}
	var r modproto.HelloResult
	_ = json.Unmarshal(m.Result, &r)
	return r
}

func (s *server) event(name string, payload any) int64 {
	s.t.Helper()
	b, _ := json.Marshal(payload)
	return s.request(modproto.MethodEvent, modproto.Event{Name: name, Payload: b})
}

// next returns the next message the host sends.
func (s *server) next() *modproto.Message {
	s.t.Helper()
	if len(s.held) > 0 {
		m := s.held[0]
		s.held = s.held[1:]
		return m
	}
	select {
	case m, ok := <-s.msgs:
		if !ok {
			s.t.Fatal("the host closed stdout")
		}
		return m
	case <-time.After(5 * time.Second):
		s.t.Fatal("no message from the host")
	}
	return nil
}

// until returns the first message matching ok, skipping others.
func (s *server) until(what string, ok func(*modproto.Message) bool) *modproto.Message {
	s.t.Helper()
	for i, m := range s.held {
		if ok(m) {
			s.held = append(s.held[:i:i], s.held[i+1:]...)
			return m
		}
	}
	for {
		select {
		case m, open := <-s.msgs:
			if !open {
				s.t.Fatalf("the host closed stdout waiting for %s", what)
			}
			if ok(m) {
				return m
			}
			s.held = append(s.held, m)
		case <-time.After(5 * time.Second):
			s.t.Fatalf("waiting for %s", what)
		}
	}
}

func (s *server) reply(id int64) *modproto.Message {
	s.t.Helper()
	return s.until(fmt.Sprintf("reply %d", id), func(m *modproto.Message) bool { return m.IsResponse() && *m.ID == id })
}

func (s *server) method(name string) *modproto.Message {
	s.t.Helper()
	return s.until(name, func(m *modproto.Message) bool { return m.Method == name })
}

func (s *server) failed() modproto.HandlerFailed {
	s.t.Helper()
	var f modproto.HandlerFailed
	_ = json.Unmarshal(s.method(modproto.MethodHandlerFailed).Params, &f)
	return f
}

func (s *server) eventResult(id int64) modproto.EventResult {
	s.t.Helper()
	m := s.reply(id)
	if m.Error != nil {
		s.t.Fatalf("event: %v", m.Error)
	}
	var r modproto.EventResult
	_ = json.Unmarshal(m.Result, &r)
	return r
}

func (s *server) exit() int {
	s.t.Helper()
	select {
	case c := <-s.code:
		return c
	case <-time.After(5 * time.Second):
		s.t.Fatal("the host didn't exit")
	}
	return -1
}

func logText(m *modproto.Message) string {
	var l modproto.Log
	_ = json.Unmarshal(m.Params, &l)
	return l.Text
}

func TestHelloRunsTheScriptAndListsEvents(t *testing.T) {
	s := startHost(t, `
		$.log("loaded", {grants: 1}, 2);
		$.on("task.changed", () => {});
		$.on("clock.minute", () => {});
		$.on("task.changed", () => {});
	`, time.Second)
	r := s.hello()
	if r.Protocol != modproto.Protocol || r.Runtime != "goja" || strings.Join(r.Events, ",") != "clock.minute,task.changed" {
		t.Fatalf("hello result %+v", r)
	}
}

func TestNothingRunsBeforeHello(t *testing.T) {
	s := startHost(t, `$.log("loaded")`, time.Second)
	id := s.event("x", nil)
	m := s.next()
	if !m.IsResponse() || *m.ID != id || m.Error == nil || m.Error.Code != modproto.CodeNotReady {
		t.Fatalf("event before hello: %+v", m)
	}
	s.hello()
	if got := logText(s.method(modproto.MethodLog)); got != "loaded" {
		t.Fatalf("log %q", got)
	}
}

func TestLogFormatsValues(t *testing.T) {
	s := startHost(t, `$.log("n", 1, {a: [1, "b"]}, null)`, time.Second)
	s.hello()
	if got := logText(s.method(modproto.MethodLog)); got != `n 1 {"a":[1,"b"]} null` {
		t.Fatalf("log %q", got)
	}
}

func TestEventPayloadIsPlainJS(t *testing.T) {
	s := startHost(t, `
		$.on("task.changed", (ev) => {
			$.log(Array.isArray(ev.steps), Object.keys(ev).join(","), ev.steps.map((x) => x * 2).join(","));
		});
	`, time.Second)
	s.hello()
	id := s.event("task.changed", map[string]any{"id": "T1", "steps": []int{1, 2}})
	if got := logText(s.method(modproto.MethodLog)); got != "true id,steps 2,4" {
		t.Fatalf("log %q", got)
	}
	if r := s.eventResult(id); r.Handlers != 1 || r.Failed != 0 {
		t.Fatalf("result %+v", r)
	}
	// An event nobody handles is answered at once.
	if r := s.eventResult(s.event("nobody", nil)); r.Handlers != 0 {
		t.Fatalf("result %+v", r)
	}
}

func TestThrowIsReportedAndTheHostLivesOn(t *testing.T) {
	s := startHost(t, `
		$.on("boom", () => { throw new Error("boom"); });
		$.on("boom", () => { $.log("second ran"); });
		$.on("ok", () => $.log("ok"));
	`, time.Second)
	s.hello()
	id := s.event("boom", nil)
	f := s.failed()
	if f.Where != "boom" || f.Kind != modproto.KindException || f.Error != "Error: boom" || !strings.Contains(f.Stack, "main.js") {
		t.Fatalf("failed %+v", f)
	}
	if got := logText(s.method(modproto.MethodLog)); got != "second ran" {
		t.Fatalf("log %q", got)
	}
	if r := s.eventResult(id); r.Handlers != 2 || r.Failed != 1 {
		t.Fatalf("result %+v", r)
	}
	s.event("ok", nil)
	if got := logText(s.method(modproto.MethodLog)); got != "ok" {
		t.Fatalf("log %q", got)
	}
}

func TestAsyncHandlerAwaitsTheBroker(t *testing.T) {
	s := startHost(t, `
		$.on("task.changed", async (ev) => {
			const p = await $.call("project.get", {slug: ev.project});
			$.log("tasks", p.tasks.length);
			try {
				await $.call("inbox.raise", {});
			} catch (e) {
				$.log("refused", e.message, e.code, e instanceof Error);
			}
		});
	`, time.Second)
	s.hello()
	ev := s.event("task.changed", map[string]any{"project": "tm"})
	call := s.method("project.get")
	if !call.IsRequest() || string(call.Params) != `{"slug":"tm"}` {
		t.Fatalf("call %+v", call)
	}
	// The event isn't answered while its handler waits.
	_ = s.conn.Reply(*call.ID, map[string]any{"tasks": []int{1, 2, 3}})
	if got := logText(s.method(modproto.MethodLog)); got != "tasks 3" {
		t.Fatalf("log %q", got)
	}
	call = s.method("inbox.raise")
	_ = s.conn.ReplyError(*call.ID, -32010, "no inbox grant")
	if got := logText(s.method(modproto.MethodLog)); got != "refused no inbox grant -32010 true" {
		t.Fatalf("log %q", got)
	}
	if r := s.eventResult(ev); r.Handlers != 1 || r.Failed != 0 {
		t.Fatalf("result %+v", r)
	}
}

func TestRejectedHandlerIsCountedOnce(t *testing.T) {
	s := startHost(t, `
		$.on("x", async () => { await $.call("a"); throw new Error("late"); });
		$.on("ok", () => {});
	`, time.Second)
	s.hello()
	ev := s.event("x", nil)
	call := s.method("a")
	_ = s.conn.Reply(*call.ID, nil)
	f := s.failed()
	if f.Kind != modproto.KindRejected || f.Error != "Error: late" || f.Where != "x" {
		t.Fatalf("failed %+v", f)
	}
	if r := s.eventResult(ev); r.Failed != 1 {
		t.Fatalf("result %+v", r)
	}
	// Not reported a second time as an unhandled rejection.
	ok := s.event("ok", nil)
	if m := s.next(); !m.IsResponse() || *m.ID != ok {
		t.Fatalf("expected only the ok reply, got %+v", m)
	}
}

func TestUnhandledRejectionInATimer(t *testing.T) {
	s := startHost(t, `setTimeout(() => { Promise.reject(new Error("lost")); }, 1);`, time.Second)
	s.hello()
	f := s.failed()
	if f.Where != "timer" || f.Kind != modproto.KindRejected || f.Error != "Error: lost" {
		t.Fatalf("failed %+v", f)
	}
}

func TestTimers(t *testing.T) {
	s := startHost(t, `
		let n = 0;
		const iv = setInterval((step) => {
			n += step;
			if (n === 3) { clearInterval(iv); $.log("interval done"); }
		}, 1, 1);
		const never = setTimeout(() => $.log("cleared timer ran"), 1);
		clearTimeout(never);
		setTimeout(() => $.log("timeout", n >= 0), 0);
	`, time.Second)
	s.hello()
	var got []string
	for len(got) < 2 {
		got = append(got, logText(s.method(modproto.MethodLog)))
	}
	if strings.Join(got, "|") != "timeout true|interval done" {
		t.Fatalf("logs %q", got)
	}
	id := s.request(modproto.MethodStats, nil)
	var st modproto.Stats
	_ = json.Unmarshal(s.reply(id).Result, &st)
	if st.Timers != 0 || st.Pending != 0 || st.HeapInuse == 0 {
		t.Fatalf("stats %+v", st)
	}
}

func TestSubscribeAfterHello(t *testing.T) {
	s := startHost(t, `$.on("first", () => { $.on("later", () => {}); $.on("later", () => {}); });`, time.Second)
	s.hello()
	s.event("first", nil)
	var sub modproto.Subscribe
	_ = json.Unmarshal(s.method(modproto.MethodSubscribe).Params, &sub)
	if strings.Join(sub.Events, ",") != "later" {
		t.Fatalf("subscribe %+v", sub)
	}
}

func TestOnNeedsAFunction(t *testing.T) {
	s := startHost(t, `$.on("x", 1)`, time.Second)
	id := s.request(modproto.MethodHello, modproto.Hello{Protocol: 1})
	m := s.reply(id)
	if m.Error == nil || m.Error.Code != modproto.CodeLoad || !strings.Contains(m.Error.Message, "TypeError") {
		t.Fatalf("hello %+v", m)
	}
	if c := s.exit(); c != modproto.ExitLoad {
		t.Fatalf("exit %d", c)
	}
}

func TestNoAmbientIO(t *testing.T) {
	s := startHost(t, `$.log(typeof require, typeof process, typeof fetch, typeof console, typeof setTimeout)`, time.Second)
	s.hello()
	if got := logText(s.method(modproto.MethodLog)); got != "undefined undefined undefined undefined function" {
		t.Fatalf("globals %q", got)
	}
}

// The budget: every slice, and the host exits 3 after any interrupt.

func TestSpinInAHandlerExits3(t *testing.T) {
	s := startHost(t, `$.on("spin", () => { for (;;) {} });`, 50*time.Millisecond)
	s.hello()
	s.event("spin", nil)
	f := s.failed()
	if f.Kind != modproto.KindInterrupted || f.Where != "spin" || f.AfterMS < 40 {
		t.Fatalf("failed %+v", f)
	}
	if c := s.exit(); c != modproto.ExitInterrupted {
		t.Fatalf("exit %d", c)
	}
}

func TestSpinAfterAnAwaitExits3(t *testing.T) {
	s := startHost(t, `$.on("spin", async () => { await $.call("a"); for (;;) {} });`, 50*time.Millisecond)
	s.hello()
	s.event("spin", nil)
	call := s.method("a")
	_ = s.conn.Reply(*call.ID, true)
	f := s.failed()
	if f.Kind != modproto.KindInterrupted || f.Where != "reply:a" {
		t.Fatalf("failed %+v", f)
	}
	if c := s.exit(); c != modproto.ExitInterrupted {
		t.Fatalf("exit %d", c)
	}
}

func TestSpinInATimerExits3(t *testing.T) {
	s := startHost(t, `setTimeout(() => { for (;;) {} }, 1);`, 50*time.Millisecond)
	s.hello()
	if f := s.failed(); f.Kind != modproto.KindInterrupted || f.Where != "timer" {
		t.Fatalf("failed %+v", f)
	}
	if c := s.exit(); c != modproto.ExitInterrupted {
		t.Fatalf("exit %d", c)
	}
}

func TestSpinAfterATimerSettlesAPromiseExits3(t *testing.T) {
	s := startHost(t, `
		const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
		$.on("spin", async () => { await sleep(1); for (;;) {} });
	`, 50*time.Millisecond)
	s.hello()
	s.event("spin", nil)
	if f := s.failed(); f.Kind != modproto.KindInterrupted || f.Where != "timer" {
		t.Fatalf("failed %+v", f)
	}
	if c := s.exit(); c != modproto.ExitInterrupted {
		t.Fatalf("exit %d", c)
	}
}

func TestSpinWhileLoadingExits3(t *testing.T) {
	s := startHost(t, `for (;;) {}`, 50*time.Millisecond)
	s.request(modproto.MethodHello, modproto.Hello{Protocol: 1})
	if f := s.failed(); f.Kind != modproto.KindInterrupted || f.Where != "load" {
		t.Fatalf("failed %+v", f)
	}
	if c := s.exit(); c != modproto.ExitInterrupted {
		t.Fatalf("exit %d", c)
	}
}

func TestShutdownAndEOF(t *testing.T) {
	s := startHost(t, `setInterval(() => {}, 1)`, time.Second)
	s.hello()
	id := s.request(modproto.MethodShutdown, nil)
	if m := s.reply(id); m.Error != nil {
		t.Fatal(m.Error)
	}
	if c := s.exit(); c != modproto.ExitOK {
		t.Fatalf("exit %d", c)
	}

	s = startHost(t, ``, time.Second)
	s.hello()
	s.in.Close()
	if c := s.exit(); c != modproto.ExitOK {
		t.Fatalf("exit %d", c)
	}
}

func TestProtocolErrors(t *testing.T) {
	s := startHost(t, ``, time.Second)
	_, _ = s.in.Write([]byte("not json\n"))
	if m := s.method(modproto.MethodLog); !strings.Contains(logText(m), "bad frame") {
		t.Fatalf("log %q", logText(m))
	}
	id := s.request("nope", nil)
	if m := s.reply(id); m.Error == nil || m.Error.Code != modproto.CodeMethodNotFound {
		t.Fatalf("reply %+v", m)
	}
	s.hello()
	id = s.request(modproto.MethodHello, modproto.Hello{Protocol: 1})
	if m := s.reply(id); m.Error == nil || m.Error.Code != modproto.CodeNotReady {
		t.Fatalf("second hello %+v", m)
	}
	_ = s.conn.Reply(99, true)
	if m := s.method(modproto.MethodLog); !strings.Contains(logText(m), "unknown call 99") {
		t.Fatalf("log %q", logText(m))
	}
}

func TestNewerProtocolIsRefused(t *testing.T) {
	s := startHost(t, `$.log("ran")`, time.Second)
	id := s.request(modproto.MethodHello, modproto.Hello{Protocol: modproto.Protocol + 1})
	if m := s.reply(id); m.Error == nil || !strings.Contains(m.Error.Message, "protocol") {
		t.Fatalf("hello %+v", m)
	}
	if c := s.exit(); c != modproto.ExitUsage {
		t.Fatalf("exit %d", c)
	}
}

func TestCompileErrorExits1(t *testing.T) {
	if c := Run(Options{Name: "x.js", Script: "function (", In: strings.NewReader(""), Out: io.Discard}); c != modproto.ExitLoad {
		t.Fatalf("exit %d", c)
	}
}
