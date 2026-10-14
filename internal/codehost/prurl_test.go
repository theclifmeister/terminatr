package codehost

import (
	"os/exec"
	"testing"
)

func TestParsePRURL(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		kind string
		ok   bool
	}{
		{"https://github.com/o/r/pull/12", 12, GitHubKind, true},
		{"https://github.com/o/r/pull/0", 0, "", false},
		{"https://github.com/o/r/pull/1234567890", 0, "", false},
		{"https://github.com/o/r/pull/12?x=1", 0, "", false},
		{"https://github.com/o/pull/12", 0, "", false},
		{"https://evil.example/o/r/pull/12", 0, "", false},
		{"http://github.com/o/r/pull/12", 0, "", false},
		{"https://dev.azure.com/org/proj/_git/repo/pullrequest/7", 7, AzureKind, true},
		{"https://dev.azure.com/org/_git/repo/pullrequest/7", 7, AzureKind, true},
		{"https://dev.azure.com/org/My%20Proj/_git/repo/pullrequest/7", 7, AzureKind, true},
		{"https://org.visualstudio.com/proj/_git/repo/pullrequest/7", 7, AzureKind, true},
		{"https://org.visualstudio.com/DefaultCollection/proj/_git/repo/pullrequest/7", 7, AzureKind, true},
		{"https://dev.azure.com/org/proj/_git/repo/pullrequest/0", 0, "", false},
		{"https://dev.azure.com/org/proj/_git/repo/pullrequest/7/", 0, "", false},
		{"https://dev.azure.com/org/proj/_git/repo/pullrequest/7?_a=files", 0, "", false},
		{"https://dev.azure.com/org/proj/repo/pullrequest/7", 0, "", false},
		{"https://dev.azure.com/org/proj/_git/repo/pull/7", 0, "", false},
		{"https://example.com/org/proj/_git/repo/pullrequest/7", 0, "", false},
		{"", 0, "", false},
	} {
		n, kind, ok := ParsePRURL(c.in)
		if n != c.n || kind != c.kind || ok != c.ok {
			t.Errorf("ParsePRURL(%q) = %d, %q, %v; want %d, %q, %v", c.in, n, kind, ok, c.n, c.kind, c.ok)
		}
	}
}

func TestOwnsPRURL(t *testing.T) {
	az := Target{Kind: AzureKind, OrgURL: "https://dev.azure.com/Org", Project: "Proj", Repo: "Repo"}
	vs := Target{Kind: AzureKind, OrgURL: "https://org.visualstudio.com", Project: "proj", Repo: "repo"}
	for _, c := range []struct {
		t    Target
		in   string
		want bool
	}{
		{az, "https://dev.azure.com/org/proj/_git/repo/pullrequest/3", true},
		{az, "https://org.visualstudio.com/proj/_git/repo/pullrequest/3", true},
		{vs, "https://dev.azure.com/ORG/proj/_git/repo/pullrequest/3", true},
		{az, "https://dev.azure.com/other/proj/_git/repo/pullrequest/3", false},
		{az, "https://dev.azure.com/org/other/_git/repo/pullrequest/3", false},
		{az, "https://dev.azure.com/org/proj/_git/other/pullrequest/3", false},
		{az, "https://github.com/o/r/pull/3", false},
		{Target{Kind: GitHubKind}, "https://github.com/o/r/pull/3", true},
		{Target{Kind: GitHubKind}, "https://dev.azure.com/org/proj/_git/repo/pullrequest/3", false},
	} {
		if got := ownsPRURL(c.t, c.in); got != c.want {
			t.Errorf("ownsPRURL(%+v, %q) = %v; want %v", c.t, c.in, got, c.want)
		}
	}
}

func FuzzParsePRURL(f *testing.F) {
	for _, s := range []string{"https://github.com/o/r/pull/1", "https://dev.azure.com/o/p/_git/r/pullrequest/2", "https://o.visualstudio.com/DefaultCollection/p/_git/r/pullrequest/3", "", "https://dev.azure.com//_git//pullrequest/1"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		n, kind, ok := ParsePRURL(s)
		if ok && (n < 1 || n > 999999999 || (kind != GitHubKind && kind != AzureKind)) {
			t.Fatalf("ParsePRURL(%q) = %d, %q, true", s, n, kind)
		}
		if !ok && (n != 0 || kind != "") {
			t.Fatalf("ParsePRURL(%q) = %d, %q, false", s, n, kind)
		}
	})
}

func TestInRepo(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init", "-q")
	pr := PR{Number: 7, URL: "https://github.com/acme/shop/pull/7"}
	if InRepo(repo, GitHub{}, pr) {
		t.Fatal("no origin: in repo")
	}
	git("remote", "add", "origin", "git@github.com:acme/shop.git")
	for url, want := range map[string]bool{
		"https://github.com/acme/shop/pull/7":  true,
		"https://github.com/Acme/Shop/pull/7":  true,
		"https://github.com/acme/shop/pull/8":  false, // not pr's number
		"https://github.com/other/shop/pull/7": false,
		"https://github.com/acme/shopx/pull/7": false,
		"":                                     false,
	} {
		if got := InRepo(repo, GitHub{}, PR{Number: 7, URL: url}); got != want {
			t.Errorf("%q: %v, want %v", url, got, want)
		}
	}
	git("remote", "set-url", "origin", "https://github.com/acme/shop")
	if !InRepo(repo, GitHub{}, pr) {
		t.Error("https origin without .git")
	}
	// Azure DevOps and mods are only asked about the origin's repo.
	if !InRepo(repo, Azure{}, PR{Number: 7}) {
		t.Error("azure")
	}
}
