package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/caller"
	tmclient "github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/guardlog"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/models"
	"github.com/theclifmeister/terminatr/internal/modnotify"
	"github.com/theclifmeister/terminatr/internal/modstore"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/ticker"
)

// Data is one poll of everything the dashboard shows. The dashboard holds
// no state of its own (docs/SPEC.md §4): every refresh asks the server
// for sessions, and takes the projects as the server's projects.watch
// last sent them, or reads the project files without it.
type Data struct {
	ServerOK bool
	Sessions []proto.SessionInfo
	// ModNotices are the notifications of user mods in the server's tray
	// (modnotice.go).
	ModNotices []modnotify.Notice
	// Alerts is the server's notification count; the bell rings when it
	// goes up (docs/SPEC.md §4).
	Alerts   uint64
	Projects []ProjectData
	// Defaults are the all-projects settings, which a project follows
	// for each one it doesn't set; nil when they can't be read.
	Defaults *config.Safety
	// Env are the secret environment's tables, names and sources only:
	// all projects' under "", each project's own under its slug
	// (config.EnvOf).
	Env map[string]map[string]config.EnvSource
	// Sandbox are the sandbox tables: all projects' under "", each
	// project's own under its slug (config.SandboxOf).
	Sandbox map[string]config.SandboxTable
	// ContextHint is [ui] context_hint: the percent of its context window
	// from which a save and clear of a coordinator is offered; 0 for never.
	ContextHint int
	// NeedsFirst is [ui] needs_first: the sidebar lists the projects
	// where something needs you first.
	NeedsFirst bool
	// UpdateCheck is [ui] update_check, on unless set false.
	UpdateCheck bool
	// Server is what the server says about itself: its version and
	// build, and a newer release it found (docs/SPEC.md §4).
	Server ServerInfo
	Err    string // why the poll failed, shown in the header
	// NoAgent: no agent tm knows is installed, so nothing can start;
	// the header says so (docs/SPEC.md §8.2).
	NoAgent bool
	// Mods are the installed mods, by id, for the settings popup's Mods
	// tab; ModsErr says why they can't be listed. ModHosts are the
	// enabled code mods' hosts as the mod supervisor has them (mods
	// design §8: idle, starting, running, backoff, failed), by id; nil
	// when the server doesn't say (mod.hosts).
	Mods    []mod.Mod
	ModsErr string
	// ModData says, by mod id, which of the mod's data files don't load
	// (mod.CheckData), one per line.
	ModData  map[string]string
	ModHosts map[string]proto.ModHost
	// ModCommands are the enabled code mods' commands (mod.Registry.Commands).
	ModCommands []mod.ModCommand
	// NoMods: the server runs in safe mode (tm --no-mods): no user mod
	// runs (mod.hosts).
	NoMods bool
	// Cells are the mods' cells and status segments (session.list's
	// Mods and the sessions' Cells); nil for none.
	Cells *modCells
	// Toasts are the mods' toasts of the last few seconds (session.list's
	// Mods), oldest first.
	Toasts []proto.ModToast
	// Peers are the paired servers and their shared projects as the
	// server's mirror has them (peer.list, docs/SPEC.md §16.10), with
	// their share requests (§16.9); none
	// without any, or from a server that doesn't say. With any the
	// sidebar shows server groups (sidepeers.go).
	Peers []proto.PeerMirror
}

// ServerInfo is the running server's identity, from session.list.
type ServerInfo struct {
	Version, Build string
	// OS is its platform (runtime.GOOS), where its threads run; "" from
	// a server that doesn't say.
	OS string
	// Latest is the tag of a newer release; Upgrade how to install it.
	Latest, Upgrade string
}

// ProjectData is one project's rows.
type ProjectData struct {
	Slug   string
	Goal   string
	Repos  []string
	Counts map[string]int
	// Safety are the project's settings; nil when they can't be read.
	Safety *config.Safety
	// Own are the settings the project sets itself (config keys); it
	// follows all projects in the rest.
	Own []string
	// Unread counts every unhandled inbox item; Items are all of them,
	// for the inbox view.
	Unread int
	Items  []project.Item
	// Threads are the project's unresolved threads (M6).
	Threads []ThreadRow
	// Usage is what all the project's threads used, resolved ones
	// included; TaskUsage splits it by task number.
	Usage     thread.Usage
	TaskUsage map[int]thread.Usage
	// NeedsYou are the tasks in the board's Needs you group (review or
	// blocked), in board order, for NEEDS YOU.
	NeedsYou []*tasks.Task
	// Questions counts the coordinator's open questions (tm ask), a
	// NEEDS YOU row whose enter has it open them.
	Questions int
	// Checkouts are notes on the repos whose local default branch is
	// behind origin, by repo path, as the ticker last saw them.
	Checkouts map[string]string
	// Badges are the mods' annotations in the project, by target
	// (modstore.Task, modstore.Thread), of the mods that run for it.
	Badges map[string][]modstore.Badge
	// PRHostFails counts the PR polls in a row on which the project's PR
	// host failed, as the ticker last saw them; 0 while they work, or
	// when it has no PR host. The sidebar marks its name red (▲).
	PRHostFails int
	// Links are the project's links to other projects' coordinators
	// (link.Rows); LinkWaits counts its messages waiting for the user's
	// OK, its links the breaker paused and the paired servers' link
	// requests waiting for the user (LinkAsks of them) and the files
	// offered it (LinkFiles of them), a NEEDS YOU row.
	Links     []link.Row
	LinkWaits int
	LinkAsks  int
	LinkFiles int
	Err       string
}

