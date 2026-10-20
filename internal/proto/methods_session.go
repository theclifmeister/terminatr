package proto

import (
	"fmt"
	"time"

	"github.com/theclifmeister/terminatr/internal/modnotify"
)

// Control methods of sessions: start, stop, read and type into them, and
// what the agent layer adds (M1, M3).
const (
	MethodSessionList   = "session.list"
	MethodSessionStart  = "session.start"
	MethodSessionStop   = "session.stop"
	MethodSessionRead   = "session.read"
	MethodSessionKeys   = "session.keys"
	MethodSessionPrompt = "session.prompt"
	MethodSessionWait   = "session.wait"
	// MethodSessionRemote turns a coordinator's remote control on or off
	// in the running session (docs/SPEC.md §8.2).
	MethodSessionRemote = "session.remote"
	// MethodSessionModel applies the project's coordinator_model to its
	// running coordinator (docs/SPEC.md §8.2, Models).
	MethodSessionModel = "session.model"
	// MethodSessionAdopt makes a plain agent session a project thread's
	// (docs/SPEC.md §9, Adopt).
	MethodSessionAdopt = "session.adopt"
	// MethodSessionAdoptAsk records the human's go-ahead to adopt a
	// session into a project (T on the dashboard), or with Check only
	// reports whether one waits (docs/SPEC.md §9, Adopt). Human-only: an
	// agent's tm thread adopt needs that go-ahead, which nothing an agent
	// writes can stand in for.
	MethodSessionAdoptAsk = "session.adopt.ask"
)

// SessionAdoptParams are the params of session.adopt: the session, and
// the project and thread it now belongs to. Brief is the thread's brief,
// attached as the system prompt when the server resumes it.
type SessionAdoptParams struct {
	ID      string `json:"id"`
	Project string `json:"project"`
	Thread  string `json:"thread"`
	Brief   string `json:"brief,omitempty"`
	// Asked adopts only on the human's go-ahead for this session and
	// project (session.adopt.ask), and uses it up: set for an agent's
	// tm thread adopt.
	Asked bool `json:"asked,omitempty"`
}

// SessionAdoptAskParams are the params of session.adopt.ask: the session
// the human asks to adopt into project, or with Check whether such a
// go-ahead waits, recording nothing, or with Withdraw the human taking
// theirs back.
type SessionAdoptAskParams struct {
	ID       string `json:"id"`
	Project  string `json:"project"`
	Check    bool   `json:"check,omitempty"`
	Withdraw bool   `json:"withdraw,omitempty"`
}

// SessionAdoptAskResult says whether the human's go-ahead waits.
type SessionAdoptAskResult struct {
	Asked bool `json:"asked"`
}

// ClosedRestarting is the FrameClosed reason of a session that the server
// relaunches at once under the same id (a remote control change): a
// client attaches to it again instead of closing the pane.
const ClosedRestarting = "session restarting"

// SessionRemoteParams are the params of session.remote.
type SessionRemoteParams struct {
	ID string `json:"id"`
	On bool   `json:"on"`
}

// How session.remote applied the change.
const (
	RemoteUnchanged = "unchanged" // it already was so
	RemotePrompted  = "prompted"  // the agent's in-session text was pasted
	RemoteRestarted = "restarted" // the agent was resumed with or without the flag
)

// SessionRemoteResult is the result of session.remote.
type SessionRemoteResult struct {
	RemoteControl bool   `json:"remote_control"`
	How           string `json:"how"`
	// Held: turned off while the project's setting keeps coordinators'
	// remote control on; the ticker leaves it off until the coordinator
	// is started anew (docs/SPEC.md §11.2).
	Held bool `json:"held,omitempty"`
}

// SessionModelParams are the params of session.model: the coordinator
// session whose project's coordinator_model the server applies.
type SessionModelParams struct {
	ID string `json:"id"`
}

// How session.model applied the setting.
const (
	ModelUnchanged = "unchanged"  // the coordinator already runs it
	ModelSwitched  = "switched"   // the agent's switch command was sent
	ModelNextStart = "next-start" // the agent can't switch: on its next start
)

// SessionModelResult is the result of session.model: the model wanted
// ("" the agent's own default) and how it was applied.
type SessionModelResult struct {
	Model string `json:"model"`
	How   string `json:"how"`
}

// Session roles.
const (
	RoleShell       = "shell"
	RoleCoordinator = "coordinator"
	RoleThread      = "thread"
)

