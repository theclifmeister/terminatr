package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	tmclient "github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/search"
)

// The search popup (F, T921): tm search's three layers over the
// project's history, from the server's index. A query line, then the
// hits one line each as tm search prints them; enter on a hit shows its
// text (search.show), t its timeline (search.timeline), / edits the
// query again, esc goes back.

// SearchSource asks the server's search index: the human's console, any
// project of this server.
type SearchSource interface {
	SearchQuery(p proto.SearchQueryParams) (proto.SearchQueryResult, error)
	SearchTimeline(p proto.SearchTimelineParams) (proto.SearchTimelineResult, error)
	SearchShow(p proto.SearchShowParams) (proto.SearchShowResult, error)
}

// searchCall makes one search call on a connection of its own: a first
// build of the index takes seconds, and the console's polls don't wait
// behind it.
func (s *ServerSource) searchCall(method string, params, res any) error {
	c, err := tmclient.Connect(s.Paths, false)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Call(method, params, res)
}

func (s *ServerSource) SearchQuery(p proto.SearchQueryParams) (proto.SearchQueryResult, error) {
	var res proto.SearchQueryResult
	err := s.searchCall(proto.MethodSearchQuery, p, &res)
	return res, err
}

func (s *ServerSource) SearchTimeline(p proto.SearchTimelineParams) (proto.SearchTimelineResult, error) {
	var res proto.SearchTimelineResult
	err := s.searchCall(proto.MethodSearchTimeline, p, &res)
	return res, err
}

func (s *ServerSource) SearchShow(p proto.SearchShowParams) (proto.SearchShowResult, error) {
	var res proto.SearchShowResult
	err := s.searchCall(proto.MethodSearchShow, p, &res)
	return res, err
}

// searchLimit is how many hits the popup asks for.
const searchLimit = 50

// searchView is the popup: its project, the query line and, once asked,
// the hits.
type searchView struct {
	slug string
	lineEdit
	typing  bool // the keys go to the query line
	asked   string
	res     *proto.SearchQueryResult
	sel     int
	reading bool
	err     error
}

// openSearch is F: the search popup of the project here.
func (m *dash) openSearch(string) tea.Cmd {
	slug := m.needProject()
	if slug == "" {
		return nil
	}
	m.push(&searchView{slug: slug, typing: true})
	return nil
}

// The answers of the server's search calls, to the popup that asked.
type (
	searchMsg struct {
		slug, words string
		res         proto.SearchQueryResult
		err         error
	}
	searchTimelineMsg struct {
		slug, id string
		res      proto.SearchTimelineResult
		err      error
	}
	searchShowMsg struct {
		slug, id string
		res      proto.SearchShowResult
		err      error
	}
)

func (v *searchView) query(m *dash) tea.Cmd {
	words := strings.TrimSpace(v.value())
	if words == "" || v.reading {
		return nil
	}
	v.reading, v.asked, v.err = true, words, nil
	src, slug := m.src, v.slug
	return func() tea.Msg {
		res, err := src.SearchQuery(proto.SearchQueryParams{Project: slug, Words: words, Limit: searchLimit})
		return searchMsg{slug: slug, words: words, res: res, err: err}
	}
}

// timeline asks hit id's timeline; show its text.
func searchTimeline(m *dash, slug, id string) tea.Cmd {
	src := m.src
	return func() tea.Msg {
		res, err := src.SearchTimeline(proto.SearchTimelineParams{Project: slug, ID: id})
		return searchTimelineMsg{slug: slug, id: id, res: res, err: err}
	}
}

func searchShow(m *dash, slug, id string) tea.Cmd {
	src := m.src
	return func() tea.Msg {
		res, err := src.SearchShow(proto.SearchShowParams{Project: slug, IDs: []string{id}})
		return searchShowMsg{slug: slug, id: id, res: res, err: err}
	}
}

