package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/guardlog"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/models"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/version"
	"github.com/theclifmeister/terminatr/internal/view"
)

// statusLine is statusBar's line, without its buttons.
func statusLine(s proto.SessionInfo, ts *thread.Status, pending bool, cols int, where string) string {
	line, _ := statusBar(s, ts, "", pending, cols, where, nil)
	return line
}

// fakeSource records the dashboard's actions. Settings go to the real
// settings file under the test's TERMINATR_HOME, and Load reads them
// back once one was set.
type fakeSource struct {
	agentCalls int // Agents() calls (perf_render_test.go)
	data       Data
	board      *tasks.Board
	opened     []string
	started    []string
	settings   []string // table.key=value
	repos      []string // +path or -path
	// delegated are the tasks asked to be delegated: "slug T12"; asked
	// the other asks: "accept slug T12", "send-back slug T12 note".
	delegated   []string
	asked       []string
	adopted     []string // "slug s-3"
	withdrawn   []string // "slug s-3"
	answered    []string // slugs whose questions were opened
	saveCleared []string // slugs whose coordinators were saved and cleared
	reviews     map[int]Review
	designs     []project.DesignBuild // the Designs tab's (designs_test.go)
	peers       PeerList
	// devices and devErr are Devices' answer; devActs the changes to
	// them ("level <id> answer", "name <id> New", "remove <id>"), and
	// pairDevice runs PairDevice (networkphones_test.go).
	devices    DeviceList
	devErr     error
	devActs    []string
	pairDevice func(ctx context.Context, relay string, ev DevicePairEvents) (DeviceEntry, error)
	// logs are the Logs tab's (logstab_test.go), read from their files;
	// logReads the log.read calls.
	logs     []proto.LogInfo
	logReads []proto.LogReadParams
	peerErr  error
	nameErr  error // SetPeerName's answer
	// selfSet are SetSelfName's calls.
	selfSet []string
	// popup and popupErr are PeerPopup's answer; popups its calls,
	// "alias/slug".
	popup        proto.PeerPopup
	popupErr     error
	popups       []string
	unpinned     []string                  // aliases unpaired in the Network popup
	panesSet     []string                  // "alias level" set in the Network popup
	convSet      []string                  // "alias on" set in the Network popup
	needsSet     []string                  // "alias level" set in the Network popup
	pagesSet     []string                  // "alias project on" set in the Network popup
	acts         []proto.PeerNeedActParams // paired servers' needs acted on
	actErr       error
	relaySet     []string // "alias relay" set in the Network popup
	namesSet     []string // "alias name" set in the Network popup
	relayUpdates []string // aliases whose relay u asked to update
	pairs        []string // PairPeer's and JoinPeer's calls: "pair relay", "join code"
	pairErr      error    // fails them before the code shows
	// shares are AskShare's and AnswerShare's calls, "ask alias for
	// note" and "accept id slug" / "decline id"; shareErr refuses them.
	shares       []string
	shareErr     error
	agents       []string
	remote       []string // "slug on" or "slug off"
	lifecycle    []string // "slug verb"
	library      []thread.LibFile
	libFiles     map[string]string // "t-0001/name" -> content
	libRemoved   []string          // "t-0001/name", "t-0001/" for all
	notesRemoved []string          // "demo/memory/a.md"
	done         []string          // action.done: "mod action project/task ok"
	// refusals are the guard's log's open refusals; decided its
	// decisions: "id how session".
	refusals []guardlog.Entry
	decided  []string
	// secrets are SetSecretEnv's calls ("table NAME=from", "table NAME
	// removed"), secretTests TestSecretEnv's ("slug NAME").
	secrets     []string
	secretTests []string
	// sandbox are EditSandbox's calls ("table what").
	sandbox []string
	// access is what AccessShow gives (its project and thread set to
	// the call's), accessShown its calls ("slug id").
	access      proto.AccessShowResult
	accessShown []string
	restarted   []string // mod ids
	fetchedMods []string // what FetchMod was asked for
	templates   []mod.Template
	added       []string // "slug title|notes|step;step"
	// links are SetLink's and SetLinkAsk's calls ("slug other both",
	// "slug other ask true"), linkActs
	// LinkAct's ("slug verb arg text"); linkLog is what LinkLog gives.
	links    []string
	linkActs []string
	linkLog  []*link.Message
	// linkLogs, when set, are LinkLog's by the other project instead;
	// linkFiles what LinkFiles gives.
	linkLogs  map[string][]*link.Message
	linkFiles []link.Transfer
	saved     string // what SaveLink says
	modViews  proto.ModViewsResult
	// tasksTab: ModViews lists the Tasks tab mod's view, drawn from the
	// board (taskstab_test.go), its done tasks listed with tasksDoneAll.
	tasksTab     bool
	tasksDoneAll bool
	shown        []modview.Shown // the views the last ModViews said were shown
	inputs       []proto.ModViewInputParams
	commands     []proto.ModCommandParams
	cleared      []string // session ids
	answers      []string // "s-2 0 Which colour? → Blue"
}

func (f *fakeSource) Templates(string) []mod.Template { return f.templates }

func (f *fakeSource) AddTask(slug, title string, tpl *mod.Template) (string, error) {
	notes, steps := "", []string(nil)
	if tpl != nil {
		title, notes, steps = tpl.Apply(title)
	}
	f.added = append(f.added, slug+" "+title+"|"+notes+"|"+strings.Join(steps, ";"))
	return fmt.Sprintf("T%d", 10+len(f.added)), nil
}

func (f *fakeSource) SetRemote(slug string, on bool) (string, error) {
	state := map[bool]string{true: "on", false: "off"}[on]
	f.remote = append(f.remote, slug+" "+state)
	return "remote control " + state + " for " + slug, nil
}

func (f *fakeSource) Lifecycle(slug, verb string) (string, error) {
	f.lifecycle = append(f.lifecycle, slug+" "+verb)
	if verb == "pause" || verb == "resume" {
		if err := config.SetProject(slug, "paused", verb == "pause"); err != nil {
			return "", err
		}
	}
	if verb == "pin" || verb == "unpin" {
		if err := config.SetProject(slug, "pinned", verb == "pin"); err != nil {
			return "", err
		}
	}
	return verb + " " + slug, nil
}

func (f *fakeSource) Load() Data {
	if len(f.settings) > 0 || len(f.lifecycle) > 0 {
		if cfg, err := config.Load(); err == nil {
			for i := range f.data.Projects {
				s, _ := cfg.Safety(f.data.Projects[i].Slug)
				f.data.Projects[i].Safety = &s
				f.data.Projects[i].Own = cfg.Own(f.data.Projects[i].Slug)
			}
			all, _ := cfg.AllProjects()
			f.data.Defaults = &all
		}
	}
	return f.data
}
func (f *fakeSource) Board(string) (*tasks.Board, error) {
	if f.board != nil {
		return f.board, nil
	}
	return &tasks.Board{}, nil
}
func (f *fakeSource) SetSetting(table, key string, value any) error {
	f.settings = append(f.settings, fmt.Sprintf("%s.%s=%v", table, key, value))
	if table == config.DefaultsTable {
		if value == nil {
			return config.UnsetDefaults(key)
		}
		return config.SetDefaults(key, value)
	}
	if slug, ok := strings.CutPrefix(table, "projects."); ok {
		if value == nil {
			return config.UnsetProject(slug, key)
		}
		return config.SetProject(slug, key, value)
	}
	if value == nil {
		return config.Unset(table, key)
	}
	return config.Set(table, key, value)
}
func (f *fakeSource) SetLink(slug, other, allow string) error {
	f.links = append(f.links, fmt.Sprintf("%s %s %s", slug, other, allow))
	return nil
}
func (f *fakeSource) SaveLink(slug, other string, l config.Link) (string, error) {
	f.links = append(f.links, fmt.Sprintf("%s %s save %s ask %v files %s", slug, other, l.Allow, l.Ask, l.FilesMode()))
	return f.saved, nil
}
func (f *fakeSource) SetLinkAsk(slug, other string, ask bool) error {
	f.links = append(f.links, fmt.Sprintf("%s %s ask %v", slug, other, ask))
	return nil
}
func (f *fakeSource) SetLinkFiles(slug, other, files string) error {
	f.links = append(f.links, fmt.Sprintf("%s %s files %s", slug, other, files))
	return nil
}
func (f *fakeSource) LinkFiles(slug, other string) ([]link.Transfer, error) { return f.linkFiles, nil }
func (f *fakeSource) LinkLog(slug, other string) ([]*link.Message, error) {
	if f.linkLogs != nil {
		return f.linkLogs[other], nil
	}
	return f.linkLog, nil
}
func (f *fakeSource) LinkAct(slug, verb, arg, text string) (string, error) {
	f.linkActs = append(f.linkActs, strings.TrimSpace(strings.Join([]string{slug, verb, arg, text}, " ")))
	return verb + " done", nil
}
func (f *fakeSource) SetRepo(slug, path string, add bool) error {
	sign := map[bool]string{true: "+", false: "-"}[add]
	f.repos = append(f.repos, sign+path)
	for i := range f.data.Projects {
		if p := &f.data.Projects[i]; p.Slug == slug {
			if add {
				p.Repos = append(p.Repos, path)
			} else {
				p.Repos = slices.DeleteFunc(p.Repos, func(r string) bool { return r == path })
			}
		}
	}
	return nil
}
func (f *fakeSource) ClearSession(id string) error {
	f.cleared = append(f.cleared, id)
	return nil
}
func (f *fakeSource) AnswerQuestion(id string, index int, question, option string) error {
	f.answers = append(f.answers, fmt.Sprintf("%s %d %s → %s", id, index, question, option))
	return nil
}
func (f *fakeSource) OpenQuestions(slug string) (proto.QuestionsOpenResult, error) {
	f.answered = append(f.answered, slug)
	return proto.QuestionsOpenResult{Session: "s-1", Via: "queued", Open: 2}, nil
}
func (f *fakeSource) SaveClear(slug string) (proto.CoordinatorClearResult, error) {
	f.saveCleared = append(f.saveCleared, slug)
	return proto.CoordinatorClearResult{Session: "s-1"}, nil
}
func (f *fakeSource) AskAdopt(slug string, s proto.SessionInfo) (bool, error) {
	f.adopted = append(f.adopted, slug+" "+s.ID)
	return true, nil
}
func (f *fakeSource) WithdrawAdopt(slug, id string) error {
	f.withdrawn = append(f.withdrawn, slug+" "+id)
	return nil
}
func (f *fakeSource) Ask(slug string, id int, kind, note string) (bool, error) {
	ref := fmt.Sprintf("T%d", id)
	what := slug + " " + ref
	if note != "" {
		what += " " + note
	}
	switch kind {
	case project.KindDelegate:
		f.delegated = append(f.delegated, what)
	default:
		f.asked = append(f.asked, kind+" "+what)
	}
	for i := range f.data.Projects {
		if p := &f.data.Projects[i]; p.Slug == slug {
			if project.TaskAsked(p.Items, ref) != "" {
				return false, nil
			}
			p.Items = append(p.Items, project.Item{ID: "x-" + kind + "-" + ref, Kind: kind, Subject: ref})
		}
	}
	return true, nil
}
func (f *fakeSource) Library(string) ([]thread.LibFile, error)      { return f.library, nil }
func (f *fakeSource) Designs(string) ([]project.DesignBuild, error) { return f.designs, nil }
func (f *fakeSource) LibraryRead(_, id, name string, max int64) ([]byte, bool, error) {
	return []byte(f.libFiles[id+"/"+name]), false, nil
}
func (f *fakeSource) LinkFileRead(_, _, id, name string, max int64) ([]byte, bool, error) {
	data, ok := f.libFiles[id+"/"+name]
	if !ok {
		return nil, false, errors.New(id + " has no file " + name)
	}
	if int64(len(data)) > max {
		return []byte(data[:max]), true, nil
	}
	return []byte(data), false, nil
}
func (f *fakeSource) HasNote(slug, path string) bool {
	return path == "memory/a.md" && !slices.Contains(f.notesRemoved, slug+"/"+path)
}

