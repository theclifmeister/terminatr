package textsafe

import (
	"strings"
	"testing"
)

func TestVault(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"plain **text** and [a link](https://x.y)", "plain **text** and [a link](https://x.y)"},
		{`<img src="https://x.y/a.png">`, `&lt;img src="https://x.y/a.png">`},
		{"a <!-- c --> b <https://x.y>", "a &lt;!-- c --> b &lt;https://x.y>"},
		{`escaped \<b> too`, `escaped &lt;b> too`},
		{"![alt](https://x.y/a.png) and ![r][ref]", "[alt](https://x.y/a.png) and [r][ref]"},
		{"see ![[other/note]] here", "see [[other/note]] here"},
		{`not an image: \![x](y)`, `not an image: \![x](y)`},
		{"code `<b>` and `` a ` <i> `` stay", "code `<b>` and `` a ` <i> `` stay"},
		{"\\`<b>`", "\\`&lt;b>`"},
		{"run `= this.file.name` and `$= dv.x()`", "run &#96;\\= this\\.file\\.name&#96; and &#96;\\$\\= dv\\.x\\(\\)&#96;"},
		{"```go\nif a < b {}\n```\n<b>", "```go\nif a < b {}\n```\n&lt;b>"},
		{"```dataviewjs\ndv.el(\"<img>\")\n```", "```text\ndv.el(\"<img>\")\n```"},
		{"~~~~ js-engine x\n<b>\n~~~\n<i>\n~~~~", "~~~~text\n<b>\n~~~\n<i>\n~~~~"},
		{"```\n<b>\n", "```\n<b>\n"},
		// A fence in a list or a quote is unsure: its text is guarded too.
		{"- ```templater\n  <% tp.x %>\n  ```\n<b>", "- ```text\n  &lt;% tp.x %>\n  ```\n&lt;b>"},
		{"> ```\n> ![x](y)\n<b>", "> ```\n> [x](y)\n&lt;b>"},
		// A backtick run closing on no line of its own: the paragraph's
		// rest trusts no code span.
		{"a `b\nc` <img> `d`\n\n`<e>`", "a `b\nc` &lt;img> `d`\n\n`<e>`"},
		// Not a fence: a backtick info string holds no backtick.
		{"```a`b```\n<b>", "```a`b```\n&lt;b>"},
		// Nor after the inline query in its info string is text.
		{"````0```=```\n<b>", "&#96;```0&#96;\\=&#96;\n&lt;b>"},
	} {
		got := Vault(c.in)
		if got != c.want {
			t.Errorf("Vault(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
		if again := Vault(got); again != got {
			t.Errorf("Vault not idempotent on %q: %q", got, again)
		}
	}
}

// FuzzVault: Vault of what Vault writes is that, and a line of it
// outside every fence and without a backtick has no "<" and no image.
func FuzzVault(f *testing.F) {
	for _, s := range []string{"<b>", "![a](b)", "```js\n<b>\n```", "- ```\n<x>", "`a\nb` <c>", "\\`<b>`", "`= x`"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out := Vault(s)
		if Vault(out) != out {
			t.Fatalf("not idempotent: %q → %q → %q", s, out, Vault(out))
		}
		var fn *fence
		for _, l := range strings.Split(out, "\n") {
			if fn != nil {
				if fn.closedBy(l) {
					fn = nil
				}
				continue
			}
			if o, ok := openFence(l); ok {
				fn = &o.fence
				continue
			}
			if strings.Contains(l, "`") {
				continue
			}
			if strings.Contains(l, "<") || hasImage(l) {
				t.Fatalf("%q: %q has a < or an image", s, l)
			}
		}
	})
}

// hasImage reports whether l has a "![" whose "!" isn't escaped.
func hasImage(l string) bool {
	for i := 0; i+1 < len(l); i++ {
		switch {
		case l[i] == '\\':
			i++
		case l[i] == '!' && l[i+1] == '[':
			return true
		}
	}
	return false
}
