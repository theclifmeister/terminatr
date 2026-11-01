package search

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// MaxChunk is about the most bytes a chunk holds: a section longer than
// this is split again at paragraphs (then lines, then bytes).
const MaxChunk = 2048

// Chunk is one piece of the project's text the index finds: a note's
// section, a task, a thread's report section, a journal line. Its text
// stays in its file; Off and Len place it there (in the file's text as
// Text reads it).
type Chunk struct {
	// ID names the chunk across the project: note:decisions#ruled-out,
	// T914, t-0833/report.3#next, j:20261101T095912Z, lib:t-0359/x.md.
	ID string `json:"id"`
	// Kind is the source group: one of Kinds.
	Kind string `json:"kind"`
	// Title is what a hit shows: the file's title, then the section's
	// heading.
	Title string `json:"title"`
	// Time is when it was written, as best known: a journal line's or
	// follow-up's timestamp, a task's updated date, else the file's
	// modification time.
	Time   time.Time `json:"time"`
	Task   string    `json:"task,omitempty"`
	Thread string    `json:"thread,omitempty"`
	// Coordinator: only the coordinator (and the human) may see it: an
	// inbox item or a note whose audience is the coordinator.
	Coordinator bool `json:"coordinator,omitempty"`
	// Path is the file, relative to the project folder, slash-separated;
	// Member the file inside it when Path is a tar.gz archive.
	Path   string `json:"path"`
	Member string `json:"member,omitempty"`
	Off    int    `json:"off"`
	Len    int    `json:"len"`
}

// The kinds of chunk, by source.
const (
	KindNote    = "notes"   // memory/*.md
	KindContext = "context" // CONTEXT.md, PROJECT.md
	KindTask    = "tasks"   // TASKS.md, tasks/ARCHIVE.md
	KindThread  = "threads" // briefs, follow-ups, reports
	KindJournal = "journal" // JOURNAL.md, journal/*.md.gz
	KindDesign  = "designs" // designs/*.md
	KindLibrary = "library" // threads/<id>/library text files
	KindInbox   = "inbox"   // inbox (coordinator only)
)

// Kinds are all kinds, in the order tm search --in lists them.
var Kinds = []string{KindNote, KindTask, KindThread, KindJournal, KindDesign, KindLibrary, KindContext, KindInbox}

// piece is a chunk being made: its place in the text and its head (the
// words that weigh as a title: the section's heading or the file's
// title).
type piece struct {
	Chunk
	head string
	text string // the whole text Off and Len are in
}

// section is a run of a markdown text under one heading.
type section struct {
	heading    string // "" for the text before the first heading
	level      int
	off, end   int
	headingEnd int // where the heading line ends (off when none)
}

// headingRE is a markdown heading of level 1 to 3.
var headingRE = regexp.MustCompile(`^(#{1,3})[ \t]+(.+?)[ \t#]*$`)

// sections splits text[start:] at its headings of level 1 to maxLevel,
// outside code fences.
func sections(text string, start, maxLevel int) []section {
	var out []section
	cur := section{off: start, headingEnd: start}
	fence := ""
	for i := start; i < len(text); {
		j := strings.IndexByte(text[i:], '\n')
		next := len(text)
		if j >= 0 {
			next = i + j + 1
		}
		line := strings.TrimRight(text[i:next], "\r\n")
		t := strings.TrimSpace(line)
		switch {
		case fence != "":
			if strings.HasPrefix(t, fence) {
				fence = ""
			}
		case strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~"):
			fence = t[:3]
		default:
			if m := headingRE.FindStringSubmatch(line); m != nil && len(m[1]) <= maxLevel {
				cur.end = i
				out = append(out, cur)
				cur = section{heading: strings.TrimSpace(m[2]), level: len(m[1]), off: i, headingEnd: next}
			}
		}
		i = next
	}
	cur.end = len(text)
	return append(out, cur)
}

// blank reports whether s holds only white space.
func blank(s string) bool { return strings.TrimSpace(s) == "" }

// pieces cuts text[off:end] into parts of at most MaxChunk bytes, at
// blank lines first, then at line ends, then at rune boundaries.
func pieces(text string, off, end int) [][2]int {
	if end-off <= MaxChunk {
		return [][2]int{{off, end}}
	}
	var out [][2]int
	for off < end {
		if end-off <= MaxChunk {
			out = append(out, [2]int{off, end})
			break
		}
		lim := off + MaxChunk
		cut := strings.LastIndex(text[off:lim], "\n\n")
		if cut <= 0 {
			cut = strings.LastIndexByte(text[off:lim], '\n')
		}
		if cut <= 0 {
			cut = lim - off
			for cut > 0 && !utf8.RuneStart(text[off+cut]) {
				cut--
			}
			if cut == 0 {
				cut = lim - off
			}
		} else {
			cut++
		}
		out = append(out, [2]int{off, off + cut})
		off += cut
	}
	return out
}