func (f *fakeSource) RemoveNote(slug, path string) error {
	f.notesRemoved = append(f.notesRemoved, slug+"/"+path)
	return nil
}

func (f *fakeSource) LibraryRemove(_, id, name string) (int, error) {
	f.libRemoved = append(f.libRemoved, id+"/"+name)
	var keep []thread.LibFile
	n := 0
	for _, l := range f.library {
		if (l.Thread == id || l.Message == id) && (name == "" || l.Name == name) {
			n++
			continue
		}
		keep = append(keep, l)
	}
	f.library = keep
	return n, nil
}
func (f *fakeSource) Review(slug string, t *tasks.Task) Review { return f.reviews[t.ID] }
func (f *fakeSource) Agents() []string {
	f.agentCalls++
	if f.agents == nil {
		return []string{"claude"} // one agent installed
	}
	return f.agents
}
func (f *fakeSource) NewProject(name string) (string, error) { return name, nil }
func (f *fakeSource) OpenProject(slug string, c, r int) (string, error) {
	f.opened = append(f.opened, slug)
	return "s-" + slug, nil
}

func (f *fakeSource) Peers() (PeerList, error) { return f.peers, f.peerErr }
func (f *fakeSource) PeerPopup(alias, slug string) (proto.PeerPopup, error) {
	f.popups = append(f.popups, alias+"/"+slug)
	return f.popup, f.popupErr
}
func (f *fakeSource) SetPeerPanes(alias, level string) error {
	f.panesSet = append(f.panesSet, alias+" "+level)
	for i := range f.peers.Peers {
		if f.peers.Peers[i].Alias == alias {
			f.peers.Peers[i].Panes = level
		}
	}
	return nil
}
func (f *fakeSource) SetPeerConversations(alias string, on bool) error {
	f.convSet = append(f.convSet, fmt.Sprintf("%s %v", alias, on))
	for i := range f.peers.Peers {
		if f.peers.Peers[i].Alias == alias {
			f.peers.Peers[i].ConversationsOff = !on
		}
	}
	return nil
}
func (f *fakeSource) ActOnNeed(p proto.PeerNeedActParams) (proto.PeerActResult, error) {
	f.acts = append(f.acts, p)
	return proto.PeerActResult{Act: p.Act, Asked: true}, f.actErr
}
func (f *fakeSource) SetPeerNeeds(alias, level string) error {
	f.needsSet = append(f.needsSet, alias+" "+level)
	for i := range f.peers.Peers {
		if f.peers.Peers[i].Alias == alias {
			f.peers.Peers[i].Needs = level
		}
	}
	return nil
}
func (f *fakeSource) SetPeerPages(alias, project string, on bool) error {
	f.pagesSet = append(f.pagesSet, fmt.Sprintf("%s %s %v", alias, project, on))
	for i := range f.peers.Peers {
		if f.peers.Peers[i].Alias == alias {
			e := &f.peers.Peers[i]
			e.Pages = slices.DeleteFunc(e.Pages, func(s string) bool { return s == project })
			if on {
				e.Pages = append(e.Pages, project)
			}
		}
	}
	return nil
}
func (f *fakeSource) SetSelfName(name string) error {
	if f.nameErr != nil {
		return f.nameErr
	}
	f.selfSet = append(f.selfSet, name)
	return nil
}
func (f *fakeSource) SetPeerName(alias, name string) error {
	if f.nameErr != nil {
		return f.nameErr
	}
	f.namesSet = append(f.namesSet, alias+" "+name)
	for i := range f.peers.Peers {
		if f.peers.Peers[i].Alias == alias {
			f.peers.Peers[i].Name = name
		}
	}
	return nil
}
func (f *fakeSource) RelayUpdate(alias string) (string, error) {
	f.relayUpdates = append(f.relayUpdates, alias)
	return "the relay updates", nil
}
func (f *fakeSource) SetPeerRelay(alias, relay string) (string, error) {
	f.relaySet = append(f.relaySet, alias+" "+relay)
	return "", nil
}
func (f *fakeSource) PairPeer(ctx context.Context, relay string, ev PairEvents) (string, error) {
	return f.pairing("pair "+relay, "relay.example.net/7Q4M-K2XD-9HVA-PT3C", ev)
}
func (f *fakeSource) JoinPeer(ctx context.Context, code string, ev PairEvents) (string, error) {
	return f.pairing("join "+code, "", ev)
}

// pairing shows code (a pairing's), then studio's six digits, and pins
// studio on a yes.
func (f *fakeSource) pairing(call, code string, ev PairEvents) (string, error) {
	f.pairs = append(f.pairs, call)
	if f.pairErr != nil {
		return "", f.pairErr
	}
	if code != "" {
		ev.Waiting(code)
	}
	if !ev.Confirm(PairOffer{Name: "studio.local", Short: "77AB-91C3", Version: "v0.27.0", Check: "381 204"}) {
		return "", errors.New("not paired: the digits weren't confirmed here")
	}
	f.peers.Peers = append(f.peers.Peers, PeerEntry{Alias: "studio", ID: "s", Short: "77AB-91C3", Relay: "wss://relay.example.net"})
	return "studio", nil
}

func (f *fakeSource) AskShare(alias, forSlug, note string) (proto.ShareRequest, error) {
	f.shares = append(f.shares, "ask "+alias+" "+forSlug+" "+note)
	if f.shareErr != nil {
		return proto.ShareRequest{}, f.shareErr
	}
	r := proto.ShareRequest{ID: fmt.Sprintf("S-4F2A-%d", 30+len(f.shares)), Out: true, Peer: alias, For: forSlug, Note: note, State: proto.ShareWaiting}
	for i := range f.data.Peers {
		if f.data.Peers[i].Alias == alias {
			f.data.Peers[i].Shares = append(f.data.Peers[i].Shares, r)
		}
	}
	return r, nil
}
func (f *fakeSource) AnswerShare(id string, accept bool, slug string) (proto.ShareRequest, error) {
	call := "decline " + id
	if accept {
		call = "accept " + id + " " + slug
	}
	f.shares = append(f.shares, call)
	if f.shareErr != nil {
		return proto.ShareRequest{}, f.shareErr
	}
	var r proto.ShareRequest
	for i := range f.data.Peers {
		for j := range f.data.Peers[i].Shares {
			if q := &f.data.Peers[i].Shares[j]; q.ID == id && !q.Out {
				q.State = proto.ShareIsDeclined
				if accept {
					q.State, q.Project = proto.ShareIsAccepted, slug
				}
				r = *q
			}
		}
	}
	if accept {
		for i := range f.data.Projects {
			if p := &f.data.Projects[i]; p.Slug == slug {
				sf := config.Defaults
				if p.Safety != nil {
					sf = *p.Safety
				}
				sf.Share = true
				p.Safety = &sf
			}
		}
	}
	return r, nil
}

func (f *fakeSource) RemovePeer(alias string) error {
	f.unpinned = append(f.unpinned, alias)
	f.peers.Peers = slices.DeleteFunc(f.peers.Peers, func(e PeerEntry) bool { return e.Alias == alias })
	return nil
}

