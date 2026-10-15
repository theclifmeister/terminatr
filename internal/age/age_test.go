package age

import (
	"testing"
	"time"
)

func TestWords(t *testing.T) {
	for d, want := range map[time.Duration]string{-time.Second: "0s", 40 * time.Second: "40s", 80 * time.Second: "1m20s",
		time.Minute: "1m", 125 * time.Minute: "2h5m", 2 * time.Hour: "2h"} {
		if got := Words(d); got != want {
			t.Errorf("Words(%v) = %q, want %q", d, got, want)
		}
	}
}
