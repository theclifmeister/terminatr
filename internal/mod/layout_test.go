package mod

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// TestParseLayoutV1: the v1 array is the sidebar's columns, as before,
// and needs no newer API.
func TestParseLayoutV1(t *testing.T) {
	l, err := ParseLayout([]byte(`sidebar = ["count", "review-age.age", "state", "needs"]`))
	want := Layout{Columns: []string{"count", "review-age.age", "state", "needs"}}
	if err != nil || !reflect.DeepEqual(l, want) {
		t.Fatalf("%+v %v", l, err)
	}
	if err := l.CheckAPI("1.0"); err != nil {
		t.Errorf("v1 under 1.0: %v", err)
	}
	for _, src := range []string{"", "sidebar = []"} {
		if l, err := ParseLayout([]byte(src)); err != nil || l.V2 || len(l.Columns) != 0 {
			t.Errorf("%q: %+v %v", src, l, err)
		}
	}
}

// TestParseLayoutV2: every key of the v2 tables, sizes in cells and
// shares.
func TestParseLayoutV2(t *testing.T) {
	l, err := ParseLayout([]byte(`
[sidebar]
columns = ["count", "ci", "state", "remote", "paused", "inactive", "needs"]
side    = "right"
width   = "22%"
mode    = "hidden"
[details]
place = "bottom"
size  = 12
[header]
style = "hidden"
[footer]
style = "compact"
[status_bar]
style = "compact"
[rows]
density = "compact"
[dashboard]
sections = ["project", "needs"]
`))
	if err != nil {
		t.Fatal(err)
	}
	want := Layout{
		Columns: []string{"count", "ci", "state", "remote", "paused", "inactive", "needs"},
		Side:    "right", Width: Size{Share: 0.22}, Mode: "hidden",
		Details: "bottom", DetSize: Size{Cells: 12},
		Header: "hidden", Footer: "compact", StatusBar: "compact", Density: "compact",
		Sections: []string{"project", "needs"},
		V2:       true, v2Key: "[sidebar] columns",
	}
	if !reflect.DeepEqual(l, want) {
		t.Fatalf("got  %+v\nwant %+v", l, want)
	}
	if l.Width.String() != `"22%"` || l.DetSize.String() != "12" || !(Size{}).IsZero() {
		t.Errorf("sizes %s %s", l.Width, l.DetSize)
	}
	// The three layouts terminatr-mods asked for (design §2.1).
	for name, src := range map[string]string{
		"zen":     "[sidebar]\nmode = \"hidden\"\n[details]\nplace = \"off\"\n[footer]\nstyle = \"compact\"\n[dashboard]\nsections = [\"needs\", \"project\"]\n",
		"cockpit": "[sidebar]\nside = \"right\"\nwidth = \"22%\"\n[details]\nplace = \"bottom\"\nsize = \"40%\"\n",
		"compact": "[sidebar]\nwidth = 30\n[footer]\nstyle = \"compact\"\n[status_bar]\nstyle = \"compact\"\n[rows]\ndensity = \"compact\"\n",
	} {
		if l, err := ParseLayout([]byte(src)); err != nil || !l.V2 {
			t.Errorf("%s: %+v %v", name, l, err)
		}
	}
	// What is only a fallback is not refused: no needs, a hidden
	// sidebar, an empty table.
	for _, src := range []string{"[dashboard]\nsections = [\"other\"]", "[sidebar]\nmode = \"hidden\"", "[rows]", "[details]\nplace = \"right\"\nsize = 24"} {
		if _, err := ParseLayout([]byte(src)); err != nil {
			t.Errorf("%q refused: %v", src, err)
		}
	}
}

