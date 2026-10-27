package server

import "testing"

func TestMenuLabel(t *testing.T) {
	for in, want := range map[string]string{
		"Blue":                 "Blue",
		"[ ] Apple":            "Apple",
		"[✔] Apple":            "Apple",
		"Yes  Proceed with it": "Yes",
		"Type something.":      "Type something.",
	} {
		if got := menuLabel(in); got != want {
			t.Errorf("menuLabel(%q) = %q, want %q", in, got, want)
		}
	}
}