// ThreadRow is one thread: its record (thread.toml) and its STATUS.md,
// nil when it has none yet.
type ThreadRow struct {
	*thread.Record
	Status *thread.Status
	// Report is the latest report (its PR line and ## Next), nil
	// without one; TaskRec is the linked task, for its steps.
	Report  *thread.Report
	TaskRec *tasks.Task
	// Checks are its pull request's checks as the ticker last saw them:
	// pass, fail or pending; "" without a PR or checks. PR is its
	// number, 0 for none.
	Checks string
	PR     int
}

// Source is the dashboard's view of the world; tests use a fake. It
// only reads, opens projects and asks the coordinator to
// act on a task: what happens to threads and tasks is the coordinator's
// (docs/SPEC.md §4). It is one interface per concern, so code and tests
// that need one concern take or fake only that one.
type Source interface {
	PollSource
	ProjectSource
	SettingsSource
	LinkSource
	PeerSource
	DeviceSource
	ModSource
	ModViewSource
	SessionSource
	LogSource
}

// LogSource reads the logs on the server's host (the , popup's Logs
// tab): log.list and log.read, the human's.
type LogSource interface {
	Logs() ([]proto.LogInfo, error)
	ReadLog(p proto.LogReadParams) (proto.LogReadResult, error)
}

// PollSource is what the dashboard reads: each poll, and on opening a
// popup or a tab.
type PollSource interface {
	Load() Data
	Board(slug string) (*tasks.Board, error)
	// Library lists the files of every thread's library, newest first,
	// for the project popup's Library tab; LibraryRead reads up to max
	// bytes of one (truncated says it was longer); LibraryRemove
	// deletes one of thread id's files, or all of them when name is
	// "", journaled, saying how many went.
	Library(slug string) ([]thread.LibFile, error)
	LibraryRead(slug, id, name string, max int64) (data []byte, truncated bool, err error)
	LibraryRemove(slug, id, name string) (int, error)
	// Designs are the project's designs with their build, from its
	// board and archive (project.DesignBuilds), for the project popup's
	// Designs tab.
	Designs(slug string) ([]project.DesignBuild, error)
	// RemoveNote deletes memory note path (memory/<name>.md) of the
	// project as tm memory rm does: MEMORY.md written again, journaled,
	// and an inbox item for the coordinator.
	RemoveNote(slug, path string) error
	// HasNote says whether the project has memory note path, a
	// regular file in its memory/ folder.
	HasNote(slug, path string) bool
	// Review is how to check task t, in review, and whether its change
	// has shipped.
	Review(slug string, t *tasks.Task) Review
	// Templates are the task templates of the mods that run for the
	// project (mod.Registry.Templates): what the new-task prompt offers.
	Templates(slug string) []mod.Template
	// Agents lists the installed agents: the ones tm can run.
	Agents() []string
	// Catalogs are the installed agents' models: what each agent said,
	// with the user's settings and the learnt refusals over it
	// (docs/SPEC.md §8.2, §11.2), for the General Models page and the
	// choices of the Thread models and Coordinator model settings.
	Catalogs() []Catalog
}

