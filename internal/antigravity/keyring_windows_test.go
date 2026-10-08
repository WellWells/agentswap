//go:build windows

package antigravity

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestProcessRunning(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := processRunning(filepath.Base(exe)); !ok || err != nil {
		t.Fatalf("own process: %v %v", ok, err)
	}
	if ok, err := processRunning("agentswap-no-such-process.exe"); ok || err != nil {
		t.Fatalf("missing process: %v %v", ok, err)
	}
}

func TestWinCredRoundTrip(t *testing.T) {
	target := fmt.Sprintf("agentswap-test:%d", time.Now().UnixNano())
	name, _ := syscall.UTF16PtrFromString(target)
	t.Cleanup(func() { advapi32.NewProc("CredDeleteW").Call(uintptr(unsafe.Pointer(name)), credTypeGeneric, 0) })
	w := winCred{target: target}
	if _, err := w.Read(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
	b := blob("1", "a@x", "at", "rt", future)
	if err := w.Write(b); err != nil {
		t.Fatal(err)
	}
	got, err := w.Read()
	if err != nil || !bytes.Equal(got, b) {
		t.Fatalf("%s %v", got, err)
	}
	if err := w.Write(bytes.Repeat([]byte("x"), maxBlob+1)); err == nil {
		t.Fatal("oversized value accepted")
	}
}
