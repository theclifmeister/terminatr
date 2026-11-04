package search

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// RelatedHeading starts the brief's section of related past work. That
// section stays out of the index: other tasks' titles in a brief would
// make each thread look related to what it was once told about.
const RelatedHeading = "Related past work"

// RelatedKinds are the kinds a brief's related past work comes from.
var RelatedKinds = []string{KindTask, KindThread, KindDesign}

// RelatedTask is the task a thread starts on, as filed.
type RelatedTask struct {
	Ref   string // T988
	Title string
	Notes string
	// Created is the day it was filed (YYYY-MM-DD); "" sets no bound.
	Created string
}

// RelatedRow is one past task a search of a task's text found.
type RelatedRow struct {
	ID    string
	Title string
}

// What a task's text holds that names rather than says: as words they
// match the other chunks that happen to carry an id, not what the task
// is about.
var (
	wikiLabelRE = regexp.MustCompile(`\[\[[^\]]*\|([^\]]*)\]\]`)
	wikiLinkRE  = regexp.MustCompile(`\[\[([^\]]*)\]\]`)
	queryNoise  = []*regexp.Regexp{
		regexp.MustCompile(`https?://\S+`),
		regexp.MustCompile(`\S*threads/t-[0-9]+\S*`), // a path into a thread's folder
		regexp.MustCompile(`\S*\.terminatr/\S*`),
		regexp.MustCompile(`\bT[0-9]+\b`),
		regexp.MustCompile(`\bt-[0-9]{3,}\b`),
		regexp.MustCompile(`#[0-9]+\b`),
		regexp.MustCompile(`\b(?:PR|pr|run|runs)\s+[0-9]+\b`),
		regexp.MustCompile(`\b[0-9]{6,}\b`), // a CI run's number
	}
	namedTaskRE = regexp.MustCompile(`\bT[1-9][0-9]*\b`)
)

// RelatedQuery is the words to search a task's title and notes for:
// those without links' targets and without ids of tasks, threads, pull
// requests and CI runs.
func RelatedQuery(title, notes string) string {
	s := title + "\n" + notes
	s = wikiLabelRE.ReplaceAllString(s, "$1")
	s = wikiLinkRE.ReplaceAllString(s, "$1")
	for _, re := range queryNoise {
		s = re.ReplaceAllString(s, " ")
	}
	return strings.Join(strings.Fields(s), " ")
}

// NamedTasks are the tasks a text names.
func NamedTasks(text string) []string {
	return namedTaskRE.FindAllString(text, -1)
}

// taskNumber is a task ref's number, 0 when it isn't one.
func taskNumber(ref string) int {
	if !strings.HasPrefix(ref, "T") {
		return 0
	}
	n, err := strconv.Atoi(ref[1:])
	if err != nil {
		return 0
	}
	return n
}

// Related is the past tasks a task's text finds, best first, at most
// limit: one row per task, from RelatedKinds, as a thread's search sees
// them (never the coordinator's chunks), without the half-the-words
// floor, which is for a person's few words. A chunk of no task is left
// out. So are the task itself, those it names already (it has them) and
// what is newer than it: a later task, and a thread's text written after
// the day it was filed. A task's or design's own text stays: those are
// rewritten in place, so their times say little.
func (x *Index) Related(t RelatedTask, limit int) ([]RelatedRow, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if _, err := x.refresh(); err != nil {
		return nil, err
	}
	words := RelatedQuery(t.Title, t.Notes)
	r := x.search(Query{Words: words, Kinds: RelatedKinds, NoFloor: true})
	named := NamedTasks(t.Title + "\n" + t.Notes)
	self := taskNumber(t.Ref)
	var filedEnd time.Time
	if day, err := time.Parse("2006-01-02", t.Created); err == nil {
		// The day's end by any clock: Created is a local date.
		filedEnd = day.Add(48 * time.Hour)
	}
	seen := map[string]bool{}
	rows := []RelatedRow{}
	for _, h := range r.Hits {
		n := taskNumber(h.Task)
		switch {
		case n == 0, self > 0 && n >= self, seen[h.Task], slices.Contains(named, h.Task),
			h.Kind == KindThread && !filedEnd.IsZero() && h.Time.After(filedEnd):
			continue
		}
		seen[h.Task] = true
		rows = append(rows, RelatedRow{ID: h.Task, Title: x.taskTitle(h)})
		if len(rows) == limit {
			break
		}
	}
	return rows, nil
}

// taskTitle is the title of hit h's task, from its chunk, without the
// task's id; the hit's own when the index no longer holds the task.
func (x *Index) taskTitle(h Hit) string {
	c := x.byID[h.Task]
	if c == nil {
		return h.Title
	}
	if m := taskHeadRE.FindStringSubmatch(c.Title); m != nil {
		return m[2]
	}
	return c.Title
}
