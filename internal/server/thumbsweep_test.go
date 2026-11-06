package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/project"
)

// TestSweepThumbs: at start, thumbnails of projects that no longer exist
// go; those of existing projects stay.
func TestSweepThumbs(t *testing.T) {
	t.Setenv(home.Env, t.TempDir())
	if _, err := project.New(project.Options{Slug: "live"}); err != nil {
		t.Fatal(err)
	}
	d, _ := home.ThumbCacheDir()
	for _, s := range []string{"live", "gone"} {
		os.MkdirAll(filepath.Join(d, s, "t-0001"), 0o700)
		os.WriteFile(filepath.Join(d, s, "t-0001", "x.jpg"), []byte("x"), 0o600)
	}
	var logged int
	sweepThumbs(func(string, ...any) { logged++ })
	if _, err := os.Stat(filepath.Join(d, "gone")); err == nil {
		t.Error("gone project's thumbnails stay")
	}
	if _, err := os.Stat(filepath.Join(d, "live", "t-0001", "x.jpg")); err != nil {
		t.Errorf("live project's: %v", err)
	}
	if logged != 1 {
		t.Errorf("%d log lines", logged)
	}
}
