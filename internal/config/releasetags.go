package config

import (
	"fmt"
	"strings"
)

// ReleaseTagsOff is release_tags = "none": no tag counts as a release.
// Safety has it as a list of this one word.
const ReleaseTagsOff = "none"

// A project's patterns can't be more, or longer, than this.
const (
	MaxReleaseTags       = 32
	MaxReleaseTagPattern = 100
)

// ReleaseTags is release_tags in the file: a list of patterns, or the
// word "none" for a list of that one word.
type ReleaseTags []string

// UnmarshalTOML reads either form.
func (r *ReleaseTags) UnmarshalTOML(v any) error {
	switch v := v.(type) {
	case string:
		if v != ReleaseTagsOff {
			return fmt.Errorf("must be a list of patterns or %q, not %q", ReleaseTagsOff, v)
		}
		*r = ReleaseTags{ReleaseTagsOff}
		return nil
	case []any:
		out := make(ReleaseTags, 0, len(v))
		for _, x := range v {
			s, ok := x.(string)
			if !ok {
				return fmt.Errorf("must be a list of patterns or %q", ReleaseTagsOff)
			}
			out = append(out, s)
		}
		*r = out
		return nil
	}
	return fmt.Errorf("must be a list of patterns or %q", ReleaseTagsOff)
}

// ReleaseTagsAreOff says patterns are "none".
func ReleaseTagsAreOff(patterns []string) bool {
	return len(patterns) == 1 && patterns[0] == ReleaseTagsOff
}

// CheckReleaseTags checks release_tags: empty is the default rule, the
// one word none turns releases off, and each other is a tag name with *
// for any text.
func CheckReleaseTags(patterns []string) error {
	if len(patterns) > MaxReleaseTags {
		return limit(fmt.Sprintf("can have at most %d patterns", MaxReleaseTags))
	}
	if ReleaseTagsAreOff(patterns) {
		return nil
	}
	for _, p := range patterns {
		switch {
		case p == "":
			return limit("can't have an empty pattern")
		case p == ReleaseTagsOff:
			return limit(fmt.Sprintf("can't have %q beside patterns", ReleaseTagsOff))
		case len(p) > MaxReleaseTagPattern:
			return limit(fmt.Sprintf("patterns can have at most %d characters", MaxReleaseTagPattern))
		case strings.ContainsFunc(p, func(r rune) bool { return r <= ' ' || r == 0x7f }):
			return limit("patterns can't have spaces or control characters")
		}
	}
	return nil
}

// ReleaseTagsWords says patterns in the words of the settings: the rule,
// none, or the patterns themselves.
func ReleaseTagsWords(patterns []string) string {
	switch {
	case len(patterns) == 0:
		return "version tags"
	case ReleaseTagsAreOff(patterns):
		return ReleaseTagsOff
	}
	return strings.Join(patterns, ", ")
}

// ParseReleaseTags reads what a person typed: patterns split on spaces
// and commas. Nothing is the default rule, and "none" releases off.
func ParseReleaseTags(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' })
}
