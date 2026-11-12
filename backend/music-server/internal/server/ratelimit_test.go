package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIPTrustsForwardedForOnlyFromLoopback(t *testing.T) {
	t.Setenv("MUSIC_TRUSTED_PROXIES", "")
	cases := []struct {
		peer, forwarded, want string
	}{
		{"127.0.0.1:5000", "203.0.113.9", "203.0.113.9"},
		{"[::1]:5000", "203.0.113.9", "203.0.113.9"},
		{"127.0.0.1:5000", "198.51.100.1, 203.0.113.9", "203.0.113.9"},
		{"192.168.1.20:5000", "203.0.113.9", "192.168.1.20"},
		{"10.0.0.7:5000", "203.0.113.9", "10.0.0.7"},
		{"172.18.0.2:5000", "203.0.113.9", "172.18.0.2"},
		{"198.51.100.4:5000", "203.0.113.9", "198.51.100.4"},
		{"127.0.0.1:5000", "", "127.0.0.1"},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
		r.RemoteAddr = c.peer
		if c.forwarded != "" {
			r.Header.Set("X-Forwarded-For", c.forwarded)
		}
		if got := clientIP(r); got != c.want {
			t.Errorf("peer %s, X-Forwarded-For %q: client = %s, want %s", c.peer, c.forwarded, got, c.want)
		}
	}
}
