package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"music-server/internal/config"
)

const (
	authAttemptWindow        = 15 * time.Minute
	authMaxAttempts          = 10
	registrationMaxPerWindow = 3
	authLimiterMaxKeys       = 10000
)

type attemptRecord struct {
	count         int
	registrations int
	first         time.Time
}

var authLimiter = struct {
	mu      sync.Mutex
	entries map[string]*attemptRecord
}{entries: map[string]*attemptRecord{}}

func rateLimitAuth(r *http.Request) (blocked bool, retryAfter time.Duration) {
	key := clientIP(r)
	now := time.Now()

	authLimiter.mu.Lock()
	defer authLimiter.mu.Unlock()

	rec, ok := authLimiter.entries[key]
	if !ok || now.Sub(rec.first) > authAttemptWindow {
		return false, 0
	}
	if rec.count >= authMaxAttempts || rec.registrations >= registrationMaxPerWindow {
		return true, authAttemptWindow - now.Sub(rec.first)
	}
	return false, 0
}

func noteAuthFailure(r *http.Request) {
	touch(r, func(rec *attemptRecord) { rec.count++ })
}

func noteRegistration(r *http.Request) {
	touch(r, func(rec *attemptRecord) { rec.registrations++ })
}

func touch(r *http.Request, fn func(*attemptRecord)) {
	key := clientIP(r)
	now := time.Now()

	authLimiter.mu.Lock()
	defer authLimiter.mu.Unlock()

	rec, ok := authLimiter.entries[key]
	if !ok || now.Sub(rec.first) > authAttemptWindow {
		if len(authLimiter.entries) >= authLimiterMaxKeys {
			evictLocked(now)
		}
		rec = &attemptRecord{first: now}
		authLimiter.entries[key] = rec
	}
	fn(rec)
}

func evictLocked(now time.Time) {
	for k, v := range authLimiter.entries {
		if now.Sub(v.first) > authAttemptWindow {
			delete(authLimiter.entries, k)
		}
	}
	for len(authLimiter.entries) >= authLimiterMaxKeys*9/10 {
		var oldestKey string
		var oldest time.Time
		for k, v := range authLimiter.entries {
			if oldestKey == "" || v.first.Before(oldest) {
				oldestKey, oldest = k, v.first
			}
		}
		if oldestKey == "" {
			return
		}
		delete(authLimiter.entries, oldestKey)
	}
}

func clearAuthFailures(r *http.Request) {
	authLimiter.mu.Lock()
	defer authLimiter.mu.Unlock()
	delete(authLimiter.entries, clientIP(r))
}

func clientIP(r *http.Request) string {
	peer := remoteHost(r.RemoteAddr)
	if !isTrustedProxy(peer) {
		return peer
	}
	candidates := forwardedChain(r)
	for i := len(candidates) - 1; i >= 0; i-- {
		if !isTrustedProxy(candidates[i]) {
			return candidates[i]
		}
	}
	return peer
}

func forwardedChain(r *http.Request) []string {
	var out []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		for _, part := range strings.Split(v, ",") {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
	}
	if len(out) == 0 {
		if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); xr != "" {
			out = append(out, xr)
		}
	}
	return out
}

func remoteHost(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

var (
	trustedProxiesOnce sync.Once
	trustedProxies     []*net.IPNet
)

func isTrustedProxy(host string) bool {
	trustedProxiesOnce.Do(loadTrustedProxies)
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return false
	}
	for _, n := range trustedProxies {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func loadTrustedProxies() {
	raw := strings.TrimSpace(config.Value("MUSIC_TRUSTED_PROXIES"))
	if strings.EqualFold(raw, "none") {
		return
	}
	if raw == "" {
		raw = "127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7"
	}
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if !strings.Contains(entry, "/") {
			if strings.Contains(entry, ":") {
				entry += "/128"
			} else {
				entry += "/32"
			}
		}
		if _, n, err := net.ParseCIDR(entry); err == nil {
			trustedProxies = append(trustedProxies, n)
		} else {
			logErr("trusted proxies", err)
		}
	}
}
