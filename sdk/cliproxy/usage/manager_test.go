package usage

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestManagerStopWaitDrainsQueue(t *testing.T) {
	mgr := NewManager(4)
	var handled int64
	mgr.Register(pluginFunc(func(context.Context, Record) {
		atomic.AddInt64(&handled, 1)
	}))

	for i := 0; i < 3; i++ {
		mgr.Publish(context.Background(), Record{Model: "gpt-5"})
	}
	mgr.Stop()
	if !mgr.Wait(time.Second) {
		t.Fatalf("Wait timed out")
	}
	if got := atomic.LoadInt64(&handled); got != 3 {
		t.Fatalf("handled = %d, want 3", got)
	}
}

type pluginFunc func(context.Context, Record)

func (fn pluginFunc) HandleUsage(ctx context.Context, record Record) {
	fn(ctx, record)
}
