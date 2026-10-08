//go:build windows

package antigravity

import (
	"errors"
	"fmt"
	"io/fs"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

var (
	advapi32       = syscall.NewLazyDLL("advapi32.dll")
	kernel32       = syscall.NewLazyDLL("kernel32.dll")
	procCredRead   = advapi32.NewProc("CredReadW")
	procCredWrite  = advapi32.NewProc("CredWriteW")
	procCredFree   = advapi32.NewProc("CredFree")
	procSnapshot   = kernel32.NewProc("CreateToolhelp32Snapshot")
	procFirstEntry = kernel32.NewProc("Process32FirstW")
	procNextEntry  = kernel32.NewProc("Process32NextW")
)

const (
	credTypeGeneric     = 1
	persistLocalMachine = 2
	maxBlob             = 2560
	errNotFound         = syscall.Errno(1168)
	snapProcess         = 0x2
)

type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        syscall.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

type winCred struct{ target string }

func (w winCred) Read() ([]byte, error) {
	name, err := syscall.UTF16PtrFromString(w.target)
	if err != nil {
		return nil, err
	}
	var c *credential
	r, _, err := procCredRead.Call(uintptr(unsafe.Pointer(name)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&c)))
	if r == 0 {
		if errors.Is(err, errNotFound) {
			return nil, fs.ErrNotExist
		}
		return nil, fmt.Errorf("CredReadW: %w", err)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(c)))
	if c.CredentialBlobSize == 0 {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), unsafe.Slice(c.CredentialBlob, c.CredentialBlobSize)...), nil
}

func (w winCred) Write(b []byte) error {
	if len(b) == 0 || len(b) > maxBlob {
		return fmt.Errorf("credentials are %d bytes; Windows Credential Manager holds 1 to %d", len(b), maxBlob)
	}
	name, err := syscall.UTF16PtrFromString(w.target)
	if err != nil {
		return err
	}
	u, err := syscall.UTF16PtrFromString(user)
	if err != nil {
		return err
	}
	c := credential{Type: credTypeGeneric, TargetName: name, CredentialBlobSize: uint32(len(b)), CredentialBlob: &b[0], Persist: persistLocalMachine, UserName: u}
	r, _, err := procCredWrite.Call(uintptr(unsafe.Pointer(&c)), 0)
	runtime.KeepAlive(b)
	if r == 0 {
		return fmt.Errorf("CredWriteW: %w", err)
	}
	return nil
}

type processEntry struct {
	Size            uint32
	Usage           uint32
	ProcessID       uint32
	DefaultHeapID   uintptr
	ModuleID        uint32
	Threads         uint32
	ParentProcessID uint32
	PriClassBase    int32
	Flags           uint32
	ExeFile         [syscall.MAX_PATH]uint16
}

func processRunning(exe string) (bool, error) {
	h, _, err := procSnapshot.Call(snapProcess, 0)
	if syscall.Handle(h) == syscall.InvalidHandle {
		return false, fmt.Errorf("CreateToolhelp32Snapshot: %w", err)
	}
	defer syscall.CloseHandle(syscall.Handle(h))
	var e processEntry
	e.Size = uint32(unsafe.Sizeof(e))
	r, _, _ := procFirstEntry.Call(h, uintptr(unsafe.Pointer(&e)))
	for r != 0 {
		if strings.EqualFold(syscall.UTF16ToString(e.ExeFile[:]), exe) {
			return true, nil
		}
		r, _, _ = procNextEntry.Call(h, uintptr(unsafe.Pointer(&e)))
	}
	return false, nil
}