// Catalogs are the installed (Agents) built-in agents' models as the
// test saved their answers (modelstest), with its config.toml over them.
func (f *fakeSource) Catalogs() []Catalog {
	reg, _ := agent.Load("")
	cfg, _ := config.Load()
	return models.Catalogs(reg, cfg, f.Agents())
}
func (f *fakeSource) RefreshModels(name string) error {
	f.settings = append(f.settings, "refresh "+name)
	return nil
}
func (f *fakeSource) Unrefuse(name, model string) error { return models.Unrefuse(name, model) }
func (f *fakeSource) GuardRefusals(slug string) ([]guardlog.Entry, error) {
	var out []guardlog.Entry
	for _, e := range f.refusals {
		if slug == "" || e.Project == slug {
			out = append(out, e)
		}
	}
	return out, nil
}
func (f *fakeSource) GuardDecide(e guardlog.Entry, how, session string) (string, error) {
	f.decided = append(f.decided, e.ID+" "+how+" "+session)
	return "decided " + how, nil
}
func (f *fakeSource) ResetAccess(table string) ([]string, error) {
	f.settings = append(f.settings, table+" reset")
	return config.ResetAccess(table)
}
func (f *fakeSource) SetSecretEnv(table, name string, src *config.EnvSource) error {
	if src == nil {
		f.secrets = append(f.secrets, table+" "+name+" removed")
		return config.UnsetEnv(table, name)
	}
	f.secrets = append(f.secrets, table+" "+name+"="+src.From)
	return config.SetEnv(table, name, *src)
}
func (f *fakeSource) EditSandbox(table, what string, fn func(*config.SandboxTable) error) error {
	f.sandbox = append(f.sandbox, table+" "+what)
	return config.EditSandbox(table, fn)
}
func (f *fakeSource) EditSandboxRule(table, what string, e config.SandboxEdit) error {
	f.sandbox = append(f.sandbox, table+" "+what)
	_, err := config.EditSandboxRule(table, e)
	return err
}
func (f *fakeSource) AccessShow(slug, id string) (proto.AccessShowResult, error) {
	f.accessShown = append(f.accessShown, slug+" "+id)
	res := f.access
	res.Project, res.Thread = slug, id
	return res, nil
}

func (f *fakeSource) TestSecretEnv(slug, name string) (string, error) {
	f.secretTests = append(f.secretTests, slug+" "+name)
	return secretTestWord(name, true, ""), nil
}
func (f *fakeSource) SyncModel(id string) error {
	f.settings = append(f.settings, "sync "+id)
	return nil
}
func (f *fakeSource) SetModels(name string, s config.AgentSettings) error {
	f.settings = append(f.settings, fmt.Sprintf("agents.%s=%v", name, s))
	return config.SetAgentModels(name, s)
}

func testData() Data {
	now := time.Now()
	return Data{ServerOK: true,
		Sessions: []proto.SessionInfo{
			{ID: "s-1", Role: proto.RoleCoordinator, Project: "alpha", Agent: "claude", State: "blocked", Reason: "question", Created: now},
			{ID: "s-2", Role: proto.RoleThread, Project: "alpha", Thread: "t-0002", Agent: "claude", State: "blocked", Reason: "permission", Created: now},
			{ID: "s-5", Role: proto.RoleThread, Project: "beta", Thread: "t-0005", Agent: "claude", State: "working", Created: now},
			{ID: "s-3", Role: proto.RoleShell, Argv: []string{"/bin/zsh", "-l"}, State: "", Cwd: "/x", Created: now},
			{ID: "s-4", Role: proto.RoleShell, Agent: "claude", State: "blocked", Reason: "permission", Cwd: "/y", Created: now},
		},
		Projects: []ProjectData{
			{Slug: "alpha", Counts: map[string]int{"needs_you": 1, "in_motion": 1}},
			{Slug: "beta", Counts: map[string]int{}, Threads: []ThreadRow{
				{Record: &thread.Record{ID: "t-0005", Title: "Write docs", Task: "T4", State: thread.Running, Session: "s-5", Reports: 1},
					Status: &thread.Status{Percent: 60, PercentSource: "steps", StepsDone: 3, StepsTotal: 5, Current: "Draft §2", NeedsYou: "Which licence?",
						Todos: []agent.Todo{{Text: "Outline", Status: agent.TodoCompleted}, {Text: "Draft §2", Status: agent.TodoInProgress}}},
					Report:  &thread.Report{PR: "https://github.com/o/r/pull/7", Next: []string{"Merge the PR", "Delete the branch"}},
					TaskRec: &tasks.Task{ID: 4, Title: "Docs", Steps: []tasks.Step{{N: 1, Text: "Plan", Done: true}, {N: 2, Text: "Write"}}}},
				{Record: &thread.Record{ID: "t-0006", Title: "Old work", State: thread.Stopped}},
			}},
		},
	}
}

// screen is the dashboard as text, without its colours or the sidebar;
// with a popup open the whole window, which the popup is centred on.
func screen(m *dash) string {
	lines := strings.Split(whole(m), "\n")
	if m.geo != nil {
		return strings.Join(lines, "\n")
	}
	for i, l := range lines {
		r := []rune(l)
		lines[i] = string(r[min(m.sideW(), len(r)):])
	}
	return strings.Join(lines, "\n")
}

// whole is the whole window as text: the sidebar and the dashboard.
func whole(m *dash) string { return ansi.Strip(m.render()) }

func press(m *dash, keys ...string) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		default:
			msg = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
		}
		_, cmd = m.Update(msg)
	}
	return cmd
}

// TestDashboardRows: the current project's NEEDS YOU, then its own
// section (its coordinator, threads and task counts; the sidebar's tree
// lists the projects), then SESSIONS. Another project's needs aren't
// listed: the PROJECTS header's all-projects view has them
// (TestDashboardAllProjects). A block's reason and a new report lead
// the rest of a row, so they are never cut off.
func TestDashboardRows(t *testing.T) {
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 30, State: DashState{Current: "beta"}})
	m.layout.Details = false // one column: the rows at full width
	m.setData(src.data)
	out := screen(m)
	needs := strings.Index(out, "NEEDS YOU")
	proj := strings.Index(out, " beta ─")
	sess := strings.Index(out, "SESSIONS")
	if needs < 0 || !(needs < proj && proj < sess) {
		t.Fatalf("sections out of order:\n%s", out)
	}
	// beta waits on nothing: alpha's blocked coordinator isn't beta's;
	// the user's own blocked session, outside the projects, is in every
	// project's NEEDS YOU.
	if strings.Contains(out, "s-5 ") || strings.Contains(out, "PROJECTS") || strings.Contains(out, " alpha ─") || strings.Contains(out[needs:proj], "alpha") {
		t.Errorf("another project's needs, a thread's session, a PROJECTS section or another project listed:\n%s", out)
	}
	for _, want := range []string{
		"NEEDS YOU 1 ─",
		"  s-4           claude                                    ▲ blocked   permission",
		"  coordinator                               —           not running; enter starts it",
		"  T4 Write docs                             ● working   report new  ▰▰▰▱▱  60% 3/5 ▸ Draft §2  PR #7  t-0005",
		"  t-0006 Old work                           · stopped",
		"  tasks: 0 need you · 0 in motion · 0 on deck",
		"  s-3           /bin/zsh -l                               ▷ running",
		"● server ok · 5 sessions",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}

	// alpha's NEEDS YOU: its blocked coordinator, unlabelled (the
	// section is alpha's), and nothing of its threads (a blocked one, a
	// report, a question), which are their coordinator's.
	m = newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 30, State: DashState{Current: "alpha"}})
	m.layout.Details = false
	m.setData(src.data)
	out = screen(m)
	needs, proj = strings.Index(out, "NEEDS YOU 2 ─"), strings.Index(out, " alpha ─")
	if needs < 0 || needs > proj {
		t.Fatalf("alpha's NEEDS YOU:\n%s", out)
	}
	needsYou := out[needs:proj]
	if !strings.Contains(needsYou, "  coordinator                               ▲ blocked   question") || !strings.Contains(needsYou, "  s-4  ") {
		t.Errorf("alpha's NEEDS YOU lacks its coordinator or the user's own session:\n%s", needsYou)
	}
	for _, not := range []string{"t-0002", "t-0005", "report", "Which licence?", "alpha "} {
		if strings.Contains(needsYou, not) {
			t.Errorf("NEEDS YOU shows %q:\n%s", not, needsYou)
		}
	}
	// The first selectable row is the blocked coordinator.
	if r, _ := m.selected(); r.session != "s-1" {
		t.Errorf("selected %+v", r)
	}
	// Without a current project, the first is listed.
	m = newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 30})
	m.setData(src.data)
	if out := screen(m); !strings.Contains(out, " alpha ─") || strings.Contains(out, "t-0005") {
		t.Errorf("no current project:\n%s", out)
	}
}

// TestDashboardAllProjects: the PROJECTS header's view lists every
// project's NEEDS YOU, each row labelled with its project, the user's
// own blocked sessions last, and no project section; the header says
// all projects and keys about a project want a row of one.
func TestDashboardAllProjects(t *testing.T) {
	d := testData()
	d.Projects[1].NeedsYou = []*tasks.Task{{ID: 9, Title: "Review the docs", Status: tasks.Review, Thread: "t-0005"}}
	src := &fakeSource{data: d}
	m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 30, State: DashState{Current: allProjects}})
	m.layout.Details = false
	m.setData(src.data)
	out := screen(m)
	for _, want := range []string{
		"tm dashboard · all projects",
		"NEEDS YOU 3 ─",
		"  alpha         coordinator                               ▲ blocked   question",
		"  beta          T9 Review the docs",
		"  s-4           claude                                    ▲ blocked   permission",
		"OTHER SESSIONS",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, " alpha ─") || strings.Contains(out, " beta ─") || strings.Contains(out, "tasks: ") {
		t.Errorf("a project section in the all-projects view:\n%s", out)
	}
	if a, b, s := strings.Index(out, "  alpha  "), strings.Index(out, "  beta  "), strings.Index(out, "  s-4  "); !(a < b && b < s) {
		t.Errorf("not in the projects' order, the user's sessions last:\n%s", out)
	}
	if r, _ := m.selected(); r.session != "s-1" {
		t.Errorf("selected %+v, want alpha's coordinator", r)
	}
	if m.sel = "n:s-4"; m.projectHere() != "" {
		t.Errorf("project here %q on a session outside the projects", m.projectHere())
	}

	// Nothing waits anywhere: the view says so.
	src.data.Projects[1].NeedsYou = nil
	src.data.Sessions = slices.DeleteFunc(slices.Clone(src.data.Sessions), func(s proto.SessionInfo) bool { return s.State == "blocked" })
	m.setData(src.data)
	if out := screen(m); !strings.Contains(out, "NEEDS YOU ─") || !strings.Contains(out, "nothing waits on you in any project") {
		t.Errorf("nothing needs you:\n%s", out)
	}
}

