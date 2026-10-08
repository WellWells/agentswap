package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/WellWells/agentswap/internal/fsx"
)

const Version = 1

var (
	ErrNotFound  = errors.New("no matching account")
	ErrAmbiguous = errors.New("query matches more than one account")
)

type Account struct {
	Key     string    `json:"key"`
	Alias   string    `json:"alias,omitempty"`
	Email   string    `json:"email"`
	Plan    string    `json:"plan,omitempty"`
	Mode    string    `json:"mode,omitempty"`
	AddedAt time.Time `json:"added_at"`
}

type Registry struct {
	Version  int       `json:"version"`
	Active   string    `json:"active,omitempty"`
	Previous string    `json:"previous,omitempty"`
	Accounts []Account `json:"accounts"`
}

func (r *Registry) Index(key string) int {
	for i, a := range r.Accounts {
		if a.Key == key {
			return i
		}
	}
	return -1
}

func (r *Registry) Upsert(a Account) {
	i := r.Index(a.Key)
	if i < 0 {
		r.Accounts = append(r.Accounts, a)
		return
	}
	old := r.Accounts[i]
	if a.Alias == "" {
		a.Alias = old.Alias
	}
	if !old.AddedAt.IsZero() {
		a.AddedAt = old.AddedAt
	}
	r.Accounts[i] = a
}

func (r *Registry) Remove(key string) {
	if i := r.Index(key); i >= 0 {
		r.Accounts = append(r.Accounts[:i], r.Accounts[i+1:]...)
	}
	if r.Active == key {
		r.Active = ""
	}
	if r.Previous == key {
		r.Previous = ""
	}
}

func (r *Registry) Find(q string) (int, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return -1, ErrNotFound
	}
	if q == "-" {
		if i := r.Index(r.Previous); r.Previous != "" && i >= 0 {
			return i, nil
		}
		return -1, fmt.Errorf("%w: no previous account", ErrNotFound)
	}
	if n, err := strconv.Atoi(q); err == nil {
		if n >= 1 && n <= len(r.Accounts) {
			return n - 1, nil
		}
		return -1, fmt.Errorf("%w: %s", ErrNotFound, q)
	}
	for i, a := range r.Accounts {
		if strings.EqualFold(a.Alias, q) || strings.EqualFold(a.Email, q) {
			return i, nil
		}
	}
	lq := strings.ToLower(q)
	found := -1
	for i, a := range r.Accounts {
		if strings.Contains(strings.ToLower(a.Alias), lq) || strings.Contains(strings.ToLower(a.Email), lq) {
			if found >= 0 {
				return -1, fmt.Errorf("%w: %s", ErrAmbiguous, q)
			}
			found = i
		}
	}
	if found < 0 {
		return -1, fmt.Errorf("%w: %s", ErrNotFound, q)
	}
	return found, nil
}

type Store struct{ Dir string }

func (s Store) registryPath() string { return filepath.Join(s.Dir, "registry.json") }

func (s Store) LockPath() string { return filepath.Join(s.Dir, ".lock") }

func (s Store) Load() (*Registry, error) {
	b, err := os.ReadFile(s.registryPath())
	if errors.Is(err, os.ErrNotExist) {
		return &Registry{Version: Version}, nil
	}
	if err != nil {
		return nil, err
	}
	var r Registry
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", s.registryPath(), err)
	}
	if r.Version > Version {
		return nil, fmt.Errorf("%s: registry version %d is newer than this agentswap", s.registryPath(), r.Version)
	}
	return &r, nil
}

func (s Store) Save(r *Registry) error {
	r.Version = Version
	if r.Accounts == nil {
		r.Accounts = []Account{}
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteAtomic(s.registryPath(), append(b, '\n'), 0o600)
}

func (s Store) snapshotPath(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(s.Dir, "accounts", hex.EncodeToString(sum[:12])+".json")
}

func (s Store) ReadSnapshot(key string) ([]byte, error) { return os.ReadFile(s.snapshotPath(key)) }

func (s Store) WriteSnapshot(key string, data []byte) error {
	return fsx.WriteAtomic(s.snapshotPath(key), data, 0o600)
}

func (s Store) DeleteSnapshot(key string) error {
	err := os.Remove(s.snapshotPath(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s Store) Backup(prefix string, data []byte, keep int) error {
	dir := filepath.Join(s.Dir, "backups")
	ts := time.Now().UTC().Format("20060102T150405.000000000")
	var p string
	for i := 0; ; i++ {
		p = filepath.Join(dir, fmt.Sprintf("%s-%s-%02d.json", prefix, ts, i))
		if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
			break
		}
	}
	if err := fsx.WriteAtomic(p, data, 0o600); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var mine []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix+"-") {
			mine = append(mine, e.Name())
		}
	}
	for len(mine) > keep {
		os.Remove(filepath.Join(dir, mine[0]))
		mine = mine[1:]
	}
	return nil
}
