package proto

import (
	"time"

	"github.com/theclifmeister/terminatr/internal/modview"
)

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
