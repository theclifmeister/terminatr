//go:build windows

package fsx

import (
	"encoding/binary"
	"os"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// LinkDir makes link point at the directory target, an absolute path. A
// symlink needs Developer Mode or admin rights, so link is a directory
// junction, which any user may make; a target a junction can't name (a
// network share) falls back to a symlink.
func LinkDir(target, link string) error {
	jerr := junction(target, link)
	if jerr == nil {
		return nil
	}
	if err := os.Symlink(target, link); err != nil {
		return &os.LinkError{Op: "junction", Old: target, New: link, Err: jerr}
	}
	return nil
}

// junction makes link an empty directory, then a mount point reparse
// point (REPARSE_DATA_BUFFER's MountPointReparseBuffer) for target.
func junction(target, link string) (err error) {
	if err := os.Mkdir(link, 0o700); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.Remove(link)
		}
	}()
	p, err := windows.UTF16PtrFromString(link)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	sub := utf16.Encode([]rune(`\??\` + target))
	name := utf16.Encode([]rune(target))
	path := append(append(append(sub, 0), name...), 0)
	data := 8 + 2*len(path) // the four offsets and lengths, then the names
	if data > 0xffff {
		return windows.ERROR_FILENAME_EXCED_RANGE
	}
	buf := make([]byte, 8+data)
	le := binary.LittleEndian
	le.PutUint32(buf[0:], windows.IO_REPARSE_TAG_MOUNT_POINT)
	le.PutUint16(buf[4:], uint16(data))
	le.PutUint16(buf[8:], 0)                     // SubstituteNameOffset
	le.PutUint16(buf[10:], uint16(2*len(sub)))   // SubstituteNameLength
	le.PutUint16(buf[12:], uint16(2*len(sub)+2)) // PrintNameOffset
	le.PutUint16(buf[14:], uint16(2*len(name)))  // PrintNameLength
	for i, c := range path {
		le.PutUint16(buf[16+2*i:], c)
	}
	var n uint32
	return windows.DeviceIoControl(h, windows.FSCTL_SET_REPARSE_POINT, &buf[0], uint32(len(buf)), nil, 0, &n, nil)
}
