package claude

import (
	"os"
	"syscall"
	"time"
	"unsafe"
)

var setFileInformation = syscall.NewLazyDLL("kernel32.dll").NewProc("SetFileInformationByHandle")

func openLockDir(path string) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	const deleteAccess = 0x10000
	const readAttributes = 0x80
	const listDirectory = 1
	h, err := syscall.CreateFile(name, syscall.FILE_WRITE_ATTRIBUTES|readAttributes|deleteAccess|listDirectory,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_BACKUP_SEMANTICS|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}

func touchLockDir(f *os.File) error {
	t := syscall.NsecToFiletime(time.Now().UnixNano())
	return syscall.SetFileTime(syscall.Handle(f.Fd()), nil, nil, &t)
}

func removeLockDir(f *os.File) error {
	const fileDispositionInfo = 4
	remove := byte(1)
	if ok, _, err := setFileInformation.Call(f.Fd(), fileDispositionInfo, uintptr(unsafe.Pointer(&remove)), unsafe.Sizeof(remove)); ok == 0 {
		return err
	}
	return nil
}
