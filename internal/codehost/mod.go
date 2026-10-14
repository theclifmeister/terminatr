package codehost

// Code host mods (mods design §2 tier 5, §11, §12 Phase 2): a host of a
// kind tm has no Go host for (GitLab first) is served by a code mod that
// answers the codehost.* methods over the mod host's wire, typically by
// running glab under its commands grant. Mod is the adapter that makes
// such a mod a Host. It sends the mod what the call needs, never the
// repo's folder, and keeps the fixed words of a PR whatever the mod
// answers: a field outside them is dropped, as for gh's answer.
//
// What is destructive stays tm's, whoever supplies the host: a mod's
// word never removes a worktree or a branch (External: git checks that
// the commits are on the remote), merge detection by git alone is tm's
// (MergeCommit, MergedPR), and tm thread merge makes its checks itself
// and only asks the mod to merge the head it checked.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

// The methods a code host mod answers, as $.codehost.handle names them
// after "codehost.".
const (
	MethodPR        = "codehost.pr"        // ModRef → ModPR, or null for no PR
	MethodPRState   = "codehost.prState"   // ModRef (branch) → {state}
	MethodPRHead    = "codehost.prHead"    // ModRef (url) → {state, number, head}
	MethodFailedLog = "codehost.failedLog" // ModRef (number, head) → {job, log}
	MethodMerge     = "codehost.merge"     // ModRef (number, head) → anything
	MethodDoctor    = "codehost.doctor"    // ModRef (remote only) → {checks: [{name, ok, detail}]}
)

// ModMethods are the codehost.* methods, in the order of the Host's.
var ModMethods = []string{MethodPR, MethodPRState, MethodPRHead, MethodFailedLog, MethodMerge, MethodDoctor}

// modKindRE is a code host mod's kind: lower case, as code_host names it.
var modKindRE = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

// ModKind says whether kind is one a code host mod may provide: a
// well-formed kind that isn't GitHub's or Azure DevOps', which stay tm's
// own hosts.
func ModKind(kind string) bool {
	return modKindRE.MatchString(kind) && kind != GitHubKind && kind != AzureKind
}

// ModRemote is the server and project path of a remote URL, as a code
// host mod is asked about it: Target.OrgURL "https://<host>", Target.Repo
// "group/subgroup/project" (no .git, percent-decoded). ok is false for no
// URL, a local one or a path tm can't read.
func ModRemote(remote string) (t Target, ok bool) {
	if strings.HasPrefix(strings.TrimSpace(remote), "file://") {
		return Target{}, false
	}
	host, path, ok := splitRemote(remote)
	if !ok || !hostRE.MatchString(host) {
		return Target{}, false
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	p, err := url.PathUnescape(path)
	if err != nil || !projectPathRE.MatchString(p) {
		return Target{}, false
	}
	return Target{OrgURL: "https://" + host, Repo: p}, true
}

var (
	hostRE        = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,252}$`)
	projectPathRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)+$`)
)

// ModCall sends one codehost.* method to a mod and answers its result:
// the server's supervisor in the server, a test's host in tests.
type ModCall func(ctx context.Context, method string, params any) (json.RawMessage, error)

// ModProvider finds the mod that serves kind for project ("" for any):
// its id and how to call it, or why there is none (a *CLIError naming
// the fix).
type ModProvider func(kind, project string) (id string, call ModCall, err error)

var provider atomic.Pointer[ModProvider]

// Provide sets how Pick reaches code host mods; nil takes it away. Only
// the server sets one: it runs the mods (a command run outside it has
// none, and gets Unserved).
func Provide(p ModProvider) {
	if p == nil {
		provider.Store(nil)
		return
	}
	provider.Store(&p)
}

// pickMod is the Mod for t from the provider, or Unserved.
func pickMod(t Target, project string) Host {
	p := provider.Load()
	if p == nil {
		return Unserved{Target: t, Err: &CLIError{CLI: t.Kind, Problem: "no tm server runs its code host mod here",
			Advice: "the user starts the tm server (tm doctor)", Err: fmt.Errorf("code host %s: served by a mod, which only the tm server runs", t.Kind)}}
	}
	id, call, err := (*p)(t.Kind, project)
	if err != nil {
		return Unserved{Target: t, Err: err}
	}
	return Mod{Target: t, ID: id, Call: call}
}

// Mod is a Host served by a code host mod (ModProvider).
type Mod struct {
	// Target is the repo's: the mod's kind, its server and project path
	// (ModRemote of its origin).
	Target Target
	// ID is the mod's id, for errors and tm doctor.
	ID   string
	Call ModCall
	// Timeout bounds each call; zero is ModTimeout (ModMergeTimeout for
	// a merge).
	Timeout time.Duration
}

