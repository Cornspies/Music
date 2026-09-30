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
	// Wrong passwords for one account within authAttemptWindow, from
	// anywhere: someone with many addresses gets past the per-client limit.
	accountMaxFailures = 10
)

type attemptRecord struct {
	count         int
	registrations int
	first         time.Time
}

// limiter counts attempts per key over authAttemptWindow.
type limiter struct {
	mu      sync.Mutex
	entries map[string]*attemptRecord
}

var (
	// Per client; see limiterKey.
	authLimiter = &limiter{entries: map[string]*attemptRecord{}}
	// Per account; see loginKey and accountKey.
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

// accountLocked: the account has had accountMaxFailures wrong passwords
// lately. It's checked before the password, so from then on guesses aren't
// even tried; devices already signed in carry on.
func accountLocked(key string) (bool, time.Duration) {
	return accountLimiter.blocked(key, func(rec *attemptRecord) bool { return rec.count >= accountMaxFailures })
}

func noteAccountFailure(key string) {
	accountLimiter.touch(key, func(rec *attemptRecord) { rec.count++ })
}

func clearAccountFailures(key string) {
	accountLimiter.clear(key)
}

// loginKey counts sign-ins by the name tried, whether or not such an account
// exists, so the limit doesn't give away which names do.
func loginKey(username string) string {
	if len(username) > 64 {
		username = username[:64]
	}
	return "name:" + username
}

// accountKey counts password confirmations by a signed-in account.
func accountKey(userID string) string { return "id:" + userID }

// limiterKey is the client attempts count against: its address, or for IPv6
// its /64, since one connection usually has a whole /64 to hop around in.
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
