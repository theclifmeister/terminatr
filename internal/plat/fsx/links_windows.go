//go:build windows

package fsx

import (
	"os"

	"golang.org/x/sys/windows"
)

// FileLinks is the open file f's hard link count (NTFS keeps one per
// name), read from its handle.
func FileLinks(f *os.File) (uint64, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); err != nil {
		return 0, err
	}
	return uint64(info.NumberOfLinks), nil
}

// FileMine reports whether the open file f is owned by this user (or by
// the token's default owner, as checkOwnerMode), read from its handle:
// a read handle has READ_CONTROL, which the owner needs.
func FileMine(f *os.File) bool {
	sd, err := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return false
	}
	ok, err := ownedByMe(owner)
	return err == nil && ok
}

// mineAt reports whether path, fi, is owned by this user.
func mineAt(path string, fi os.FileInfo) bool { return checkOwnerMode(path, fi) == nil }

// OpenInNoBlock opens rel under r for reading. Windows has no FIFOs a
// plain open blocks on.
func OpenInNoBlock(r *os.Root, rel string) (*os.File, error) { return r.Open(rel) }
