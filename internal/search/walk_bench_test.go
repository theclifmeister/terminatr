package search

import (
	"os"
	"testing"
)

func BenchmarkWalk(b *testing.B) {
	dir := os.Getenv("TM_SEARCH_PROJECT")
	if dir == "" {
		b.Skip("TM_SEARCH_PROJECT not set")
	}
	for b.Loop() {
		if _, err := walk(dir); err != nil {
			b.Fatal(err)
		}
	}
}
