package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Credential endpoints are the one place an attacker gets unlimited free
// guesses, so they get a small in-memory limiter. It is per-process and resets
// on restart, which is fine for a self-hosted instance — the goal is to turn
// "millions of guesses per hour" into "a handful per minute", not to be a WAF.
const (
	authAttemptWindow = 15 * time.Minute
	authMaxAttempts   = 10
	// Entries are dropped once they fall outside the window; this caps memory
	// if someone cycles through many source addresses.
	authLimiterMaxKeys = 10000
)

type attemptRecord struct {
	count int
	first time.Time
}

var authLimiter = struct {
	mu      sync.Mutex
	entries map[string]*attemptRecord
}{entries: map[string]*attemptRecord{}}

// rateLimitAuth reports whether this request should be rejected, and how long
// the caller should wait. Only failed attempts count: a successful login
// clears the record, so normal use never trips it.
func rateLimitAuth(r *http.Request) (blocked bool, retryAfter time.Duration) {
	key := clientIP(r)
	now := time.Now()

	authLimiter.mu.Lock()
	defer authLimiter.mu.Unlock()

	if len(authLimiter.entries) > authLimiterMaxKeys {
		for k, v := range authLimiter.entries {
			if now.Sub(v.first) > authAttemptWindow {
				delete(authLimiter.entries, k)
			}
		}
	}

	rec, ok := authLimiter.entries[key]
	if !ok || now.Sub(rec.first) > authAttemptWindow {
		return false, 0
	}
	if rec.count >= authMaxAttempts {
		return true, authAttemptWindow - now.Sub(rec.first)
	}
	return false, 0
}

func noteAuthFailure(r *http.Request) {
	key := clientIP(r)
	now := time.Now()

	authLimiter.mu.Lock()
	defer authLimiter.mu.Unlock()

	rec, ok := authLimiter.entries[key]
	if !ok || now.Sub(rec.first) > authAttemptWindow {
		authLimiter.entries[key] = &attemptRecord{count: 1, first: now}
		return
	}
	rec.count++
}

func clearAuthFailures(r *http.Request) {
	authLimiter.mu.Lock()
	defer authLimiter.mu.Unlock()
	delete(authLimiter.entries, clientIP(r))
}

// clientIP prefers X-Forwarded-For, since this server is expected to sit
// behind a reverse proxy that terminates TLS. Only the left-most entry is
// used. Note that this header is trivially spoofable if the server is exposed
// directly — don't do that.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first, _, ok := strings.Cut(xff, ","); ok {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(xff)
	}
	if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); xr != "" {
		return xr
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
