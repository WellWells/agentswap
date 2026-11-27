package procs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/WellWells/agentswap/internal/execx"
	"github.com/WellWells/agentswap/internal/fsx"
)

type Proc struct {
	PID     int
	PPID    int
	Name    string
	Path    string
	Other   bool
	started int64
}

type System struct {
	Run       execx.Runner
	Wait      time.Duration
	ClaudeDir string
}

func baseName(name string) string {
	name = strings.ToLower(filepath.Base(strings.ReplaceAll(name, `\`, "/")))
	return strings.TrimSuffix(name, ".exe")
}

func Filter(all []Proc, names, skipPaths []string, self int) []Proc {
	return filter(all, names, skipPaths, self, nil)
}

func filter(all []Proc, names, skipPaths []string, self int, sessions map[int]bool) []Proc {
	byPID := map[int]Proc{}
	for _, p := range all {
		byPID[p.PID] = p
	}
	mine := map[int]bool{}
	for pid := self; pid > 0 && !mine[pid]; {
		mine[pid] = true
		p, ok := byPID[pid]
		if !ok {
			break
		}
		pid = p.PPID
	}
	var out []Proc
	for _, p := range all {
		if mine[p.PID] || p.Other || !wanted(p, names, skipPaths) {
			continue
		}
		if len(sessions) > 0 && baseName(p.Name) == "claude" && managedChild(p, byPID, sessions) {
			continue
		}
		out = append(out, p)
	}
	return out
}

func managedChild(p Proc, processes map[int]Proc, sessions map[int]bool) bool {
	for steps := 0; p.PPID > 0 && steps < len(processes); steps++ {
		parent, ok := processes[p.PPID]
		if !ok || parent.Other || (p.started != 0 && parent.started > p.started) {
			break
		}
		if sessions[parent.PID] {
			return true
		}
		p = parent
	}
	return false
}

func RegisterSession(claudeDir string) (func(), error) {
	return registerSession(claudeDir, os.Getpid())
}

func registerSession(claudeDir string, pid int) (func(), error) {
	path := filepath.Join(claudeDir, "running", strconv.Itoa(pid))
	lease, err := fsx.Acquire(path, 2*time.Second)
	if err != nil {
		return nil, err
	}
	return func() {
		lease.Release()
		os.Remove(path)
	}, nil
}

func (s System) sessionPIDs() (map[int]bool, error) {
	if s.ClaudeDir == "" {
		return nil, nil
	}
	dir := filepath.Join(s.ClaudeDir, "running")
	files, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	pids := make(map[int]bool)
	for _, file := range files {
		pid, err := strconv.Atoi(file.Name())
		if err != nil || pid <= 0 || !file.Type().IsRegular() {
			continue
		}
		locked, err := fsx.Locked(filepath.Join(dir, file.Name()))
		if err != nil {
			return nil, err
		}
		if locked {
			pids[pid] = true
		}
	}
	return pids, nil
}

func wanted(p Proc, names, skipPaths []string) bool {
	path := strings.ReplaceAll(p.Path, `\`, "/")
	for _, s := range skipPaths {
		if path != "" && strings.Contains(strings.ToLower(path), strings.ToLower(s)) {
			return false
		}
	}
	base := baseName(p.Name)
	for _, n := range names {
		if base == n {
			return true
		}
	}
	return false
}

func (s System) runner() execx.Runner {
	if s.Run == nil {
		return execx.Run
	}
	return s.Run
}

func (s System) Find(names, skipPaths []string) ([]Proc, error) {
	all, err := list(s.runner())
	if err != nil {
		return nil, err
	}
	var sessions map[int]bool
	for _, name := range names {
		if name == "claude" {
			sessions, err = s.sessionPIDs()
			if err != nil {
				return nil, err
			}
			break
		}
	}
	return filter(all, names, skipPaths, os.Getpid(), sessions), nil
}

func (s System) remaining(ps []Proc) []Proc {
	all, err := list(s.runner())
	if err != nil {
		return ps
	}
	alive := map[int]string{}
	for _, p := range all {
		alive[p.PID] = baseName(p.Name)
	}
	var out []Proc
	for _, p := range ps {
		if n, ok := alive[p.PID]; ok && n == baseName(p.Name) {
			out = append(out, p)
		}
	}
	return out
}

func (s System) waitGone(ps []Proc, d time.Duration) []Proc {
	deadline := time.Now().Add(d)
	for {
		ps = s.remaining(ps)
		if len(ps) == 0 || time.Now().After(deadline) {
			return ps
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (s System) Stop(ps []Proc) error {
	wait := s.Wait
	if wait == 0 {
		wait = 5 * time.Second
	}
	for _, p := range ps {
		terminate(s.runner(), p.PID)
	}
	left := s.waitGone(ps, wait)
	for _, p := range left {
		if proc, err := os.FindProcess(p.PID); err == nil {
			proc.Kill()
		}
	}
	left = s.waitGone(left, 2*time.Second)
	if len(left) > 0 {
		pids := make([]string, len(left))
		for i, p := range left {
			pids[i] = strconv.Itoa(p.PID)
		}
		return fmt.Errorf("could not end PID %s", strings.Join(pids, ", "))
	}
	return nil
}

func parsePS(out string, uid int) []Proc {
	var ps []Proc
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		owner, err3 := strconv.Atoi(f[2])
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		comm := strings.Join(f[3:], " ")
		p := Proc{PID: pid, PPID: ppid, Name: filepath.Base(comm), Other: owner != uid}
		if strings.HasPrefix(comm, "/") {
			p.Path = comm
		}
		ps = append(ps, p)
	}
	return ps
}
