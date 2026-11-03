package appreport

import (
	"os"
	"testing"

	"github.com/theclifmeister/terminatr/internal/home/hometest"
)

// TestMain runs the tests under hometest.Run: CI's umask, so a store folder
// that is not 0700 fails here as on Linux CI, and a throwaway home.
func TestMain(m *testing.M) { os.Exit(hometest.Run(m.Run)) }
