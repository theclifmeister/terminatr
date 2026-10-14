package modtest

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/theclifmeister/terminatr/internal/changes"
	"github.com/theclifmeister/terminatr/internal/modapi"
	"github.com/theclifmeister/terminatr/internal/modproto"
)

// Broker is the fake broker: it answers a mod's $ API calls as the
// server's broker would, from the fixture's answers, then from its own
// store and files, refuses a method the mod lacks the grant of, and
// records every call in order.
//
// Without an answer: store.* and fs.* use the broker's own store and
// files; projects.list, tasks.list and actions.list are empty;
// thread.get and report.get null; thread.changes a worktree with no
// changes (state none); actions.available false;
// project.get, http.fetch and process.run an error (the fixture must
// say); everything else (the ui, annotate, inbox.raise) null.
type Broker struct {
	grants  []string
	answers []Answer

	mu    sync.Mutex
	store map[string]json.RawMessage
	files map[string]string
	calls []Call
}

// NewBroker makes a broker for a mod granted grants, with the fixture's
// answers, store and files.
func NewBroker(grants []string, answers []Answer, store map[string]json.RawMessage, files map[string]string) *Broker {
	b := &Broker{grants: grants, answers: answers, store: maps.Clone(store), files: maps.Clone(files)}
	if b.store == nil {
		b.store = map[string]json.RawMessage{}
	}
	if b.files == nil {
		b.files = map[string]string{}
	}
	return b
}

// Calls are the calls so far, in the order the mod made them.
func (b *Broker) Calls() []Call {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.calls)
}

// Store is the store now.
func (b *Broker) Store() map[string]json.RawMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	return maps.Clone(b.store)
}

// Answer records the call and answers it: a result, or an error.
func (b *Broker) Answer(method string, params json.RawMessage) (json.RawMessage, *modproto.Error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, Call{Method: method, Params: params})
	m, ok := modapi.Lookup(method)
	if !ok || m.Local {
		return nil, &modproto.Error{Code: modproto.CodeMethodNotFound, Message: "unknown method " + method}
	}
	if m.Needs != "" && !slices.Contains(b.grants, m.Needs) {
		return nil, &modproto.Error{Code: modproto.CodeDenied, Message: fmt.Sprintf("refused: %s needs the %s permission", method, m.Needs)}
	}
	for _, a := range b.answers {
		if a.Method == method && Match(a.Params, params) {
			if a.Error != "" {
				return nil, &modproto.Error{Code: modproto.CodeInternal, Message: a.Error}
			}
			if len(a.Result) == 0 {
				return json.RawMessage("null"), nil
			}
			return a.Result, nil
		}
	}
	var p struct {
		Key    string          `json:"key"`
		Value  json.RawMessage `json:"value"`
		Path   string          `json:"path"`
		Text   string          `json:"text"`
		Append bool            `json:"append"`
		Slug   string          `json:"slug"`
		Level  string          `json:"level"`
	}
	_ = json.Unmarshal(params, &p)
	switch method {
	case "store.get":
		if v, ok := b.store[p.Key]; ok {
			return v, nil
		}
		return json.RawMessage("null"), nil
	case "store.set":
		if len(p.Value) == 0 || string(p.Value) == "null" {
			delete(b.store, p.Key)
		} else {
			b.store[p.Key] = p.Value
		}
	case "store.del":
		delete(b.store, p.Key)
	case "fs.read":
		t, ok := b.files[p.Path]
		if !ok {
			return nil, &modproto.Error{Code: modproto.CodeInternal, Message: "no file " + p.Path}
		}
		return marshal(t), nil
	case "fs.write":
		if p.Append {
			b.files[p.Path] += p.Text
		} else {
			b.files[p.Path] = p.Text
		}
	case "ui.toast":
		// Refused as the server refuses it (ui.toast in internal/server).
		if strings.TrimSpace(p.Text) == "" || p.Level != "" && !slices.Contains(modapi.Levels, p.Level) {
			return nil, &modproto.Error{Code: modproto.CodeInvalidParams, Message: "ui.toast: a toast needs a text, and a level of info, warn or error"}
		}
	case "projects.list", "tasks.list", "actions.list", "memory.list":
		return json.RawMessage("[]"), nil
	case "actions.available":
		return json.RawMessage("false"), nil
	case "thread.changes":
		return marshal(changes.Changeset{State: changes.StateNone, Mode: changes.ModeBase, Files: []changes.File{}}), nil
	case "project.get":
		return nil, &modproto.Error{Code: modproto.CodeInternal, Message: fmt.Sprintf("no project %q: the fixture answers project.get", p.Slug)}
	case "memory.read":
		return nil, &modproto.Error{Code: modproto.CodeNotFound, Message: "no memory file: the fixture answers memory.read"}
	case "http.fetch", "process.run":
		return nil, &modproto.Error{Code: modproto.CodeInternal, Message: "the fixture has no answer for " + method}
	}
	return json.RawMessage("null"), nil
}

func marshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
