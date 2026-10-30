package codehost

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// build is one Build of shop's repo for the build-check tests.
func build(id, def int, ref, commit, status, result string) string {
	return fmt.Sprintf(`{"id":%d,"status":%q,"result":%q,"sourceBranch":%q,"sourceVersion":%q,"definition":{"id":%d},"repository":{"id":"3411ebc1-d5aa-464f-9615-0b527bc66719","name":"web","type":"TfsGit"}}`,
		id, status, result, ref, commit, def)
}

// TestAzureBranchBuilds (T877): a pipeline a push to the PR's branch
// triggers, with no build validation policy, shows as the PR's checks,
// as captured live (testdata/azure/README.md); a build policy's build of
// the merge ref isn't counted twice.
func TestAzureBranchBuilds(t *testing.T) {
	// PR 7: no policies, no statuses; its branch's build failed at its
	// head.
	pr, err := azurePR(shop, fixture(t, "pr-branch-ci.json"), []byte(`{"count":0,"value":[]}`), []byte(`{"count":0,"value":[]}`),
		fixture(t, "builds-branch.json"), []byte(`{"count":0,"value":[]}`))
	if err != nil || pr.Checks != "fail" || pr.Failed != 1 || pr.Branch != "feature/ci" {
		t.Fatalf("branch build: %+v %v", pr, err)
	}
	// Before the fix: nothing.
	if pr, _ := azurePR(shop, fixture(t, "pr-branch-ci.json"), nil, nil); pr.Checks != "" {
		t.Fatalf("no builds: %+v", pr)
	}
	// PR 8: a build policy approved, naming build 7 of the merge ref.
	pr, err = azurePR(shop, fixture(t, "pr-policy-ci.json"), fixture(t, "policies-ci.json"), nil,
		[]byte(`{"count":0,"value":[]}`), fixture(t, "builds-merge.json"))
	if err != nil || pr.Checks != "pass" || pr.Failed != 0 {
		t.Fatalf("policy build: %+v %v", pr, err)
	}
	// The policy approved, a second pipeline of the merge ref failed:
	// one failure.
	merge := "c9018378bf768cbd37d35cc5ccccbeaf65020ca8"
	pr, _ = azurePR(shop, fixture(t, "pr-policy-ci.json"), fixture(t, "policies-ci.json"), nil,
		[]byte("["+build(7, 2, "refs/pull/8/merge", merge, "completed", "succeeded")+","+build(9, 3, "refs/pull/8/merge", merge, "completed", "failed")+"]"))
	if pr.Checks != "fail" || pr.Failed != 1 {
		t.Fatalf("second merge pipeline: %+v", pr)
	}

	head := "e2c76a6dde8bc3ad9f9452ba4d2a956f16194542"
	br := "refs/heads/feature/ci"
	list := func(b ...string) []byte { return []byte(`{"count":1,"value":[` + strings.Join(b, ",") + `]}`) }
	for _, c := range []struct {
		name   string
		builds []byte
		checks string
		failed int
	}{
		{"running", list(build(8, 2, br, head, "inProgress", "")), "pending", 0},
		{"not started", list(build(8, 2, br, head, "notStarted", "")), "pending", 0},
		{"succeeded", list(build(8, 2, br, head, "completed", "succeeded")), "pass", 0},
		{"partially succeeded", list(build(8, 2, br, head, "completed", "partiallySucceeded")), "pass", 0},
		{"canceled", list(build(8, 2, br, head, "completed", "canceled")), "", 0},
		// an older head's build is not the PR's
		{"old head", list(build(8, 2, br, strings.Repeat("ab", 20), "completed", "failed")), "", 0},
		// the newest of a pipeline counts: rerun and passed
		{"rerun", list(build(9, 2, br, head, "completed", "succeeded"), build(8, 2, br, head, "completed", "failed")), "pass", 0},
		// two pipelines
		{"two pipelines", list(build(9, 2, br, head, "completed", "failed"), build(8, 3, br, head, "completed", "failed")), "fail", 2},
		// another repo's branch of the same name
		{"other repo", list(strings.Replace(build(8, 2, br, head, "completed", "failed"), `"id":"3411ebc1-d5aa-464f-9615-0b527bc66719"`, `"id":"00000000-0000-0000-0000-000000000001"`, 1)), "", 0},
		{"garbage", []byte(`{"value":[1,"x",{"id":-1}]}`), "", 0},
	} {
		pr, err := azurePR(shop, fixture(t, "pr-branch-ci.json"), nil, nil, c.builds)
		if err != nil || pr.Checks != c.checks || pr.Failed != c.failed {
			t.Errorf("%s: %+v %v", c.name, pr, err)
		}
	}
}

