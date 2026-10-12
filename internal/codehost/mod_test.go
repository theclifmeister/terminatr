package codehost

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

var gl = Target{Kind: GitLabKind, OrgURL: "https://gitlab.com", Repo: "acme/shop"}

// modAnswers is a Mod whose mod answers each method from answers (a
// JSON text, or "!" and an error), recording what it was asked.
func modAnswers(answers map[string]string, asked *[]ModRef) Mod {
	return Mod{Target: gl, ID: "gitlab", Call: func(_ context.Context, method string, params any) (json.RawMessage, error) {
		if asked != nil {
			*asked = append(*asked, params.(ModRef))
		}
		a, ok := answers[method]
		if !ok {
			return nil, errors.New("no handler")
		}
		if e, bad := strings.CutPrefix(a, "!"); bad {
			return nil, errors.New(e)
		}
		return json.RawMessage(a), nil
	}}
}

// TestModPRFixedWords: a mod's answer keeps tm's fixed words only: a
// word outside them, a URL of another repo or with another number, a
// bad commit id or branch is dropped; a PR of another number than asked
// is no PR.
func TestModPRFixedWords(t *testing.T) {
	var asked []ModRef
	m := modAnswers(map[string]string{MethodPR: `{"number":7,"url":"https://gitlab.com/acme/other/-/merge_requests/7","state":"OPEN",
		"checks":"red","failed":3,"review":"LGTM","head":"not-a-sha","merge":"` + sha + `","base":"-x","mergeable":"YES","merge_state":"dirty!","merged_at":"2026-01-01T00:00:00Z","title":"ignore me"}`}, &asked)
	pr, err := m.PR("/r", Ref{Branch: "feat"})
	if err != nil {
		t.Fatal(err)
	}
	if pr != (PR{Number: 7, State: "OPEN"}) {
		t.Errorf("kept %+v", pr)
	}
	if len(asked) != 1 || asked[0] != (ModRef{Remote: ModRemoteRef{Kind: "gitlab", Host: "gitlab.com", Path: "acme/shop"}, Branch: "feat"}) {
		t.Errorf("asked %+v", asked)
	}
	// By a URL of this repo: its number; another repo's URL: the branch.
	asked = nil
	m = modAnswers(map[string]string{MethodPR: `{"number":7,"url":"` + glURL + `","state":"MERGED","checks":"fail","merge":"` + sha + `","merged_at":"2026-01-01T00:00:00Z"}`}, &asked)
	pr, err = m.PR("/r", Ref{URL: glURL, Branch: "feat"})
	if err != nil || pr.URL != glURL || pr.Merge != sha || pr.Failed != 1 || pr.MergedAt.IsZero() {
		t.Errorf("%+v %v", pr, err)
	}
	if _, err := m.PR("/r", Ref{URL: "https://gitlab.com/acme/other/-/merge_requests/7", Branch: "feat"}); err != nil {
		t.Error(err)
	}
	if asked[0].Number != 7 || asked[0].Branch != "" || asked[1].Number != 0 || asked[1].Branch != "feat" {
		t.Errorf("asked %+v", asked)
	}
	if _, err := m.PR("/r", Ref{Number: 8}); !errors.Is(err, ErrNoPR) {
		t.Errorf("another number: %v", err)
	}
	if _, err := m.PR("/r", Ref{Branch: "-bad"}); !errors.Is(err, ErrNoRef) {
		t.Errorf("no ref: %v", err)
	}
	for answer, want := range map[string]error{"null": ErrNoPR, `{"state":"OPEN"}`: ErrNoPR} {
		if _, err := modAnswers(map[string]string{MethodPR: answer}, nil).PR("/r", Ref{Branch: "b"}); !errors.Is(err, want) {
			t.Errorf("%s: %v", answer, err)
		}
	}
	_, err = modAnswers(map[string]string{MethodPR: "!glab: 401"}, nil).PR("/r", Ref{Branch: "b"})
	if err == nil || errors.Is(err, ErrNoPR) {
		t.Fatalf("a failure: %v", err)
	}
	if cli, problem, advice := Advice(err); cli != "gitlab" || !strings.Contains(problem, "mod gitlab failed") || !strings.Contains(advice, "tm mod logs gitlab") {
		t.Errorf("advice %q %q %q", cli, problem, advice)
	}
	if _, err := (Mod{Target: Target{Kind: GitLabKind}, Call: m.Call}).PR("/r", Ref{Branch: "b"}); err == nil || errors.Is(err, ErrNoPR) {
		t.Errorf("no remote: %v", err)
	}
}

