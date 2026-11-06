// Package releases says which of a repo's tags count as releases
// (docs/SPEC.md §5.1, History; design board-library, B11 to B13): the
// default rule or the project's release_tags patterns, only tags since
// the project's first record, and one release a commit. It reads tags
// locally, pinned to the repo tm knows and cached by the refs they are,
// never from a remote or a code host; git failing or absent, or a repo
// that moved, gives no tags and no error.
package releases

import (
	"cmp"
	"context"
	"crypto/sha256"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/gitx"
)

// Release is a tag that counts: Part names a monorepo's part ("api",
// "pkg"), "" when it has none, and Version is what to show after it
// ("1.2"). A tag only a release_tags pattern admits shows whole.
type Release struct {
	Tag     string
	Part    string
	Version string
	At      time.Time
	commit  string
	// fields is how many numbers Version has: v1.2.0 is more specific
	// than v1.2.
	fields int
}

// version is a version's shape: at least two numbers, so that v1, a
// moving major tag, and 20261106 aren't versions.
var version = regexp.MustCompile(`^[vV]?([0-9]+(?:\.[0-9]+)+)$`)

// partName is a part's shape: api, web/admin, @scope/pkg.
var partName = regexp.MustCompile(`^@?[A-Za-z0-9][A-Za-z0-9._/@-]*$`)

// notParts are words that say a name before a version isn't a part of
// the repo but a channel: nightly/1.2.3 and test/1.0.0 are no releases.
var notParts = map[string]bool{
	"nightly": true, "nightlies": true, "test": true, "tests": true, "testing": true, "build": true, "builds": true, "ci": true,
	"dev": true, "tmp": true, "temp": true, "wip": true, "canary": true, "snapshot": true, "snapshots": true, "alpha": true, "beta": true,
	"rc": true, "pre": true, "preview": true, "experimental": true, "draft": true, "staging": true, "latest": true, "stable": true,
	"edge": true, "backup": true, "archive": true, "old": true, "deprecated": true,
}

// parse reads a tag by the default rule (design board-library, the tag
// table): version-shaped, with or without a v, after release- or a part
// name. Anything after the numbers, a pre-release's -rc1, is no version.
func parse(tag string) (part, ver string, ok bool) {
	rest := tag
	if r, isRelease := strings.CutPrefix(rest, "release-"); isRelease {
		rest = r
	} else if i := strings.LastIndexAny(rest, "/@"); i >= 0 {
		part, rest = rest[:i], rest[i+1:]
		if !isPart(part) {
			return "", "", false
		}
		if l := strings.ToLower(part); l == "release" || l == "releases" {
			part = ""
		}
	}
	m := version.FindStringSubmatch(rest)
	if m == nil {
		return "", "", false
	}
	return part, m[1], true
}

func isPart(name string) bool {
	if !partName.MatchString(name) || !strings.ContainsFunc(name, func(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }) {
		return false
	}
	for _, word := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool { return strings.ContainsRune("/@-_.", r) }) {
		if notParts[word] {
			return false
		}
	}
	return true
}

// Classify says whether tag counts under patterns, a project's
// release_tags: none is the default rule, config.ReleaseTagsOff no tag,
// others replace the rule. A tag a pattern admits shows as the rule would
// show it when it has that shape, else whole.
func Classify(tag string, patterns []string) (Release, bool) {
	if config.ReleaseTagsAreOff(patterns) {
		return Release{}, false
	}
	part, ver, shaped := parse(tag)
	if len(patterns) > 0 {
		if !slices.ContainsFunc(patterns, func(p string) bool { return Match(p, tag) }) {
			return Release{}, false
		}
		if !shaped {
			return Release{Tag: tag, Version: tag, fields: 1}, true
		}
	} else if !shaped {
		return Release{}, false
	}
	return Release{Tag: tag, Part: part, Version: ver, fields: strings.Count(ver, ".") + 1}, true
}

// Match says whether tag fits pattern, where * is any text, / too.
func Match(pattern, tag string) bool {
	star := strings.Split(pattern, "*")
	if len(star) == 1 {
		return pattern == tag
	}
	rest, ok := strings.CutPrefix(tag, star[0])
	if !ok {
		return false
	}
	last := star[len(star)-1]
	if len(rest) < len(last) || !strings.HasSuffix(rest, last) {
		return false
	}
	rest = rest[:len(rest)-len(last)]
	for _, mid := range star[1 : len(star)-1] {
		i := strings.Index(rest, mid)
		if i < 0 {
			return false
		}
		rest = rest[i+len(mid):]
	}
	return true
}

