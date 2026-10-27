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
// controls are never here: they stay the TUI's and the CLI's, but for
// the one exception named in the spec, answering a guard refusal.
var methods = map[string]Method{}

func init() {
	for _, m := range []Method{
		// Reading.
		unary(proto.MethodSessionList, false, noParams, sessionListOf),
		stream(proto.MethodProjectsWatch, []string{proto.EventProjectsChanged}, projectsWatchParams, projectsOf,
			func(ev server.ProjectsWatchEvent) (string, Projects, bool) {
				return ev.Event, projectsOf(server.ProjectsWatch{Projects: ev.Projects}), ev.Event == proto.EventProjectsChanged
			}, nil),
		stream(proto.MethodSessionWatch, []string{proto.EventWatchChanged}, sessionIDParams, watchOf,
			func(ev proto.WatchEvent) (string, Watch, bool) {
				return ev.Event, watchOf(ev.Watch), ev.Event == proto.EventWatchChanged
			}, nil),
		// A session's conversation (the server's caps say transcript).
		// The bridge acks the items it wrote to the app.
		stream(proto.MethodTranscriptWatch, transcriptEvents, transcriptParams, transcriptResultOf,
			func(ev proto.TranscriptEvent) (string, TranscriptEvent, bool) {
				return ev.Event, transcriptEventOf(ev), slices.Contains(transcriptEvents, ev.Event)
			}, transcriptAck),
		// The bytes of a file the conversation puts in front of the user
		// (app API 11; the server's caps say session-file), by the ref its
		// item gave: only what the transcript's line names there.
		unary(proto.MethodSessionFile, false, sessionFileParams, sessionFileOf),

		// A pane in plain VT (the server's caps say pane-vt): repaints,
		// output and resizes; the bridge acks the bytes it wrote to the
		// app. Its keys, size and repaints go in through pane.input,
		// which acts as the human (typed text takes a thread over).
		stream(proto.MethodPaneVT, paneEvents, paneParams, paneResultOf,
			func(ev proto.PaneVTEvent) (string, PaneEvent, bool) {
				return ev.Event, paneEventOf(ev), slices.Contains(paneEvents, ev.Event)
			}, paneAck),
		input(MethodPaneInput, proto.MethodPaneVT, true, paneInput),

		// The paired servers and the projects they share (the server's
		// mirror), with their coordinators and threads, whose panes
		// pane.vt opens as "<alias>/<session>" through the server's link
		// (its caps say pane-vt-remote), and whose conversations
		// transcript.watch reads so under the far server's switches (its
		// caps say transcript-remote; app API 12), where session.prompt
		// types into the pane.
		unary(proto.MethodPeerList, false, noParams, peerListOf),

		// The notice tray (mods' notifications): one console draws each.
		unary(proto.MethodNotifyClaim, false, noticeParams, noticeResultOf),
		unary(proto.MethodNotifyDismiss, false, noticeParams, noticeResultOf),

		// Acting as the human; the server checks each as it checks the
		// TUI's, and journals it as "human (app)".
		unary(proto.MethodSessionPrompt, true, promptParams, promptResultOf),
		// A file for a prompt (app API 15): its bytes, stored in the
		// session's project's uploads/; session.prompt's files name its
		// ref. This server's sessions only.
		unary(proto.MethodSessionUpload, true, uploadParams, uploadOf),
		unary(proto.MethodSessionAnswer, true, answerParams, answerResultOf),
		unary(proto.MethodSessionApprove, true, approveParams, approveResultOf),
		unary(proto.MethodSessionKeys, true, keysParams, func(struct{}) Empty { return Empty{} }),
		unary(proto.MethodTaskAsk, true, taskAskParams, taskAskResultOf),
		// The task actions by name (app API 5), with the checks of tm
		// task accept and send-back; each still only asks the
		// coordinator. project.open starts the project's coordinator
		// when none runs.
		unary(proto.MethodTaskAccept, true, taskParams, taskAskResultOf),
		unary(proto.MethodTaskSendBack, true, sendBackParams, taskAskResultOf),
		unary(proto.MethodTaskDelegate, true, taskParams, taskAskResultOf),
		unary(proto.MethodProjectOpen, true, projectOpenParams, projectOpenOf),
		// The coordinator's open questions (tm ask), put to the user
		// in its session, where the app's question card answers them
		// (app API 6).
		unary(proto.MethodQuestionsOpen, true, questionsOpenParams, questionsOpenOf),
		// A task added to a project's board, as tm task add (app API 7).
		unary(proto.MethodTaskAdd, true, taskAddParams, taskAddedOf),
		// The coordinator's save and clear (tm clear, prefix+C), and a
		// project activated or deactivated (tm project activate, S in
		// the sidebar), each asked first in the app (app API 13).
		unary(proto.MethodCoordinatorClear, true, coordinatorClearParams, coordinatorClearOf),
		unary(proto.MethodProjectActive, true, projectActiveParams, projectActiveOf),
		// One task with its notes and steps, which the board's tasks
		// come without, for the task view (app API 14).
		unary(proto.MethodTaskGet, false, taskGetParams, taskOf),
		// What a project's link waits on the user for, answered in place
		// (app API 16; the server's caps say link-act), with tm link's
		// checks: a held message approved or dropped, a file accepted,
		// declined or cancelled, a paired server's link request accepted
		// or declined. A message is never written through it. And a
		// paired server's share request accepted, which shares one of
		// this server's projects, or declined.
		unary(proto.MethodLinkAct, true, linkActParams, linkActOf),
		unary(proto.MethodPeerShareAnswer, true, shareAnswerParams, shareAnswerOf),

		// The guard's refusals, read and answered one at a time (app
		// API 18): the bridge's own, from the human's log (guard.go).
		// No other setting or access joins.
		local(MethodGuardRefusals, false, guardRefusals),
		local(MethodGuardDecide, true, guardDecide),
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

// MethodPaneInput sends keys, a size claim or a repaint request into
// an open pane.vt stream (PaneInputParams): the bridge's own method,
// answered once the line went to the server.
const MethodPaneInput = "pane.input"

// The events of the streams with several.
var (
	paneEvents       = []string{proto.EventPaneRepaint, proto.EventPaneOutput, proto.EventPaneResize, proto.EventPaneClosed}
	transcriptEvents = []string{proto.EventTranscriptReset, proto.EventTranscriptItems, proto.EventTranscriptStatus, proto.EventTranscriptClosed}
)

// unary is a method answered once: toServer copies the app's params AP
// into the server's SP, of copies the server's result SR into the app's
// AR.
func unary[AP, SP, SR, AR any](name string, write bool, toServer func(AP) SP, of func(SR) AR) Method {
	m := Method{Name: name, Write: write, Result: reflect.TypeFor[AR]()}
	m.toServer, m.Params = paramsOf(toServer)
	m.result = resultOf(of)
	return m
}

// stream is a method that streams after its result: ev names each of
// the server's event lines SE and copies it into the app's data AE,
// keep false for a line that isn't one of events; ack, when not nil,
// is the line to send the server once the event is written to the app
// (nil for none).
func stream[AP, SP, SR, AR, SE, AE any](name string, events []string, toServer func(AP) SP, of func(SR) AR,
	ev func(SE) (string, AE, bool), ack func(SE) any) Method {
	m := Method{Name: name, Stream: true, Events: events, Result: reflect.TypeFor[AR](), Data: reflect.TypeFor[AE]()}
	m.toServer, m.Params = paramsOf(toServer)
	m.result = resultOf(of)
	m.event = func(raw json.RawMessage) (string, any, bool, any, error) {
		var se SE
		if err := json.Unmarshal(raw, &se); err != nil {
			return "", nil, false, nil, err
		}
		name, ae, keep := ev(se)
		var a any
		if keep && ack != nil {
			a = ack(se)
		}
		return name, ae, keep, a, nil
	}
	return m
}

// input is a stream's input, the bridge's own method name: f copies the
// app's params AP into the open target stream's id and its line for the
// server.
func input[AP any](name, target string, write bool, f func(AP) (int64, any)) Method {
	m := Method{Name: name, Write: write, Target: target, Result: reflect.TypeFor[Empty]()}
	var toServer func(json.RawMessage) (any, error)
	toServer, m.Params = paramsOf(func(ap AP) idLine {
		id, line := f(ap)
		return idLine{id, line}
	})
	m.toStream = func(raw json.RawMessage) (int64, any, error) {
		v, err := toServer(raw)
		if err != nil {
			return 0, nil, err
		}
		il := v.(idLine)
		return il.id, il.line, nil
	}
	m.result = func(json.RawMessage) (any, error) { return Empty{}, nil }
	return m
}

// local is a method the bridge answers itself (Method.local): f gets
// the app's params AP, decoded as strictly as a server method's.
func local[AP, AR any](name string, write bool, f func(*bridge, AP) (AR, error)) Method {
	m := Method{Name: name, Write: write, Result: reflect.TypeFor[AR]()}
	m.toServer, m.Params = paramsOf(func(ap AP) AP { return ap })
	m.local = func(b *bridge, params any) (any, error) { return f(b, params.(AP)) }
	return m
}

// idLine is a stream input's stream and line.
type idLine struct {
	id   int64
	line any
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
	return proto.ProjectsWatchParams{Detail: p.Detail, Lean: p.Lean, Shown: p.Shown}
}

func sessionIDParams(p SessionIDParams) proto.SessionIDParams { return proto.SessionIDParams{ID: p.ID} }

func noticeParams(p NoticeParams) proto.NoticeParams { return proto.NoticeParams{ID: p.ID} }

func promptParams(p PromptParams) proto.SessionPromptParams {
	return proto.SessionPromptParams{ID: p.ID, Text: p.Text, Mode: p.Mode, Files: append([]string(nil), p.Files...)}
}

func uploadParams(p UploadParams) proto.SessionUploadParams {
	return proto.SessionUploadParams{Session: p.Session, Upload: p.Upload, Name: p.Name, Size: p.Size, Offset: p.Offset, Data: p.Data}
}

func answerParams(p AnswerParams) proto.SessionAnswerParams {
	return proto.SessionAnswerParams{ID: p.ID, Index: p.Index, Answer: p.Answer, Question: p.Question}
}

func approveParams(p ApproveParams) proto.SessionApproveParams {
	return proto.SessionApproveParams{ID: p.ID, Choice: p.Choice, Check: p.Check, Escape: p.Escape, Session: p.Session}
}

func keysParams(p KeysParams) proto.SessionKeysParams {
	return proto.SessionKeysParams{ID: p.ID, Data: p.Data}
}

// The app never names the console a pane's takeover is counted by:
// the server counts the bridge's process.
func paneParams(p PaneParams) proto.PaneVTParams { return proto.PaneVTParams{Session: p.Session} }

func transcriptParams(p TranscriptParams) proto.TranscriptWatchParams {
	return proto.TranscriptWatchParams{Session: p.Session}
}

func sessionFileParams(p SessionFileParams) proto.SessionFileParams {
	return proto.SessionFileParams{Session: p.Session, Ref: p.Ref, Offset: p.Offset, Stat: p.Stat}
}

// paneInput copies the app's input; its acks are the bridge's (paneAck).
func paneInput(p PaneInputParams) (int64, any) {
	in := proto.PaneVTInput{Input: p.Input, Repaint: p.Repaint}
	if p.Size != nil {
		in.Size = &proto.PaneSize{Cols: p.Size.Cols, Rows: p.Size.Rows}
	}
	return p.ID, in
}

// paneAck acks the data of a repaint or output event.
func paneAck(ev proto.PaneVTEvent) any {
	if len(ev.Data) == 0 {
		return nil
	}
	return proto.PaneVTInput{Ack: len(ev.Data)}
}

// transcriptAck acks the items of a reset or items event.
func transcriptAck(ev proto.TranscriptEvent) any {
	if len(ev.Items) == 0 {
		return nil
	}
	return proto.TranscriptInput{Ack: len(ev.Items)}
}

func taskAskParams(p TaskAskParams) proto.TaskAskParams {
	return proto.TaskAskParams{Project: p.Project, Task: p.Task, Kind: p.Kind, Note: p.Note}
}

func taskParams(p TaskParams) proto.TaskActionParams {
	return proto.TaskActionParams{Project: p.Project, Task: p.Task}
}

func sendBackParams(p SendBackParams) proto.TaskActionParams {
	return proto.TaskActionParams{Project: p.Project, Task: p.Task, Note: p.Note}
}

func questionsOpenParams(p ProjectParams) proto.QuestionsOpenParams {
	return proto.QuestionsOpenParams{Project: p.Project}
}

func taskGetParams(p TaskGetParams) proto.TaskGetParams {
	return proto.TaskGetParams{Project: p.Project, Task: p.Task}
}

func taskAddParams(p TaskAddParams) proto.TaskAddParams {
	return proto.TaskAddParams{Project: p.Project, Title: p.Title, Notes: p.Notes}
}

func coordinatorClearParams(p ProjectParams) proto.CoordinatorClearParams {
	return proto.CoordinatorClearParams{Project: p.Project}
}

func projectActiveParams(p ProjectActiveParams) proto.ProjectActiveParams {
	return proto.ProjectActiveParams{Project: p.Project, Active: p.Active}
}

func projectOpenParams(p ProjectOpenParams) proto.ProjectOpenParams {
	return proto.ProjectOpenParams{Project: p.Project, Cols: p.Cols, Rows: p.Rows}
}

func linkActParams(p LinkActParams) proto.LinkActParams {
	return proto.LinkActParams{Project: p.Project, Action: p.Action, ID: p.ID, Allow: p.Allow, Ask: p.Ask}
}

func shareAnswerParams(p ShareAnswerParams) proto.PeerShareAnswerParams {
	return proto.PeerShareAnswerParams{ID: p.ID, Accept: p.Accept, Project: p.Project}
}