// CoordinatorOf is project slug's coordinator among sessions, nil when
// none is listed.
func CoordinatorOf(sessions []SessionInfo, slug string) *SessionInfo {
	for i, s := range sessions {
		if s.Role == RoleCoordinator && s.Project == slug {
			return &sessions[i]
		}
	}
	return nil
}

// SessionInfo describes one hosted session.
type SessionInfo struct {
	ID      string `json:"id"`
	Role    string `json:"role"`
	Agent   string `json:"agent,omitempty"`
	Project string `json:"project,omitempty"`
	Thread  string `json:"thread,omitempty"`
	// Task is a thread session's task ("T12"), "" for a thread without
	// one: the UI names a thread by its task (docs/SPEC.md §4).
	Task    string    `json:"task,omitempty"`
	Argv    []string  `json:"argv"`
	Cwd     string    `json:"cwd"`
	PID     int       `json:"pid"`
	Cols    uint16    `json:"cols"`
	Rows    uint16    `json:"rows"`
	Title   string    `json:"title,omitempty"`
	Created time.Time `json:"created"`
	Clients int       `json:"clients"`

	// Agent state (docs/SPEC.md §8.4), for agent sessions and for shells
	// whose foreground job was identified as an agent.
	State  string `json:"state,omitempty"`
	Reason string `json:"reason,omitempty"`
	// Escape: blocked on a permission prompt that asks to run outside
	// the agent's sandbox, which the user answers, never the
	// coordinator (T574 case 6).
	Escape       bool   `json:"escape,omitempty"`
	StateSources string `json:"state_sources,omitempty"`
	AgentSID     string `json:"agent_session_id,omitempty"`
	Identified   bool   `json:"identified,omitempty"` // found by process, not launched
	TodosDone    int    `json:"todos_done,omitempty"`
	TodosTotal   int    `json:"todos_total,omitempty"`
	Current      string `json:"current,omitempty"` // the in-progress todo
	Queued       int    `json:"queued_prompts,omitempty"`
	// QueuedSince is when the oldest queued prompt was queued.
	QueuedSince time.Time `json:"queued_since,omitzero"`
	// QueueHeld says why the next queued prompt isn't pasted although the
	// agent is idle ("prompt box not empty", "dialog on screen"), since
	// QueueHeldSince. The server resolves a prompt held for its bound
	// (docs/SPEC.md §8.6), so this never lasts.
	QueueHeld      string    `json:"queue_held,omitempty"`
	QueueHeldSince time.Time `json:"queue_held_since,omitzero"`
	// RemoteControl: the agent is reachable from another device.
	RemoteControl bool `json:"remote_control,omitempty"`
	// RemoteHeld: the user turned the coordinator's remote control off
	// (session.remote); the ticker doesn't turn it back on until the
	// coordinator is started anew.
	RemoteHeld bool `json:"remote_held,omitempty"`
	// Model is the model the agent runs as tm launched or switched it
	// ("" its own default). ModelPending: the coordinator's setting
	// wants ModelNext, which its agent can't switch to live, so it
	// applies on the next start (docs/SPEC.md §8.2, Models).
	Model        string `json:"model,omitempty"`
	ModelNext    string `json:"model_next,omitempty"`
	ModelPending bool   `json:"model_pending,omitempty"`
	// HooksSkipped says another of the agent's mods skipped terminatr's
	// context hooks, and who: the mod's name, or "another mod" when it
	// can't be told. tm's command hooks give the context instead
	// (docs/SPEC.md §7.8).
	HooksSkipped string `json:"hooks_skipped,omitempty"`
	// Question is the question menu open in the agent, as its mod sent it
	// (session.ask); nil without one, or without a mod.
	Question *Question `json:"question,omitempty"`
	// Context is how many tokens the agent's latest turn read as context
	// (input and cache, not the running total), ContextWindow the window
	// of its model, both as its mod sent them; 0 without a mod or
	// before the first turn.
	Context       int64 `json:"context,omitempty"`
	ContextWindow int64 `json:"context_window,omitempty"`
	// Server is the paired server's alias for one of its panes this
	// server shows (docs/SPEC.md §16.12): the id is "<alias>/<its id>";
	// "" for this server's own. ViewOnly: its switch lets this server
	// watch it, not type (since protocol 16).
	Server   string `json:"server,omitempty"`
	ViewOnly bool   `json:"view_only,omitempty"`
	// Cells are the mods' cells for this session (a target of Session
	// alone), since protocol 13.
	Cells []ModCell `json:"cells,omitempty"`
}

// ContextPercent is the share of the context window the session uses, in
// whole percent; -1 when unknown.
func (s SessionInfo) ContextPercent() int {
	if s.Context <= 0 || s.ContextWindow <= 0 {
		return -1
	}
	return int(s.Context * 100 / s.ContextWindow)
}

