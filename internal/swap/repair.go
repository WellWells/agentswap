package swap

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"

	"github.com/WellWells/agentswap/internal/store"
)

var ErrRepairLive = errors.New("switch away from this account before repairing its saved login")

func (m *Manager) Repair(key string, raw []byte) error {
	return m.withLocks(func(r *store.Registry, _ func()) error {
		if r.Index(key) < 0 {
			return store.ErrNotFound
		}
		id, err := m.P.Identify(raw)
		if err != nil {
			return err
		}
		if id.Key != key {
			return ErrMismatch
		}
		if err := m.checkOtherAccounts(r, key, raw, true); err != nil {
			return err
		}
		l, err := m.readLive()
		if err != nil {
			return err
		}
		if l.idErr != nil {
			return l.idErr
		}
		if l.ok() && l.id.Key == key {
			return ErrRepairLive
		}
		if sl, ok := m.P.(SameLogin); ok && l.ok() && sl.SameLogin(l.raw, raw) {
			return ErrMismatch
		}
		raw, err = m.accountSnapshot(raw)
		if err != nil {
			return err
		}
		old, err := m.S.ReadSnapshot(key)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if v, ok := m.P.(interface {
			SameLogin
			VerifyOwner([]byte) error
			CheckRepair(saved, incoming []byte) error
		}); ok {
			if l.ok() {
				local, err := m.S.ReadSnapshot(l.id.Key)
				i := r.Index(l.id.Key)
				if err != nil || i < 0 || r.Accounts[i].PendingOwner || !v.SameLogin(local, l.raw) {
					if err := v.VerifyOwner(l.raw); err != nil {
						return err
					}
				}
			}
			known := old
			if id, err := m.P.Identify(old); err != nil || id.Key != key {
				known = nil
			}
			if err := v.CheckRepair(known, raw); err != nil {
				return err
			}
		}
		changed := !bytes.Equal(old, raw)
		if changed && old != nil {
			hash := sha256.Sum256([]byte(key))
			if err := m.S.Backup(fmt.Sprintf("%s-repair-%x", m.P.Name(), hash[:12]), old, 5); err != nil {
				return err
			}
		}
		if err := m.removeUsageCache(key); err != nil {
			return err
		}
		if changed {
			if err := m.S.WriteSnapshot(key, raw); err != nil {
				return err
			}
		}
		i := r.Index(key)
		if r.Accounts[i].PendingOwner {
			r.Accounts[i].PendingOwner = false
			return m.S.Save(r)
		}
		return nil
	})
}
