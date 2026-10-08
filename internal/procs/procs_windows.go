//go:build windows

package procs

import (
	"fmt"
	"strconv"
	"syscall"
	"unsafe"

	"github.com/WellWells/agentswap/internal/execx"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procSnapshot     = kernel32.NewProc("CreateToolhelp32Snapshot")
	procFirst        = kernel32.NewProc("Process32FirstW")
	procNext         = kernel32.NewProc("Process32NextW")
	procQueryImage   = kernel32.NewProc("QueryFullProcessImageNameW")
	snapProcess      = uintptr(0x2)
	queryLimitedInfo = uint32(0x1000)
)

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

func imagePath(pid uint32) string {
	h, err := syscall.OpenProcess(queryLimitedInfo, false, pid)
	if err != nil {
		return ""
	}
	defer syscall.CloseHandle(h)
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	if r, _, _ := procQueryImage.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n))); r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

func list(execx.Runner) ([]Proc, error) {
	h, _, err := procSnapshot.Call(snapProcess, 0)
	if syscall.Handle(h) == syscall.InvalidHandle {
		return nil, fmt.Errorf("CreateToolhelp32Snapshot: %w", err)
	}
	defer syscall.CloseHandle(syscall.Handle(h))
	var e processEntry
	e.Size = uint32(unsafe.Sizeof(e))
	var out []Proc
	r, _, _ := procFirst.Call(h, uintptr(unsafe.Pointer(&e)))
	for r != 0 {
		out = append(out, Proc{PID: int(e.ProcessID), PPID: int(e.ParentProcessID), Name: syscall.UTF16ToString(e.ExeFile[:]), Path: imagePath(e.ProcessID)})
		r, _, _ = procNext.Call(h, uintptr(unsafe.Pointer(&e)))
	}
	return out, nil
}

func terminate(run execx.Runner, pid int) {
	run(nil, "taskkill", "/PID", strconv.Itoa(pid))
}
