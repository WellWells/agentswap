package swap

import (
	"bytes"
	"context"
	"errors"
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
	if !w.ResetsAt.IsZero() && !w.ResetsAt.After(now) {
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

type refreshingUsageReader interface {
	UsageWithRefresh(ctx context.Context, snapshot []byte, active bool, save func([]byte) error) (Usage, []byte, error)
}

type UsageResult struct {
	Usage     Usage
	Err       error
	Cached    bool
	Stale     bool
	RetryAt   time.Time
	CheckedAt time.Time
}

type usageJob struct {
	account store.Account
	snap    []byte
	active  bool
	saved   bool
	cache   usageCache
}

func (m *Manager) Usage(ctx context.Context, all bool) (Status, map[string]UsageResult, error) {
	return m.usage(ctx, all, "")
}

func (m *Manager) UsageOf(ctx context.Context, q string) (Status, map[string]UsageResult, error) {
	return m.usage(ctx, true, q)
}

func usageAccounts(r *store.Registry, q string) ([]store.Account, error) {
	if q == "" {
		return r.Accounts, nil
	}
	i, err := r.Find(q)
	if err != nil {
		return nil, err
	}
	return r.Accounts[i : i+1], nil
}

func (m *Manager) usage(ctx context.Context, all bool, q string) (Status, map[string]UsageResult, error) {
	var st Status
	results := map[string]UsageResult{}
	err := m.withRegistry(false, func(r *store.Registry, l live) error {
		st = Status{Registry: r, Live: l.id, LiveOK: l.ok(), LiveErr: l.idErr}
		accounts, err := usageAccounts(r, q)
		if err != nil {
			return err
		}
		reader, ok := m.P.(UsageReader)
		if !ok {
			return m.S.Save(r)
		}
		var jobs []usageJob
		_, checksLogin := m.P.(SameLogin)
		for _, a := range accounts {
			active := l.ok() && a.Key == l.id.Key
			if !all && !active {
				continue
			}
			cache, err := m.readUsageCache(a)
			if err != nil {
				return err
			}
			if checksLogin && l.raw != nil && !l.ok() {
				res := cache.result(m.now())
				res.Err, res.Usage.Live = l.idErr, false
				results[a.Key] = res
				continue
			}
			if l.raw != nil && !l.ok() && a.Key == r.Active {
				active = true
			}
			snap, err := m.savedSnapshot(r, a.Key)
			if err != nil {
				res := cache.result(m.now())
				res.Err, res.Usage.Live = err, false
				results[a.Key] = res
				continue
			}
			now := m.now()
			if cache.RetryAt.After(now) || cache.reusable(now) || !active && cache.deadLogin(snap) {
				results[a.Key] = cache.result(now)
				continue
			}
			jobs = append(jobs, usageJob{account: a, snap: snap, active: active, saved: true, cache: cache})
		}
		if q == "" && l.ok() && r.Index(l.id.Key) < 0 {
			jobs = append(jobs, usageJob{account: store.Account{Key: l.id.Key}, snap: l.raw, active: true})
		}
		if err := m.S.Save(r); err != nil {
			return err
		}
		queue := make(chan usageJob, len(jobs))
		for _, j := range jobs {
			queue <- j
		}
		close(queue)
		var wg sync.WaitGroup
		var mu sync.Mutex
		var firstErr error
		var snapshotMu sync.Mutex
		for range min(4, len(jobs)) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := range queue {
					res, err := m.fetchAccountUsage(ctx, reader, r, j, &snapshotMu)
					mu.Lock()
					results[j.account.Key] = res
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		return firstErr
	})
	return st, results, err
}

func (m *Manager) fetchAccountUsage(ctx context.Context, reader UsageReader, r *store.Registry, j usageJob, snapshotMu *sync.Mutex) (UsageResult, error) {
	if err := ctx.Err(); err != nil {
		res := j.cache.result(m.now())
		res.Err, res.Usage.Live = err, false
		return res, nil
	}
	var saveErr error
	rotated := false
	save := func(b []byte) error {
		if j.active || !j.saved || b == nil {
			return nil
		}
		snapshotMu.Lock()
		defer snapshotMu.Unlock()
		if err := m.validateSnapshot(r, j.account.Key, b); err != nil {
			saveErr = err
			return err
		}
		original, err := m.savedSnapshot(r, j.account.Key)
		if err == nil && !bytes.Equal(original, j.snap) {
			err = errors.New("saved login changed during refresh")
		}
		if err == nil {
			err = m.S.WriteSnapshot(j.account.Key, b)
		}
		if err == nil {
			rotated = true
		}
		saveErr = err
		return err
	}
	var u Usage
	var b []byte
	var err error
	if checkpoint, ok := reader.(refreshingUsageReader); ok {
		u, _, err = checkpoint.UsageWithRefresh(ctx, j.snap, j.active, save)
	} else {
		u, b, err = reader.Usage(ctx, j.snap, j.active)
		if b != nil {
			save(b)
		}
	}
	if saveErr != nil {
		return UsageResult{Err: saveErr}, saveErr
	}
	res := j.cache.record(u, err, m.now())
	if !j.saved {
		return res, nil
	}
	j.cache.DeadLogin = ""
	if !j.active && !rotated && j.cache.Failure == "login_expired" {
		j.cache.DeadLogin = snapshotPrint(j.snap)
	}
	return res, m.saveUsageCache(j.account, j.cache)
}
