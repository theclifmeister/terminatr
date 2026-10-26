package appapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
)

// methods is the app API: the control methods the app may call, by name.
// A method joins on purpose, here, with its copies; nothing reaches the
// server that isn't listed. Settings, access, mods and the server's own
// controls are never here: they stay the TUI's and the CLI's.
var methods = map[string]Method{}

func init() {
	for _, m := range []Method{
		// Reading.
		unary(proto.MethodSessionList, false, noParams, sessionListOf),
		stream(proto.MethodProjectsWatch, proto.EventProjectsChanged, projectsWatchParams, projectsOf,
			func(ev server.ProjectsWatchEvent) (Projects, bool) {
				return projectsOf(server.ProjectsWatch{Projects: ev.Projects}), ev.Event == proto.EventProjectsChanged
			}),
		stream(proto.MethodSessionWatch, proto.EventWatchChanged, sessionIDParams, watchOf,
			func(ev proto.WatchEvent) (Watch, bool) { return watchOf(ev.Watch), ev.Event == proto.EventWatchChanged }),

		// The notice tray (mods' notifications): one console draws each.
		unary(proto.MethodNotifyClaim, false, noticeParams, noticeResultOf),
		unary(proto.MethodNotifyDismiss, false, noticeParams, noticeResultOf),

		// Acting as the human; the server checks each as it checks the
		// TUI's, and journals it as "human (app)".
		unary(proto.MethodSessionPrompt, true, promptParams, promptResultOf),
		unary(proto.MethodSessionAnswer, true, answerParams, answerResultOf),
		unary(proto.MethodSessionApprove, true, approveParams, approveResultOf),
		unary(proto.MethodSessionKeys, true, keysParams, func(struct{}) Empty { return Empty{} }),
		unary(proto.MethodTaskAsk, true, taskAskParams, taskAskResultOf),
	} {
		methods[m.Name] = m
	}
}

// Methods are the app API's methods, by name.
func Methods() []Method {
	out := make([]Method, 0, len(methods))
	for _, name := range MethodNames() {
		out = append(out, methods[name])
	}
	return out
}

// MethodNames are the names of the app API's methods, sorted.
func MethodNames() []string {
	names := make([]string, 0, len(methods))
	for n := range methods {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// Lookup is method name of the app API.
func Lookup(name string) (Method, bool) {
	m, ok := methods[name]
	return m, ok
}

// unary is a method answered once: toServer copies the app's params AP
// into the server's SP, of copies the server's result SR into the app's
// AR.
func unary[AP, SP, SR, AR any](name string, write bool, toServer func(AP) SP, of func(SR) AR) Method {
	m := Method{Name: name, Write: write, Result: reflect.TypeFor[AR]()}
	m.toServer, m.Params = paramsOf(toServer)
	m.result = resultOf(of)
	return m
}

// stream is a method that streams after its result: event copies each
// of the server's event lines SE into the app's data AE, keep false for a
// line that isn't one of its events.
func stream[AP, SP, SR, AR, SE, AE any](name, event string, toServer func(AP) SP, of func(SR) AR, ev func(SE) (AE, bool)) Method {
	m := Method{Name: name, Stream: true, Event: event, Result: reflect.TypeFor[AR](), Data: reflect.TypeFor[AE]()}
	m.toServer, m.Params = paramsOf(toServer)
	m.result = resultOf(of)
	m.event = func(raw json.RawMessage) (any, bool, error) {
		var se SE
		if err := json.Unmarshal(raw, &se); err != nil {
			return nil, false, err
		}
		ae, keep := ev(se)
		return ae, keep, nil
	}
	return m
}

// paramsOf decodes the app's params strictly (an unknown field is an
// error) and copies them; a method without params (struct{}) takes none
// or {}.
func paramsOf[AP, SP any](toServer func(AP) SP) (func(json.RawMessage) (any, error), reflect.Type) {
	t := reflect.TypeFor[AP]()
	f := func(raw json.RawMessage) (any, error) {
		var ap AP
		if len(bytes.TrimSpace(raw)) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&ap); err != nil {
				return nil, err
			}
			if dec.More() {
				return nil, fmt.Errorf("params: more than one value")
			}
		}
		return toServer(ap), nil
	}
	if t.Kind() == reflect.Struct && t.NumField() == 0 {
		return f, nil
	}
	return f, t
}

// resultOf decodes the server's SR and copies it.
func resultOf[SR, AR any](of func(SR) AR) func(json.RawMessage) (any, error) {
	return func(raw json.RawMessage) (any, error) {
		var sr SR
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &sr); err != nil {
				return nil, err
			}
		}
		return of(sr), nil
	}
}

// The params' copies: field by field, so what the app can't set (Relay,
// a server method's params for tm's own commands) never crosses.

func noParams(struct{}) any { return nil }

func projectsWatchParams(p ProjectsWatchParams) proto.ProjectsWatchParams {
	return proto.ProjectsWatchParams{Detail: p.Detail}
}

func sessionIDParams(p SessionIDParams) proto.SessionIDParams { return proto.SessionIDParams{ID: p.ID} }

func noticeParams(p NoticeParams) proto.NoticeParams { return proto.NoticeParams{ID: p.ID} }

func promptParams(p PromptParams) proto.SessionPromptParams {
	return proto.SessionPromptParams{ID: p.ID, Text: p.Text, Mode: p.Mode}
}

func answerParams(p AnswerParams) proto.SessionAnswerParams {
	return proto.SessionAnswerParams{ID: p.ID, Index: p.Index, Answer: p.Answer, Question: p.Question}
}

func approveParams(p ApproveParams) proto.SessionApproveParams {
	return proto.SessionApproveParams{ID: p.ID, Choice: p.Choice, Check: p.Check, Escape: p.Escape}
}

func keysParams(p KeysParams) proto.SessionKeysParams {
	return proto.SessionKeysParams{ID: p.ID, Data: p.Data}
}

func taskAskParams(p TaskAskParams) proto.TaskAskParams {
	return proto.TaskAskParams{Project: p.Project, Task: p.Task, Kind: p.Kind, Note: p.Note}
}
