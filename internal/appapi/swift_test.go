package appapi

import (
	"flag"
	"os"
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
