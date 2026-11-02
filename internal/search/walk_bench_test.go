package search

import (
	"os"
	"testing"

	"github.com/theclifmeister/terminatr/internal/project"
)

func BenchmarkWalk(b *testing.B) {
	dir := os.Getenv("TM_SEARCH_PROJECT")
	if dir == "" {
		b.Skip("TM_SEARCH_PROJECT not set")
	}
	for b.Loop() {
		if _, err := walk(dir, project.KindProject); err != nil {
			b.Fatal(err)
		}
	}
}