// Timeouts of a mod's calls: the mod runs its CLI, which may run a few
// requests, under the broker's own limit on each program.
const (
	ModTimeout      = 60 * time.Second
	ModMergeTimeout = 120 * time.Second
)

// ModRemoteRef is the repo as a code host mod gets it.
type ModRemoteRef struct {
	Kind string `json:"kind"`
	Host string `json:"host"` // gitlab.com, or a server's host[:port]
	Path string `json:"path"` // group/subgroup/project
}

// ModRef is every codehost.* method's params: the repo's remote, and
// what the method asks about.
type ModRef struct {
	Remote ModRemoteRef `json:"remote"`
	URL    string       `json:"url,omitempty"`
	Branch string       `json:"branch,omitempty"`
	Number int          `json:"number,omitempty"`
	Head   string       `json:"head,omitempty"`
}

// ModPR is codehost.pr's answer, PR's fields by their state-file names.
type ModPR struct {
	Number     int    `json:"number"`
	URL        string `json:"url"`
	State      string `json:"state"`
	Checks     string `json:"checks"`
	Failed     int    `json:"failed"`
	Review     string `json:"review"`
	MergedAt   string `json:"merged_at"`
	Head       string `json:"head"`
	Merge      string `json:"merge"`
	Base       string `json:"base"`
	Mergeable  string `json:"mergeable"`
	MergeState string `json:"merge_state"`
	Branch     string `json:"branch"`
}

func (m Mod) Kind() string { return m.Target.Kind }

func (m Mod) ref() ModRef {
	host := strings.TrimPrefix(m.Target.OrgURL, "https://")
	return ModRef{Remote: ModRemoteRef{Kind: m.Target.Kind, Host: host, Path: m.Target.Repo}}
}

