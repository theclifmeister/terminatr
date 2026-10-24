// Package mdfiletest checks a front matter struct reads the same from
// YAML as from TOML, for the tests of the packages that have one.
package mdfiletest

import (
	"bytes"
	"reflect"
	"slices"
	"testing"

	"github.com/BurntSushi/toml"
	"go.yaml.in/yaml/v3"

	"github.com/theclifmeister/terminatr/internal/mdfile"
)

// SameYAML checks that front (a pointer to a filled front matter struct;
// times UTC, whole seconds) has the same keys in YAML as in TOML, and
// that mdfile.Decode reads its YAML into the value it reads from its
// TOML: every field has a yaml tag naming its toml key.
func SameYAML(t *testing.T, front any) {
	t.Helper()
	var tb bytes.Buffer
	if err := toml.NewEncoder(&tb).Encode(front); err != nil {
		t.Fatal(err)
	}
	yb, err := yaml.Marshal(front)
	if err != nil {
		t.Fatal(err)
	}
	var tk, yk map[string]any
	if err := toml.Unmarshal(tb.Bytes(), &tk); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(yb, &yk); err != nil {
		t.Fatal(err)
	}
	keys := func(m map[string]any) []string {
		var out []string
		for k := range m {
			out = append(out, k)
		}
		slices.Sort(out)
		return out
	}
	if !slices.Equal(keys(tk), keys(yk)) {
		t.Fatalf("%T: TOML keys %v, YAML keys %v", front, keys(tk), keys(yk))
	}
	typ := reflect.TypeOf(front).Elem()
	fromTOML, fromYAML := reflect.New(typ).Interface(), reflect.New(typ).Interface()
	if _, err := mdfile.Decode(append(append([]byte("+++\n"), tb.Bytes()...), "+++\n"...), fromTOML); err != nil {
		t.Fatal(err)
	}
	if _, err := mdfile.Decode(append(append([]byte("---\n"), yb...), "---\n"...), fromYAML); err != nil {
		t.Fatalf("%T: %v\n%s", front, err, yb)
	}
	if !reflect.DeepEqual(fromTOML, fromYAML) {
		t.Fatalf("%T from TOML %+v, from YAML %+v", front, fromTOML, fromYAML)
	}
}
