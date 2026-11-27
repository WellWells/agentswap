package swap

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/WellWells/agentswap/internal/fsx"
	"github.com/WellWells/agentswap/internal/store"
)

var (
	ErrSameAccount = errors.New("still signed in to the same account")
	ErrNoLive      = errors.New("no live credentials found; log in first")
	ErrEmptyLogin  = errors.New("login contains no tokens")
	ErrMismatch    = errors.New("live credentials belong to another saved account")
	ErrUnverified  = errors.New("could not verify the account or login generation")
	ErrStaleLogin  = errors.New("the saved account has a newer login")
	ErrExists      = errors.New("account is already saved")
)

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
	LiveErr  error
}

type live struct {
	raw          []byte
	id           Identity
	idErr        error
	pendingOwner bool
}

func (l live) ok() bool { return l.raw != nil && l.idErr == nil }

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Manager) withRegistry(hold bool, fn func(r *store.Registry, l live) error) error {
	return m.withLocks(func(r *store.Registry, release func()) error {
		if err := m.S.Migrate(); err != nil {
			return err
		}
		l, err := m.sync(r)
		if !hold {
			release()
		}
		if err != nil {
			return err
		}
		return fn(r, l)
	})
}

func (m *Manager) withLocks(fn func(r *store.Registry, release func()) error) error {
	timeout := m.LockTimeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	lk, err := fsx.Acquire(m.S.LockPath(), timeout)
	if err != nil {
		return err
	}
	defer lk.Release()
	r, err := m.S.Load()
	if err != nil {
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
	return fn(r, release)
}

func (m *Manager) readLive() (live, error) {
	raw, err := m.P.ReadLive()
	if errors.Is(err, fs.ErrNotExist) {
		return live{}, nil
	}
	if err != nil {
		return live{}, err
	}
	id, idErr := m.P.Identify(raw)
	return live{raw: raw, id: id, idErr: idErr}, nil
}

func (m *Manager) sync(r *store.Registry) (live, error) {
	l, err := m.readLive()
	if err != nil || !l.ok() {
		return l, err
	}
	id, raw := l.id, l.raw
	if err := m.checkOtherAccounts(r, id.Key, raw, false); err != nil {
		l.idErr = err
		return l, nil
	}
	i := r.Index(id.Key)
	if v, ok := m.P.(LoginVerifier); ok && (i < 0 || r.Accounts[i].PendingOwner) {
		if err := v.VerifyOwner(raw); err != nil {
			l.idErr = err
			l.pendingOwner = errors.Is(err, ErrUnverified)
			return l, nil
		}
	}
	if i < 0 {
		r.Active = ""
		return l, nil
	}
	old, err := m.S.ReadSnapshot(id.Key)
	if err != nil {
		l.idErr = fmt.Errorf("%w: %w", ErrUnverified, err)
		return l, nil
	}
	if !bytes.Equal(old, raw) {
		if v, ok := m.P.(LoginVerifier); ok {
			stored, err := m.P.Identify(old)
			if err != nil || stored.Key != id.Key {
				l.idErr = ErrUnverified
				return l, nil
			}
			if err := v.CheckReplacement(old, raw, false); err != nil {
				l.idErr = err
				return l, nil
			}
		}
		if err := m.S.WriteSnapshot(id.Key, raw); err != nil {
			return l, err
		}
	}
	r.Accounts[i].Email, r.Accounts[i].Plan, r.Accounts[i].Mode = id.Email, id.Plan, id.Mode
	r.Accounts[i].PendingOwner = false
	r.Active = id.Key
	return l, nil
}

func (m *Manager) checkOtherAccounts(r *store.Registry, key string, raw []byte, allowMissing bool) error {
	sl, ok := m.P.(SameLogin)
	if !ok {
		return nil
	}
	for _, a := range r.Accounts {
		if a.Key == key {
			continue
		}
		snap, err := m.S.ReadSnapshot(a.Key)
		if allowMissing && errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("%w: %w", ErrUnverified, err)
		}
		if sl.SameLogin(raw, snap) {
			return fmt.Errorf("%w: %s", ErrMismatch, a.Email)
		}
	}
	return nil
}

func (m *Manager) validateSnapshot(r *store.Registry, key string, raw []byte) error {
	id, err := m.P.Identify(raw)
	if err != nil {
		return err
	}
	if id.Key != key {
		return ErrMismatch
	}
	return m.checkOtherAccounts(r, key, raw, false)
}

