package transfer

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/WellWells/agentswap/internal/antigravity"
	"github.com/WellWells/agentswap/internal/claude"
	"github.com/WellWells/agentswap/internal/codex"
	"github.com/WellWells/agentswap/internal/store"
	"github.com/WellWells/agentswap/internal/swap"
)

func fakeJWT(claims map[string]any) string {
	b, _ := json.Marshal(claims)
	enc := base64.RawURLEncoding
	return enc.EncodeToString([]byte(`{"alg":"RS256"}`)) + "." + enc.EncodeToString(b) + ".sig"
}

type memKeyring struct {
	b []byte
}

func (m *memKeyring) Read() ([]byte, error) {
	if m.b == nil {
		return nil, os.ErrNotExist
	}
	return m.b, nil
}

func (m *memKeyring) Write(b []byte) error {
	m.b = append([]byte(nil), b...)
	return nil
}

func newTestManagers(t *testing.T) map[string]*swap.Manager {
	t.Helper()
	dir := t.TempDir()

	codexHome := filepath.Join(dir, "codex_home")
	claudeHome := filepath.Join(dir, "claude_home")
	os.MkdirAll(codexHome, 0o700)
	os.MkdirAll(claudeHome, 0o700)

	cxMgr := &swap.Manager{
		P: codex.Provider{Home: codexHome},
		S: store.Store{Dir: filepath.Join(dir, "store_codex")},
	}
	ccMgr := &swap.Manager{
		P: claude.New(func(string) string { return "" }, claudeHome, "linux", nil),
		S: store.Store{Dir: filepath.Join(dir, "store_claude")},
	}
	agMgr := &swap.Manager{
		P: antigravity.Provider{Keyring: &memKeyring{}},
		S: store.Store{Dir: filepath.Join(dir, "store_agy")},
	}

	return map[string]*swap.Manager{
		"codex":       cxMgr,
		"claude":      ccMgr,
		"antigravity": agMgr,
	}
}

func TestCodexAPIKey(t *testing.T) {
	snap := []byte(`{"OPENAI_API_KEY":"sk-proj-1234567890abcdef"}`)

	// 1. Extract
	tokRaw, err := Extract("codex", snap)
	if err != nil {
		t.Fatalf("extract codex apikey: %v", err)
	}

	var apiKey string
	if err := json.Unmarshal(tokRaw, &apiKey); err != nil {
		t.Fatalf("expected string token, got err: %v", err)
	}
	if apiKey != "sk-proj-1234567890abcdef" {
		t.Fatalf("unexpected token string: %s", apiKey)
	}

	// 2. Reconstruct from bare string
	reconstructed, err := Reconstruct("codex", tokRaw)
	if err != nil {
		t.Fatalf("reconstruct codex apikey: %v", err)
	}
	id, err := codex.Identify(reconstructed)
	if err != nil {
		t.Fatalf("identify reconstructed codex apikey: %v", err)
	}
	if id.Mode != "apikey" || id.Email != "sk-...cdef" {
		t.Fatalf("unexpected identity: %+v", id)
	}

	// 3. Import
	mgrs := newTestManagers(t)
	bundleJSON, _ := json.Marshal(Bundle{
		"codex": []AccountItem{{Alias: "work-key", Token: tokRaw}},
	})
	rep, err := Import(bundleJSON, mgrs)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(rep.Failures()) > 0 {
		t.Fatalf("import failures: %+v", rep.Failures())
	}
	if len(rep.Successes()) != 1 {
		t.Fatalf("expected 1 success, got %d", len(rep.Successes()))
	}
	succ := rep.Successes()[0]
	if succ.Account.Alias != "work-key" || succ.Account.Email != "sk-...cdef" {
		t.Fatalf("unexpected account: %+v", succ.Account)
	}
}

func TestCodexOAuth(t *testing.T) {
	idToken := fakeJWT(map[string]any{
		"email": "user@openai.com",
		"sub":   "auth0|user123",
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "org-xyz",
			"chatgpt_user_id":    "u-123",
			"chatgpt_plan_type":  "plus",
		},
	})
	snap, _ := json.Marshal(map[string]any{
		"tokens": map[string]any{
			"id_token":      idToken,
			"refresh_token": "rt-openai",
			"account_id":    "org-xyz",
			"access_token":  "at-openai",
		},
	})

	// 1. Extract
	tokRaw, err := Extract("codex", snap)
	if err != nil {
		t.Fatalf("extract codex oauth: %v", err)
	}

	// 2. Reconstruct
	reconstructed, err := Reconstruct("codex", tokRaw)
	if err != nil {
		t.Fatalf("reconstruct codex oauth: %v", err)
	}
	id, err := codex.Identify(reconstructed)
	if err != nil {
		t.Fatalf("identify reconstructed oauth: %v", err)
	}
	if id.Email != "user@openai.com" || id.Plan != "plus" {
		t.Fatalf("unexpected identity: %+v", id)
	}

	// 3. Import
	mgrs := newTestManagers(t)
	bundleJSON, _ := json.Marshal(Bundle{
		"codex": []AccountItem{{Alias: "plus-acct", Token: tokRaw}},
	})
	rep, err := Import(bundleJSON, mgrs)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(rep.Failures()) > 0 {
		t.Fatalf("import failures: %+v", rep.Failures())
	}
	if rep.Successes()[0].Account.Alias != "plus-acct" {
		t.Fatalf("unexpected alias: %+v", rep.Successes()[0].Account)
	}
}

