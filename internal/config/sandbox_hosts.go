package config

import (
	"fmt"
	"slices"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// Host and guidance rules of a sandbox table (T574 §3.1): hosts a
// thread's sandboxed commands may reach (Claude's allowedDomains), and
// the guidance texts tm adds after its brief, by name.

// maxHost is the longest host name DNS has.
const maxHost = 253

// CheckHost checks one host rule: a host name in lower case
// (registry.npmjs.org), or every host under one (*.example.com), never
// an address, a port, a URL, or a name of one word (localhost, com):
// local ports are local_ports' to open, and a wildcard must leave the
// domain fixed.
func CheckHost(h string) error {
	if err := checkSandboxText(h); err != nil {
		return err
	}
	if len(h) > maxHost {
		return fmt.Errorf("%q is longer than a host name can be", h)
	}
	if strings.Contains(h, "://") || strings.ContainsAny(h, "/:@ ") {
		return fmt.Errorf("%q isn't a host name: name the host alone, e.g. registry.npmjs.org", h)
	}
	if strings.ToLower(h) != h {
		return fmt.Errorf("%q has capitals: write the host in lower case", h)
	}
	name, wild := strings.CutPrefix(h, "*.")
	labels := strings.Split(name, ".")
	for _, l := range labels {
		if l == "" || len(l) > 63 || strings.HasPrefix(l, "-") || strings.HasSuffix(l, "-") ||
			strings.Trim(l, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
			return fmt.Errorf("%q isn't a host name: letters, digits, - and dots, optionally after *., e.g. registry.npmjs.org", h)
		}
	}
	last := labels[len(labels)-1]
	switch {
	case last[0] >= '0' && last[0] <= '9':
		// No top-level domain starts with a digit; an address in any
		// form does (10.0.0.1, 0x7f.0x1, 127.0.0.0x1).
		return fmt.Errorf("%q is an address: name the host", h)
	case len(labels) < 2 && wild:
		return fmt.Errorf("%q opens a whole top-level domain: name the domain, e.g. *.example.com", h)
	case len(labels) < 2:
		return fmt.Errorf("%q is one word: name the whole host, e.g. registry.npmjs.org", h)
	case slices.Contains(labels, "localhost") || last == "localdomain":
		return fmt.Errorf("%q is this computer: local ports are the Local ports setting's (local_ports)", h)
	case loopbackName(name) != "":
		return fmt.Errorf("%q is under %s, whose names resolve to any address, this computer's and your network's too: local ports are the Local ports setting's (local_ports)", h, loopbackName(name))
	}
	if _, err := publicsuffix.EffectiveTLDPlusOne(name); err != nil {
		return fmt.Errorf("%q is a public suffix (every site under it has its own owner): name the host or domain, e.g. *.example.co.uk", h)
	}
	return nil
}

// loopbackDomains are domains whose names resolve to the address they
// spell (127.0.0.1.nip.io) or to this computer: a host rule under one
// opens local ports or your network, which local_ports alone opens.
// Others can exist; OPERATIONS says so.
var loopbackDomains = []string{"nip.io", "sslip.io", "xip.io", "lvh.me", "localtest.me", "traefik.me", "localhost.direct", "vcap.me"}

// loopbackName is the loopbackDomains domain name is, or is under; "".
func loopbackName(name string) string {
	for _, d := range loopbackDomains {
		if name == d || strings.HasSuffix(name, "."+d) {
			return d
		}
	}
	return ""
}

// sharedHosts are hosts that serve many owners' data, with whose.
var sharedHosts = map[string]string{
	"storage.googleapis.com":        "every Google Cloud Storage bucket's host",
	"raw.githubusercontent.com":     "every GitHub repository's files",
	"objects.githubusercontent.com": "every GitHub release's downloads",
	"s3.amazonaws.com":              "every Amazon S3 bucket's host",
}

// hostWarning is why host rule h, which passes its check, opens more
// than one site, or "".
func hostWarning(h string) string {
	if d, ok := strings.CutPrefix(h, "*."); ok {
		return "opens every host under " + d + " to every command a thread runs"
	}
	if whose, ok := sharedHosts[h]; ok {
		return h + " is " + whose + ": a thread's commands can download from, or send your files to, any of them"
	}
	return ""
}

// Names of the guidance texts tm ships.
const (
	GuideBrowserTesting   = "browser-testing"
	GuideBrowserDownloads = "browser-downloads"
)

// A Guide is a guidance text tm adds after a thread's brief, named by a
// guides rule. Its text is the server's, worded from the thread's other
// rules (downloads open or not, local ports open or not).
type Guide struct {
	Name, Title, About string
}

// Guides are the guidance texts tm ships, in the order a brief gets
// them.
var Guides = []Guide{
	{GuideBrowserTesting, "browser testing",
		"how to run Playwright and other Chromium tools inside macOS's sandbox: the headless shell in one process, and a ready Playwright test setup; it says whether downloads and local ports are open"},
	{GuideBrowserDownloads, "browser downloads",
		"how to install npm packages and Playwright's browser inside the sandbox: the npm cache and browsers path of the thread's own, and curl when Playwright's downloader is cut off"},
}

// GuideNamed is the guide called name.
func GuideNamed(name string) (Guide, bool) {
	i := slices.IndexFunc(Guides, func(g Guide) bool { return g.Name == name })
	if i < 0 {
		return Guide{}, false
	}
	return Guides[i], true
}

// CheckGuide checks one guidance rule: the name of a text tm ships.
func CheckGuide(name string) error {
	if err := checkSandboxText(name); err != nil {
		return err
	}
	if _, ok := GuideNamed(name); !ok {
		names := make([]string, len(Guides))
		for i, g := range Guides {
			names[i] = g.Name
		}
		return fmt.Errorf("%q isn't a guidance text tm has: %s", name, strings.Join(names, ", "))
	}
	return nil
}
