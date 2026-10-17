package ticker

import (
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/thread"
)

func (r *rig) report(text string) {
	r.t.Helper()
	if _, err := thread.StoreReport(r.p, "t-0001", text, nil, r.now); err != nil {
		r.t.Fatal(err)
	}
}

// A new report with ## Remember lines raises one remember item; a report
// without them raises none, and a newer report's item supersedes an older
// one still unhandled.
func TestRememberItem(t *testing.T) {
	r := newRig(t)
	r.sweep(0)
	r.report("## Report\nDid it.\n## Next\nMerge it\n")
	r.sweep(time.Second)
	if k := r.kinds(); k != "" {
		t.Fatalf("items for a report without lessons: %s", k)
	}
	r.report("## Report\nDid it.\n## Next\nMerge it\n## Remember\n- one\n* two\n")
	r.sweep(time.Second)
	items := r.items()
	if len(items) != 1 || items[0].Kind != KindRemember || items[0].Subject != "t-0001" ||
		items[0].Summary != "T1 Fix it (t-0001)'s report 2 has 2 lessons under ## Remember, not yet filed: put what lasts in memory as your own summary, or dismiss them (tm thread show t-0001), then tm inbox done <id>" {
		t.Fatalf("remember item: %+v", items)
	}
	r.sweep(time.Second)
	if k := r.kinds(); k != KindRemember {
		t.Fatalf("raised again without a new report: %s", k)
	}
	r.report("## Report\nFixed CI.\n## Next\nMerge it\n## Remember\n- one\n")
	r.sweep(time.Second)
	items = r.items()
	if len(items) != 1 || !strings.Contains(items[0].Summary, "report 3 has 1 lesson under") {
		t.Fatalf("not superseded: %+v", items)
	}
	// Filed: a later sweep raises nothing.
	r.handleAll()
	r.sweep(time.Second)
	if k := r.kinds(); k != "" {
		t.Fatalf("raised again once filed: %s", k)
	}
}
