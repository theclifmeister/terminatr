// Package codehost is where a repo's pull requests live (docs/SPEC.md
// §7.5): GitHub through gh, Azure DevOps through az rest. The
// ticker, resolve and doctor ask a Host, picked per repo from its origin
// URL (Pick), so the rest of tm keeps one set of fixed words for a PR
// (PR) whatever the host. It imports no other tm package, so worktree,
// ticker and doctor can all use it.
package codehost

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Host kinds (Host.Kind, Config.CodeHost).
const (
	GitHubKind = "github"
	AzureKind  = "azure"
	// GitLabKind is GitLab's: no host of tm's own serves it, a code
	// host mod does (Mod). tm knows its PR URLs (ParsePR) and the merge
	// commits it writes (MergeCommit), so neither is the mod's word.
	GitLabKind = "gitlab"
	// NoKind is a repo with no PR host to ask: no origin, a local one,
	// or a well-known host that isn't GitHub or Azure DevOps.
	NoKind = ""
)

// ErrNoPR means the host works but has no PR for what was asked. Errors
// that wrap exec.ErrNotFound mean its CLI isn't installed (tm doctor
// says so); every other error is a failed call.
var ErrNoPR = errors.New("no pull request found")

// ErrNoRef means there was nothing to look a PR up by: no PR URL of this
// host and no usable branch. Nothing ran.
var ErrNoRef = errors.New("nothing to look a pull request up by")

// Ref names a PR to look up: by Number when set, else by URL when it is
// one of this host's PR URLs, else by the Branch it comes from.
type Ref struct {
	URL    string
	Branch string
	Number int
}

// Hints are the commands a prompt names for reading PR n's checks and
// its review (fixed text with the number).
type Hints struct {
	Checks string // `gh pr checks 12`
	Review string // `gh pr view 12 --comments`
}

// Check is one tm doctor line about a host's tooling; the doctor
// package puts it in its toolchain group.
type Check struct {
	Name   string
	OK     bool // false is a warning
	Detail string
}

// DoctorDeps is what Doctor may touch: doctor.Deps' LookPath and Run,
// the environment (nil reads none) and Azure DevOps with a PAT (nil is
// PATGet).
type DoctorDeps struct {
	LookPath func(string) (string, error)
	Run      func(dir, name string, args ...string) (string, error)
	Getenv   func(string) string
	PATGet   func(url, pat string) ([]byte, error)
}

// Host is one pull-request provider. Every method takes the repo (a
// checkout of it) the PR belongs to.
type Host interface {
	// Kind is GitHubKind, AzureKind, or a code host mod's kind.
	Kind() string
	// PR looks a PR up. ErrNoRef when ref names nothing, ErrNoPR when the
	// host has none; other errors are failures (see ErrNoPR).
	PR(repo string, ref Ref) (PR, error)
	// FailedLog is the failing CI job of pr's head and an excerpt of its
	// log, "" when there is none or it can't be had.
	FailedLog(repo string, pr PR) (job, log string)
	// PRState is the state ("MERGED", "OPEN", "CLOSED") of the PR whose
	// head is branch, "" when there is none or the host can't tell.
	PRState(repo, branch string) string
	// PRHead is the state, number and head branch of the PR at url; all
	// zero when the host can't tell.
	PRHead(repo, url string) (state string, number int, head string)
	// MergeCommit is the commit that merged PR n into the default branch
	// as last fetched, by git alone; "" for none.
	MergeCommit(repo string, n int) string
	// MergedPR is the PR number commit's subject says it merged, 0 for
	// none, by git alone.
	MergedPR(repo, commit string) int
	// Merge merges pr (open, as PR just read it) with a regular merge
	// commit, only if its head is still pr.Head: never a squash, a
	// rebase or a forced one. It checks nothing else; tm thread merge
	// does (docs/SPEC.md §9).
	Merge(repo string, pr PR) error
	// Hints are the commands prompts name for PR n.
	Hints(n int) Hints
	// Doctor checks the host's CLI and its login.
	Doctor(d DoctorDeps) []Check
	// ParsePRURL is the number of one of this host's PR URLs.
	ParsePRURL(s string) (n int, ok bool)
}

// Config is a project's override of the host (PROJECT.md code_host and
// azure_url, docs/SPEC.md §5.1). Zero picks by the origin URL.
type Config struct {
	// CodeHost is GitHubKind, AzureKind, "" (by origin), or the kind a
	// code host mod provides ("gitlab", ModKind).
	CodeHost string
	// AzureURL is the Azure DevOps organization (or collection) URL, for
	// a server whose host Detect doesn't know.
	AzureURL string
	// Project is the project's slug: a code host mod serves the projects
	// it runs for.
	Project string
}

// Target is what Detect makes of a repo: its host kind and, for Azure
// DevOps, where the repo is.
type Target struct {
	Kind    string
	OrgURL  string // https://dev.azure.com/org, https://org.visualstudio.com; GitLab's https://host
	Project string
	Repo    string // GitLab's project path, group/subgroup/project
}

