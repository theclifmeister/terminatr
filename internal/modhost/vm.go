package modhost

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/dop251/goja"

	"github.com/theclifmeister/terminatr/internal/modproto"
)

// setupVM gives the VM its globals: $ with on, log, tool.handle and
// codehost.handle, the
// timers, and nothing else (no require, console, fetch or process). Code
// can't be made from strings (noCodeFromStrings).
func (h *Host) setupVM() {
	vm := h.vm
	noCodeFromStrings(vm)
	j := vm.Get("JSON").ToObject(vm)
	h.parse, _ = goja.AssertFunction(j.Get("parse"))
	h.strfy, _ = goja.AssertFunction(j.Get("stringify"))
	h.errCtr = vm.Get("Error")

	vm.SetPromiseRejectionTracker(func(p *goja.Promise, op goja.PromiseRejectionOperation) {
		if op == goja.PromiseRejectionReject {
			h.unhandled[p] = struct{}{}
		} else {
			delete(h.unhandled, p)
		}
	})

	h.dollar = vm.NewObject()
	_ = h.dollar.Set("on", h.jsOn)
	_ = h.dollar.Set("log", h.jsLog)
	tool := vm.NewObject()
	_ = tool.Set("handle", h.jsToolHandle)
	_ = h.dollar.Set("tool", tool)
	ch := vm.NewObject()
	_ = ch.Set("handle", h.jsCodeHostHandle)
	_ = h.dollar.Set("codehost", ch)
	_ = vm.Set("$", h.dollar)

	_ = vm.Set("setTimeout", func(c goja.FunctionCall) goja.Value { return h.jsTimer(c, false) })
	_ = vm.Set("setInterval", func(c goja.FunctionCall) goja.Value { return h.jsTimer(c, true) })
	_ = vm.Set("clearTimeout", h.jsClearTimer)
	_ = vm.Set("clearInterval", h.jsClearTimer)
}

// ErrCodeFromStrings is what eval's stand-ins throw.
const ErrCodeFromStrings = "code from strings is off in mods: no eval, no Function constructor"

// noCodeFromStrings takes eval away and makes every function constructor
// (Function, and those of async, generator and async generator
// functions, as reached through their prototypes' constructor) throw.
// None of them reaches anything outside the VM; what they would let a
// mod do is run text it was given as code, with its grants: a report's
// words, a fetched page. That is the confused deputy the broker's checks
// can't see, and no mod needs it (a bundle has its code ahead of time).
// instanceof Function still works: each stand-in keeps the prototype.
func noCodeFromStrings(vm *goja.Runtime) {
	_ = vm.GlobalObject().Delete("eval")
	refuse := func(goja.FunctionCall) goja.Value { panic(vm.NewTypeError(ErrCodeFromStrings)) }
	for i, src := range []string{"(function(){})", "(async function(){})", "(function*(){})", "(async function*(){})"} {
		v, err := vm.RunString(src)
		if err != nil {
			continue // a kind this goja doesn't have
		}
		proto := v.ToObject(vm).Prototype()
		if proto == nil {
			continue
		}
		stand := vm.ToValue(refuse).ToObject(vm)
		_ = stand.DefineDataProperty("prototype", proto, goja.FLAG_FALSE, goja.FLAG_FALSE, goja.FLAG_FALSE)
		_ = proto.DefineDataProperty("constructor", stand, goja.FLAG_FALSE, goja.FLAG_FALSE, goja.FLAG_FALSE)
		if i == 0 {
			_ = vm.GlobalObject().DefineDataProperty("Function", stand, goja.FLAG_FALSE, goja.FLAG_FALSE, goja.FLAG_FALSE)
		}
	}
}

// $.on(name, fn) adds a handler for an event.
func (h *Host) jsOn(c goja.FunctionCall) goja.Value {
	name := c.Argument(0)
	fn, ok := goja.AssertFunction(c.Argument(1))
	if goja.IsUndefined(name) || name.String() == "" || !ok {
		panic(h.vm.NewTypeError("$.on(name, handler): needs an event name and a function"))
	}
	n := name.String()
	first := len(h.handlers[n]) == 0
	h.handlers[n] = append(h.handlers[n], fn)
	if first && h.ready {
		_ = h.conn.Notify(modproto.MethodSubscribe, modproto.Subscribe{Events: []string{n}})
	}
	return goja.Undefined()
}

// $.tool.handle(id, fn) answers the agent tool id of the manifest's
// [[provides.tools]] with fn; a second handle replaces the first.
func (h *Host) jsToolHandle(c goja.FunctionCall) goja.Value {
	name := c.Argument(0)
	fn, ok := goja.AssertFunction(c.Argument(1))
	if goja.IsUndefined(name) || name.String() == "" || !ok {
		panic(h.vm.NewTypeError("$.tool.handle(id, handler): needs a tool id and a function"))
	}
	h.tools[name.String()] = fn
	return goja.Undefined()
}

// $.codehost.handle(method, fn) answers code host method method
// (modproto.CodeHostMethods: "pr", "prState", …) of a code host mod
// with fn; a second handle replaces the first.
func (h *Host) jsCodeHostHandle(c goja.FunctionCall) goja.Value {
	name := c.Argument(0)
	fn, ok := goja.AssertFunction(c.Argument(1))
	if goja.IsUndefined(name) || !ok || !modproto.IsCodeHost(modproto.CodeHostPrefix+name.String()) {
		panic(h.vm.NewTypeError("$.codehost.handle(method, handler): needs one of " + strings.Join(modproto.CodeHostMethods, ", ") + " and a function"))
	}
	h.codehost[name.String()] = fn
	return goja.Undefined()
}

