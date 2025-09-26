package rendezvous_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/rendezvous"
)

func TestConcurrentCreateRedeem(t *testing.T) {
	s := rendezvous.NewStore(2 * time.Minute)

	const N = 200

	// ---- Phase 1: collect exactly N unique codes (retrying on rare collisions) ----
	seen := make(map[string]struct{}, N)
	var mu sync.Mutex
	var wg sync.WaitGroup

	producer := func() {
		defer wg.Done()
		for {
			mu.Lock()
			if len(seen) >= N {
				mu.Unlock()
				return
			}
			mu.Unlock()

			code, _, _, err := s.CreateCode(context.Background())
			if err != nil {
				// exhausted (collision), just retry
				continue
			}

			mu.Lock()
			if len(seen) < N {
				if _, dup := seen[code]; !dup {
					seen[code] = struct{}{}
				}
			}
			mu.Unlock()
		}
	}

	workers := 4
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go producer()
	}
	wg.Wait()

	if got := len(seen); got != N {
		t.Fatalf("expected %d unique codes, got %d", N, got)
	}

	// Flatten to slice for deterministic iteration
	keys := make([]string, 0, N)
	for c := range seen {
		keys = append(keys, c)
	}

	// ---- Phase 2: for each code, redeem it twice concurrently and assert 1 ok + 1 fail ----
	var okTotal, failTotal int32
	for _, code := range keys {
		var ok, fail int32
		var inner sync.WaitGroup
		inner.Add(2)

		go func(c string) {
			defer inner.Done()
			if _, _, err := s.Redeem(context.Background(), c); err == nil {
				atomic.AddInt32(&ok, 1)
				atomic.AddInt32(&okTotal, 1)
			} else {
				atomic.AddInt32(&fail, 1)
				atomic.AddInt32(&failTotal, 1)
			}
		}(code)

		go func(c string) {
			defer inner.Done()
			if _, _, err := s.Redeem(context.Background(), c); err == nil {
				atomic.AddInt32(&ok, 1)
				atomic.AddInt32(&okTotal, 1)
			} else {
				atomic.AddInt32(&fail, 1)
				atomic.AddInt32(&failTotal, 1)
			}
		}(code)

		inner.Wait()

		// Per-code invariant: exactly one success and one failure
		if ok != 1 || fail != 1 {
			t.Fatalf("per-code redeem mismatch for %q: ok=%d fail=%d (want 1/1)", code, ok, fail)
		}
	}

	// Global totals should match as well
	if okTotal != N || failTotal != N {
		t.Fatalf("redeem totals mismatch ok=%d fail=%d want=%d each", okTotal, failTotal, N)
	}
}
