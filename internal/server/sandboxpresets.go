package server

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// sandbox is project slug's sandbox lists and presets for its threads on
// platform goos (docs/SPEC.md §5.2), the presets on that apply there, and
// a note for each setting that doesn't, which the launch skips:
// config.toml keeps it. A config.toml that doesn't load, or an entry
// that fails the sandbox checks, opens nothing (fails closed) and gives
// one note saying why, for the log and the launch's warnings. It is
// launchSandbox of the rules (access.go).
func (l launchConfig) sandbox(slug, goos string) (lists agent.SandboxLists, presets, skipped []string) {
	rules, why := sandboxRules(l, slug, goos)
	return launchSandbox(proto.AccessSet{Rules: rules, SandboxError: why})
}

// browserGuide is what a thread with the browser testing preset is told
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

// browserDownloads is the guide's downloads part with browser downloads
// on; browserNoDownloads with it off.
const (
	browserDownloads = `Downloads: the human turned on browser downloads, so your commands may reach
registry.npmjs.org and Playwright's browser downloads (cdn.playwright.dev,
playwright.download.prss.microsoft.com, storage.googleapis.com).

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
download hosts (the human hasn't turned on browser downloads): use what is
installed already (node_modules, ~/Library/Caches/ms-playwright).

`
)

// The guide's local ports paragraph, with local dev servers off and on.
const (
	browserPortsClosed = `Playwright talks to the browser over a pipe, which needs no local port, and
file:// pages need none either. Your commands may not listen on or reach local
ports (the human hasn't turned on local dev servers), so a dev server
(Playwright's webServer) or a page served on localhost won't work.
`
	browserPortsOpen = `Playwright talks to the browser over a pipe, which needs no local port. The
human also turned on local dev servers: your commands may listen on and reach
local ports, so a dev server (Playwright's webServer) works.
`
)

// maxBrief is the most of a thread's brief.md that presetBrief copies:
// the brief is the coordinator's text, not unbounded.
const maxBrief = 1 << 20

// presetBrief is the brief a thread with presets gets: brief (its
// brief.md, "" for none) with the presets' guidance after it (browser
// testing's, saying whether downloads and local ports are open; browser
// downloads' alone without it), written in
// its runtime dir rt at every launch, so a resume sees today's switches;
// brief itself when no preset on has any.
func presetBrief(rt, brief string, presets []string) (string, error) {
	browser, dl := slices.Contains(presets, config.SandboxBrowser), slices.Contains(presets, config.SandboxDownloads)
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
	downloads, ports := browserNoDownloads, browserPortsClosed
	if slices.Contains(presets, config.SandboxDownloads) {
		downloads = browserDownloads
	}
	if slices.Contains(presets, config.SandboxDevServers) {
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