// ProjectSource opens projects and asks their coordinators to act:
// what happens to threads and tasks is the coordinator's (docs/SPEC.md §4).
type ProjectSource interface {
	NewProject(slug string) (string, error)
	// OpenProject returns the project's coordinator session, started
	// first if none runs.
	OpenProject(slug string, cols, rows int) (string, error)
	// Lifecycle activates, deactivates, pauses, resumes, archives,
	// deletes, pins or unpins a project (verb "activate", "deactivate",
	// "pause", "resume", "archive", "delete", "pin", "unpin"), saying
	// what it did (lifecycle.go).
	// Deactivating stops the project's coordinator and threads: the
	// caller asks first.
	Lifecycle(slug, verb string) (string, error)
	// AddTask adds a task to the project as the human, with template
	// tpl laid on it when not nil (tm task add --template), and returns
	// its ref (T12). It is refused when tm runs inside an agent.
	AddTask(slug, title string, tpl *mod.Template) (string, error)
	// Ask asks the project's coordinator to act on task id: an inbox
	// item of kind (project.KindDelegate, KindAccept or KindSendBack,
	// with the user's note) that is the user's word (docs/SPEC.md §4).
	// It reports false when an item already asks something of the task.
	Ask(slug string, id int, kind, note string) (bool, error)
	// AskAdopt asks the project's coordinator to adopt session s, an
	// agent session outside the projects, as a thread (docs/SPEC.md §4,
	// Adopt). It reports false when an item already asks it.
	AskAdopt(slug string, s proto.SessionInfo) (bool, error)
	// WithdrawAdopt takes the user's go-ahead to adopt session id back
	// (T again while it is asked), and the coordinator's item with it.
	WithdrawAdopt(slug, id string) error
	// OpenQuestions has the project's coordinator put its open questions
	// (tm ask) to the user in its question dialog (questions.open).
	OpenQuestions(slug string) (proto.QuestionsOpenResult, error)
	// SaveClear has the project's coordinator save what it learned, then
	// clears its conversation, as auto_clear does (coordinator.clear).
	SaveClear(slug string) (proto.CoordinatorClearResult, error)
	// SetRemote turns remote control of the project's running
	// coordinator on or off (prefix+r), saying what it did.
	SetRemote(slug string, on bool) (string, error)
}

// SettingsSource changes settings and models, on the human's keypress
// in a settings popup.
type SettingsSource interface {
	// SetSetting changes a setting, on the human's keypress in a settings
	// popup (docs/SPEC.md §11.2): key in table "" (the top level), "keys",
	// "defaults" (all projects) or "projects.<slug>"; a nil value removes
	// a project's own value, so it follows all projects again. It is
	// refused when tm runs inside an agent.
	SetSetting(table, key string, value any) error
	// ResetAccess puts table's Access switches back to what it follows
	// (config.ResetAccess), on the human's keypress; reset are the keys
	// it removed. Refused inside an agent.
	ResetAccess(table string) (reset []string, err error)
	// SetRepo adds or removes one of a project's repositories.
	SetRepo(slug, path string, add bool) error
	// ApplyModDefaults applies the defaults an enabled mod suggests for
	// all projects, on the human's keypress (ApplyModDefaults).
	ApplyModDefaults(id string) ([]mod.Suggestion, error)
	// SetModels writes an agent's model settings (config.toml
	// [agents.<name>]: hide, add, default_model); AgentSettings{} resets
	// it to what the agent lists. It is refused when tm runs inside an
	// agent.
	SetModels(agentName string, s config.AgentSettings) error
	// SyncModel applies the project's coordinator_model to its running
	// coordinator session (session.model): switched live when its agent
	// can, else on its next start.
	SyncModel(session string) error
	// RefreshModels asks the installed agent for its models again.
	RefreshModels(agentName string) error
	// Unrefuse forgets that the user's account refused one of the
	// agent's models. It is refused when tm runs inside an agent.
	Unrefuse(agentName, model string) error
	// GuardRefusals lists the guard's open refusals of project slug (all
	// projects' for ""), the latest first; GuardDecide allows refusal e
	// for its project (how "here") or all projects ("all"), telling
	// session it may retry, or keeps refusing it ("keep"), and says what
	// it did. Both are refused when tm runs inside an agent.
	GuardRefusals(slug string) ([]guardlog.Entry, error)
	GuardDecide(e guardlog.Entry, how, session string) (string, error)
	// SetSecretEnv names a secret variable of threads in table
	// ("defaults" or "projects.<slug>") with its source, or removes it
	// (src nil); TestSecretEnv has the server get the variable of
	// project slug's table (all projects' for "") from its source, and
	// says whether that worked, never the value. Both are refused when
	// tm runs inside an agent.
	SetSecretEnv(table, name string, src *config.EnvSource) error
	TestSecretEnv(slug, name string) (string, error)
	// EditSandbox changes the sandbox table of table ("defaults" or
	// "projects.<slug>") with fn, as config.EditSandbox does, and
	// journals what (e.g. "hosts + registry.npmjs.org"). Refused when tm
	// runs inside an agent.
	EditSandbox(table, what string, fn func(*config.SandboxTable) error) error
	// EditSandboxRule changes one entry of table's sandbox table in
	// place, as config.EditSandboxRule does (all projects' taking the
	// projects' drops of it along), and journals what. Refused when tm
	// runs inside an agent.
	EditSandboxRule(table, what string, e config.SandboxEdit) error
	// AccessShow is the effective access of thread id of project slug
	// (id "" for a new thread of it), read-only (access.show).
	AccessShow(slug, id string) (proto.AccessShowResult, error)
}

