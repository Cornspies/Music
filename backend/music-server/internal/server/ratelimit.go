package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"music-server/internal/config"
)

// Credential endpoints are the one place an attacker gets unlimited free
// guesses, so they get a small in-memory limiter. It is per-process and resets
// on restart, which is fine for a self-hosted instance — the goal is to turn
// "millions of guesses per hour" into "a handful per minute", not to be a WAF.
const (
	authAttemptWindow = 15 * time.Minute
	authMaxAttempts   = 10
	// Successful sign-ups count too, with their own (lower) ceiling: an open
	// server should not let one address script thousands of accounts.
	registrationMaxPerWindow = 3
	// Hard cap on tracked addresses. Past this the oldest entries are
	// evicted, so someone cycling through spoofed addresses can't grow the
	// map without bound.
	authLimiterMaxKeys = 10000
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

// rateLimitAuth reports whether this request should be rejected, and how long
// the caller should wait. Only failed attempts count: a successful login
// clears the record, so normal use never trips it.
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

// noteRegistration records a successful sign-up against the address.
func noteRegistration(r *http.Request) {
	touch(r, func(rec *attemptRecord) { rec.registrations++ })
}

// touch finds or creates the record for the caller's address and applies fn
// to it, evicting stale and (if needed) oldest entries first.
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

// evictLocked drops expired entries, and if that isn't enough, the oldest
// ones until the map is comfortably under the cap. Caller holds the lock.
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

// clientIP works out who is really calling.
//
// X-Forwarded-For is only believed when the direct peer is a trusted proxy
// (MUSIC_TRUSTED_PROXIES, default: loopback and private ranges — where a
// reverse proxy on the same box or LAN lives). Even then the header is read
// from the right: each proxy appends the address it saw, so the right-most
// entry that isn't itself a trusted proxy is the client, and anything the
// client put in the header itself is further left and ignored. Set
// MUSIC_TRUSTED_PROXIES to an empty value ("none") to ignore the header
// entirely when the server is exposed directly.
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

// forwardedChain returns the hops from X-Forwarded-For (left to right), or
// X-Real-IP as a single entry when only that is present.
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

// isTrustedProxy reports whether an address belongs to a proxy whose
// forwarding headers we accept.
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

// loadTrustedProxies parses MUSIC_TRUSTED_PROXIES: a comma-separated list of
// IPs or CIDRs, "none" to trust nobody, or unset for the default set.
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
