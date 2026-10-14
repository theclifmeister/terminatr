package proto

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/modnotify"
	"github.com/theclifmeister/terminatr/internal/modview"
)

// Error codes of control responses. Clients match on these, never on
// messages.
const (
	ErrUnknownMethod  = "unknown-method"
	ErrBadParams      = "bad-params"
	ErrUnknownSession = "unknown-session"
	ErrRefused        = "refused"
	ErrInternal       = "internal"
)

// Error is a structured failure.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Errorf builds an *Error.
func Errorf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Request is one control call.
type Request struct {
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response answers the Request with the same ID.
type Response struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

// Control methods served since M1.
const (
	MethodPing         = "ping"
	MethodServerStatus = "server.status"
	MethodServerStop   = "server.stop"
	MethodSessionList  = "session.list"
	MethodSessionStart = "session.start"
	MethodSessionStop  = "session.stop"
	MethodSessionRead  = "session.read"
	MethodSessionKeys  = "session.keys"
	// MethodServerKeychain reports whether the server's sessions can reach
	// the macOS login keychain (docs/SPEC.md §3.1).
	MethodServerKeychain = "server.keychain"
	// MethodServerCodeHost runs tm doctor's code-host checks (gh, az, git
	// origin) in the server's own context (docs/SPEC.md §3.3).
	MethodServerCodeHost = "server.codehost"
)

// Control methods of the agent layer (M3).
const (
	MethodSessionPrompt = "session.prompt"
	MethodSessionWait   = "session.wait"
	MethodAgentList     = "agent.list"
	MethodAgentReload   = "agent.reload"
	MethodAgentExplain  = "agent.explain"
	MethodHookEvent     = "hook.event"
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

// ServerStatus is the result of server.status.
type ServerStatus struct {
	PID      int       `json:"pid"`
	Version  string    `json:"version"`
	Build    string    `json:"build"`
	Protocol int       `json:"protocol"`
	Started  time.Time `json:"started"`
	Sessions int       `json:"sessions"`
	Socket   string    `json:"socket"`
	Home     string    `json:"home"`
	// PreviousShutdown is "clean", "crash" or "" (first start).
	PreviousShutdown string `json:"previous_shutdown,omitempty"`
	// Lost lists sessions of the previous server that were not restored.
	Lost []string `json:"lost,omitempty"`
	// Resumed lists agent sessions of the previous server that were
	// relaunched with their agent session ids (docs/SPEC.md §3.6).
	Resumed []string `json:"resumed,omitempty"`
	// Link is this server's links to its paired servers (docs/SPEC.md
	// §16.7); nil until it has an identity key.
	Link *LinkStatus `json:"link,omitempty"`
}

// MethodPeerReload has the server read its identity key and peers again
// (docs/SPEC.md §16.8): tm peer pair, join, remove and rotate call it
// after changing them. The result is the LinkStatus after the reload
// (null without an identity key). The human's.
const MethodPeerReload = "peer.reload"

// LinkStatus is this server's identity and its links.
type LinkStatus struct {
	// ID is this server's id, 64 hex digits; Short the 8 people read.
	ID    string `json:"id"`
	Short string `json:"short"`
	// Error is why the identity key or the peers can't be read.
	Error string     `json:"error,omitempty"`
	Peers []PeerLink `json:"peers,omitempty"`
}

// Link states.
const (
	LinkConnecting = "connecting" // dialling, or proving keys
	LinkUp         = "up"
	LinkOffline    = "offline" // Reason says why; Retry is the next dial
)

// Why a link is offline.
const (
	LinkReasonRelay   = "relay"   // the transport is down or closed the link
	LinkReasonPeer    = "peer"    // the relay is up; the peer isn't in the room
	LinkReasonRefused = "refused" // the other end's key isn't the paired one, or it refused ours
	LinkReasonVersion = "version" // the peer's link protocol is too old
)

// PeerLink is the link to one paired server.
type PeerLink struct {
	Alias     string `json:"alias"`
	ID        string `json:"id"`
	Transport string `json:"transport"` // "relay relay.example.net"
	State     string `json:"state"`
	Reason    string `json:"reason,omitempty"`
	// Since is when the link went into its state.
	Since time.Time `json:"since"`
	// Error is the last failure, for people.
	Error string `json:"error,omitempty"`
	// Retry is the next dial, when offline.
	Retry time.Time `json:"retry,omitzero"`
	// RTT is the last ping's round trip in milliseconds, when up.
	RTT float64 `json:"rtt_ms,omitempty"`
	// The peer's hello, from when the link was last up.
	Version      string   `json:"version,omitempty"`
	LinkProtocol int      `json:"link_protocol,omitempty"`
	Caps         []string `json:"caps,omitempty"`
	// RelayVersion is the relay's tm release, as it said when the link
	// last went through it ("" for a relay older than that).
	RelayVersion string `json:"relay_version,omitempty"`
}

// KeychainStatus is the result of server.keychain.
type KeychainStatus struct {
	// Checked is false where there is no keychain to check (Linux).
	Checked bool `json:"checked"`
	// OK means the server's sessions can reach the login keychain.
	OK bool `json:"ok"`
	// OverSSH means the server's environment is an SSH login's.
	OverSSH bool `json:"over_ssh,omitempty"`
	// Session is launchd's name for the server's session: Aqua for the
	// desktop's, Background or StandardIO for an SSH login's.
	Session string `json:"session,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// CodeHostStatus is the result of server.codehost: the code-host checks
// as the server, and so its sessions, would see them.
type CodeHostStatus struct {
	Checks []CodeHostCheck `json:"checks"`
}

// CodeHostCheck is one line of CodeHostStatus.
type CodeHostCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// ServerStopParams are the params of server.stop.
type ServerStopParams struct {
	// Yes stops the server even while agent sessions run.
	Yes bool `json:"yes,omitempty"`
}

// Session roles.
const (
	RoleShell       = "shell"
	RoleCoordinator = "coordinator"
	RoleThread      = "thread"
)

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
	State        string `json:"state,omitempty"`
	Reason       string `json:"reason,omitempty"`
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

// AppendFrame appends one encoded frame to b, the in-memory form of
// WriteFrame for queues that batch frames.
func AppendFrame(b []byte, t FrameType, payload []byte) []byte {
	n := len(payload)
	b = append(b, byte(t), byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	return append(b, payload...)
}

// SessionPromptParams are the params of session.prompt.
type SessionPromptParams struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// SessionPromptResult says how the prompt went in: "channel" (written to
// the agent's structured channel, which may not confirm delivery) or
// "queued" (pasted once the agent is idle with an empty prompt box).
type SessionPromptResult struct {
	Via string `json:"via"`
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

// AgentInfo describes one known agent for agent.list.
type AgentInfo struct {
	Name       string   `json:"name"`
	Display    string   `json:"display,omitempty"`
	Source     string   `json:"source"` // builtin:… or a file path
	Command    string   `json:"command"`
	Injector   string   `json:"injector"`
	Tested     []string `json:"tested_versions,omitempty"`
	LastTested string   `json:"last_tested,omitempty"` // information only
	Unenforced bool     `json:"unenforced,omitempty"`  // renders no access policy
}

// AgentListResult is the result of agent.list and agent.reload.
type AgentListResult struct {
	Agents []AgentInfo `json:"agents"`
	// Errors lists broken user manifests, which were skipped.
	Errors []string `json:"errors,omitempty"`
}

// HookEventParams are what `tm hook` sends: one event of one session.
type HookEventParams struct {
	Session string         `json:"session"`
	Agent   string         `json:"agent"`
	Event   string         `json:"event"`
	Payload map[string]any `json:"payload"`
	// Token is the agent's prompt-channel token from the hook's
	// environment (the manifest's inject.token_env), when it has one.
	Token string `json:"token,omitempty"`
}

// HookEventResult is what `tm hook` prints back to the harness.
type HookEventResult struct {
	Stdout string `json:"stdout,omitempty"`
}

// Control methods of projects and threads (M6).
const (
	// MethodCLIRun runs a project command (tm task, thread, report,
	// status, done, …) inside the server for an agent caller: the server
	// tells the caller from the peer pid (docs/SPEC.md §11.1) and writes
	// the project folder with its own permissions, which a sandboxed
	// thread lacks (§5.2).
	MethodCLIRun = "cli.run"
)

// HumanOnly reports whether method m is one only the human calls: the
// server answers it on the human socket alone (ipc.HumanName, docs/SPEC.md
// §11.1), and still refuses an agent caller there. The press tokens of
// mod.action.check and mod.action.done are among them, and so is what
// starts, stops or types into a session, or steers the human's console
// (view.*, but view.digest, a test's request that a console check its
// mirror): an agent's own uses of those (tm thread start, prompt, …) run
// inside the server through cli.run, whose calls come in on the human
// socket after the command's own caller check.
func HumanOnly(m string) bool {
	switch m {
	case MethodProjectRename, MethodProjectActive,
		MethodModViews, MethodModViewInput, MethodModCommand, MethodModRestart,
		MethodNotifyClaim, MethodNotifyDismiss,
		MethodModActionCheck, MethodModActionDone, MethodSessionClear,
		MethodSessionStart, MethodSessionStop, MethodSessionKeys, MethodSessionPrompt,
		MethodSessionAdopt, MethodSessionAdoptAsk, MethodSessionRemote, MethodSessionModel, MethodQuestionsOpen,
		MethodServerStop, MethodAgentReload, MethodPeerReload, MethodPeerList, MethodProjectsWatch:
		return true
	}
	return strings.HasPrefix(m, "view.") && m != MethodViewDigest
}

// MethodProjectRename renames a project's slug (tm project rename,
// docs/SPEC.md §5.1): the server checks no thread of it runs, stops its
// coordinator, renames it between two ticker sweeps, and starts the
// coordinator again under the new slug. The human's.
const MethodProjectRename = "project.rename"

// MethodProjectActive activates or deactivates a project (tm project
// activate|deactivate, the sidebar's space, docs/SPEC.md §5.1): the
// server writes the setting, then stops the project's coordinator and
// threads, keeping their records dormant to resume, or resumes its
// dormant ones. The human's.
const MethodProjectActive = "project.active"

// ProjectActiveParams are the params of project.active.
type ProjectActiveParams struct {
	Project string `json:"project"`
	Active  bool   `json:"active"`
}

// ProjectActiveResult says what project.active did: whether the setting
// changed, and the sessions it stopped, resumed (or started fresh) and
// dropped, by name (coordinator, a thread id).
type ProjectActiveResult struct {
	Changed bool     `json:"changed"`
	Stopped []string `json:"stopped,omitempty"`
	Resumed []string `json:"resumed,omitempty"`
	Lost    []string `json:"lost,omitempty"`
}

// ProjectRenameParams are the params of project.rename. The slug is a
// project's only name.
type ProjectRenameParams struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// ProjectRenameResult is what the rename did: the project's folder and
// worktrees folder now ("" for none), how many thread records changed,
// what didn't go through, and the coordinator session started again
// ("" when none was running).
type ProjectRenameResult struct {
	Dir         string   `json:"dir"`
	Worktrees   string   `json:"worktrees,omitempty"`
	Threads     int      `json:"threads"`
	Notes       []string `json:"notes,omitempty"`
	Coordinator string   `json:"coordinator,omitempty"`
}

// CLIRunParams are the params of cli.run.
type CLIRunParams struct {
	Args  []string `json:"args"`
	Cwd   string   `json:"cwd"`
	Stdin []byte   `json:"stdin,omitempty"`
}

// CLIRunResult is the command's output and exit code.
type CLIRunResult struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Code   int    `json:"code"`
}

// MethodCallerWho asks the server who the calling process is (M4,
// docs/SPEC.md §11.1): the server walks the peer pid's ancestors to a
// hosted session.
const MethodCallerWho = "caller.who"

// CallerInfo is the result of caller.who. Kind is "human", "coordinator"
// or "thread".
type CallerInfo struct {
	Kind    string `json:"kind"`
	Project string `json:"project,omitempty"`
	Thread  string `json:"thread,omitempty"`
}

// MethodToolRun runs one of a session's tools (a thread's report,
// status, steps and done, and the tools user mods offer; docs/SPEC.md
// §8.6) for `tm mcp`, which serves them to agents that speak MCP. The
// server tells the session from the peer pid, as for cli.run, and
// answers like POST /v1/tools/{name} on the mod socket.
const MethodToolRun = "tool.run"

// MethodToolList lists the tools of the caller's session, found from
// the peer pid, as GET /v1/tools on the mod socket does: no params,
// answered with ToolListResult.
const MethodToolList = "tool.list"

// ToolDef is a tool as an agent is given it: its name, description and
// input as a JSON schema (MCP's tools/list shape, and what Claude's
// $.tool.register takes).
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ToolListResult is the answer of tool.list and GET /v1/tools.
type ToolListResult struct {
	Tools []ToolDef `json:"tools"`
}

// ToolRunParams are the params of tool.run: the tool and its input as
// the model gave it (a JSON object).
type ToolRunParams struct {
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input,omitempty"`
}

// ToolRunResult is the answer of tool.run: the tool's text, or with
// IsError why it was refused, for the model to read.
type ToolRunResult struct {
	Text    string `json:"text"`
	IsError bool   `json:"is_error,omitempty"`
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

// MethodModActionDone tells the server a human ran a core action that a
// mod's row, view or notification bound (the mods design §11; docs/SPEC.md
// §5.3, Action binding), so the mod gets action.done and can redraw. The
// console calls it after the core ran the action, or refused it as not
// available; human-only, refused for an agent, and only with the press
// token mod.action.check gave that console for that binding.
const MethodModActionDone = "mod.action.done"

// ModActionDoneParams are the params of mod.action.done: the mod whose
// binding was pressed, the action and its target, and whether it ran.
type ModActionDoneParams struct {
	ModActionParams
	OK bool `json:"ok"`
	// Token is the press token of the mod.action.check that let this
	// press run: good once, for this binding, from the console that
	// asked, for ModActionTokenTTL.
	Token string `json:"token"`
}

// MethodSessionClear clears an agent session's conversation
// (SessionIDParams): the server pastes its agent's clear prompt ([inject]
// clear, /clear for Claude) as a plain prompt, queued until the agent is
// idle, and journals it. Human-only: a console runs it for a mod's
// session.clear binding once the human confirmed it. The result is a
// SessionPromptResult.
const MethodSessionClear = "session.clear"

// MethodModActionCheck asks the server, before the console runs a
// pressed binding, whether it is in its mod's reach: project_read, and
// a target in a project the mod runs for (a session's own project).
// Human-only. In reach, it answers a press token (ModActionCheckResult)
// that the console's mod.action.done for this press must present.
const MethodModActionCheck = "mod.action.check"

// ModActionCheckResult is mod.action.check's answer: the press token.
type ModActionCheckResult struct {
	Token string `json:"token"`
}

// ModActionTokenTTL is how long a press token stays good: long enough
// for the human to answer the core's confirmation.
const ModActionTokenTTL = 30 * time.Minute

// ModActionParams name a mod's binding: the mod, the action and its
// target.
type ModActionParams struct {
	Mod    string            `json:"mod"`
	Action string            `json:"action"`
	Target map[string]string `json:"target,omitempty"`
}

// MethodNotifyClaim says a console draws mod notification id as an OS
// notification: the first caller gets OK, the others none, so that one
// console rings it. MethodNotifyDismiss takes it out of the tray (a
// button pressed, or the human dismissed it). Both are the human's.
const (
	MethodNotifyClaim   = "notify.claim"
	MethodNotifyDismiss = "notify.dismiss"
)

// NoticeParams are the params of notify.claim and notify.dismiss.
type NoticeParams struct {
	ID string `json:"id"`
}

// NoticeResult is their result.
type NoticeResult struct {
	OK bool `json:"ok"`
}

// MethodModHosts lists the supervised code mods' hosts and where each
// stands (mods design §8), for the Mods tab, tm doctor and tm mod dev.
const MethodModHosts = "mod.hosts"

// ModHostsResult is the answer of mod.hosts, by id. NoMods: the server
// runs in safe mode (tm --no-mods, TERMINATR_NO_MODS=1): no user mod
// runs.
type ModHostsResult struct {
	NoMods bool      `json:"no_mods,omitempty"`
	Hosts  []ModHost `json:"hosts"`
}

// ModHost is one code mod's host: State is idle, starting, running,
// backoff or failed; RSS its resident set in bytes when it runs; Next
// when a host in backoff restarts; Err why the last host ended, or why
// the mod failed.
type ModHost struct {
	ID       string    `json:"id"`
	State    string    `json:"state"`
	Since    time.Time `json:"since"`
	PID      int       `json:"pid,omitempty"`
	RSS      uint64    `json:"rss,omitempty"`
	Restarts int       `json:"restarts,omitempty"`
	Next     time.Time `json:"next,omitzero"`
	Err      string    `json:"err,omitempty"`
	// Sandbox is modsup.Status.Sandbox: the OS sandbox the host runs in,
	// or "none: <why>".
	Sandbox string `json:"sandbox,omitempty"`
}

// MethodModRestart reads the installed mods and config.toml again, then
// starts a code mod's host again now (the Mods tab's r, tm mod dev after
// each save): a failed mod gets a fresh count, an idle one starts, a mod
// just enabled runs. It answers as mod.hosts. The human's; refused in
// safe mode.
const MethodModRestart = "mod.restart"

// ModRestartParams are the params of mod.restart.
type ModRestartParams struct {
	ID string `json:"id"`
}

// MethodModViews is a console's poll of the views user mods draw (mods
// design §6): it says which views the console shows now (the server
// sends the mods view.opened and view.closed from that), and gets every
// view it may show, with the trees the mods published, and the views a
// mod asked this console to open after a human input. The human's.
const MethodModViews = "mod.views"

// ModViewsParams are the params of mod.views: the views the console
// shows now (none: it shows none).
type ModViewsParams struct {
	Shown []modview.Shown `json:"shown"`
}

// ModViewsResult is the answer of mod.views.
type ModViewsResult struct {
	Views []ModView `json:"views"`
	// Open are the mods' requests to open a view on this console, each
	// to act on once, by its seq.
	Open []modview.Open `json:"open,omitempty"`
}

// ModView is one view of an enabled code mod with the views grant.
type ModView struct {
	Mod string `json:"mod"`
	// Name is the mod's name, for a card's header.
	Name  string `json:"name"`
	View  string `json:"view"`
	Title string `json:"title"`
	Slot  string `json:"slot"` // project.tab, dashboard.card, full, a section's
	// Description is the view's line for the sidebar's details line, and
	// Opens a sidebar section's companion view: enter on its header
	// sends the mod a ui.click (mod API 1.7).
	Description string `json:"description,omitempty"`
	Opens       string `json:"opens,omitempty"`
	// State is "" while the mod's host runs, else "failed" (it failed or
	// waits to restart: Err says why, Log names its log): a failed
	// view shows that, never stale trees.
	State string `json:"state,omitempty"`
	Err   string `json:"err,omitempty"`
	Log   string `json:"log,omitempty"`
	// Projects are the projects the mod runs for; nil for all.
	Projects []string `json:"projects,omitempty"`
	// Trees are what the mod published: for one project, or for all.
	Trees []modview.Tree `json:"trees,omitempty"`
}

// ModViewFailed is a ModView state; the view failed.
const ModViewFailed = "failed"

// MethodModCommand runs one of a mod's commands (mods design §2 tier 4):
// the human's key or ≡ menu entry on a console, or tm mod run. The mod
// gets command.run. The human's: an agent is refused.
const MethodModCommand = "mod.command"

// ModCommandParams are the params of mod.command: the mod, its command,
// the project it runs in ("" for none) and the row selected then.
// Console says it comes from a console's key or menu, which lets the
// mod open one of its views there (as a key in its view does).
type ModCommandParams struct {
	Mod       string     `json:"mod"`
	Command   string     `json:"command"`
	Project   string     `json:"project,omitempty"`
	Selection *ModTarget `json:"selection,omitempty"`
	Console   bool       `json:"console,omitempty"`
}

// ModCommandResult answers mod.command. After: from a console, the view
// the mod may open shows up in that console's mod.views Open with a Seq
// greater than After (a session's console goes over to it).
type ModCommandResult struct {
	After uint64 `json:"after,omitempty"`
}

// ModTarget is a row a mod is told of: a project, task, thread or
// session (modbus.Target).
type ModTarget struct {
	Project string `json:"project,omitempty"`
	Task    string `json:"task,omitempty"`
	Thread  string `json:"thread,omitempty"`
	Session string `json:"session,omitempty"`
}

// MethodModViewInput sends a human's key or click in a mod's view to
// the mod, as ui.key or ui.click (mods design §6): only the keys the mod
// declared, and clicks on its rows and key hints; and where the cursor
// went, as ui.cursor. The human's: an agent is refused.
const MethodModViewInput = "mod.view.input"

// ModViewInputParams are the params of mod.view.input: Kind is
// widget.EventKey, widget.EventClick or widget.EventCursor, Widget the
// focused (or clicked) list or keys widget, Row the row under its cursor.
type ModViewInputParams struct {
	Mod     string `json:"mod"`
	View    string `json:"view"`
	Project string `json:"project,omitempty"`
	Kind    string `json:"kind"`
	Widget  string `json:"widget,omitempty"`
	Row     string `json:"row,omitempty"`
	Key     string `json:"key,omitempty"`
}

// ModViewInputResult is mod.view.input's answer to a key or a click:
// the mod's open requests that follow it have a Seq greater than After,
// as mod.command's (ModCommandResult).
type ModViewInputResult struct {
	After uint64 `json:"after,omitempty"`
}
