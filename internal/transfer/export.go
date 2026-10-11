package transfer

import (
	"fmt"
	"sort"

	"github.com/WellWells/agentswap/internal/swap"
)

// Export collects all saved accounts from the provided managers and extracts their minimal tokens into a Bundle.
func Export(mgrs map[string]*swap.Manager) (Bundle, error) {
	bundle := make(Bundle)

	// Collect keys in deterministic order
	keys := make([]string, 0, len(mgrs))
	for k := range mgrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, provider := range keys {
		mgr := mgrs[provider]
		if mgr == nil {
			continue
		}

		r, err := mgr.S.Load()
		if err != nil {
			return nil, fmt.Errorf("load registry for %s: %w", provider, err)
		}

		if len(r.Accounts) == 0 {
			continue
		}

		items := make([]AccountItem, 0, len(r.Accounts))
		for _, a := range r.Accounts {
			snap, err := mgr.S.ReadSnapshot(a.Key)
			if err != nil {
				return nil, fmt.Errorf("read snapshot for %s (%s): %w", provider, a.Key, err)
			}

			tok, err := Extract(provider, snap)
			if err != nil {
				return nil, fmt.Errorf("extract token for %s (%s): %w", provider, a.Key, err)
			}

			items = append(items, AccountItem{
				Alias: a.Alias,
				Token: tok,
			})
		}

		bundle[provider] = items
	}

	return bundle, nil
}
