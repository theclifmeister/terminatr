package codehost

import (
	"errors"
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