// LinkSource sets and works the links between projects.
type LinkSource interface {
	// SetLink links slug and other (link.Set): slug's direction allow,
	// other's the mirror, off removing both; SetLinkAsk turns slug's ask
	// switch for the link on or off (link.SetAsk). LinkLog reads the messages between them;
	// LinkAct approves, drops (arg a message id) or resumes (arg the
	// other project), or sends text to arg as the user; accepts,
	// declines or cancels a file (accept-file …, arg its X- id).
	// LinkFiles are the files between them (link.Transfers);
	// SetLinkFiles sets what slug's link to other does with files
	// (link.SetFiles). All but the reads are the human's, refused when
	// tm runs inside an agent.
	SetLink(slug, other, allow string) error
	// SaveLink writes slug's side of its link to other as the Network
	// popup's link dialog sets it, in one act (link.Save): on this
	// server both sides, to a paired server's project this side only;
	// it says what became of the files that waited on it, "" for none.
	SaveLink(slug, other string, l config.Link) (string, error)
	SetLinkAsk(slug, other string, ask bool) error
	SetLinkFiles(slug, other, files string) error
	LinkLog(slug, other string) ([]*link.Message, error)
	LinkFiles(slug, other string) ([]link.Transfer, error)
	LinkAct(slug, verb, arg, text string) (string, error)
}

// PeerSource manages the paired servers (the Network popup).
type PeerSource interface {
	// Peers is the paired servers as tm peer list has them (docs/SPEC.md
	// §16.8); RemovePeer unpins one as tm peer remove does, and has the
	// server close its link. Both are the human's, refused when tm runs
	// inside an agent.
	Peers() (PeerList, error)
	RemovePeer(alias string) error
	// SetPeerPanes sets what the paired server alias may do with this
	// server's panes (proto.PanesOff, PanesView, PanesType; §16.12), as
	// tm peer panes does, and has the server read the peers again.
	SetPeerPanes(alias, level string) error
	// SetPeerConversations turns the paired server alias's
	// conversations switch on or off (§16.14), as tm peer
	// conversations does, and has the server read the peers again.
	SetPeerConversations(alias string, on bool) error
	// SetPeerNeeds sets what the paired server alias may see of this
	// server's needs (proto.NeedsOff, NeedsView, NeedsAnswer; §16.16),
	// as tm peer needs does, and has the server read the peers again.
	SetPeerNeeds(alias, level string) error
	// ActOnNeed acts on a need of a paired server's shared project as
	// this server's user (peer.need-act, §16.16): that server checks it
	// again and refuses it when the need changed.
	ActOnNeed(p proto.PeerNeedActParams) (proto.PeerActResult, error)
	// SetPeerName gives the paired server alias a display name, or
	// clears it with "", as tm peer name does (§16.8), and has the
	// server read the peers again.
	SetPeerName(alias, name string) error
	// SetSelfName sets this server's own name, or clears it with "" for
	// the host name, as tm network name does (§16.8), and has the server
	// read the peers again, which tells the paired servers.
	SetSelfName(name string) error
	// SetPeerRelay gives the paired server alias another relay, as tm
	// peer relay does, and has the server read the peers again; warn
	// says when the relay is on a private address.
	SetPeerRelay(alias, relay string) (warn string, err error)
	// RelayUpdate asks the relay of the paired server alias to update
	// itself to the latest official release, as tm peer relay-update
	// does (§16.2), saying what came of it.
	RelayUpdate(alias string) (string, error)
	// PairPeer pairs this server with another through relay, as tm peer
	// pair does, and JoinPeer joins the pairing code names, as tm peer
	// join does: both pin the other server and have the server read the
	// peers again, and return its alias. ev shows the code and asks
	// for the six digits; ctx cancels. The human's, as above.
	PairPeer(ctx context.Context, relay string, ev PairEvents) (string, error)
	JoinPeer(ctx context.Context, code string, ev PairEvents) (string, error)
	// PeerPopup reads the project popup of the paired server alias's
	// shared project slug, read-only and cleaned (peer.popup-read,
	// §16.9): a *proto.Error with ErrUnknownMethod from a tm that
	// serves none, ErrPanesOff while its pane switch for this server is
	// off. The human's.
	PeerPopup(alias, slug string) (proto.PeerPopup, error)
	// AskShare asks the paired server alias's user to share a project
	// with this server, for project forSlug here ("" for none in
	// particular), with an optional one-line note (peer.share-ask,
	// §16.9); AnswerShare answers request id from one: accepting shares
	// project slug here, declining shares nothing (peer.share-answer).
	// A refusal is its words. The human's.
	AskShare(alias, forSlug, note string) (proto.ShareRequest, error)
	AnswerShare(id string, accept bool, slug string) (proto.ShareRequest, error)
}