// Detect tells which host repo's PRs live on, from cfg, else its origin
// URL (ParseRemote); GitHub when neither says Azure DevOps, unless the
// repo has no origin or one on a host with no PRs to poll (NoKind). A
// code host mod's kind in cfg is taken as it is, with the server and
// project path of the origin (modRemote).
func Detect(repo string, cfg Config) Target {
	url := origin(repo)
	if ModKind(cfg.CodeHost) {
		t, _ := modRemote(url)
		t.Kind = cfg.CodeHost
		return t
	}
	switch cfg.CodeHost {
	case GitHubKind:
		return Target{Kind: GitHubKind}
	case AzureKind:
		t, _ := ParseRemote(url)
		t.Kind = AzureKind
		if cfg.AzureURL != "" {
			t.OrgURL = strings.TrimRight(cfg.AzureURL, "/")
			if t.Project == "" {
				t.Project, t.Repo = gitPath(url)
			}
		}
		return t
	}
	if t, ok := ParseRemote(url); ok {
		return t
	}
	if !pollable(url) {
		return Target{Kind: NoKind}
	}
	return Target{Kind: GitHubKind}
}

// otherHosts are well-known git hosts that are neither GitHub nor Azure
// DevOps. Any other host may be a GitHub Enterprise server, which gh asks.
var otherHosts = map[string]bool{
	"gitlab.com": true, "bitbucket.org": true, "codeberg.org": true,
	"git.sr.ht": true, "gitea.com": true, "gitee.com": true,
}

// pollable is whether an origin URL could be a GitHub's: false for none,
// a local path or file:// URL, and a host in otherHosts.
func pollable(url string) bool {
	if strings.HasPrefix(strings.TrimSpace(url), "file://") {
		return false
	}
	host, _, ok := splitRemote(url)
	return ok && !otherHosts[host]
}

// Pick is the Host for repo (Detect). A code host mod's kind gets that
// mod (Provide), or a host that says why there is none (unserved).
func Pick(repo string, cfg Config) Host {
	t := Detect(repo, cfg)
	if ModKind(t.Kind) {
		return pickMod(t, cfg.Project)
	}
	if t.Kind == NoKind {
		return NoHost{}
	}
	if t.Kind == AzureKind {
		return Azure{Target: t}
	}
	return GitHub{}
}

// origin is repo's origin URL as configured, "" for none: before any
// url.<base>.insteadOf rewrite, which says how to reach the repo, not
// whose PRs it has.
//
// It is kept per repo while the repo's .git/config is the same file with
// the same mtime and size, for at most originTTL, so the dashboards that
// ask every second fork no git while nothing changes. A repo whose .git
// is not a folder (a linked worktree) asks git every time.
func origin(repo string) string {
	if repo == "" {
		return ""
	}
	fi, err := os.Stat(filepath.Join(repo, ".git", "config"))
	if err == nil {
		origins.Lock()
		o, ok := origins.m[repo]
		origins.Unlock()
		if ok && os.SameFile(o.fi, fi) && o.fi.ModTime().Equal(fi.ModTime()) && o.fi.Size() == fi.Size() &&
			time.Since(o.at) < originTTL {
			return o.url
		}
	}
	out, gerr := git(repo, "config", "--get", "remote.origin.url")
	if gerr != nil {
		out = ""
	}
	if err == nil {
		origins.Lock()
		origins.m[repo] = originOf{url: out, fi: fi, at: time.Now()}
		origins.Unlock()
	}
	return out
}

// origins is origin's cache, by repo.
var origins = struct {
	sync.Mutex
	m map[string]originOf
}{m: map[string]originOf{}}

// originTTL bounds how long origin keeps a URL: a rewrite of
// .git/config its stat can't tell apart still shows.
const originTTL = time.Minute

// originOf is a repo's origin URL, the .git/config it was read from and
// when.
type originOf struct {
	url string
	fi  os.FileInfo
	at  time.Time
}

// gitTimeout bounds git: local reads only, but a log --grep that finds
// nothing walks all of a big repo's history.
var gitTimeout = 30 * time.Second

// git runs git in dir, for at most gitTimeout, and returns its trimmed
// stdout.
func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// Advice is what to tell the user about a code host call that failed
// with err, in fixed words: the CLI that failed ("gh", "az"), what went
// wrong ("" when tm can't tell) and what the user does about it.
func Advice(err error) (cli, problem, advice string) {
	var ce *CLIError
	if errors.As(err, &ce) {
		cli, problem, advice = ce.CLI, ce.Problem, ce.Advice
	}
	switch {
	case cli == "":
		cli = "gh"
	case !cliRE.MatchString(cli):
		cli, problem, advice = "the code host CLI", "", ""
	}
	if advice == "" {
		advice = "the user checks " + cli + " auth status in a terminal (tm doctor)"
		if cli == "az" {
			advice = "the user checks az login in a terminal (tm doctor)"
		}
	}
	return cli, problem, advice
}

var cliRE = regexp.MustCompile(`^[a-z]{1,8}$`)
