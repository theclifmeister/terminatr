package config

import (
	"bytes"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sort"
)

// Links between projects' coordinators (docs/SPEC.md §7.9): each project
// lists, in [projects.<slug>.links], the other projects its coordinator
// may write to and hear from. A message goes from A to B only when A's
// entry for B allows sending and B's entry for A allows receiving. The
// human's setting, like every other; a project's own, never [defaults]'s.

// The directions a link entry allows.
const (
	LinkBoth    = "both"
	LinkSend    = "send"
	LinkReceive = "receive"
	// LinkOff is no entry: the popup's and tm project link's word for
	// removing one.
	LinkOff = "off"
)

// LinkAllows are the values of a link entry's allow, in the order the
// settings popup steps through them after off.
var LinkAllows = []string{LinkBoth, LinkSend, LinkReceive}

// What a link entry's files says of files over the link (docs/SPEC.md
// §16.13), for this side: none sent or taken, each offered file waits
// for the user (the default), or this side takes them without asking.
const (
	FilesOff  = "off"
	FilesAsk  = "ask"
	FilesAuto = "auto"
)

// LinkFiles are the values of files, in the order F steps through them.
var LinkFiles = []string{FilesAsk, FilesAuto, FilesOff}

// Link is one project's entry for another: which way this side lets
// messages go, whether each message this side sends waits for the
// user's OK, and what this side does with files ("" is ask).
type Link struct {
	Allow string `json:"allow"`
	Ask   bool   `json:"ask"`
	Files string `json:"files,omitempty"`
}

// FilesMode is the entry's files: ask when it doesn't say.
func (l Link) FilesMode() string {
	if l.Files == "" {
		return FilesAsk
	}
	return l.Files
}

// Sends reports whether this side lets its coordinator write on the link.
func (l Link) Sends() bool { return l.Allow == LinkBoth || l.Allow == LinkSend }

// Receives reports whether this side accepts messages on the link.
func (l Link) Receives() bool { return l.Allow == LinkBoth || l.Allow == LinkReceive }

// linkSlugRE is a project slug's shape (project.ValidSlug); remoteRE a
// project on a paired server, "<alias>/<slug>" (docs/SPEC.md §16.11),
// the alias as tm peer names the server (peer.CheckAlias).
var (
	linkSlugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	remoteRE   = regexp.MustCompile(`^([a-z0-9][a-z0-9-]{0,31})/([a-z0-9][a-z0-9-]{0,63})$`)
)

