package swap

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSameOriginRefusesSchemeAndHostChanges(t *testing.T) {
	var leaked string
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("Authorization")
	}))
	defer plain.Close()
	other := strings.Replace(plain.URL, "127.0.0.1", "localhost", 1)
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/downgrade":
			http.Redirect(w, r, plain.URL+"/x", http.StatusFound)
		case "/same":
			http.Redirect(w, r, "/final", http.StatusFound)
		case "/final":
			w.Write([]byte("ok"))
		}
	}))
	defer secure.Close()
	hop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other+"/x", http.StatusFound)
	}))
	defer hop.Close()

	get := func(c *http.Client, url string) int {
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer SECRET")
		resp, err := SameOrigin(c).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := get(secure.Client(), secure.URL+"/downgrade"); code != http.StatusFound || leaked != "" {
		t.Fatalf("https to http: status %d, leaked %q", code, leaked)
	}
	if code := get(hop.Client(), hop.URL+"/a"); code != http.StatusFound || leaked != "" {
		t.Fatalf("cross host: status %d, leaked %q", code, leaked)
	}
	if code := get(secure.Client(), secure.URL+"/same"); code != http.StatusOK {
		t.Fatalf("same origin redirect: status %d", code)
	}
}

func TestSecureURL(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://chatgpt.com/backend-api": true,
		"http://127.0.0.1:1455/x":         true,
		"http://127.8.9.1/x":              true,
		"http://localhost:80":             true,
		"http://[::1]/x":                  true,
		"http://chatgpt.com/backend-api":  false,
		"http://localhost.evil.example/":  false,
		"http://127.0.0.1.nip.io/":        false,
		"ftp://example.com/":              false,
		"https:///nohost":                 false,
		"chatgpt.com/backend-api":         false,
		"":                                false,
	} {
		if got := SecureURL(raw); got != want {
			t.Errorf("SecureURL(%q) = %v, want %v", raw, got, want)
		}
	}
}
