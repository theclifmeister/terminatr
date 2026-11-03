package program

import (
	"slices"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// TestUmbrellaRouteParts: a route's filed tasks become its umbrella's
// parts (a retry adds none twice); SetParts takes members' tasks only,
// for the director and the human.
func TestUmbrellaRouteParts(t *testing.T) {
	g, cfg := routeFixture(t, routeConfig)
	req := RouteRequest{Umbrella: "SSO", Tasks: []RouteTask{
		{Project: "acme-api", Title: "OIDC login"},
		{Project: "acme-web", Title: "Login button"},
	}}
	if _, err := g.Route(director, cfg, req); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Route(director, cfg, RouteRequest{Umbrella: "T1", Tasks: req.Tasks[:1]}); err != nil {
		t.Fatal(err)
	}
	u, _ := g.Tasks().Get(1)
	if !slices.Equal(u.Parts, []string{"acme-api/T1", "acme-web/T1"}) {
		t.Fatalf("parts %v", u.Parts)
	}
	if _, err := g.Route(director, cfg, RouteRequest{Umbrella: "T1", Tasks: []RouteTask{{Project: "acme-web", Title: "Logout"}}}); err != nil {
		t.Fatal(err)
	}
	if res, err := g.SetParts(director, cfg, "T1", []string{"acme-web/T2", "acme-api/T1"}, false); err != nil || !slices.Equal(res.Task.Parts, []string{"acme-web/T2", "acme-api/T1"}) {
		t.Fatalf("set: %+v %v", res.Task, err)
	}
	for _, c := range []struct {
		who   caller.Caller
		parts string
		code  string
	}{
		{director, "other/T1", "not-member"},
		{director, "acme-api/T9", "invalid-parts"},
		{director, "loner/T1", "not-member"},
		{director, "T3", "invalid-parts"},
		{caller.Caller{Kind: caller.Director, Project: "beta"}, "acme-api/T1", "other-program"},
		{caller.Caller{Kind: caller.Coordinator, Project: "acme-api"}, "acme-api/T1", "director-only"},
	} {
		_, err := g.SetParts(c.who, cfg, "T1", []string{c.parts}, true)
		if e, ok := err.(*Error); !ok || e.Code != c.code {
			if te, ok := err.(*tasks.Error); !ok || te.Code != c.code {
				t.Errorf("%v %s: %v, want %s", c.who, c.parts, err, c.code)
			}
		}
	}
	if _, err := g.SetParts(human, cfg, "T1", []string{"acme-web/T1"}, true); err != nil {
		t.Fatalf("human: %v", err)
	}
}

// TestDerive: an umbrella's status from its parts (§5.3).
func TestDerive(t *testing.T) {
	part := func(state, status string) PartState {
		return PartState{Ref: "a/T1", Dep: Dep{State: state, Status: status}}
	}
	met, open, gone := part(DepMet, "done"), part(DepOpen, "ready"), part(DepGone, "dropped")
	for _, c := range []struct {
		name  string
		own   tasks.Status
		parts []PartState
		want  tasks.Status
		ok    bool
	}{
		{"none", tasks.Ready, nil, "", false},
		{"done stays", tasks.Done, []PartState{open}, "", false},
		{"dropped stays", tasks.Dropped, []PartState{met}, "", false},
		{"all open", tasks.Ready, []PartState{open, open}, tasks.Ready, true},
		{"all open, was open", tasks.Open, []PartState{open}, tasks.Open, true},
		{"went back", tasks.Started, []PartState{open}, tasks.Ready, true},
		{"one started", tasks.Ready, []PartState{open, part(DepOpen, "started")}, tasks.Started, true},
		{"one in review", tasks.Open, []PartState{open, part(DepOpen, "review")}, tasks.Started, true},
		{"one met", tasks.Ready, []PartState{open, met}, tasks.Started, true},
		{"blocked wins", tasks.Ready, []PartState{met, part(DepOpen, "blocked"), part(DepOpen, "started")}, tasks.Blocked, true},
		{"all met", tasks.Started, []PartState{met, part(DepMet, "review")}, tasks.Review, true},
		{"gone left out", tasks.Started, []PartState{met, gone}, tasks.Review, true},
		{"only gone", tasks.Started, []PartState{gone}, "", false},
		{"unknown waits", tasks.Ready, []PartState{met, part(DepUnknown, "")}, tasks.Started, true},
	} {
		got, ok := Derive(&tasks.Task{Status: c.own, Parts: []string{"x"}}, c.parts)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: %q %v, want %q %v", c.name, got, ok, c.want, c.ok)
		}
	}
	m, of := Progress([]PartState{met, open, gone, part(DepMet, "review")})
	if m != 2 || of != 3 {
		t.Fatalf("progress %d/%d", m, of)
	}
	if got := PartsText([]PartState{{Ref: "acme-api/T12", Dep: Dep{State: DepMet, Status: "review", Merged: true}}, {Ref: "acme-web/T40", Dep: Dep{State: DepOpen, Status: "started"}}}); got != "1/2: acme-api/T12 merged, acme-web/T40 started" {
		t.Fatalf("PartsText %q", got)
	}
}