// $.log(...values) writes one line to the mod's log.
func (h *Host) jsLog(c goja.FunctionCall) goja.Value {
	parts := make([]string, len(c.Arguments))
	for i, a := range c.Arguments {
		if o, ok := a.(*goja.Object); ok && o.ClassName() != "Error" && o.ClassName() != "Function" {
			if b, err := h.toJSON(a); err == nil && b != nil {
				parts[i] = string(b)
				continue
			}
		}
		parts[i] = a.String()
	}
	h.log("info", strings.Join(parts, " "))
	return goja.Undefined()
}

// maxDelayMs is the longest timer delay in milliseconds, as in a
// browser (2^31-1): a longer one would overflow time.Duration.
const maxDelayMs = float64(math.MaxInt32)

// setTimeout(fn, ms, ...args) and setInterval: fn runs in a slice of its
// own, under the budget.
func (h *Host) jsTimer(c goja.FunctionCall, repeat bool) goja.Value {
	fn, ok := goja.AssertFunction(c.Argument(0))
	if !ok {
		panic(h.vm.NewTypeError("the timer's callback is not a function"))
	}
	msec := c.Argument(1).ToFloat()
	d := minDelay
	if !math.IsNaN(msec) && msec*float64(time.Millisecond) > float64(minDelay) {
		d = time.Duration(min(msec, maxDelayMs) * float64(time.Millisecond))
	}
	var args []goja.Value
	if len(c.Arguments) > 2 {
		args = append(args, c.Arguments[2:]...)
	}
	h.nextTmr++
	id := h.nextTmr
	tm := &timer{}
	if repeat {
		tm.every = d
	}
	var fire func()
	fire = func() {
		h.loop.post(func() {
			if h.timers[id] != tm {
				return // cleared
			}
			if tm.every == 0 {
				delete(h.timers, id)
			}
			h.slice("timer", func() error {
				_, err := fn(goja.Undefined(), args...)
				return err
			})
			if tm.every > 0 && h.timers[id] == tm && !h.stopped {
				tm.t = time.AfterFunc(tm.every, fire)
			}
		})
	}
	tm.t = time.AfterFunc(d, fire)
	h.timers[id] = tm
	return h.vm.ToValue(id)
}

func (h *Host) jsClearTimer(c goja.FunctionCall) goja.Value {
	id := c.Argument(0).ToInteger()
	if tm, ok := h.timers[id]; ok {
		tm.t.Stop()
		delete(h.timers, id)
	}
	return goja.Undefined()
}

// The bindings' side of Host (T251 builds the $ API from these).

// VM is the mod's runtime.
func (h *Host) VM() *goja.Runtime { return h.vm }

// Dollar is the $ object the bindings add to.
func (h *Host) Dollar() *goja.Object { return h.dollar }

// Hello is the server's hello: the mod's id, grants and settings.
func (h *Host) Hello() modproto.Hello { return *h.hello }

// CallsInFlight is how many calls a mod may have waiting for the
// broker's answer; Call refuses the next until one is answered. The
// server keeps its own limit (modCallsAtOnce plus those waiting).
const CallsInFlight = 64

// Call sends method to the server's broker with params (a JS value sent
// as JSON; undefined sends none) and returns a promise of its result. A
// broker error rejects it with an Error whose code is the JSON-RPC code;
// a call past CallsInFlight is rejected with CodeFailed, unsent.
func (h *Host) Call(method string, params goja.Value) *goja.Promise {
	raw, err := h.toJSON(params)
	if err != nil {
		panic(h.vm.NewTypeError(method + ": params are not JSON: " + err.Error()))
	}
	p, resolve, reject := h.vm.NewPromise()
	if len(h.pending) >= CallsInFlight {
		obj, err := h.vm.New(h.errCtr, h.vm.ToValue(fmt.Sprintf("%s: too many calls in flight (%d); await their answers", method, CallsInFlight)))
		if err != nil {
			panic(err)
		}
		_ = obj.Set("code", modproto.CodeFailed)
		if err := reject(obj); err != nil {
			panic(err)
		}
		return p
	}
	h.nextID++
	id := h.nextID
	h.pending[id] = pendingCall{method: method, settle: func(res json.RawMessage, e *modproto.Error) error {
		if e != nil {
			obj, err := h.vm.New(h.errCtr, h.vm.ToValue(e.Message))
			if err != nil {
				return err
			}
			_ = obj.Set("code", e.Code)
			return reject(obj)
		}
		v, err := h.fromJSON(res)
		if err != nil {
			return reject(err)
		}
		return resolve(v)
	}}
	if err := h.conn.Request(id, method, raw); err != nil {
		delete(h.pending, id)
		if rerr := reject(h.vm.NewGoError(err)); rerr != nil {
			panic(rerr)
		}
	}
	return p
}

// Notify sends method to the server with params, expecting no answer.
func (h *Host) Notify(method string, params goja.Value) error {
	raw, err := h.toJSON(params)
	if err != nil {
		return err
	}
	return h.conn.Notify(method, raw)
}

// toJSON is JSON.stringify(v); nil for undefined.
func (h *Host) toJSON(v goja.Value) (json.RawMessage, error) {
	if v == nil || goja.IsUndefined(v) {
		return nil, nil
	}
	s, err := h.strfy(goja.Undefined(), v)
	if err != nil {
		return nil, err
	}
	if goja.IsUndefined(s) {
		return nil, errors.New("not serialisable")
	}
	return json.RawMessage(s.String()), nil
}

// fromJSON is JSON.parse(raw): plain JS objects, not wrapped Go maps.
// Empty raw is undefined.
func (h *Host) fromJSON(raw json.RawMessage) (goja.Value, error) {
	if len(raw) == 0 {
		return goja.Undefined(), nil
	}
	return h.parse(goja.Undefined(), h.vm.ToValue(string(raw)))
}
