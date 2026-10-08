package claude

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRefreshDoesNotFollowRedirects(t *testing.T) {
	secret := "REFRESH-SECRET"
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		var leaked atomic.Bool
		receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			leaked.Store(bytes.Contains(b, []byte(secret)))
			io.WriteString(w, `{"access_token":"new","refresh_token":"new","expires_in":3600}`)
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
		snap := Pack([]byte(`{"claudeAiOauth":{"accessToken":"old","refreshToken":"`+secret+`","expiresAt":1}}`), []byte(`{"accountUuid":"u"}`))
		_, _, err := p.Usage(context.Background(), snap, false)
		origin.Close()
		receiver.Close()
		if leaked.Load() || err == nil {
			t.Fatalf("%d: leaked=%v err=%v", status, leaked.Load(), err)
		}
	}
}
