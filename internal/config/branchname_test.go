package config

import (
	"strings"
	"testing"
)

func TestExpandBranchName(t *testing.T) {
	for _, c := range []struct{ tmpl, task, title, want string }{
		{DefaultBranchName, "T497", "fix-login", "tm/demo/t-0468-fix-login"},
		{DefaultBranchName, "", "", "tm/demo/t-0468"},
		{"feature/{task}-{title}", "T497", "fix-login", "feature/T497-fix-login"},
		{"feature/{task}-{title}", "", "fix-login", "feature/fix-login"},
		{"users/me/{id}", "T1", "x", "users/me/t-0468"},
		{"{project}/{task}/{id}", "T497", "x", "demo/T497/t-0468"},
		{"{project}/{task}/{id}", "", "x", "demo/t-0468"},
		{"{id}-{task}", "", "x", "t-0468"},
	} {
		if got := ExpandBranchName(c.tmpl, "demo", "t-0468", c.task, c.title); got != c.want {
			t.Errorf("%+v: %q", c, got)
		}
	}
}

func TestCheckBranchName(t *testing.T) {
	for _, ok := range []string{DefaultBranchName, "feature/{task}-{title}-{id}", "users/me/{id}", "{id}", "{task}/{id}", "{id}-{task}", "feature/{task}-{title}-{id}"} {
		if err := CheckBranchName(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for tmpl, why := range map[string]string{
		"":                "empty",
		"feature/{title}": "{id}",
		"x/{id}/{nope}":   "unknown placeholder",
		"x/{id}{":         "stray",
		"x y/{id}":        "git refuses",
		"x/{id}..":        "git refuses",
		"x//{id}":         "doubled",
		"x/{id}/":         "trailing",
		"x--{task}{id}":   "doubled",
		"-{id}":           "leading",
	} {
		err := CheckBranchName(tmpl)
		if err == nil || !strings.Contains(err.Error(), why) {
			t.Errorf("%q: %v, want a refusal about %q", tmpl, err, why)
		}
	}
}

func TestBranchNameSetting(t *testing.T) {
	if Defaults.BranchName != DefaultBranchName {
		t.Fatal("default")
	}
	if err := CheckSetting("branch_name", "feature/{title}"); err == nil {
		t.Error("a template without {id} was accepted")
	}
	if err := CheckSetting("branch_name", "users/me/{id}"); err != nil {
		t.Error(err)
	}
}
