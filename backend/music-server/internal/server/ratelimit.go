package server

import (
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"music-server/internal/config"
)

const (
	authAttemptWindow        = 15 * time.Minute
	authMaxAttempts          = 10
	registrationMaxPerWindow = 3
	authLimiterMaxKeys       = 10000
	accountMaxFailures       = 10
	knownClientTTL           = 30 * 24 * time.Hour
	knownClientRefresh       = time.Hour
)

type attemptRecord struct {
	count         int
	registrations int
	first         time.Time
}

type limiter struct {
	mu      sync.Mutex
	entries map[string]*attemptRecord
}

var (
	authLimiter    = &limiter{entries: map[string]*attemptRecord{}}
	accountLimiter = &limiter{entries: map[string]*attemptRecord{}}
)

func (l *limiter) blocked(key string, over func(*attemptRecord) bool) (bool, time.Duration) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	rec, ok := l.entries[key]
	if !ok || now.Sub(rec.first) > authAttemptWindow || !over(rec) {
		return false, 0
	}
	return true, authAttemptWindow - now.Sub(rec.first)
}

func (l *limiter) touch(key string, fn func(*attemptRecord)) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	rec, ok := l.entries[key]
	if !ok || now.Sub(rec.first) > authAttemptWindow {
		if len(l.entries) >= authLimiterMaxKeys {
			l.evictLocked(now)
		}
		rec = &attemptRecord{first: now}
		l.entries[key] = rec
	}
	fn(rec)
}

func (l *limiter) clear(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

func (l *limiter) evictLocked(now time.Time) {
	for k, v := range l.entries {
		if now.Sub(v.first) > authAttemptWindow {
			delete(l.entries, k)
		}
	}
	for len(l.entries) >= authLimiterMaxKeys*9/10 {
		var oldestKey string
		var oldest time.Time
		for k, v := range l.entries {
			if oldestKey == "" || v.first.Before(oldest) {
				oldestKey, oldest = k, v.first
			}
		}
		if oldestKey == "" {
			return
		}
		delete(l.entries, oldestKey)
	}
}

func rateLimitAuth(r *http.Request) (blocked bool, retryAfter time.Duration) {
	return authLimiter.blocked(limiterKey(r), func(rec *attemptRecord) bool {
		return rec.count >= authMaxAttempts || rec.registrations >= registrationMaxPerWindow
	})
}

func noteAuthFailure(r *http.Request) {
	authLimiter.touch(limiterKey(r), func(rec *attemptRecord) { rec.count++ })
}

func noteRegistration(r *http.Request) {
	authLimiter.touch(limiterKey(r), func(rec *attemptRecord) { rec.registrations++ })
}

func clearAuthFailures(r *http.Request) {
	authLimiter.clear(limiterKey(r))
}

func accountLocked(key string) (bool, time.Duration) {
	return accountLimiter.blocked(key, func(rec *attemptRecord) bool { return rec.count >= accountMaxFailures })
}

func noteAccountFailure(key string) {
	accountLimiter.touch(key, func(rec *attemptRecord) { rec.count++ })
}

func clearAccountFailures(key string) {
	accountLimiter.clear(key)
}

type clientMemory struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

var knownClients = &clientMemory{seen: map[string]time.Time{}}

func knownClientKey(username string, r *http.Request) string {
	return loginKey(strings.ToLower(username)) + "|" + limiterKey(r)
}

func noteKnownClient(username string, r *http.Request) {
	key := knownClientKey(username, r)
	now := time.Now()
	c := knownClients
	c.mu.Lock()
	defer c.mu.Unlock()
	if at, ok := c.seen[key]; ok && now.Sub(at) < knownClientRefresh {
		return
	}
	if len(c.seen) >= authLimiterMaxKeys {
		for k, at := range c.seen {
			if now.Sub(at) > knownClientTTL || len(c.seen) >= authLimiterMaxKeys*9/10 {
				delete(c.seen, k)
			}
		}
	}
	c.seen[key] = now
}

func isKnownClient(username string, r *http.Request) bool {
	key := knownClientKey(username, r)
	c := knownClients
	c.mu.Lock()
	defer c.mu.Unlock()
	at, ok := c.seen[key]
	return ok && time.Since(at) < knownClientTTL
}

func loginKey(username string) string {
	if len(username) > 64 {
		username = username[:64]
	}
	return "name:" + username
}

func accountKey(userID string) string { return "id:" + userID }

func limiterKey(r *http.Request) string {
	host := clientIP(r)
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil || ip.To4() != nil {
		return host
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

func clientIP(r *http.Request) string {
	peer := remoteHost(r.RemoteAddr)
	if !isTrustedProxy(peer) {
		warnUntrustedForwarder(peer, r)
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
	forwardedIgnored   bool
	untrustedWarned    sync.Map
	untrustedWarnings  atomic.Int32
)

const maxUntrustedWarnings = 16

func warnUntrustedForwarder(peer string, r *http.Request) {
	if forwardedIgnored || (r.Header.Get("X-Forwarded-For") == "" && r.Header.Get("X-Real-IP") == "") {
		return
	}
	ip := net.ParseIP(strings.Trim(peer, "[]"))
	if ip == nil || !ip.IsPrivate() {
		return
	}
	if untrustedWarnings.Load() >= maxUntrustedWarnings {
		return
	}
	if _, seen := untrustedWarned.LoadOrStore(peer, true); seen {
		return
	}
	untrustedWarnings.Add(1)
	log.Printf("WARNING sign-in rate limit: ignoring X-Forwarded-For from %s. If that is your reverse proxy, add it to MUSIC_TRUSTED_PROXIES; otherwise everyone behind it shares one limit", peer)
}

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
		forwardedIgnored = true
		return
	}
	if raw == "" {
		raw = "127.0.0.0/8,::1/128"
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