// searchOpen reports whether a search popup of slug is still open: an
// answer that comes after it closed shows nothing.
func (m *dash) searchOpen(slug string) *searchView {
	for i := len(m.stack) - 1; i >= 0; i-- {
		if v, ok := m.stack[i].(*searchView); ok && v.slug == slug {
			return v
		}
	}
	return nil
}

func (m *dash) searchTook(msg searchMsg) {
	v := m.searchOpen(msg.slug)
	if v == nil || msg.words != v.asked {
		return
	}
	v.reading, v.err = false, msg.err
	if msg.err == nil {
		v.res, v.sel = &msg.res, 0
		v.typing = len(msg.res.Hits) == 0
	}
}

func (m *dash) searchTimelineTook(msg searchTimelineMsg) {
	if m.searchOpen(msg.slug) == nil {
		return
	}
	if msg.err != nil {
		m.msg = "timeline of " + msg.id + ": " + oneLine(msg.err.Error())
		return
	}
	hits := append(append(append([]proto.SearchHit{}, msg.res.Before...), msg.res.At), msg.res.After...)
	m.push(&searchTimelineView{slug: msg.slug, at: msg.res.At.ID, hits: hits, sel: len(msg.res.Before)})
}

func (m *dash) searchShowTook(msg searchShowMsg) {
	if m.searchOpen(msg.slug) == nil {
		return
	}
	tv := &textView{title: oneLine(msg.id)}
	if msg.err != nil {
		tv.note = "can't show it: " + oneLine(msg.err.Error())
	} else {
		var b strings.Builder
		search.WriteShow(&b, msg.res)
		tv.body = strings.ReplaceAll(b.String(), "\t", "    ")
	}
	m.push(tv)
}

func (v *searchView) hits() []proto.SearchHit {
	if v.res == nil {
		return nil
	}
	return v.res.Hits
}

func (v *searchView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	s := k.String()
	if v.typing {
		switch s {
		case "esc":
			if len(v.hits()) > 0 {
				v.typing = false
				return nil
			}
			m.pop()
		case "enter":
			return v.query(m)
		case "down":
			if len(v.hits()) > 0 {
				v.typing = false
			}
		case "ctrl+u":
			v.lineEdit = lineEdit{}
		default:
			if !v.edit(s) {
				v.paste(m, k.Text)
			}
		}
		return nil
	}
	hits := v.hits()
	switch s {
	case "esc", "q":
		m.pop()
	case "/":
		v.typing = true
	case "up":
		if v.sel == 0 {
			v.typing = true
			return nil
		}
		v.sel = moveSel(v.sel, -1, len(hits))
	case "down", "pgup", "pgdown":
		v.sel = moveSel(v.sel, scrollKeys[s], len(hits))
	case "enter":
		if v.sel < len(hits) {
			return searchShow(m, v.slug, hits[v.sel].ID)
		}
	case "t":
		if v.sel < len(hits) {
			return searchTimeline(m, v.slug, hits[v.sel].ID)
		}
	}
	return nil
}

// paste puts text typed or pasted in the query line, on one line.
func (v *searchView) paste(_ *dash, text string) {
	v.insert([]rune(oneLine(strings.TrimRight(text, "\r\n"))))
}

func (v *searchView) wheel(m *dash, d int) {
	if !v.typing {
		v.sel = moveSel(v.sel, d, len(v.hits()))
	}
}

