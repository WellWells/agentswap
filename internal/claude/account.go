package claude

import (
	"encoding/json"
	"os"
)

type OAuthAccount struct {
	AccountUUID string  `json:"accountUuid"`
	OrgUUID     *string `json:"organizationUuid"`
	Email       string  `json:"emailAddress"`
}

func (a OAuthAccount) Organization() string {
	if a.OrgUUID == nil {
		return ""
	}
	return *a.OrgUUID
}

func (a OAuthAccount) Key() string {
	return "claude:" + a.AccountUUID + ":" + a.Organization()
}

func (p Provider) ReadAccount() (OAuthAccount, error) {
	var config struct {
		Account OAuthAccount `json:"oauthAccount"`
	}
	b, err := os.ReadFile(p.GlobalConfig)
	if err != nil {
		return OAuthAccount{}, err
	}
	if err := json.Unmarshal(b, &config); err != nil {
		return OAuthAccount{}, err
	}
	if config.Account.AccountUUID == "" {
		return OAuthAccount{}, os.ErrNotExist
	}
	return config.Account, nil
}
