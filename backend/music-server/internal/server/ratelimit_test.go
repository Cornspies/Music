package server

import (
	"fmt"
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

func TestSignInDoesNotResetTheSignUpLimit(t *testing.T) {
	c, _ := newTestClient(t)
	signUp := func(name string) int {
		code, _ := c.do("POST", "/api/auth/register", "", map[string]string{"username": name, "password": "limit-password"})
		return code
	}
	signIn := func(name, password string) int {
		code, _ := c.do("POST", "/api/auth/login", "", map[string]string{"username": name, "password": password})
		return code
	}
	for i, name := range []string{"first", "second", "third"} {
		if code := signUp(name); code != http.StatusCreated {
			t.Fatalf("sign-up %d: %d", i+1, code)
		}
		if i < 2 {
			if code := signIn(name, "limit-password"); code != http.StatusOK {
				t.Fatalf("sign-in after sign-up %d: %d", i+1, code)
			}
		}
	}
	if code := signUp("fourth"); code != http.StatusTooManyRequests {
		t.Fatalf("fourth sign-up in the window: %d, want 429", code)
	}
}

func TestSignInStillForgetsWrongPasswords(t *testing.T) {
	c, _ := newTestClient(t)
	c.register("sam", "right-password")
	authLimiter = &limiter{entries: map[string]*attemptRecord{}}
	signIn := func(name, password string) int {
		code, _ := c.do("POST", "/api/auth/login", "", map[string]string{"username": name, "password": password})
		return code
	}
	for round := 0; round < 2; round++ {
		for i := 0; i < authMaxAttempts-1; i++ {
			if code := signIn(fmt.Sprintf("nobody-%d-%d", round, i), "wrong-password"); code != http.StatusUnauthorized {
				t.Fatalf("round %d, wrong attempt %d: %d", round, i+1, code)
			}
		}
		if code := signIn("sam", "right-password"); code != http.StatusOK {
			t.Fatalf("round %d, own sign-in: %d", round, code)
		}
	}
}
