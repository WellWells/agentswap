package ui

import (
	"strings"
	"time"

	"github.com/WellWells/agentswap/internal/swap"
)

func displayedPercent(c Card, w swap.Window, now time.Time) int {
	if c.Cached {
		return w.UsedPercent
	}
	return w.Percent(now)
}

func MarkModelSuggestions(cards []Card, now time.Time, cmd func(Card) string, model string) {
	candidates := make([]Card, len(cards))
	for i, c := range cards {
		candidates[i] = c
		usage := c.Usage
		usage.Windows = nil
		modelFound := model == ""
		for _, w := range c.Usage.Windows {
			if w.Label != "" {
				if model == "" || !strings.EqualFold(w.Label, model) {
					continue
				}
				modelFound = true
				w.Label = ""
			}
			if w.Percent(now) >= 100 {
				usage.Live = false
			}
			usage.Windows = append(usage.Windows, w)
		}
		usage.Live = usage.Live && !c.Stale && modelFound && len(usage.Windows) > 0 && c.ErrorCode == ""
		candidates[i].Usage = usage
		candidates[i].Suggest = ""
	}
	rankSuggestions(candidates, now, cmd)
	for i := range candidates {
		cards[i].Suggest = candidates[i].Suggest
	}
}
