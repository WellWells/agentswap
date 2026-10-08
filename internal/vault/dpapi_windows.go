//go:build windows

package vault

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	crypt32       = syscall.NewLazyDLL("crypt32.dll")
	kernel32      = syscall.NewLazyDLL("kernel32.dll")
	procProtect   = crypt32.NewProc("CryptProtectData")
	procUnprotect = crypt32.NewProc("CryptUnprotectData")
	procLocalFree = kernel32.NewProc("LocalFree")
	entropy       = []byte("agentswap-vault")
)

const (
	uiForbidden  = 0x1
	localMachine = 0x4
)

type dataBlob struct {
	size uint32
	data *byte
}

func blobOf(b []byte) *dataBlob {
	if len(b) == 0 {
		return &dataBlob{}
	}
	return &dataBlob{size: uint32(len(b)), data: &b[0]}
}

func dpapiCall(proc *syscall.LazyProc, in []byte, flags uint32) ([]byte, error) {
	var out dataBlob
	r, _, err := proc.Call(uintptr(unsafe.Pointer(blobOf(in))), 0, uintptr(unsafe.Pointer(blobOf(entropy))), 0, 0, uintptr(flags), uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return nil, err
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.data)))
	return append([]byte(nil), unsafe.Slice(out.data, out.size)...), nil
}

type dpapi struct{}

func (dpapi) Seal(plain []byte) ([]byte, error) {
	inner, err := dpapiCall(procProtect, plain, uiForbidden)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKey, err)
	}
	outer, err := dpapiCall(procProtect, inner, uiForbidden|localMachine)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKey, err)
	}
	return frame(schemeDPAPI, outer), nil
}

func (dpapi) Open(b []byte) ([]byte, error) {
	if !Sealed(b) {
		return b, nil
	}
	scheme, body := unframe(b)
	if scheme != schemeDPAPI {
		return nil, ErrCorrupt
	}
	inner, err := dpapiCall(procUnprotect, body, uiForbidden)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	plain, err := dpapiCall(procUnprotect, inner, uiForbidden)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	return plain, nil
}