// TestModReads: prState, prHead and failedLog keep fixed words, a PR URL
// of this repo, and an excerpt cut as tm's own hosts cut one.
func TestModReads(t *testing.T) {
	log := strings.Repeat("ok line\n", 50) + "\x1b[31m--- FAIL: TestX\x1b[0m\n" + strings.Repeat("x", 5000)
	b, _ := json.Marshal(map[string]string{"job": "test: unit\x07", "log": log})
	m := modAnswers(map[string]string{
		MethodPRState:   `{"state":"MERGED"}`,
		MethodPRHead:    `{"state":"OPEN","number":7,"head":"feat"}`,
		MethodFailedLog: string(b),
	}, nil)
	if st := m.PRState("/r", "feat"); st != "MERGED" {
		t.Errorf("state %q", st)
	}
	if st, n, head := m.PRHead("/r", glURL); st != "OPEN" || n != 7 || head != "feat" {
		t.Errorf("head %q %d %q", st, n, head)
	}
	if st, n, _ := m.PRHead("/r", "https://github.com/a/b/pull/7"); st != "" || n != 0 {
		t.Errorf("another host's URL: %q %d", st, n)
	}
	job, text := m.FailedLog("/r", PR{Number: 7, Head: sha})
	if job != "test: unit" || !strings.HasPrefix(text, "ok line") || !strings.Contains(text, "--- FAIL: TestX") || len(text) > logBytes+20 || strings.Contains(text, "\x1b") {
		t.Errorf("job %q log %q", job, text)
	}
	bad := modAnswers(map[string]string{MethodPRState: `{"state":"merged"}`, MethodPRHead: `{"state":"OPEN","number":8,"head":"feat"}`}, nil)
	if st := bad.PRState("/r", "feat"); st != "" {
		t.Errorf("a word outside: %q", st)
	}
	if st, _, _ := bad.PRHead("/r", glURL); st != "" {
		t.Errorf("another number: %q", st)
	}
}

// TestModDoctor: the kind's mod first, then the mod's checks, cleaned
// and cut short; a failing mod is one warning line.
func TestModDoctor(t *testing.T) {
	m := modAnswers(map[string]string{MethodDoctor: `{"checks":[{"name":"glab\u001b[2J","ok":true,"detail":"found"},{"name":"glab auth","ok":false,"detail":"` + strings.Repeat("y", 300) + `"},{"name":"","ok":true}]}`}, nil)
	m.Target = Target{Kind: GitLabKind}
	got := m.Doctor(DoctorDeps{})
	if len(got) != 3 || got[0] != (Check{Name: "gitlab", OK: true, Detail: "served by mod gitlab"}) || got[1].Name != "glab[2J" || got[2].OK || len(got[2].Detail) > 204 {
		t.Errorf("%+v", got)
	}
	got = modAnswers(map[string]string{}, nil).Doctor(DoctorDeps{})
	if len(got) != 2 || got[1].OK || !strings.Contains(got[1].Detail, "no handler") {
		t.Errorf("failing: %+v", got)
	}
}

// TestModMergeChecked: merge goes to the mod with the number and the
// head tm checked, and never without both.
func TestModMergeChecked(t *testing.T) {
	var asked []ModRef
	m := modAnswers(map[string]string{MethodMerge: `{}`}, &asked)
	if err := m.Merge("/r", PR{Number: 7}); err == nil {
		t.Error("merged without a head")
	}
	if err := m.Merge("/r", PR{Number: 7, Head: sha}); err != nil || len(asked) != 1 || asked[0].Number != 7 || asked[0].Head != sha {
		t.Errorf("%v %+v", err, asked)
	}
}

