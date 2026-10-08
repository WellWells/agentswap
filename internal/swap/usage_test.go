package swap

import (
	"context"
	"errors"
	"sync"
	"testing"
)

type usageFake struct {
	*fakeProvider
	mu     sync.Mutex
	active map[string]bool
}

func (u *usageFake) Usage(ctx context.Context, snap []byte, active bool) (Usage, []byte, error) {
	id, err := u.Identify(snap)
	if err != nil {
		return Usage{}, nil, err
	}
	u.mu.Lock()
	u.active[id.Key] = active
	u.mu.Unlock()
	switch id.Key {
	case "k3":
		return Usage{}, nil, errors.New("boom")
	default:
		return Usage{Plan: "p-" + id.Key, Live: true}, []byte(id.Key + "|" + id.Email + "|refreshed"), nil
	}
}

func newUsageManager(t *testing.T) (*Manager, *usageFake) {
	m, f := newManager(t)
	u := &usageFake{fakeProvider: f, active: map[string]bool{}}
	m.P = u
	return m, u
}

func TestUsageForAllAccounts(t *testing.T) {
	m, u := newUsageManager(t)
	addAccount(t, m, u.fakeProvider, "k1|a@x|v1", "")
	addAccount(t, m, u.fakeProvider, "k3|c@x|v1", "")
	addAccount(t, m, u.fakeProvider, "k2|b@x|v1", "")
	u.live = []byte("k1|a@x|v1")

	st, res, err := m.Usage(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if st.Live.Key != "k1" || len(res) != 3 {
		t.Fatalf("status %+v results %+v", st, res)
	}
	if res["k1"].Usage.Plan != "p-k1" || res["k2"].Usage.Plan != "p-k2" || res["k3"].Err == nil {
		t.Fatalf("results %+v", res)
	}
	if !u.active["k1"] || u.active["k2"] || u.active["k3"] {
		t.Fatalf("active flags %+v", u.active)
	}
	if got := snapshot(t, m, "k2"); got != "k2|b@x|refreshed" {
		t.Fatalf("inactive refreshed snapshot not stored: %q", got)
	}
	if got := snapshot(t, m, "k1"); got != "k1|a@x|v1" {
		t.Fatalf("active snapshot must not be replaced: %q", got)
	}
	if string(u.live) != "k1|a@x|v1" {
		t.Fatalf("live credentials touched: %q", u.live)
	}
}

func TestUsageOnlyLiveIncludesUnsavedLogin(t *testing.T) {
	m, u := newUsageManager(t)
	addAccount(t, m, u.fakeProvider, "k1|a@x|v1", "")
	u.live = []byte("k9|z@x|v1")
	_, res, err := m.Usage(context.Background(), false)
	if err != nil || len(res) != 1 || res["k9"].Usage.Plan != "p-k9" || !u.active["k9"] {
		t.Fatalf("res %+v err %v active %+v", res, err, u.active)
	}
}

func TestUsageWithoutReaderIsEmpty(t *testing.T) {
	m, f := newManager(t)
	addAccount(t, m, f, "k1|a@x|v1", "")
	_, res, err := m.Usage(context.Background(), true)
	if err != nil || len(res) != 0 {
		t.Fatalf("res %+v err %v", res, err)
	}
}
