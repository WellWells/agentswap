package swap

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/WellWells/agentswap/internal/fsx"
	"github.com/WellWells/agentswap/internal/store"
)

type fakeProvider struct {
	live   []byte
	writes int
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) ReadLive() ([]byte, error) {
	if f.live == nil {
		return nil, os.ErrNotExist
	}
	return f.live, nil
}

func (f *fakeProvider) WriteLive(b []byte) error {
	f.live = append([]byte(nil), b...)
	f.writes++
	return nil
}

func (f *fakeProvider) Identify(b []byte) (Identity, error) {
	key, rest, ok := strings.Cut(string(b), "|")
	if !ok {
		return Identity{}, errors.New("bad")
	}
	email, _, _ := strings.Cut(rest, "|")
	return Identity{Key: key, Email: email, Mode: "test"}, nil
}

func newManager(t *testing.T) (*Manager, *fakeProvider) {
	f := &fakeProvider{}
	return &Manager{P: f, S: store.Store{Dir: t.TempDir()}, LockTimeout: 100 * time.Millisecond}, f
}

func addAccount(t *testing.T, m *Manager, f *fakeProvider, live, alias string) {
	t.Helper()
	f.live = []byte(live)
	if _, err := m.Add(alias); err != nil {
		t.Fatal(err)
	}
}

func snapshot(t *testing.T, m *Manager, key string) string {
	t.Helper()
	b, err := m.S.ReadSnapshot(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAddSavesLiveAccount(t *testing.T) {
	m, f := newManager(t)
	f.live = []byte("k1|a@x|v1")
	a, err := m.Add("work")
	if err != nil {
		t.Fatal(err)
	}
	if a.Key != "k1" || a.Alias != "work" || a.Email != "a@x" || a.AddedAt.IsZero() {
		t.Fatalf("got %+v", a)
	}
	r, _ := m.S.Load()
	if r.Active != "k1" || len(r.Accounts) != 1 {
		t.Fatalf("registry %+v", r)
	}
	if snapshot(t, m, "k1") != "k1|a@x|v1" {
		t.Fatal("snapshot not saved")
	}
}

func TestAddWithoutLiveCredentialsFails(t *testing.T) {
	m, _ := newManager(t)
	if _, err := m.Add(""); !errors.Is(err, ErrNoLive) {
		t.Fatalf("got %v", err)
	}
}

func TestSwitchActivatesTargetAndRemembersPrevious(t *testing.T) {
	m, f := newManager(t)
	addAccount(t, m, f, "k1|a@x|v1", "")
	addAccount(t, m, f, "k2|b@x|v1", "")
	a, changed, err := m.Switch("a@x")
	if err != nil || !changed || a.Key != "k1" {
		t.Fatalf("got %+v %v %v", a, changed, err)
	}
	if string(f.live) != "k1|a@x|v1" {
		t.Fatalf("live %q", f.live)
	}
	r, _ := m.S.Load()
	if r.Active != "k1" || r.Previous != "k2" {
		t.Fatalf("registry %+v", r)
	}
	entries, _ := os.ReadDir(filepath.Join(m.S.Dir, "backups"))
	if len(entries) != 1 {
		t.Fatalf("want 1 backup, got %d", len(entries))
	}
}

func TestSwitchSyncsRotatedTokensBeforeLeaving(t *testing.T) {
	m, f := newManager(t)
	addAccount(t, m, f, "k1|a@x|v1", "")
	addAccount(t, m, f, "k2|b@x|v1", "")
	f.live = []byte("k2|b@x|v2-rotated")
	if _, _, err := m.Switch("1"); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(t, m, "k2"); got != "k2|b@x|v2-rotated" {
		t.Fatalf("rotated token lost: %q", got)
	}
}

func TestSwitchDashReturnsToPrevious(t *testing.T) {
	m, f := newManager(t)
	addAccount(t, m, f, "k1|a@x|v1", "")
	addAccount(t, m, f, "k2|b@x|v1", "")
	m.Switch("1")
	a, _, err := m.Switch("-")
	if err != nil || a.Key != "k2" || string(f.live) != "k2|b@x|v1" {
		t.Fatalf("got %+v %v live=%q", a, err, f.live)
	}
}

func TestSwitchToActiveAccountIsNoop(t *testing.T) {
	m, f := newManager(t)
	addAccount(t, m, f, "k1|a@x|v1", "")
	before := f.writes
	_, changed, err := m.Switch("1")
	if err != nil || changed || f.writes != before {
		t.Fatalf("changed=%v err=%v writes=%d", changed, err, f.writes-before)
	}
}

func TestSwitchFailsWhileLocked(t *testing.T) {
	m, f := newManager(t)
	addAccount(t, m, f, "k1|a@x|v1", "")
	l, err := fsx.Acquire(m.S.LockPath(), time.Second, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	if _, _, err := m.Switch("1"); !errors.Is(err, fsx.ErrLocked) {
		t.Fatalf("got %v", err)
	}
}

func TestStatusFollowsManualLogin(t *testing.T) {
	m, f := newManager(t)
	addAccount(t, m, f, "k1|a@x|v1", "")
	addAccount(t, m, f, "k2|b@x|v1", "")
	f.live = []byte("k1|a@x|v9")
	st, err := m.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !st.LiveOK || st.Live.Key != "k1" || st.Registry.Active != "k1" {
		t.Fatalf("got %+v", st)
	}
	if snapshot(t, m, "k1") != "k1|a@x|v9" {
		t.Fatal("live tokens not synced on status")
	}
	f.live = []byte("k3|c@x|v1")
	st, _ = m.Status()
	if st.Registry.Active != "" || st.Live.Key != "k3" {
		t.Fatalf("unsaved live account: %+v", st)
	}
}

func TestRemoveDeletesAccountButNotLive(t *testing.T) {
	m, f := newManager(t)
	addAccount(t, m, f, "k1|a@x|v1", "")
	a, err := m.Remove("1")
	if err != nil || a.Key != "k1" {
		t.Fatalf("got %+v %v", a, err)
	}
	r, _ := m.S.Load()
	if len(r.Accounts) != 0 || r.Active != "" {
		t.Fatalf("registry %+v", r)
	}
	if _, err := m.S.ReadSnapshot("k1"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("snapshot not deleted")
	}
	if string(f.live) != "k1|a@x|v1" {
		t.Fatal("live credentials touched")
	}
}

func TestSetAlias(t *testing.T) {
	m, f := newManager(t)
	addAccount(t, m, f, "k1|a@x|v1", "")
	addAccount(t, m, f, "k2|b@x|v1", "home")
	if err := m.SetAlias("1", "work"); err != nil {
		t.Fatal(err)
	}
	r, _ := m.S.Load()
	if r.Accounts[0].Alias != "work" {
		t.Fatalf("alias %q", r.Accounts[0].Alias)
	}
	for _, bad := range []string{"home", "3", "-"} {
		if err := m.SetAlias("1", bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if err := m.SetAlias("1", ""); err != nil {
		t.Fatal(err)
	}
}

func TestSwitchSavesUnsavedLiveAccountFirst(t *testing.T) {
	m, f := newManager(t)
	addAccount(t, m, f, "k1|a@x|v1", "")
	f.live = []byte("k9|new@x|v1")
	if _, _, err := m.Switch("1"); err != nil {
		t.Fatal(err)
	}
	r, _ := m.S.Load()
	if r.Index("k9") < 0 || r.Previous != "k9" || snapshot(t, m, "k9") != "k9|new@x|v1" {
		t.Fatalf("unsaved live account lost: %+v", r)
	}
}
