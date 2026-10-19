package config

import (
	"slices"
)

// The sandbox presets (docs/SPEC.md §5.2, T574 §3.3): named groups of
// rules tm ships, each what one kind of tool needs while its commands
// stay inside the sandbox. A preset is no setting: adding it adds its
// rules to a sandbox table (all projects' or a project's) as if the
// human had typed them, and each then stands on its own, to change or
// remove one by one. Nothing in config.toml names it afterwards. A
// preset's guide is its text written out, worded from the rules the
// threads have once it is added (WordRules), to edit like any other.
//
// tm 0.37 and before had a switch per preset in a sandbox table
// (browser_testing, browser_downloads, local_dev_servers). They are
// still read, as the rules they unfold into (a project's false as drops
// of them), and the writers replace them with those rules when they
// write the table.

// Preset names.
const (
	PresetBrowserTesting   = "browser-testing"
	PresetBrowserDownloads = "browser-downloads"
	PresetLocalDevServers  = "local-dev-servers"
)

// A PresetRule is one rule a preset adds: a sandbox list's key and an
// entry of it, or SandboxLocalPorts with "on". A guide's Value is its
// title, its Body the text WordRules gives it.
type PresetRule struct {
	Key, Value string
	Body       string `json:",omitempty"`
}

// A Preset is a named group of rules, with one line saying what it is
// for and the platforms its rules apply on.
type Preset struct {
	Name, Title, About string
	OS                 []string
	Rules              []PresetRule
}

// BrowserDomains are the hosts the browser downloads preset adds, so
// npm i playwright and npx playwright install work inside the sandbox:
// the npm registry (packages and their tarballs), Playwright's browser
// download mirrors (PLAYWRIGHT_CDN_MIRRORS in playwright-core 1.61 to
// 1.64; playwright.azureedge.net is no longer one), and
// storage.googleapis.com, where cdn.playwright.dev redirects the Chrome
// for Testing builds (the headless shell among them). That last one is
// every Google Cloud Storage bucket's host, which its list says.
var BrowserDomains = []string{"registry.npmjs.org", "cdn.playwright.dev", "playwright.download.prss.microsoft.com", "storage.googleapis.com"}

// Presets are the presets tm ships, in the order the Access tab lists
// them.
var Presets = []Preset{
	{PresetBrowserTesting, "Browser testing (Playwright, Chromium)",
		"Lets threads run Playwright and other Chromium tools inside macOS's sandbox: guidance only, it opens nothing.",
		[]string{OSMac}, []PresetRule{{Key: SandboxGuides, Value: GuideBrowserTesting}}},
	{PresetBrowserDownloads, "Browser downloads (npm, Playwright)",
		"Lets threads install Playwright and its browser inside the sandbox.",
		[]string{OSMac}, append(hostRules(BrowserDomains), PresetRule{Key: SandboxGuides, Value: GuideBrowserDownloads})},
	{PresetLocalDevServers, "Local dev servers",
		"Lets threads' commands listen on and reach local ports: a dev server, a test server, a local database.",
		[]string{OSMac}, []PresetRule{{Key: SandboxLocalPorts, Value: "on"}}},
}

// hostRules are a host rule for each host.
func hostRules(hosts []string) []PresetRule {
	out := make([]PresetRule, len(hosts))
	for i, h := range hosts {
		out[i] = PresetRule{Key: SandboxHosts, Value: h}
	}
	return out
}

// PresetNamed is the preset called name.
func PresetNamed(name string) (Preset, bool) {
	i := slices.IndexFunc(Presets, func(p Preset) bool { return p.Name == name })
	if i < 0 {
		return Preset{}, false
	}
	return Presets[i], true
}

// Missing are p's rules has says threads don't have yet, in p's order:
// what adding p adds; the rest are there already.
func (p Preset) Missing(has func(PresetRule) bool) []PresetRule {
	var out []PresetRule
	for _, r := range p.Rules {
		if !has(r) {
			out = append(out, r)
		}
	}
	return out
}