// call sends method; an error is a failed call (CLIError of the kind).
func (m Mod) call(method string, params ModRef, out any) error {
	if m.Target.Repo == "" && method != MethodDoctor {
		return &CLIError{CLI: m.Target.Kind, Problem: "the repo's origin is not a remote tm can name",
			Advice: "the user checks git remote get-url origin", Err: fmt.Errorf("%s: no remote to ask about", method)}
	}
	d := m.Timeout
	if d <= 0 {
		d = ModTimeout
		if method == MethodMerge {
			d = ModMergeTimeout
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	raw, err := m.Call(ctx, method, params)
	if err != nil {
		return m.failed(method, err)
	}
	if out == nil || len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return m.failed(method, fmt.Errorf("its answer isn't the expected shape: %v", err))
	}
	return nil
}

func (m Mod) failed(method string, err error) error {
	return &CLIError{CLI: m.Target.Kind, Problem: "its code host mod " + m.ID + " failed",
		Advice: "the user checks tm mod logs " + m.ID + " and tm doctor", Err: fmt.Errorf("mod %s %s: %w", m.ID, method, err)}
}

// PR asks the mod about ref: by number, else by a PR URL of this repo,
// else by branch. A null answer, or one without a number, is ErrNoPR.
func (m Mod) PR(repo string, ref Ref) (PR, error) {
	q := m.ref()
	switch {
	case ref.Number > 0:
		q.Number = ref.Number
	case ref.URL != "":
		if n, ok := m.ParsePRURL(ref.URL); ok {
			q.Number = n
		} else if branchRE.MatchString(ref.Branch) {
			q.Branch = ref.Branch
		}
	case branchRE.MatchString(ref.Branch):
		q.Branch = ref.Branch
	}
	if q.Number == 0 && q.Branch == "" {
		return PR{}, ErrNoRef
	}
	var raw json.RawMessage
	if err := m.call(MethodPR, q, &raw); err != nil {
		return PR{}, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return PR{}, ErrNoPR
	}
	var a ModPR
	if err := json.Unmarshal(raw, &a); err != nil {
		return PR{}, m.failed(MethodPR, fmt.Errorf("its answer isn't a PR: %v", err))
	}
	pr := m.fixed(a)
	if pr.Number == 0 || (q.Number > 0 && pr.Number != q.Number) {
		return PR{}, ErrNoPR
	}
	return pr, nil
}

// Fixed words a mod's PR may use (PR); anything else is dropped.
var (
	modStates     = []string{"OPEN", "CLOSED", "MERGED"}
	modChecks     = []string{"pass", "fail", "pending"}
	modReviews    = []string{"APPROVED", "CHANGES_REQUESTED", "REVIEW_REQUIRED"}
	modMergeables = []string{"MERGEABLE", "CONFLICTING", "UNKNOWN"}
)

func oneOf(s string, words []string) string {
	for _, w := range words {
		if s == w {
			return s
		}
	}
	return ""
}

// fixed keeps what a mod's answer may say: the fixed words, a number, a
// commit id, a branch, and a URL that is one of this repo's PR URLs with
// the same number.
func (m Mod) fixed(a ModPR) PR {
	pr := PR{
		State:     oneOf(a.State, modStates),
		Checks:    oneOf(a.Checks, modChecks),
		Review:    oneOf(a.Review, modReviews),
		Mergeable: oneOf(a.Mergeable, modMergeables),
	}
	if a.Number > 0 && a.Number < 1e9 {
		pr.Number = a.Number
	}
	if n, ok := m.ParsePRURL(a.URL); ok && n == pr.Number {
		pr.URL = a.URL
	}
	if pr.Checks == "fail" {
		pr.Failed = min(max(a.Failed, 1), 999)
	}
	if upperRE.MatchString(a.MergeState) {
		pr.MergeState = a.MergeState
	}
	if at, err := time.Parse(time.RFC3339, a.MergedAt); err == nil && pr.State == "MERGED" && at.Year() > 2000 {
		pr.MergedAt = at.UTC()
	}
	if oidRE.MatchString(a.Head) {
		pr.Head = a.Head
	}
	if pr.State == "MERGED" && oidRE.MatchString(a.Merge) {
		pr.Merge = a.Merge
	}
	if branchRE.MatchString(a.Base) {
		pr.Base = a.Base
	}
	if branchRE.MatchString(a.Branch) {
		pr.Branch = a.Branch
	}
	return pr
}

// FailedLog asks the mod for pr's failing job and its log, cut to an
// excerpt as tm's own hosts' are.
func (m Mod) FailedLog(repo string, pr PR) (job, log string) {
	if pr.Number <= 0 {
		return "", ""
	}
	q := m.ref()
	q.Number = pr.Number
	if oidRE.MatchString(pr.Head) {
		q.Head = pr.Head
	}
	var a struct {
		Job string `json:"job"`
		Log string `json:"log"`
	}
	if err := m.call(MethodFailedLog, q, &a); err != nil || a.Job == "" {
		return "", ""
	}
	if len(a.Job) > 200 {
		a.Job = a.Job[:200]
	}
	if len(a.Log) > 1<<20 {
		a.Log = a.Log[len(a.Log)-1<<20:]
	}
	return excerptLines(clean(a.Job), strings.Split(a.Log, "\n"))
}

// PRState is the state of the PR whose head is branch.
func (m Mod) PRState(repo, branch string) string {
	if !branchRE.MatchString(branch) {
		return ""
	}
	q := m.ref()
	q.Branch = branch
	var a struct {
		State string `json:"state"`
	}
	if err := m.call(MethodPRState, q, &a); err != nil {
		return ""
	}
	return oneOf(a.State, modStates)
}

// PRHead is the state, number and head branch of the PR at url, a PR
// URL of this repo; all zero for any other.
func (m Mod) PRHead(repo, url string) (state string, number int, head string) {
	n, ok := m.ParsePRURL(url)
	if !ok {
		return "", 0, ""
	}
	q := m.ref()
	q.URL, q.Number = url, n
	var a struct {
		State  string `json:"state"`
		Number int    `json:"number"`
		Head   string `json:"head"`
	}
	if err := m.call(MethodPRHead, q, &a); err != nil || a.Number != n || !branchRE.MatchString(a.Head) {
		return "", 0, ""
	}
	if state = oneOf(a.State, modStates); state == "" {
		return "", 0, ""
	}
	return state, n, a.Head
}

// gitlabMergedRE finds the merge request in the message GitLab writes on
// the merge commit it makes (a merge, or a squash with a merge commit):
// "See merge request group/project!12". A fast-forward merge writes none.
var gitlabMergedRE = regexp.MustCompile(`(?m)^See merge request [^\s!]+!([0-9]{1,9})$`)

// MergeCommit is the commit that merged PR n, by git alone: for GitLab
// the merge commit naming the merge request, else as GitHub writes it.
// The mod is not asked: what feeds completing tasks and closing threads
// is tm's.
func (m Mod) MergeCommit(repo string, n int) string {
	if m.Target.Kind != GitLabKind {
		return GitHub{}.MergeCommit(repo, n)
	}
	if n <= 0 {
		return ""
	}
	return grepMerge(repo, fmt.Sprintf("^See merge request [^[:space:]!]+!%d$", n), true)
}

// MergedPR is the PR number commit's message says it merged, by git
// alone, as MergeCommit reads it.
func (m Mod) MergedPR(repo, commit string) int {
	if m.Target.Kind != GitLabKind {
		return GitHub{}.MergedPR(repo, commit)
	}
	if !commitRE.MatchString(commit) {
		return 0
	}
	body, err := git(repo, "log", "-1", "--format=%B", commit)
	if err != nil {
		return 0
	}
	ms := gitlabMergedRE.FindAllStringSubmatch(body, -1)
	if len(ms) == 0 {
		return 0
	}
	var n int
	fmt.Sscanf(ms[len(ms)-1][1], "%d", &n)
	return n
}

// Merge asks the mod to merge pr with a merge commit, only at pr.Head:
// tm thread merge has checked it is open, green and mergeable at that
// head, and the mod passes the head on to the host (glab mr merge --sha),
// which refuses it once the head moved.
func (m Mod) Merge(repo string, pr PR) error {
	if pr.Number <= 0 || !oidRE.MatchString(pr.Head) {
		return fmt.Errorf("%s: no PR number or head to merge", m.Target.Kind)
	}
	q := m.ref()
	q.Number, q.Head = pr.Number, pr.Head
	return m.call(MethodMerge, q, nil)
}

// Hints are fixed text by tm (prompts carry no mod's words): glab's
// commands for GitLab, none for another kind.
func (m Mod) Hints(n int) Hints {
	if m.Target.Kind != GitLabKind {
		return Hints{}
	}
	return Hints{
		Checks: fmt.Sprintf("glab mr view %d", n),
		Review: fmt.Sprintf("glab mr view %d --comments", n),
	}
}

// Doctor names the mod that serves the kind, then the mod's own checks
// (its CLI and login), each line cut short and cleaned.
func (m Mod) Doctor(DoctorDeps) []Check {
	head := Check{Name: m.Target.Kind, OK: true, Detail: "served by mod " + m.ID}
	var a struct {
		Checks []struct {
			Name   string `json:"name"`
			OK     bool   `json:"ok"`
			Detail string `json:"detail"`
		} `json:"checks"`
	}
	if err := m.call(MethodDoctor, m.ref(), &a); err != nil {
		var ce *CLIError
		errors.As(err, &ce)
		return []Check{head, {Name: m.Target.Kind + " mod", Detail: clip(ce.Err.Error(), 200)}}
	}
	out := []Check{head}
	for i, c := range a.Checks {
		if i == 8 {
			break
		}
		if name := clip(clean(c.Name), 40); name != "" {
			out = append(out, Check{Name: name, OK: c.OK, Detail: clip(clean(c.Detail), 200)})
		}
	}
	return out
}

// ParsePRURL is the number of one of this repo's PR URLs (OwnsPRURL);
// a kind tm knows no URLs of has none.
func (m Mod) ParsePRURL(s string) (int, bool) {
	if !OwnsPRURL(m.Target, s) {
		return 0, false
	}
	l, _ := ParsePR(s)
	return l.Number, true
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") + "…"
}

// Unserved is the Host of a code host mod's kind that no mod serves here
// (no server, no enabled mod for the project, no commands grant): every
// lookup fails with Err, which says what to do, and nothing destructive
// is ever allowed by it. Merge detection by git still works.
type Unserved struct {
	Target Target
	Err    error
}

func (u Unserved) Kind() string                              { return u.Target.Kind }
func (u Unserved) PR(string, Ref) (PR, error)                { return PR{}, u.Err }
func (Unserved) FailedLog(string, PR) (job, log string)      { return "", "" }
func (Unserved) PRState(string, string) string               { return "" }
func (Unserved) PRHead(string, string) (string, int, string) { return "", 0, "" }
func (u Unserved) MergeCommit(repo string, n int) string {
	return Mod{Target: u.Target}.MergeCommit(repo, n)
}
func (u Unserved) MergedPR(repo, commit string) int {
	return Mod{Target: u.Target}.MergedPR(repo, commit)
}
func (u Unserved) Merge(string, PR) error          { return u.Err }
func (u Unserved) Hints(n int) Hints               { return Mod{Target: u.Target}.Hints(n) }
func (u Unserved) ParsePRURL(s string) (int, bool) { return Mod{Target: u.Target}.ParsePRURL(s) }
func (u Unserved) Doctor(DoctorDeps) []Check {
	return []Check{{Name: u.Target.Kind, Detail: clip(u.Err.Error(), 200)}}
}

// External says whether h is served by a mod: its word that a PR merged,
// or that a commit is a merged PR's head, never lets tm remove a branch
// or a worktree by itself. Git must find the commits on the remote.
func External(h Host) bool {
	switch h.(type) {
	case Mod, Unserved:
		return true
	}
	return false
}
