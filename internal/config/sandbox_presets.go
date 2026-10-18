package config

import (
	"fmt"
	"slices"
	"strings"
)

// The sandbox presets of threads (docs/SPEC.md §5.2, Sandbox presets):
// named switches in a sandbox table, each opening what one kind of tool
// needs while its commands stay inside the sandbox, which is why the
// Sandbox tab puts them before the excluded commands:
//
//	[projects.web.sandbox]
//	browser_testing = true
//	browser_downloads = true
//	local_dev_servers = true
//
// A project's switch, when it sets one, replaces all projects'
// ([defaults.sandbox]). What each opens is the launch's (agent.SandboxLists),
// on the platforms SandboxSettings gives; config.toml keeps a switch on
// any platform, and a launch where it doesn't apply skips it.

// Preset keys of a sandbox table.
const (
	SandboxBrowser    = "browser_testing"
	SandboxDownloads  = "browser_downloads"
	SandboxDevServers = "local_dev_servers"
)

// SandboxPresets are the presets' keys, in the Sandbox tab's order.
var SandboxPresets = []string{SandboxBrowser, SandboxDownloads, SandboxDevServers}

// BrowserDomains are the hosts the browser downloads preset opens to a
// thread's sandboxed commands, so npm i playwright and npx playwright
// install work inside it: the npm registry (packages and their
// tarballs), Playwright's browser download mirrors (PLAYWRIGHT_CDN_MIRRORS
// in playwright-core 1.61 to 1.64; playwright.azureedge.net is no longer
// one), and storage.googleapis.com, where cdn.playwright.dev redirects
// the Chrome for Testing builds (the headless shell among them). That
// last one is every Google Cloud Storage bucket's host, which the
// Sandbox tab says. They are a key of their own, not browser testing's
// (v0.35 had that without hosts): a switch turned on before them opens
// none until the human turns this one on, after its question.
var BrowserDomains = []string{"registry.npmjs.org", "cdn.playwright.dev", "playwright.download.prss.microsoft.com", "storage.googleapis.com"}

// The platforms a sandbox setting applies on, as runtime.GOOS names them.
const (
	OSMac     = "darwin"
	OSLinux   = "linux"
	OSWindows = "windows"
)

// SandboxSetting is where one key of a sandbox table applies: the
// platforms whose agent sandbox has what it maps to, and whether Codex
// threads get it (else only Claude's do). From the T561 findings
// (Claude Code 2.1.302 and Codex 0.162.0 binaries):
//   - excluded commands and allow rules are Claude settings on every
//     platform; Codex has no per-session form of either.
//   - local dev servers are Claude's sandbox.network.allowLocalBinding,
//     which only its macOS sandbox reads (Linux doesn't pass it, Windows
//     has no setting for localhost), and Codex's
//     permissions.<profile>.network.allow_local_binding (macOS policy).
//   - browser testing is how to launch Chromium in a
//     macOS sandbox (no local binding: Playwright's pipe transport needs
//     none; a dev server needs local dev servers too), which can't register Mach services (multi-process
//     Chromium needs that; no setting of either agent grants it): the
//     headless shell with --single-process. On Linux Chromium needs
//     nothing more (bubblewrap, no Mach), so it doesn't apply there.
//   - browser downloads opens BrowserDomains (Claude's allowedDomains; a
//     Codex thread's network is open already), with guidance for the
//     macOS sandbox's read-only caches, so macOS only too.
type SandboxSetting struct {
	OS    []string
	Codex bool
}

// SandboxSettings are every sandbox key's platforms.
var SandboxSettings = map[string]SandboxSetting{
	SandboxExcluded:   {OS: []string{OSMac, OSLinux, OSWindows}},
	SandboxAllow:      {OS: []string{OSMac, OSLinux, OSWindows}},
	SandboxBrowser:    {OS: []string{OSMac}, Codex: true},
	SandboxDownloads:  {OS: []string{OSMac}, Codex: true},
	SandboxDevServers: {OS: []string{OSMac}, Codex: true},
}

// SandboxApplies reports whether sandbox key applies on platform goos.
func SandboxApplies(key, goos string) bool {
	return slices.Contains(SandboxSettings[key].OS, goos)
}

// OSName is a platform as the UI and the log say it.
func OSName(goos string) string {
	switch goos {
	case OSMac:
		return "macOS"
	case OSLinux:
		return "Linux"
	case OSWindows:
		return "Windows"
	}
	return goos
}

// preset is the table's switch of key, nil when it doesn't set it.
func (r *rawSandbox) preset(key string) *bool {
	if r == nil {
		return nil
	}
	switch key {
	case SandboxBrowser:
		return r.BrowserTesting
	case SandboxDownloads:
		return r.BrowserDownloads
	case SandboxDevServers:
		return r.LocalDevServers
	}
	return nil
}

// SandboxPresetsOf is one table's preset switches by key, only those it
// sets: all projects' for slug "", else the project's own. None for a
// nil Config.
func (c *Config) SandboxPresetsOf(slug string) map[string]bool {
	if c == nil {
		return nil
	}
	r := c.defaults.Sandbox
	if slug != "" {
		r = c.projects[slug].Sandbox
	}
	out := map[string]bool{}
	for _, k := range SandboxPresets {
		if v := r.preset(k); v != nil {
			out[k] = *v
		}
	}
	return out
}

// presetTable is the sandbox table of table for preset key.
func presetTable(table, key string) (string, error) {
	if !slices.Contains(SandboxPresets, key) {
		return "", fmt.Errorf("unknown sandbox preset %q", key)
	}
	if table != DefaultsTable && !strings.HasPrefix(table, "projects.") {
		return "", fmt.Errorf("no sandbox presets in [%s]", table)
	}
	return table + ".sandbox", nil
}

// SetSandboxPreset turns preset key on or off in table's sandbox table
// (DefaultsTable or "projects.<slug>"); nil removes it, so a project
// follows all projects'.
func SetSandboxPreset(table, key string, on *bool) error {
	t, err := presetTable(table, key)
	if err != nil {
		return err
	}
	if on == nil {
		return Unset(t, key)
	}
	return Set(t, key, *on)
}