func TestClaude(t *testing.T) {
	credsData, _ := json.Marshal(map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":  "at-claude",
			"refreshToken": "rt-claude",
		},
	})
	oauthData, _ := json.Marshal(map[string]any{
		"accountUuid":      "uuid-claude-1",
		"organizationUuid": "org-claude-1",
		"emailAddress":     "claude@example.com",
	})
	snap := claude.Pack(credsData, oauthData)

	// 1. Extract
	tokRaw, err := Extract("claude", snap)
	if err != nil {
		t.Fatalf("extract claude: %v", err)
	}

	// 2. Reconstruct
	reconstructed, err := Reconstruct("claude", tokRaw)
	if err != nil {
		t.Fatalf("reconstruct claude: %v", err)
	}
	id, err := claude.Identify(reconstructed)
	if err != nil {
		t.Fatalf("identify reconstructed claude: %v", err)
	}
	if id.Email != "claude@example.com" || id.Key != "claude:uuid-claude-1:org-claude-1" {
		t.Fatalf("unexpected identity: %+v", id)
	}

	// 3. Import
	mgrs := newTestManagers(t)
	bundleJSON, _ := json.Marshal(Bundle{
		"claude": []AccountItem{{Alias: "claude-team", Token: tokRaw}},
	})
	rep, err := Import(bundleJSON, mgrs)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(rep.Failures()) > 0 {
		t.Fatalf("import failures: %+v", rep.Failures())
	}
	if rep.Successes()[0].Account.Alias != "claude-team" {
		t.Fatalf("unexpected alias: %+v", rep.Successes()[0].Account)
	}
}

func TestAntigravity(t *testing.T) {
	idToken := fakeJWT(map[string]any{
		"sub":   "google-sub-789",
		"email": "agy@google.com",
	})
	snap, _ := json.Marshal(map[string]any{
		"token": map[string]any{
			"access_token":  "at-agy",
			"refresh_token": "rt-agy",
		},
		"id_token": idToken,
	})

	// 1. Extract
	tokRaw, err := Extract("antigravity", snap)
	if err != nil {
		t.Fatalf("extract antigravity: %v", err)
	}

	// 2. Reconstruct
	reconstructed, err := Reconstruct("antigravity", tokRaw)
	if err != nil {
		t.Fatalf("reconstruct antigravity: %v", err)
	}
	id, err := antigravity.Identify(reconstructed)
	if err != nil {
		t.Fatalf("identify reconstructed antigravity: %v", err)
	}
	if id.Email != "agy@google.com" || id.Key != "google:google-sub-789" {
		t.Fatalf("unexpected identity: %+v", id)
	}

	// 3. Import
	mgrs := newTestManagers(t)
	bundleJSON, _ := json.Marshal(Bundle{
		"antigravity": []AccountItem{{Alias: "agy-main", Token: tokRaw}},
	})
	rep, err := Import(bundleJSON, mgrs)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(rep.Failures()) > 0 {
		t.Fatalf("import failures: %+v", rep.Failures())
	}
	if rep.Successes()[0].Account.Alias != "agy-main" {
		t.Fatalf("unexpected alias: %+v", rep.Successes()[0].Account)
	}
}

func TestOverwriteImport(t *testing.T) {
	mgrs := newTestManagers(t)

	// Step 1: Initial import with alias "original"
	snap1 := []byte(`{"OPENAI_API_KEY":"sk-proj-same-key"}`)
	tok1, _ := Extract("codex", snap1)
	bundle1, _ := json.Marshal(Bundle{
		"codex": []AccountItem{{Alias: "original", Token: tok1}},
	})
	rep1, err := Import(bundle1, mgrs)
	if err != nil || len(rep1.Failures()) > 0 {
		t.Fatalf("initial import failed: %v %+v", err, rep1.Failures())
	}
	if rep1.Successes()[0].Account.Alias != "original" {
		t.Fatalf("expected 'original', got %s", rep1.Successes()[0].Account.Alias)
	}

	// Check registry has 1 account
	reg1, _ := mgrs["codex"].S.Load()
	if len(reg1.Accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(reg1.Accounts))
	}

	// Step 2: Overwrite import with new alias "updated"
	bundle2, _ := json.Marshal(Bundle{
		"codex": []AccountItem{{Alias: "updated", Token: tok1}},
	})
	rep2, err := Import(bundle2, mgrs)
	if err != nil || len(rep2.Failures()) > 0 {
		t.Fatalf("overwrite import failed: %v %+v", err, rep2.Failures())
	}

	// Check registry still has 1 account, and alias is updated
	reg2, _ := mgrs["codex"].S.Load()
	if len(reg2.Accounts) != 1 {
		t.Fatalf("expected 1 account after overwrite, got %d", len(reg2.Accounts))
	}
	if reg2.Accounts[0].Alias != "updated" {
		t.Fatalf("expected alias to be updated to 'updated', got %s", reg2.Accounts[0].Alias)
	}
}