// queueNotice is how long a queued prompt must be held while its agent is
// idle before listings, tm doctor and tm context call it out: shorter
// holds are a box being typed into or a screen not yet re-read.
const queueNotice = time.Minute

// QueueNote says why the session's next queued prompt is held, e.g.
// "held 12m: prompt box not empty", once it has been for queueNotice;
// "" otherwise.
func (s SessionInfo) QueueNote(now time.Time) string {
	if s.Queued == 0 || s.QueueHeld == "" || now.Sub(s.QueueHeldSince) < queueNotice {
		return ""
	}
	return fmt.Sprintf("held %s: %s", now.Sub(s.QueueHeldSince).Truncate(time.Second), s.QueueHeld)
}

// SessionStartParams are the params of session.start.
type SessionStartParams struct {
	// Argv defaults to the user's login shell.
	Argv []string `json:"argv,omitempty"`
	// Cwd must be an absolute directory; it defaults to the home directory.
	Cwd  string `json:"cwd,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`

	// Agent starts the named agent (docs/SPEC.md §8) instead of Argv.
	Agent string `json:"agent,omitempty"`
	// Role is coordinator, thread or shell (the default). It decides the
	// access policy and the context re-injected after a clear.
	Role    string `json:"role,omitempty"`
	Project string `json:"project,omitempty"`
	Thread  string `json:"thread,omitempty"`
	// Brief is a file attached as the agent's system prompt, if the agent
	// supports it; Kickoff is the first prompt.
	Brief   string `json:"brief,omitempty"`
	Kickoff string `json:"kickoff,omitempty"`
	Yolo    bool   `json:"yolo,omitempty"`
	Model   string `json:"model,omitempty"`
	// NoSandboxEscapes starts a thread whose agent can't run a command
	// outside its sandbox (sandbox_escapes = false, docs/SPEC.md §5.2).
	NoSandboxEscapes bool `json:"no_sandbox_escapes,omitempty"`
	// RemoteControl starts a coordinator reachable from another device,
	// named after its project; the agent's manifest must support it.
	RemoteControl bool `json:"remote_control,omitempty"`
	// ResumeSID resumes this agent session id instead of starting fresh
	// (tm thread restart); Kickoff is then not sent.
	ResumeSID string `json:"resume_sid,omitempty"`
}

// SessionStartResult is the result of session.start.
type SessionStartResult struct {
	Session SessionInfo `json:"session"`
	// Warnings are what the launch did to the human's settings that
	// they should hear of (sandbox rules refused or left out, and why).
	Warnings []string `json:"warnings,omitempty"`
}

// SessionListResult is the result of session.list.
type SessionListResult struct {
	Sessions []SessionInfo `json:"sessions"`
	// Alerts counts the server's notifications (a session blocked, a
	// thread reported); a client rings its bell when it goes up
	// (docs/SPEC.md §4).
	Alerts uint64 `json:"alerts"`
	// ModNotices are the notifications user mods sent that are still in
	// the tray (modnotify); consoles show them.
	ModNotices []modnotify.Notice `json:"mod_notices,omitempty"`
	// Version and Build are the server's own (version.Version and
	// version.BuildID()), which a client shows and compares with its
	// own.
	Version string `json:"version,omitempty"`
	Build   string `json:"build,omitempty"`
	// Latest is the tag of a release newer than the server, found by its
	// daily check (docs/SPEC.md §10.1); Upgrade is how to install it.
	Latest  string `json:"latest,omitempty"`
	Upgrade string `json:"upgrade,omitempty"`
	// OS is the server's platform (runtime.GOOS), where its threads run:
	// the Access tab shows the settings that apply there.
	OS string `json:"os,omitempty"`
	// Mods are the mods' columns, cells and status segments (since
	// protocol 13); nil when no mod publishes any.
	Mods *ModCells `json:"mods,omitempty"`
	// Scoped is set when the list holds only the sessions the caller
	// sees (a session's connection), not every session of the server.
	Scoped bool `json:"scoped,omitempty"`
}

// SessionIDParams name one session.
type SessionIDParams struct {
	ID string `json:"id"`
}

// SessionReadParams are the params of session.read.
type SessionReadParams struct {
	ID string `json:"id"`
	// Scrollback includes the scrollback above the visible rows.
	Scrollback bool `json:"scrollback,omitempty"`
}

