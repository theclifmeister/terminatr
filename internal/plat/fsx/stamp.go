package fsx

import (
	"encoding/binary"
	"hash/fnv"
	"os"
)

// StatStamp hashes the stats of paths (each one's path, mtime and size;
// a missing one counts too), so a reader that keeps what it read from
// them can tell they changed with a few stats instead of the reads.
// Files written by rename change their folder's mtime as well. What
// changes within the mtime's granularity to the same size goes unseen:
// keep a backstop age besides.
func StatStamp(paths ...string) uint64 {
	h := fnv.New64a()
	var buf [8]byte
	for _, path := range paths {
		h.Write([]byte(path))
		fi, err := os.Stat(path)
		if err != nil {
			h.Write([]byte{0})
			continue
		}
		binary.LittleEndian.PutUint64(buf[:], uint64(fi.ModTime().UnixNano()))
		h.Write(buf[:])
		binary.LittleEndian.PutUint64(buf[:], uint64(fi.Size()))
		h.Write(buf[:])
	}
	return h.Sum64()
}
