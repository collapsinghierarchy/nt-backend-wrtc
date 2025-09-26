package hub_test

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/hub"
)

func TestConcurrentEnqueueAckHello(t *testing.T) {
	h := hub.New()
	app := "app1"

	const N = 1000
	var wg sync.WaitGroup

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			msgAB := json.RawMessage([]byte(fmt.Sprintf(`{"n":%d}`, i)))
			msgBA := json.RawMessage([]byte(`{"m":"x"}`))
			_ = h.Enqueue(app, "A", "B", msgAB)
			_ = h.Enqueue(app, "B", "A", msgBA)
		}(i)
	}
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h.AckUpTo(app, "A", uint64(i))
			h.AckUpTo(app, "B", uint64(i))
		}(i)
	}
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h.Hello(app, "A", "", uint64(i))
			h.Hello(app, "B", "", uint64(i))
		}(i)
	}
	wg.Wait()
}

func TestMarkEstablishedOnce(t *testing.T) {
	h := hub.New()
	app := "app2"

	// Ensure room exists (create via Enqueue).
	_ = h.Enqueue(app, "A", "B", json.RawMessage(`{"x":1}`))

	var wins int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := h.MarkEstablished(app); ok {
				atomic.AddInt32(&wins, 1)
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("MarkEstablished should return ok exactly once; got %d", wins)
	}
}
