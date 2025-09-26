// internal/rendezvous/rendezvous_heavy_test.go
package rendezvous_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/rendezvous"
)

func TestRedeemManyContendersPerCode(t *testing.T) {
	s := rendezvous.NewStore(2 * time.Minute)

	const (
		N = 100 // number of codes
		M = 32  // contenders per code
	)

	// produce N unique codes
	keys := make([]string, 0, N)
	seen := map[string]struct{}{}
	for len(keys) < N {
		c, _, _, err := s.CreateCode(context.Background())
		if err != nil {
			continue
		}
		if _, dup := seen[c]; dup {
			continue
		}
		seen[c] = struct{}{}
		keys = append(keys, c)
	}

	var okTotal, failTotal int32

	for _, code := range keys {
		var ok, fail int32
		var inner sync.WaitGroup
		inner.Add(M)

		for i := 0; i < M; i++ {
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
		}

		inner.Wait()

		// ✅ Assertion happens in the test goroutine, not a spawned goroutine.
		if ok != 1 || fail != M-1 {
			t.Fatalf("code %q: ok=%d fail=%d (want 1/%d)", code, ok, fail, M-1)
		}
	}

	if okTotal != int32(N) || failTotal != int32(N*(M-1)) {
		t.Fatalf("totals: ok=%d fail=%d (want %d/%d)", okTotal, failTotal, N, N*(M-1))
	}
}
