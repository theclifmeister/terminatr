package project

import (
	"cmp"
	"path"
	"slices"
	"strings"
	"unicode"
)

// Scores of Match: a query term in a note's title, one only in its
// body, and a path the note touches.
const (
	ScoreTitle = 3
	ScoreBody  = 1
	ScorePath  = 4
	// MinScore is the least score a hit needs.
	MinScore = 3
)

// Hit is a note Match found, with why: its score, the query terms it
// holds and the paths it shares with the query's.
type Hit struct {
	Note  Note     `json:"note"`
	Score int      `json:"score"`
	Terms []string `json:"terms,omitempty"`
	Paths []string `json:"paths,omitempty"`
}

// Match scores notes against query (words) and paths (files, folders
// or repo:<name>) and answers the best limit of those that clear, best
// first (all of them when limit is 0 or less). Each query term counts
// once per note: ScoreTitle in its title, else ScoreBody in its body;
// each of paths a note touches counts ScorePath. A note clears with a
// shared path, two matched terms or a title term, and a score of at
// least MinScore.
func Match(notes []Note, query string, paths []string, limit int) []Hit {
	terms := Terms(query)
	var hits []Hit
	for _, n := range notes {
		h := Hit{Note: n}
		title, body := wordSet(n.Title), wordSet(n.Body)
		inTitle := false
		for _, t := range terms {
			switch {
			case title[t]:
				h.Score += ScoreTitle
				inTitle = true
			case body[t]:
				h.Score += ScoreBody
			default:
				continue
			}
			h.Terms = append(h.Terms, t)
		}
		for _, q := range paths {
			if slices.ContainsFunc(n.Touches, func(t string) bool { return pathsMeet(q, t) }) {
				h.Score += ScorePath
				h.Paths = append(h.Paths, q)
			}
		}
		if (len(h.Paths) > 0 || len(h.Terms) >= 2 || inTitle) && h.Score >= MinScore {
			hits = append(hits, h)
		}
	}
	slices.SortStableFunc(hits, func(a, b Hit) int {
		return cmp.Or(cmp.Compare(b.Score, a.Score), cmp.Compare(a.Note.Path, b.Note.Path))
	})
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// stopWords are words too common to match on.
var stopWords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true, "be": true, "by": true,
	"for": true, "from": true, "has": true, "in": true, "is": true, "it": true, "its": true,
	"of": true, "on": true, "or": true, "that": true, "the": true, "this": true, "to": true,
	"was": true, "with": true, "not": true, "but": true, "into": true, "when": true, "what": true,
}

// Terms are query's words to match: lowercased runs of letters and
// digits, two or more long, less stop words and repeats, in order.
func Terms(query string) []string {
	var out []string
	for _, w := range words(query) {
		if len(w) >= 2 && !stopWords[w] && !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return out
}

func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func wordSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, w := range words(s) {
		set[w] = true
	}
	return set
}

// pathsMeet reports whether a query path and a note's touch are the same
// file or folder, or one is a folder holding the other. repo:<name>
// entries meet only themselves.
func pathsMeet(a, b string) bool {
	a, b = cleanTouch(a), cleanTouch(b)
	if a == "" || b == "" {
		return false
	}
	if strings.HasPrefix(a, "repo:") || strings.HasPrefix(b, "repo:") {
		return a == b
	}
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func cleanTouch(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, `\`, "/"))
	if s == "" || strings.HasPrefix(s, "repo:") {
		return s
	}
	s = path.Clean(s)
	if s == "." || s == "/" {
		return ""
	}
	return strings.TrimPrefix(s, "./")
}