func (m *Manager) accountSnapshot(raw []byte) ([]byte, error) {
	if f, ok := m.P.(SnapshotFilter); ok {
		return f.AccountSnapshot(raw)
	}
	return raw, nil
}

func (m *Manager) savedSnapshot(r *store.Registry, key string) ([]byte, error) {
	raw, err := m.S.ReadSnapshot(key)
	if err != nil {
		return nil, err
	}
	if err := m.validateSnapshot(r, key, raw); err != nil {
		return nil, err
	}
	if i := r.Index(key); i >= 0 && r.Accounts[i].PendingOwner {
		v, ok := m.P.(LoginVerifier)
		if !ok {
			return nil, ErrUnverified
		}
		if err := v.VerifyOwner(raw); err != nil {
			return nil, err
		}
		r.Accounts[i].PendingOwner = false
	}
	return m.accountSnapshot(raw)
}

func (m *Manager) save(r *store.Registry, l live, alias string) (store.Account, error) {
	a := store.Account{Key: l.id.Key, Alias: alias, Email: l.id.Email, Plan: l.id.Plan, Mode: l.id.Mode, AddedAt: m.now().UTC(), PendingOwner: l.pendingOwner}
	raw, err := m.accountSnapshot(l.raw)
	if err != nil {
		return a, err
	}
	if err := m.S.WriteSnapshot(a.Key, raw); err != nil {
		return a, err
	}
	r.Upsert(a)
	return r.Accounts[r.Index(a.Key)], nil
}

func (m *Manager) Add(alias string) (store.Account, error) {
	a, _, err := m.AddWithResult(alias)
	return a, err
}

func (m *Manager) AddWithResult(alias string) (store.Account, bool, error) {
	var out store.Account
	var added bool
	err := m.withRegistry(true, func(r *store.Registry, l live) error {
		if l.raw == nil {
			return ErrNoLive
		}
		if l.idErr != nil && !(l.pendingOwner && r.Index(l.id.Key) < 0) {
			return l.idErr
		}
		if alias != "" {
			if err := validAlias(r, alias, l.id.Key); err != nil {
				return err
			}
		}
		added = r.Index(l.id.Key) < 0
		a, err := m.save(r, l, alias)
		if err != nil {
			return err
		}
		if !a.PendingOwner {
			r.Active = a.Key
		}
		out = a
		return m.S.Save(r)
	})
	return out, added && err == nil, err
}

