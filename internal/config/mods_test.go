package config

import "testing"

func TestDropTable(t *testing.T) {
	in := "# top\n[mods.nord]\nenabled = true\n\n[mods.nord.settings]\ndark = true\n[mods.nordic]\nenabled = true\n[[agents.x.models]]\nname = \"m\"\n"
	got, err := DropTable([]byte(in), "mods.nord")
	if err != nil {
		t.Fatal(err)
	}
	want := "# top\n[mods.nordic]\nenabled = true\n[[agents.x.models]]\nname = \"m\"\n"
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if got, err := DropTable([]byte("[ui]\nicons = \"ascii\"\n"), "mods.nord"); err != nil || string(got) != "[ui]\nicons = \"ascii\"\n" {
		t.Errorf("no table: %q %v", got, err)
	}
	if _, err := DropTable([]byte("mods.nord.enabled = true\n"), "mods.nord"); err != ErrForm {
		t.Errorf("dotted keys: %v", err)
	}
}
