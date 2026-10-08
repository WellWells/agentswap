package claude

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCswap(t *testing.T, dir string, slot int, email, credsJSON, config string) {
	t.Helper()
	os.MkdirAll(filepath.Join(dir, "credentials"), 0o700)
	os.MkdirAll(filepath.Join(dir, "configs"), 0o700)
	os.WriteFile(filepath.Join(dir, "credentials", fmt.Sprintf(".creds-%d-%s.enc", slot, email)), []byte(base64.StdEncoding.EncodeToString([]byte(credsJSON))), 0o600)
	os.WriteFile(filepath.Join(dir, "configs", fmt.Sprintf(".claude-config-%d-%s.json", slot, email)), []byte(config), 0o600)
}

func TestReadCswap(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "sequence.json"), []byte(`{"activeAccountNumber":1,"sequence":[2,1,3,4],"accounts":{
		"1":{"email":"a@x","uuid":"u1","organizationUuid":"o1","alias":"work"},
		"2":{"email":"b@x","uuid":"u2","organizationUuid":"o2"},
		"3":{"email":"api-key-3@token.local","kind":"api_key"},
		"4":{"email":"d@x"}}}`), 0o600)
	writeCswap(t, dir, 1, "a@x", strings.TrimSuffix(creds("at1", "rt1"), "}")+`,"mcpOAuth":{"n":1}}`, `{"oauthAccount":`+oauth("u1", "o1", "a@x")+`,"projects":{}}`)
	writeCswap(t, dir, 2, "b@x", creds("at2", "rt2"), `{"oauthAccount":`+oauth("u2", "o2", "b@x")+`}`)
	before, _ := os.ReadFile(filepath.Join(dir, "sequence.json"))
	got, err := ReadCswap(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || got[0].Slot != 2 || got[1].Alias != "work" {
		t.Fatalf("%+v", got)
	}
	id, err := Identify(got[1].Snapshot)
	if err != nil || id.Key != "claude:u1:o1" {
		t.Fatalf("%+v %v", id, err)
	}
	if strings.Contains(string(got[1].Snapshot), "mcpOAuth") {
		t.Fatalf("shared key leaked: %s", got[1].Snapshot)
	}
	if !errors.Is(got[2].Err, ErrUnsupportedLogin) {
		t.Fatalf("api key: %v", got[2].Err)
	}
	if got[3].Err == nil {
		t.Fatal("missing files should fail")
	}
	if after, _ := os.ReadFile(filepath.Join(dir, "sequence.json")); string(before) != string(after) {
		t.Fatal("source modified")
	}
}

func TestReadCswapRejectsRawAPIKey(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "sequence.json"), []byte(`{"sequence":[1],"accounts":{"1":{"email":"a@x"}}}`), 0o600)
	writeCswap(t, dir, 1, "a@x", "sk-ant-api03-xxx", `{"oauthAccount":`+oauth("u1", "o1", "a@x")+`}`)
	got, err := ReadCswap(dir, nil)
	if err != nil || !errors.Is(got[0].Err, ErrUnsupportedLogin) {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestReadCswapUsesKeychainWhenEncMissing(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "sequence.json"), []byte(`{"sequence":[1],"accounts":{"1":{"email":"a@x"}}}`), 0o600)
	os.MkdirAll(filepath.Join(dir, "configs"), 0o700)
	os.WriteFile(filepath.Join(dir, "configs", ".claude-config-1-a@x.json"), []byte(`{"oauthAccount":`+oauth("u1", "o1", "a@x")+`}`), 0o600)
	got, err := ReadCswap(dir, func(slot int, email string) ([]byte, error) {
		if slot != 1 || email != "a@x" {
			t.Fatalf("%d %s", slot, email)
		}
		return []byte(creds("at", "rt")), nil
	})
	if err != nil || got[0].Err != nil {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestCswapDirs(t *testing.T) {
	env := func(string) string { return "" }
	if d := CswapDirs("/h", "windows", env); len(d) != 1 || d[0] != filepath.Join("/h", ".claude-swap-backup") {
		t.Fatal(d)
	}
	if d := CswapDirs("/h", "linux", env); d[0] != filepath.Join("/h", ".local", "share", "claude-swap") || d[1] != filepath.Join("/h", ".claude-swap-backup") {
		t.Fatal(d)
	}
}
