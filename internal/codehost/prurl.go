package codehost

import (
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// prLink is a parsed PR URL: its host kind, number and, for Azure DevOps,
// where the repo is (Target.OrgURL, Project, Repo as ParseRemote reads
// them from a remote, so the two compare with sameRepo).
type prLink struct {
	Kind   string
	Number int
	Target Target
}

var (
	githubPRRE = regexp.MustCompile(`^https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/pull/([0-9]{1,9})$`)
	// azurePRRE admits only the characters of a percent-encoded name.
	azurePRRE = regexp.MustCompile(`^https://[A-Za-z0-9.-]+(/[A-Za-z0-9_.~%-]+)+/pullrequest/([0-9]{1,9})$`)
	// gitlabPRRE is a GitLab merge request's URL: a host (with a port
	// on a server), a project path of a group, any subgroups and the
	// project, then /-/merge_requests/<n>.
	gitlabPRRE = regexp.MustCompile(`^https://([A-Za-z0-9.-]+(?::[0-9]{1,5})?)/((?:[A-Za-z0-9_.-]+/)+[A-Za-z0-9_.-]+)/-/merge_requests/([0-9]{1,9})$`)
)

// ParsePR reads a PR URL of GitHub (https://github.com/o/r/pull/12) or
// Azure DevOps:
//
//	https://dev.azure.com/{org}/{project}/_git/{repo}/pullrequest/12
//	https://dev.azure.com/{org}/_git/{repo}/pullrequest/12     (project = repo)
//	https://{org}.visualstudio.com/[DefaultCollection/]{project}/_git/{repo}/pullrequest/12
//
// or a GitLab merge request, which code host mods serve (mods design §2
// tier 5):
//
//	https://gitlab.com/{group}[/{subgroup}…]/{project}/-/merge_requests/12
//
// ok is false for anything else: other hosts, a query or fragment, a
// number of 0 or over nine digits.
func ParsePR(s string) (l prLink, ok bool) {
	if m := gitlabPRRE.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[3])
		if err != nil || n < 1 || slices.ContainsFunc(strings.Split(m[2], "/"), func(seg string) bool { return strings.Trim(seg, ".") == "" }) {
			return prLink{}, false
		}
		return prLink{Kind: GitLabKind, Number: n, Target: Target{Kind: GitLabKind, OrgURL: "https://" + strings.ToLower(m[1]), Repo: m[2]}}, true
	}
	if m := githubPRRE.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 {
			return prLink{}, false
		}
		return prLink{Kind: GitHubKind, Number: n, Target: Target{Kind: GitHubKind}}, true
	}
	m := azurePRRE.FindStringSubmatch(s)
	if m == nil {
		return prLink{}, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil || n < 1 {
		return prLink{}, false
	}
	repoURL := strings.TrimSuffix(s, "/pullrequest/"+m[2])
	t, ok := ParseRemote(repoURL)
	if !ok {
		return prLink{}, false
	}
	return prLink{Kind: AzureKind, Number: n, Target: t}, true
}

// ParsePRURL is the number and host kind of a PR URL (ParsePR).
func ParsePRURL(s string) (n int, kind string, ok bool) {
	l, ok := ParsePR(s)
	return l.Number, l.Kind, ok
}

// sameRepo says whether two Azure DevOps targets are the same repo:
// organization, project and repo, names compared without case as Azure
// DevOps does. A target of another kind is never the same.
func sameRepo(a, b Target) bool {
	if a.Kind != AzureKind || b.Kind != AzureKind {
		return false
	}
	return orgName(a.OrgURL) != "" && orgName(a.OrgURL) == orgName(b.OrgURL) &&
		strings.EqualFold(a.Project, b.Project) && strings.EqualFold(a.Repo, b.Repo)
}

// orgName is the organization of an OrgURL, lower case, so that
// dev.azure.com/org and org.visualstudio.com name the same one.
func orgName(orgURL string) string {
	u, err := url.Parse(orgURL)
	if err != nil {
		return ""
	}
	h := strings.ToLower(u.Hostname())
	if org, ok := strings.CutSuffix(h, ".visualstudio.com"); ok {
		return org
	}
	if h == "dev.azure.com" {
		org, _, _ := strings.Cut(strings.Trim(u.Path, "/"), "/")
		return strings.ToLower(org)
	}
	return ""
}

// ownsPRURL says whether s is a PR URL of the repo t names: for GitHub
// any github.com PR URL (gh resolves it itself), for Azure DevOps one
// whose organization, project and repo are t's, for GitLab one on t's
// server and project path (without case, as GitLab compares them).
func ownsPRURL(t Target, s string) bool {
	l, ok := ParsePR(s)
	if !ok || l.Kind != t.Kind {
		return false
	}
	switch t.Kind {
	case GitHubKind:
		return true
	case GitLabKind:
		return t.OrgURL != "" && strings.EqualFold(t.OrgURL, l.Target.OrgURL) && strings.EqualFold(t.Repo, l.Target.Repo)
	}
	return sameRepo(t, l.Target)
}

// InRepo says whether pr is one of repo's own PRs, as tm thread merge
// asks before merging it. A GitHub PR's URL must name the owner and repo
// of repo's origin (gh may be asking another repo of its choice); the
// server isn't compared, as an origin may name it by an ssh alias. Azure
// DevOps and code host mods are only ever asked about the origin's repo
// and keep no PR of another, so theirs are.
func InRepo(repo string, h Host, pr PR) bool {
	if h.Kind() != GitHubKind {
		return true
	}
	if !urlRE.MatchString(pr.URL) {
		return false
	}
	_, path, ok := splitRemote(origin(repo))
	if !ok {
		return false
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	want := "/" + path + "/pull/" + strconv.Itoa(pr.Number)
	_, got, _ := strings.Cut(strings.TrimPrefix(pr.URL, "https://"), "/")
	return path != "" && strings.EqualFold("/"+got, want)
}