// Tag is one tag as git has it.
type Tag struct {
	Name string
	// Commit is what the tag points at, an annotated tag's own commit.
	Commit string
	// At is the tagger's time, a lightweight tag's commit's time.
	At time.Time
}

// Count are the releases of a repo's tags, oldest first: the tags that
// count under patterns and aren't older than firstDay (a day in loc,
// the project's first record), and of the tags on one commit, for each
// part, only the most specific version. No firstDay, no releases: a
// project with no record has no curve to put them on.
func Count(tags []Tag, patterns []string, firstDay string, loc *time.Location) []Release {
	if firstDay == "" {
		return nil
	}
	type one struct{ commit, part string }
	best := map[one]Release{}
	for _, t := range tags {
		if t.At.In(loc).Format(time.DateOnly) < firstDay {
			continue
		}
		r, ok := Classify(t.Name, patterns)
		if !ok {
			continue
		}
		r.At, r.commit = t.At, t.Commit
		k := one{t.Commit, r.Part}
		if b, seen := best[k]; !seen || r.moreSpecific(b) {
			best[k] = r
		}
	}
	out := slices.Collect(maps.Values(best))
	slices.SortFunc(out, func(a, b Release) int {
		return cmp.Or(a.At.Compare(b.At), cmp.Compare(a.Tag, b.Tag))
	})
	return out
}

func (r Release) moreSpecific(than Release) bool {
	if r.fields != than.fields {
		return r.fields > than.fields
	}
	return r.Tag < than.Tag
}

// Timeout bounds a repo's read: Pin's calls and the tags'.
const Timeout = 10 * time.Second

// Cache keeps repos' tags in memory: a repo's refs are asked each time,
// which reads no object, and its tags' dates only when those changed.
type Cache struct {
	mu    sync.Mutex
	repos map[string]*cached
}

type cached struct {
	refs [sha256.Size]byte
	tags []Tag
}

// Tags are the repo's tags, none when git fails or is absent, the path
// is gone or isn't a checkout of its own. Newest first; a repo with more
// than git's output cap keeps its newest. The tags are read locally: a
// remote, or none, or another host make no difference.
func (c *Cache) Tags(repo string) []Tag {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.repos == nil {
		c.repos = map[string]*cached{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	tree, err := gitx.Pin(ctx, repo, repo)
	if err != nil {
		delete(c.repos, repo)
		return nil
	}
	o := gitx.Opts{Tree: tree, Timeout: Timeout}
	refs, _, err := gitx.Output(ctx, "", o, "for-each-ref", "--format=%(objectname) %(refname)", "refs/tags")
	if err != nil {
		delete(c.repos, repo)
		return nil
	}
	sum := sha256.Sum256([]byte(refs))
	if e := c.repos[repo]; e != nil && e.refs == sum {
		return e.tags
	}
	out, cut, err := gitx.Output(ctx, "", o, "for-each-ref", "--sort=-creatordate",
		"--format=%(objectname)%00%(*objectname)%00%(creatordate:unix)%00%(refname:lstrip=2)", "refs/tags")
	if err != nil {
		delete(c.repos, repo)
		return nil
	}
	tags := parseTags(out, cut)
	c.repos[repo] = &cached{refs: sum, tags: tags}
	return tags
}

// parseTags reads for-each-ref's lines; cut, the last one may be whole
// or not, so it goes. A tag with no date (of a tree or a blob) is none.
func parseTags(out string, cut bool) []Tag {
	lines := strings.Split(out, "\n")
	if cut && len(lines) > 0 {
		lines = lines[:len(lines)-1]
	}
	tags := make([]Tag, 0, len(lines))
	for _, l := range lines {
		f := strings.Split(l, "\x00")
		if len(f) != 4 || f[3] == "" {
			continue
		}
		unix, err := strconv.ParseInt(f[2], 10, 64)
		if err != nil {
			continue
		}
		tags = append(tags, Tag{Name: f[3], Commit: cmp.Or(f[1], f[0]), At: time.Unix(unix, 0).UTC()})
	}
	return tags
}
