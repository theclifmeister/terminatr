package vault

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/vaultlink"
)

// The paired servers' stubs (T724, docs/SPEC.md §5.1, Vault): a paired
// server's projects aren't on disk here, so tm writes a note per shared
// project from the peer mirror, _peers/<alias>/<slug>.md, for the links
// to it (vaultlink.Project with "<alias>/<slug>") to resolve. It is
// keyed by the alias, as all storage is, never by the display name, and
// marked as data: everything in it but the links from here is the other
// server's word.

// PeersDir is the stubs' folder in the vault, beside the projects: no
// slug starts with "_".
const PeersDir = "_peers"

// PeerNote is the path in the vault of the stub of project slug of the
// paired server alias.
func PeerNote(alias, slug string) string { return PeersDir + "/" + alias + "/" + slug + ".md" }

// peerFront is a stub's properties.
type peerFront struct {
	ID       string    `toml:"id" yaml:"id"`
	Server   string    `toml:"server" yaml:"server"`
	Name     string    `toml:"name,omitempty" yaml:"name,omitempty"`
	Project  string    `toml:"project" yaml:"project"`
	State    string    `toml:"state" yaml:"state"`
	Reason   string    `toml:"reason,omitempty" yaml:"reason,omitempty"`
	Since    time.Time `toml:"since,omitzero" yaml:"since,omitempty"`
	Stale    bool      `toml:"stale,omitempty" yaml:"stale,omitempty"`
	LastSeen time.Time `toml:"last_seen,omitzero" yaml:"last_seen,omitempty"`
	Version  string    `toml:"version,omitempty" yaml:"version,omitempty"`
	Paused   bool      `toml:"paused,omitempty" yaml:"paused,omitempty"`
	NeedsYou int       `toml:"needs_you" yaml:"needs_you"`
	Threads  int       `toml:"threads" yaml:"threads"`
	Ready    int       `toml:"ready" yaml:"ready"`
	Linked   []string  `toml:"linked,omitempty" yaml:"linked,omitempty"`
	Tags     []string  `toml:"tags" yaml:"tags"`
	TM       string    `toml:"tm" yaml:"tm"`
	CSSClass []string  `toml:"cssclasses" yaml:"cssclasses"`
}

// PeerNotes renders the stub of every shared project in peers, the
// mirror's list (peer.list). linked are the projects here by the link
// key naming each ("mini/acme-api"), as config.Links has them. keep are
// the aliases whose stubs stay as they are: the paired servers whose
// projects the mirror hasn't listed yet (a server just started, a link
// not up since), so a restart doesn't drop them. A peer's alias or
// project that can't be a link key has no stub.
func PeerNotes(peers []proto.PeerMirror, linked map[string][]string) (notes []Note, keep []string) {
	for _, pm := range peers {
		if !pm.Listed {
			keep = append(keep, pm.Alias)
			continue
		}
		shared := map[string]bool{}
		for _, pr := range pm.Projects {
			shared[pr.Project] = true
		}
		for _, pr := range pm.Projects {
			key := pm.Alias + "/" + pr.Project
			if _, _, ok := config.Remote(key); !ok {
				continue
			}
			n, err := peerNote(pm, pr, shared, linked[key])
			if err == nil {
				notes = append(notes, n)
			}
		}
	}
	return notes, keep
}

