package mod

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNoSource refuses to update a mod that records no source: installed
// by hand, or linked by tm mod dev.
var ErrNoSource = errors.New("it records no source (installed by hand, or by tm mod dev)")

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
		return nil, fmt.Errorf("%s: %s", next, strings.ReplaceAll(Clean(err.Error(), true), "\n", "; "))
	}
	if st.Source.Hash == src.Hash {
		st.Close()
		return nil, nil
	}
	return st, nil
}