// TestDashboardAllProjectsPeers: the all-projects view lists a paired
// server's shared projects' needs too, from the mirror's watch, labelled
// "mini › acme-api"; enter shows its read-only dashboard.
func TestDashboardAllProjectsPeers(t *testing.T) {
	d := testData()
	d.Peers = []proto.PeerMirror{{Alias: "mini", State: "up", Watch: true, Projects: []proto.PeerMirrorProject{
		{PeerProject: proto.PeerProject{Project: "acme-api"}, Watch: &proto.ProjectWatch{Questions: 1,
			NeedsYou: []proto.WatchNeed{{Why: proto.WhyReview, Task: "T3", Title: "Rate limits", Thread: "t-0009"}}}},
	}}}
	src := &fakeSource{data: d}
	m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 30, State: DashState{Current: allProjects}})
	m.layout.Details = false
	m.setData(src.data)
	out := screen(m)
	for _, want := range []string{"mini › acme…", "1 question waiting", "on mini", "T3 Rate limits", "in review"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	m.sel = "nr:@mini/acme-api:0"
	if foot := m.footKeys(); !strings.Contains(foot, "enter show") || strings.Contains(foot, "t tasks") {
		t.Errorf("footer on a remote need %q", foot)
	}
	m.enter("")
	if m.current != "@mini/acme-api" {
		t.Errorf("enter on a remote need shows %q", m.current)
	}
	// A project of this server's lists no peer's needs.
	m.showProject("alpha")
	if out := screen(m); strings.Contains(out, "acme") {
		t.Errorf("alpha's dashboard lists a peer's needs:\n%s", out)
	}
}

// TestDashboardPeerNeedsInFull: a paired server whose needs switch for
// this one is view sends each need in full: a row each, with what it
// asks and "answer on mini", an escape's without what it would run;
// enter still shows its read-only dashboard.
func TestDashboardPeerNeedsInFull(t *testing.T) {
	d := testData()
	menu := &proto.Question{Questions: []proto.QuestionItem{{Question: "Which cache?", Options: []proto.QuestionOption{{Label: "Redis"}}}}}
	d.Peers = []proto.PeerMirror{{Alias: "mini", State: "up", Watch: true, NeedsCap: true, Needs: proto.NeedsView, Projects: []proto.PeerMirrorProject{
		{PeerProject: proto.PeerProject{Project: "acme-api"}, Watch: &proto.ProjectWatch{Questions: 1,
			NeedsYou: []proto.WatchNeed{{Why: proto.WhyReview, Task: "T3", Title: "Rate limits", Thread: "t-0009"}}},
			Needs: []proto.PeerNeed{
				{ID: "question:s-1:a", Kind: proto.NeedQuestion, Task: "T7", Session: "s-1", Question: menu},
				{ID: "approval:s-2:b", Kind: proto.NeedApproval, Task: "T8", Approval: &proto.PeerApproval{Question: "Allow curl example.com?", Options: []string{"Yes", "No"}}},
				{ID: "approval:s-3", Kind: proto.NeedApproval, Role: proto.RoleCoordinator, Escape: true},
				{ID: "ask:Q2", Kind: proto.NeedAsk, Role: proto.RoleCoordinator, Question: &proto.Question{Questions: []proto.QuestionItem{{Question: "Both apps?"}}}},
				{ID: "review:T3:1", Kind: proto.NeedReview, Task: "T3", Title: "Rate limits", Status: "review"},
				{ID: "question:s-4:m", Kind: proto.NeedQuestion, Task: "T9", Session: "s-4", Question: &proto.Question{Screen: true, Questions: []proto.QuestionItem{
					{Question: "Which camera?", Answered: true, Answer: "DSLR", Options: []proto.QuestionOption{{Label: "DSLR"}}},
					{Question: "Which subjects?", MultiSelect: true, Options: []proto.QuestionOption{{Label: "Plants"}, {Label: "Birds"}, {Label: "Hills"}}}}}},
			}},
	}}}
	src := &fakeSource{data: d}
	m := newDash(DashOptions{Source: src, Width: 160 + sideDefault, Height: 30, State: DashState{Current: allProjects}})
	m.layout.Details = false
	m.setData(src.data)
	out := screen(m)
	for _, want := range []string{"T7  asks: Which cache?", "T8  needs approval: Allow curl", "coordinator  asks to run a command",
		"only on mini", "coordinator  asks: Both apps?", "T3  Rate limits", "answer on mini"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "1 question waiting") {
		t.Errorf("the masked count shows beside the needs in full:\n%s", out)
	}
	m.sel = "nr:@mini/acme-api:question:s-1:a"
	m.enter("")
	if m.current != "@mini/acme-api" {
		t.Errorf("enter on a remote need shows %q", m.current)
	}
}

// TestDashboardDoneThread: a done thread with a new report says so in
// full, its blocked reason or report leading the row even in a narrow
// list.
func TestDashboardDoneThread(t *testing.T) {
	d := testData()
	th := &d.Projects[1].Threads[1]
	th.Done, th.Reports, th.State = true, 1, thread.Running
	d.Sessions = append(d.Sessions, proto.SessionInfo{ID: "s-6", Role: proto.RoleThread, Project: "beta", Thread: "t-0006", Agent: "claude", State: "idle"})
	th.Session = "s-6"
	src := &fakeSource{data: d}
	m := newDash(DashOptions{Source: src, Width: 82 + sideDefault, Height: 30, State: DashState{Current: "beta"}})
	m.setData(src.data)
	if out := screen(m); !strings.Contains(out, "t-0006 Old work                 ✓ done      report new") {
		t.Fatalf("done thread:\n%s", out)
	}
}

func TestDashboardKeys(t *testing.T) {
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Cwd: "/work", Width: 100, Height: 30, State: DashState{Current: "alpha"}})
	m.setData(src.data)

	// enter on the blocked coordinator attaches to it.
	drain(t, m, press(m, "enter"))
	if m.result.Attach != "s-1" {
		t.Fatalf("attach %q", m.result.Attach)
	}

	// The keys that acted on threads and tasks are gone: c, d, 1-9 do
	// nothing on a thread's row (on a coordinator's they type through
	// to it: typethrough_test.go).
	for _, sel := range []string{"th:beta:t-0005", "th:beta:t-0006"} {
		m.sel, m.result = sel, DashResult{}
		for _, k := range []string{"c", "d", "1", "2"} {
			if cmd := press(m, k); cmd != nil || m.top() != nil {
				t.Fatalf("%s on %s did something: overlay %T", k, sel, m.top())
			}
		}
	}
	if len(src.started)+len(src.opened) != 0 {
		t.Fatalf("started %v opened %v", src.started, src.opened)
	}

	// enter on a thread attaches its session. (Attaching alpha's
	// coordinator made alpha current: back to beta.)
	m.current = "beta"
	m.rebuild()
	m.sel = "th:beta:t-0005"
	drain(t, m, press(m, "enter"))
	if m.result.Attach != "s-5" {
		t.Fatalf("attach %q", m.result.Attach)
	}

	// ] from alpha opens beta's coordinator; [ from beta wraps to alpha.
	m.current = "alpha"
	drain(t, m, press(m, "]"))
	m.current = "beta"
	drain(t, m, press(m, "["))
	if got := strings.Join(src.opened, ","); got != "beta,alpha" {
		t.Fatalf("opened %q", got)
	}
}

func TestDashboardBell(t *testing.T) {
	src := &fakeSource{data: testData()}
	src.data.Alerts = 3
	m := newDash(DashOptions{Source: src, Width: 100, Height: 30})
	if hasBell(m.setData(src.data)) {
		t.Fatal("bell for alerts from before the dashboard opened")
	}
	if hasBell(m.setData(src.data)) {
		t.Fatal("bell without a new alert")
	}
	d := testData()
	d.Alerts = 4
	if !hasBell(m.setData(d)) {
		t.Fatal("no bell for a new alert")
	}
}

// TestDashboardThreadRow: a selected thread row shows its todos, its
// task's steps and its report's state, to read: the coordinator acts on
// them, so the report's Next lines never show. i opens the project's
// inbox.
func TestDashboardThreadRow(t *testing.T) {
	src := &fakeSource{data: testData()}
	src.data.Projects[1].Items = []project.Item{{ID: "x", Kind: "report", Subject: "t-0005", Summary: "t-0005 handed in report 1"}}
	m := newDash(DashOptions{Source: src, Width: 100, Height: 40, State: DashState{Current: "beta"}})
	m.setData(src.data)
	m.sel = "th:beta:t-0005"
	out := screen(m)
	for _, want := range []string{"✓ Outline", "◐ Draft §2", "steps 1/2", "✓ 1 Plan", "○ 2 Write",
		"report new · for the coordinator"} {
		if !strings.Contains(out, want) {
			t.Errorf("thread detail lacks %q:\n%s", want, out)
		}
	}
	for _, not := range []string{"a acks", "1-9", "next:", "Merge the PR", "Delete the branch"} {
		if strings.Contains(out, not) {
			t.Errorf("thread detail offers %q:\n%s", not, out)
		}
	}
	press(m, "I")
	if pv, ok := m.top().(*projectView); !ok || pv.tab != tabInbox || !strings.Contains(screen(m), "t-0005 handed in report 1") {
		t.Fatalf("inbox view:\n%s", screen(m))
	}
}

