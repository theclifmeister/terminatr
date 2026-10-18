package server

import (
	"slices"
	"testing"
)

// TestParseLoginPath: the PATH line is the last marked one, whatever the
// shell prints around it.
func TestParseLoginPath(t *testing.T) {
	out := []byte("welcome\n" + loginPathMarker + "/old\nprompt% \n" + loginPathMarker + "/usr/bin:/h/tools/bin\r\nbye\n")
	if got := parseLoginPath(out); !slices.Equal(got, []string{"/usr/bin", "/h/tools/bin"}) {
		t.Errorf("got %q", got)
	}
	if got := parseLoginPath([]byte("no marker\n")); got != nil {
		t.Errorf("no marker: %q", got)
	}
}
