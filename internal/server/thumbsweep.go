package server

import (
	"os"
	"path/filepath"

	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/thumb"
	"github.com/theclifmeister/terminatr/internal/vaultlink"
)

// sweepThumbs removes the library thumbnails of projects that no longer
// exist (deleted or renamed while no server ran): a thumbnail shows what
// its file showed. Nothing is removed if the projects can't be listed.
func sweepThumbs(logf func(string, ...any)) {
	root, err := home.ProjectsDir()
	if err != nil {
		return
	}
	if _, err := os.ReadDir(root); err != nil && !os.IsNotExist(err) {
		return
	}
	n, err := thumb.Sweep(func(slug string) bool {
		if slug == vaultlink.ProgramsDir {
			return true // the programs' own libraries' (project.Folder.LinkSlug)
		}
		_, err := os.Stat(filepath.Join(root, slug, "PROJECT.md"))
		return err == nil
	})
	if n > 0 {
		logf("library thumbnails of %d projects that are gone removed", n)
	}
	if err != nil {
		logf("library thumbnails: %v", err)
	}
}
