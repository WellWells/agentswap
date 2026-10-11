package transfer

import "encoding/json"

// AccountItem represents a single exported account with its alias and minimal token.
type AccountItem struct {
	Alias string          `json:"alias,omitempty"`
	Token json.RawMessage `json:"token"`
}

// Bundle represents an export bundle of accounts grouped by provider name.
type Bundle map[string][]AccountItem
