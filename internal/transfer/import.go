package transfer

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/WellWells/agentswap/internal/store"
	"github.com/WellWells/agentswap/internal/swap"
)

// Result records the outcome of importing a single account.
type Result struct {
	Provider string
	Alias    string
	Account  store.Account
	Err      error
}

// Report holds the aggregated results of an import operation.
type Report struct {
	Results []Result
}

// Successes returns all successfully imported results.
func (r Report) Successes() []Result {
	var out []Result
	for _, res := range r.Results {
		if res.Err == nil {
			out = append(out, res)
		}
	}
	return out
}

// Failures returns all failed results.
func (r Report) Failures() []Result {
	var out []Result
	for _, res := range r.Results {
		if res.Err != nil {
			out = append(out, res)
		}
	}
	return out
}

func canonicalProvider(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "codex", "cx":
		return "codex"
	case "claude", "cc":
		return "claude"
	case "antigravity", "agy", "ag":
		return "antigravity"
	default:
		return name
	}
}

// Import parses the input JSON bytes, reconstructs snapshots for each account,
// and imports them into their respective managers using native overwrite strategy.
func Import(data []byte, mgrs map[string]*swap.Manager) (Report, error) {
	var bundle Bundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		// Lenient fallback: if input is a raw []AccountItem and exactly one manager is supplied
		var items []AccountItem
		if errList := json.Unmarshal(data, &items); errList == nil && len(mgrs) == 1 {
			for k := range mgrs {
				bundle = Bundle{k: items}
				break
			}
		} else {
			return Report{}, fmt.Errorf("invalid import JSON: %w", err)
		}
	}

	report := Report{Results: make([]Result, 0)}

	for providerRaw, items := range bundle {
		canon := canonicalProvider(providerRaw)
		mgr := mgrs[canon]
		if mgr == nil {
			mgr = mgrs[providerRaw]
		}

		if mgr == nil {
			for _, it := range items {
				report.Results = append(report.Results, Result{
					Provider: providerRaw,
					Alias:    it.Alias,
					Err:      fmt.Errorf("%w: %s", ErrUnsupportedProvider, providerRaw),
				})
			}
			continue
		}

		for _, it := range items {
			snap, err := Reconstruct(canon, it.Token)
			if err != nil {
				report.Results = append(report.Results, Result{
					Provider: canon,
					Alias:    it.Alias,
					Err:      err,
				})
				continue
			}

			acct, err := mgr.Import(snap, it.Alias)
			report.Results = append(report.Results, Result{
				Provider: canon,
				Alias:    it.Alias,
				Account:  acct,
				Err:      err,
			})
		}
	}

	return report, nil
}
