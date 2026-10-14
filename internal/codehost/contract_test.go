package codehost

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// hostFake is one host whose CLI answers a scenario: PR answers its PR
// call for branch b, or the error.
type hostFake struct {
	name string
	make func(s scenario) Host
}

// scenario is one PR situation, of a PR from branch b, as each host's
// CLI would answer it.
type scenario struct {
	name string
	gh   string // `gh pr view --json`; "!" prefix => error text
	az   string // `az rest` PR answer, else "!" error text
	pol  string // `az rest` policy evaluations
	sts  string // `az rest` PR statuses
	// gl is GitLab's merge request (glab api projects/…/merge_requests/7),
	// "" for none, "!" prefix => error text; gla its approvals.
	gl, gla string
	want    PR    // URL and Head left out
	err     error // ErrNoPR, or errCLI for a CLI failure
}

var errCLI = errors.New("a CLI failure")

const sha = "0123456789abcdef0123456789abcdef01234567"

func azList(status, mergeStatus, extra string) string {
	return fmt.Sprintf(`[{"pullRequestId":7,"status":%q,"mergeStatus":%q,"sourceRefName":"refs/heads/b","targetRefName":"refs/heads/main",`+
		`"lastMergeSourceCommit":{"commitId":%q},"repository":{"name":"web","project":{"id":"`+shopPID+`","name":"Shop"}}%s}]`, status, mergeStatus, sha, extra)
}

// glMR is GitLab's merge request !7 of acme/shop from branch b in state,
// with extra fields.
func glMR(state, extra string) string {
	if extra != "" {
		extra = "," + extra
	}
	return fmt.Sprintf(`{"iid":7,"web_url":%q,"state":%q,"source_branch":"b","target_branch":"main","sha":%q%s}`, glURL, state, sha, extra)
}

const glURL = "https://gitlab.com/acme/shop/-/merge_requests/7"

var buildPolicy = func(status string) string {
	return `[{"status":"` + status + `","configuration":{"isBlocking":true,"isEnabled":true,"type":{"id":"` + policyBuild + `"}}}]`
}

var scenarios = []scenario{
	{name: "opened, checks running",
		gh: `{"number":7,"headRefName":"b","state":"OPEN","baseRefName":"main","mergeable":"MERGEABLE","statusCheckRollup":[{"status":"IN_PROGRESS"}]}`,
		az: azList("active", "succeeded", ""), pol: buildPolicy("running"),
		gl:   glMR("opened", `"detailed_merge_status":"ci_still_running","head_pipeline":{"id":9,"status":"running"}`),
		want: PR{Number: 7, Branch: "b", State: "OPEN", Checks: "pending", Base: "main", Mergeable: "MERGEABLE"}},
	{name: "checks failed",
		gh: `{"number":7,"headRefName":"b","state":"OPEN","baseRefName":"main","mergeable":"MERGEABLE","statusCheckRollup":[{"status":"COMPLETED","conclusion":"FAILURE"}]}`,
		az: azList("active", "succeeded", ""), pol: buildPolicy("rejected"),
		gl:   glMR("opened", `"detailed_merge_status":"ci_must_pass","head_pipeline":{"id":9,"status":"failed"}`),
		want: PR{Number: 7, Branch: "b", State: "OPEN", Checks: "fail", Failed: 1, Base: "main", Mergeable: "MERGEABLE"}},
	{name: "checks passed, external status failed",
		gh: `{"number":7,"headRefName":"b","state":"OPEN","baseRefName":"main","mergeable":"MERGEABLE","statusCheckRollup":[{"status":"COMPLETED","conclusion":"SUCCESS"},{"state":"FAILURE"}]}`,
		az: azList("active", "succeeded", ""), pol: buildPolicy("approved"), sts: `{"value":[{"id":1,"state":"failed","context":{"name":"x"}}]}`,
		gl:   glMR("opened", `"detailed_merge_status":"mergeable","head_pipeline":{"id":9,"status":"failed"}`),
		want: PR{Number: 7, Branch: "b", State: "OPEN", Checks: "fail", Failed: 1, Base: "main", Mergeable: "MERGEABLE"}},
	{name: "changes requested",
		gh: `{"number":7,"headRefName":"b","state":"OPEN","reviewDecision":"CHANGES_REQUESTED","baseRefName":"main","mergeable":"MERGEABLE","statusCheckRollup":[{"status":"COMPLETED","conclusion":"SUCCESS"}]}`,
		az: azList("active", "succeeded", `,"reviewers":[{"vote":-10}]`), pol: buildPolicy("approved"),
		gl:   glMR("opened", `"detailed_merge_status":"requested_changes","head_pipeline":{"id":9,"status":"success"}`),
		want: PR{Number: 7, Branch: "b", State: "OPEN", Checks: "pass", Review: "CHANGES_REQUESTED", Base: "main", Mergeable: "MERGEABLE"}},
	{name: "approved, no CI",
		gh: `{"number":7,"headRefName":"b","state":"OPEN","reviewDecision":"APPROVED","baseRefName":"main","mergeable":"MERGEABLE","statusCheckRollup":[]}`,
		az: azList("active", "succeeded", `,"reviewers":[{"vote":10,"isRequired":true}]`), pol: `[]`,
		gl: glMR("opened", `"detailed_merge_status":"mergeable","head_pipeline":null`), gla: `{"approved":true,"approvals_left":0,"approved_by":[{"user":{"username":"ada"}}]}`,
		want: PR{Number: 7, Branch: "b", State: "OPEN", Review: "APPROVED", Base: "main", Mergeable: "MERGEABLE"}},
	{name: "conflict",
		gh: `{"number":7,"headRefName":"b","state":"OPEN","baseRefName":"main","mergeable":"CONFLICTING","mergeStateStatus":"DIRTY"}`,
		az: azList("active", "conflicts", ""), pol: `[]`,
		gl:   glMR("opened", `"has_conflicts":true,"detailed_merge_status":"conflict"`),
		want: PR{Number: 7, Branch: "b", State: "OPEN", Base: "main", Mergeable: "CONFLICTING", MergeState: "DIRTY"}},
	{name: "merge state not computed yet",
		gh: `{"number":7,"headRefName":"b","state":"OPEN","baseRefName":"main","mergeable":"UNKNOWN"}`,
		az: azList("active", "queued", ""), pol: `[]`,
		gl:   glMR("opened", `"detailed_merge_status":"checking"`),
		want: PR{Number: 7, Branch: "b", State: "OPEN", Base: "main", Mergeable: "UNKNOWN"}},
	{name: "merged (squash)",
		gh:   `{"number":7,"headRefName":"b","state":"MERGED","baseRefName":"main","mergeable":"UNKNOWN","mergeCommit":{"oid":"` + strings.Repeat("ab", 20) + `"}}`,
		az:   azList("completed", "notSet", `,"lastMergeCommit":{"commitId":"`+strings.Repeat("ab", 20)+`"}`),
		gl:   glMR("merged", `"squash_commit_sha":"`+strings.Repeat("ab", 20)+`"`),
		want: PR{Number: 7, Branch: "b", State: "MERGED", Base: "main", Mergeable: "UNKNOWN", Merge: strings.Repeat("ab", 20)}},
	{name: "closed",
		gh:   `{"number":7,"headRefName":"b","state":"CLOSED","baseRefName":"main","mergeable":"UNKNOWN"}`,
		az:   azList("abandoned", "notSet", ""),
		gl:   glMR("closed", ""),
		want: PR{Number: 7, Branch: "b", State: "CLOSED", Base: "main", Mergeable: "UNKNOWN"}},
	{name: "no PR",
		gh:  `!no pull requests found for branch "b"`,
		az:  `[]`,
		err: ErrNoPR},
	{name: "logged out",
		gh:  `!To get started with GitHub CLI, please run:  gh auth login`,
		az:  `!ERROR: Please run 'az login' to setup account.`,
		gl:  `!glab: 401 Unauthorized (HTTP 401)`,
		err: errCLI},
}