func hasBell(cmd tea.Cmd) bool {
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return false
	}
	for _, c := range batch {
		if raw, ok := c().(tea.RawMsg); ok && raw.Msg == "\a" {
			return true
		}
	}
	return false
}

// TestStatusLine: bar B, unfilled. Left the state glyph, project ›
// pane, the progress and the note; right the global keys. As it
// narrows, the step gives way, then the bar, then the preview note
// shrinks; the percent stays. After the prefix: the commands.
func TestStatusLine(t *testing.T) {
	info := proto.SessionInfo{ID: "s-4", Role: proto.RoleThread, Project: "terminatr", Agent: "claude", Thread: "t-0216", Task: "T232",
		State: "working", TodosDone: 2, TodosTotal: 5, Current: "Write §8"}
	line, _ := statusBar(info, nil, "Sidebar rework", false, 140, previewNote, nil)
	got := ansi.Strip(line)
	want := " " + ic().working + " terminatr › T232 Sidebar rework  40% " + bar(40) + " 2/5 ▸ Write §8 · " + previewNote
	if !strings.HasPrefix(got, want) || !strings.HasSuffix(got, " prefix+? help · prefix+d dashboard · ≡ menu ") {
		t.Fatalf("status line %q", got)
	}
	if strings.Contains(line, "\x1b[7m") {
		t.Fatalf("status line in reverse video: %q", line)
	}
	if w := ansi.StringWidth(line); w != 140 {
		t.Fatalf("status line is %d cells, want 140", w)
	}
	for cols, tail := range map[int]string{
		120: "40% " + bar(40) + " · " + previewNote,
		112: "40% · " + previewNote,
		105: "40% " + bar(40) + " · preview",
		95:  "40% · preview",
	} {
		line, _ := statusBar(info, nil, "Sidebar rework", false, cols, previewNote, nil)
		if got := ansi.Strip(line); !strings.Contains(got, "T232 Sidebar rework  "+tail+" ") {
			t.Errorf("%d columns: %q, want …%q", cols, got, tail)
		}
	}
	line, _ = statusBar(info, nil, "Sidebar rework", false, 85, previewNote, nil)
	if got := ansi.Strip(line); !strings.Contains(got, " terminatr › T232 Sid…  40% · preview ") {
		t.Errorf("85 columns: %q", got)
	}
	// A long note in passing is cut before the path is.
	coord := proto.SessionInfo{ID: "s-1", Role: proto.RoleCoordinator, Project: "alpha", Agent: "claude", State: "idle"}
	line, _ = statusBar(coord, nil, "", false, 120, "remote control off for alpha; it stays off until the coordinator is started anew", nil)
	if got := ansi.Strip(line); !strings.Contains(got, " alpha › coordinator  remote control off") || ansi.StringWidth(line) != 120 {
		t.Errorf("long note: %q", got)
	}
	// After the prefix: the commands.
	got = ansi.Strip(statusLine(info, nil, true, 160, ""))
	if !strings.Contains(got, ` prefix ▸ d dashboard · ] [ projects · + new · ; last place · ? all keys `) || !strings.HasSuffix(got, `prefix again sends it `) {
		t.Fatalf("pending status line %q", got)
	}
	// Too narrow for both: the commands stay, "prefix again sends it" goes.
	got = ansi.Strip(statusLine(info, nil, true, 80, ""))
	if !strings.Contains(got, `? all keys`) || strings.Contains(got, `again`) {
		t.Fatalf("pending status line, 80 columns: %q", got)
	}
}

