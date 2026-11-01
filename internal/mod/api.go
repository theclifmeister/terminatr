package mod

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/theclifmeister/terminatr/internal/update"
	"github.com/theclifmeister/terminatr/internal/version"
)

// The mod API this tm speaks (mods design §5, Versioning): one major at
// a time, at a minor. Within a major, events, methods and fields are
// only added; removing or changing one is a new major.
const (
	APIMajor = 1
	APIMinor = 13
)

// APIVersion is "MAJOR.MINOR" of the mod API this tm speaks.
func APIVersion() string { return fmt.Sprintf("%d.%d", APIMajor, APIMinor) }

// CheckAPI reports why this tm won't load m: the mod API it was written
// for has another major, or a newer minor, than this tm's (proto.Check's
// rule applied to mods), or it needs a newer tm (min_tm). A source build,
// whose version isn't a release's, passes min_tm.
func CheckAPI(m *Manifest) error { return checkAPI(m, APIMajor, APIMinor, version.Version) }

func checkAPI(m *Manifest, major, minor int, tm string) error {
	maj, min, ok := splitAPI(m.API)
	if !ok {
		return fmt.Errorf("%s: api %q isn't MAJOR.MINOR", m.ID, m.API)
	}
	have := fmt.Sprintf("%d.%d", major, minor)
	switch {
	case maj > major || maj == major && min > minor:
		return fmt.Errorf("%s needs mod API %s; this tm has %s: run tm update", m.ID, m.API, have)
	case maj < major:
		return fmt.Errorf("%s was written for mod API %s; this tm has %s and no longer loads it: update the mod", m.ID, m.API, have)
	}
	if m.MinTM != "" {
		if newer, ok := update.Newer(m.MinTM, tm); ok && newer {
			return fmt.Errorf("%s needs tm %s; this tm is %s: run tm update", m.ID, m.MinTM, tm)
		}
	}
	return nil
}

// Before says whether m was written for a mod API 1.x older than
// 1.minor; false when its api doesn't read.
func (m *Manifest) Before(minor int) bool {
	maj, min, ok := splitAPI(m.API)
	return ok && maj == 1 && min < minor
}

// splitAPI reads "MAJOR.MINOR".
func splitAPI(s string) (major, minor int, ok bool) {
	a, b, found := strings.Cut(s, ".")
	if !found || !apiRE.MatchString(s) {
		return 0, 0, false
	}
	major, _ = strconv.Atoi(a)
	minor, _ = strconv.Atoi(b)
	return major, minor, true
}