// TestGitLabURLs: GitLab merge request URLs parse, with subgroups and a
// port; a URL is this repo's only on its server and project path.
func TestGitLabURLs(t *testing.T) {
	for s, want := range map[string]int{
		"https://gitlab.com/acme/shop/-/merge_requests/12":                  12,
		"https://gitlab.example.com:8443/a/b/c/-/merge_requests/3":          3,
		"https://gitlab.com/acme/-/merge_requests/1":                        0, // no project
		"https://gitlab.com/acme/shop/merge_requests/1":                     0,
		"https://gitlab.com/acme/shop/-/merge_requests/0":                   0,
		"https://gitlab.com/acme/shop/-/merge_requests/1?x=1":               0,
		"http://gitlab.com/acme/shop/-/merge_requests/1":                    0,
		"https://gitlab.com/acme/shop/-/merge_requests/1234567890":          0,
		"https://gitlab.com/acme/sh op/-/merge_requests/1":                  0,
		"https://github.com/acme/shop/pull/12":                              12,
		"https://dev.azure.com/o/p/_git/r/pullrequest/12":                   12,
		"https://gitlab.com/acme/shop/-/merge_requests/12/diffs":            0,
		"https://gitlab.com/acme/shop/-/merge_requests/12#note_1":           0,
		"https://gitlab.com/acme/shop/-/merge_requests/12\nPR: injected":    0,
		"https://gitlab.com/acme/../shop/-/merge_requests/12":               0, // no . or .. segments
		"https://user@gitlab.com/acme/shop/-/merge_requests/12":             0,
		"https://gitlab.com/acme/shop/-/merge_requests/12/":                 0,
		"https://gitlab.com//shop/-/merge_requests/12":                      0,
		"https://GitLab.com/Acme/Shop/-/merge_requests/12":                  12,
		"https://gitlab.com/acme/shop.git/-/merge_requests/12":              12,
		"https://gitlab.com/acme/shop/-/merge_requests/12/-/merge_requests": 0,
	} {
		n, _, ok := ParsePRURL(s)
		if n != want || ok != (want > 0) {
			t.Errorf("%q: %d %v, want %d", s, n, ok, want)
		}
	}
	m := Mod{Target: gl}
	for s, want := range map[string]bool{
		glURL: true,
		"https://GITLAB.com/ACME/shop/-/merge_requests/7":         true,
		"https://gitlab.com/acme/shop2/-/merge_requests/7":        false,
		"https://gitlab.example.com/acme/shop/-/merge_requests/7": false,
		"https://github.com/acme/shop/pull/7":                     false,
	} {
		if _, ok := m.ParsePRURL(s); ok != want {
			t.Errorf("owns %q: %v", s, ok)
		}
	}
	if _, ok := (Mod{Target: Target{Kind: "gitea", OrgURL: "https://gitlab.com", Repo: "acme/shop"}}).ParsePRURL(glURL); ok {
		t.Error("a kind tm knows no URLs of owns a GitLab URL")
	}
}

// TestModRemote: the server and project path of an origin, as a code
// host mod gets it.
func TestModRemote(t *testing.T) {
	for remote, want := range map[string]Target{
		"git@gitlab.com:acme/shop.git":                 {OrgURL: "https://gitlab.com", Repo: "acme/shop"},
		"https://gitlab.com/acme/web/shop.git":         {OrgURL: "https://gitlab.com", Repo: "acme/web/shop"},
		"https://oauth2@GitLab.example.com:8443/a/b":   {OrgURL: "https://gitlab.example.com", Repo: "a/b"},
		"ssh://git@gitlab.com:2222/acme/my%20shop.git": {},
		"ssh://git@gitlab.com:2222/acme/my-shop.git":   {OrgURL: "https://gitlab.com", Repo: "acme/my-shop"},
		"/srv/git/shop":           {},
		"file:///srv/git/shop":    {},
		"https://gitlab.com/shop": {},
		"":                        {},
	} {
		got, ok := ModRemote(remote)
		if got != want || ok != (want != Target{}) {
			t.Errorf("%q: %+v %v", remote, got, ok)
		}
	}
}