// DeviceSource manages the paired devices (the Network popup's devices,
// docs/SPEC.md §16.15), as tm device does: all of it the human's,
// refused when tm runs inside an agent. Each change has a running
// server read the devices again. A device is named by its id.
type DeviceSource interface {
	// Devices is the paired devices with their links, as tm device
	// list has them.
	Devices() (DeviceList, error)
	// SetDeviceLevel sets a device's level (view, answer, write: above
	// view it needs a write key), as tm device access does;
	// RenameDevice renames it, as tm device name; RemoveDevice removes
	// it, its link closing, as tm device remove.
	SetDeviceLevel(id, level string) error
	RenameDevice(id, name string) error
	RemoveDevice(id string) error
	// PairDevice pairs a device through relay ("" for the one the paired
	// devices and servers use; errRelayNeeded when none is known), as
	// tm device pair does: ev shows the invite once it works and asks
	// about the device that joined; ctx cancels. It returns the device
	// added.
	PairDevice(ctx context.Context, relay string, ev DevicePairEvents) (DeviceEntry, error)
}

// ModSource installs, updates and runs mods (the Mods tab).
type ModSource interface {
	// ModAct enables, disables or removes installed mod id (verb
	// "enable", "disable", "remove"), on the human's answer in the Mods
	// tab (mods design §9); with slug, enable and disable do what tm mod
	// enable/disable --project does. Enabling grants every permission
	// the mod asks for: the human has just seen them. It is refused when
	// tm runs inside an agent, as every mod change is.
	ModAct(id, verb, slug string) error
	// SetModProjects limits mod id to projects, none for every project
	// (mod.SetProjects).
	SetModProjects(id string, projects []string) error
	// SetModGrants sets the permissions mod id runs with, of those it
	// asks for; SetModSetting one of its own settings, for all projects
	// or for project slug (mod API 1.11), nil for its default (or, for a
	// project, all projects' value).
	SetModGrants(id string, grants []string) error
	SetModSetting(id, slug, key string, value any) error
	// FetchModUpdate stages mod id's update from its source, nil when it
	// is up to date; ReplaceMod installs it, granting grant too (the new
	// permissions the human accepted). The caller closes the staged copy.
	FetchModUpdate(id string) (*mod.Staged, error)
	ReplaceMod(id string, st *mod.Staged, grant []string) error
	// FetchMod stages the mod at arg, a folder or a git URL as tm mod
	// install takes it, and checks that its id is free (mod.Taken), so
	// the Mods tab shows the consent of what InstallMod would install;
	// InstallMod installs it, disabled (tm mod install, mods design §8).
	// Both are refused when tm runs inside an agent. The caller closes
	// the staged copy.
	FetchMod(arg string) (*mod.Staged, error)
	InstallMod(st *mod.Staged) error
	// ModLog is the end of mod id's log, at most max bytes (cut says it
	// was longer); "" when it has none.
	ModLog(id string, max int64) (text string, cut bool, err error)
	// RestartMod starts code mod id's host again now (mod.restart): a
	// failed mod gets a fresh count. It is refused when tm runs inside
	// an agent.
	RestartMod(id string) error
}

// ModViewSource is the mods' views and the bindings and commands run
// from them.
type ModViewSource interface {
	// ModViews are the views user mods draw, with their trees and the
	// mods' requests to open one here; shown are the views this console
	// shows now (mod.views). ModViewInput sends a key or a click in a
	// mod's view to the mod (mod.view.input).
	ModViews(shown []modview.Shown) (proto.ModViewsResult, error)
	ModViewInput(p proto.ModViewInputParams) error
	// ModCommand runs one of a mod's commands (mod.command).
	ModCommand(p proto.ModCommandParams) error
	// ModActionDone tells the server a human ran (ok) or was refused an
	// action mod's row or button bound, for the mod's action.done, with
	// the press token ModActionCheck gave for that press.
	ModActionDone(mod, action string, target map[string]string, ok bool, token string) error
	// ModActionCheck refuses a binding outside its mod's reach (its
	// projects, project_read), asked before the console runs one; in
	// reach, it gives the press's token.
	ModActionCheck(mod, action string, target map[string]string) (token string, err error)
}

