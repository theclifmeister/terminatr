package mod

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNoSource refuses to update a mod that records no source: installed
// by hand, or linked by tm mod dev.
var ErrNoSource = errors.New("it records no source (installed by hand, or by tm mod dev)")

// FetchError is why Fetch could not stage a mod from From: Err, cleaned
// and on one line.
type FetchError struct {
	From string
	Err  error
}

func (e *FetchError) Error() string {
	return e.From + ": " + strings.ReplaceAll(Clean(e.Err.Error(), true), "\n", "; ")
}

// FetchUpdate stages the update of installed mod m from the source it
// records (Next: a newer tag, or the branch or folder as it is now);
// nil, with no error, when what it fetched is what is installed. The
// caller shows Diff's changes when they ask, then calls Replace, and
// closes the staged copy either way.
func FetchUpdate(m Mod) (*Staged, error) {
	if m.Builtin {
		return nil, fmt.Errorf("%s is built in: it updates with tm", m.ID)
	}
	src, ok := ReadSource(m.Dir)
	if !ok {
		return nil, ErrNoSource
	}
	next, err := Next(src)
	if err != nil {
		return nil, err
	}
	st, err := Fetch(next)
	if err != nil {
		if st != nil {
			st.Close()
		}
		return nil, &FetchError{From: next.String(), Err: err}
	}
	if st.Source.Hash == src.Hash {
		RecordSource(m, src, st)
		st.Close()
		return nil, nil
	}
	return st, nil
}

// RecordSource notes in installed mod m's .tm-source.json that its
// files, unchanged, are now those of st's newer ref or commit, so a
// check for updates doesn't keep finding it behind. It does nothing when
// the ref and commit are the same; a failure to write is ignored, the
// mod being up to date either way.
func RecordSource(m Mod, was Source, st *Staged) {
	if st.Source.Ref == was.Ref && st.Source.Commit == was.Commit && st.Source.Tree == was.Tree {
		return
	}
	next := st.Source
	next.Installed = was.Installed
	_ = writeSource(m.Dir, next)
}