// Remote splits a link key naming a project on a paired server,
// "mini/acme-api", into the server's alias and the project's slug; ok
// is false for any other key.
func Remote(key string) (alias, slug string, ok bool) {
	m := remoteRE.FindStringSubmatch(key)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// ValidLinkKey reports whether key can name the other side of a link: a
// project slug here, or "<alias>/<slug>" on a paired server.
func ValidLinkKey(key string) bool {
	_, _, remote := Remote(key)
	return remote || linkSlugRE.MatchString(key)
}

// Links returns project slug's links by the other project's slug,
// checked: each key a slug other than its own, or "<alias>/<slug>" for
// a project on a paired server, each value a direction ("both") or a
// table { allow = "send", ask = true }. A paired server's project asks
// unless its entry says ask = false.
func (c *Config) Links(slug string) (map[string]Link, error) {
	if c == nil {
		return nil, nil
	}
	r, ok := c.projects[slug]
	if !ok || r.Links == nil {
		return nil, nil
	}
	return parseLinks(r.Links, c.Path, "projects."+slug+".links", slug)
}

func parseLinks(raw map[string]any, path, table, self string) (map[string]Link, error) {
	out := map[string]Link{}
	for _, other := range slices.Sorted(maps.Keys(raw)) {
		if !ValidLinkKey(other) {
			return nil, fmt.Errorf("%s: %s: %q is not a project slug (or <server>/<slug>)", path, table, other)
		}
		if other == self {
			return nil, fmt.Errorf("%s: %s.%s: a project can't link to itself", path, table, other)
		}
		l, askSet, err := parseLink(raw[other])
		if err != nil {
			return nil, fmt.Errorf("%s: %s.%s %w", path, table, other, err)
		}
		if _, _, remote := Remote(other); remote && !askSet {
			l.Ask = true
		}
		out[other] = l
	}
	return out, nil
}

// parseLink reads an entry; askSet says it gives ask.
func parseLink(v any) (l Link, askSet bool, err error) {
	switch v := v.(type) {
	case string:
		if !slices.Contains(LinkAllows, v) {
			return Link{}, false, fmt.Errorf("must be %q, %q or %q, not %q", LinkBoth, LinkSend, LinkReceive, v)
		}
		return Link{Allow: v}, false, nil
	case map[string]any:
		for _, k := range slices.Sorted(maps.Keys(v)) {
			switch k {
			case "allow":
				s, ok := v[k].(string)
				if !ok || !slices.Contains(LinkAllows, s) {
					return Link{}, false, fmt.Errorf("allow must be %q, %q or %q", LinkBoth, LinkSend, LinkReceive)
				}
				l.Allow = s
			case "ask":
				b, ok := v[k].(bool)
				if !ok {
					return Link{}, false, fmt.Errorf("ask must be true or false")
				}
				l.Ask, askSet = b, true
			case "files":
				f, ok := v[k].(string)
				if !ok || !slices.Contains(LinkFiles, f) {
					return Link{}, false, fmt.Errorf("files must be %q, %q or %q", FilesOff, FilesAsk, FilesAuto)
				}
				if f != FilesAsk {
					l.Files = f
				}
			default:
				return Link{}, false, fmt.Errorf("has an unknown key %q (allow, ask, files)", k)
			}
		}
		if l.Allow == "" {
			return Link{}, false, fmt.Errorf("needs allow = %q, %q or %q", LinkBoth, LinkSend, LinkReceive)
		}
		return l, askSet, nil
	}
	return Link{}, false, fmt.Errorf("must be %q, %q, %q or a table { allow = …, ask = …, files = … }", LinkBoth, LinkSend, LinkReceive)
}

// LinkRefusal says why from's coordinator may not write to to's: "" when
// the link allows it, else a refusal code and its words. The settings
// are read, not the projects: callers check that both exist. For a
// project on a paired server only from's side is here: that server
// checks its own when the message arrives.
func (c *Config) LinkRefusal(from, to string) (code, msg string) {
	if from == to {
		return "self-link", from + " can't message itself"
	}
	mine, _ := c.Links(from)
	if !mine[to].Sends() {
		return "no-link", fmt.Sprintf("%s has no link to %s; links are the user's (Settings › Linked projects)", from, to)
	}
	if _, _, remote := Remote(to); remote {
		return "", ""
	}
	theirs, _ := c.Links(to)
	if !theirs[from].Receives() {
		return "not-linked-back", fmt.Sprintf("%s doesn't accept messages from %s; ask the user to allow it on %s's side", to, from, to)
	}
	return "", ""
}

// LinkPeers lists, sorted, the projects either side's entry names for
// slug: those slug lists and those that list slug.
func (c *Config) LinkPeers(slug string) []string {
	if c == nil {
		return nil
	}
	seen := map[string]bool{}
	mine, _ := c.Links(slug)
	for other := range mine {
		seen[other] = true
	}
	for other := range c.projects {
		if other == slug {
			continue
		}
		if theirs, _ := c.Links(other); theirs != nil {
			if _, ok := theirs[slug]; ok {
				seen[other] = true
			}
		}
	}
	out := slices.Collect(maps.Keys(seen))
	sort.Strings(out)
	return out
}

// Mirror is the entry the other side needs for a link this side set to
// allow: both for both, receive for send, send for receive.
func Mirror(allow string) string {
	switch allow {
	case LinkSend:
		return LinkReceive
	case LinkReceive:
		return LinkSend
	}
	return allow
}

// SetLink writes project slug's entry for other ([projects.<slug>.links]),
// or removes it for LinkOff, leaving the other side as it is: only the
// ask switch is one side's alone (SetLinkPair links the pair). An entry
// without ask is written as its direction alone.
func SetLink(slug, other string, l Link) error {
	return edit(func(data []byte) ([]byte, error) { return editLink(data, slug, other, l) })
}

// SetLinkPair links a and b in one write (docs/SPEC.md §7.9): a's entry
// is la, b's the mirror of la's direction (Mirror) with b's ask askB;
// LinkOff removes both. Linking is one act of the user's, never a side
// at a time.
func SetLinkPair(a, b string, la Link, askB bool) error {
	lb := Link{Allow: Mirror(la.Allow), Ask: askB, Files: la.Files}
	return edit(func(data []byte) ([]byte, error) {
		out, err := editLink(data, a, b, la)
		if err != nil {
			return nil, err
		}
		return editLink(out, b, a, lb)
	})
}

func editLink(data []byte, slug, other string, l Link) ([]byte, error) {
	if !ValidLinkKey(other) || !linkSlugRE.MatchString(slug) || other == slug {
		return nil, fmt.Errorf("%q can't be linked from %s", other, slug)
	}
	table := "projects." + slug + ".links"
	if l.Allow == LinkOff || l.Allow == "" {
		out := Remove(data, table, other)
		if bytes.Equal(out, data) && sets(data, table, other) {
			return nil, ErrForm
		}
		return out, nil
	}
	if !slices.Contains(LinkAllows, l.Allow) {
		return nil, fmt.Errorf("a link allows %q, %q or %q", LinkBoth, LinkSend, LinkReceive)
	}
	if l.Files == FilesAsk {
		l.Files = ""
	}
	if l.Files != "" && !slices.Contains(LinkFiles, l.Files) {
		return nil, fmt.Errorf("a link's files are %q, %q or %q", FilesOff, FilesAsk, FilesAuto)
	}
	if _, _, remote := Remote(other); remote {
		return Edit(data, table, other, remoteLink(l))
	}
	return Edit(data, table, other, l)
}

// remoteLink is an entry for a paired server's project as it is
// written: ask is on unless it says ask = false.
type remoteLink Link

// Half reports whether an entry and the other side's for it don't
// match: one side allows a direction the other doesn't, so the link
// carries less than its entries say. Only a hand edit of config.toml
// makes one; tm doctor and tm link list say so.
func Half(mine, theirs Link, hasMine, hasTheirs bool) bool {
	if !hasMine && !hasTheirs {
		return false
	}
	return !hasMine || !hasTheirs || mine.Sends() != theirs.Receives() || mine.Receives() != theirs.Sends()
}
