package swap

import (
	"net"
	"net/http"
	"net/url"
)

func NoRedirect(c *http.Client) *http.Client {
	n := *c
	n.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &n
}

func SameOrigin(c *http.Client) *http.Client {
	n := *c
	n.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 || req.URL.Scheme != via[0].URL.Scheme || req.URL.Host != via[0].URL.Host {
			return http.ErrUseLastResponse
		}
		return nil
	}
	return &n
}

func SecureURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		h := u.Hostname()
		if h == "localhost" {
			return true
		}
		ip := net.ParseIP(h)
		return ip != nil && ip.IsLoopback()
	}
	return false
}
