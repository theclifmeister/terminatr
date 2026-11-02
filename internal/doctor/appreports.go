package doctor

import (
	"fmt"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/appreport"
)

// appReportsNew is how far back a report counts as new for doctor.
const appReportsNew = 7 * 24 * time.Hour

// AppReports shows, under "apps", the crash and hang reports the apps
// sent this server (T957) in the last 7 days, the newest named: a
// warning, so a new crash is seen; tm device reports lists them. Nothing
// when none came in that time.
func AppReports(d Deps) []Check {
	return appReports(appreport.DirIn(d.Paths.Home), time.Now())
}

func appReports(dir string, now time.Time) []Check {
	ms, err := appreport.Recent(dir, now.Add(-appReportsNew))
	if err != nil {
		return []Check{{Group: "apps", Name: "reports", Status: Warn, Detail: firstLine(err.Error())}}
	}
	if len(ms) == 0 {
		return nil
	}
	n := map[string]int{}
	var kinds []string
	for _, m := range ms {
		if n[m.Kind] == 0 {
			kinds = append(kinds, m.Kind)
		}
		n[m.Kind]++
	}
	var parts []string
	for _, k := range kinds {
		parts = append(parts, fmt.Sprintf("%d %s", n[k], k))
	}
	m := ms[0]
	from := m.Device
	if from == "" {
		from = "the Mac app"
	}
	return []Check{{Group: "apps", Name: "reports", Status: Warn,
		Detail: fmt.Sprintf("%s in the last 7 days; newest a %s from %s, app %s build %s: tm device reports", strings.Join(parts, ", "), m.Kind, from, m.AppVersion, m.Build)}}
}
