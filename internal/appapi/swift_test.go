package appapi

import (
	"flag"
	"os"
	"slices"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the Mac app's AppAPI.swift from the schema")

const swiftFile = "../../macos/Sources/TerminatrKit/AppAPI.swift"

// The Mac app's types are Swift(MakeSchema()): `go test ./internal/appapi
// -update` rewrites them.
func TestSwiftCurrent(t *testing.T) {
	got := Swift(MakeSchema())
	if *update {
		if err := os.WriteFile(swiftFile, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(swiftFile)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatal("macos/Sources/TerminatrKit/AppAPI.swift is not the schema's: run go test ./internal/appapi -update")
	}
}

// An embedded struct's fields are the outer one's in the schema, as
// encoding/json writes them: program.get's ProgramDetail embeds Program,
// and the apps decode its slug and members at the top level.
func TestSchemaEmbeddedFlat(t *testing.T) {
	d := MakeSchema().Defs["ProgramDetail"]
	if _, ok := d.Properties["Program"]; ok {
		t.Fatal("ProgramDetail has a Program property; encoding/json flattens it")
	}
	for _, k := range []string{"slug", "members", "authority", "umbrellas"} {
		if _, ok := d.Properties[k]; !ok {
			t.Errorf("ProgramDetail lacks %q", k)
		}
	}
	if !slices.Contains(d.Required, "slug") || slices.Contains(d.Required, "goal") {
		t.Errorf("required = %v, want slug in, goal (omitempty) out", d.Required)
	}
}