func (m *Manager) LoginLive(alias string, signIn func() error) (store.Account, error) {
	var out store.Account
	err := m.withRegistry(true, func(r *store.Registry, before live) error {
		if before.idErr != nil {
			return before.idErr
		}
		if before.ok() {
			if r.Index(before.id.Key) < 0 {
				if _, err := m.save(r, before, ""); err != nil {
					return err
				}
			}
			r.Active = before.id.Key
			if err := m.S.Save(r); err != nil {
				return err
			}
			p, ok := m.P.(interface{ ClearLive([]byte) error })
			if !ok {
				return fmt.Errorf("%s cannot clear its current login", m.P.Name())
			}
			if err := p.ClearLive(before.raw); err != nil {
				return err
			}
		}
		signInErr := signIn()
		after, err := m.sync(r)
		if err != nil {
			return errors.Join(signInErr, err)
		}
		if after.raw == nil {
			if before.ok() {
				if err := m.P.WriteLive(before.raw); err != nil {
					return errors.Join(signInErr, err)
				}
			}
			if signInErr != nil {
				return signInErr
			}
			return ErrNoLive
		}
		if signInErr != nil {
			return signInErr
		}
		if after.idErr != nil {
			return after.idErr
		}
		if before.ok() && after.id.Key == before.id.Key {
			return ErrSameAccount
		}
		if alias != "" {
			if err := validAlias(r, alias, after.id.Key); err != nil {
				return err
			}
		}
		out, err = m.save(r, after, alias)
		if err != nil {
			return err
		}
		if before.ok() {
			r.Previous = before.id.Key
		}
		r.Active = after.id.Key
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
		if err := m.checkIncoming(r, l, id.Key, raw); err != nil {
			return err
		}
		src := live{raw: raw, id: id}
		if v, ok := m.P.(LoginVerifier); ok {
			if i := r.Index(id.Key); i >= 0 {
				old, err := m.S.ReadSnapshot(id.Key)
				if err != nil {
					return err
				}
				if prev, err := m.P.Identify(old); err != nil || prev.Key != id.Key {
					return ErrUnverified
				}
				if r.Accounts[i].PendingOwner || !bytes.Equal(old, raw) {
					if err := v.CheckReplacement(old, raw, true); err != nil {
						return err
					}
				}
			} else if l.ok() && l.id.Key == id.Key {
				if !bytes.Equal(l.raw, raw) {
					if err := v.CheckReplacement(l.raw, raw, true); err != nil {
						return err
					}
				}
			} else if err := v.VerifyOwner(raw); err != nil {
				if !errors.Is(err, ErrUnverified) {
					return err
				}
				src.pendingOwner = true
			}
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
		a, err := m.save(r, src, alias)
		if err != nil {
			return err
		}
		out = a
		return m.S.Save(r)
	})
	return out, err
}

func (m *Manager) checkIncoming(r *store.Registry, l live, key string, raw []byte) error {
	if err := m.checkOtherAccounts(r, key, raw, false); err != nil {
		return err
	}
	if sl, ok := m.P.(SameLogin); ok && l.raw != nil && sl.SameLogin(l.raw, raw) {
		if l.pendingOwner && key == l.id.Key && bytes.Equal(l.raw, raw) {
			return nil
		}
		if !l.ok() {
			return l.idErr
		}
		if key != l.id.Key {
			return ErrMismatch
		}
	}
	return nil
}

func (m *Manager) Adopt(raw []byte, alias string) (store.Account, error) {
	var out store.Account
	err := m.withRegistry(true, func(r *store.Registry, l live) error {
		id, err := m.P.Identify(raw)
		if err != nil {
			return err
		}
		if r.Index(id.Key) >= 0 {
			return ErrExists
		}
		if err := m.checkIncoming(r, l, id.Key, raw); err != nil {
			return err
		}
		if alias != "" {
			if err := validAlias(r, alias, id.Key); err != nil {
				return err
			}
		}
		src := live{raw: raw, id: id}
		if l.ok() && l.id.Key == id.Key {
			src = l
		} else if v, ok := m.P.(LoginVerifier); ok {
			if err := v.VerifyOwner(raw); err != nil {
				if !errors.Is(err, ErrUnverified) {
					return err
				}
				src.pendingOwner = true
			}
		}
		a, err := m.save(r, src, alias)
		if err != nil {
			return err
		}
		if l.ok() && l.id.Key == id.Key {
			r.Active = id.Key
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
		_, verifiesOwner := m.P.(LoginVerifier)
		if l.idErr != nil && ((verifiesOwner && !errors.Is(l.idErr, ErrEmptyLogin)) || errors.Is(l.idErr, ErrMismatch) || errors.Is(l.idErr, ErrUnverified)) {
			return l.idErr
		}
		if l.ok() && l.id.Key == out.Key {
			return m.S.Save(r)
		}
		if l.ok() && r.Index(l.id.Key) < 0 {
			if _, err := m.save(r, l, ""); err != nil {
				return err
			}
		}
		snap, err := m.savedSnapshot(r, out.Key)
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
		st = Status{Registry: r, Live: l.id, LiveOK: l.ok(), LiveErr: l.idErr}
		return m.S.Save(r)
	})
	return st, err
}

type Saved struct {
	Account store.Account
	Raw     []byte
}

func (m *Manager) Snapshots() ([]Saved, error) {
	var out []Saved
	err := m.withRegistry(false, func(r *store.Registry, l live) error {
		for _, a := range r.Accounts {
			raw, err := m.savedSnapshot(r, a.Key)
			if err != nil {
				return fmt.Errorf("saved credentials for %s: %w", a.Email, err)
			}
			out = append(out, Saved{Account: a, Raw: raw})
		}
		return m.S.Save(r)
	})
	return out, err
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
		m.removeUsageCache(out.Key)
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
	if _, err := strconv.Atoi(alias); err == nil || alias == "-" || strings.ContainsAny(alias, " \t") || strings.IndexFunc(alias, unicode.IsControl) >= 0 {
		return aliasError(fmt.Sprintf("invalid alias %q", alias))
	}
	for _, a := range r.Accounts {
		if a.Key != key && (strings.EqualFold(a.Alias, alias) || strings.EqualFold(a.Email, alias)) {
			return aliasError(fmt.Sprintf("alias %q is already used by %s", alias, a.Email))
		}
	}
	return nil
}
