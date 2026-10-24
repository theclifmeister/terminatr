package thread

import (
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/mdfile/mdfiletest"
)

// TestStatusYAML: STATUS.md's front matter reads the same from YAML as
// from TOML, and its todos from the body after either fence.
func TestStatusYAML(t *testing.T) {
	at := time.Date(2026, 10, 24, 18, 12, 56, 0, time.UTC)
	mdfiletest.SameYAML(t, &Status{Thread: "t-0001", Task: "T1", Percent: 40, PercentSource: "steps", Current: "c", StepsDone: 2, StepsTotal: 5, TodosDone: 1, TodosTotal: 3,
		Activity: "a", NeedsYou: "q", SelfPercent: 30, SelfAt: at, Done: true, Updated: at})
	st, err := ParseStatus([]byte("---\npercent: 40\nneeds_you: \"which: this?\"\nupdated: 2026-10-24T18:12:56Z\n---\n- [x] one\n- [ ] two\n"))
	if err != nil || st.Percent != 40 || st.NeedsYou != "which: this?" || st.SelfPercent != -1 || len(st.Todos) != 2 || !st.Updated.Equal(at) {
		t.Fatalf("%+v %v", st, err)
	}
}