// anchor is a heading as an id's #part: lower case letters and digits,
// runs of anything else as one '-', at most 40 bytes.
func anchor(h string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(h) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
			if b.Len() >= 40 {
				break
			}
		} else {
			dash = true
		}
	}
	if b.Len() == 0 {
		return "s"
	}
	return b.String()
}

// markdown cuts a markdown text (from start, past any front matter)
// into pieces: the text before the first heading, or under a first
// heading of level 1 with nothing before it, is base, each other
// section base#<anchor>, each further part of a long one -2, -3….
// Sections that hold only their heading are left out. title is the
// file's title.
func markdown(text string, start, maxLevel int, base, title string, c Chunk) []piece {
	var out []piece
	for i, s := range sections(text, start, maxLevel) {
		top := i == 1 && s.level == 1 && blank(text[start:s.off])
		if blank(text[s.headingEnd:s.end]) && (s.heading != "" || blank(text[s.off:s.end])) {
			continue
		}
		id, head, t := base, title, title
		if top {
			head = s.heading
			if title == "" {
				t = s.heading
			}
		} else if s.heading != "" {
			id = base + "#" + anchor(s.heading)
			head = s.heading
			if title == "" || title == s.heading {
				t = s.heading
			} else {
				t = title + ": " + s.heading
			}
		}
		for k, pr := range pieces(text, s.off, s.end) {
			p := piece{Chunk: c, head: head}
			p.ID, p.Title, p.Off, p.Len = id, t, pr[0], pr[1]-pr[0]
			if k > 0 {
				if strings.Contains(id, "#") {
					p.ID = id + "-" + strconv.Itoa(k+1)
				} else {
					p.ID = id + "#" + strconv.Itoa(k+1)
				}
			}
			out = append(out, p)
		}
	}
	return out
}

// plain cuts a text that isn't markdown into pieces: base, base#2….
func plain(text, base, title string, c Chunk) []piece {
	var out []piece
	for k, pr := range pieces(text, 0, len(text)) {
		if blank(text[pr[0]:pr[1]]) {
			continue
		}
		p := piece{Chunk: c, head: title}
		p.ID, p.Title, p.Off, p.Len = base, title, pr[0], pr[1]-pr[0]
		if k > 0 {
			p.ID = base + "#" + strconv.Itoa(k+1)
		}
		out = append(out, p)
	}
	return out
}

// frontEnd is where text's YAML front matter ends (0 when it has none).
func frontEnd(text string) int {
	if !strings.HasPrefix(text, "---\n") && !strings.HasPrefix(text, "---\r\n") {
		return 0
	}
	i := strings.IndexByte(text, '\n') + 1
	for i < len(text) {
		j := strings.IndexByte(text[i:], '\n')
		next := len(text)
		if j >= 0 {
			next = i + j + 1
		}
		if strings.TrimRight(text[i:next], "\r\n") == "---" {
			return next
		}
		i = next
	}
	return 0
}

// firstHeading is text's first heading from start, else "".
func firstHeading(text string, start int) string {
	for _, s := range sections(text, start, 3) {
		if s.heading != "" {
			return s.heading
		}
	}
	return ""
}

var (
	// taskRefRE is a task id in running text.
	taskRefRE = regexp.MustCompile(`\bT[1-9][0-9]*\b`)
	// threadRefRE is a thread id in running text.
	threadRefRE = regexp.MustCompile(`\bt-[0-9]{4,}\b`)
	// linkRE is an Obsidian link's [[target|label]] or [[target]].
	linkRE = regexp.MustCompile(`\[\[[^\]|]*\|([^\]]*)\]\]|\[\[([^\]]*)\]\]`)
)

// unlink is s with Obsidian links as their labels.
func unlink(s string) string {
	if !strings.Contains(s, "[[") {
		return s
	}
	return linkRE.ReplaceAllStringFunc(s, func(m string) string {
		sub := linkRE.FindStringSubmatch(m)
		if sub[1] != "" {
			return sub[1]
		}
		return sub[2]
	})
}

var (
	scriptRE = regexp.MustCompile(`(?is)<(script|style)\b.*?</(script|style)\s*>`)
	tagRE    = regexp.MustCompile(`(?s)<[^>]*>`)
	blankRE  = regexp.MustCompile(`\n[ \t]*\n(?:[ \t]*\n)+`)
)

// htmlText is an HTML page's text: no scripts, styles or tags, entities
// for <, >, & and quotes undone, blank runs as one blank line.
func htmlText(s string) string {
	s = scriptRE.ReplaceAllString(s, "")
	s = tagRE.ReplaceAllStringFunc(s, func(t string) string {
		name := strings.TrimLeft(t, "</")
		if i := strings.IndexAny(name, " \t\n/>"); i >= 0 {
			name = name[:i]
		}
		switch strings.ToLower(name) {
		case "p", "br", "li", "tr", "div", "h1", "h2", "h3", "h4", "pre", "table", "ul", "ol":
			return "\n"
		}
		return " "
	})
	s = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&", "&quot;", `"`, "&#39;", "'", "&nbsp;", " ").Replace(s)
	return blankRE.ReplaceAllString(s, "\n\n")
}
