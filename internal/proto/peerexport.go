package proto

import "time"

// What a paired server may read of this one (docs/SPEC.md §16.9): the
// projects the user shares, each as its ProjectWatch and its sessions,
// trimmed. These cross a link (internal/peer), never the control socket.
const (
	// MethodPeerProjects lists the shared projects: a PeerProjects.
	MethodPeerProjects = "peer.projects"
	// MethodPeerWatch reads a shared project (PeerWatchParams): a
	// PeerWatch once its state differs from the params' Rev, or the
	// same state again after Wait.
	MethodPeerWatch = "peer.watch"
)

// ErrNotShared: no shared project has that slug. A project that isn't
// shared and one that doesn't exist answer alike.
const ErrNotShared = "not-shared"

// PeerProjects is the answer of peer.projects.
type PeerProjects struct {
	Projects []PeerProject `json:"projects"`
	// Now is the peer's clock as it answered: the reader's mirror
	// measures the offset between the two clocks from it.
	Now time.Time `json:"now,omitzero"`
}

// PeerProject is one shared project as the list shows it: its goal line,
// whether it is paused, and its counts.
type PeerProject struct {
	Project  string `json:"project"`
	Goal     string `json:"goal,omitempty"`
	Paused   bool   `json:"paused,omitempty"`
	NeedsYou int    `json:"needs_you"`
	Threads  int    `json:"threads"`
	Ready    int    `json:"ready"`
}

// PeerWatchParams are the params of peer.watch. Rev is the Rev of the
// state the caller has ("" for none): the answer waits until the state
// differs from it, at most Wait seconds (default and most
// PeerWatchMaxWait), and then answers the state as it is.
type PeerWatchParams struct {
	Project string `json:"project"`
	Rev     string `json:"rev,omitempty"`
	Wait    int    `json:"wait,omitempty"`
}

// PeerWatchMaxWait is the longest peer.watch waits for a change, in
// seconds: well inside a link's idle time.
const PeerWatchMaxWait = 50

// PeerWatch is a shared project's state: its ProjectWatch as the /tm
// pane has it, links only to other shared projects, and its sessions,
// trimmed. Rev names the state; Now is the peer's clock as it answered,
// outside the state Rev names.
type PeerWatch struct {
	Rev      string        `json:"rev"`
	Now      time.Time     `json:"now,omitzero"`
	Watch    ProjectWatch  `json:"watch"`
	Sessions []PeerSession `json:"sessions"`
}

// PeerSession is a SessionInfo with only what a paired server may see:
// no argv, working directory, pid, agent session id, terminal title or
// question text, no mod cells.
type PeerSession struct {
	ID            string    `json:"id"`
	Role          string    `json:"role"`
	Agent         string    `json:"agent,omitempty"`
	Project       string    `json:"project"`
	Thread        string    `json:"thread,omitempty"`
	Task          string    `json:"task,omitempty"`
	Created       time.Time `json:"created"`
	State         string    `json:"state,omitempty"`
	Reason        string    `json:"reason,omitempty"`
	TodosDone     int       `json:"todos_done,omitempty"`
	TodosTotal    int       `json:"todos_total,omitempty"`
	Current       string    `json:"current,omitempty"`
	Queued        int       `json:"queued_prompts,omitempty"`
	RemoteControl bool      `json:"remote_control,omitempty"`
	Model         string    `json:"model,omitempty"`
	// Asking: a question menu is open in the agent (its text stays
	// here).
	Asking        bool  `json:"asking,omitempty"`
	Context       int64 `json:"context,omitempty"`
	ContextWindow int64 `json:"context_window,omitempty"`
}

// PeerSessionOf is s as a paired server may see it. Fields are copied
// one by one: a field added to SessionInfo stays home until it is added
// here.
func PeerSessionOf(s SessionInfo) PeerSession {
	return PeerSession{ID: s.ID, Role: s.Role, Agent: s.Agent, Project: s.Project, Thread: s.Thread, Task: s.Task,
		Created: s.Created, State: s.State, Reason: s.Reason, TodosDone: s.TodosDone, TodosTotal: s.TodosTotal,
		Current: s.Current, Queued: s.Queued, RemoteControl: s.RemoteControl, Model: s.Model, Asking: s.Question != nil,
		Context: s.Context, ContextWindow: s.ContextWindow}
}
