package tui

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modannot"
	"github.com/theclifmeister/terminatr/internal/models"
	"github.com/theclifmeister/terminatr/internal/modnotify"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/ticker"
	"github.com/theclifmeister/terminatr/internal/worktree"
)

// Data is one poll of everything the dashboard shows. The dashboard holds
// no state of its own (docs/SPEC.md §4): every refresh asks the server
// for sessions and reads the project files.
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
	// AgentMod, AgentModBand and AgentModPane are the settings popup's Agent mod, Agent mod
	// band and Agent mod pane (docs/SPEC.md §8.6); the band is on unless set
	// off, the pane off unless set on.
	AgentMod, AgentModBand, AgentModPane bool
	// ContextHint is [ui] context_hint: the percent of its context window
	// from which a coordinator is told to consider /clear; 0 for never.
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
	// server's mirror has them (peer.list, docs/SPEC.md §16.10); none
	// without any, or from a server that doesn't say. With any the
	// sidebar shows server groups (sidepeers.go).
	Peers []proto.PeerMirror
}

// ServerInfo is the running server's identity, from session.list.
type ServerInfo struct {
	Version, Build string
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
	// (modannot.Task, modannot.Thread), of the mods that run for it.
	Badges map[string][]modannot.Badge
	// PRHostFails counts the PR polls in a row on which the project's PR
	// host failed, as the ticker last saw them; 0 while they work, or
	// when it has no PR host. The sidebar marks its name red (▲).
	PRHostFails int
	// Links are the project's links to other projects' coordinators
	// (link.Rows); LinkWaits counts its messages waiting for the user's
	// OK and its links the breaker paused, a NEEDS YOU row.
	Links     []link.Row
	LinkWaits int
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
// (docs/SPEC.md §4).
type Source interface {
	Load() Data
	Board(slug string) (*tasks.Board, error)
	NewProject(slug string) (string, error)
	// OpenProject returns the project's coordinator session, started
	// first if none runs.
	OpenProject(slug string, cols, rows int) (string, error)
	// SetSetting changes a setting, on the human's keypress in a settings
	// popup (docs/SPEC.md §11.2): key in table "" (the top level), "keys",
	// "defaults" (all projects) or "projects.<slug>"; a nil value removes
	// a project's own value, so it follows all projects again. It is
	// refused when tm runs inside an agent.
	SetSetting(table, key string, value any) error
	// SetLink links slug and other (link.Set): slug's direction allow,
	// other's the mirror, off removing both; SetLinkAsk turns slug's ask
	// switch for the link on or off (link.SetAsk). LinkLog reads the messages between them;
	// LinkAct approves, drops (arg a message id) or resumes (arg the
	// other project), or sends text to arg as the user. All are the
	// human's, refused when tm runs inside an agent.
	SetLink(slug, other, allow string) error
	SetLinkAsk(slug, other string, ask bool) error
	LinkLog(slug, other string) ([]*link.Message, error)
	LinkAct(slug, verb, arg, text string) (string, error)
	// ApplyModDefaults applies the defaults an enabled mod suggests for
	// all projects, on the human's keypress (ApplyModDefaults).
	ApplyModDefaults(id string) ([]mod.Suggestion, error)
	// SetRepo adds or removes one of a project's repositories.
	SetRepo(slug, path string, add bool) error
	// Templates are the task templates of the mods that run for the
	// project (mod.Registry.Templates): what the new-task prompt offers.
	Templates(slug string) []mod.Template
	// AddTask adds a task to the project as the human, with template
	// tpl laid on it when not nil (tm task add --template), and returns
	// its ref (T12). It is refused when tm runs inside an agent.
	AddTask(slug, title string, tpl *mod.Template) (string, error)
	// Agents lists the installed agents: the ones tm can run.
	Agents() []string
	// ModsNote names the agents the Agent mod setting is for and the version
	// each needs ("Claude Code 2.1.289").
	ModsNote() string
	// Catalogs are the installed agents' models: what each agent said,
	// with the user's settings and the learnt refusals over it
	// (docs/SPEC.md §8.2, §11.2), for the General Models page and the
	// choices of the Thread models and Coordinator model settings.
	Catalogs() []Catalog
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
	// Review is how to check task t, in review, and whether its change
	// has shipped.
	Review(slug string, t *tasks.Task) Review
	// SetRemote turns remote control of the project's running
	// coordinator on or off (prefix+r), saying what it did.
	SetRemote(slug string, on bool) (string, error)
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
	// Lifecycle activates, deactivates, pauses, resumes, archives,
	// deletes, pins or unpins a project (verb "activate", "deactivate",
	// "pause", "resume", "archive", "delete", "pin", "unpin"), saying
	// what it did (lifecycle.go).
	// Deactivating stops the project's coordinator and threads: the
	// caller asks first.
	Lifecycle(slug, verb string) (string, error)
	// Library lists the files of every thread's library, newest first,
	// for the project popup's Library tab; LibraryRead reads up to max
	// bytes of one (truncated says it was longer); LibraryRemove
	// deletes one of thread id's files, or all of them when name is
	// "", journaled, saying how many went.
	Library(slug string) ([]thread.LibFile, error)
	LibraryRead(slug, id, name string, max int64) (data []byte, truncated bool, err error)
	LibraryRemove(slug, id, name string) (int, error)
	// ModActionDone tells the server a human ran (ok) or was refused an
	// action mod's row or button bound, for the mod's action.done, with
	// the press token ModActionCheck gave for that press.
	ModActionDone(mod, action string, target map[string]string, ok bool, token string) error
	// ModActionCheck refuses a binding outside its mod's reach (its
	// projects, project_read), asked before the console runs one; in
	// reach, it gives the press's token.
	ModActionCheck(mod, action string, target map[string]string) (token string, err error)
	// ClearSession clears session id's conversation (session.clear), on
	// the human's confirmed press of a mod's session.clear binding.
	ClearSession(id string) error
	// AnswerQuestion answers question index of the menu open on session
	// id with option, its label, on the human's confirmed press of a
	// mod's question.answer binding; refused unless that question still
	// asks question, unanswered (session.answer).
	AnswerQuestion(id string, index int, question, option string) error
	// ModAct enables, disables or removes installed mod id (verb
	// "enable", "disable", "remove"), on the human's answer in the Mods
	// tab (mods design §9). Enabling grants every permission the mod
	// asks for: the human has just seen them. It is refused when tm runs
	// inside an agent, as every mod change is.
	ModAct(id, verb string) error
	// SetModGrants sets the permissions mod id runs with, of those it
	// asks for; SetModSetting one of its own settings, nil for its
	// default.
	SetModGrants(id string, grants []string) error
	SetModSetting(id, key string, value any) error
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
	// ModViews are the views user mods draw, with their trees and the
	// mods' requests to open one here; shown are the views this console
	// shows now (mod.views). ModViewInput sends a key or a click in a
	// mod's view to the mod (mod.view.input).
	ModViews(shown []modview.Shown) (proto.ModViewsResult, error)
	ModViewInput(p proto.ModViewInputParams) error
	// ModCommand runs one of a mod's commands (mod.command).
	ModCommand(p proto.ModCommandParams) error
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
	Paths server.Paths
	// Caller is who acts: the human, unless tm runs inside an agent.
	Caller caller.Caller

	mu  sync.Mutex
	ctl *server.Client
}

// call makes one control call, reconnecting once if the connection broke.
func (s *ServerSource) call(method string, params, result any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for try := 0; ; try++ {
		if s.ctl == nil {
			c, err := server.Connect(s.Paths, true)
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

// Close drops the control connection.
func (s *ServerSource) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctl != nil {
		s.ctl.Close()
		s.ctl = nil
	}
}

func (s *ServerSource) Load() Data {
	d := Data{AgentModBand: true, UpdateCheck: true, ContextHint: config.DefaultContextHint}
	var res proto.SessionListResult
	if err := s.call(proto.MethodSessionList, nil, &res); err != nil {
		d.Err = err.Error()
	} else {
		d.ServerOK, d.Sessions, d.Alerts, d.ModNotices = true, res.Sessions, res.Alerts, res.ModNotices
		d.Cells = newModCells(res.Mods, res.Sessions)
		if res.Mods != nil {
			d.Toasts = res.Mods.Toasts
		}
		d.Server = ServerInfo{Version: res.Version, Build: res.Build, Latest: res.Latest, Upgrade: res.Upgrade}
	}
	cfg, err := config.Load()
	if err != nil {
		cfg = nil
	}
	if reg, err := mod.Load(cfg); err != nil {
		d.ModsErr = err.Error()
	} else {
		d.Mods, d.ModData, d.ModCommands = reg.Mods, modData(reg.Mods), reg.Commands()
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
		d.AgentMod, d.AgentModBand, d.AgentModPane, d.ContextHint, d.UpdateCheck = cfg.AgentMod, cfg.AgentModBand, cfg.AgentModPane, cfg.ContextHint, cfg.UpdateCheck
		d.NeedsFirst = cfg.NeedsFirst
		if all, err := cfg.AllProjects(); err == nil {
			d.Defaults = &all
		}
	}
	if reg, _ := agent.Load(s.Paths.AgentsDir()); reg != nil {
		d.NoAgent = len(models.Installed(reg, nil)) == 0
	}
	list, err := project.List()
	if err != nil && d.Err == "" {
		d.Err = err.Error()
	}
	badgers := badgeMods(d.Mods)
	fails := ticker.HostFails(ticker.StatePath(s.Paths.Sessions))
	for _, sum := range list {
		if sum.Safety != nil && sum.Safety.Archived {
			continue // hidden: tm project unarchive brings it back
		}
		pd := ProjectData{Slug: sum.Slug, Goal: sum.Goal, Repos: sum.Repos,
			Counts: sum.Counts, Safety: sum.Safety, Own: sum.Own, Err: sum.Error,
			Checkouts: ticker.Checkouts(ticker.StatePath(s.Paths.Sessions), sum.Slug)}
		if p, err := project.Open(sum.Slug); err == nil {
			b, err := p.Tasks().Load()
			if err != nil {
				b = nil
			}
			if b != nil {
				for _, t := range b.Tasks {
					if tasks.GroupOf(t.Status) == tasks.NeedsYou {
						pd.NeedsYou = append(pd.NeedsYou, t)
					}
				}
			}
			recs, _ := thread.List(p)
			pd.TaskUsage, pd.Usage = thread.TaskUsage(recs)
			prs := ticker.PRs(ticker.StatePath(s.Paths.Sessions), sum.Slug)
			for _, r := range recs {
				if r.State == thread.Resolved {
					continue
				}
				tr := ThreadRow{Record: r}
				tr.Status, _ = thread.ReadStatus(p, r.ID)
				tr.Report, _ = thread.ReadReport(p, r.ID)
				tr.Checks, tr.PR = prs[r.ID].Checks, prs[r.ID].Number
				if b != nil && r.TaskID() > 0 {
					tr.TaskRec = b.Find(r.TaskID())
				}
				pd.Threads = append(pd.Threads, tr)
			}
			items, _ := p.Inbox()
			pd.Unread, pd.Items = len(items), items
			qs, _ := p.Questions()
			pd.Questions = len(qs)
			pd.Badges = badgesOf(badgers, sum.Slug)
			pd.PRHostFails = hostFails(fails, p)
			if cfg != nil {
				pd.Links = link.Rows(cfg, p)
				for _, r := range pd.Links {
					pd.LinkWaits += r.Held
					if r.Paused {
						pd.LinkWaits++
					}
				}
			}
		}
		d.Projects = append(d.Projects, pd)
	}
	return d
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

// hostFails is project p's PRHostFails, of the ticker's fails by slug
// (ticker.HostFails): none without a PR host, as the coordinator's
// panel has it (server.NoPRHost).
func hostFails(fails map[string]int, p *project.Project) int {
	if n := fails[p.Slug]; n > 0 && !server.NoPRHost(p) {
		return n
	}
	return 0
}

// badgeMods are the mods whose annotations show: enabled, with the
// project_read grant that $.annotate needs. A mod disabled, regranted or
// removed shows no badges.
func badgeMods(mods []mod.Mod) []mod.Mod {
	var out []mod.Mod
	for _, m := range mods {
		if m.State == mod.Enabled && m.Granted(mod.PermProjectRead) {
			out = append(out, m)
		}
	}
	return out
}

// badgesOf reads the badges of mods for project slug, by target, in the
// mods' id order.
func badgesOf(mods []mod.Mod, slug string) map[string][]modannot.Badge {
	state, err := home.ModStateDir()
	if err != nil {
		return nil
	}
	var out map[string][]modannot.Badge
	for _, m := range mods {
		if !m.Runs(slug) {
			continue
		}
		for target, bs := range modannot.Read(filepath.Join(state, m.ID), m.ID, slug) {
			if out == nil {
				out = map[string][]modannot.Badge{}
			}
			out[target] = append(out[target], bs...)
		}
	}
	return out
}

func (s *ServerSource) Board(slug string) (*tasks.Board, error) {
	p, err := project.Open(slug)
	if err != nil {
		return nil, err
	}
	return p.Tasks().Load()
}

func (s *ServerSource) Library(slug string) ([]thread.LibFile, error) {
	p, err := project.Open(slug)
	if err != nil {
		return nil, err
	}
	return thread.Library(p)
}

func (s *ServerSource) LibraryRead(slug, id, name string, max int64) ([]byte, bool, error) {
	p, err := project.Open(slug)
	if err != nil {
		return nil, false, err
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
	return thread.RemoveLibrary(p, s.Caller, id, name)
}

func (s *ServerSource) NewProject(slug string) (string, error) {
	if s.Caller.IsAgent() {
		return "", errors.New("human-only: only the human creates projects")
	}
	p, err := project.New(project.Options{Slug: slug})
	if err != nil {
		return "", err
	}
	return p.Slug, nil
}

// coordinatorKickoff is the coordinator's first prompt. Its context (the
// role rules and tm context) arrives through the agent's session-start
// hook (docs/SPEC.md §7.8); this only asks it to speak first.
const coordinatorKickoff = "You are this project's coordinator. Greet the user: say in a few lines where the project stands, from your context, then ask what to do next."

func (s *ServerSource) OpenProject(slug string, cols, rows int) (string, error) {
	return OpenCoordinator(s.call, slug, "", cols, rows)
}

// errHumanOnly refuses a settings change from inside an agent.
var errHumanOnly = errors.New("human-only: settings are changed by the human")

// PeerList is the paired servers, for the settings' Peers tab
// (peerstab.go): this server's id ("" before it has an identity key)
// and each paired server with its link as the server has it.
type PeerList struct {
	ID, Short string
	Peers     []PeerEntry
	// Running: a server answered; LinkErr is why it can't read the
	// identity key or the peers.
	Running bool
	LinkErr string
}

// PeerEntry is one paired server: the alias the user knows it by, its
// server id and relay, and its link (nil while there is none).
type PeerEntry struct {
	Alias, ID, Short, Relay string
	Link                    *proto.PeerLink
	// Panes is this server's pane switch for it (proto.PanesOff …).
	Panes string
}

func (s *ServerSource) Peers() (PeerList, error) {
	var l PeerList
	if s.Caller.IsAgent() {
		return l, errHumanOnly
	}
	dir := peer.DirIn(s.Paths.Home)
	id, err := peer.LoadIdentity(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return l, err
	}
	if id != nil {
		l.ID, l.Short = id.ID.String(), id.ID.Short()
	}
	peers, err := peer.LoadPeers(dir)
	if err != nil {
		return l, err
	}
	links := map[string]proto.PeerLink{}
	var st proto.ServerStatus
	if s.call(proto.MethodServerStatus, nil, &st) == nil {
		l.Running = true
		if st.Link != nil {
			l.LinkErr = st.Link.Error
			for _, pl := range st.Link.Peers {
				links[pl.ID] = pl
			}
		}
	}
	for _, p := range peers {
		e := PeerEntry{Alias: p.Alias, ID: p.ID().String(), Short: p.ID().Short(), Relay: p.Relay, Panes: p.PanesLevel()}
		if pl, ok := links[e.ID]; ok {
			e.Link = &pl
		}
		l.Peers = append(l.Peers, e)
	}
	return l, nil
}

func (s *ServerSource) RemovePeer(alias string) error {
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	found, err := peer.Unpin(peer.DirIn(s.Paths.Home), alias)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no paired server %s (tm peer list)", alias)
	}
	var st *proto.LinkStatus
	if err := s.call(proto.MethodPeerReload, nil, &st); err != nil && !errors.Is(err, server.ErrNotRunning) {
		return fmt.Errorf("removed %s, but the server didn't read the peers again (%v); tm server restart does", alias, err)
	}
	return nil
}

func (s *ServerSource) SetPeerPanes(alias, level string) error {
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	found, err := peer.SetPanes(peer.DirIn(s.Paths.Home), alias, level)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no paired server %s (tm peer list)", alias)
	}
	var st *proto.LinkStatus
	if err := s.call(proto.MethodPeerReload, nil, &st); err != nil && !errors.Is(err, server.ErrNotRunning) {
		return fmt.Errorf("set, but the server didn't read the peers again (%v); tm server restart does", err)
	}
	return nil
}

func (s *ServerSource) SetLink(slug, other, allow string) error {
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	return link.Set(s.Caller, slug, other, allow, nil)
}

func (s *ServerSource) SetLinkAsk(slug, other string, ask bool) error {
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	return link.SetAsk(s.Caller, slug, other, ask)
}

func (s *ServerSource) LinkLog(slug, other string) ([]*link.Message, error) {
	p, err := project.Open(slug)
	if err != nil {
		return nil, err
	}
	return link.Log(p, other)
}

func (s *ServerSource) LinkAct(slug, verb, arg, text string) (string, error) {
	if s.Caller.IsAgent() {
		return "", errHumanOnly
	}
	p, err := project.Open(slug)
	if err != nil {
		return "", err
	}
	switch verb {
	case "approve":
		m, err := link.Approve(s.Caller, p, arg, false)
		if err != nil {
			return "", err
		}
		return "sent " + m.ID + " to " + m.Peer, nil
	case "drop":
		m, err := link.Drop(s.Caller, p, arg, false)
		if err != nil {
			return "", err
		}
		return "dropped " + m.ID, nil
	case "resume":
		return "resumed the link with " + arg, link.Resume(s.Caller, p, arg, false)
	case "send":
		m, err := link.Send(s.Caller, p, arg, link.Opts{Text: text})
		if err != nil {
			return "", err
		}
		return "sent " + m.ID + " to " + m.Peer, nil
	}
	return "", fmt.Errorf("unknown link action %q", verb)
}

func (s *ServerSource) SetSetting(table, key string, value any) error {
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	if table == config.DefaultsTable {
		var err error
		if value == nil {
			err = config.UnsetDefaults(key)
		} else {
			err = config.SetDefaults(key, value)
		}
		if err != nil {
			return err
		}
		journalAll(s.Caller, key, value, "all projects")
		return nil
	}
	slug, isProject := strings.CutPrefix(table, "projects.")
	if !isProject {
		if value == nil {
			return config.Unset(table, key)
		}
		return config.Set(table, key, value)
	}
	detail := fmt.Sprint(value)
	if value == nil {
		if err := config.UnsetProject(slug, key); err != nil {
			return err
		}
		detail = "(follows all projects)"
	} else if err := config.SetProject(slug, key, value); err != nil {
		return err
	}
	// The project's journal records the change, as every tm action.
	if p, err := project.Open(slug); err == nil {
		p.Journal(s.Caller, "settings."+key, slug, detail)
	}
	return nil
}

// ApplyModDefaults applies the defaults enabled mod id suggests for all
// projects (mods design §7, Tighten only), on the human's word: each in
// [defaults], journaled as the human's change, except a key config.toml
// already sets there (the human's stays) or one already at the value.
// It returns the suggestions applied.
func ApplyModDefaults(c caller.Caller, id string) ([]mod.Suggestion, error) {
	if c.IsAgent() {
		return nil, errHumanOnly
	}
	cfg, err := config.Load()
	if err != nil && cfg == nil {
		return nil, err
	}
	r, err := mod.Load(cfg)
	if err != nil {
		return nil, err
	}
	md, ok := r.Get(id)
	switch {
	case !ok:
		return nil, fmt.Errorf("no mod %q is installed", id)
	case md.State != mod.Enabled || md.Manifest == nil:
		return nil, fmt.Errorf("%s is off: its defaults apply once it is enabled", id)
	}
	var done []mod.Suggestion
	for _, sg := range md.Manifest.Suggestions(cfg) {
		if !sg.Applies() {
			continue
		}
		if err := config.SetDefaults(sg.Key, sg.Value); err != nil {
			return done, fmt.Errorf("%s: %w", sg.Key, err)
		}
		journalAll(c, sg.Key, sg.Value, "all projects, suggested by mod:"+id)
		done = append(done, sg)
	}
	return done, nil
}

func (s *ServerSource) ApplyModDefaults(id string) ([]mod.Suggestion, error) {
	return ApplyModDefaults(s.Caller, id)
}

// journalAll records a change of an all-projects setting in the journal
// of each project that follows it, i.e. doesn't set key itself; why
// follows the value in brackets.
func journalAll(c caller.Caller, key string, value any, why string) {
	cfg, err := config.Load()
	if err != nil {
		return
	}
	if key == "auto_resolve" {
		key = "auto_close"
	}
	list, _ := project.List()
	for _, sum := range list {
		if slices.Contains(cfg.Own(sum.Slug), key) {
			continue
		}
		if p, err := project.Open(sum.Slug); err == nil {
			p.Journal(c, "settings."+key, sum.Slug, fmt.Sprint(value)+" ("+why+")")
		}
	}
}

func (s *ServerSource) Lifecycle(slug, verb string) (string, error) {
	switch verb {
	case "activate", "deactivate":
		return ActivateProject(s.call, s.Caller, slug, verb == "activate")
	case "pause", "resume":
		return PauseProject(s.Caller, slug, verb == "pause")
	case "pin", "unpin":
		return PinProject(s.Caller, slug, verb == "pin")
	case "archive":
		return ArchiveProject(s.call, s.Caller, slug, true)
	case "delete":
		return DeleteProject(s.call, s.Caller, slug)
	}
	return "", fmt.Errorf("unknown project action %q", verb)
}

func (s *ServerSource) Templates(slug string) []mod.Template {
	cfg, err := config.Load()
	if err != nil {
		cfg = nil
	}
	reg, err := mod.Load(cfg)
	if err != nil {
		return nil
	}
	ts, _ := reg.Templates(slug) // tm doctor and the Mods tab say which don't load
	return ts
}

func (s *ServerSource) AddTask(slug, title string, tpl *mod.Template) (string, error) {
	if s.Caller.IsAgent() {
		return "", errHumanOnly
	}
	p, err := project.Open(slug)
	if err != nil {
		return "", err
	}
	it := tasks.NewTask{Title: title}
	if tpl != nil {
		it.Title, it.Notes, it.Steps = tpl.Apply(title)
	}
	res, err := p.Tasks().Add(s.Caller, []tasks.NewTask{it})
	switch {
	case err != nil:
		return "", err
	case len(res) != 1:
		return "", errors.New("the task wasn't added")
	case res[0].Result == "failed":
		return "", errors.New(res[0].Error)
	case res[0].Result == "existing":
		return res[0].ID, fmt.Errorf("%s %s is there already", res[0].ID, oneLine(res[0].Title))
	}
	return res[0].ID, nil
}

func (s *ServerSource) SetRepo(slug, path string, add bool) error {
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	p, err := project.Open(slug)
	if err != nil {
		return err
	}
	changed, err := p.SetRepo(path, add)
	if err != nil || !changed {
		return err
	}
	verb := map[bool]string{true: "add", false: "remove"}[add]
	return p.Journal(s.Caller, "project.repo."+verb, slug, path)
}

func (s *ServerSource) Ask(slug string, id int, kind, note string) (bool, error) {
	if s.Caller.IsAgent() {
		return false, errors.New("human-only: only the human asks the coordinator about a task")
	}
	p, err := project.Open(slug)
	if err != nil {
		return false, err
	}
	t, err := p.Tasks().Get(id)
	if err != nil {
		return false, err
	}
	if why := notAskable(t, kind); why != "" {
		return false, errors.New(why)
	}
	switch kind {
	case project.KindAccept:
		return p.AskAccept(s.Caller, t.Ref())
	case project.KindSendBack:
		return p.AskSendBack(s.Caller, t.Ref(), note)
	}
	return p.AskDelegate(s.Caller, t.Ref())
}

func (s *ServerSource) ModActionDone(mod, action string, target map[string]string, ok bool, token string) error {
	return s.call(proto.MethodModActionDone, proto.ModActionDoneParams{ModActionParams: proto.ModActionParams{Mod: mod, Action: action, Target: target}, OK: ok, Token: token}, nil)
}

func (s *ServerSource) ModActionCheck(mod, action string, target map[string]string) (string, error) {
	var res proto.ModActionCheckResult
	err := s.call(proto.MethodModActionCheck, proto.ModActionParams{Mod: mod, Action: action, Target: target}, &res)
	return res.Token, err
}

func (s *ServerSource) ClearSession(id string) error {
	return s.call(proto.MethodSessionClear, proto.SessionIDParams{ID: id}, nil)
}

func (s *ServerSource) AnswerQuestion(id string, index int, question, option string) error {
	return s.call(proto.MethodSessionAnswer, proto.SessionAnswerParams{ID: id, Index: index, Answer: option, Question: question}, nil)
}

func (s *ServerSource) OpenQuestions(slug string) (proto.QuestionsOpenResult, error) {
	var res proto.QuestionsOpenResult
	err := s.call(proto.MethodQuestionsOpen, proto.QuestionsOpenParams{Project: slug}, &res)
	return res, err
}

func (s *ServerSource) AskAdopt(slug string, sess proto.SessionInfo) (bool, error) {
	if s.Caller.IsAgent() {
		return false, errors.New("human-only: only the human asks the coordinator to adopt a session")
	}
	p, err := project.Open(slug)
	if err != nil {
		return false, err
	}
	// The go-ahead the coordinator's tm thread adopt needs is the
	// server's, given only on the human socket; the inbox item, which an
	// agent could write too, only tells the coordinator.
	if err := s.call(proto.MethodSessionAdoptAsk, proto.SessionAdoptAskParams{ID: sess.ID, Project: slug}, nil); err != nil {
		return false, err
	}
	return p.AskAdopt(s.Caller, sess.ID, adoptWhere(sess, ""))
}

// WithdrawAdopt takes back the user's go-ahead to adopt session id into
// project slug: the server's, which the coordinator's tm thread adopt
// needs, and the inbox item that told it.
func (s *ServerSource) WithdrawAdopt(slug, id string) error {
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	p, err := project.Open(slug)
	if err != nil {
		return err
	}
	if err := s.call(proto.MethodSessionAdoptAsk, proto.SessionAdoptAskParams{ID: id, Project: slug, Withdraw: true}, nil); err != nil {
		return err
	}
	if err := p.DoneItems(project.KindAdopt, id); err != nil {
		return err
	}
	return p.Journal(s.Caller, "thread.adopt.withdraw", id, "")
}

// adoptWhere says what runs in session s and where, for an adopt ask:
// "claude in /src/app on fix-login"; home, when set, is shortened to ~.
func adoptWhere(s proto.SessionInfo, home string) string {
	out := s.Agent + " in " + shortHome(s.Cwd, home)
	if pl, ok := worktree.Locate(s.Cwd); ok && pl.Branch != "" {
		out += " on " + pl.Branch
	}
	return out
}

func (s *ServerSource) Review(slug string, t *tasks.Task) Review {
	var rv Review
	p, err := project.Open(slug)
	if err != nil {
		return rv
	}
	var rec *thread.Record
	if thread.ValidID(t.Thread) {
		rec, _ = thread.Load(p, t.Thread)
	}
	reportPR, repo := "", ""
	if rec != nil {
		repo = rec.Repo
		if rep, _ := thread.ReadReport(p, rec.ID); rep != nil {
			rv.Check, reportPR = rep.Check, rep.PR
		}
	}
	if repo == "" && len(p.Repos) > 0 {
		repo = p.Repos[0]
	}
	pr := ticker.PRs(ticker.StatePath(s.Paths.Sessions), slug)[t.Thread]
	rv.PR = pr.Number
	if rv.PR == 0 {
		rv.PR = ticker.PRNumber(reportPR)
	}
	rv.Ship = shipped(repo, rv.PR, pr)
	return rv
}

// shipped is where pull request n stands (Review.Ship), from repo's
// history as last fetched, else the ticker's last look at it (pr).
func shipped(repo string, n int, pr ticker.PR) string {
	switch {
	case n == 0:
		return ""
	case pr.State == "MERGED" || pr.Merge != "" || repo != "" && worktree.MergeCommit(repo, n) != "":
		return ShipMerged
	case pr.State == "OPEN":
		return ShipOpen
	case pr.State == "CLOSED":
		return ShipClosed
	}
	return ""
}

func (s *ServerSource) SetRemote(slug string, on bool) (string, error) {
	res, err := SetRemote(s.call, slug, on)
	if err != nil {
		return "", err
	}
	return RemoteMessage(slug, res), nil
}

func (s *ServerSource) Agents() []string {
	reg, _ := agent.Load(s.Paths.AgentsDir())
	return models.Installed(reg, nil)
}

func (s *ServerSource) ModsNote() string {
	reg, _ := agent.Load(s.Paths.AgentsDir())
	return agent.ModNote(reg)
}

func (s *ServerSource) ModAct(id, verb string) error {
	switch verb {
	case "enable":
		return mod.Enable(s.Caller, id, "")
	case "disable":
		return mod.Disable(s.Caller, id, "")
	case "remove":
		mod.SweepRemoved(time.Now())
		return mod.Remove(s.Caller, id, false)
	}
	return fmt.Errorf("no mod action %q", verb)
}

func (s *ServerSource) SetModGrants(id string, grants []string) error {
	return mod.SetGrants(s.Caller, id, grants)
}

func (s *ServerSource) SetModSetting(id, key string, value any) error {
	return mod.SetSetting(s.Caller, id, key, value)
}

func (s *ServerSource) FetchModUpdate(id string) (*mod.Staged, error) {
	if s.Caller.IsAgent() {
		return nil, mod.ErrHumanOnly
	}
	m, err := installedMod(id)
	if err != nil {
		return nil, err
	}
	return mod.FetchUpdate(m)
}

func (s *ServerSource) ReplaceMod(id string, st *mod.Staged, grant []string) error {
	return mod.Replace(s.Caller, id, st, grant)
}

func (s *ServerSource) FetchMod(arg string) (*mod.Staged, error) {
	if s.Caller.IsAgent() {
		return nil, mod.ErrHumanOnly
	}
	mod.SweepRemoved(time.Now())
	src, err := mod.ParseSource(homePath(arg), "")
	if err != nil {
		return nil, err
	}
	st, err := mod.Fetch(src)
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("%s: %s", src, strings.ReplaceAll(mod.Clean(err.Error(), true), "\n", "; "))
	}
	cfg, err := config.Load()
	if err != nil {
		cfg = nil
	}
	reg, err := mod.Load(cfg)
	if err == nil {
		if why := mod.Taken(reg, st.Manifest.ID); why != "" {
			err = errors.New(why)
		}
	}
	if err != nil {
		st.Close()
		return nil, err
	}
	return st, nil
}

func (s *ServerSource) InstallMod(st *mod.Staged) error {
	return mod.Install(s.Caller, st)
}

// homePath expands a leading ~/ in a folder typed into the console,
// as a shell would have for tm mod install.
func homePath(arg string) string {
	if rest, ok := strings.CutPrefix(arg, "~/"); ok {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, rest)
		}
	}
	return arg
}

