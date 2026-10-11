package proc

// RSS is the resident set of each process in pids that is alive, in
// bytes (the working set on Windows). A process that is gone, or closed
// to this one, is left out. The mod supervisor's memory ceiling reads it
// (mods design §7: goja has no heap cap, so the RSS is the only limit).
func RSS(pids []int) (map[int]uint64, error) {
	out := map[int]uint64{}
	if len(pids) == 0 {
		return out, nil
	}
	return out, rss(pids, out)
}
