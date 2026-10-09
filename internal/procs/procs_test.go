package procs

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func pids(ps []Proc) []int {
	out := make([]int, len(ps))
	for i, p := range ps {
		out[i] = p.PID
	}
	return out
}

func TestFilterSkipsSelfAncestorsAndPaths(t *testing.T) {
	all := []Proc{
		{PID: 1, PPID: 0, Name: "init"},
		{PID: 10, PPID: 1, Name: "claude.exe", Path: `C:\Users\u\.local\bin\claude.exe`},
		{PID: 11, PPID: 10, Name: "bash"},
		{PID: 12, PPID: 11, Name: "ccswap.exe"},
		{PID: 20, PPID: 1, Name: "CLAUDE.EXE", Path: `C:\Users\u\.local\bin\claude.exe`},
		{PID: 21, PPID: 1, Name: "claude.exe", Path: `C:\Users\u\AppData\Local\AnthropicClaude\app-1.0\claude.exe`},
		{PID: 22, PPID: 1, Name: "Claude", Path: "/Applications/Claude.app/Contents/MacOS/Claude"},
		{PID: 23, PPID: 1, Name: "claude"},
		{PID: 30, PPID: 1, Name: "claudex.exe"},
	}
	got := Filter(all, []string{"claude"}, []string{"AnthropicClaude", "Claude.app/Contents/MacOS/Claude"}, 12)
	if want := []int{20, 23}; len(got) != 2 || got[0].PID != want[0] || got[1].PID != want[1] {
		t.Fatalf("%v", pids(got))
	}
}

func TestFilterSkipsOtherUsers(t *testing.T) {
	all := []Proc{
		{PID: 5, PPID: 1, Name: "agy", Other: true},
		{PID: 6, PPID: 1, Name: "agy"},
		{PID: 7, PPID: 8, Name: "claude", Other: true},
		{PID: 8, PPID: 9, Name: "ccswap"},
	}
	if got := Filter(all, []string{"agy", "claude"}, nil, 8); len(got) != 1 || got[0].PID != 6 {
		t.Fatalf("%v", pids(got))
	}
}

func TestListMarksOtherUsers(t *testing.T) {
	all, err := list(System{}.runner())
	if err != nil {
		t.Fatal(err)
	}
	var self, system *Proc
	for i, p := range all {
		if p.PID == os.Getpid() {
			self = &all[i]
		}
		if (runtime.GOOS == "windows" && strings.EqualFold(p.Name, "lsass.exe")) || (runtime.GOOS != "windows" && p.PID == 1) {
			system = &all[i]
		}
	}
	if self == nil || self.Other {
		t.Fatalf("own process: %+v", self)
	}
	if runtime.GOOS != "windows" && os.Getuid() == 0 {
		return
	}
	if system == nil || !system.Other {
		t.Fatalf("system process: %+v", system)
	}
}

func TestFilterHandlesParentLoops(t *testing.T) {
	all := []Proc{{PID: 5, PPID: 6, Name: "agy"}, {PID: 6, PPID: 5, Name: "x"}, {PID: 7, PPID: 1, Name: "agy"}}
	if got := Filter(all, []string{"agy"}, nil, 6); len(got) != 1 || got[0].PID != 7 {
		t.Fatalf("%v", pids(got))
	}
}

func TestParsePS(t *testing.T) {
	ps := parsePS("  101     1   501 /Applications/Codex App.app/Contents/MacOS/Codex\n  202   101   502 agy\nbad line\n  303 1 x\n", 501)
	if len(ps) != 2 || ps[0].Name != "Codex" || ps[0].Path != "/Applications/Codex App.app/Contents/MacOS/Codex" || ps[0].Other || ps[1].PID != 202 || ps[1].PPID != 101 || ps[1].Name != "agy" || ps[1].Path != "" || !ps[1].Other {
		t.Fatalf("%+v", ps)
	}
}

func TestStopEndsProcess(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	name := "sleep"
	if runtime.GOOS == "windows" {
		cmd = exec.Command("ping", "-n", "30", "127.0.0.1")
		name = "PING.EXE"
	}
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	if err := (System{Wait: time.Second}).Stop([]Proc{{PID: cmd.Process.Pid, Name: name}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		t.Fatal("process still running")
	}
}

func TestFindSeesOwnProcessOnlyAsAncestor(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := baseName(filepath.Base(exe))
	got, err := System{}.Find([]string{name}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range got {
		if p.PID == os.Getpid() {
			t.Fatal("listed itself")
		}
	}
}