// TestAzurePRBuildsAsked: an open PR's poll asks its branch's builds
// and its merge ref's, and the PR shows them; builds failing don't fail
// the poll.
func TestAzurePRBuildsAsked(t *testing.T) {
	f := &azFake{answers: map[string]string{
		"prs":      `{"count":1,"value":[` + string(fixture(t, "pr-branch-ci.json")) + `]}`,
		"policies": `{"count":0,"value":[]}`,
		"statuses": `{"count":0,"value":[]}`,
		"builds":   string(fixture(t, "builds-branch.json")),
	}}
	a := f.host()
	pr, err := a.PR("/r", Ref{Branch: "feature/ci"})
	if err != nil || pr.Number != 7 || pr.Checks != "fail" {
		t.Fatalf("%+v %v", pr, err)
	}
	f.answers["builds"] = "!ERROR: Forbidden"
	if pr, err := a.PR("/r", Ref{Branch: "feature/ci"}); err != nil || pr.Number != 7 || pr.Checks != "" {
		t.Fatalf("builds failing: %+v %v", pr, err)
	}
}

// TestAzureNoPRWhy (T877): no PR says why, in fixed words naming the
// repo asked and the one answered; az failing is no "no PR".
func TestAzureNoPRWhy(t *testing.T) {
	f := &azFake{answers: map[string]string{"prs": `{"count":0,"value":[]}`}}
	a := f.host()
	why := func(ref Ref) (string, error) {
		_, err := a.PR("/r", ref)
		return NoPRWhy(err), err
	}
	if w, err := why(Ref{Branch: "feature/x"}); !errors.Is(err, ErrNoPR) || w != "Azure DevOps has no PR from branch feature/x in Shop/web" {
		t.Errorf("none: %q %v", w, err)
	}
	f.answers["pr"] = strings.Replace(string(fixture(t, "pr-active.json")), `"name": "web"`, `"name": "api"`, 1)
	if w, err := why(Ref{Number: 1}); !errors.Is(err, ErrNoPR) || w != "PR 1 is of Shop/api, not of Shop/web, the repo's origin" {
		t.Errorf("other repo: %q %v", w, err)
	}
	f.answers["prs"] = `[` + f.answers["pr"] + `]`
	if w, _ := why(Ref{Branch: "feature/pass"}); w != "PR 1 is of Shop/api, not of Shop/web, the repo's origin" {
		t.Errorf("other repo by branch: %q", w)
	}
	// a name that isn't plain isn't shown
	f.answers["pr"] = strings.Replace(string(fixture(t, "pr-active.json")), `"name": "web"`, `"name": "a\u001b[31mb"`, 1)
	if w, _ := why(Ref{Number: 1}); w != "PR 1 is of another repo, not of Shop/web, the repo's origin" {
		t.Errorf("odd name: %q", w)
	}
	f.answers["pr"] = `!ERROR: Not Found({"message":"TF401180: The requested pull request was not found."})`
	if w, err := why(Ref{Number: 4}); !errors.Is(err, ErrNoPR) || w != "Azure DevOps has no PR 4 in Shop/web" || !strings.Contains(err.Error(), "TF401180") {
		t.Errorf("not found: %q %v", w, err)
	}
	f.answers["prs"] = "!ERROR: Please run 'az login' to setup account."
	if w, err := why(Ref{Branch: "b"}); errors.Is(err, ErrNoPR) || w != "az failed: az is not logged in" {
		t.Errorf("logged out: %q %v", w, err)
	}
	f.answers["prs"] = "!ERROR: TF400813: The user '' is not authorized to access this resource."
	if w, _ := why(Ref{Branch: "b"}); w != "az failed: Azure DevOps doesn't know the user az signs in as (TF400813)" {
		t.Errorf("TF400813: %q", w)
	}
	f.answers["prs"] = "!something odd"
	if w, _ := why(Ref{Branch: "b"}); w != "az failed" {
		t.Errorf("unknown failure: %q", w)
	}
	if w, _ := why(Ref{}); w != "" {
		t.Errorf("no ref: %q", w)
	}
	if w := NoPRWhy(fmt.Errorf("%w: gh said so", ErrNoPR)); w != "no PR from its branch" {
		t.Errorf("plain ErrNoPR: %q", w)
	}
}