// SessionSource acts on a session, on the human's confirmed press of a
// mod's binding.
type SessionSource interface {
	// ClearSession clears session id's conversation (session.clear), on
	// the human's confirmed press of a mod's session.clear binding.
	ClearSession(id string) error
	// AnswerQuestion answers question index of the menu open on session
	// id with option, its label, on the human's confirmed press of a
	// mod's question.answer binding; refused unless that question still
	// asks question, unanswered (session.answer).
	AnswerQuestion(id string, index int, question, option string) error
}

// Review is what the user needs to review a task: how to check it (the
// thread report's ## Check) and where its pull request stands.
type Review struct {
	Check []string
	// PR is the pull request's number, 0 when none is known; Ship is
	// where it stands: ShipOpen, ShipClosed, ShipMerged, "" unknown.
	PR   int
	Ship string
}

// Where a task's change stands (Review.Ship).
const (
	ShipOpen   = "open"
	ShipClosed = "closed"
	ShipMerged = "merged"
)

// ServerSource is the real Source: the control socket plus the project
// folders.
type ServerSource struct {
	Paths tmclient.Paths
	// Caller is who acts: the human, unless tm runs inside an agent.
	Caller caller.Caller

	mu  sync.Mutex
	ctl *tmclient.Client
	// slow is what Load reads that an install, a setting or an edit
	// changes (slowCache).
	slow slowCache

	// feed is the projects as the server's projects.watch with detail
	// sends them, which Load shows (watched); stopFeed ends its run.
	// touched is when an action of this source last changed project
	// files itself.
	feedMu   sync.Mutex
	feed     *sideFeed
	stopFeed context.CancelFunc
	touched  time.Time
}

// touchWait is how long after an action Load reads the projects itself
// while the watch hasn't sent what came after it: the server looks once
// a watch poll.
const touchWait = 3 * time.Second

// slowMaxAge is how long what slowCache read serves at most, for what
// its stamp doesn't see: a mod's data file edited in its folder's
// subfolder, an agent installed on the PATH.
const slowMaxAge = 5 * time.Second

// slowCache is what the dashboard's Load reads that changes only on an
// install, a setting or an edit: the config, the mods' registry with
// their data files' checks, and whether any agent is installed. It reads
// them again when the stamp of config.toml, the mods and agents folders,
// each mod's folder and manifest changes, or slowMaxAge after.
type slowCache struct {
	mu       sync.Mutex // Loads may overlap: a tick's and an action's
	stamp    uint64
	at       time.Time
	modsDirs []string // the mods folder's entries, for the stamp
	slowData
}

// slowData is what slowCache read. Its callers only read it.
type slowData struct {
	cfg     *config.Config // nil when it doesn't load, and cfgErr says why
	cfgErr  error
	reg     *mod.Registry
	regErr  error
	modData map[string]string
	noAgent bool
}

// load is what it read, read again where its stamp changed; agentsDir
// is the agents' folder.
func (sc *slowCache) load(agentsDir string) slowData {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	st := sc.stampNow(agentsDir)
	if !sc.at.IsZero() && st == sc.stamp && time.Since(sc.at) < slowMaxAge {
		return sc.slowData
	}
	cfg, err := config.Load()
	if err != nil {
		cfg = nil
	}
	sc.cfg, sc.cfgErr = cfg, err
	sc.reg, sc.regErr = mod.Load(cfg)
	sc.modData = nil
	if sc.regErr == nil {
		sc.modData = modData(sc.reg.Mods)
	}
	sc.noAgent = false
	if reg, _ := agent.Load(agentsDir); reg != nil {
		sc.noAgent = len(models.Installed(reg, nil)) == 0
	}
	sc.modsDirs = nil
	if dir, err := home.ModsDir(); err == nil {
		if ents, err := os.ReadDir(dir); err == nil {
			for _, e := range ents {
				sc.modsDirs = append(sc.modsDirs, filepath.Join(dir, e.Name()))
			}
		}
	}
	// Stamped after the reads, with the mods they found: a change while
	// reading shows slowMaxAge later at most.
	sc.stamp, sc.at = sc.stampNow(agentsDir), time.Now()
	return sc.slowData
}

// stampNow is the stamp of the files load reads: config.toml, the mods
// folder, each mod's folder and manifest as the last load listed them,
// the agents folder.
func (sc *slowCache) stampNow(agentsDir string) uint64 {
	path, _ := config.Path()
	paths := []string{path, agentsDir}
	if dir, err := home.ModsDir(); err == nil {
		paths = append(paths, dir)
	}
	for _, d := range sc.modsDirs {
		paths = append(paths, d, filepath.Join(d, mod.ManifestFile))
	}
	return fsx.StatStamp(paths...)
}