// TestDashboardOverlays: views open on top of the list and close back
// to the one below; the help lists every labelled action.
func TestDashboardOverlays(t *testing.T) {
	src := &fakeSource{data: testData(), tasksTab: true}
	m := newDash(DashOptions{Source: src, Width: 100, Height: 30})
	m.setData(src.data)
	m.sel = "p:alpha"

	m.Update(boardMsg{}) // no board open: ignored
	m.Update(boardMsg{slug: "alpha", board: &tasks.Board{}})
	prefixed(m, "T") // a coordinator's row: T types through
	pv, ok := m.top().(*projectView)
	if !ok || pv.slug != "alpha" || pv.tab != tabTasks {
		t.Fatalf("t: overlay %T", m.top())
	}
	src.board = &tasks.Board{Tasks: []*tasks.Task{{ID: 1, Title: "One", Status: tasks.Review}}}
	m.Update(boardMsg{slug: "alpha", board: src.board})
	tasksTabNow(m, src)
	drain(t, m, press(m, "enter"))
	tv, ok := m.top().(*taskView)
	if !ok || !strings.Contains(screen(m), "Task · alpha") {
		t.Fatalf("enter on the board:\n%s", screen(m))
	}
	press(m, "?") // the task takes its own keys; ? isn't one
	if m.top() != tv {
		t.Fatalf("? on a task opened %T", m.top())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.top() != pv {
		t.Fatal("esc on a task returns to the popup")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.top() != nil {
		t.Fatalf("esc on the popup returns to the list, have %T", m.top())
	}

	prefixed(m, "?")
	out := ansi.Strip(strings.Join(m.top().(*helpView).box(m).body, "\n"))
	for _, a := range actions {
		if a.label != "" && !strings.Contains(out, " "+a.label+" ") && !strings.HasPrefix(out, a.label+" ") && !strings.Contains(out, "\n"+a.label+" ") {
			t.Errorf("help lacks %q:\n%s", a.label, out)
		}
	}
	press(m, "down") // the arrows scroll
	if m.top() == nil {
		t.Fatal("down closed the help")
	}
	press(m, "x")
	if m.top() == nil {
		t.Fatal("x closed the help")
	}
	keyPress(m, "esc")
	if m.top() != nil {
		t.Fatal("esc didn't close the help")
	}
}

// TestDashboardSplit: a wide window shows the selected row's details
// beside the list; < > | change the layout and ui.json keeps it; the
// mouse selects rows and drags the divider.
func TestDashboardSplit(t *testing.T) {
	src := &fakeSource{data: testData()}
	ui := filepath.Join(t.TempDir(), "ui.json")
	m := newDash(DashOptions{Source: src, Width: 140 + sideDefault, Height: 40, UIFile: ui, State: DashState{Current: "beta"}})
	m.setData(src.data)

	m.sel = "th:beta:t-0005"
	out := screen(m)
	for _, want := range []string{"│ T4 Write docs", "● working", "thread    t-0005", "progress  ▰▰▰▱▱ 60% 3/5",
		"PR        https://github.com/o/r/pull/7", "report    1, new · for the coordinator", "✓ Outline",
		"enter attach"} {
		if !strings.Contains(out, want) {
			t.Errorf("details lack %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Merge the PR") {
		t.Errorf("details show the report's Next lines:\n%s", out)
	}
	if strings.Contains(out, "        todos") {
		t.Errorf("details shown under the row as well as beside it:\n%s", out)
	}
	m.sel = "p:beta"
	if out := screen(m); !strings.Contains(out, "not running; enter starts it") {
		t.Errorf("project details:\n%s", out)
	}

	// < narrows the list and saves the layout; | hides the panel.
	before := m.geom().list().w
	press(m, "<")
	if after := m.geom().list().w; after >= before {
		t.Fatalf("< kept the list at %d columns", after)
	}
	press(m, "|")
	if m.panel() != "" {
		t.Fatal("| left the panel shown")
	}
	if l := LoadLayout(ui); l.Details || l.Split != defaultSplit-splitStep {
		t.Fatalf("ui.json holds %+v", l)
	}
	press(m, "|")

	// A click selects the row under it; dragging the divider resizes.
	sw := m.sideW()
	m.Update(tea.MouseClickMsg{X: sw + 3, Y: 2, Button: tea.MouseLeft}) // NEEDS YOU's first row
	if m.sel != "n:s-4" {
		t.Fatalf("click selected %q", m.sel)
	}
	lw := m.geom().list().w
	m.Update(tea.MouseClickMsg{X: sw + lw, Y: 5, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: sw + 70, Y: 5, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: sw + 70, Y: 5, Button: tea.MouseLeft})
	if lw := m.geom().list().w; lw != 70 || LoadLayout(ui).Split != 0.5 {
		t.Fatalf("drag: list %d wide, ui.json %+v", lw, LoadLayout(ui))
	}

	// Too narrow a window: one column, and < says why.
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	press(m, "<")
	if !strings.Contains(m.msg, "120 columns") {
		t.Fatalf("msg %q", m.msg)
	}
}

func TestLayoutFile(t *testing.T) {
	dir := t.TempDir()
	if l := LoadLayout(filepath.Join(dir, "none.json")); !reflect.DeepEqual(l, DefaultLayout) {
		t.Fatalf("missing file: %+v", l)
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{"), 0o644)
	if l := LoadLayout(bad); !reflect.DeepEqual(l, DefaultLayout) {
		t.Fatalf("bad file: %+v", l)
	}
	far := filepath.Join(dir, "far.json")
	os.WriteFile(far, []byte(`{"details":true,"split":5}`), 0o644)
	if l := LoadLayout(far); l.Split != maxSplit {
		t.Fatalf("split out of range: %+v", l)
	}
}

// TestDashboardPrefix: the prefix works on the dashboard as in a
// session, and a key typed after it in a session runs once back here.
func TestDashboardPrefix(t *testing.T) {
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 100, Height: 30})
	m.setData(src.data)
	m.sel = "p:alpha"
	prefix := tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl}
	m.Update(prefix)
	if !m.prefixed || !strings.Contains(screen(m), `prefix ▸ d dashboard`) {
		t.Fatalf("prefix not shown:\n%s", screen(m))
	}
	press(m, "t") // prefix+t is prefix+T
	if _, ok := m.top().(*projectView); !ok || m.prefixed {
		t.Fatalf("prefix t: overlay %T", m.top())
	}
	m.pop()
	m.Update(prefix)
	press(m, "d") // already on the dashboard: nothing
	if m.top() != nil || m.prefixed {
		t.Fatal("prefix d on the dashboard did something")
	}

}

// TestDashboardOver: a popup over a session (prefix then a key in it)
// opens about the session's project, draws over its screen under a
// header naming it, and esc ends the dashboard to attach again; prefix d
// there turns it into the dashboard.
func TestDashboardOver(t *testing.T) {
	src := &fakeSource{data: testData()}
	scr := make([]string, 29) // the popup covers all but its last rows
	scr[0] = "first row"
	for i := 1; i < 29; i++ {
		scr[i] = strings.Repeat(" ", 60) + "last"
	}
	m := newDash(DashOptions{Source: src, Width: 100, Height: 30, State: DashState{Current: "beta"},
		Over: &Over{Key: "I", Project: "alpha", Session: "s-5", Title: "s-5 · beta t-0005", Screen: scr}})
	m.setData(src.data)
	if pv, ok := m.top().(*projectView); !ok || pv.slug != "alpha" || pv.tab != tabInbox {
		t.Fatalf("over: overlay %T %+v", m.top(), m.top())
	}
	out := screen(m)
	for _, want := range []string{"tm s-5 · beta t-0005", "last", "2 inbox"} {
		if !strings.Contains(out, want) {
			t.Errorf("over lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "NEEDS YOU") || strings.Contains(out, "first row") {
		t.Errorf("over shows the list, or the row under the header:\n%s", out)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.result.Attach != "s-5" {
		t.Fatalf("esc over a session: result %+v", m.result)
	}

	// prefix d: the dashboard, with the popup closed.
	m = newDash(DashOptions{Source: src, Width: 100, Height: 30,
		Over: &Over{Key: "?", Session: "s-5", Screen: scr}})
	m.setData(src.data)
	if _, ok := m.top().(*helpView); !ok {
		t.Fatalf("over ?: overlay %T", m.top())
	}
	m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if m.over != nil || m.top() != nil || m.result.Attach != "" {
		t.Fatalf("prefix d over a session: over %v top %T result %+v", m.over, m.top(), m.result)
	}
	if !strings.Contains(screen(m), "NEEDS YOU") {
		t.Fatalf("prefix d didn't show the dashboard:\n%s", screen(m))
	}
}

// TestDashboardFooter: the footer lists the ≡ menu, then the keys that
// apply to the selected row.
func TestDashboardFooter(t *testing.T) {
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 100, Height: 30, State: DashState{Current: allProjects}})
	m.setData(src.data)
	m.sel = "n:s-1" // alpha's, in every project's NEEDS YOU
	if got, want := m.footKeys(), "≡ menu · type to write to the coordinator · enter attach · prefix+A project · prefix+T tasks · prefix+I inbox · prefix+, settings · prefix+? help · prefix+q quit"; !strings.HasPrefix(got, want) {
		t.Errorf("n:s-1: footer %q, want %q…", got, want)
	}
	m.showProject("beta")
	for sel, want := range map[string]string{
		"th:beta:t-0005": "≡ menu · enter attach · A project · T tasks · I inbox",
		"th:beta:t-0006": "≡ menu · A project · T tasks · I inbox",
		"p:beta":         "≡ menu · enter open · A project · T tasks",
	} {
		m.sel = sel
		if got := m.footKeys(); !strings.HasPrefix(got, want) {
			t.Errorf("%s: footer %q, want %q…", sel, got, want)
		}
	}
	m.data.Projects = nil
	m.setData(Data{ServerOK: true})
	if got := m.footKeys(); !strings.HasPrefix(got, "≡ menu · + new project · , settings") {
		t.Errorf("no projects: footer %q", got)
	}
}

// TestDashboardPopups: views draw as boxes over the dimmed list; the
// settings popup has the settings of every project.
func TestDashboardPopups(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TERMINATR_HOME", home)
	src := &fakeSource{data: testData()}
	src.data.Projects[0].Items = []project.Item{{ID: "x", Kind: "report", Summary: "t-0002 handed in report 1"}}
	m := newDash(DashOptions{Source: src, Width: 100, Height: 30})
	m.setData(src.data)
	m.sel = "p:alpha"

	prefixed(m, "I") // a coordinator's row
	out := screen(m)
	for _, want := range []string{"╭─ alpha ─", "2 inbox", "│ ", "t-0002 handed in report 1", "╰─", "esc close",
		"PROJECTS"} { // the sidebar stays in view beside the box
		if !strings.Contains(out, want) {
			t.Errorf("inbox popup lacks %q:\n%s", want, out)
		}
	}
	for _, l := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(l); w > 100 {
			t.Errorf("line %d cells wide: %q", w, l)
		}
	}
	m.pop()

	prefixed(m, ",")
	out = screen(m)
	for _, want := range []string{"Settings", "Prefix key", "ctrl+b", "Details panel", "List size", "Sidebar"} {
		if !strings.Contains(out, want) {
			t.Errorf("settings lack %q:\n%s", want, out)
		}
	}

	// A tiny window: the box takes the body, and nothing overflows.
	m.Update(tea.WindowSizeMsg{Width: 30, Height: 8})
	for _, l := range strings.Split(screen(m), "\n") {
		if w := ansi.StringWidth(l); w > 30 {
			t.Errorf("tiny window: line %d cells wide: %q", w, l)
		}
	}
}

// TestPromptWraps: a long prompt text wraps in the popup instead of
// losing its start.
func TestPromptWraps(t *testing.T) {
	long := "/var/folders/0g/abcdefghijklmnopqrstuvwxyz/T/TestSmokeMakeRun3197026846/003/work"
	lines := wrapInput("claude session in directory: ", long+"█", 40)
	got := ansi.Strip(strings.Join(lines, ""))
	if got != "claude session in directory: "+long+"█" || len(lines) != 3 {
		t.Fatalf("wrapped %q", lines)
	}
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > 40 {
			t.Fatalf("line %d cells wide: %q", w, l)
		}
	}
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 100, Height: 30})
	m.setData(src.data)
	prefixed(m, "+")
	m.top().(*inputView).lineEdit = newLine(long)
	if out := screen(m); !strings.Contains(out, "Its name (becomes a lower-case slug") || !strings.Contains(out, "│ /var/folders") {
		t.Fatalf("prompt:\n%s", out)
	}
}

// TestPopupMargins: beside a popup, over the details panel and the list
// alike, no margin too narrow to read shows cut-off letters (a stray
// "O" of OTHER SESSIONS, "b…" of the details): it is cleared, or the box
// takes the width.
func TestPopupMargins(t *testing.T) {
	for _, w := range []int{60, 100, 140, 180} {
		for _, key := range []string{"?", "A", ","} {
			src := &fakeSource{data: testData()}
			m := newDash(DashOptions{Source: src, Width: w + sideDefault, Height: 40, State: DashState{Current: "beta"}})
			m.setData(src.data)
			m.sel = "th:beta:t-0005"
			keyPress(m, key)
			lines := strings.Split(whole(m), "\n")
			g := m.geo
			if g == nil {
				t.Fatalf("%d %s: no popup", w, key)
			}
			for y := g.y; y < g.y+g.h; y++ {
				r := []rune(lines[y])
				left, right := string(r[:g.x]), string(r[min(g.x+g.w, len(r)):])
				if g.x-m.sideW() < sliver && strings.TrimSpace(left[min(m.sideW(), len(left)):]) != "" || m.winW-g.x-g.w < sliver && strings.TrimSpace(right) != "" {
					t.Fatalf("%d %s: row %d shows %q | %q beside the box:\n%s", w, key, y, left, right, screen(m))
				}
			}
		}
	}
}