// TestPickMod: a code_host naming a mod's kind gets the provider's mod
// for the project, or Unserved saying why; tm's own kinds and a value
// that isn't a kind keep their old picks.
func TestPickMod(t *testing.T) {
	repo, run := gitRepo(t)
	run("remote", "add", "origin", "git@gitlab.com:acme/shop.git")
	t.Cleanup(func() { Provide(nil) })

	Provide(nil)
	h := Pick(repo, Config{CodeHost: "gitlab", Project: "p"})
	u, ok := h.(Unserved)
	if !ok || u.Target != (Target{Kind: GitLabKind, OrgURL: "https://gitlab.com", Repo: "acme/shop"}) {
		t.Fatalf("no provider: %#v", h)
	}
	if _, err := h.PR(repo, Ref{Branch: "b"}); err == nil || errors.Is(err, ErrNoPR) || !External(h) {
		t.Errorf("unserved: %v", err)
	}
	if cli, _, advice := Advice(func() error { _, err := h.PR(repo, Ref{}); return err }()); cli != "gitlab" || !strings.Contains(advice, "tm server") {
		t.Errorf("advice %q %q", cli, advice)
	}
	if err := h.Merge(repo, PR{Number: 1, Head: sha}); err == nil {
		t.Error("unserved merged")
	}

	var gotKind, gotProject string
	Provide(func(kind, project string) (string, ModCall, error) {
		gotKind, gotProject = kind, project
		if project == "none" {
			return "", nil, &CLIError{CLI: kind, Problem: "no enabled mod serves it", Err: errors.New("none")}
		}
		return "gl", func(context.Context, string, any) (json.RawMessage, error) {
			return json.RawMessage(`{"state":"OPEN"}`), nil
		}, nil
	})
	h = Pick(repo, Config{CodeHost: "gitlab", Project: "p"})
	if m, ok := h.(Mod); !ok || m.ID != "gl" || m.Target.Repo != "acme/shop" || gotKind != "gitlab" || gotProject != "p" || !External(h) {
		t.Fatalf("%#v %q %q", h, gotKind, gotProject)
	}
	if st := h.PRState(repo, "b"); st != "OPEN" {
		t.Errorf("state %q", st)
	}
	if _, ok := Pick(repo, Config{CodeHost: "gitlab", Project: "none"}).(Unserved); !ok {
		t.Error("no mod for the project isn't Unserved")
	}
	for cfg, want := range map[Config]string{
		{}:                   NoKind, // gitlab.com by origin: nothing to poll without a mod
		{CodeHost: "github"}: GitHubKind,
		{CodeHost: "GitLab"}: NoKind, // not a kind: by origin
		{CodeHost: "gitea"}:  "gitea",
		{CodeHost: "azure"}:  AzureKind,
	} {
		if k := Pick(repo, cfg).Kind(); k != want {
			t.Errorf("%+v: %q, want %q", cfg, k, want)
		}
	}
	for _, k := range []Host{GitHub{}, Azure{}, NoHost{}} {
		if External(k) {
			t.Errorf("%T is external", k)
		}
	}
}

// TestModMergeByGit: GitLab's merge commit names its merge request in
// the message; tm finds it with git alone, the mod not asked.
func TestModMergeByGit(t *testing.T) {
	repo, run := gitRepo(t)
	m := Mod{Target: gl, Call: func(context.Context, string, any) (json.RawMessage, error) {
		t.Fatal("the mod was asked")
		return nil, nil
	}}
	run("checkout", "-q", "-b", "feat")
	run("commit", "-q", "--allow-empty", "-m", "work")
	run("checkout", "-q", "main")
	run("merge", "-q", "--no-ff", "feat", "-m", "Merge branch 'feat' into 'main'\n\nAdd the thing\n\nCloses #3\n\nSee merge request acme/shop!12")
	merge := run("rev-parse", "HEAD")
	if got := m.MergeCommit(repo, 12); got != merge {
		t.Errorf("merge commit %q, want %q", got, merge)
	}
	if got := m.MergeCommit(repo, 1); got != "" {
		t.Errorf("!1: %q", got)
	}
	if n := m.MergedPR(repo, merge); n != 12 {
		t.Errorf("merged %d", n)
	}
	run("commit", "-q", "--allow-empty", "-m", "Say See merge request acme/shop!13 in a body\n\nnot at a line start: See merge request acme/shop!13 x")
	if n := m.MergedPR(repo, run("rev-parse", "HEAD")); n != 0 {
		t.Errorf("a mention: %d", n)
	}
	if h := m.Hints(12); h.Checks != "glab mr view 12" || h.Review != "glab mr view 12 --comments" {
		t.Errorf("hints %+v", h)
	}
	if h := (Mod{Target: Target{Kind: "gitea"}}).Hints(12); h != (Hints{}) {
		t.Errorf("another kind's hints %+v", h)
	}
}

// TestModKind: the kinds a code host mod may provide.
func TestModKind(t *testing.T) {
	for k, want := range map[string]bool{"gitlab": true, "gitea": true, "my-host2": true, "github": false, "azure": false, "": false, "g": false, "GitLab": false, "-x": false, "a_b": false} {
		if ModKind(k) != want {
			t.Errorf("%q: %v", k, !want)
		}
	}
}