// installedMod is mod id as the registry has it now.
func installedMod(id string) (mod.Mod, error) {
	cfg, err := config.Load()
	if err != nil {
		cfg = nil
	}
	reg, err := mod.Load(cfg)
	if err != nil {
		return mod.Mod{}, err
	}
	m, ok := reg.Get(id)
	if !ok {
		return mod.Mod{}, fmt.Errorf("no mod %q is installed", id)
	}
	return m, nil
}

func (s *ServerSource) ModLog(id string, max int64) (string, bool, error) {
	if mod.CheckID(id) != nil {
		return "", false, fmt.Errorf("no mod %q", id)
	}
	dir, err := home.ModLogsDir()
	if err != nil {
		return "", false, err
	}
	return tail(filepath.Join(dir, id+".log"), max)
}

func (s *ServerSource) ModViews(shown []modview.Shown) (proto.ModViewsResult, error) {
	var res proto.ModViewsResult
	err := s.call(proto.MethodModViews, proto.ModViewsParams{Shown: shown}, &res)
	return res, err
}

func (s *ServerSource) ModViewInput(p proto.ModViewInputParams) error {
	return s.call(proto.MethodModViewInput, p, nil)
}

func (s *ServerSource) ModCommand(p proto.ModCommandParams) error {
	return s.call(proto.MethodModCommand, p, nil)
}