// TestParseLayoutRefuses: each refusal of the design's §2.3, by its key.
func TestParseLayoutRefuses(t *testing.T) {
	for _, tc := range []struct{ toml, want string }{
		// Unknown keys and tables.
		{"sidebar = []\nfooter_style = 1", "unknown key footer_style"},
		{"[sidebar]\nside = \"left\"\ncolour = \"red\"", "unknown key sidebar.colour (sidebar takes columns, side, width, mode)"},
		{"[info]\nwidth = 30", "unknown key info"},
		// Wrong types.
		{`sidebar = "count"`, "sidebar: an array of columns, or a table"},
		{"details = 1", "details: a table, [details]"},
		{"[sidebar]\nwidth = true", `sidebar.width: true; an integer or a share like "25%"`},
		{"[sidebar]\nwidth = \"wide\"", `sidebar.width: "wide"; an integer or a share like "25%"`},
		{"[sidebar]\nwidth = 36.5", `sidebar.width: 36.5; an integer`},
		{"[sidebar]\ncolumns = \"count\"", "sidebar.columns: a list of columns"},
		{"[sidebar]\nside = 1", "sidebar.side: 1; a string, left or right"},
		{"[dashboard]\nsections = \"needs\"", "dashboard.sections: a list of sections"},
		{"[dashboard]\nsections = [1]", "dashboard.sections: sections are strings"},
		// Enum values.
		{"[sidebar]\nside = \"top\"", `sidebar.side: "top"; left or right`},
		{"[sidebar]\nmode = \"tiny\"", `sidebar.mode: "tiny"; full, slim or hidden`},
		{"[details]\nplace = \"left\"", `details.place: "left"; right, bottom or off`},
		{"[header]\nstyle = \"compact\"", `header.style: "compact": the header is one row, shown or hidden`},
		{"[footer]\nstyle = \"big\"", `footer.style: "big"; shown, compact or hidden`},
		{"[status_bar]\nstyle = \"hidden\"", `status_bar.style: "hidden"; shown or compact`},
		{"[rows]\ndensity = \"dense\"", `rows.density: "dense"; normal or compact`},
		// Sizes out of range.
		{"[sidebar]\nwidth = 8", "sidebar.width: 8; at least 14"},
		{"[sidebar]\nwidth = 201", "sidebar.width: 201; at most 200"},
		{"[sidebar]\nwidth = \"5%\"", `sidebar.width: "5%"; 10% to 60%`},
		{"[sidebar]\nwidth = \"0%\"", `sidebar.width: "0%"; 10% to 60%`},
		{"[details]\nsize = \"90%\"", `details.size: "90%"; 20% to 70%`},
		{"[details]\nsize = 20", "details.size: 20; at least 24"},
		{"[details]\nplace = \"bottom\"\nsize = 4", "details.size: 4; at least 6 rows"},
		{"[details]\nplace = \"bottom\"\nsize = 101", "details.size: 101; at most 100 rows"},
		{"[details]\nplace = \"off\"\nsize = \"30%\"", "details.size has no effect when place is off"},
		// Columns, as v1, in either form.
		{`sidebar = ["count", "bogus"]`, `sidebar: no column "bogus"`},
		{`sidebar = ["count", "count"]`, "sidebar: count is listed twice"},
		{`sidebar = ["count", 1]`, "sidebar: columns are strings"},
		{"sidebar = [" + strings.Repeat(`"a.b", `, 33) + "]", "sidebar: 33 columns, at most 32"},
		{"[sidebar]\ncolumns = [\"bogus\"]", `sidebar.columns: no column "bogus"`},
		{"[sidebar]\ncolumns = [\"ci\", \"ci\"]", "sidebar.columns: ci is listed twice"},
		// Sections.
		{"[dashboard]\nsections = [\"needs\", \"tasks\"]", `dashboard.sections: no section "tasks" (needs, project, other, cards)`},
		{"[dashboard]\nsections = [\"needs\", \"needs\"]", "dashboard.sections: needs is listed twice"},
	} {
		if _, err := ParseLayout([]byte(tc.toml)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s:\n  %v\n  want %q", tc.toml, err, tc.want)
		}
	}
	// Every error is said, not only the first.
	_, err := ParseLayout([]byte("[sidebar]\nside = \"top\"\nwidth = 8\n[header]\nstyle = \"compact\""))
	if err == nil || strings.Count(err.Error(), "\n") != 2 {
		t.Errorf("joined errors: %v", err)
	}
}

// TestLayoutAPI: a v2 layout needs mod API 1.5, naming its first v2 key;
// a v1 one goes with any 1.x.
func TestLayoutAPI(t *testing.T) {
	for _, tc := range []struct{ toml, api, want string }{
		{`sidebar = ["count"]`, "1.0", ""},
		{"[sidebar]\nside = \"right\"", "1.4", `[sidebar] side needs api = "1.5" in mod.toml`},
		{"[sidebar]\ncolumns = [\"count\"]", "1.0", `[sidebar] columns needs api = "1.5"`},
		{"[rows]", "1.4", `[rows] needs api = "1.5"`},
		{"[footer]\nstyle = \"compact\"", "1.4", `[footer] style needs api = "1.5"`},
		{"[sidebar]\nside = \"right\"", "1.5", ""},
		{"[sidebar]\nside = \"right\"", "2.0", ""}, // CheckAPI refuses the major
	} {
		l, err := ParseLayout([]byte(tc.toml))
		if err != nil {
			t.Fatalf("%s: %v", tc.toml, err)
		}
		err = l.CheckAPI(tc.api)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%s under %s: %v, want %q", tc.toml, tc.api, err, tc.want)
		}
	}
}

// TestInstallLayoutAPI: tm mod install refuses a v2 layout in a mod that
// says api 1.4, with the 1.5 message, and takes it at 1.5.
func TestInstallLayoutAPI(t *testing.T) {
	withHome(t)
	files := func(api string) map[string]string {
		return map[string]string{
			"mod.toml":    strings.Replace(dataMod("cockpit", `layout = "layout.toml"`), `api = "1.0"`, `api = "`+api+`"`, 1),
			"layout.toml": "[sidebar]\nside = \"right\"\nwidth = \"22%\"\n",
		}
	}
	src := install(t, t.TempDir(), "cockpit", files("1.4"))
	st, err := Fetch(Source{Path: src})
	if st != nil {
		st.Close()
	}
	if err == nil || !strings.Contains(err.Error(), `layout: [sidebar] side needs api = "1.5" in mod.toml`) {
		t.Fatalf("api 1.4: %v", err)
	}
	src = install(t, t.TempDir(), "cockpit", files("1.5"))
	if _, err := Validate(src); err != nil {
		t.Fatalf("api 1.5: %v", err)
	}
}

// TestDefaultLayoutDocumented: the bundled default-layout, the copy-me
// template, documents every key: uncommented, its lines set every field
// and still parse.
func TestDefaultLayoutDocumented(t *testing.T) {
	data := BundledData(BundledLayout, KindLayout)
	l, err := ParseLayout(data)
	if err != nil || l.CheckAPI("1.5") != nil {
		t.Fatalf("%+v %v", l, err)
	}
	if l.Side != "" || !l.Width.IsZero() || l.Mode != "" || l.Details != "" || l.Header != "" || l.Sections != nil {
		t.Errorf("the default layout sets more than the columns: %+v", l)
	}
	keyLine := regexp.MustCompile(`(?m)^# ?([a-z_]+ += .*)$`)
	all, err := ParseLayout(keyLine.ReplaceAll(data, []byte("$1")))
	if err != nil {
		t.Fatal(err)
	}
	v := reflect.ValueOf(all)
	for i := range v.NumField() {
		if f := v.Type().Field(i); f.IsExported() && v.Field(i).IsZero() {
			t.Errorf("default-layout doesn't document %s", f.Name)
		}
	}
}
