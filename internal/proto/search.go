package proto

import "time"

// tm search (design agent-recall, T916): the project's history in three
// layers, as tm search, tm search timeline and tm search show ask them,
// and the MCP tools search, search_timeline and search_show. The server
// holds each project's index (internal/search) and answers on both
// sockets: the human on the human socket names any of this server's
// projects; an agent its own only (scopeProject). A thread reads it
// without the coordinator's: notes with audience coordinator and the
// inbox (where link messages arrive). The server's hello lists
// CapSearch.
const (
	// MethodSearchQuery finds chunks (SearchQueryParams): a
	// SearchQueryResult, best first.
	MethodSearchQuery = "search.query"
	// MethodSearchTimeline is what happened around a chunk
	// (SearchTimelineParams): a SearchTimelineResult. An id the index
	// doesn't hold (or the caller may not see) is refused ("no-chunk").
	MethodSearchTimeline = "search.timeline"
	// MethodSearchShow is chunks' text by id (SearchShowParams): a
	// SearchShowResult. A bare thread or task id answers its chunks.
	MethodSearchShow = "search.show"
)

// CapSearch says the server answers search.query, search.timeline and
// search.show.
const CapSearch = "search"

// ErrNoChunk is the code of an id the index doesn't hold.
const ErrNoChunk = "no-chunk"

// Bounds of search.show: a chunk's text at most SearchShowEach bytes,
// all at most SearchShowAll; what is cut says so.
const (
	SearchShowEach = 6 << 10
	SearchShowAll  = 24 << 10
)

// SearchQueryParams: Words to find (by stem), and optionally only In
// these kinds (notes, tasks, threads, journal, designs, library,
// context, inbox), Since and Until (YYYY-MM-DD, both days included), of
// Task or Thread, at most Limit hits (20 when 0). Reindex rebuilds the
// index first (the human and the coordinator only).
type SearchQueryParams struct {
	Project string   `json:"project"`
	Words   string   `json:"words"`
	In      []string `json:"in,omitempty"`
	Since   string   `json:"since,omitempty"`
	Until   string   `json:"until,omitempty"`
	Task    string   `json:"task,omitempty"`
	Thread  string   `json:"thread,omitempty"`
	Limit   int      `json:"limit,omitempty"`
	Reindex bool     `json:"reindex,omitempty"`
}

// SearchHit is a chunk: its id, kind, title, time, task and thread,
// its file in the project folder (Member inside a tar.gz), and for a
// hit its Score, the query's stems it holds (Terms) and More, the
// journal lines of its task folded into it.
type SearchHit struct {
	ID     string    `json:"id"`
	Kind   string    `json:"kind"`
	Title  string    `json:"title"`
	Time   time.Time `json:"time"`
	Task   string    `json:"task,omitempty"`
	Thread string    `json:"thread,omitempty"`
	Path   string    `json:"path"`
	Member string    `json:"member,omitempty"`
	Score  float64   `json:"score,omitempty"`
	Terms  []string  `json:"terms,omitempty"`
	More   int       `json:"more,omitempty"`
}

// SearchQueryResult are the hits, best first, of Total in all; Terms
// are the stems searched; Dir is the project folder the paths are in;
// Read is how many files this query's update read, Errors those it
// could not.
type SearchQueryResult struct {
	Project string      `json:"project"`
	Dir     string      `json:"dir"`
	Hits    []SearchHit `json:"hits"`
	Total   int         `json:"total"`
	Terms   []string    `json:"terms"`
	Read    int         `json:"read,omitempty"`
	Errors  []string    `json:"errors,omitempty"`
}

// SearchTimelineParams: the chunk ID, how many chunks Before and After
// it (8 each when 0), and All for the project's journal lines besides
// its task's and thread's.
type SearchTimelineParams struct {
	Project string `json:"project"`
	ID      string `json:"id"`
	Before  int    `json:"before,omitempty"`
	After   int    `json:"after,omitempty"`
	All     bool   `json:"all,omitempty"`
}

// SearchTimelineResult is the chunk At and those Before and After it,
// each in time order.
type SearchTimelineResult struct {
	Project string      `json:"project"`
	At      SearchHit   `json:"at"`
	Before  []SearchHit `json:"before"`
	After   []SearchHit `json:"after"`
}

// SearchShowParams name chunks by id; a bare thread (t-0833) or task
// (T914) id names that thread's or task's chunks.
type SearchShowParams struct {
	Project string   `json:"project"`
	IDs     []string `json:"ids"`
}

// SearchText is one id search.show answers: a chunk with its Text (Cut
// when it was longer than what is given, of Bytes in all), or for a
// bare thread or task id its Chunks, or Error for an id it can't show.
type SearchText struct {
	SearchHit
	Text   string      `json:"text,omitempty"`
	Bytes  int         `json:"bytes,omitempty"`
	Cut    bool        `json:"cut,omitempty"`
	Chunks []SearchHit `json:"chunks,omitempty"`
	Error  string      `json:"error,omitempty"`
}

// SearchShowResult is search.show's answer, in the order asked; Dir is
// the project folder.
type SearchShowResult struct {
	Project string       `json:"project"`
	Dir     string       `json:"dir"`
	Items   []SearchText `json:"items"`
}