// call makes one control call, reconnecting once if the connection broke.
func (s *ServerSource) call(method string, params, result any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for try := 0; ; try++ {
		if s.ctl == nil {
			c, err := tmclient.Connect(s.Paths, true)
			if err != nil {
				return err
			}
			s.ctl = c
		}
		err := s.ctl.Call(method, params, result)
		var perr *proto.Error
		if err == nil || errors.As(err, &perr) || try > 0 {
			return err
		}
		s.ctl.Close()
		s.ctl = nil
	}
}

// Close drops the control connection and ends the projects' watch.
func (s *ServerSource) Close() {
	s.mu.Lock()
	if s.ctl != nil {
		s.ctl.Close()
		s.ctl = nil
	}
	s.mu.Unlock()
	s.feedMu.Lock()
	defer s.feedMu.Unlock()
	if s.stopFeed != nil {
		s.stopFeed()
		s.feed.close()
		s.feed, s.stopFeed = nil, nil
	}
}

// watched is the projects as the server's projects.watch with detail
// last sent them, the watch started at the first ask; false while it
// doesn't run (no server, or one of an older protocol), or for a while
// after an action of this source changed project files (touch).
func (s *ServerSource) watched() ([]ProjectData, bool) {
	s.feedMu.Lock()
	if s.feed == nil {
		f, list := watchSide(s.Paths, true)
		if f.st != nil {
			f.putList(list)
		}
		ctx, cancel := context.WithCancel(context.Background())
		s.feed, s.stopFeed = f, cancel
		go f.run(ctx, s.Paths)
	}
	f, touched := s.feed, s.touched
	s.feedMu.Unlock()
	return f.current(touched, touchWait)
}

// touch notes that an action changed project files: Load reads them
// itself until the watch sends what came after.
func (s *ServerSource) touch() {
	s.feedMu.Lock()
	defer s.feedMu.Unlock()
	s.touched = time.Now()
}

func (s *ServerSource) Load() Data {
	d := Data{UpdateCheck: true, ContextHint: config.DefaultContextHint}
	var res proto.SessionListResult
	if err := s.call(proto.MethodSessionList, nil, &res); err != nil {
		d.Err = err.Error()
	} else {
		d.ServerOK, d.Sessions, d.Alerts, d.ModNotices = true, res.Sessions, res.Alerts, res.ModNotices
		d.Cells = newModCells(res.Mods, res.Sessions)
		if res.Mods != nil {
			d.Toasts = res.Mods.Toasts
		}
		d.Server = ServerInfo{Version: res.Version, Build: res.Build, OS: res.OS, Latest: res.Latest, Upgrade: res.Upgrade}
	}
	sc := s.slow.load(s.Paths.AgentsDir())
	cfg := sc.cfg
	if sc.regErr != nil {
		d.ModsErr = sc.regErr.Error()
	} else {
		d.Mods, d.ModData, d.ModCommands = sc.reg.Mods, sc.modData, sc.reg.Commands()
	}
	if d.ServerOK {
		d.Peers = loadPeers(s.call)
		var hosts proto.ModHostsResult
		if err := s.call(proto.MethodModHosts, nil, &hosts); err == nil {
			d.NoMods = hosts.NoMods
			d.ModHosts = make(map[string]proto.ModHost, len(hosts.Hosts))
			for _, h := range hosts.Hosts {
				d.ModHosts[h.ID] = h
			}
		}
	}
	if cfg != nil {
		d.ContextHint, d.UpdateCheck = cfg.ContextHint, cfg.UpdateCheck
		d.NeedsFirst = cfg.NeedsFirst
		if all, err := cfg.AllProjects(); err == nil {
			d.Defaults = &all
		}
		d.Env = map[string]map[string]config.EnvSource{"": cfg.EnvOf("")}
		d.Sandbox = map[string]config.SandboxTable{"": cfg.SandboxOf("")}
		for _, slug := range cfg.ProjectSlugs() {
			d.Env[slug] = cfg.EnvOf(slug)
			d.Sandbox[slug] = cfg.SandboxOf(slug)
		}
	}
	d.NoAgent = sc.noAgent
	if ps, ok := s.watched(); ok {
		d.Projects = ps
		if sc.cfgErr != nil && d.Err == "" {
			d.Err = sc.cfgErr.Error() // project.List's
		}
		return d
	}
	s.loadProjects(&d)
	return d
}

// loadProjects reads every project's rows from its files into d, as
// the server's watch sends them when it runs (server.ReadProjects), with
// the badges of d's mods.
func (s *ServerSource) loadProjects(d *Data) {
	ps, err := server.ReadProjects(ticker.StatePath(s.Paths.Sessions), d.Mods)
	if err != nil && d.Err == "" {
		d.Err = err.Error()
	}
	d.Projects = sideProjects(ps)
}

