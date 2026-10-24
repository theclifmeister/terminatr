// Package vaultlink writes the wikilinks tm puts in the files it owns
// and reads them back (docs/SPEC.md §5.1). The projects folder is one
// Obsidian vault, and every project has its own T12 and t-0001, so a
// link names its note from the vault's root and shows the id as its
// alias: [[terminatr/tasks/T12|T12]]. Plain turns links back into their
// aliases wherever tm writes to a terminal or an agent.
//
// Links are written only once the projects' front matter is YAML
// (mdfile.WritesYAML): a tm older than 0.51.0 may run until then, and it
// reads ids, not links. Until then Link gives the alias alone.
package vaultlink

import (
	"regexp"
	"strings"

	"github.com/theclifmeister/terminatr/internal/mdfile"
)

// Kind is what a link points at; each lives at its own place in the
// vault (Target).
type Kind int

const (
	// Project is <slug>/PROJECT. Its id is the project's slug, or a
	// paired server's project as a link key names it ("mini/acme-api"),
	// whose stub is _peers/<alias>/<slug>.
	Project Kind = iota
	// Task is <slug>/tasks/<ref>, ref as "T12".
	Task
	// Thread is <slug>/threads/<id>/<id>, id as "t-0001".
	Thread
	// ThreadFile is a file of a thread's folder, <slug>/threads/<id>/<name>,
	// id as "t-0001/task".
	ThreadFile
	// Memory is <slug>/memory/<name>, name as "ci" or "memory/ci.md".
	Memory
	// Message is <slug>/messages/<other>/<id>, id as "<other>/M-12" with
	// other a link key: a paired server's project's folder is
	// messages/@<alias>/<slug>.
	Message
	// Design is a design's note, <slug>/designs/<name>.
	Design
)

// Target is the vault path a link of kind k with id points at, without
// .md; "" when id can't be one (empty, a part that is "." or "..", or a
// character a link can't hold).
func Target(slug string, k Kind, id string) string {
	t := target(slug, k, id)
	if strings.Contains(t, "//") || strings.HasSuffix(t, "/") {
		return ""
	}
	return t
}

func target(slug string, k Kind, id string) string {
	if k != Project && !validPart(slug) || !validID(id) {
		return ""
	}
	switch k {
	case Project:
		if alias, other, ok := strings.Cut(id, "/"); ok {
			if strings.Contains(other, "/") {
				return ""
			}
			return "_peers/" + alias + "/" + other
		}
		return id + "/PROJECT"
	case Task:
		return slug + "/tasks/" + one(id)
	case Thread:
		return slug + "/threads/" + one(id) + "/" + one(id)
	case ThreadFile:
		if id, name, ok := strings.Cut(id, "/"); ok && !strings.Contains(name, "/") {
			return slug + "/threads/" + id + "/" + strings.TrimSuffix(name, ".md")
		}
	case Memory:
		name := strings.TrimSuffix(strings.TrimPrefix(id, "memory/"), ".md")
		return slug + "/memory/" + one(name)
	case Design:
		return slug + "/designs/" + one(id)
	case Message:
		parts := strings.Split(id, "/")
		switch len(parts) {
		case 2:
			return slug + "/messages/" + id
		case 3:
			return slug + "/messages/@" + id
		}
	}
	return ""
}

// one is s when it is one part of a path, else "" (which Target turns
// into no link).
func one(s string) string {
	if strings.Contains(s, "/") {
		return ""
	}
	return s
}

// Link is the wikilink to id of kind k in project slug, showing alias
// (id when empty): [[slug/tasks/T12|T12]]. It is the alias alone when
// links aren't written yet (mdfile.WritesYAML) or id can't be linked.
func Link(slug string, k Kind, id, alias string) string {
	if alias == "" {
		alias = id
	}
	alias = cleanAlias(alias)
	if !mdfile.WritesYAML() {
		return alias
	}
	t := Target(slug, k, id)
	if t == "" {
		return alias
	}
	return "[[" + t + "|" + alias + "]]"
}

// Ref links s when it is a task ref or a thread id of project slug, and
// is s unchanged otherwise.
func Ref(slug, s string) string {
	switch {
	case refRE.MatchString(s):
		return Link(slug, Task, s, s)
	case threadRE.MatchString(s):
		return Link(slug, Thread, s, s)
	}
	return s
}

// The task ref and thread id shapes, as tasks.RefPattern and
// tasks.ThreadIDPattern (tasks imports this package).
var (
	refRE    = regexp.MustCompile(`^T[1-9][0-9]{0,8}$`)
	threadRE = regexp.MustCompile(`^t-[0-9]{4,}$`)
)

// linkRE is a wikilink: its target, then its alias when it has one.
var linkRE = regexp.MustCompile(`\[\[([^\[\]|\n]+?)(?:\|([^\[\]\n]*))?\]\]`)

// Plain is s with each wikilink turned into what it shows: its alias,
// else its target's last part ([[a/b/T12]] is T12). Text without links
// is unchanged, so Plain reads what a tm older than links wrote too.
func Plain(s string) string {
	if !strings.Contains(s, "[[") {
		return s
	}
	return linkRE.ReplaceAllStringFunc(s, func(l string) string {
		m := linkRE.FindStringSubmatch(l)
		if a := strings.TrimSpace(m[2]); a != "" {
			return a
		}
		t := strings.TrimSpace(m[1])
		t, _, _ = strings.Cut(t, "#")
		return t[strings.LastIndexByte(t, '/')+1:]
	})
}

// Each calls fn with the target and alias of each wikilink in s, in
// order; the alias is "" when the link has none.
func Each(s string, fn func(target, alias string)) {
	for _, m := range linkRE.FindAllStringSubmatch(s, -1) {
		fn(m[1], m[2])
	}
}

// Rename is s with the links into project from pointed at project to,
// after tm project rename: a target in from's folder, or in another
// project's messages with from (<x>/messages/from/…). The link to from's
// PROJECT that shows from shows to. Text outside links is unchanged.
func Rename(s, from, to string) string {
	if !strings.Contains(s, "[[") || from == "" || to == "" {
		return s
	}
	return linkRE.ReplaceAllStringFunc(s, func(l string) string {
		m := linkRE.FindStringSubmatch(l)
		parts := strings.Split(m[1], "/")
		changed := false
		if parts[0] == from {
			parts[0], changed = to, true
		}
		if len(parts) > 3 && parts[1] == "messages" && parts[2] == from {
			parts[2], changed = to, true
		}
		if !changed {
			return l
		}
		t := strings.Join(parts, "/")
		alias := m[2]
		if t == to+"/PROJECT" && alias == from {
			alias = to
		}
		if alias == "" && !strings.HasSuffix(l, "|]]") {
			return "[[" + t + "]]"
		}
		return "[[" + t + "|" + alias + "]]"
	})
}

// validID reports whether id can name a note in a link: no character
// that ends or splits one, no part that is empty, "." or "..".
func validID(id string) bool {
	if id == "" || strings.ContainsAny(id, "[]|#^\\\n\r") {
		return false
	}
	for _, p := range strings.Split(id, "/") {
		if !validPart(p) {
			return false
		}
	}
	return true
}

func validPart(p string) bool {
	return p != "" && p != "." && p != ".." && !strings.ContainsAny(p, "/[]|#^\\\n\r")
}

// cleanAlias is alias as a link can show it: on one line, without the
// brackets and bar that would end the link.
func cleanAlias(alias string) string {
	alias = strings.NewReplacer("[", "(", "]", ")", "|", "/").Replace(alias)
	return strings.Join(strings.Fields(alias), " ")
}