func (s *ServerSource) RestartMod(id string) error {
	if s.Caller.IsAgent() {
		return mod.ErrHumanOnly
	}
	return s.call(proto.MethodModRestart, proto.ModRestartParams{ID: id}, nil)
}

// tail reads the last max bytes of file path, from its first whole
// line; a missing file is "".
func tail(path string, max int64) (string, bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", false, err
	}
	cut := fi.Size() > max
	if cut {
		if _, err := f.Seek(fi.Size()-max, io.SeekStart); err != nil {
			return "", false, err
		}
	}
	data, err := io.ReadAll(io.LimitReader(f, max))
	if err != nil {
		return "", false, err
	}
	if cut {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}
	return string(data), cut, nil
}

func (s *ServerSource) Catalogs() []Catalog {
	reg, _ := agent.Load(s.Paths.AgentsDir())
	if reg == nil {
		return nil
	}
	cfg, _ := config.Load()
	return models.Catalogs(reg, cfg, models.Installed(reg, nil))
}

func (s *ServerSource) SetModels(agentName string, set config.AgentSettings) error {
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	return config.SetAgentModels(agentName, set)
}

func (s *ServerSource) SyncModel(session string) error {
	var res proto.SessionModelResult
	return s.call(proto.MethodSessionModel, proto.SessionModelParams{ID: session}, &res)
}

