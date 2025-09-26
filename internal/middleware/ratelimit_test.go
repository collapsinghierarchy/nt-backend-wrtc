// internal/middleware/ratelimit_test.go
package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/middleware"
)

func TestRateLimitHTTP(t *testing.T) {
	// fresh, instance-based limiter: 1 request/min per client
	rl := middleware.New(1)

	// Wrap a simple handler
	h := rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Use a fixed client key (X-Forwarded-For) so both requests count to the same bucket
	const client = "203.0.113.9"

	// First request -> allowed (200)
	rr1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.Header.Set("X-Forwarded-For", client)
	h.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first req code %d", rr1.Code)
	}

	// Second request (same client) -> limited (429)
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("X-Forwarded-For", client)
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("second req should be 429, got %d", rr2.Code)
	}
}

// Optional: prove that perMin <= 0 disables limiting.
func TestRateLimitHTTP_Disabled(t *testing.T) {
	rl := middleware.New(0) // disabled
	h := rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first req code %d", rr1.Code)
	}

	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("second req should be 200 when disabled, got %d", rr2.Code)
	}
}

// Optional: exercise AllowWS directly (pre-upgrade gate).
func TestAllowWS(t *testing.T) {
	rl := middleware.New(1)

	// Same client twice -> first allowed, second blocked
	req1 := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req1.Header.Set("X-Forwarded-For", "198.51.100.7")
	if !rl.AllowWS(req1) {
		t.Fatalf("first WS attempt should be allowed")
	}
	req2 := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req2.Header.Set("X-Forwarded-For", "198.51.100.7")
	if rl.AllowWS(req2) {
		t.Fatalf("second WS attempt should be rate-limited")
	}
}