// loadPeers asks the server for its mirror of the paired servers
// (peer.list) by call: none when it has none, or can't say (an older
// server, or a caller that isn't the human).
func loadPeers(call func(method string, params, result any) error) []proto.PeerMirror {
	var l proto.PeerList
	if err := call(proto.MethodPeerList, nil, &l); err != nil || len(l.Peers) == 0 {
		return nil
	}
	return l.Peers
}

func (s *ServerSource) Board(slug string) (*tasks.Board, error) {
	p, err := project.Open(slug)
	if err != nil {
		return nil, err
	}
	return p.Tasks().Load()
}

// Library is the project's library files and, in their own groups, the
// files its links brought (link.LibraryFiles).
func (s *ServerSource) Library(slug string) ([]thread.LibFile, error) {
	p, err := project.Open(slug)
	if err != nil {
		return nil, err
	}
	files, err := thread.Library(p)
	if err != nil {
		return nil, err
	}
	return mergeNewest(files, link.LibraryFiles(p)), nil
}

func (s *ServerSource) Designs(slug string) ([]project.DesignBuild, error) {
	p, err := project.Open(slug)
	if err != nil {
		return nil, err
	}
	return p.BoardDesigns()
}

// mergeNewest merges two lists of files, each newest first.
func mergeNewest(a, b []thread.LibFile) []thread.LibFile {
	out := make([]thread.LibFile, 0, len(a)+len(b))
	for len(a) > 0 || len(b) > 0 {
		if len(b) == 0 || len(a) > 0 && !b[0].Time.After(a[0].Time) {
			out, a = append(out, a[0]), a[1:]
		} else {
			out, b = append(out, b[0]), b[1:]
		}
	}
	return out
}

// LibraryRead reads a thread's library file, or, for id a message's,
// the file it brought.
func (s *ServerSource) LibraryRead(slug, id, name string, max int64) ([]byte, bool, error) {
	p, err := project.Open(slug)
	if err != nil {
		return nil, false, err
	}
	if link.ValidID(id) {
		return link.ReadFile(p, id, name, max)
	}
	return thread.ReadLibraryFile(p, id, name, max)
}

func (s *ServerSource) LibraryRemove(slug, id, name string) (int, error) {
	if s.Caller.IsAgent() {
		return 0, errors.New("human-only: library files are deleted by the human")
	}
	p, err := project.Open(slug)
	if err != nil {
		return 0, err
	}
	if link.ValidID(id) {
		if err := link.RemoveFile(p, s.Caller, id, name); err != nil {
			return 0, err
		}
		return 1, nil
	}
	return thread.RemoveLibrary(p, s.Caller, id, name)
}

func (s *ServerSource) HasNote(slug, path string) bool {
	p, err := project.Open(slug)
	return err == nil && p.HasNote(path)
}

func (s *ServerSource) RemoveNote(slug, path string) error {
	if s.Caller.IsAgent() {
		return errors.New("human-only: the human deletes memory notes from the console")
	}
	p, err := project.Open(slug)
	if err != nil {
		return err
	}
	rel, err := p.NotePath(path)
	if err != nil {
		return err
	}
	if err := p.RemoveNote(rel); err != nil {
		return err
	}
	return p.NoteChanged(s.Caller, project.NoteRm, rel)
}

// errHumanOnly refuses a settings change from inside an agent.
var errHumanOnly = errors.New("human-only: settings are changed by the human")

func (s *ServerSource) Agents() []string {
	reg, _ := agent.Load(s.Paths.AgentsDir())
	return models.Installed(reg, nil)
}

// inactive says whether the user deactivated the project (§5.1).
func (p ProjectData) inactive() bool { return p.Safety != nil && !p.Safety.Active }

// pinned says the user pinned the project to the top of the sidebar.
func (p ProjectData) pinned() bool { return p.Safety != nil && p.Safety.Pinned }

// modData are the mods' data files that don't load, by mod id
// (Data.ModData): of the mods that load, not the built-in ones, which
// tests check.
func modData(mods []mod.Mod) map[string]string {
	var out map[string]string
	for _, md := range mods {
		if md.Builtin || md.State != mod.Enabled && md.State != mod.Disabled {
			continue
		}
		if errs := md.CheckData(); len(errs) > 0 {
			lines := make([]string, len(errs))
			for i, e := range errs {
				lines[i] = oneLine(e.Error())
			}
			if out == nil {
				out = map[string]string{}
			}
			out[md.ID] = strings.Join(lines, "\n")
		}
	}
	return out
}