func (s *ServerSource) RefreshModels(agentName string) error {
	reg, _ := agent.Load(s.Paths.AgentsDir())
	if reg == nil {
		return errors.New("the agent manifests don't load")
	}
	a, ok := reg.Get(agentName)
	m := agent.ManifestOf(a)
	if !ok || m == nil {
		return fmt.Errorf("no agent %s", agentName)
	}
	_, err := models.Refresh(context.Background(), m, nil)
	return err
}

func (s *ServerSource) Unrefuse(agentName, model string) error {
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	return models.Unrefuse(agentName, model)
}

// Catalog is one installed agent's models (models.Catalog).
type Catalog = models.Catalog

// catalogNames lists the models the catalogs offer, each name once: the
// choices of the Thread models setting.
func catalogNames(cats []Catalog) []string {
	var out []string
	for _, c := range cats {
		for _, n := range c.Names() {
			if !slices.Contains(out, n) {
				out = append(out, n)
			}
		}
	}
	return out
}

// inactive says whether the user deactivated the project (§5.1).
func (p ProjectData) inactive() bool { return p.Safety != nil && !p.Safety.Active }

// pinned says the user pinned the project to the top of the sidebar.
func (p ProjectData) pinned() bool { return p.Safety != nil && p.Safety.Pinned }

