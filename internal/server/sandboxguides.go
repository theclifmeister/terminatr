package server

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// sandbox is project slug's sandbox lists for its threads on platform
// goos (docs/SPEC.md §5.2), the guidance texts they get there, and a
// note for each rule that doesn't apply or is refused, which the launch
// skips: config.toml keeps it. A config.toml that doesn't load opens
// nothing (fails closed) and gives one note saying why, for the log and
// the launch's warnings. It is launchSandbox of the rules (access.go).
func (l launchConfig) sandbox(slug, goos string) (lists agent.SandboxLists, guides, skipped []string) {
	rules, why := sandboxRules(l, slug, goos, "")
	return launchSandbox(proto.AccessSet{Rules: rules, SandboxError: why})
}

// browserGuide is the browser-testing guidance a thread is told
// (macOS: T561's and T571's findings; no local binding, the pipe
// transport needs none). Seatbelt has no mach-register rule in Claude
// or Codex, which multi-process Chromium needs for its child processes;
// the headless shell in one process needs none, and holds one browser
// context at a time, so the config reuses one per worker. Claude's
// sandbox proxy resets Playwright's own downloader near the end of a
// large file (curl and Node's fetch get it whole), and ~/.npm and
// ~/Library/Caches/ms-playwright aren't writable: hence the cache and
// browsers path of the thread's own. Its %s are what it says of
// downloads and of local ports.
const browserGuide = `## Sandbox: browser testing

The human turned on browser testing for this project's threads. On macOS your
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

%s%s
If a browser is missing or what you need still fails, don't run it outside the
sandbox: say so in your report; only the human can add an excluded command.
`

// browserDownloads is the guidance's downloads part with download hosts
// open (%s says which), browserNoDownloads with none.
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

// The guidance's local ports paragraph, with local ports closed and open.
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

// maxBrief is the most of a thread's brief.md that guideBrief copies:
// the brief is the coordinator's text, not unbounded.
const maxBrief = 1 << 20

// downloadsText is the guidance's downloads part for a thread whose
// sandboxed commands reach hosts: which of the browser download hosts
// they reach, and which not.
func downloadsText(hosts []string) string {
	var open, closed []string
	for _, h := range config.BrowserDomains {
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

// guideBrief is the brief a thread with guidance texts gets: brief (its
// brief.md, "" for none) with the texts after it, each worded from the
// thread's other rules in lists (which download hosts it reaches,
// whether local ports are open), written in its runtime dir rt at every
// launch, so a resume sees today's rules; brief itself without any.
func guideBrief(rt, brief string, guides []string, lists agent.SandboxLists) (string, error) {
	browser, dl := slices.Contains(guides, config.GuideBrowserTesting), slices.Contains(guides, config.GuideBrowserDownloads)
	if !browser && !dl {
		return brief, nil
	}
	var text []byte
	if brief != "" {
		f, err := os.Open(brief)
		if err != nil {
			return brief, err
		}
		b, err := io.ReadAll(io.LimitReader(f, maxBrief+1))
		f.Close()
		if err != nil {
			return brief, err
		}
		if len(b) > maxBrief {
			return brief, fmt.Errorf("brief %s is longer than %d bytes", brief, maxBrief)
		}
		text = append(b, '\n')
	}
	downloads, ports := downloadsText(lists.Domains), browserPortsClosed
	if lists.LocalBinding {
		ports = browserPortsOpen
	}
	guide := fmt.Sprintf(browserGuide, downloads, ports)
	if !browser {
		guide = "## Sandbox: browser downloads\n\n" + downloads
	}
	path := filepath.Join(rt, "brief.md")
	if err := os.WriteFile(path, append(text, guide...), 0o600); err != nil {
		return brief, err
	}
	return path, nil
}
