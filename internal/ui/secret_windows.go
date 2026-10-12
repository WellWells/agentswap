//go:build windows

package ui

import (
	"os"
	"syscall"
)

const (
	enableProcessedInput = 0x1
	enableLineInput      = 0x2
	enableEchoInput      = 0x4
)

func noEcho(f *os.File) (func(), error) {
	h := syscall.Handle(f.Fd())
	var mode uint32
	if err := syscall.GetConsoleMode(h, &mode); err != nil {
		return nil, err
	}
	if r, _, err := procSetConsoleMode.Call(uintptr(h), uintptr(mode&^enableEchoInput|enableLineInput|enableProcessedInput)); r == 0 {
		return nil, err
	}
	return func() { procSetConsoleMode.Call(uintptr(h), uintptr(mode)) }, nil
}
