//go:build !windows

package procs

import (
	"fmt"
	"os"
	"syscall"

	"github.com/WellWells/agentswap/internal/execx"
)

func list(run execx.Runner) ([]Proc, error) {
	out, code, err := run(nil, "ps", "-axo", "pid=,ppid=,uid=,comm=")
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("ps: exit %d", code)
	}
	ps := parsePS(string(out), os.Getuid())
	for i := range ps {
		if ps[i].Path == "" {
			if p, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", ps[i].PID)); err == nil {
				ps[i].Path = p
			}
		}
	}
	return ps, nil
}

func terminate(_ execx.Runner, pid int) {
	syscall.Kill(pid, syscall.SIGTERM)
}
