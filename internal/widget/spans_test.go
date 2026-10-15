package widget

import (
	"strings"
	"testing"
	"time"
)

var spanNow = time.Date(2026, 10, 13, 14, 0, 0, 0, time.UTC)

func TestSpanWords(t *testing.T) {
	for _, tc := range []struct {
		s    Span
		want string
	}{
		{Span{Text: "x"}, "x"},
		{Span{Ago: "2026-10-13T13:59:20Z"}, "40s ago"},
		{Span{Ago: "2026-10-13T16:59:20+03:00"}, "40s ago"},
		{Span{Ago: "2026-10-13T11:55:00Z"}, "2h5m ago"},
		{Span{Ago: "2026-10-13T14:00:30Z"}, "0s ago"}, // the mod's clock ahead
		{Span{Until: "2026-10-13T14:01:20Z"}, "1m20s"},
		{Span{Until: "2026-10-13T14:00:00Z"}, "due"},
		{Span{Until: "2026-10-13T13:00:00Z"}, "due"},
		{Span{Ago: "yesterday"}, ""},
	} {
		if got := tc.s.Words(spanNow); got != tc.want {
			t.Errorf("%+v: %q, want %q", tc.s, got, tc.want)
		}
	}
}

func TestCheckTimeSpans(t *testing.T) {
	for _, tc := range []struct{ js, want string }{
		{`{"kind":"text","lines":[[{"ago":"40s"}]]}`, "lines[0][0].ago: not a time in RFC 3339"},
		{`{"kind":"kv","pairs":[{"key":"k","value":[{"until":"2026-10-13 14:00"}]}]}`, "pairs[0].value[0].until: not a time"},
		{`{"kind":"list","id":"a","rows":[{"spans":[{"text":"x","ago":"2026-10-13T14:00:00Z"}]}]}`, "has 2 of text, ago and until"},
		{`{"kind":"list","id":"a","rows":[{"spans":[{"ago":"2026-10-13T14:00:00Z","until":"2026-10-13T14:00:00Z"}]}]}`, "has 2 of text, ago and until"},
	} {
		_, err := Decode([]byte(tc.js))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err %v, want %q", tc.js, err, tc.want)
		}
	}
	ss := []Span{{Ago: "2026-10-13T14:00:00Z", Role: "faint", Icon: "review"}, {Until: "2026-10-13T14:00:00+02:00"}}
	if err := CheckSpans(ss); err != nil {
		t.Errorf("CheckSpans: %v", err)
	}
	if err := CheckSpans([]Span{{Until: "soon"}}); err == nil || !strings.Contains(err.Error(), "spans[0].until") {
		t.Errorf("CheckSpans of a bad until: %v", err)
	}
}

// Time spans draw in every widget kind that has spans: a text widget's
// lines, a kv's values, a list's rows and the lines under one, a table's
// cells; and the drawing moves with the clock without a new publish.
func TestTimeSpansRender(t *testing.T) {
	ago := `{"ago":"2026-10-13T13:59:20Z"}`
	until := `{"until":"2026-10-13T14:01:20Z"}`
	tree := mustDecode(t, `{"kind":"stack","children":[
		{"kind":"text","lines":[[{"text":"text "},`+ago+`]]},
		{"kind":"kv","pairs":[{"key":"PRs","value":[`+ago+`,{"text":" · next "},`+until+`]}]},
		{"kind":"list","id":"l","rows":[{"id":"r","spans":[{"text":"row "},`+until+`],"more":[[{"text":"more "},`+ago+`]]}]},
		{"kind":"table","id":"t","columns":[{"title":"when"}],"rows":[{"id":"c","cells":[[{"text":"cell "},`+ago+`]]}]}
	]}`)
	if !tree.Timed() {
		t.Fatal("Timed: false")
	}
	clock := spanNow
	v := NewView()
	v.now = func() time.Time { return clock }
	v.Set(tree)
	got := strings.Join(v.Render(60, 20, plainLook{}), "\n")
	for _, want := range []string{"text 40s ago", "PRs  40s ago · next 1m20s", "row 1m20s", "more 40s ago", "cell 40s ago"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
	clock = spanNow.Add(90 * time.Second)
	got = strings.Join(v.Render(60, 20, plainLook{}), "\n")
	for _, want := range []string{"text 2m10s ago", "PRs  2m10s ago · next due", "row due", "cell 2m10s ago"} {
		if !strings.Contains(got, want) {
			t.Errorf("a second later, no %q in\n%s", want, got)
		}
	}
	if (&Widget{Kind: KindText, Lines: [][]Span{{{Text: "x"}}}}).Timed() {
		t.Error("a tree without time spans is Timed")
	}
}
