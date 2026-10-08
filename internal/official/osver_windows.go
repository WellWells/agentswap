//go:build windows

package official

import (
	"fmt"
	"syscall"
	"unsafe"
)

type osVersionInfo struct {
	Size         uint32
	MajorVersion uint32
	MinorVersion uint32
	BuildNumber  uint32
	PlatformID   uint32
	CSDVersion   [128]uint16
}

func windowsVersion() string {
	proc := syscall.NewLazyDLL("ntdll.dll").NewProc("RtlGetVersion")
	if proc.Find() != nil {
		return ""
	}
	var v osVersionInfo
	v.Size = uint32(unsafe.Sizeof(v))
	if r, _, _ := proc.Call(uintptr(unsafe.Pointer(&v))); r != 0 {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
}