// SessionReadResult is the result of session.read.
type SessionReadResult struct {
	Text string `json:"text"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// SessionKeysParams are the params of session.keys: raw bytes written to
// the session's PTY as if typed.
type SessionKeysParams struct {
	ID   string `json:"id"`
	Data string `json:"data"`
	// Relay is the role whose answer the keys carry, when tm's own
	// commands send them for an agent (the coordinator's tm thread
	// approve and answer): "coordinator" keys never answer an escape's
	// prompt (SessionInfo.Escape), whatever the session's state was
	// when the command looked.
	Relay string `json:"relay,omitempty"`
}

// AttachRequest is the first line a client sends on an attach connection
// after the hello.
type AttachRequest struct {
	Attach AttachParams `json:"attach"`
}

// AttachParams name the session and the client's window size. The size is
// informational: attaching never resizes the pane (docs/SPEC.md §3.3).
type AttachParams struct {
	Session string `json:"session"`
	Cols    uint16 `json:"cols,omitempty"`
	Rows    uint16 `json:"rows,omitempty"`
}

// AttachReply is the server's answer; frames follow only if Error is nil.
type AttachReply struct {
	Attached *SessionInfo `json:"attached,omitempty"`
	Error    *Error       `json:"error,omitempty"`
}

// SessionPromptParams are the params of session.prompt.
type SessionPromptParams struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	// Mode is "" (queued), PromptNow or PromptInterrupt (docs/SPEC.md
	// §8.6, Priority prompts).
	Mode string `json:"mode,omitempty"`
}

// session.prompt modes.
const (
	// PromptNow steers the prompt into the agent's running turn.
	PromptNow = "now"
	// PromptInterrupt stops the running turn for the prompt.
	PromptInterrupt = "interrupt"
)

// SessionPromptResult says how the prompt went in: "channel" (written to
// the agent's structured channel, which may not confirm delivery),
// "queued" (pasted once the agent is idle with an empty prompt box), and
// for a Mode, "steered" (pasted into the running turn) or "interrupted"
// (the turn stopped for it). Why says why a Mode's prompt went another
// way (the agent idle, a draft in its prompt box, no interrupt key).
type SessionPromptResult struct {
	Via string `json:"via"`
	Why string `json:"why,omitempty"`
}

// SessionWaitParams are the params of session.wait: block until the
// session's agent state is one of States (any change when empty), or
// until TimeoutMS passes.
type SessionWaitParams struct {
	ID        string   `json:"id"`
	States    []string `json:"states,omitempty"`
	TimeoutMS int      `json:"timeout_ms,omitempty"`
}

// SessionWaitResult is the state when session.wait returned.
type SessionWaitResult struct {
	State    string `json:"state"`
	Reason   string `json:"reason,omitempty"`
	TimedOut bool   `json:"timed_out,omitempty"`
}

// MethodQuestionsOpen has a project's coordinator open its open
// questions (tm ask) in its question dialog (docs/SPEC.md §7.5,
// Questions): the band in its pane, a dashboard key or `tm ask open`.
// The server queues a fixed-word prompt to the coordinator, dropped at
// delivery when no question is open any more.
const MethodQuestionsOpen = "questions.open"

// QuestionsOpenParams are the params of questions.open.
type QuestionsOpenParams struct {
	Project string `json:"project"`
}

// QuestionsOpenResult is what questions.open did: the coordinator
// session prompted, how (as session.prompt's Via) and how many
// questions were open.
type QuestionsOpenResult struct {
	Session string `json:"session"`
	Via     string `json:"via"`
	Open    int    `json:"open"`
}

// MethodCoordinatorClear saves and clears a project's coordinator
// (docs/SPEC.md §7.5, Auto-clear): `tm clear`, the context hint's key,
// /saveclear in its pane. The ticker takes auto-clear's steps for it at
// its next sweep: the save ask, then the agent's clear prompt once the
// coordinator rested after it. Refused for a coordinator whose agent has
// no clear prompt ([inject] clear).
const MethodCoordinatorClear = "coordinator.clear"

// CoordinatorClearParams are the params of coordinator.clear.
type CoordinatorClearParams struct {
	Project string `json:"project"`
}

// CoordinatorClearResult names the coordinator session asked to save and
// clear.
type CoordinatorClearResult struct {
	Session string `json:"session"`
}

// MethodSessionClear clears an agent session's conversation
// (SessionIDParams): the server pastes its agent's clear prompt ([inject]
// clear, /clear for Claude) as a plain prompt, queued until the agent is
// idle, and journals it. Human-only: a console runs it for a mod's
// session.clear binding once the human confirmed it. The result is a
// SessionPromptResult.
const MethodSessionClear = "session.clear"
