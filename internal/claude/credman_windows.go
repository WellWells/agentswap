//go:build windows

package claude

import (
	"errors"
	"fmt"
	"io/fs"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	advapi32       = syscall.NewLazyDLL("advapi32.dll")
	procCredRead   = advapi32.NewProc("CredReadW")
	procCredWrite  = advapi32.NewProc("CredWriteW")
	procCredDelete = advapi32.NewProc("CredDeleteW")
	procCredFree   = advapi32.NewProc("CredFree")
)

const (
	credTypeGeneric   = 1
	credPersistDomain = 3
	credMaxBlob       = 2560
	credNotFound      = syscall.Errno(1168)
)

type winCredential struct {
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

type winCredStore struct{}

func systemCredStore() CredStore { return winCredStore{} }

func credTarget(service, name string) (*uint16, error) {
	return syscall.UTF16PtrFromString(service + "/" + name)
}

func (winCredStore) Get(service, name string) ([]byte, error) {
	target, err := credTarget(service, name)
	if err != nil {
		return nil, err
	}
	var c *winCredential
	r, _, err := procCredRead.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&c)))
	if r == 0 {
		if errors.Is(err, credNotFound) {
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

func (winCredStore) Set(service, name string, value []byte) error {
	if len(value) == 0 || len(value) > credMaxBlob {
		return fmt.Errorf("a %d-byte value does not fit in one Windows credential", len(value))
	}
	target, err := credTarget(service, name)
	if err != nil {
		return err
	}
	user, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	c := winCredential{Type: credTypeGeneric, TargetName: target, CredentialBlobSize: uint32(len(value)), CredentialBlob: &value[0], Persist: credPersistDomain, UserName: user}
	r, _, err := procCredWrite.Call(uintptr(unsafe.Pointer(&c)), 0)
	runtime.KeepAlive(value)
	if r == 0 {
		return fmt.Errorf("CredWriteW: %w", err)
	}
	return nil
}

func (winCredStore) Delete(service, name string) error {
	target, err := credTarget(service, name)
	if err != nil {
		return err
	}
	r, _, err := procCredDelete.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0)
	if r == 0 && !errors.Is(err, credNotFound) {
		return fmt.Errorf("CredDeleteW: %w", err)
	}
	return nil
}
