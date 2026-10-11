package proc

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var getProcessMemoryInfo = windows.NewLazySystemDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")

// processMemoryCounters is PROCESS_MEMORY_COUNTERS.
type processMemoryCounters struct {
	cb                         uint32
	pageFaultCount             uint32
	peakWorkingSetSize         uintptr
	workingSetSize             uintptr
	quotaPeakPagedPoolUsage    uintptr
	quotaPagedPoolUsage        uintptr
	quotaPeakNonPagedPoolUsage uintptr
	quotaNonPagedPoolUsage     uintptr
	pagefileUsage              uintptr
	peakPagefileUsage          uintptr
}

// rss reads each process's working set (K32GetProcessMemoryInfo).
func rss(pids []int, out map[int]uint64) error {
	if err := getProcessMemoryInfo.Find(); err != nil {
		return err
	}
	for _, pid := range pids {
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
		if err != nil {
			continue
		}
		var c processMemoryCounters
		c.cb = uint32(unsafe.Sizeof(c))
		r, _, _ := getProcessMemoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&c)), uintptr(c.cb))
		var code uint32
		alive := windows.GetExitCodeProcess(h, &code) == nil && code == 259 // STILL_ACTIVE
		windows.CloseHandle(h)
		if r != 0 && alive {
			out[pid] = uint64(c.workingSetSize)
		}
	}
	return nil
}
