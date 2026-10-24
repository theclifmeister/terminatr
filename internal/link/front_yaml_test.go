package link

import (
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/mdfile/mdfiletest"
)

// TestMessageFrontYAML: a message's front matter reads the same from
// YAML as from TOML.
func TestMessageFrontYAML(t *testing.T) {
	at := time.Date(2026, 10, 24, 18, 12, 56, 0, time.UTC)
	mdfiletest.SameYAML(t, &Message{ID: "M1", Out: true, Created: at, Re: "M0", Bytes: 3, State: "sent", By: "coordinator",
		Attachments: []string{"a.txt"}, Refused: "r", Due: at, File: "f", FileSize: 9, Transfer: "X-1", Sender: "a"})
}
