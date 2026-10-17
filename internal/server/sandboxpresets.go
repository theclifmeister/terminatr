package server

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/config"
)

// sandbox is project slug's sandbox lists and presets for its threads on
// platform goos (docs/SPEC.md §5.2), the presets on that apply there, and
// a note for each setting that doesn't, which the launch skips:
// config.toml keeps it. A config.toml that doesn't load opens nothing.
func (l launchConfig) sandbox(slug, goos string) (lists agent.SandboxLists, presets, skipped []string) {
	if l.err != nil {
		return lists, nil, nil
	}
	t, err := l.cfg.ThreadSandbox(slug)
	if err != nil {
		return lists, nil, nil
	}
	skip := func(key, what string) bool {
		if config.SandboxApplies(key, goos) {
			return false
		}
		skipped = append(skipped, fmt.Sprintf("sandbox %s is set but doesn't apply on %s: skipped", what, config.OSName(goos)))
		return true
	}
	if len(t.ExcludedCommands) > 0 && !skip(config.SandboxExcluded, "excluded commands") {
		lists.Excluded = t.ExcludedCommands
	}
	if len(t.Allow) > 0 && !skip(config.SandboxAllow, "allow rules") {
		lists.Allow = t.Allow
	}
	for _, k := range t.Presets {
		if skip(k, "preset "+strings.ReplaceAll(k, "_", " ")) {
			continue
		}
		presets = append(presets, k)
		switch k {
		case config.SandboxBrowser, config.SandboxDevServers:
			lists.LocalBinding = true
		}
	}
	return lists, presets, skipped
}

// browserGuide is what a thread with the browser testing preset is told
// (macOS: T561's findings). Seatbelt has no mach-register rule in Claude
// or Codex, which multi-process Chromium needs for its child processes;
// the headless shell in one process needs none.
const browserGuide = `## Sandbox: browser testing

The human turned on browser testing for this project's threads. On macOS your
sandbox can't start Chromium with its usual child processes (it may not
register Mach services, and no setting allows that). What works inside the
sandbox is Playwright's headless shell in one process:

- Library: chromium.launch({ headless: true, args: ['--single-process'] })
  (Python: p.chromium.launch(headless=True, args=["--single-process"])).
- npx playwright test: headless, with use.launchOptions.args ['--single-process'];
  put that in a config of your own that extends the project's (e.g.
  playwright.sandbox.config.ts, run with --config) rather than changing the
  project's config.
- Not channel 'chrome' or 'chromium' (full Chrome) and not headed mode: those
  fail in this sandbox.

Your commands may listen on and reach localhost, so a dev server (Playwright's
webServer) works. Downloads (npx playwright install) need domains the sandbox
may not allow. If a browser is missing or what you need still fails, don't run
it outside the sandbox: say so in your report; only the human can add an
excluded command.
`

// presetBrief is the brief a thread with presets gets: brief (its
// brief.md, "" for none) with the presets' guidance after it, written in
// its runtime dir rt at every launch, so a resume sees today's switches;
// brief itself when no preset on has any.
func presetBrief(rt, brief string, presets []string) (string, error) {
	if !slices.Contains(presets, config.SandboxBrowser) {
		return brief, nil
	}
	var text []byte
	if brief != "" {
		b, err := os.ReadFile(brief)
		if err != nil {
			return brief, err
		}
		text = append(b, '\n')
	}
	path := filepath.Join(rt, "brief.md")
	if err := os.WriteFile(path, append(text, browserGuide...), 0o600); err != nil {
		return brief, err
	}
	return path, nil
}
