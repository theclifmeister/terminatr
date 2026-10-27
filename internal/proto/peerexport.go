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

// ErrNotShared means no shared project has that slug. A project that isn't
// shared and one that doesn't exist answer alike.
const ErrNotShared = "not-shared"

// PeerProjects is the answer of peer.projects.
type PeerProjects struct {
	Projects []PeerProject `json:"projects"`
	// Panes is the answering server's pane switch for the caller
	// (PanesOff, PanesView, PanesType; docs/SPEC.md §16.12), from a
	// server whose hello lists pane-vt.
	Panes string `json:"panes,omitempty"`
	// Conversations is the answering server's conversations switch for
	// the caller (ConversationsOn, ConversationsNot; docs/SPEC.md
	// §16.14), from a server whose hello lists transcript.
	Conversations string `json:"conversations,omitempty"`
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

// PeerQuestion stands for a question's text in what a paired server
// sees: that one is open, never what it asks.
const PeerQuestion = "a question in its session"

// PeerProjectWatch is w as a paired server may see it (docs/SPEC.md
// §16.9): no mods' cells, no question's text (PeerQuestion says one is
// open), and of the inbox only each item's kind, count and task. As in
// PeerSessionOf, fields are copied one by one, down to the nested
// records: a field added to any of them stays home until it is added
// here. Links are copied whole here; which may show is the caller's.
func PeerProjectWatch(w ProjectWatch) ProjectWatch {
	out := ProjectWatch{Project: w.Project, Questions: w.Questions,
		NeedsYou: make([]WatchNeed, 0, len(w.NeedsYou)), Inbox: make([]WatchItem, 0, len(w.Inbox)),
		Threads: make([]WatchThread, 0, len(w.Threads)), Ready: make([]WatchTodo, 0, len(w.Ready))}
	question := func(q string) string {
		if q == "" {
			return ""
		}
		return PeerQuestion
	}
	for _, n := range w.NeedsYou {
		out.NeedsYou = append(out.NeedsYou, WatchNeed{Why: n.Why, Task: n.Task, Title: n.Title, Status: n.Status,
			Thread: n.Thread, Session: n.Session, Question: question(n.Question), PR: n.PR, PRURL: n.PRURL,
			PRNumber: n.PRNumber, Mergeable: n.Mergeable, Asked: n.Asked})
	}
	for _, it := range w.Inbox {
		out.Inbox = append(out.Inbox, WatchItem{Kind: it.Kind, NeedsUser: it.NeedsUser, Count: it.Count,
			Task: it.Task, Title: it.Title})
	}
	for _, t := range w.Threads {
		wt := WatchThread{ID: t.ID, Title: t.Title, Session: t.Session, State: t.State, Reason: t.Reason,
			NeedsYou: question(t.NeedsYou), PR: t.PR, PRURL: t.PRURL, PRBad: t.PRBad, PRChecks: t.PRChecks,
			PRChecked: t.PRChecked, Reports: t.Reports, Done: t.Done}
		if t.Task != nil {
			wt.Task = &WatchTask{ID: t.Task.ID, Title: t.Task.Title, Status: t.Task.Status,
				StepsDone: t.Task.StepsDone, StepsTotal: t.Task.StepsTotal, Current: t.Task.Current}
		}
		out.Threads = append(out.Threads, wt)
	}
	for _, r := range w.Ready {
		out.Ready = append(out.Ready, WatchTodo{Task: r.Task, Title: r.Title, Status: r.Status, Asked: r.Asked})
	}
	if c := w.Context; c != nil {
		out.Context = &WatchContext{Tokens: c.Tokens, Window: c.Window, Percent: c.Percent, Threshold: c.Threshold, Hint: c.Hint}
	}
	if t := w.Ticker; t != nil {
		out.Ticker = &WatchTicker{PRChecked: t.PRChecked, Synced: t.Synced, PRPollSeconds: t.PRPollSeconds,
			GHFailing: t.GHFailing, NoPRHost: t.NoPRHost}
	}
	for _, l := range w.Links {
		out.Links = append(out.Links, WatchLink{Peer: l.Peer, State: l.State, Send: l.Send, Receive: l.Receive,
			Paused: l.Paused, Unread: l.Unread, Held: l.Held, Queued: l.Queued, Last: l.Last})
	}
	return out
}
