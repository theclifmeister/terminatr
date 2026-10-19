package changes

import (
	"os"
	"testing"

	"github.com/theclifmeister/terminatr/internal/home/hometest"
)

func TestMain(m *testing.M) {
	os.Exit(hometest.Run(m.Run))
}