// InactiveError is opening an inactive project's coordinator: opening
// never activates (docs/SPEC.md §5.1).
func InactiveError(slug string) error {
	return &project.Error{Code: "project-inactive", Msg: slug + " is inactive: activate it first (space on it in the sidebar, or tm project activate " + slug + ")"}
}

// OpenCoordinator returns the id of the project's coordinator session,
// starting one (agent, role coordinator, cwd the project folder) if none
// runs: `tm project open` and the dashboard's project rows. An empty
// agentName is the project's coordinator_agent setting (§11.2).
func OpenCoordinator(call func(method string, params, result any) error, slug, agentName string, cols, rows int) (string, error) {
	p, err := project.Open(slug)
	if err != nil {
		return "", err
	}
	var res proto.SessionListResult
	if err := call(proto.MethodSessionList, nil, &res); err != nil {
		return "", err
	}
	for _, s := range res.Sessions {
		if s.Role == proto.RoleCoordinator && s.Project == slug {
			return s.ID, nil
		}
	}
	// The project's agent, model and remote control settings; a broken
	// config.toml shows in the settings popup, it doesn't keep the
	// coordinator from starting.
	safety := config.Defaults
	cfg, err := config.Load()
	if err == nil {
		safety, _ = cfg.Safety(slug)
	}
	if !safety.Active {
		return "", InactiveError(slug)
	}
	name, model, err := coordinatorAgent(cfg, agentName, safety)
	if err != nil {
		return "", err
	}
	var started proto.SessionStartResult
	err = call(proto.MethodSessionStart, proto.SessionStartParams{
		Agent: name, Model: model, Role: proto.RoleCoordinator, Project: slug, Cwd: p.Dir,
		Cols: uint16(cols), Rows: uint16(rows), Kickoff: coordinatorKickoff,
		RemoteControl: safety.CoordinatorRemoteControl}, &started)
	if err != nil {
		return "", err
	}
	return started.Session.ID, nil
}

// coordinatorAgent is the agent and model a new coordinator runs: the
// agent named (tm project open --agent), else the project's
// coordinator_agent, else the one installed agent (docs/SPEC.md §8.2,
// §11.2); and the project's coordinator_model while that agent offers
// it in the project's scope, else none (the agent's own default, or the
// user's default_model, which the server passes). An agent that isn't
// installed, none, or several with none chosen are refused
// (models.Error: the dashboard asks for CodeNotChosen).
func coordinatorAgent(cfg *config.Config, flag string, safety config.Safety) (name, model string, err error) {
	reg := models.Registry()
	if reg == nil {
		return "", "", errors.New("no home directory for the agent manifests")
	}
	installed := models.Installed(reg, nil)
	setting := cmp.Or(flag, safety.CoordinatorAgent)
	name, _, err = models.Resolve(reg, installed, setting, "coordinator agent")
	if err != nil {
		return "", "", err
	}
	if safety.CoordinatorModel != "" {
		// An agent never asked (the server just started) is asked first.
		models.Ensure(context.Background(), reg, name, nil)
		if c, ok := models.CatalogOf(reg, cfg, name, installed); ok {
			model = c.CoordinatorModel(safety)
		}
	}
	return name, model, nil
}

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
