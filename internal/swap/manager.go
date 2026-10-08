package swap

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/WellWells/agentswap/internal/fsx"
	"github.com/WellWells/agentswap/internal/store"
)

var ErrNoLive = errors.New("no live credentials found; log in first")

type Manager struct {
	P           Provider
	S           store.Store
	Now         func() time.Time
	LockTimeout time.Duration
}

type Status struct {
	Registry *store.Registry
	Live     Identity
	LiveOK   bool
}

type live struct {
	raw   []byte
	id    Identity
	idErr error
}

func (l live) ok() bool { return l.raw != nil && l.idErr == nil }

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Manager) withRegistry(hold bool, fn func(r *store.Registry, l live) error) error {
	timeout := m.LockTimeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	lk, err := fsx.Acquire(m.S.LockPath(), timeout, 2*time.Minute)
	if err != nil {
		return err
	}
	defer lk.Release()
	r, err := m.S.Load()
	if err != nil {
		return err
	}
	if err := m.S.Migrate(); err != nil {
		return err
	}
	unlock := func() {}
	if ll, ok := m.P.(LiveLocker); ok {
		if unlock, err = ll.LockLive(); err != nil {
			return err
		}
	}
	released := false
	release := func() {
		if !released {
			released = true
			unlock()
		}
	}
	defer release()
	l, err := m.sync(r)
	if !hold {
		release()
	}
	if err != nil {
		return err
	}
	return fn(r, l)
}

func (m *Manager) sync(r *store.Registry) (live, error) {
	raw, err := m.P.ReadLive()
	if errors.Is(err, fs.ErrNotExist) {
		return live{}, nil
	}
	if err != nil {
		return live{}, err
	}
	id, idErr := m.P.Identify(raw)
	l := live{raw: raw, id: id, idErr: idErr}
	if !l.ok() {
		return l, nil
	}
	i := r.Index(id.Key)
	if i < 0 {
		r.Active = ""
		return l, nil
	}
	if old, err := m.S.ReadSnapshot(id.Key); err != nil || !bytes.Equal(old, raw) {
		if err := m.S.WriteSnapshot(id.Key, raw); err != nil {
			return l, err
		}
	}
	r.Accounts[i].Email, r.Accounts[i].Plan, r.Accounts[i].Mode = id.Email, id.Plan, id.Mode
	r.Active = id.Key
	return l, nil
}

func (m *Manager) save(r *store.Registry, l live, alias string) (store.Account, error) {
	a := store.Account{Key: l.id.Key, Alias: alias, Email: l.id.Email, Plan: l.id.Plan, Mode: l.id.Mode, AddedAt: m.now().UTC()}
	if err := m.S.WriteSnapshot(a.Key, l.raw); err != nil {
		return a, err
	}
	r.Upsert(a)
	return r.Accounts[r.Index(a.Key)], nil
}

func (m *Manager) Add(alias string) (store.Account, error) {
	var out store.Account
	err := m.withRegistry(true, func(r *store.Registry, l live) error {
		if l.raw == nil {
			return ErrNoLive
		}
		if l.idErr != nil {
			return l.idErr
		}
		if alias != "" {
			if err := validAlias(r, alias, l.id.Key); err != nil {
				return err
			}
		}
		a, err := m.save(r, l, alias)
		if err != nil {
			return err
		}
		r.Active = a.Key
		out = a
		return m.S.Save(r)
	})
	return out, err
}

func (m *Manager) Import(raw []byte, alias string) (store.Account, error) {
	var out store.Account
	err := m.withRegistry(true, func(r *store.Registry, l live) error {
		id, err := m.P.Identify(raw)
		if err != nil {
			return err
		}
		if alias != "" {
			if err := validAlias(r, alias, id.Key); err != nil {
				return err
			}
		}
		if l.ok() && l.id.Key == id.Key && !bytes.Equal(l.raw, raw) {
			if err := m.S.Backup(m.P.Name()+"-live", l.raw, 5); err != nil {
				return err
			}
			if err := m.P.WriteLive(raw); err != nil {
				return err
			}
		}
		a, err := m.save(r, live{raw: raw, id: id}, alias)
		if err != nil {
			return err
		}
		out = a
		return m.S.Save(r)
	})
	return out, err
}

func (m *Manager) Switch(q string) (store.Account, bool, error) {
	var out store.Account
	changed := false
	err := m.withRegistry(true, func(r *store.Registry, l live) error {
		i, err := r.Find(q)
		if err != nil {
			return err
		}
		out = r.Accounts[i]
		if l.ok() && l.id.Key == out.Key {
			return m.S.Save(r)
		}
		if l.ok() && r.Index(l.id.Key) < 0 {
			if _, err := m.save(r, l, ""); err != nil {
				return err
			}
		}
		snap, err := m.S.ReadSnapshot(out.Key)
		if err != nil {
			return fmt.Errorf("saved credentials for %s: %w", out.Email, err)
		}
		if l.raw != nil {
			if err := m.S.Backup(m.P.Name()+"-live", l.raw, 5); err != nil {
				return err
			}
		}
		if err := m.P.WriteLive(snap); err != nil {
			return err
		}
		if l.ok() {
			r.Previous = l.id.Key
		} else if r.Active != "" {
			r.Previous = r.Active
		}
		r.Active = out.Key
		changed = true
		return m.S.Save(r)
	})
	return out, changed, err
}

func (m *Manager) Status() (Status, error) {
	var st Status
	err := m.withRegistry(false, func(r *store.Registry, l live) error {
		st = Status{Registry: r, Live: l.id, LiveOK: l.ok()}
		return m.S.Save(r)
	})
	return st, err
}

func (m *Manager) Remove(q string) (store.Account, error) {
	var out store.Account
	err := m.withRegistry(false, func(r *store.Registry, l live) error {
		i, err := r.Find(q)
		if err != nil {
			return err
		}
		out = r.Accounts[i]
		r.Remove(out.Key)
		if err := m.S.DeleteSnapshot(out.Key); err != nil {
			return err
		}
		return m.S.Save(r)
	})
	return out, err
}

func (m *Manager) SetAlias(q, alias string) error {
	return m.withRegistry(false, func(r *store.Registry, l live) error {
		i, err := r.Find(q)
		if err != nil {
			return err
		}
		if alias != "" {
			if err := validAlias(r, alias, r.Accounts[i].Key); err != nil {
				return err
			}
		}
		r.Accounts[i].Alias = alias
		return m.S.Save(r)
	})
}

var ErrAlias = errors.New("invalid alias")

type aliasError string

func (a aliasError) Error() string { return string(a) }
func (a aliasError) Unwrap() error { return ErrAlias }

func validAlias(r *store.Registry, alias, key string) error {
	if _, err := strconv.Atoi(alias); err == nil || alias == "-" || strings.ContainsAny(alias, " \t") {
		return aliasError(fmt.Sprintf("invalid alias %q", alias))
	}
	for _, a := range r.Accounts {
		if a.Key != key && strings.EqualFold(a.Alias, alias) {
			return aliasError(fmt.Sprintf("alias %q is already used by %s", alias, a.Email))
		}
	}
	return nil
}
