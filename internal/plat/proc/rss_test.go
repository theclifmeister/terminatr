package proc

import (
	"errors"
	"os"
	"testing"
)

func TestRSSSelf(t *testing.T) {
	gone := 99998
	for Alive(gone) {
		gone--
	}
	got, err := RSS([]int{os.Getpid(), gone})
	if errors.Is(err, errors.ErrUnsupported) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if n := got[os.Getpid()]; n < 1<<20 {
		t.Errorf("own RSS %d bytes, want at least 1 MiB", n)
	}
	if _, ok := got[1<<30]; ok {
		t.Error("a pid nobody has got an RSS")
	}
}