// TestDashboardNeedsYouTasks: a project's tasks in review or blocked are
// rows of its NEEDS YOU, the sidebar hints at them and the count says
// where to look; enter shows the task in the project popup's Tasks tab,
// and acts on nothing.
func TestDashboardNeedsYouTasks(t *testing.T) {
	d := testData()
	board := &tasks.Board{Tasks: []*tasks.Task{
		{ID: 1, Title: "Write the README", Status: tasks.Started},
		{ID: 3, Title: "Remove the prefix caption", Status: tasks.Review, Thread: "t-0002",
			Steps: []tasks.Step{{N: 1, Text: "Drop it", Done: true}, {N: 2, Text: "Goldens", Done: true}}},
		{ID: 5, Title: "Pick a licence", Status: tasks.Blocked},
	}}
	alpha := &d.Projects[0]
	alpha.NeedsYou = []*tasks.Task{board.Tasks[1], board.Tasks[2]}
	alpha.Counts = map[string]int{"needs_you": 2, "in_motion": 1}
	src := &fakeSource{data: d, board: board, tasksTab: true}
	m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 30, State: DashState{Current: "alpha"}})
	m.layout.Details = false
	m.setData(src.data)
	tasksTabNow(m, src)
	out := screen(m)
	needs := strings.Index(out, "NEEDS YOU 4 ─")
	if needs < 0 {
		t.Fatalf("no NEEDS YOU 4:\n%s", out)
	}
	at := needs
	for _, want := range []string{
		"  coordinator ",
		"  T3 Remove the prefix caption              ◆ review    ▰▰▰▰▰  2/2  t-0002",
		"  T5 Pick a licence                         ▲ blocked",
		"  s-4 ",
		" alpha ─",
		"tasks: 2 need you (T lists them) · 1 in motion · 0 on deck",
	} {
		i := strings.Index(out[at:], want)
		if i < 0 {
			t.Fatalf("missing %q after the previous row in\n%s", want, out)
		}
		at += i
	}
	if side := whole(m); !strings.Contains(side, " ■ alpha              0         ⚑  │") {
		t.Errorf("no sidebar hint for alpha:\n%s", side)
	}

	m.sel = "nt:alpha:5"
	if foot := m.footKeys(); !strings.Contains(foot, "enter show") {
		t.Errorf("footer %q", foot)
	}
	cmd := press(m, "enter")
	pv, ok := m.top().(*projectView)
	if !ok || pv.slug != "alpha" || pv.tab != tabTasks {
		t.Fatalf("enter opened %T %+v", m.top(), m.top())
	}
	feed(m, cmd) // the board
	// The mod hears which task the tab shows; the cursor goes to its row.
	pollModViews(m)
	if i := slices.IndexFunc(src.shown, func(s modview.Shown) bool { return s.Mod == mod.BundledTasks }); i < 0 || src.shown[i].Task != "T5" {
		t.Errorf("shown %+v, want the Tasks tab for T5", src.shown)
	}
	if out := screen(m); !strings.Contains(out, "c coordinator") {
		t.Errorf("Tasks tab:\n%s", out)
	}
	if host := m.mv.host(src.tasksTabView(), "alpha"); host == nil || host.Cursor("tasks") != "T5" {
		t.Errorf("cursor not on T5")
	}
	if len(src.opened) != 0 || m.result.Attach != "" {
		t.Errorf("enter on a task acted: opened %v, attach %q", src.opened, m.result.Attach)
	}
}

// An inbox row leads with the kind, then the task, what happened and last
// the title; items of one kind for one thread are one row, counted.
func TestInboxLinesRows(t *testing.T) {
	items := []project.Item{
		{ID: "a", Kind: "report", Subject: "t-0001", Created: time.Now(), Summary: "T1 Fix the login (t-0001) handed in report 1"},
		{ID: "b", Kind: "report", Subject: "t-0001", Created: time.Now(), Summary: "T1 Fix the login (t-0001) handed in report 2"},
	}
	lines, _, hits := inboxLines(items, 0, 80)
	if len(lines) != 1 || len(hits) != 1 {
		t.Fatalf("lines %q hits %v", lines, hits)
	}
	got := ansi.Strip(lines[0])
	if !strings.HasPrefix(got, "report") || !strings.Contains(got, "×2 T1 handed in report 2 Fix the login") {
		t.Errorf("row %q", got)
	}
	if narrow, _, _ := inboxLines(items, -1, 50); strings.Contains(ansi.Strip(narrow[0]), "login") || !strings.Contains(ansi.Strip(narrow[0]), "report 2") {
		t.Errorf("narrow row %q", ansi.Strip(narrow[0]))
	}
}

// TestOldConsoleHint: a console older than the server (started before
// an upgrade the server restarted on) says to restart the console, not
// the server, once as a message and in the footer in the warning colour
// (T344).
func TestOldConsoleHint(t *testing.T) {
	defer func(v string) { version.Version = v }(version.Version)
	freshConsole(t)
	version.Version = "v0.24.0"
	srv := ServerInfo{Version: "v0.25.0", Build: "v0.25.0+x+y"}
	if !consoleOlder(srv) {
		t.Fatal("v0.24.0 console, v0.25.0 server: not older")
	}
	h := versionHint(srv)
	if !strings.Contains(h, "quit it and run tm again") || strings.Contains(h, "server restart") {
		t.Errorf("hint %q", h)
	}
	if n := oldConsoleNews(srv); n != h {
		t.Errorf("first news %q", n)
	}
	if n := oldConsoleNews(srv); n != "" {
		t.Errorf("said again: %q", n)
	}
	for _, s := range []ServerInfo{
		{Version: "v0.23.0", Build: "v0.23.0+x+y"}, // the server is older
		{Version: "dev", Build: "dev+x+y"},         // a source build
		{},                                         // a server too old to say
	} {
		if consoleOlder(s) {
			t.Errorf("%+v: console older", s)
		}
	}
}

func TestVersionHint(t *testing.T) {
	own := ServerInfo{Version: version.Version, Build: version.BuildID()}
	if h := versionHint(own); h != "" {
		t.Errorf("same build: %q", h)
	}
	if h := versionHint(ServerInfo{}); h != "" {
		t.Errorf("old server: %q", h)
	}
	newer := own
	newer.Latest, newer.Upgrade = "v99.0.0", "brew upgrade terminatr"
	if h := versionHint(newer); !strings.Contains(h, "v99.0.0") || !strings.Contains(h, "brew upgrade terminatr") {
		t.Errorf("newer release: %q", h)
	}
	other := ServerInfo{Version: "v0.1.0", Build: "v0.1.0+x+y", Latest: "v99.0.0", Upgrade: "tm update"}
	if h := versionHint(other); !strings.Contains(h, "tm server restart") {
		t.Errorf("other build: %q", h)
	}
	if v := serverVersion(other); !strings.Contains(v, "v0.1.0") || !strings.Contains(v, "(tm "+version.Version+")") {
		t.Errorf("footer: %q", v)
	}
	if v := serverVersion(own); strings.Contains(v, "(tm") {
		t.Errorf("footer for the same version: %q", v)
	}
	last := func(w int, s ServerInfo) string {
		f := sideFooter(nil, w, sideHeadIn{}, s, false, false)
		if len(f) == 0 {
			return ""
		}
		return strings.TrimSpace(ansi.Strip(f[len(f)-1]))
	}
	if f := last(32, other); f != versionHintShort(other) {
		t.Errorf("narrow footer %q", f)
	}
	if f := last(130, other); f != versionHint(other) {
		t.Errorf("wide footer %q", f)
	}
	if f := sideFooter(nil, sideSlim, sideHeadIn{}, other, false, false); f != nil {
		t.Errorf("slim footer %q", f)
	}
	if f := sideFooter(nil, 32, sideHeadIn{}, ServerInfo{}, false, false); len(f) != 4 {
		t.Errorf("footer without a hint %q", f)
	}
}

// The version is in the help and the ≡ menu's title, not the header or
// the sidebar; settings say it in an About line.
func TestVersionPlaces(t *testing.T) {
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 30, State: DashState{Current: "beta"}})
	d := src.data
	d.ServerOK, d.UpdateCheck = true, true
	d.Server = ServerInfo{Version: "v9.9.9", Build: version.BuildID()}
	m.setData(d)
	lines := strings.Split(ansi.Strip(whole(m)), "\n")
	if strings.Contains(lines[0], "v9.9.9") || !strings.Contains(lines[0], "server ok") {
		t.Errorf("header %q", lines[0])
	}
	if strings.Contains(whole(m), "v9.9.9") {
		t.Errorf("the version shows:\n%s", whole(m))
	}
	keyPress(m, "?")
	if s := whole(m); !strings.Contains(s, "terminatr v9.9.9") {
		t.Errorf("help:\n%s", s)
	}
	keyPress(m, "esc")
	m.openMenu(versionTitle(m.data.Server), m.dashItems(), 0, m.bodyRows())
	if s := whole(m); !strings.Contains(s, "terminatr v9.9.9") {
		t.Errorf("menu:\n%s", s)
	}
	m.stack = nil
	m.openSettings()
	if s := ansi.Strip(whole(m)); !strings.Contains(s, "About: tm "+version.Version+" · server v9.9.9 · update check on") {
		t.Errorf("settings:\n%s", s)
	}
}

func (f *fakeSource) ModActionDone(mod, action string, target map[string]string, ok bool, token string) error {
	d := fmt.Sprintf("%s %s %s/%s%s %v", mod, action, target["project"], target["task"], target["session"], ok)
	if token != fakePressToken(mod, action) {
		// Not the token its check gave: the server would refuse it.
		d += " (no press token)"
	}
	f.done = append(f.done, d)
	return nil
}

func (f *fakeSource) ModActionCheck(mod, action string, target map[string]string) (string, error) {
	if target["project"] == "beta" {
		return "", errors.New(mod + " doesn't run for project beta")
	}
	return fakePressToken(mod, action), nil
}

func fakePressToken(mod, action string) string { return "press " + mod + " " + action }

// TestViewCallsInTurn: the view calls of one console reach the server in
// the order they were made, though each runs in a command of its own. A
// click on a project, then one on its coordinator, must not apply as
// view.attach, then view.project: the view would leave the attach just
// begun for the dashboard (T283, TestSmokeCoordinatorRemoteControl).
func TestViewCallsInTurn(t *testing.T) {
	var mu sync.Mutex
	var got []string
	vc := &ViewConn{v: view.View{Name: "v", Seq: 1}, up: true, watch: map[chan struct{}]bool{},
		serve: func(method string, _ proto.ViewParams) (view.View, error) {
			mu.Lock()
			defer mu.Unlock()
			got = append(got, method)
			return view.View{Name: "v", Seq: uint64(len(got) + 1)}, nil
		}}
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 120, Height: 30, State: DashState{Current: "beta"}})
	m.setData(src.data)
	m.view = vc
	project := m.showProject("alpha")
	_, attach := m.update(actionMsg{attach: "s-1", current: "alpha"})
	if project == nil || attach == nil {
		t.Fatal("no view calls")
	}
	// The attach's command runs first, as the scheduler may have it.
	ran := make(chan struct{})
	go func() { attach(); close(ran) }()
	select {
	case <-ran:
		t.Fatal("view.attach went before the view.project made earlier")
	case <-time.After(50 * time.Millisecond):
	}
	project()
	<-ran
	if want := []string{proto.MethodViewProject, proto.MethodViewAttach}; !slices.Equal(got, want) {
		t.Fatalf("calls %v, want %v", got, want)
	}
}