func TestRoundTripExportImport(t *testing.T) {
	mgrsSrc := newTestManagers(t)

	// Populate src with 1 codex apikey, 1 codex oauth, 1 claude, 1 antigravity
	// 1) Codex API Key
	snapCXKey := []byte(`{"OPENAI_API_KEY":"sk-proj-roundtrip-key"}`)
	if _, err := mgrsSrc["codex"].Import(snapCXKey, "cx-key"); err != nil {
		t.Fatalf("seed cx key: %v", err)
	}

	// 2) Codex OAuth
	idTokenCX := fakeJWT(map[string]any{
		"email": "cx-user@roundtrip.com",
		"sub":   "sub-cx",
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acct-cx",
			"chatgpt_user_id":    "user-cx",
			"chatgpt_plan_type":  "pro",
		},
	})
	snapCXOAuth, _ := json.Marshal(map[string]any{
		"tokens": map[string]any{
			"id_token":      idTokenCX,
			"refresh_token": "rt-cx-roundtrip",
			"account_id":    "acct-cx",
		},
	})
	if _, err := mgrsSrc["codex"].Import(snapCXOAuth, "cx-oauth"); err != nil {
		t.Fatalf("seed cx oauth: %v", err)
	}

	// 3) Claude
	credsClaude, _ := json.Marshal(map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":  "at-cl-roundtrip",
			"refreshToken": "rt-cl-roundtrip",
		},
	})
	oauthClaude, _ := json.Marshal(map[string]any{
		"accountUuid":  "uuid-cl-roundtrip",
		"emailAddress": "cl-user@roundtrip.com",
	})
	if _, err := mgrsSrc["claude"].Import(claude.Pack(credsClaude, oauthClaude), "cl-alias"); err != nil {
		t.Fatalf("seed claude: %v", err)
	}

	// 4) Antigravity
	idTokenAG := fakeJWT(map[string]any{
		"sub":   "sub-ag-roundtrip",
		"email": "ag-user@roundtrip.com",
	})
	snapAG, _ := json.Marshal(map[string]any{
		"token": map[string]any{
			"refresh_token": "rt-ag-roundtrip",
			"access_token":  "at-ag-roundtrip",
		},
		"id_token": idTokenAG,
	})
	if _, err := mgrsSrc["antigravity"].Import(snapAG, "ag-alias"); err != nil {
		t.Fatalf("seed antigravity: %v", err)
	}

	// Export
	bundle, err := Export(mgrsSrc)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(bundle["codex"]) != 2 || len(bundle["claude"]) != 1 || len(bundle["antigravity"]) != 1 {
		t.Fatalf("unexpected export counts: %+v", bundle)
	}

	// Serialize
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}

	// Create brand new target managers
	mgrsDst := newTestManagers(t)

	// Import into target managers
	rep, err := Import(data, mgrsDst)
	if err != nil {
		t.Fatalf("import roundtrip: %v", err)
	}
	if len(rep.Failures()) > 0 {
		t.Fatalf("import failures: %+v", rep.Failures())
	}
	if len(rep.Successes()) != 4 {
		t.Fatalf("expected 4 successes, got %d", len(rep.Successes()))
	}

	// Verify target registries
	regCX, _ := mgrsDst["codex"].S.Load()
	if len(regCX.Accounts) != 2 {
		t.Fatalf("expected 2 codex accounts, got %d", len(regCX.Accounts))
	}
	regCL, _ := mgrsDst["claude"].S.Load()
	if len(regCL.Accounts) != 1 || regCL.Accounts[0].Alias != "cl-alias" {
		t.Fatalf("unexpected claude accounts: %+v", regCL.Accounts)
	}
	regAG, _ := mgrsDst["antigravity"].S.Load()
	if len(regAG.Accounts) != 1 || regAG.Accounts[0].Alias != "ag-alias" {
		t.Fatalf("unexpected antigravity accounts: %+v", regAG.Accounts)
	}
}