// TestAzureFailedLogBranch (T877): a failed build of the PR's branch at
// its head gives the log too; a build of an older head doesn't.
func TestAzureFailedLogBranch(t *testing.T) {
	head := "e2c76a6dde8bc3ad9f9452ba4d2a956f16194542"
	for _, c := range []struct {
		name, builds string
		want         int
	}{
		{"branch build", "[" + build(8, 2, "refs/heads/feature/ci", head, "completed", "failed") + "]", 8},
		{"old head", "[" + build(8, 2, "refs/heads/feature/ci", strings.Repeat("ab", 20), "completed", "failed") + "]", 0},
	} {
		var asked []string
		a := buildLogHost(t, "", &asked)
		run := a.Run
		a.Run = func(dir string, args ...string) ([]byte, error) {
			if route, u := azRoute(args); route == "builds" {
				asked = append(asked, u)
				if strings.Contains(u, "refs%2Fheads%2F") {
					return []byte(c.builds), nil
				}
				return []byte(`{"count":0,"value":[]}`), nil
			}
			return run(dir, args...)
		}
		job, _ := a.FailedLog("/repo", PR{Number: 7, Branch: "feature/ci", Head: head})
		b := "https://dev.azure.com/acme/Shop/_apis/build/builds"
		if !slices.Contains(asked, b+"?branchName=refs%2Fheads%2Ffeature%2Fci&resultFilter=failed&queryOrder=queueTimeDescending&$top=20&api-version=7.1") {
			t.Errorf("%s: asked %q", c.name, asked)
		}
		if got := slices.Contains(asked, fmt.Sprintf("%s/%d/timeline?api-version=7.1", b, c.want)); (c.want > 0) != got || (c.want > 0) != (job != "") {
			t.Errorf("%s: job %q, asked %q", c.name, job, asked)
		}
	}
}

// TestAzureInvokeArgs: every URL tm asks maps onto az devops invoke's
// area, resource and parameters, as the extension picks a location
// (its first of the highest version: the project's for pullRequests).
func TestAzureInvokeArgs(t *testing.T) {
	a := Azure{Target: shop}
	b := "https://dev.azure.com/acme/Shop/_apis/build/builds"
	pre := "devops invoke --org https://dev.azure.com/acme "
	for u, want := range map[string]string{
		shopRepo + "?api-version=7.1": "--area git --resource repositories --http-method GET --route-parameters project=Shop repositoryId=web --api-version 7.1",
		shopRepo + "/pullrequests?searchCriteria.sourceRefName=refs%2Fheads%2Fa%2Fb&searchCriteria.status=all&$top=20&api-version=7.1": "--area git --resource pullRequests --http-method GET --route-parameters project=Shop repositoryId=web " +
			"--query-parameters $top=20 searchCriteria.repositoryId=web searchCriteria.sourceRefName=refs/heads/a/b searchCriteria.status=all --api-version 7.1",
		shopRepo + "/pullrequests/7?api-version=7.1":                                                      "--area git --resource pullRequests --http-method GET --route-parameters project=Shop repositoryId=web pullRequestId=7 --api-version 7.1",
		shopRepo + "/pullRequests/7/statuses?api-version=7.1-preview.1":                                   "--area git --resource pullRequestStatuses --http-method GET --route-parameters project=Shop repositoryId=web pullRequestId=7 --api-version 7.1-preview",
		"https://dev.azure.com/acme/Shop/_apis/policy/evaluations?artifactId=x&api-version=7.1-preview.1": "--area policy --resource evaluations --http-method GET --route-parameters project=Shop --query-parameters artifactId=x --api-version 7.1-preview",
		b + "?branchName=refs%2Fpull%2F7%2Fmerge&$top=20&api-version=7.1":                                 "--area build --resource builds --http-method GET --route-parameters project=Shop --query-parameters $top=20 branchName=refs/pull/7/merge --api-version 7.1",
		b + "/6/timeline?api-version=7.1":                                                                 "--area build --resource timeline --http-method GET --route-parameters project=Shop buildId=6 --api-version 7.1",
		b + "/6/logs?api-version=7.1":                                                                     "--area build --resource logs --http-method GET --route-parameters project=Shop buildId=6 --api-version 7.1",
		b + "/6/logs/7?startLine=1&endLine=2000&api-version=7.1":                                          "--area build --resource logs --http-method GET --route-parameters project=Shop buildId=6 logId=7 --query-parameters endLine=2000 startLine=1 --api-version 7.1",
	} {
		args, ok := a.invokeArgs(u)
		if got := strings.Join(args, " "); !ok || got != pre+want {
			t.Errorf("%s:\n got %s\nwant %s", u, got, pre+want)
		}
	}
	for _, u := range []string{"https://dev.azure.com/evil/Shop/_apis/git/repositories/web", "https://dev.azure.com/acme/Shop/_apis/wit/workitems/1", "https://dev.azure.com/acme/Shop/_git/web", "::"} {
		if args, ok := a.invokeArgs(u); ok {
			t.Errorf("%s mapped: %q", u, args)
		}
	}
	// a visualstudio.com organization, a project with a space
	v := Azure{Target: Target{Kind: AzureKind, OrgURL: "https://acme.visualstudio.com", Project: "My Shop", Repo: "web"}}
	if args, ok := v.invokeArgs(v.prURL(3)); !ok || !slices.Contains(args, "project=My Shop") || !slices.Contains(args, "https://acme.visualstudio.com") {
		t.Errorf("visualstudio.com: %q", args)
	}
}

