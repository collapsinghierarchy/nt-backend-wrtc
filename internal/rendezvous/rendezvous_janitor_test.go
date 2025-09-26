package rendezvous

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Verifies: after TTL passes and the sweep runs, old codes are gone (Redeem => errGone).
func TestJanitorSweepRemovesExpired(t *testing.T) {
	ttl := 30 * time.Millisecond
	s := NewStore(ttl)

	// Create a bunch of codes and remember them
	const N = 50
	codes := make([]string, 0, N)
	for i := 0; i < N; i++ {
		code, _, _, err := s.CreateCode(context.Background())
		if err != nil {
			t.Fatalf("CreateCode: %v", err)
		}
		codes = append(codes, code)
	}

	// Wait past TTL so they are considered expired
	time.Sleep(ttl + 20*time.Millisecond)

	// Run a manual sweep (instead of waiting for the 1-minute janitor tick)
	s.sweep(time.Now())

	// All codes should now be gone (Redeem returns errGone)
	for _, c := range codes {
		if _, _, err := s.Redeem(context.Background(), c); !errors.Is(err, errGone) {
			t.Fatalf("expected errGone for code %q after sweep, got %v", c, err)
		}
	}
}

// Verifies: expired slots are reclaimed by CreateCode (fresh codes keep coming).
func TestCreateCodeReclaimsExpiredSlots(t *testing.T) {
	ttl := 25 * time.Millisecond
	s := NewStore(ttl)

	// Fill several codes then let them expire
	const N = 100
	for i := 0; i < N; i++ {
		if _, _, _, err := s.CreateCode(context.Background()); err != nil {
			t.Fatalf("CreateCode: %v", err)
		}
	}
	time.Sleep(ttl + 20*time.Millisecond)
	s.sweep(time.Now()) // reclaim

	// Should be able to create N more without exhaustion
	for i := 0; i < N; i++ {
		if _, _, _, err := s.CreateCode(context.Background()); err != nil {
			t.Fatalf("CreateCode after expiry: %v", err)
		}
	}
}
