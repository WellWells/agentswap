package transfer

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestPBKDF2Vectors(t *testing.T) {
	cases := []struct {
		pass, salt string
		iter       int
		want       string
	}{
		{"passwd", "salt", 1, "55ac046e56e3089fec1691c22544b605f94185216dde0465e68b9d57c20dacbc49ca9cccf179b645991664b39d77ef317c71b845b1e30bd509112041d3a19783"},
		{"Password", "NaCl", 80000, "4ddcd8f60b98be21830cee5ef22701f9641a4418d04c0414aeff08876b34ab56a1d425a1225833549adb841b51c9b3176a272bdebba1d078478f62b397f33c8d"},
	}
	for _, c := range cases {
		got := hex.EncodeToString(pbkdf2([]byte(c.pass), []byte(c.salt), c.iter, 64))
		if got != c.want {
			t.Fatalf("pbkdf2(%q, %q, %d) = %s, want %s", c.pass, c.salt, c.iter, got, c.want)
		}
	}
}

func fastKDF(t *testing.T) {
	old := iterations
	iterations = 1000
	t.Cleanup(func() { iterations = old })
}

func sample() Bundle {
	return Bundle{
		"codex":       {{Alias: "work", Snapshot: []byte("{\n  \"auth_mode\": \"chatgpt\"\n}\n")}},
		"antigravity": {{Snapshot: []byte(`{"token":{}}`)}},
	}
}

func TestSealRoundTrip(t *testing.T) {
	fastKDF(t)
	data, err := Seal(sample(), []byte("correct horse"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("chatgpt")) || bytes.Contains(data, []byte("work")) {
		t.Fatalf("sealed file leaks plaintext:\n%s", data)
	}
	f, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if !f.Encrypted() {
		t.Fatal("sealed file not reported as encrypted")
	}
	got, err := f.Open([]byte("correct horse"))
	if err != nil {
		t.Fatal(err)
	}
	want := sample()
	if len(got) != 2 || got["codex"][0].Alias != "work" || !bytes.Equal(got["codex"][0].Snapshot, want["codex"][0].Snapshot) || !bytes.Equal(got["antigravity"][0].Snapshot, want["antigravity"][0].Snapshot) {
		t.Fatalf("round trip changed the bundle: %+v", got)
	}
}

func TestSealUsesDefaultIterations(t *testing.T) {
	if iterations != 600_000 {
		t.Fatalf("iterations = %d", iterations)
	}
}

func TestSealFreshSaltAndNonce(t *testing.T) {
	fastKDF(t)
	a, _ := Seal(sample(), []byte("passphrase"))
	b, _ := Seal(sample(), []byte("passphrase"))
	fa, _ := Parse(a)
	fb, _ := Parse(b)
	if bytes.Equal(fa.env.KDF.Salt, fb.env.KDF.Salt) || bytes.Equal(fa.env.Nonce, fb.env.Nonce) {
		t.Fatal("salt or nonce reused")
	}
}

func TestOpenWrongPassphrase(t *testing.T) {
	fastKDF(t)
	data, _ := Seal(sample(), []byte("correct horse"))
	f, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Open([]byte("wrong horse")); !errors.Is(err, ErrPassphrase) {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenDetectsTampering(t *testing.T) {
	fastKDF(t)
	data, _ := Seal(sample(), []byte("correct horse"))
	edit := func(fn func(m map[string]any)) []byte {
		var m map[string]any
		json.Unmarshal(data, &m)
		fn(m)
		out, _ := json.Marshal(m)
		return out
	}
	cases := map[string][]byte{
		"iterations": edit(func(m map[string]any) { m["kdf"].(map[string]any)["iterations"] = 999 }),
		"data": edit(func(m map[string]any) {
			s := m["data"].(string)
			m["data"] = "A" + s[1:]
			if s[0] == 'A' {
				m["data"] = "B" + s[1:]
			}
		}),
	}
	for name, b := range cases {
		f, err := Parse(b)
		if err != nil {
			t.Fatalf("%s: parse: %v", name, err)
		}
		if _, err := f.Open([]byte("correct horse")); !errors.Is(err, ErrPassphrase) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
}

func TestPlainRoundTrip(t *testing.T) {
	data, err := Plain(sample())
	if err != nil {
		t.Fatal(err)
	}
	f, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if f.Encrypted() {
		t.Fatal("plain file reported as encrypted")
	}
	got, err := f.Open(nil)
	if err != nil || got["codex"][0].Alias != "work" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestParseRejects(t *testing.T) {
	cases := map[string]string{
		"not json":       "hello",
		"other format":   `{"format":"something","version":1,"accounts":{}}`,
		"no version":     `{"format":"agentswap-export","accounts":{}}`,
		"plain no data":  `{"format":"agentswap-export","version":1}`,
		"bad kdf":        `{"format":"agentswap-export","version":1,"kdf":{"name":"md5","iterations":1,"salt":"AAAAAAAAAAA="},"nonce":"AAAAAAAAAAAAAAAA","data":"AA=="}`,
		"huge iteration": `{"format":"agentswap-export","version":1,"kdf":{"name":"pbkdf2-sha256","iterations":2000000000,"salt":"AAAAAAAAAAA="},"nonce":"AAAAAAAAAAAAAAAA","data":"AA=="}`,
		"short nonce":    `{"format":"agentswap-export","version":1,"kdf":{"name":"pbkdf2-sha256","iterations":10,"salt":"AAAAAAAAAAA="},"nonce":"AAAA","data":"AA=="}`,
	}
	for name, s := range cases {
		if _, err := Parse([]byte(s)); !errors.Is(err, ErrFormat) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
	if _, err := Parse([]byte(`{"format":"agentswap-export","version":2}`)); !errors.Is(err, ErrVersion) {
		t.Fatalf("newer version: err = %v", err)
	}
}

func TestSealedHeaderIsReadable(t *testing.T) {
	fastKDF(t)
	data, _ := Seal(sample(), []byte("passphrase"))
	if !strings.Contains(string(data), `"format": "agentswap-export"`) || !strings.Contains(string(data), `"name": "pbkdf2-sha256"`) {
		t.Fatalf("header missing:\n%s", data)
	}
}
