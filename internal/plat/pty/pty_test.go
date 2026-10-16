package pty

import (
	"strings"
	"testing"
	"time"
)

// waitFor reads c until its output contains s; it returns the output.
func waitFor(t *testing.T, c Console, s string) string {
	t.Helper()
	var out []byte
	buf := make([]byte, 1024)
	c.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer c.SetReadDeadline(time.Time{})
	for !strings.Contains(string(out), s) {
		n, err := c.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			t.Fatalf("waiting for %q: %v (output %q)", s, err, out)
		}
	}
	return string(out)
}