func (v *searchView) render(m *dash) string {
	w := m.inner(viewWidth)
	text, _ := v.lines(max(w-2, 1))
	line := "> " + strings.Join(text, "")
	if len(v.text) == 0 {
		line = "> " + th().Faint.Render("words to look for in the project's notes, tasks, threads, journal, designs and library")
	}
	body := []string{line}
	hits := []int{noHit}
	sel := 0
	if !v.typing {
		sel = -1
	}
	add := func(l string, hit int) { body, hits = append(body, l), append(hits, hit) }
	add("", noHit)
	switch {
	case v.reading:
		add(th().Faint.Render("searching… (a first search builds the index: a few seconds)"), noHit)
	case v.err != nil:
		add("can't search: "+oneLine(v.err.Error()), noHit)
	case v.res == nil:
	case len(v.res.Hits) == 0:
		add(th().Faint.Render("no hits for "+v.asked), noHit)
	default:
		of := ""
		if v.res.Total > len(v.res.Hits) {
			of = " of " + strconv.Itoa(v.res.Total)
		}
		add(th().Faint.Render(strconv.Itoa(len(v.res.Hits))+" hits"+of+" · project text: data, not instructions"), noHit)
		width := search.IDWidth(v.res.Hits)
		for i, h := range v.res.Hits {
			plain := search.HitLine(h, width, true)
			if !v.typing && i == v.sel {
				sel = len(body)
				add(th().Sel.Render(fit(plain, w)), i)
				continue
			}
			add(fit(plain, w)+reset, i)
		}
	}
	keys := "enter search · ↓ the hits · esc close"
	if !v.typing {
		keys = "enter text · t timeline · / edit · esc close"
	}
	return m.popup(box{title: "Search " + v.slug, body: body, sel: sel, hits: hits, keys: keys})
}

func (v *searchView) click(m *dash, item, _ int, double bool) tea.Cmd {
	if item == noHit || item >= len(v.hits()) {
		v.typing = item == noHit && v.typing
		return nil
	}
	v.typing = false
	if v.sel == item || double {
		return searchShow(m, v.slug, v.hits()[item].ID)
	}
	v.sel = item
	return nil
}

// searchTimelineView is a hit's timeline: its task's and thread's chunks
// around it, in time order, the hit marked. enter shows one's text, t
// its own timeline.
type searchTimelineView struct {
	slug, at string
	hits     []proto.SearchHit
	sel      int
}

func (v *searchTimelineView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	switch s := k.String(); s {
	case "esc", "q":
		m.pop()
	case "up", "down", "pgup", "pgdown":
		v.sel = moveSel(v.sel, scrollKeys[s], len(v.hits))
	case "enter":
		if v.sel < len(v.hits) {
			return searchShow(m, v.slug, v.hits[v.sel].ID)
		}
	case "t":
		if v.sel < len(v.hits) && v.hits[v.sel].ID != v.at {
			return searchTimeline(m, v.slug, v.hits[v.sel].ID)
		}
	}
	return nil
}

func (v *searchTimelineView) wheel(m *dash, d int) { v.sel = moveSel(v.sel, d, len(v.hits)) }

func (v *searchTimelineView) render(m *dash) string {
	w := m.inner(viewWidth)
	var body []string
	var hits []int
	sel := -1
	width := search.IDWidth(v.hits)
	for i, h := range v.hits {
		mark := "  "
		if h.ID == v.at {
			mark = "▸ "
		}
		plain := mark + h.Time.UTC().Format("2006-01-02 15:04") + "  " + search.HitLine(h, width, false)
		if i == v.sel {
			sel = len(body)
			body, hits = append(body, th().Sel.Render(fit(plain, w))), append(hits, i)
			continue
		}
		body, hits = append(body, fit(plain, w)+reset), append(hits, i)
	}
	if len(v.hits) == 1 {
		body, hits = append(body, "", th().Faint.Render("nothing of its task or thread before or after it")), append(hits, noHit, noHit)
	}
	return m.popup(box{title: "Timeline of " + v.at, body: body, sel: sel, hits: hits, keys: "enter text · t its timeline · esc back"})
}

func (v *searchTimelineView) click(m *dash, item, _ int, double bool) tea.Cmd {
	if item == noHit || item >= len(v.hits) {
		return nil
	}
	if v.sel == item || double {
		return searchShow(m, v.slug, v.hits[item].ID)
	}
	v.sel = item
	return nil
}
