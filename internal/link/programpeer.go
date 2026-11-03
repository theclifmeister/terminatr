package link

// Program messages to paired servers (the programs design §3.5, §9.2): the
// program kinds go in their own outbox stream per paired server,
// outbox/<alias>/program/, numbered in their own sequence, so pausing
// them never holds a coordinator's message back, nor skips a number the
// receiver then takes for a duplicate. While this server's programs are
// off (SetPrograms, from the server's switch) the stream is left as it
// is: nothing in it is sent or expired, and once programs are back on
// what waited longer than Expiry shows as undelivered and the rest goes.
// A receiving server with programs off refuses them programs-off.

import (
	"path/filepath"
	"sync/atomic"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// programStream names the program kinds' stream: its outbox folder under
// the alias's, and the suffix of its sequence keys (an alias has no
// "/").
const programStream = "program"

// programsOn is this server's [features] programs, as the server last
// set it; off until then.
var programsOn atomic.Bool

// SetPrograms tells the outbox and Receive whether programs are on: the
// server calls it at start and on every switch.
func SetPrograms(on bool) { programsOn.Store(on) }

// ProgramsOn says what SetPrograms last said.
func ProgramsOn() bool { return programsOn.Load() }

// ProgramTransport is a Transport that also says whether a paired
// server takes program messages: its link is up and its hello lists the
// capability "program". A Transport without it takes none.
type ProgramTransport interface {
	ProgramReady(alias string) bool
}

// streamKey is the sequence key of kind's stream to or from alias.
func streamKey(alias, kind string) string {
	if proto.IsProgramKind(kind) {
		return alias + "/" + programStream
	}
	return alias
}

// streamDir is the outbox folder of kind's stream to alias.
func streamDir(root, alias, kind string) string {
	if proto.IsProgramKind(kind) {
		return filepath.Join(root, alias, programStream)
	}
	return filepath.Join(root, alias)
}

// programReady says whether the program stream to alias goes now.
func programReady(t Transport, alias string) bool {
	pt, ok := t.(ProgramTransport)
	return ok && ProgramsOn() && pt.ProgramReady(alias)
}
