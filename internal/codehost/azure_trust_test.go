package codehost

import (
	"errors"
	"os/exec"
	"testing"
)

func TestTrustedAzureURL(t *testing.T) {
	defer func(f func() []string) { azureHosts = f }(azureHosts)
	azureHosts = func() []string { return []string{"tfs.example.com"} }
	if !TrustedAzureURL("https://TFS.example.com/tfs/Coll") || TrustedAzureURL("https://tfs.example.com.evil.test/x") {
		t.Error("listed host")
	}
	for u, want := range map[string]bool{
		"https://dev.azure.com/acme":               true,
		"https://acme.visualstudio.com/Shop":       true,
		"https://attacker.example":                 false,
		"http://dev.azure.com/acme":                false,
		"https://dev.azure.com@attacker.example":   false,
		"https://user:pw@dev.azure.com/acme":       false,
		"https://dev.azure.com.attacker.example/x": false,
		"https://evil.com/.visualstudio.com":       false,
		"https://a.b.visualstudio.com":             false,
		"https://dev.azure.com:8443/acme":          false,
		"":                                         false,
	} {
		if got := TrustedAzureURL(u); got != want {
			t.Errorf("TrustedAzureURL(%q) = %v", u, got)
		}
	}
}

func TestAzureURLConfigUntrusted(t *testing.T) {
	defer func(f func() []string) { azureHosts = f }(azureHosts)
	azureHosts = func() []string { return nil }
	got := Detect(t.TempDir(), Config{CodeHost: AzureKind, AzureURL: "https://attacker.example"})
	if got.OrgURL != "" {
		t.Fatalf("OrgURL %q", got.OrgURL)
	}
}

func TestAzureNoTokenToUntrustedHost(t *testing.T) {
	defer func(f func() []string) { azureHosts = f }(azureHosts)
	azureHosts = func() []string { return nil }
	var ran [][]string
	called := false
	a := Azure{
		Target: Target{Kind: AzureKind, OrgURL: "https://attacker.example", Project: "p", Repo: "r"},
		PATGet: func(u, pat string) ([]byte, error) { called = true; return nil, nil },
	}
	_ = ran
	if _, err := a.rest(t.TempDir(), a.repoURL()); !errors.Is(err, errUntrustedURL) {
		t.Fatalf("rest: %v", err)
	}
	if _, err := a.patGet(a.repoURL(), "s3cret"); !errors.Is(err, errUntrustedURL) || called {
		t.Fatalf("patGet: %v %v", err, called)
	}
	if _, err := PATGet(a.repoURL(), "s3cret"); !errors.Is(err, errUntrustedURL) {
		t.Fatalf("PATGet: %v", err)
	}
}

func TestAzureURLTrustsOriginAndAzDefault(t *testing.T) {
	defer func(f func() []string, g func() string) { azureHosts, azDefaultHost = f, g }(azureHosts, azDefaultHost)
	azureHosts = func() []string { return nil }
	azDefaultHost = func() string { return "" }
	mk := func(remote string) string {
		repo := t.TempDir()
		for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", remote}} {
			if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v %s", args, err, out)
			}
		}
		return repo
	}
	const org = "https://onprem.example/tfs/Coll"
	for _, remote := range []string{
		"https://onprem.example/tfs/Coll/Shop/_git/web",
		"ssh://tfs@onprem.example:22/tfs/Coll/Shop/_git/web",
		"tfs@onprem.example:v3/Coll/Shop/web",
	} {
		repo := mk(remote)
		got := Detect(repo, Config{CodeHost: AzureKind, AzureURL: org, OriginRepos: []string{repo}})
		if got.OrgURL != org {
			t.Errorf("origin %q: OrgURL %q", remote, got.OrgURL)
		}
		a := Azure{Target: got}
		if !a.trusted(org+"/_apis/x") || a.trusted("https://other.example/x") {
			t.Errorf("origin %q: target trust", remote)
		}
		// The trust is the target's: nothing global, no other repo's.
		if TrustedAzureURL(org) || (Azure{Target: Target{Kind: AzureKind}}).trusted(org) {
			t.Errorf("origin %q: trust leaked", remote)
		}
	}
	repo := mk("https://onprem.example/tfs/Coll/Shop/_git/web")
	// A repo the user didn't register (not in OriginRepos) doesn't vouch.
	if got := Detect(repo, Config{CodeHost: AzureKind, AzureURL: org}); got.OrgURL != "" {
		t.Errorf("unregistered repo vouched: %q", got.OrgURL)
	}
	if got := Detect(repo, Config{CodeHost: AzureKind, AzureURL: org, OriginRepos: []string{t.TempDir()}}); got.OrgURL != "" {
		t.Errorf("another repo vouched: %q", got.OrgURL)
	}
	// An azure_url on a host that is not the origin's stays ignored.
	if got := Detect(repo, Config{CodeHost: AzureKind, AzureURL: "https://unrelated.example/tfs", OriginRepos: []string{repo}}); got.OrgURL != "" {
		t.Errorf("unrelated host trusted: %q", got.OrgURL)
	}
	// A host only PROJECT.md names is not trusted.
	if TrustedAzureURL("https://only-project.example/tfs") {
		t.Error("PROJECT.md-only host trusted")
	}
	// The az default organization's host.
	azDefaultHost = func() string { return "azorg.example" }
	if !TrustedAzureURL("https://azorg.example/tfs/Coll") || TrustedAzureURL("https://other.example/tfs") {
		t.Error("az default organization host")
	}
}

func TestOrgHostFromConfig(t *testing.T) {
	for in, want := range map[string]string{
		"[defaults]\norganization = https://TFS.example.com/tfs/Coll\nproject = x\n": "tfs.example.com",
		"[defaults]\nproject = x\n":                                "",
		"[defaults]\norganization = http://tfs.example.com\n":      "",
		"[defaults]\norganization = https://u:p@tfs.example.com\n": "",
	} {
		if got := orgHostFromConfig(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
