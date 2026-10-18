package server

import (
	"context"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/gitx"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// A Codex thread's network is limited to its host rules (T574 case 5),
// which would leave its pushes asking the user, or failing with
// escapes off. So a Codex thread also reaches the hosts of its repo's
// git remotes (coordinator decision, 2026-10-18): a tm rule per host,
// read by tm from the repository's own config file, which a thread
// can't write (never its worktree's git dir or config.worktree, which
// it can). A push over https or ssh names only the remote's host, so
// the host is all it needs. Claude threads don't get it: their
// sandbox asks per host.

// codexOpenNote is codex.OpenNote: what the effective view says of a
// Codex thread's network on Linux and Windows.
func codexOpenNote(goos string) string {
	if goos == config.OSWindows {
		return "network not limited on Windows: Codex's proxy needs its elevated sandbox"
	}
	return "network not limited on Linux: Codex's Linux sandbox can't allow tm's socket with its proxy on"
}

// remoteHostNote is the note of a git remote host rule.
const remoteHostNote = "git remote host"

// remoteHostRules are the git remote host rules of a Codex thread whose
// repository's git dir is common.
func remoteHostRules(common string) []proto.AccessRule {
	var out []proto.AccessRule
	for _, h := range remoteHosts(common) {
		out = append(out, proto.AccessRule{Where: proto.AccessInside, Kind: proto.AccessHost, Value: h, Source: proto.AccessFromTM,
			Note: remoteHostNote, Agents: []string{"codex"}})
	}
	return out
}

// remoteHosts are the host names of the fetch and push URLs of the
// remotes in common's config file, read as a file (no includes, no
// worktree config), in order, each once; a local path, an address or
// a name that isn't a host gives none.
func remoteHosts(common string) []string {
	if common == "" {
		return nil
	}
	out, err := gitx.Run(context.Background(), common, gitx.Opts{Untrusted: true},
		"config", "--file", filepath.Join(common, "config"), "--get-regexp", `^remote\..*\.(push)?url$`)
	if err != nil {
		return nil
	}
	var hosts []string
	for _, line := range strings.Split(out, "\n") {
		_, u, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		if h := remoteHost(strings.TrimSpace(u)); h != "" && !slices.Contains(hosts, h) {
			hosts = append(hosts, h)
		}
	}
	return hosts
}

// remoteHost is the host name of git remote URL u (https://host/…,
// ssh://user@host:port/…, user@host:path), lower case, or "" for a
// local path, a file:// URL, an address, or a name of one word.
func remoteHost(u string) string {
	var host string
	if strings.Contains(u, "://") {
		p, err := url.Parse(u)
		if err != nil || p.Scheme == "file" {
			return ""
		}
		host = p.Hostname()
	} else {
		h, _, ok := strings.Cut(u, ":")
		if !ok || strings.ContainsAny(h, `/\`) {
			return ""
		}
		if _, after, at := strings.Cut(h, "@"); at {
			h = after
		}
		host = h
	}
	host = strings.ToLower(host)
	labels := strings.Split(host, ".")
	if len(labels) < 2 || host == "localhost" || slices.Contains(labels, "localhost") {
		return ""
	}
	for _, l := range labels {
		if l == "" || len(l) > 63 || strings.Trim(l, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" || strings.HasPrefix(l, "-") || strings.HasSuffix(l, "-") {
			return ""
		}
	}
	if last := labels[len(labels)-1]; last[0] >= '0' && last[0] <= '9' {
		return "" // an address
	}
	return host
}
