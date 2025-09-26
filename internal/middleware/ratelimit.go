package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Limiter implements a fixed-window per-minute limit per client key (usually IP).
type Limiter struct {
	perMin int

	mu sync.Mutex
	m  map[string]*bucket
}

type bucket struct {
	count int
	reset time.Time
}

// New returns a limiter allowing at most perMin requests per key per minute.
// perMin <= 0 disables limiting (always allow).
func New(perMin int) *Limiter {
	return &Limiter{
		perMin: perMin,
		m:      make(map[string]*bucket),
	}
}

// Allow reports whether a request for the given key is allowed right now.
func (l *Limiter) Allow(key string) bool {
	if l == nil || l.perMin <= 0 {
		return true
	}
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	b := l.m[key]
	if b == nil || now.After(b.reset) {
		b = &bucket{count: 0, reset: now.Add(time.Minute)}
		l.m[key] = b
	}
	if b.count >= l.perMin {
		return false
	}
	b.count++
	return true
}

// Middleware wraps an http.Handler with this limiter.
// Key is derived from the request via KeyFromRequest.
func (l *Limiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !l.Allow(KeyFromRequest(r)) {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte("rate limit"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AllowWS checks allowance for a WebSocket upgrade request (use before Upgrader.Upgrade).
func (l *Limiter) AllowWS(r *http.Request) bool {
	return l.Allow(KeyFromRequest(r))
}

// KeyFromRequest extracts a best-effort client key from the request.
// Prefers the first X-Forwarded-For entry (if present), else RemoteAddr host.
func KeyFromRequest(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// take the left-most (client) IP
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
