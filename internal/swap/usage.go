package swap

import (
	"context"
	"sync"
	"time"

	"github.com/WellWells/agentswap/internal/store"
)

type Window struct {
	UsedPercent int
	Minutes     int
	ResetsAt    time.Time
	Label       string
}

func (w Window) Percent(now time.Time) int {
	if !w.ResetsAt.IsZero() && now.After(w.ResetsAt) {
		return 0
	}
	return w.UsedPercent
}

type Usage struct {
	Plan    string
	Windows []Window
	At      time.Time
	Live    bool
}

type UsageReader interface {
	Usage(ctx context.Context, snapshot []byte, active bool) (Usage, []byte, error)
}

type UsageResult struct {
	Usage Usage
	Err   error
}

type usageJob struct {
	key    string
	snap   []byte
	active bool
}

func (m *Manager) Usage(ctx context.Context, all bool) (Status, map[string]UsageResult, error) {
	return m.usage(ctx, all, "")
}

func (m *Manager) UsageOf(ctx context.Context, q string) (Status, map[string]UsageResult, error) {
	return m.usage(ctx, true, q)
}

func (m *Manager) usage(ctx context.Context, all bool, q string) (Status, map[string]UsageResult, error) {
	var st Status
	results := map[string]UsageResult{}
	err := m.withRegistry(false, func(r *store.Registry, l live) error {
		st = Status{Registry: r, Live: l.id, LiveOK: l.ok()}
		accounts := r.Accounts
		if q != "" {
			i, err := r.Find(q)
			if err != nil {
				return err
			}
			accounts = r.Accounts[i : i+1]
		}
		reader, ok := m.P.(UsageReader)
		if !ok {
			return m.S.Save(r)
		}
		var jobs []usageJob
		if !all {
			if l.ok() {
				jobs = append(jobs, usageJob{l.id.Key, l.raw, true})
			}
		} else {
			for _, a := range accounts {
				snap, err := m.S.ReadSnapshot(a.Key)
				if err != nil {
					results[a.Key] = UsageResult{Err: err}
					continue
				}
				active := a.Key == l.id.Key && l.ok()
				if l.raw != nil && !l.ok() && a.Key == r.Active {
					active = true
				}
				jobs = append(jobs, usageJob{a.Key, snap, active})
			}
			if q == "" && l.ok() && r.Index(l.id.Key) < 0 {
				jobs = append(jobs, usageJob{l.id.Key, l.raw, true})
			}
		}
		refreshed := make([][]byte, len(jobs))
		out := make([]UsageResult, len(jobs))
		var wg sync.WaitGroup
		for i, j := range jobs {
			wg.Add(1)
			go func(i int, j usageJob) {
				defer wg.Done()
				u, b, err := reader.Usage(ctx, j.snap, j.active)
				out[i] = UsageResult{Usage: u, Err: err}
				if !j.active {
					refreshed[i] = b
				}
			}(i, j)
		}
		wg.Wait()
		for i, j := range jobs {
			results[j.key] = out[i]
			if refreshed[i] != nil && r.Index(j.key) >= 0 {
				if err := m.S.WriteSnapshot(j.key, refreshed[i]); err != nil {
					return err
				}
			}
		}
		return m.S.Save(r)
	})
	return st, results, err
}
