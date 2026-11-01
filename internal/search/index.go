// Package search is tm search's index over a project's history
// (design agent-recall, T916): memory notes, CONTEXT.md and PROJECT.md,
// tasks and their archive, threads' briefs, follow-ups and reports, the
// journal and its archives, designs, the library's text files and the
// inbox. Each file is cut into chunks with ids (chunk.go, sources.go);
// an inverted index of their stems (project.Stems, the stemmer tm
// memory find uses) ranks them by BM25 (project.BM25).
//
// The index lives in its own folder (~/.terminatr/state/search/<slug>/,
// never the project's: it is derived and rebuilt at will), one file per
// segment: notes and context, tasks, designs, JOURNAL.md, each month of
// journal archive, each hundred threads, the library, the inbox and
// each month of its archive. A segment file is a gzipped gob of its
// files' sizes and times and their chunks with each chunk's stems; the
// postings are built from those on load. Before each query a stat walk
// finds the files that changed and reads only those again; JOURNAL.md is
// read on from where the index left it; an archive is read once, and
// again only if it changes. Format is the version: a segment of another
// is rebuilt.
package search

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"encoding/gob"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
)

// Format is the index's version. Bump it when chunks, ids, stems or the
// segment files change: an index of another version is rebuilt.
const Format = 1

// TitleWeight is how many body words a word of a chunk's head (its
// section's heading, or its file's title) counts as, as in tm memory
// find (project.TitleWeight).
const TitleWeight = project.TitleWeight

// SaveEvery is how long changes may stay in memory only before Update
// writes them: a crash in between loses nothing, since the next load
// reads again the files whose sizes or times differ from those saved.
const SaveEvery = 30 * time.Second

// saveRead is how many files read in one update make it save at once.
const saveRead = 64

// tailLen is how many bytes before the end of what the index read of
// JOURNAL.md it keeps, to tell lines appended from a file rewritten.
const tailLen = 64

// Index is one project's search index. Its methods are safe for
// concurrent use; one process should hold it (the server).
type Index struct {
	project string // the project's folder
	dir     string // the index's folder

	mu       sync.Mutex
	loaded   bool
	segs     map[string]*segment
	byID     map[string]*chunk
	lastSave time.Time
	// now is time.Now, or a test's clock.
	now func() time.Time
}

// Open is the index of the project at projectDir, kept in dir. It reads
// nothing until it is used.
func Open(projectDir, dir string) *Index {
	return &Index{project: projectDir, dir: dir, now: time.Now}
}

// Dir is the index's folder.
func (x *Index) Dir() string { return x.dir }

// segment is one segment in memory: its files, their chunks, and the
// postings of each stem. Stems are ids into the segment's dict, so a
// chunk's list of them costs 4 bytes a stem.
type segment struct {
	name   string
	files  map[string]*fileEnt
	chunks []*chunk
	dict   map[string]int32 // stem → id
	stems  []string         // id → stem
	post   [][]posting      // id → postings
	dead   int
	dirty  bool
	// count and length are the live chunks' number and total weighted
	// length, all and the coordinator's only.
	count, countCoord   int
	length, lengthCoord float64
}

type fileEnt struct {
	source
	// Done is how much of JOURNAL.md is indexed (to a line's end), Tail
	// the bytes before it.
	Done   int64
	Tail   string
	Err    string
	chunks []*chunk
}

type chunk struct {
	Chunk
	idx    int32
	length float32
	terms  []int32 // the segment's stem ids, ascending
	tf     []uint16
	dead   bool
}

type posting struct {
	c  int32
	tf float32
}

// The segment file.
type segDisk struct {
	Format int
	Name   string
	Terms  []string
	Files  []fileDisk
}

type fileDisk struct {
	// Src is a named field: gob leaves out an embedded unexported one.
	Src    source
	Done   int64
	Tail   string
	Err    string
	Chunks []chunkDisk
}

type chunkDisk struct {
	Chunk
	Length float32
	Terms  []int32
	TF     []uint16
}

// Stats is what an update did and what the index holds.
type Stats struct {
	Segments int `json:"segments"`
	Files    int `json:"files"`
	Chunks   int `json:"chunks"`
	Terms    int `json:"terms"`
	// Read is how many files this update read (changed, new or grown);
	// Removed how many it dropped.
	Read    int `json:"read"`
	Removed int `json:"removed"`
	// Errors are the files it could not read, with why.
	Errors []string      `json:"errors,omitempty"`
	Took   time.Duration `json:"took"`
}

