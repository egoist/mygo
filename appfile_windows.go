//go:build windows

package mygo

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var procSetFileInformationByHandle = syscall.NewLazyDLL("kernel32.dll").NewProc("SetFileInformationByHandle")

const (
	fileRenameInfoEx          = 22 // FILE_INFO_BY_HANDLE_CLASS
	fileRenameReplaceIfExists = 0x1
	fileRenamePOSIXSemantics  = 0x2
	accessDelete              = 0x00010000
	accessSynchronize         = 0x00100000
	fileFlagOpenReparsePoint  = 0x00200000
	errorInvalidFunction      = syscall.Errno(1)
	errorNotSupported         = syscall.Errno(50)
	errorInvalidParameter     = syscall.Errno(87)
)

// fileRenameInfo is FILE_RENAME_INFO, its name running on past the end.
type fileRenameInfo struct {
	Flags          uint32
	RootDirectory  syscall.Handle
	FileNameLength uint32
	FileName       [1]uint16
}

// replaceFile renames from over to with POSIX semantics, as Rust's
// fs::rename does: a classic MoveFileEx, as os.Rename, is refused while
// any handle of to is open, even one sharing its deletion (openShared),
// so the app reading its own file kept it from being replaced, as long as
// the reads kept coming. With POSIX semantics it is replaced while handles
// that share deletion are open, and those read what they opened. A file
// system without them (FAT) gets os.Rename.
func replaceFile(from, to string) error {
	err := renamePOSIX(from, to)
	var errno syscall.Errno
	if errors.As(err, &errno) && (errno == errorInvalidParameter || errno == errorNotSupported || errno == errorInvalidFunction) {
		return os.Rename(from, to)
	}
	return err
}

func renamePOSIX(from, to string) error {
	fail := func(err error) error { return &os.LinkError{Op: "rename", Old: from, New: to, Err: err} }
	abs, err := filepath.Abs(to)
	if err != nil {
		return fail(err)
	}
	name, err := syscall.UTF16FromString(abs)
	if err != nil {
		return fail(err)
	}
	src, err := syscall.UTF16PtrFromString(from)
	if err != nil {
		return fail(err)
	}
	h, err := syscall.CreateFile(src, accessDelete|accessSynchronize, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, fileFlagOpenReparsePoint|syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return fail(err)
	}
	defer syscall.CloseHandle(h)
	// The name, with its NUL, after the fields: in words, for the handle's
	// alignment.
	size := unsafe.Offsetof(fileRenameInfo{}.FileName) + uintptr(len(name))*2
	buf := make([]uint64, (size+7)/8)
	info := (*fileRenameInfo)(unsafe.Pointer(&buf[0]))
	info.Flags = fileRenameReplaceIfExists | fileRenamePOSIXSemantics
	info.FileNameLength = uint32(len(name)-1) * 2
	copy(unsafe.Slice(&info.FileName[0], len(name)), name)
	if r, _, err := procSetFileInformationByHandle.Call(uintptr(h), fileRenameInfoEx, uintptr(unsafe.Pointer(info)), size); r == 0 {
		return fail(err)
	}
	return nil
}
