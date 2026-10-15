package relay

import (
	"regexp"

	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// Text the relay sends is not trusted: it may come from a relay that was
// taken over, or from the path to one. What reaches a caller is checked
// here, at the boundary, so nothing downstream shows relay-chosen bytes.

// releasePattern is a tm release as the relay's challenge names it:
// "v0.28.0", optionally with a pre-release suffix.
var releasePattern = regexp.MustCompile(`^v?[0-9]{1,4}\.[0-9]{1,4}\.[0-9]{1,4}(-[0-9A-Za-z.]{1,24})?$`)

// CleanVersion is v when it looks like a release version, else "".
func CleanVersion(v string) string {
	if releasePattern.MatchString(v) {
		return v
	}
	return ""
}

// ReasonUnknown is the close reason shown for any word that isn't one of
// the Err* words.
const ReasonUnknown = "unknown"

var knownReasons = map[string]bool{
	ErrBadHello: true, ErrBadSignature: true, ErrBadRoom: true, ErrTimeout: true,
	ErrReplaced: true, ErrRoomFull: true, ErrPeerGone: true, ErrRateLimited: true,
	ErrPairLimit: true, ErrShutdown: true, ErrProtocol: true,
}

// cleanReason maps a close reason to one of the Err* words; an empty one
// stays empty, anything else is ReasonUnknown.
func cleanReason(s string) string {
	switch {
	case s == "", knownReasons[s]:
		return s
	}
	return ReasonUnknown
}

// Line is s cut to one plain line of at most n runes, for text that came
// from the other end of a link and is shown or logged.
func Line(s string, n int) string { return textsafe.Line(s, n) }
