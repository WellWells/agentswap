package store

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/WellWells/agentswap/internal/vault"
)

func sample() *Registry {
	return &Registry{
		Active:   "k2",
		Previous: "k1",
		Accounts: []Account{
			{Key: "k1", Alias: "work", Email: "alice@corp.com"},
			{Key: "k2", Email: "bob@home.net"},
			{Key: "k3", Alias: "spare", Email: "bobby@home.net"},
		},
	}
}

func TestLoadMissingRegistryIsEmpty(t *testing.T) {
	r, err := Store{Dir: t.TempDir()}.Load()
	if err != nil || len(r.Accounts) != 0 {
		t.Fatalf("got %+v, %v", r, err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if err := s.Save(sample()); err != nil {
		t.Fatal(err)
	}
	r, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != Version || r.Active != "k2" || len(r.Accounts) != 3 || r.Accounts[0].Alias != "work" {
		t.Fatalf("got %+v", r)
	}
}

func TestFind(t *testing.T) {
	r := sample()
	cases := map[string]string{
		"1":            "k1",
		"3":            "k3",
		"-":            "k1",
		"WORK":         "k1",
		"bob@home.net": "k2",
		"corp":         "k1",
		"spare":        "k3",
	}
	for q, want := range cases {
		i, err := r.Find(q)
		if err != nil {
			t.Errorf("%q: %v", q, err)
			continue
		}
		if r.Accounts[i].Key != want {
			t.Errorf("%q: got %s want %s", q, r.Accounts[i].Key, want)
		}
	}
}

func TestFindErrors(t *testing.T) {
	r := sample()
	if _, err := r.Find("home"); !errors.Is(err, ErrAmbiguous) {
		t.Errorf("home: %v", err)
	}
	r.Accounts[0].Alias = "bob@home.net"
	if _, err := r.Find("BOB@home.net"); !errors.Is(err, ErrAmbiguous) {
		t.Errorf("alias/email collision: %v", err)
	}
	r.Accounts[0].Alias = "work"
	for _, q := range []string{"", "0", "4", "nobody"} {
		if _, err := r.Find(q); !errors.Is(err, ErrNotFound) {
			t.Errorf("%q: %v", q, err)
		}
	}
	r.Previous = ""
	if _, err := r.Find("-"); !errors.Is(err, ErrNotFound) {
		t.Errorf("-: %v", err)
	}
}

func TestUpsertKeepsAliasAndAddedAt(t *testing.T) {
	r := sample()
	r.Upsert(Account{Key: "k1", Email: "alice@new.com"})
	if len(r.Accounts) != 3 || r.Accounts[0].Email != "alice@new.com" || r.Accounts[0].Alias != "work" {
		t.Fatalf("got %+v", r.Accounts[0])
	}
	r.Upsert(Account{Key: "k4", Email: "d@x"})
	if len(r.Accounts) != 4 {
		t.Fatalf("not appended")
	}
}

func TestSnapshotRoundTripAndDelete(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	key := "chatgpt:user-1:acct-1"
	if err := s.WriteSnapshot(key, []byte("data")); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadSnapshot(key)
	if err != nil || string(got) != "data" {
		t.Fatalf("got %q, %v", got, err)
	}
	if err := s.DeleteSnapshot(key); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadSnapshot(key); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("still exists: %v", err)
	}
}

func TestBackupKeepsNewest(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	for i := 0; i < 7; i++ {
		if err := s.Backup("auth", []byte{byte('a' + i)}, 5); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(s.Dir, "backups"))
	if len(entries) != 5 {
		t.Fatalf("kept %d backups", len(entries))
	}
	newest, _ := os.ReadFile(filepath.Join(s.Dir, "backups", entries[len(entries)-1].Name()))
	if string(newest) != "g" {
		t.Fatalf("newest backup %q", newest)
	}
}

func sealedStore(t *testing.T) Store {
	return Store{Dir: t.TempDir(), Vault: vault.WithKey(bytes.Repeat([]byte{3}, 32))}
}

func TestSnapshotsAndBackupsAreSealed(t *testing.T) {
	s := sealedStore(t)
	if err := s.WriteSnapshot("k", []byte(`{"rt":"secret"}`)); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(s.snapshotPath("k"))
	if !vault.Sealed(raw) || bytes.Contains(raw, []byte("secret")) {
		t.Fatalf("snapshot not sealed: %q", raw)
	}
	got, err := s.ReadSnapshot("k")
	if err != nil || string(got) != `{"rt":"secret"}` {
		t.Fatalf("read %q %v", got, err)
	}
	if err := s.Backup("codex-live", []byte("secret"), 5); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(s.Dir, "backups"))
	b, _ := os.ReadFile(filepath.Join(s.Dir, "backups", entries[0].Name()))
	if !vault.Sealed(b) {
		t.Fatal("backup not sealed")
	}
}

func TestMigrateSealsLegacyFiles(t *testing.T) {
	s := sealedStore(t)
	plain := Store{Dir: s.Dir}
	plain.WriteSnapshot("k", []byte(`{"legacy":true}`))
	plain.Backup("codex-live", []byte(`{"old":1}`), 5)
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(s.snapshotPath("k"))
	if !vault.Sealed(raw) {
		t.Fatal("snapshot still plaintext")
	}
	if got, _ := s.ReadSnapshot("k"); string(got) != `{"legacy":true}` {
		t.Fatalf("content changed: %q", got)
	}
	entries, _ := os.ReadDir(filepath.Join(s.Dir, "backups"))
	b, _ := os.ReadFile(filepath.Join(s.Dir, "backups", entries[0].Name()))
	if !vault.Sealed(b) {
		t.Fatal("backup still plaintext")
	}
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateWithoutVaultIsNoop(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	s.WriteSnapshot("k", []byte("x"))
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(s.snapshotPath("k")); string(raw) != "x" {
		t.Fatal("changed without vault")
	}
}
