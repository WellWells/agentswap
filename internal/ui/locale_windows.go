//go:build windows

package ui

import (
	"syscall"
	"unsafe"
)

var (
	procGetUserPreferredUILanguages = kernel32.NewProc("GetUserPreferredUILanguages")
	procGetUserDefaultLocaleName    = kernel32.NewProc("GetUserDefaultLocaleName")
)

const muiLanguageName = 0x8

func SystemLanguages() []string {
	var langs []string
	var num, size uint32
	if r, _, _ := procGetUserPreferredUILanguages.Call(muiLanguageName, uintptr(unsafe.Pointer(&num)), 0, uintptr(unsafe.Pointer(&size))); r != 0 && size > 0 {
		buf := make([]uint16, size)
		if r, _, _ := procGetUserPreferredUILanguages.Call(muiLanguageName, uintptr(unsafe.Pointer(&num)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))); r != 0 {
			langs = splitMultiSZ(buf)
		}
	}
	buf := make([]uint16, 85)
	if r, _, _ := procGetUserDefaultLocaleName.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))); r != 0 {
		langs = append(langs, syscall.UTF16ToString(buf))
	}
	return langs
}
