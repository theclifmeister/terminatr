package config

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// Guidance for threads (T605): texts tm adds after a thread's brief,
// each a title and a body the human writes, plain text that opens
// nothing:
//
//	[[projects.web.sandbox.guides]]
//	title = "Test data"
//	body = """
//	The fixtures in testdata/ are generated: run make fixtures, never edit them.
//	"""
//
// tm writes them inline (guides = [{ title = …, body = """…""" }]); a
// project's guides add to all projects', and its drop takes one of
// theirs out by its title.
//
// tm 0.38 named two texts tm kept itself (guides = ["browser-testing"]),
// worded at each launch. Such a name is still read, worded so, until
// the migration writes it out (MigrateGuides); any other plain string
// is refused.

// A Guide is one guidance text: a title of one line and its body.
type Guide struct {
	Title, Body string
}

// The limits of a guide.
const (
	maxGuideTitle = 120
	MaxGuideBody  = 8 << 10
)

// guideList is a guides list as config.toml has it: tables { title,
// body }, or the names of tm 0.38 (a Guide without a body).
type guideList []Guide

// UnmarshalTOML reads each entry; any other form fails the file's load,
// as a wrong type does.
func (l *guideList) UnmarshalTOML(v any) error {
	vs, ok := v.([]any)
	if ms, tables := v.([]map[string]any); tables {
		for _, m := range ms {
			vs = append(vs, m)
		}
		ok = true
	}
	if !ok {
		return fmt.Errorf("guides must be a list of { title = …, body = … }")
	}
	out := make(guideList, 0, len(vs))
	for _, e := range vs {
		g, err := parseGuide(e)
		if err != nil {
			return fmt.Errorf("guides: %w", err)
		}
		out = append(out, g)
	}
	*l = out
	return nil
}

// parseGuide reads one entry of guides.
func parseGuide(v any) (Guide, error) {
	switch v := v.(type) {
	case string:
		return Guide{Title: v}, nil
	case map[string]any:
		var g Guide
		for _, k := range slices.Sorted(maps.Keys(v)) {
			s, ok := v[k].(string)
			switch {
			case k != "title" && k != "body":
				return g, fmt.Errorf("an entry has an unknown key %q (title, body)", k)
			case !ok:
				return g, fmt.Errorf("%s must be text", k)
			case k == "title":
				g.Title = s
			default:
				// tm writes the closing quotes on a line of their own.
				g.Body = strings.TrimSuffix(s, "\n")
			}
		}
		if g.Title == "" || g.Body == "" {
			return g, fmt.Errorf("an entry needs a title and a body, e.g. { title = \"Test data\", body = \"…\" }")
		}
		return g, nil
	}
	return Guide{}, fmt.Errorf("an entry is a table { title = …, body = … }")
}

// titles are the list's titles (and names), in its order.
func (l guideList) titles() []string {
	out := make([]string, len(l))
	for i, g := range l {
		out[i] = g.Title
	}
	return out
}

// bodies are the list's bodies by title, none for a name.
func (l guideList) bodies() map[string]string {
	out := map[string]string{}
	for _, g := range l {
		if g.Body != "" {
			out[g.Title] = g.Body
		}
	}
	return out
}

// guidesOf is list as entries of guides, each with its body in bodies
// (a name of tm 0.38 has none).
func guidesOf(list []string, bodies map[string]string) []Guide {
	out := make([]Guide, len(list))
	for i, t := range list {
		out[i] = Guide{Title: t, Body: bodies[t]}
	}
	return out
}

// CheckGuideTitle checks a guide's title: one line of plain text, at
// most maxGuideTitle characters.
func CheckGuideTitle(t string) error {
	if err := checkSandboxText(t); err != nil {
		return err
	}
	if n := len([]rune(t)); n > maxGuideTitle {
		return fmt.Errorf("the title %q is longer than %d characters", textsafe.Clip(t, 40), maxGuideTitle)
	}
	return nil
}

// CheckGuideBody checks a guide's body: plain text in lines (tabs too),
// not empty, at most MaxGuideBody bytes.
func CheckGuideBody(b string) error {
	switch {
	case strings.TrimSpace(b) == "":
		return fmt.Errorf("a guide needs a text")
	case len(b) > MaxGuideBody:
		return fmt.Errorf("the text is longer than %d bytes (%d)", MaxGuideBody, len(b))
	case textsafe.Text(b) != b || strings.Contains(b, "\r"):
		return fmt.Errorf("the text has control or invisible characters")
	}
	return nil
}