// TestQueuedKeyKeepsAttach: a key that waited for the action in flight
// doesn't swallow that action's attach or its selection.
func TestQueuedKeyKeepsAttach(t *testing.T) {
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 120, Height: 30, State: DashState{Current: "beta"}})
	m.setData(src.data)
	m.busy = true
	ran := false
	m.queued = append(m.queued, func(*dash) tea.Cmd {
		ran = true
		return func() tea.Msg { return nil }
	})
	_, cmd := m.update(actionMsg{attach: "s-1", current: "alpha"})
	if ran {
		t.Fatal("the queued key ran before the attach")
	}
	if m.result.Attach != "s-1" || cmd == nil {
		t.Fatalf("attach %q, cmd %v: the attach was dropped", m.result.Attach, cmd)
	}
	if m.current != "alpha" {
		t.Fatalf("current %q", m.current)
	}
}

// TestStatusBarButtonsFollowRebind: the buttons sit under their text
// when help.open is bound to a wider key.
func TestStatusBarButtonsFollowRebind(t *testing.T) {
	old := binds
	t.Cleanup(func() { binds = old })
	if err := ApplyKeymap(map[string][]string{"help.open": {"f1"}}, ""); err != nil {
		t.Skipf("keymap: %v", err)
	}
	info := proto.SessionInfo{ID: "s-4", Role: proto.RoleThread, Project: "terminatr", Agent: "claude", State: "working"}
	line, hs := statusBar(info, nil, "", false, 140, previewNote, nil)
	got := ansi.Strip(line)
	for key, text := range map[string]string{"?": "prefix+f1", "d": "prefix+d dashboard", "menu": menuButton + " menu"} {
		i := strings.Index(got, text)
		if i < 0 {
			t.Fatalf("no %q in %q", text, got)
		}
		col := ansi.StringWidth(got[:i])
		if hintAt(hs, col) != key {
			t.Errorf("column %d (%q) is button %q, want %q; line %q", col, text, hintAt(hs, col), key, got)
		}
	}
}

// TestActWaitsForTheRunningOne: an action asked for while another runs
// is queued, not started beside it.
func TestActWaitsForTheRunningOne(t *testing.T) {
	m := &dash{}
	if m.act(func() actionMsg { return actionMsg{} }) == nil || !m.busy {
		t.Fatal("the first action didn't start")
	}
	if m.act(func() actionMsg { return actionMsg{} }) != nil || len(m.queued) != 1 || m.acts != 1 {
		t.Fatalf("the second ran beside the first: %d queued, %d started", len(m.queued), m.acts)
	}
	m.busy = false
	if m.runQueued() == nil || !m.busy || len(m.queued) != 0 || m.acts != 2 {
		t.Fatalf("the queued action didn't run in its turn: busy %v, %d started", m.busy, m.acts)
	}
}

// TestDashboardPeerNeedAct: at a needs switch of answer, a paired
// server's need says "answer here", enter opens its dialog with what it
// takes (a menu's options, a dialog's once-only options, accept and
// send back), a pick asks first and goes to peer.need-act with the words
// shown as the guard; an escape's opens nothing; at view enter still
// shows the read-only dashboard.
func TestDashboardPeerNeedAct(t *testing.T) {
	d := testData()
	menu := &proto.Question{Questions: []proto.QuestionItem{{Question: "Which cache?", Options: []proto.QuestionOption{{Label: "Redis"}, {Label: "None"}}}}}
	d.Peers = []proto.PeerMirror{{Alias: "mini", State: "up", Watch: true, NeedsCap: true, Needs: proto.NeedsAnswer, Projects: []proto.PeerMirrorProject{
		{PeerProject: proto.PeerProject{Project: "acme-api"}, Watch: &proto.ProjectWatch{},
			Needs: []proto.PeerNeed{
				{ID: "question:s-1:a", Kind: proto.NeedQuestion, Task: "T7", Session: "s-1", Question: menu},
				{ID: "approval:s-2:b", Kind: proto.NeedApproval, Task: "T8", Approval: &proto.PeerApproval{Question: "Allow curl?",
					Options: []string{"Yes", "Yes, and don't ask again", "No"}, Scopes: []string{"once", "persistent", "once"}}},
				{ID: "approval:s-3", Kind: proto.NeedApproval, Role: proto.RoleCoordinator, Escape: true},
				{ID: "review:T3:1", Kind: proto.NeedReview, Task: "T3", Title: "Rate limits", Status: "review"},
				{ID: "question:s-4:m", Kind: proto.NeedQuestion, Task: "T9", Session: "s-4", Question: &proto.Question{Screen: true, Questions: []proto.QuestionItem{
					{Question: "Which camera?", Answered: true, Answer: "DSLR", Options: []proto.QuestionOption{{Label: "DSLR"}}},
					{Question: "Which subjects?", MultiSelect: true, Options: []proto.QuestionOption{{Label: "Plants"}, {Label: "Birds"}, {Label: "Hills"}}}}}},
			}},
	}}}
	src := &fakeSource{data: d}
	m := newDash(DashOptions{Source: src, Width: 160 + sideDefault, Height: 30, State: DashState{Current: allProjects}})
	m.layout.Details = false
	m.setData(src.data)
	if out := screen(m); !strings.Contains(out, "answer here, for mini") {
		t.Errorf("no answer here in\n%s", out)
	}
	open := func(id string) {
		t.Helper()
		m.stack = nil
		m.sel = "nr:@mini/acme-api:" + id
		m.enter("")
	}
	open("question:s-1:a")
	if out := screen(m); !strings.Contains(out, "Which cache?") || !strings.Contains(out, "2  None") {
		t.Fatalf("the question's dialog:\n%s", out)
	}
	drain(t, m, press(m, "2"))
	drain(t, m, press(m, "Y"))
	want := proto.PeerNeedActParams{Peer: "mini", PeerAct: proto.PeerAct{Project: "acme-api", Need: "question:s-1:a", Act: proto.ActAnswer, Question: "Which cache?", Answer: "None"}}
	if len(src.acts) != 1 || !reflect.DeepEqual(src.acts[0], want) {
		t.Fatalf("acts %+v, want %+v", src.acts, want)
	}

	open("approval:s-2:b")
	v, ok := m.stack[len(m.stack)-1].(*peerNeedView)
	if !ok || len(v.choices) != 2 || v.choices[1].act.Choice != 3 || v.choices[1].act.Option != "No" {
		t.Fatalf("the dialog's choices: %+v", m.stack)
	}

	open("review:T3:1")
	drain(t, m, press(m, "2"))
	in, ok := m.stack[len(m.stack)-1].(*inputView)
	if !ok {
		t.Fatalf("send back asked no note: %+v", m.stack)
	}
	m.pop()
	drain(t, m, in.submit("tests fail"))
	if a := src.acts[len(src.acts)-1]; a.Act != proto.ActSendBack || a.Status != "review" || a.Note != "tests fail" {
		t.Errorf("send back %+v", a)
	}

	// A multi-select (T900): numbers and space tick, submit sends the
	// ticked ones by index, their labels the guard.
	open("question:s-4:m")
	if out := screen(m); !strings.Contains(out, "1  [ ] Plants") || !strings.Contains(out, "submit the options ticked") {
		t.Fatalf("the multi-select's dialog:\n%s", out)
	}
	n := len(src.acts)
	drain(t, m, press(m, "enter")) // the cursor on option 1 ticks it
	drain(t, m, press(m, "3"))
	drain(t, m, press(m, "2"))
	drain(t, m, press(m, "2"))
	if out := screen(m); !strings.Contains(out, "1  [x] Plants") || !strings.Contains(out, "2  [ ] Birds") || !strings.Contains(out, "3  [x] Hills") {
		t.Fatalf("ticked:\n%s", out)
	}
	drain(t, m, press(m, "4")) // submit
	if !strings.Contains(screen(m), "Plants, Hills?") {
		t.Fatalf("no confirmation of Plants, Hills:\n%s", screen(m))
	}
	drain(t, m, press(m, "Y"))
	want = proto.PeerNeedActParams{Peer: "mini", PeerAct: proto.PeerAct{Project: "acme-api", Need: "question:s-4:m", Act: proto.ActAnswer,
		Index: 1, Question: "Which subjects?", Answer: "Plants, Hills", Picks: []int{0, 2}}}
	if len(src.acts) != n+1 || !reflect.DeepEqual(src.acts[n], want) {
		t.Fatalf("acts %+v, want %+v", src.acts[n:], want)
	}
	// Nothing ticked: submit says so and stays.
	open("question:s-4:m")
	drain(t, m, press(m, "4"))
	if len(src.acts) != n+1 || !strings.Contains(m.msg, "tick one option or more") {
		t.Errorf("submit with none ticked: %d acts, msg %q", len(src.acts), m.msg)
	}

	open("approval:s-3")
	if len(m.stack) != 0 || !strings.Contains(m.msg, "only mini's user") {
		t.Errorf("an escape: stack %v, msg %q", m.stack, m.msg)
	}

	src.data.Peers[0].Needs = proto.NeedsView
	m.setData(src.data)
	open("question:s-1:a")
	if len(m.stack) != 0 || m.current != "@mini/acme-api" {
		t.Errorf("at view: stack %v, current %q", m.stack, m.current)
	}
}
