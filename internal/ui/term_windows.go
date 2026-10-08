//go:build windows

package ui

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	procGetUserDefaultLocaleName   = kernel32.NewProc("GetUserDefaultLocaleName")
	procSetConsoleMode             = kernel32.NewProc("SetConsoleMode")
	procGetConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
)

const enableVirtualTerminalProcessing = 0x4

func SystemLocale() string {
	buf := make([]uint16, 85)
	r, _, _ := procGetUserDefaultLocaleName.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

func systemZone() string { return "" }

func Terminal(f *os.File) (bool, int) {
	h := syscall.Handle(f.Fd())
	var mode uint32
	if err := syscall.GetConsoleMode(h, &mode); err != nil {
		return false, 0
	}
	color := true
	if mode&enableVirtualTerminalProcessing == 0 {
		r, _, _ := procSetConsoleMode.Call(uintptr(h), uintptr(mode|enableVirtualTerminalProcessing))
		color = r != 0
	}
	var info struct {
		size, cursor [2]int16
		attributes   uint16
		window       [4]int16
		maxSize      [2]int16
	}
	if r, _, _ := procGetConsoleScreenBufferInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&info))); r == 0 {
		return color, 0
	}
	return color, int(info.window[2]-info.window[0]) + 1
}
