package project

import (
	"cmp"
	"math"
	"path"
	"slices"
	"strings"
	"unicode"

	"github.com/kljensen/snowball/english"
)

// Scoring of Match: BM25 over each note's words, its title's counting
// TitleWeight times, plus ScorePath for each path it shares with the
// query.
const (
	// TitleWeight is how many body words a word of the title counts as.
	TitleWeight = 3
	// ScorePath is what each shared path adds: more than words ever
	// score in practice, and a note sharing more paths ranks first
	// whatever its words.
	ScorePath = 10
	// MinScore is the least word score a note needs without a shared
	// path: about one rare word in its title, or two in its body.
	MinScore = 1.0
	// StandOut is how many standard deviations above the mean word
	// score of the notes without a shared path a note's must be,
	// besides MinScore.
	StandOut = 1.0
	// idfNotes is the fewest notes idf counts: a word of a small
	// memory weighs as in one of this many notes, not as a common word.
	idfNotes = 10
	// bm25K1 and bm25B are BM25's term saturation and length
	// normalization.
	bm25K1 = 1.2
	bm25B  = 0.75
)

// Hit is a note Match found, with why: its score, the query words it
// holds and the paths it shares with the query's.
type Hit struct {
	Note  Note     `json:"note"`
	Score float64  `json:"score"`
	Terms []string `json:"terms,omitempty"`
	Paths []string `json:"paths,omitempty"`
}

// Match scores notes against query (words) and paths (files, folders
// or repo:<name>) and answers the best limit of those that clear, best
// first (all of them when limit is 0 or less). Words are compared by
// stem (Terms), each query word once: a note scores BM25 over its
// title (each word TitleWeight times) and body, with notes as
// the collection, so a word most notes hold weighs little. Each of
// paths the note touches adds ScorePath. A note clears with a shared
// path, or with a word score of at least MinScore from two words or
// one in its title; notes sharing more paths come first, then by
// score.
func Match(notes []Note, query string, paths []string, limit int) []Hit {
	terms, said := queryTerms(query)
	docs := make([]noteDoc, len(notes))
	df := map[string]int{}
	total := 0.0
	for i, n := range notes {
		docs[i] = newNoteDoc(n)
		total += docs[i].length
		for t := range docs[i].tf {
			df[t]++
		}
	}
	avg := max(total/float64(max(len(notes), 1)), 1)
	size := float64(max(len(notes), idfNotes))
	all := make([]Hit, len(notes))
	scores := make([]float64, len(notes))
	for i, n := range notes {
		h, d := Hit{Note: n}, docs[i]
		for _, t := range terms {
			tf := d.tf[t]
			nt := float64(df[t])
			idf := math.Log(1 + (size-nt+0.5)/(nt+0.5))
			if tf == 0 {
				continue
			}
			scores[i] += idf * tf * (bm25K1 + 1) / (tf + bm25K1*(1-bm25B+bm25B*d.length/avg))
			h.Terms = append(h.Terms, said[t])
			d.inTitle = d.inTitle || d.title[t]
		}
		for _, q := range paths {
			if slices.ContainsFunc(n.Touches, func(t string) bool { return pathsMeet(q, t) }) {
				h.Paths = append(h.Paths, q)
			}
		}
		h.Score = math.Round((scores[i]+float64(ScorePath*len(h.Paths)))*10) / 10
		docs[i], all[i] = d, h
	}
	// Notes in by a shared path don't compete: the bar is the rest's.
	var rest []float64
	for i, h := range all {
		if len(h.Paths) == 0 {
			rest = append(rest, scores[i])
		}
	}
	bar := max(MinScore, standOut(rest))
	var hits []Hit
	for i, h := range all {
		if len(h.Paths) > 0 || scores[i] >= bar && (len(h.Terms) >= min(2, len(terms)) || docs[i].inTitle) {
			hits = append(hits, h)
		}
	}
	slices.SortStableFunc(hits, func(a, b Hit) int {
		return cmp.Or(cmp.Compare(len(b.Paths), len(a.Paths)), cmp.Compare(b.Score, a.Score), cmp.Compare(a.Note.Path, b.Note.Path))
	})
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// standOut is the word score a note needs to stand out from the others
// without a shared path: StandOut standard deviations above the mean
// of their scores, counting at least idfNotes notes (the missing ones
// score 0), as idf does.
func standOut(scores []float64) float64 {
	n := float64(max(len(scores), idfNotes))
	sum, sq := 0.0, 0.0
	for _, s := range scores {
		sum += s
		sq += s * s
	}
	mean := sum / n
	return mean + StandOut*math.Sqrt(max(sq/n-mean*mean, 0))
}

// noteDoc is a note as Match reads it: its stems' weighted counts, the
// stems of its title, and its weighted length.
type noteDoc struct {
	tf     map[string]float64
	title  map[string]bool
	length float64
	// inTitle: a query word is in the title.
	inTitle bool
}

func newNoteDoc(n Note) noteDoc {
	d := noteDoc{tf: map[string]float64{}, title: map[string]bool{}}
	for _, t := range stems(n.Title) {
		d.tf[t] += TitleWeight
		d.title[t] = true
		d.length += TitleWeight
	}
	for _, t := range stems(n.Body) {
		d.tf[t]++
		d.length++
	}
	return d
}

// stopWords are words too common to match on, besides English's
// (english.IsStopWord).
var stopWords = map[string]bool{
	"also": true, "any": true, "can": true, "could": true, "would": true, "may": true, "might": true,
	"must": true, "shall": true, "get": true, "gets": true, "got": true, "make": true, "makes": true,
	"made": true, "one": true, "two": true, "new": true, "via": true, "etc": true, "per": true,
	"like": true, "way": true, "yes": true, "else": true, "every": true, "still": true, "even": true,
	"ever": true, "much": true, "many": true, "let": true, "see": true, "say": true, "says": true,
	"said": true, "use": true, "uses": true, "used": true, "using": true, "need": true, "needs": true,
	"want": true, "wants": true, "thing": true, "things": true, "done": true,
	// what is left of a contraction
	"isn": true, "aren": true, "wasn": true, "weren": true, "doesn": true, "didn": true, "won": true,
	"hasn": true, "haven": true, "couldn": true, "shouldn": true, "wouldn": true,
	// a note's section headings
	"decided": true, "ruled": true, "watch": true,
}

// stopWord reports whether w (lowercase) is a stop word.
func stopWord(w string) bool { return stopWords[w] || english.IsStopWord(w) }

// Terms are query's words to match, as stems: lowercased runs of
// letters and digits with a letter, three or more long, less stop
// words and repeats, in order. Numbers and short words never match.
func Terms(query string) []string {
	terms, _ := queryTerms(query)
	return terms
}

// queryTerms are Terms, and for each the first word of query that has
// it, to show.
func queryTerms(query string) ([]string, map[string]string) {
	var terms []string
	said := map[string]string{}
	for _, w := range words(query) {
		t := stem(w)
		if t == "" {
			continue
		}
		if _, seen := said[t]; !seen {
			terms = append(terms, t)
			said[t] = w
		}
	}
	return terms, said
}

// stems are s's words as Terms has them, repeats kept.
func stems(s string) []string {
	var out []string
	for _, w := range words(s) {
		if t := stem(w); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// stem is w's English stem, or "" for a word not to match: a stop
// word, one shorter than three or with no letter.
func stem(w string) string {
	if len(w) < 3 || stopWord(w) || strings.IndexFunc(w, unicode.IsLetter) < 0 {
		return ""
	}
	return english.Stem(w, true)
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
