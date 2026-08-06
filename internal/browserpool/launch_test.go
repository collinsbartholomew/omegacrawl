package browserpool

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestPoolBrowserCtxStaysAlive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts, chromedp.Flag("headless", true))
	p := New(ctx, 1, opts, "")
	if err := p.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer p.Close()

	bctx, err := p.Acquire()
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if bctx.Err() != nil {
			t.Fatalf("browserCtx cancelled: %v", bctx.Err())
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Log("browserCtx stayed alive")
}
