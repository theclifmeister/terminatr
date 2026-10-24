package textsafe

import (
	"strings"
)

// Vault is Markdown that tm stores but didn't write (a thread's report,
// its todos) as the vault may show it: opening the note in Obsidian
// must never fetch a URL or run a plugin's code (docs/SPEC.md §5.1,
// Vault). It stays Markdown, with outside code:
//
//   - raw HTML escaped: every "<" is "&lt;", so no tag, comment or
//     HTML block is left (an autolink shows as text);
//   - images and embeds turned into links: "![alt](https://…)" is
//     "[alt](https://…)" and "![[note]]" is "[[note]]" (the run of "!"
//     before a "[" goes);
//   - an inline query (`= …`, `$= …`, Dataview's) written as text: its
//     backticks are entities and the rest escaped.
//
// A fenced code block keeps its text, but its language becomes "text"
// unless it is one no common plugin runs (codeLangs): dataviewjs,
// templater or js-engine blocks show as code and run nothing.
//
// Where it can't be sure what Obsidian will read as code it treats the
// text as not code: a fence inside a list or a quote, and the rest of
// a paragraph after a backtick run that closes on no later line's
// place it could, have the rules above applied inside too. That may
// show an "&lt;" in code, never a tag outside it. Vault of its own
// output is that output.
func Vault(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n") // each a line's end to Markdown
	lines := strings.Split(s, "\n")
	var f *fence
	unsure := false // a backtick run on this paragraph had no closer on its line
	for i, l := range lines {
		if f != nil {
			switch {
			case f.closedBy(l):
				f = nil
			case f.unsure:
				lines[i] = vaultLine(l, true)
			}
			continue
		}
		if open, ok := openFence(l); ok {
			lines[i] = open.line
			f = &open.fence
			unsure = false
			continue
		}
		if strings.TrimSpace(l) == "" {
			unsure = false
			continue
		}
		lines[i] = vaultLine(l, unsure)
		if _, ok := openFence(lines[i]); ok {
			// A run of backticks starts it whose info string had one,
			// which vaultLine took: it mustn't open a fence now.
			prefix, core := containers(lines[i])
			lines[i] = prefix + "&#96;" + core[1:]
		}
		if !unsure && openRun(l) {
			unsure = true
		}
	}
	return strings.Join(lines, "\n")
}

// fence is an open fenced code block: its character and length, and
// whether it is unsure (not at the line's start: in a list or a quote,
// where where it ends depends on the container).
type fence struct {
	ch     byte
	n      int
	unsure bool
}

// closedBy reports whether l may close f: after any indent, quote and
// list markers, a run of f's character at least as long, then only
// spaces. That is looser than CommonMark, so tm never leaves a fence
// open that Obsidian has closed.
func (f *fence) closedBy(l string) bool {
	_, core := containers(l)
	n := run(core, f.ch)
	return n >= f.n && strings.TrimSpace(core[n:]) == ""
}

type opened struct {
	fence
	line string
}

// openFence reads l as a fence's opening line: after any indent, quote
// and list markers, three or more backticks or tildes and an info
// string (a backtick fence's has no backtick). The line it returns has
// the language "text" unless codeLangs has it.
func openFence(l string) (opened, bool) {
	prefix, core := containers(l)
	if core == "" || core[0] != '`' && core[0] != '~' {
		return opened{}, false
	}
	ch := core[0]
	n := run(core, ch)
	info := core[n:]
	if n < 3 || ch == '`' && strings.Contains(info, "`") {
		return opened{}, false
	}
	o := opened{fence: fence{ch: ch, n: n, unsure: prefix != ""}, line: l}
	if lang := strings.Fields(info); len(lang) > 0 && !codeLangs[strings.ToLower(lang[0])] {
		o.line = prefix + core[:n] + "text"
	}
	return o, true
}

