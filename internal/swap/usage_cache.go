package swap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/WellWells/agentswap/internal/fsx"
	"github.com/WellWells/agentswap/internal/store"
)

const usageTTL = time.Minute

var (
	ErrUsageLoginExpired = errors.New("login expired")
	ErrUsageNetwork      = errors.New("network error")
	ErrUsageUnsupported  = errors.New("usage is only available for ChatGPT logins")
	ErrUsageActiveStale  = errors.New("token expired")
	ErrRateLimited       = errors.New("rate limited")
	ErrNoCachedUsage     = errors.New("no cached usage")
	ErrUsageUnavailable  = errors.New("usage unavailable")
)

type rateLimitError struct{ until time.Time }

func (e rateLimitError) Error() string        { return ErrRateLimited.Error() }
func (e rateLimitError) Unwrap() error        { return ErrRateLimited }
func (e rateLimitError) RetryTime() time.Time { return e.until }

func RateLimit(header string, now time.Time) error {
	header = strings.TrimSpace(header)
	until := now.Add(time.Minute)
	if seconds, err := strconv.ParseUint(header, 10, 63); err == nil && seconds > 0 && seconds <= uint64((1<<63-1)/int64(time.Second)) {
		until = now.Add(time.Duration(seconds) * time.Second)
	} else if date, err := http.ParseTime(header); err == nil && date.After(now) {
		until = date
	}
	return rateLimitError{until.UTC()}
}

type usageCache struct {
	AddedAt   time.Time `json:"added_at"`
	CheckedAt time.Time `json:"checked_at"`
	Usage     Usage     `json:"usage"`
	Failure   string    `json:"failure,omitempty"`
	RetryAt   time.Time `json:"retry_at"`
	DeadLogin string    `json:"dead_login,omitempty"`
}

func snapshotPrint(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (c usageCache) deadLogin(snap []byte) bool {
	return c.Failure == "login_expired" && c.DeadLogin != "" && c.DeadLogin == snapshotPrint(snap)
}

func (m *Manager) usageCachePath(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(m.S.Dir, "usage", hex.EncodeToString(sum[:12])+".json")
}

func (m *Manager) removeUsageCache(key string) error {
	err := os.Remove(m.usageCachePath(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (m *Manager) readUsageCache(a store.Account) (usageCache, error) {
	var c usageCache
	b, err := os.ReadFile(m.usageCachePath(a.Key))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if json.Unmarshal(b, &c) != nil || !a.AddedAt.Equal(c.AddedAt) {
		return usageCache{}, nil
	}
	return c, nil
}

func (m *Manager) saveUsageCache(a store.Account, c usageCache) error {
	c.AddedAt = a.AddedAt
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return fsx.WriteAtomic(m.usageCachePath(a.Key), b, 0o600)
}

func (c usageCache) fresh(now time.Time) bool {
	age := now.Sub(c.Usage.At)
	if age < 0 || age >= usageTTL || c.Failure != "" || c.CheckedAt.IsZero() {
		return false
	}
	for _, w := range c.Usage.Windows {
		if !w.ResetsAt.IsZero() && (w.ResetsAt.Before(now) || w.ResetsAt.Equal(now) && w.ResetsAt.After(c.Usage.At)) {
			return false
		}
	}
	return true
}

func (c usageCache) reusable(now time.Time) bool {
	age := now.Sub(c.CheckedAt)
	if c.CheckedAt.IsZero() || age < 0 || age >= usageTTL || c.Failure != "" {
		return false
	}
	for _, w := range c.Usage.Windows {
		if w.ResetsAt.After(c.CheckedAt) && !w.ResetsAt.After(now) {
			return false
		}
	}
	return true
}

func (c usageCache) result(now time.Time) UsageResult {
	out := UsageResult{Usage: c.Usage, Cached: true, CheckedAt: c.CheckedAt, RetryAt: c.RetryAt}
	if !c.fresh(now) {
		out.Stale = !c.Usage.At.IsZero()
		out.Usage.Live = false
	}
	if c.CheckedAt.IsZero() {
		out.Err = ErrNoCachedUsage
	} else if c.Failure != "" {
		out.Err = usageFailures[c.Failure]
		if out.Err == nil {
			out.Err = ErrUsageUnavailable
		}
	}
	return out
}

var usageFailures = map[string]error{
	"rate_limited":        ErrRateLimited,
	"login_expired":       ErrUsageLoginExpired,
	"unsupported":         ErrUsageUnsupported,
	"token_expired":       ErrUsageActiveStale,
	"timeout":             context.DeadlineExceeded,
	"network":             ErrUsageNetwork,
	"unavailable":         ErrUsageUnavailable,
	"identity_mismatch":   ErrMismatch,
	"identity_unverified": ErrUnverified,
	"stale_login":         ErrStaleLogin,
}

func usageFailure(err error) string {
	if err == nil {
		return ""
	}
	for code, known := range usageFailures {
		if errors.Is(err, known) {
			return code
		}
	}
	var network *url.Error
	if errors.As(err, &network) {
		return "network"
	}
	return "unavailable"
}

func (c *usageCache) record(u Usage, err error, now time.Time) UsageResult {
	hadOld := false
	if err == nil && len(u.Windows) == 0 && len(c.Usage.Windows) > 0 {
		err = ErrUsageUnavailable
	}
	if err == nil && u.At.IsZero() && u.Live {
		u.At = now
	}
	if (err == nil || len(u.Windows) > 0) && !u.At.Before(c.Usage.At) {
		c.Usage = u
	} else {
		hadOld = !c.Usage.At.IsZero()
	}
	c.CheckedAt = now.UTC()
	c.RetryAt = time.Time{}
	c.Failure = usageFailure(err)
	if errors.Is(err, ErrRateLimited) {
		var limited rateLimitError
		if errors.As(err, &limited) {
			c.RetryAt = limited.until
		} else {
			c.RetryAt = now.Add(time.Minute).UTC()
		}
	}
	out := c.result(now)
	out.Cached = hadOld
	out.Err = err
	out.Usage.Live = !hadOld && err == nil && u.Live
	return out
}

func (m *Manager) UsageCached(ctx context.Context, q string) (Status, map[string]UsageResult, error) {
	r, err := m.S.Load()
	if err != nil {
		return Status{}, nil, err
	}
	st := Status{Registry: r}
	accounts, err := usageAccounts(r, q)
	if err != nil {
		return st, nil, err
	}
	results := make(map[string]UsageResult, len(accounts))
	for _, a := range accounts {
		if err := ctx.Err(); err != nil {
			return st, nil, err
		}
		c, err := m.readUsageCache(a)
		if err != nil {
			results[a.Key] = UsageResult{Err: err, Cached: true}
			continue
		}
		results[a.Key] = c.result(m.now())
	}
	return st, results, nil
}