// WordRules are rules with each guide's text worded for threads that
// have ts and get rules too: the hosts they then reach, and whether
// their local ports are open.
func WordRules(rules []PresetRule, ts ThreadSandbox) []PresetRule {
	hosts, ports := ts.Given(SandboxHosts), ts.LocalPorts
	for _, r := range rules {
		switch r.Key {
		case SandboxHosts:
			hosts = append(hosts, r.Value)
		case SandboxLocalPorts:
			ports = true
		}
	}
	out := slices.Clone(rules)
	for i, r := range out {
		switch {
		case r.Key != SandboxGuides:
		case r.Value == GuideBrowserTesting:
			out[i].Body = BrowserTestingGuide(ports).Body
		case r.Value == GuideBrowserDownloads:
			out[i].Body = BrowserDownloadsGuide(hosts).Body
		}
	}
	return out
}

// legacySwitchGuides are the guide names of tm 0.38 the old switches
// gave, by preset.
var legacySwitchGuides = map[string]string{PresetBrowserTesting: legacyBrowserTesting, PresetBrowserDownloads: legacyBrowserDownloads}

// oldSwitches are the preset switches of tm 0.37 and before, by the
// preset each turned on.
var oldSwitches = []struct{ key, preset string }{
	{"browser_testing", PresetBrowserTesting},
	{"browser_downloads", PresetBrowserDownloads},
	{"local_dev_servers", PresetLocalDevServers},
}

// OldSwitchKeys are the keys of oldSwitches.
func OldSwitchKeys() []string {
	out := make([]string, len(oldSwitches))
	for i, s := range oldSwitches {
		out[i] = s.key
	}
	return out
}

// oldSwitch is the table's old switch of key, nil when it doesn't set
// it.
func (r *rawSandbox) oldSwitch(key string) *bool {
	if r == nil {
		return nil
	}
	switch key {
	case "browser_testing":
		return r.BrowserTesting
	case "browser_downloads":
		return r.BrowserDownloads
	case "local_dev_servers":
		return r.LocalDevServers
	}
	return nil
}

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
//   - local ports are Claude's sandbox.network.allowLocalBinding, which
//     only its macOS sandbox reads (Linux doesn't pass it, Windows has
//     no setting for localhost), and Codex's
//     permissions.<profile>.network.allow_local_binding (macOS policy).
//   - sockets are Claude's sandbox.network.allowUnixSockets and a Codex
//     thread's network unix_sockets, which name sockets on macOS only
//     (Claude's Linux sandbox opens every socket or none; Codex's Linux
//     one none while its proxy limits the network).
//   - approved commands aren't the agent's: tm runs them, on every
//     platform, for both agents.
//   - hosts are Claude's allowedDomains and a Codex thread's limited
//     network's domains. tm gave them only on macOS (browser downloads'), so
//     they stay macOS only: a narrower thread elsewhere, never a wider
//     one, until another platform's are checked.
//   - guides are plain text the brief gets, on every platform, for both
//     agents (the browser presets, whose texts are about macOS's
//     sandbox, are macOS's only).
type SandboxSetting struct {
	OS    []string
	Codex bool
}

// SandboxSettings are every sandbox key's platforms.
var SandboxSettings = map[string]SandboxSetting{
	SandboxExcluded:   {OS: []string{OSMac, OSLinux, OSWindows}},
	SandboxAllow:      {OS: []string{OSMac, OSLinux, OSWindows}},
	SandboxWrite:      {OS: []string{OSMac, OSLinux, OSWindows}, Codex: true},
	SandboxSockets:    {OS: []string{OSMac}, Codex: true},
	SandboxApproved:   {OS: []string{OSMac, OSLinux, OSWindows}, Codex: true},
	SandboxHosts:      {OS: []string{OSMac}, Codex: true},
	SandboxGuides:     {OS: []string{OSMac, OSLinux, OSWindows}, Codex: true},
	SandboxLocalPorts: {OS: []string{OSMac}, Codex: true},
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
