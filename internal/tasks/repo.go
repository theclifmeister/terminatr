package tasks

import (
	"fmt"
	"path/filepath"
	"strings"
)

// NoRepo is the repo of a task whose thread works in a plain folder,
// without git, even in a project with repos (docs/SPEC.md §9).
const NoRepo = "none"

// RepoName is how a task or --repo names one of the project's repos:
// its folder's name.
func RepoName(dir string) string { return filepath.Base(filepath.Clean(dir)) }

// FindRepo is the repo among repos that name names: one's path, or the
// name of one folder (RepoName); "" for NoRepo. Anything else is
// refused, unknown-repo, as is a name two repos share, ambiguous-repo.
func FindRepo(repos []string, name string) (string, error) {
	if name == NoRepo {
		return "", nil
	}
	var named []string
	for _, r := range repos {
		if filepath.Clean(r) == filepath.Clean(name) {
			return r, nil
		}
		if RepoName(r) == name {
			named = append(named, r)
		}
	}
	switch {
	case len(named) == 1:
		return named[0], nil
	case len(named) > 1:
		return "", refuse("ambiguous-repo", fmt.Sprintf("%d of the project's repos are named %s: name one by its path (%s)", len(named), name, strings.Join(named, ", ")))
	case len(repos) == 0:
		return "", refuse("unknown-repo", fmt.Sprintf("%q is not a repo of the project, which has none: use none (a folder without git)", name))
	}
	return "", refuse("unknown-repo", fmt.Sprintf("%q is not one of the project's repos (%s) nor none (a folder without git)", name, RepoList(repos)))
}

// RepoList lists repos as "name (path), …" for a message.
func RepoList(repos []string) string {
	out := make([]string, len(repos))
	for i, r := range repos {
		out[i] = RepoName(r) + " (" + r + ")"
	}
	return strings.Join(out, ", ")
}

// checkRepo validates a task's repo: "", NoRepo, or one of the store's
// repos by name or path (FindRepo).
func (s *Store) checkRepo(v string) (string, error) {
	v, err := checkLabel("repo", v)
	if err != nil || v == "" {
		return v, err
	}
	if _, err := FindRepo(s.Repos, v); err != nil {
		return "", err
	}
	return v, nil
}