// TestAzureSignInPage (T877): az rest answering with Azure DevOps'
// sign-in page (HTTP 203, exit 0) is not signed in, never no PR; the
// extension is asked then, and first after for the organization.
func TestAzureSignInPage(t *testing.T) {
	signIn := "<!DOCTYPE html>\n<html><head><title>Azure DevOps Services | Sign In</title></head></html>"
	var ran []string
	ext := true
	a := Azure{Target: shop, Getenv: func(string) string { return "" }, Run: func(dir string, args ...string) ([]byte, error) {
		ran = append(ran, args[0])
		if args[0] == "rest" {
			return []byte(signIn), nil
		}
		if !ext {
			return nil, azError(errors.New("az devops invoke: exit status 2 ERROR: 'devops' is misspelled or not recognized by the system."))
		}
		cmd := strings.Join(args, " ")
		switch {
		case strings.Contains(cmd, "--resource pullRequests") && strings.Contains(cmd, "searchCriteria.sourceRefName"):
			return []byte(`{"count":1,"value":[` + string(fixture(t, "pr-branch-ci.json")) + `]}`), nil
		case strings.Contains(cmd, "--resource builds") && strings.Contains(cmd, "refs/heads/"):
			return fixture(t, "builds-branch.json"), nil
		}
		return []byte(`{"count":0,"value":[]}`), nil
	}}
	azVia.Clear()
	defer azVia.Clear()
	pr, err := a.PR("/r", Ref{Branch: "feature/ci"})
	if err != nil || pr.Number != 7 || pr.Checks != "fail" || !a.UsesExtension() {
		t.Fatalf("through the extension: %+v %v %q", pr, err, ran)
	}
	ran = nil
	if _, err := a.PR("/r", Ref{Branch: "feature/ci"}); err != nil || slices.Contains(ran, "rest") {
		t.Fatalf("az rest asked again: %v %q", err, ran)
	}
	// no extension: not signed in, in fixed words
	azVia.Clear()
	ext = false
	_, err = a.PR("/r", Ref{Branch: "feature/ci"})
	if errors.Is(err, ErrNoPR) || NoPRWhy(err) != "az failed: "+errSignInPage {
		t.Fatalf("no extension: %q %v", NoPRWhy(err), err)
	}
	if _, problem, advice := Advice(err); problem != errSignInPage || !strings.Contains(advice, "az extension add --name azure-devops") {
		t.Fatalf("advice %q %q", problem, advice)
	}
	// an extension that fails to sign in later: az rest again
	azVia.Store(shop.OrgURL, viaExt)
	ran = nil
	a.PR("/r", Ref{Branch: "feature/ci"})
	if !slices.Contains(ran, "rest") || a.UsesExtension() {
		t.Fatalf("stuck on the extension: %q", ran)
	}
	if !signInPage([]byte("\ufeff  <html>")) || signInPage([]byte(`{"value":[]}`)) || signInPage([]byte("2026-10-30T10:00:00Z log line")) {
		t.Fatal("signInPage")
	}
}
