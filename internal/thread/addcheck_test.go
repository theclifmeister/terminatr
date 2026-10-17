package thread

import "testing"

func TestAddCheck(t *testing.T) {
	w := []string{"Memory check: a", "Memory check: b"}
	for _, c := range []struct{ name, in, want string }{
		{"into ## Check", "## Report\nr\n## Next\nn\n## Check\nRun it.\n\n## Remember\n- l\n",
			"## Report\nr\n## Next\nn\n## Check\nRun it.\nMemory check: a\nMemory check: b\n\n## Remember\n- l\n"},
		{"before ## Remember", "## Report\nr\n## Next\nn\n## Remember\n- l",
			"## Report\nr\n## Next\nn\n## Check\nMemory check: a\nMemory check: b\n\n## Remember\n- l\n"},
		{"at the end", "## Report\nr\n## Next\nn\n",
			"## Report\nr\n## Next\nn\n\n## Check\nMemory check: a\nMemory check: b\n"},
		{"already there", "## Report\nr\n## Next\nn\n## Check\nMemory check: a\n",
			"## Report\nr\n## Next\nn\n## Check\nMemory check: a\nMemory check: b\n"},
	} {
		got := AddCheck(c.in, w)
		if got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
		if _, err := Validate(got); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if got := AddCheck("## Report\nr\n## Next\nn", nil); got != "## Report\nr\n## Next\nn\n" {
		t.Errorf("no lines: %q", got)
	}
}
