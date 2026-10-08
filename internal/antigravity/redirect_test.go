package antigravity

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRefreshDoesNotFollowRedirects(t *testing.T) {
	secret := "REFRESH-SECRET"
	idToken := "e30." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"u"}`)) + ".x"
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		var leaked atomic.Bool
		receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			leaked.Store(bytes.Contains(b, []byte(secret)))
			io.WriteString(w, `{"access_token":"new","expires_in":3600}`)
		}))
		target := strings.Replace(receiver.URL, "127.0.0.1", "localhost", 1)
		origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/token" {
				http.Redirect(w, r, target, status)
				return
			}
			io.WriteString(w, `{}`)
		}))
		p := Provider{BaseURL: origin.URL, RefreshURL: origin.URL + "/token"}
		snap := []byte(`{"token":{"access_token":"old","refresh_token":"` + secret + `","expiry":"2020-01-01T00:00:00Z"},"id_token":"` + idToken + `"}`)
		_, _, err := p.Usage(context.Background(), snap, false)
		origin.Close()
		receiver.Close()
		if leaked.Load() || err == nil {
			t.Fatalf("%d: leaked=%v err=%v", status, leaked.Load(), err)
		}
	}
}
