package fsx

import "golang.org/x/sys/windows"

// Free is how many bytes the volume holding dir has free for this user;
// -1 when that can't be told.
func Free(dir string) int64 {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return -1
	}
	var avail, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &total, &free); err != nil {
		return -1
	}
	return int64(avail)
}
