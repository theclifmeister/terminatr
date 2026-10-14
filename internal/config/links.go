package config

import (
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

// Link is one project's entry for another: which way this side lets
// messages go, and whether each message this side sends waits for the
// user's OK.
type Link struct {
	Allow string `json:"allow"`
	Ask   bool   `json:"ask"`
}

// Sends reports whether this side lets its coordinator write on the link.
func (l Link) Sends() bool { return l.Allow == LinkBoth || l.Allow == LinkSend }

// Receives reports whether this side accepts messages on the link.
func (l Link) Receives() bool { return l.Allow == LinkBoth || l.Allow == LinkReceive }

// linkSlugRE is a project slug's shape (project.ValidSlug).
var linkSlugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// Links returns project slug's links by the other project's slug,
// checked: each key a slug other than its own, each value a direction
// ("both") or a table { allow = "send", ask = true }.
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
		if !linkSlugRE.MatchString(other) {
			return nil, fmt.Errorf("%s: %s: %q is not a project slug", path, table, other)
		}
		if other == self {
			return nil, fmt.Errorf("%s: %s.%s: a project can't link to itself", path, table, other)
		}
		l, err := parseLink(raw[other])
		if err != nil {
			return nil, fmt.Errorf("%s: %s.%s %w", path, table, other, err)
		}
		out[other] = l
	}
	return out, nil
}

func parseLink(v any) (Link, error) {
	switch v := v.(type) {
	case string:
		if !slices.Contains(LinkAllows, v) {
			return Link{}, fmt.Errorf("must be %q, %q or %q, not %q", LinkBoth, LinkSend, LinkReceive, v)
		}
		return Link{Allow: v}, nil
	case map[string]any:
		var l Link
		for _, k := range slices.Sorted(maps.Keys(v)) {
			switch k {
			case "allow":
				s, ok := v[k].(string)
				if !ok || !slices.Contains(LinkAllows, s) {
					return Link{}, fmt.Errorf("allow must be %q, %q or %q", LinkBoth, LinkSend, LinkReceive)
				}
				l.Allow = s
			case "ask":
				b, ok := v[k].(bool)
				if !ok {
					return Link{}, fmt.Errorf("ask must be true or false")
				}
				l.Ask = b
			default:
				return Link{}, fmt.Errorf("has an unknown key %q (allow, ask)", k)
			}
		}
		if l.Allow == "" {
			return Link{}, fmt.Errorf("needs allow = %q, %q or %q", LinkBoth, LinkSend, LinkReceive)
		}
		return l, nil
	}
	return Link{}, fmt.Errorf("must be %q, %q, %q or a table { allow = …, ask = … }", LinkBoth, LinkSend, LinkReceive)
}

// LinkRefusal says why from's coordinator may not write to to's: "" when
// the link allows it, else a refusal code and its words. The settings
// are read, not the projects: callers check that both exist.
func (c *Config) LinkRefusal(from, to string) (code, msg string) {
	if from == to {
		return "self-link", from + " can't message itself"
	}
	mine, _ := c.Links(from)
	if !mine[to].Sends() {
		return "no-link", fmt.Sprintf("%s has no link to %s; links are the user's (Settings › Linked projects)", from, to)
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
// or removes it for LinkOff: the human's, from the settings popup and tm
// project link. An entry without ask is written as its direction alone.
func SetLink(slug, other string, l Link) error {
	if !linkSlugRE.MatchString(other) || other == slug {
		return fmt.Errorf("%q can't be linked from %s", other, slug)
	}
	table := "projects." + slug + ".links"
	if l.Allow == LinkOff || l.Allow == "" {
		return Unset(table, other)
	}
	if !slices.Contains(LinkAllows, l.Allow) {
		return fmt.Errorf("a link allows %q, %q or %q", LinkBoth, LinkSend, LinkReceive)
	}
	return Set(table, other, l)
}