// CheckGuide checks guide g as config.toml may have it: a title and a
// body, or a name of tm 0.38 (no body) of a text it kept.
func CheckGuide(g Guide) error {
	if g.Body == "" {
		if err := checkSandboxText(g.Title); err != nil {
			return err
		}
		if !slices.Contains(legacyGuideNames, g.Title) {
			return fmt.Errorf("%q has no text: a guide is a title and its text, { title = …, body = … }", g.Title)
		}
		return nil
	}
	if err := CheckGuideTitle(g.Title); err != nil {
		return err
	}
	return CheckGuideBody(g.Body)
}

// GuideBody is body as a guide keeps it: line ends as newlines, no white
// space at its end or on blank lines before it.
func GuideBody(body string) string {
	body = strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n")
	return strings.TrimRight(body, " \t\n")
}

// GuideText is guides as a brief gets them: each a ## heading, its
// title, and its body; "" for none.
func GuideText(guides []Guide) string {
	var b strings.Builder
	for i, g := range guides {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("## " + textsafe.Line(g.Title, 0) + "\n\n" + strings.TrimRight(g.Body, "\n") + "\n")
	}
	return b.String()
}

// Titles of the browser guides tm 0.38 gave and its presets added.
const (
	GuideBrowserTesting   = "Sandbox: browser testing"
	GuideBrowserDownloads = "Sandbox: browser downloads"
)

// The names of tm 0.38's guidance texts.
const (
	legacyBrowserTesting   = "browser-testing"
	legacyBrowserDownloads = "browser-downloads"
)

// legacyGuideNames are tm 0.38's guidance texts, in the order it gave
// them.
var legacyGuideNames = []string{legacyBrowserTesting, legacyBrowserDownloads}

// IsLegacyGuide reports whether g is a name of tm 0.38 (no body).
func IsLegacyGuide(g Guide) bool { return g.Body == "" && slices.Contains(legacyGuideNames, g.Title) }

// LegacyGuides are the texts tm 0.38 gave a thread for the guide names
// it gets, worded from its hosts and local ports: browser-testing's
// whole text (its downloads part among it), else browser-downloads'
// downloads part alone; none for neither.
func LegacyGuides(names, hosts []string, localPorts bool) []Guide {
	switch {
	case slices.Contains(names, legacyBrowserTesting):
		return []Guide{{GuideBrowserTesting, fmt.Sprintf(browserGuide, downloadsText(hosts), portsText(localPorts)) + browserGuideEnd}}
	case slices.Contains(names, legacyBrowserDownloads):
		return []Guide{{GuideBrowserDownloads, strings.TrimRight(downloadsText(hosts), "\n")}}
	}
	return nil
}

// browserGuide is the browser-testing guidance (macOS: T561's and
// T571's findings; no local binding, the pipe transport needs none).
// Seatbelt has no mach-register rule in Claude or Codex, which
// multi-process Chromium needs for its child processes; the headless
// shell in one process needs none, and holds one browser context at a
// time, so the config reuses one per worker. Its %s are what it says of
// downloads (tm 0.38's text only) and of local ports.
const browserGuide = `The human turned on browser testing for this project's threads. On macOS your
sandbox can't start Chromium with its usual child processes (it may not
register Mach services, and no setting allows that). What works inside the
sandbox is Playwright's headless shell in one process, and that browser holds
one context at a time: a second one, or a new one after closing the first,
takes it down.

- Library: chromium.launch({ headless: true, args: ['--single-process'] })
  (Python: p.chromium.launch(headless=True, args=["--single-process"])), one
  context per browser; launch another browser for another context.
- npx playwright test: run it with a config of your own that extends the
  project's, rather than changing the project's config:
  npx playwright test --config playwright.sandbox.config.ts

` + "```ts" + `
// playwright.sandbox.config.ts
import { defineConfig, test as base } from '@playwright/test';
import project from './playwright.config';

export default defineConfig(project, {
  use: {
    headless: true,
    launchOptions: { args: ['--single-process'] },
    // One context per worker, reset between tests: tests using the page
    // and context fixtures (and test.use options) pass unchanged.
    reuseContext: true,
  } as any,
});

// A browser per test, for a test that needs a fresh one: import
// { test, expect } from './playwright.sandbox.config'.
export const test = base.extend({
  context: async ({ playwright, browserName, launchOptions, contextOptions, baseURL, viewport }, use) => {
    const browser = await playwright[browserName].launch(launchOptions);
    await use(await browser.newContext({ ...contextOptions, baseURL, viewport }));
    await browser.close().catch(() => {});
  },
});
export { expect } from '@playwright/test';
` + "```" + `

  A test that calls browser.newContext() itself still needs a browser of its
  own (chromium.launch with the same args).
- Not channel 'chrome' or 'chromium' (full Chrome) and not headed mode: those
  fail in this sandbox.

%s%s`

