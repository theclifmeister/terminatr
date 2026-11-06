package thread

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// A page's or a note's own title (docs/SPEC.md §4, Library tab): an HTML
// title or a Markdown first heading, found in the file's first 8 KB. A
// library file is whatever a thread or a linked project wrote: the title
// comes out cleaned and on one line, or not at all.

const (
	// docTitleHead is how much of a file its title is looked for in.
	docTitleHead = 8 << 10
	// DocTitleMax is a title's most, in characters.
	DocTitleMax = 200
)

func isHTML(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".html", ".htm":
		return true
	}
	return false
}

func isMarkdown(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown":
		return true
	}
	return false
}

// hasDocTitle says a file of this name may have a title: nothing else is
// opened to look.
func hasDocTitle(name string) bool { return isHTML(name) || isMarkdown(name) }

// DocTitle is the title in head, the start of file name ("" for none): at
// most the first 8 KB of it count, and a title that says only the file's
// name is none.
func DocTitle(name string, head []byte) string {
	cut := len(head) > docTitleHead
	if cut {
		head = head[:docTitleHead]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return ""
	}
	var title string
	switch {
	case isHTML(name):
		title = htmlTitle(head)
	case isMarkdown(name):
		title = markdownHeading(head, cut)
	}
	title = textsafe.Line(strings.ToValidUTF8(title, ""), DocTitleMax)
	if title == name {
		return ""
	}
	return title
}

// readDocTitle is DocTitle of what r holds, for a name that may have one.
func readDocTitle(name string, r io.Reader) string {
	if !hasDocTitle(name) {
		return ""
	}
	head, err := io.ReadAll(io.LimitReader(r, docTitleHead+1))
	if err != nil {
		return ""
	}
	return DocTitle(name, head)
}

// FileDocTitle is the title of file name at path, never through a link: a
// file it can't read has none.
func FileDocTitle(path, name string) string {
	if !hasDocTitle(name) {
		return ""
	}
	f, err := fsx.OpenNoLink(path, os.O_RDONLY, 0)
	if err != nil {
		return ""
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return ""
	}
	return readDocTitle(name, f)
}

// htmlTitle is the document's title: the head's, so a title inside a
// comment, or an SVG's further down the page, isn't taken for it. One the
// head's first 8 KB don't close is none.
func htmlTitle(head []byte) string {
	z := html.NewTokenizer(bytes.NewReader(head))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return ""
		case html.EndTagToken:
			if name, _ := z.TagName(); atom.Lookup(name) == atom.Head {
				return ""
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			switch atom.Lookup(name) {
			case atom.Title:
				text, next := "", z.Next()
				if next == html.TextToken {
					text, next = string(z.Text()), z.Next()
				}
				if next != html.EndTagToken {
					return ""
				}
				return text
			case atom.Html, atom.Head, atom.Meta, atom.Link, atom.Base, atom.Script, atom.Style, atom.Noscript, atom.Template:
			default:
				return ""
			}
		}
	}
}

// markdownHeading is the text's first heading, past any front matter and
// outside code fences: a line of # and a space, or a line underlined with
// = or -. When cut, the last line may be a piece of one and doesn't count.
func markdownHeading(head []byte, cut bool) string {
	text := strings.TrimPrefix(string(head), "\xef\xbb\xbf")
	lines := strings.Split(text, "\n")
	if cut {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			if t := strings.TrimSpace(lines[i]); t == "---" || t == "..." {
				lines = lines[i+1:]
				break
			}
		}
	}
	fence, last := "", ""
	for _, raw := range lines {
		l := strings.TrimSpace(raw)
		if indent := len(raw) - len(strings.TrimLeft(raw, " ")); indent > 3 {
			last = l
			continue
		}
		if fence != "" {
			if strings.HasPrefix(l, fence) {
				fence = ""
			}
			continue
		}
		switch {
		case strings.HasPrefix(l, "```"):
			fence, last = "```", ""
			continue
		case strings.HasPrefix(l, "~~~"):
			fence, last = "~~~", ""
			continue
		}
		if rest := strings.TrimLeft(l, "#"); len(l)-len(rest) >= 1 && len(l)-len(rest) <= 6 && (rest == "" || rest[0] == ' ' || rest[0] == '\t') {
			if h := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(rest), "#")); h != "" {
				return h
			}
			last = ""
			continue
		}
		if last != "" && l != "" && (strings.Trim(l, "=") == "" || strings.Trim(l, "-") == "") {
			return last
		}
		last = l
	}
	return ""
}

// publishedPages are the design pages of the designs list by their design
// task: what a page of that task's threads opens. A list that can't be
// read, or a project with no designs, gives none. An archived design's
// page is still where it was.
func publishedPages(p *project.Project) map[string]string {
	designs, err := p.Designs()
	if err != nil {
		return nil
	}
	var out map[string]string
	for _, d := range designs {
		url, err := project.CheckDesignURL(d.Artifact)
		if err != nil || url == "" || d.Task == "" {
			continue
		}
		if out == nil {
			out = map[string]string{}
		}
		if _, ok := out[d.Task]; !ok {
			out[d.Task] = url
		}
	}
	return out
}

// publishedOf is the published page of a file of task: only a page has one.
func publishedOf(pages map[string]string, task, name string) string {
	if task == "" || !isHTML(name) {
		return ""
	}
	return pages[task]
}