// containers splits l into its indent with any quote and list markers,
// and the rest.
func containers(l string) (prefix, core string) {
	i := 0
	for i < len(l) {
		switch c := l[i]; {
		case c == ' ' || c == '\t' || c == '>':
			i++
			continue
		case (c == '-' || c == '*' || c == '+') && i+1 < len(l) && (l[i+1] == ' ' || l[i+1] == '\t'):
			i += 2
			continue
		case c >= '0' && c <= '9':
			j := i
			for j < len(l) && j-i < 9 && l[j] >= '0' && l[j] <= '9' {
				j++
			}
			if j+1 < len(l) && (l[j] == '.' || l[j] == ')') && (l[j+1] == ' ' || l[j+1] == '\t') {
				i = j + 2
				continue
			}
		}
		break
	}
	return l[:i], l[i:]
}

// run is the length of the run of c that s starts with.
func run(s string, c byte) int {
	n := 0
	for n < len(s) && s[n] == c {
		n++
	}
	return n
}

// openRun reports whether l has a backtick run that doesn't close on
// l, which a later line may close: the paragraph's rest is unsure.
func openRun(l string) bool {
	for i := 0; i < len(l); {
		switch l[i] {
		case '\\':
			i += 2
		case '`':
			n := run(l[i:], '`')
			end := closeRun(l, i+n, n)
			if end < 0 {
				return true
			}
			i = end + n
		default:
			i++
		}
	}
	return false
}

// closeRun is where the run of exactly n backticks that closes a code
// span opened before from starts in l, or -1.
func closeRun(l string, from, n int) int {
	for j := from; j < len(l); {
		if l[j] != '`' {
			j++
			continue
		}
		m := run(l[j:], '`')
		if m == n {
			return j
		}
		j += m
	}
	return -1
}

// vaultLine is one line of text outside fences, guarded. With unsure,
// no code span is trusted: the rules apply inside them too.
func vaultLine(l string, unsure bool) string {
	var b strings.Builder
	for i := 0; i < len(l); {
		c := l[i]
		switch {
		case c == '\\' && i+1 < len(l) && isPunct(l[i+1]):
			if l[i+1] == '<' {
				b.WriteString("&lt;")
			} else {
				b.WriteString(l[i : i+2])
			}
			i += 2
		case c == '`' && !unsure:
			n := run(l[i:], '`')
			end := closeRun(l, i+n, n)
			if end < 0 {
				b.WriteString(l[i : i+n])
				i += n
				continue
			}
			code := l[i+n : end]
			if t := strings.TrimSpace(code); strings.HasPrefix(t, "=") || strings.HasPrefix(t, "$=") {
				// No backtick is left to pair with another: each is an
				// entity, which shows as one and starts no code.
				b.WriteString("&#96;")
				for k := 0; k < len(code); k++ {
					switch {
					case code[k] == '`':
						b.WriteString("&#96;")
					case code[k] == '<':
						b.WriteString("&lt;")
					case isPunct(code[k]):
						b.WriteByte('\\')
						b.WriteByte(code[k])
					default:
						b.WriteByte(code[k])
					}
				}
				b.WriteString("&#96;")
			} else {
				b.WriteString(l[i : end+n])
			}
			i = end + n
		case c == '<':
			b.WriteString("&lt;")
			i++
		case c == '!' && i+run(l[i:], '!') < len(l) && l[i+run(l[i:], '!')] == '[':
			i += run(l[i:], '!') // an image or an embed: the link it holds stays
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

func isPunct(c byte) bool {
	return c >= '!' && c <= '/' || c >= ':' && c <= '@' || c >= '[' && c <= '`' || c >= '{' && c <= '~'
}

// codeLangs are the languages a fenced block keeps: languages to read,
// which no common Obsidian plugin runs.
var codeLangs = map[string]bool{}

func init() {
	for _, l := range strings.Fields(`text txt plain plaintext console terminal log output
		sh bash zsh fish shell shellsession powershell ps1 pwsh bat cmd
		go golang c h cpp c++ cc hpp cs csharp java kotlin kt scala swift objc rust rs zig
		python py ruby rb perl php lua r julia haskell hs elixir ex erlang ocaml clojure dart
		js javascript jsx ts typescript tsx mjs cjs json jsonc json5 jsonl ndjson
		yaml yml toml ini conf cfg properties env xml html htm css scss sass less svg
		md markdown diff patch sql graphql gql proto protobuf csv tsv
		make makefile dockerfile docker nix hcl terraform tf cmake gradle groovy vim nginx
		regex tex latex asm nasm wasm wat awk sed`) {
		codeLangs[l] = true
	}
}