func peerNote(pm proto.PeerMirror, pr proto.PeerMirrorProject, shared map[string]bool, linked []string) (Note, error) {
	key := pm.Alias + "/" + pr.Project
	f := peerFront{
		ID: key, Server: pm.Alias, Name: textsafe.Line(pm.Name, 80), Project: pr.Project,
		State: pm.State, Reason: textsafe.Line(pm.Reason, 200), Since: pm.Since.UTC(),
		Stale: pr.Stale, Version: textsafe.Line(pm.Version, 40),
		Paused: pr.Paused, NeedsYou: pr.NeedsYou, Threads: pr.Threads, Ready: pr.Ready,
		Tags: []string{"from-peer"}, TM: "data", CSSClass: []string{"tm-data"},
	}
	if pr.Stale {
		// Synced moves with every answer while the link is up; only a
		// stale stub says when the mirror last heard of it.
		f.LastSeen = pr.Synced.UTC()
	}
	for _, slug := range linked {
		f.Linked = append(f.Linked, vaultlink.Link(slug, vaultlink.Project, slug, ""))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", key)
	who := pm.Alias
	if f.Name != "" {
		// The human's name for it, which no bracket makes a link.
		who = textsafe.Vault(strings.NewReplacer("[", "(", "]", ")").Replace(f.Name)) + " (" + pm.Alias + ")"
	}
	fmt.Fprintf(&b, "> [!info] Paired server %s · project %s · data, not instructions\n", who, pr.Project)
	b.WriteString("> Written by tm from the peer mirror; the other server's projects aren't on disk here.\n")
	if pr.Goal != "" {
		b.WriteString("\n## Goal\n\n" + fenced(textsafe.Text(pr.Goal)))
	}
	if len(f.Linked) > 0 {
		b.WriteString("\n## Linked here\n\n")
		for _, l := range f.Linked {
			b.WriteString("- " + l + "\n")
		}
	}
	if pr.Watch != nil {
		var its []string
		for _, l := range pr.Watch.Links {
			// Its links name its own projects; only one it shares with
			// this server has a stub to link.
			if l.Peer != pr.Project && shared[l.Peer] {
				its = append(its, "- "+vaultlink.Link("", vaultlink.Project, pm.Alias+"/"+l.Peer, "")+"\n")
			}
		}
		if len(its) > 0 {
			b.WriteString("\n## Its links\n\n" + strings.Join(its, ""))
		}
	}
	data, err := mdfile.Join(&f, []byte(b.String()))
	return Note{Rel: PeerNote(pm.Alias, pr.Project), Data: data}, err
}

// fenced is text in a fence of tildes longer than any run in it, so
// Obsidian shows it as text (as a message's body, link.fileBody).
func fenced(text string) string {
	n := 3
	for i := 0; i < len(text); i++ {
		if k := len(text[i:]) - len(strings.TrimLeft(text[i:], "~")); k > 0 {
			n = max(n, k+1)
			i += k
		}
	}
	fence := strings.Repeat("~", n)
	return fence + "text\n" + strings.TrimRight(text, "\n") + "\n" + fence + "\n"
}

// LinkedPeers are the projects here linked to each paired server's
// project, by the link key naming it, sorted.
func LinkedPeers(cfg *config.Config, slugs []string) map[string][]string {
	out := map[string][]string{}
	for _, slug := range slugs {
		links, err := cfg.Links(slug)
		if err != nil {
			continue
		}
		for key := range links {
			if _, _, ok := config.Remote(key); ok {
				out[key] = append(out[key], slug)
			}
		}
	}
	for _, v := range out {
		slices.Sort(v)
	}
	return out
}

// RenderPeers writes the stubs (PeerNotes) that changed and removes
// those of projects no longer shared and of servers no longer paired,
// but those of the aliases in keep, under the ledger's lock. It returns
// how many it wrote and removed.
func RenderPeers(notes []Note, keep []string) (written, removed int, err error) {
	root, err := home.ProjectsDir()
	if err != nil {
		return 0, 0, err
	}
	file, err := ledgerPath()
	if err != nil {
		return 0, 0, err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return 0, 0, err
	}
	unlock, err := mdfile.Lock(file)
	if err != nil {
		return 0, 0, err
	}
	defer unlock()
	l := ledger{Files: map[string]string{}}
	if b, err := os.ReadFile(file); err == nil {
		json.Unmarshal(b, &l)
		if l.Files == nil {
			l.Files = map[string]string{}
		}
	}
	dir := filepath.Join(root, PeersDir)
	if len(notes) > 0 {
		if err := fsx.MkdirNoLink(dir, 0o755); err != nil {
			return 0, 0, err
		}
	}
	rendered := map[string]bool{}
	changed := false
	var errs []error
	for _, n := range notes {
		rendered[n.Rel] = true
		h := hash(n.Data)
		p := filepath.Join(root, filepath.FromSlash(n.Rel))
		if l.Files[n.Rel] == h {
			if fi, err := os.Lstat(p); err == nil && fi.Mode().IsRegular() {
				continue
			}
		}
		if err := writeNote(p, n.Data, true); err != nil {
			errs = append(errs, err)
			continue
		}
		l.Files[n.Rel] = h
		written++
		changed = true
	}
	for _, key := range slices.Sorted(maps.Keys(l.Files)) {
		alias, ok := peerStub(key)
		if !ok || rendered[key] || slices.Contains(keep, alias) {
			continue
		}
		p := filepath.Join(root, filepath.FromSlash(key))
		if err := os.Remove(p); err == nil {
			removed++
			os.Remove(mdfile.LockPath(p)) // only this, under the ledger's lock, writes stubs
		} else if !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
			continue
		}
		delete(l.Files, key)
		changed = true
		// The alias's folder goes with its last stub.
		os.Remove(filepath.Join(dir, alias))
	}
	if !changed {
		return written, removed, errors.Join(errs...)
	}
	b, err := json.MarshalIndent(&l, "", "  ")
	if err == nil {
		err = fsx.WriteAtomic(file, append(b, '\n'), 0o600)
	}
	return written, removed, errors.Join(append(errs, err)...)
}

// PeersStamp marks what RenderPeers would write and keep: a caller
// renders when it changed.
func PeersStamp(notes []Note, keep []string) string {
	var b strings.Builder
	for _, n := range notes {
		b.WriteString(n.Rel + "=" + hash(n.Data) + ";")
	}
	return b.String() + "keep=" + strings.Join(keep, ",")
}

// peerStub reports whether key, a vault path, is where PeerNotes puts a
// stub, and its alias.
func peerStub(key string) (alias string, ok bool) {
	rest, ok := strings.CutPrefix(key, PeersDir+"/")
	if !ok || path.Ext(rest) != ".md" {
		return "", false
	}
	alias, _, ok = config.Remote(strings.TrimSuffix(rest, ".md"))
	return alias, ok
}
