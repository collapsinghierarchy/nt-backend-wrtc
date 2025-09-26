// internal/rendezvous/rendezvous_expiry_test.go
package rendezvous_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/rendezvous"
)

func TestRedeemAfterExpiryUnderLoad(t *testing.T) {
	s := rendezvous.NewStore(50 * time.Millisecond)

	const N = 200
	keys := make([]string, 0, N)
	seen := map[string]struct{}{}
	for len(keys) < N {
		c, _, _, err := s.CreateCode(context.Background())
		if err == nil {
			if _, dup := seen[c]; !dup {
				seen[c] = struct{}{}
				keys = append(keys, c)
			}
		}
	}

	// expire them all
	time.Sleep(100 * time.Millisecond)

	var ok, fail int32
	var wg sync.WaitGroup
	wg.Add(N)
	for _, code := range keys {
		code := code
		go func() {
			defer wg.Done()
			// 4 contenders per code after expiry
			var inner sync.WaitGroup
			inner.Add(4)
			for i := 0; i < 4; i++ {
				go func() {
					defer inner.Done()
					if _, _, err := s.Redeem(context.Background(), code); err == nil {
						atomic.AddInt32(&ok, 1)
					} else {
						atomic.AddInt32(&fail, 1)
					}
				}()
			}
			inner.Wait()
		}()
	}
	wg.Wait()

	if ok != 0 || fail != N*4 {
		t.Fatalf("expiry totals: ok=%d fail=%d (want 0/%d)", ok, fail, N*4)
	}
}