// Update brings the index up to date with the project's files: it
// reads again those whose size or time changed, reads on in JOURNAL.md,
// drops those gone, and saves when SaveEvery has passed.
func (x *Index) Update() (Stats, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.update(false)
}

// Reindex throws the index away and builds it again from all files.
func (x *Index) Reindex() (Stats, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.update(true)
}

// Flush writes the segments changed since the last save.
func (x *Index) Flush() error {
	x.mu.Lock()
	defer x.mu.Unlock()
	if !x.loaded {
		return nil
	}
	unlock, err := x.lock()
	if err != nil {
		return err
	}
	defer unlock()
	return x.save()
}

// Close writes what changed and lets the index go from memory; it loads
// again when used.
func (x *Index) Close() error {
	err := x.Flush()
	x.mu.Lock()
	x.loaded, x.segs, x.byID = false, nil, nil
	x.mu.Unlock()
	return err
}

func (x *Index) lock() (func(), error) {
	if err := os.MkdirAll(x.dir, 0o700); err != nil {
		return nil, err
	}
	return mdfile.Lock(filepath.Join(x.dir, "index"))
}

func (x *Index) update(rebuild bool) (Stats, error) {
	start := time.Now()
	var st Stats
	unlock, err := x.lock()
	if err != nil {
		return st, err
	}
	defer unlock()
	if rebuild {
		if err := x.wipe(); err != nil {
			return st, err
		}
		x.segs, x.byID, x.loaded = map[string]*segment{}, map[string]*chunk{}, true
	}
	if !x.loaded {
		x.load()
	}
	srcs, err := walk(x.project)
	if err != nil {
		return st, err
	}
	bySeg := map[string][]source{}
	for _, s := range srcs {
		bySeg[s.Seg] = append(bySeg[s.Seg], s)
	}
	for name, seg := range x.segs {
		if _, ok := bySeg[name]; !ok {
			for _, fe := range seg.files {
				x.drop(seg, fe)
				st.Removed++
			}
			delete(x.segs, name)
			if err := os.Remove(x.segPath(name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return st, err
			}
		}
	}
	// First every file gone or changed leaves, then the changed and new
	// are read: a thread's folder packed into its tarball keeps its
	// chunks' ids. Segments go in name order, so ids made unique (-2)
	// come out the same each time.
	names := make([]string, 0, len(bySeg))
	for name := range bySeg {
		names = append(names, name)
	}
	slices.Sort(names)
	var todo []source
	for _, name := range names {
		list := bySeg[name]
		seg := x.segs[name]
		if seg == nil {
			seg = newSegment(name)
			x.segs[name] = seg
		}
		seen := make(map[string]bool, len(list))
		for _, s := range list {
			seen[s.Rel] = true
			fe := seg.files[s.Rel]
			if fe != nil && fe.Size == s.Size && fe.MTime == s.MTime && fe.File == s.File {
				continue
			}
			todo = append(todo, s)
			if fe != nil && !(fe.File == fileJournal && s.File == fileJournal && s.Size >= fe.Done) {
				x.drop(seg, fe)
			}
		}
		for rel, fe := range seg.files {
			if !seen[rel] {
				x.drop(seg, fe)
				st.Removed++
			}
		}
	}
	for _, s := range todo {
		st.Read++
		seg := x.segs[s.Seg]
		if fe := seg.files[s.Rel]; fe != nil {
			// JOURNAL.md, read on where lines were only appended.
			ok, err := x.journalOn(seg, fe, s)
			if err != nil {
				st.Errors = append(st.Errors, s.Rel+": "+err.Error())
			}
			if ok || err != nil {
				continue
			}
			x.drop(seg, fe)
		}
		fe := &fileEnt{source: s}
		ps, data, err := parse(x.project, s)
		if err != nil {
			fe.Err = err.Error()
			st.Errors = append(st.Errors, s.Rel+": "+fe.Err)
		}
		if s.File == fileJournal && err == nil {
			fe.Done, fe.Tail = journalDone(data)
			ps = slices.DeleteFunc(ps, func(p piece) bool { return int64(p.Off) >= fe.Done })
		}
		x.add(seg, fe, ps)
	}
	for _, seg := range x.segs {
		if seg.dead > 256 && seg.dead > len(seg.chunks)/4 {
			seg.compact()
		}
	}
	// A build (many files read at once) is saved at once, not to be
	// done again after a crash; small changes wait for SaveEvery.
	if rebuild || st.Read >= saveRead || x.now().Sub(x.lastSave) >= SaveEvery {
		if err := x.save(); err != nil {
			return st, err
		}
	}
	x.fill(&st)
	st.Took = time.Since(start)
	return st, nil
}

// journalDone is how much of a journal's data is whole lines, and the
// tailLen bytes before that.
func journalDone(data []byte) (int64, string) {
	done := bytes.LastIndexByte(data, '\n') + 1
	return int64(done), string(data[max(done-tailLen, 0):done])
}

// journalOn reads on in JOURNAL.md from where the index left it, when
// what it read is still there (lines were only appended). It reports
// false when the file was rewritten, to be read whole again.
func (x *Index) journalOn(seg *segment, fe *fileEnt, s source) (bool, error) {
	data, err := readSource(x.project, s)
	if err != nil {
		return false, err
	}
	if int64(len(data)) < fe.Done || string(data[max(fe.Done-tailLen, 0):fe.Done]) != fe.Tail {
		return false, nil
	}
	seen := map[string]bool{}
	for _, c := range fe.chunks[max(len(fe.chunks)-64, 0):] {
		seen[c.ID] = true
	}
	text := string(data)
	ps := withText(journalLines(text, int(fe.Done), seen, Chunk{Path: s.Rel}), text)
	done, tail := journalDone(data)
	ps = slices.DeleteFunc(ps, func(p piece) bool { return int64(p.Off) >= done })
	fe.Size, fe.MTime, fe.Done, fe.Tail = s.Size, s.MTime, done, tail
	for _, p := range ps {
		x.addChunk(seg, fe, p)
	}
	seg.dirty = true
	return true, nil
}

func newSegment(name string) *segment {
	return &segment{name: name, files: map[string]*fileEnt{}, dict: map[string]int32{}}
}

// stem is a stem's id in the segment, added when new.
func (seg *segment) stem(t string) int32 {
	id, ok := seg.dict[t]
	if !ok {
		id = int32(len(seg.stems))
		seg.dict[t] = id
		seg.stems = append(seg.stems, t)
		seg.post = append(seg.post, nil)
	}
	return id
}

// add puts a file read into the segment with its pieces.
func (x *Index) add(seg *segment, fe *fileEnt, ps []piece) {
	seg.files[fe.Rel] = fe
	seg.dirty = true
	for _, p := range ps {
		x.addChunk(seg, fe, p)
	}
}

// addChunk adds one piece, its id made unique within the index.
func (x *Index) addChunk(seg *segment, fe *fileEnt, p piece) {
	text := p.text
	id := p.ID
	for k := 2; x.byID[id] != nil; k++ {
		id = p.ID + "-" + strconv.Itoa(k)
	}
	p.ID = id
	c := &chunk{Chunk: p.Chunk, idx: int32(len(seg.chunks))}
	tf := map[string]float64{}
	length := 0.0
	for _, t := range project.Stems(p.head) {
		tf[t] += TitleWeight
		length += TitleWeight
	}
	if p.Off >= 0 && p.Off+p.Len <= len(text) {
		for _, t := range project.Stems(text[p.Off : p.Off+p.Len]) {
			tf[t]++
			length++
		}
	}
	c.length = float32(length)
	c.terms = make([]int32, 0, len(tf))
	for t := range tf {
		c.terms = append(c.terms, seg.stem(t))
	}
	slices.Sort(c.terms)
	c.tf = make([]uint16, len(c.terms))
	for i, t := range c.terms {
		c.tf[i] = uint16(min(tf[seg.stems[t]], math.MaxUint16))
	}
	seg.put(c)
	fe.chunks = append(fe.chunks, c)
	x.byID[c.ID] = c
}

// put adds a chunk to the segment's chunks and postings.
func (seg *segment) put(c *chunk) {
	c.idx = int32(len(seg.chunks))
	seg.chunks = append(seg.chunks, c)
	for i, t := range c.terms {
		seg.post[t] = append(seg.post[t], posting{c: c.idx, tf: float32(c.tf[i])})
	}
	seg.count++
	seg.length += float64(c.length)
	if c.Coordinator {
		seg.countCoord++
		seg.lengthCoord += float64(c.length)
	}
}

// drop takes a file and its chunks out of the segment; the chunks stay
// in the postings, dead, until the segment is compacted.
func (x *Index) drop(seg *segment, fe *fileEnt) {
	for _, c := range fe.chunks {
		c.dead = true
		seg.dead++
		seg.count--
		seg.length -= float64(c.length)
		if c.Coordinator {
			seg.countCoord--
			seg.lengthCoord -= float64(c.length)
		}
		if x.byID[c.ID] == c {
			delete(x.byID, c.ID)
		}
	}
	delete(seg.files, fe.Rel)
	seg.dirty = true
}

// postings are a stem's postings in the segment.
func (seg *segment) postings(t string) []posting {
	if id, ok := seg.dict[t]; ok {
		return seg.post[id]
	}
	return nil
}

// compact rebuilds the segment's chunks and postings without the dead.
func (seg *segment) compact() {
	live := slices.DeleteFunc(seg.chunks, func(c *chunk) bool { return c.dead })
	seg.rebuild(live)
}

// rebuild makes the segment's chunks and postings from chunks, each
// stem's postings allocated to size.
func (seg *segment) rebuild(chunks []*chunk) {
	n := make([]int32, len(seg.stems))
	for _, c := range chunks {
		for _, t := range c.terms {
			n[t]++
		}
	}
	seg.post = make([][]posting, len(seg.stems))
	for t, k := range n {
		if k > 0 {
			seg.post[t] = make([]posting, 0, k)
		}
	}
	seg.chunks, seg.dead = make([]*chunk, 0, len(chunks)), 0
	seg.count, seg.countCoord, seg.length, seg.lengthCoord = 0, 0, 0, 0
	for _, c := range chunks {
		seg.put(c)
	}
}

func (x *Index) segPath(name string) string { return filepath.Join(x.dir, name+".gob.gz") }

// load reads the segment files; one that doesn't decode, or is of
// another Format, is left out, so its files are read again.
func (x *Index) load() {
	x.segs, x.byID, x.loaded = map[string]*segment{}, map[string]*chunk{}, true
	x.lastSave = x.now()
	ents, err := os.ReadDir(x.dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		name, ok := strings.CutSuffix(e.Name(), ".gob.gz")
		if !ok || !e.Type().IsRegular() {
			continue
		}
		var d segDisk
		if err := readGob(x.segPath(name), &d); err != nil || d.Format != Format || d.Name != name {
			_ = os.Remove(x.segPath(name))
			continue
		}
		seg := newSegment(name)
		for _, t := range d.Terms {
			seg.stem(t)
		}
		if len(seg.stems) != len(d.Terms) {
			_ = os.Remove(x.segPath(name)) // a stem twice: not one this wrote
			continue
		}
		// The strings many chunks share are kept once.
		strs := map[string]string{}
		intern := func(s *string) {
			if v, ok := strs[*s]; ok {
				*s = v
			} else {
				strs[*s] = *s
			}
		}
		var chunks []*chunk
		for _, f := range d.Files {
			fe := &fileEnt{source: f.Src, Done: f.Done, Tail: f.Tail, Err: f.Err}
			seg.files[fe.Rel] = fe
			for _, cd := range f.Chunks {
				c := &chunk{Chunk: cd.Chunk, length: cd.Length, terms: cd.Terms, tf: cd.TF}
				if len(c.terms) != len(c.tf) || slices.ContainsFunc(c.terms, func(t int32) bool { return t < 0 || int(t) >= len(d.Terms) }) {
					continue
				}
				for _, s := range []*string{&c.Kind, &c.Path, &c.Member, &c.Task, &c.Thread} {
					intern(s)
				}
				chunks = append(chunks, c)
				fe.chunks = append(fe.chunks, c)
				x.byID[c.ID] = c
			}
		}
		seg.rebuild(chunks)
		x.segs[name] = seg
	}
}

// save writes the changed segments, each to a temp file renamed over
// the old.
func (x *Index) save() error {
	if err := os.MkdirAll(x.dir, 0o700); err != nil {
		return err
	}
	if err := fsx.WriteAtomic(filepath.Join(x.dir, "FORMAT"), []byte(strconv.Itoa(Format)+"\n"), 0o600); err != nil {
		return err
	}
	for name, seg := range x.segs {
		if !seg.dirty {
			continue
		}
		// The dict is written anew, without the stems of dead chunks.
		d := segDisk{Format: Format, Name: name}
		ids := map[int32]int32{}
		rels := make([]string, 0, len(seg.files))
		for rel := range seg.files {
			rels = append(rels, rel)
		}
		slices.Sort(rels)
		for _, rel := range rels {
			fe := seg.files[rel]
			fd := fileDisk{Src: fe.source, Done: fe.Done, Tail: fe.Tail, Err: fe.Err}
			for _, c := range fe.chunks {
				cd := chunkDisk{Chunk: c.Chunk, Length: c.length, Terms: make([]int32, len(c.terms)), TF: c.tf}
				for i, t := range c.terms {
					id, ok := ids[t]
					if !ok {
						id = int32(len(d.Terms))
						ids[t] = id
						d.Terms = append(d.Terms, seg.stems[t])
					}
					cd.Terms[i] = id
				}
				// Ascending in the new ids too.
				order := make([]int, len(cd.Terms))
				for i := range order {
					order[i] = i
				}
				slices.SortFunc(order, func(a, b int) int { return cmp.Compare(cd.Terms[a], cd.Terms[b]) })
				terms, tf := make([]int32, len(order)), make([]uint16, len(order))
				for i, o := range order {
					terms[i], tf[i] = cd.Terms[o], c.tf[o]
				}
				cd.Terms, cd.TF = terms, tf
				fd.Chunks = append(fd.Chunks, cd)
			}
			d.Files = append(d.Files, fd)
		}
		if err := writeGob(x.segPath(name), d); err != nil {
			return err
		}
		seg.dirty = false
	}
	x.lastSave = x.now()
	return nil
}

// wipe removes the index's files.
func (x *Index) wipe() error {
	ents, err := os.ReadDir(x.dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".gob.gz") || e.Name() == "FORMAT" {
			if err := os.Remove(filepath.Join(x.dir, e.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}

func readGob(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	return gob.NewDecoder(zr).Decode(v)
}

func writeGob(path string, v any) error {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err := gob.NewEncoder(zw).Encode(v); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return fsx.WriteAtomic(path, buf.Bytes(), 0o600)
}

func (x *Index) fill(st *Stats) {
	terms := map[string]bool{}
	st.Segments = len(x.segs)
	for _, seg := range x.segs {
		st.Files += len(seg.files)
		st.Chunks += seg.count
		for id, t := range seg.stems {
			if len(seg.post[id]) > 0 {
				terms[t] = true
			}
		}
	}
	st.Terms = len(terms)
}

// Query is what to search for and where.
type Query struct {
	// Words are matched by stem, each once (project.Terms); a chunk
	// holding any scores, more and rarer ones score higher.
	Words string
	// Kinds limits the hits to these kinds (Kinds); none is all.
	Kinds []string
	// Since and Until limit the hits to chunks of that time, Task and
	// Thread to those of that task or thread.
	Since, Until time.Time
	Task         string
	Thread       string
	// Coordinator: the caller is the coordinator (or the human), who
	// sees the inbox and the coordinator's notes; a thread does not.
	Coordinator bool
	// Limit is the most hits answered; 0 or less is all.
	Limit int
}

// Hit is a chunk a search found, with its score and the query's stems
// it holds.
type Hit struct {
	Chunk
	Score float64  `json:"score"`
	Terms []string `json:"terms"`
}

// Result is a search's hits, best first, and how many there were in
// all before Limit.
type Result struct {
	Hits  []Hit    `json:"hits"`
	Total int      `json:"total"`
	Terms []string `json:"terms"`
	Stats Stats    `json:"stats"`
}

// Search updates the index (Update) and answers q's hits: BM25 over
// stems, the collection being the chunks the caller may see, a chunk's
// head weighing TitleWeight. Ties go to the newer chunk.
func (x *Index) Search(q Query) (Result, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	st, err := x.update(false)
	if err != nil {
		return Result{}, err
	}
	r := x.search(q)
	r.Stats = st
	return r, nil
}

func (x *Index) search(q Query) Result {
	terms := project.Terms(q.Words)
	r := Result{Terms: terms}
	if len(terms) == 0 {
		return r
	}
	visible := func(c *chunk) bool { return !c.dead && (q.Coordinator || !c.Coordinator) }
	n, total := 0, 0.0
	for _, seg := range x.segs {
		n += seg.count
		total += seg.length
		if !q.Coordinator {
			n -= seg.countCoord
			total -= seg.lengthCoord
		}
	}
	if n == 0 {
		return r
	}
	avg := max(total/float64(n), 1)
	df := make([]float64, len(terms))
	for i, t := range terms {
		for _, seg := range x.segs {
			for _, p := range seg.postings(t) {
				if visible(seg.chunks[p.c]) {
					df[i]++
				}
			}
		}
	}
	type acc struct {
		c     *chunk
		score float64
		terms []string
	}
	found := map[*chunk]*acc{}
	for i, t := range terms {
		for _, seg := range x.segs {
			for _, p := range seg.postings(t) {
				c := seg.chunks[p.c]
				if !visible(c) || !q.wants(c) {
					continue
				}
				a := found[c]
				if a == nil {
					a = &acc{c: c}
					found[c] = a
				}
				a.score += project.BM25(float64(p.tf), df[i], float64(n), float64(c.length), avg)
				a.terms = append(a.terms, t)
			}
		}
	}
	r.Hits = make([]Hit, 0, len(found))
	for _, a := range found {
		r.Hits = append(r.Hits, Hit{Chunk: a.c.Chunk, Score: math.Round(a.score*100) / 100, Terms: a.terms})
	}
	slices.SortFunc(r.Hits, func(a, b Hit) int {
		return cmp.Or(cmp.Compare(b.Score, a.Score), b.Time.Compare(a.Time), cmp.Compare(a.ID, b.ID))
	})
	r.Total = len(r.Hits)
	if q.Limit > 0 && len(r.Hits) > q.Limit {
		r.Hits = r.Hits[:q.Limit]
	}
	return r
}

// wants reports whether a chunk is of the kinds, time, task and thread
// q asks for.
func (q Query) wants(c *chunk) bool {
	switch {
	case len(q.Kinds) > 0 && !slices.Contains(q.Kinds, c.Kind),
		!q.Since.IsZero() && c.Time.Before(q.Since),
		!q.Until.IsZero() && c.Time.After(q.Until),
		q.Task != "" && c.Task != q.Task,
		q.Thread != "" && c.Thread != q.Thread:
		return false
	}
	return true
}

// ErrNoChunk is Text's error for an id the index doesn't hold.
var ErrNoChunk = errors.New("no such chunk in the index")

// Text updates the index and answers the chunk of an id and its text,
// read from its file. With coordinator false a coordinator's chunk is
// ErrNoChunk.
func (x *Index) Text(id string, coordinator bool) (Chunk, string, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if _, err := x.update(false); err != nil {
		return Chunk{}, "", err
	}
	c := x.byID[id]
	if c == nil || c.dead || c.Coordinator && !coordinator {
		return Chunk{}, "", fmt.Errorf("%s: %w", id, ErrNoChunk)
	}
	text, err := readText(x.project, c.Chunk)
	if err != nil {
		return c.Chunk, "", err
	}
	if c.Off < 0 || c.Off+c.Len > len(text) {
		return c.Chunk, "", fmt.Errorf("%s: %s changed since it was indexed", id, c.Path)
	}
	return c.Chunk, text[c.Off : c.Off+c.Len], nil
}

// readText reads the whole text a chunk is in: its file's, or its
// archive member's.
func readText(dir string, c Chunk) (string, error) {
	file := ""
	switch {
	case c.Member != "" && strings.HasPrefix(c.Path, "threads/archive/"):
		file = fileThreadZ
	case c.Member != "":
		file = fileInboxZ
	case strings.HasSuffix(c.Path, ".md.gz"):
		file = fileJournalZ
	case c.Path == "JOURNAL.md":
		file = fileJournal
	}
	data, err := readSource(dir, source{Rel: c.Path, File: file})
	if err != nil {
		return "", err
	}
	if c.Member == "" {
		return textOf(c.Path, data), nil
	}
	ms, err := untar(data)
	for _, m := range ms {
		if m.name == c.Member {
			return textOf(m.name, m.data), nil
		}
	}
	if err != nil {
		return "", err
	}
	return "", fmt.Errorf("%s: no %s in it", c.Path, c.Member)
}