// browserGuideEnd closes the browser-testing guidance.
const browserGuideEnd = `
If a browser is missing or what you need still fails, don't run it outside the
sandbox: say so in your report; only the human can add an excluded command.`

// browserDownloads is the downloads part with download hosts open (%s
// says which), browserNoDownloads with none. Claude's sandbox proxy
// resets Playwright's own downloader near the end of a large file (curl
// and Node's fetch get it whole), and ~/.npm and
// ~/Library/Caches/ms-playwright aren't writable: hence the cache and
// browsers path of the thread's own.
const (
	browserDownloads = `Downloads: %s

- npm can't write ~/.npm here: npm i --cache "$TMPDIR/npm-cache" ... (npx too).
- A browser already in ~/Library/Caches/ms-playwright is used as it is (npx
  playwright install --dry-run chromium-headless-shell shows where it looks).
  That folder is read-only to you: to install one, set
  PLAYWRIGHT_BROWSERS_PATH="$TMPDIR/ms-playwright" on the install and on
  every run after it.
- npx playwright install chromium-headless-shell may fail with "server closed
  connection" (the sandbox's proxy cuts its downloader off near the end). Then
  fetch it with curl: take the Install location and Download url from
  --dry-run, and run
  curl -fL -o "$TMPDIR/hs.zip" "<url>" && mkdir -p "<location>" &&
  unzip -q "$TMPDIR/hs.zip" -d "<location>" && touch "<location>/INSTALLATION_COMPLETE"

`
	browserNoDownloads = `Downloads: your commands may not reach the npm registry or Playwright's
download hosts (the human hasn't opened them): use what is installed already
(node_modules, ~/Library/Caches/ms-playwright).

`
)

// The local ports paragraph, with local ports closed and open.
const (
	browserPortsClosed = `Playwright talks to the browser over a pipe, which needs no local port, and
file:// pages need none either. Your commands may not listen on or reach local
ports (the human hasn't opened local ports), so a dev server
(Playwright's webServer) or a page served on localhost won't work.
`
	browserPortsOpen = `Playwright talks to the browser over a pipe, which needs no local port. The
human also opened local ports: your commands may listen on and reach local
ports, so a dev server (Playwright's webServer) works.
`
)

// portsText is the local ports paragraph.
func portsText(open bool) string {
	if open {
		return browserPortsOpen
	}
	return browserPortsClosed
}

// downloadsText is the downloads part for a thread whose sandboxed
// commands reach hosts: which of the browser download hosts they reach,
// and which not.
func downloadsText(hosts []string) string {
	var open, closed []string
	for _, h := range BrowserDomains {
		if slices.Contains(hosts, h) {
			open = append(open, h)
		} else {
			closed = append(closed, h)
		}
	}
	if len(open) == 0 {
		return browserNoDownloads
	}
	say := "the human opened " + strings.Join(open, ", ") + " to your commands"
	if len(closed) > 0 {
		say += ", not " + strings.Join(closed, ", ") + ": a download from there fails"
	}
	return fmt.Sprintf(browserDownloads, wrapText(say+".", 79))
}

// wrapText breaks text into lines of at most w characters, at spaces.
func wrapText(text string, w int) string {
	var b strings.Builder
	n := 0
	for i, word := range strings.Fields(text) {
		switch {
		case i == 0:
		case n+1+len(word) > w:
			b.WriteByte('\n')
			n = 0
		default:
			b.WriteByte(' ')
			n++
		}
		b.WriteString(word)
		n += len(word)
	}
	return b.String()
}