var hostFakes = []hostFake{
	{"github", func(s scenario) Host {
		return GitHub{Run: func(dir string, args ...string) ([]byte, error) {
			if e, bad := strings.CutPrefix(s.gh, "!"); bad {
				return nil, fmt.Errorf("gh %s: exit status 1 %s", strings.Join(args, " "), e)
			}
			return []byte(s.gh), nil
		}}
	}},
	{"azure", func(s scenario) Host {
		return Azure{Target: shop, Getenv: func(string) string { return "" }, Run: func(dir string, args ...string) ([]byte, error) {
			out := s.az
			switch route, _ := azRoute(args); route {
			case "statuses":
				out = s.sts
			case "policies":
				out = s.pol
			}
			if e, bad := strings.CutPrefix(out, "!"); bad {
				return nil, azError(fmt.Errorf("az %s: exit status 1 %s", strings.Join(args, " "), e))
			}
			return []byte(out), nil
		}}
	}},
}

// TestHostContract: every host maps the same situation onto the same
// fixed fields and the same errors, so the ticker needn't know which
// one it asked. A code host mod is held to it too (contract_mod_test.go
// runs the GitLab example mod through Mod).
func TestHostContract(t *testing.T) { CheckContract(t, hostFakes) }

// CheckContract runs every scenario against each of hosts.
func CheckContract(t *testing.T, hosts []hostFake) {
	t.Helper()
	for _, s := range scenarios {
		for _, h := range hosts {
			pr, err := h.make(s).PR("/r", Ref{Branch: "b"})
			switch {
			case s.err == ErrNoPR:
				if !errors.Is(err, ErrNoPR) {
					t.Errorf("%s/%s: %v, want no PR", h.name, s.name, err)
				}
				continue
			case s.err == errCLI:
				if err == nil || errors.Is(err, ErrNoPR) {
					t.Errorf("%s/%s: %v, want a failure", h.name, s.name, err)
				}
				continue
			case err != nil:
				t.Errorf("%s/%s: %v", h.name, s.name, err)
				continue
			}
			if h.name == "azure" && (pr.URL != azurePRURL(shop, 7) || pr.Head != sha) {
				t.Errorf("%s/%s: URL %q head %q", h.name, s.name, pr.URL, pr.Head)
			}
			if h.name == "gitlab mod" && (pr.URL != glURL || pr.Head != sha) {
				t.Errorf("%s/%s: URL %q head %q", h.name, s.name, pr.URL, pr.Head)
			}
			pr.URL, pr.Head = "", ""
			if pr != s.want {
				t.Errorf("%s/%s:\n got %+v\nwant %+v", h.name, s.name, pr, s.want)
			}
			if pr.Summary() != s.want.Summary() {
				t.Errorf("%s/%s: summary %q", h.name, s.name, pr.Summary())
			}
		}
	}
}
