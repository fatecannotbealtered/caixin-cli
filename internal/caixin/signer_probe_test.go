package caixin

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestSignerProbe is a diagnostic, not part of the suite: it drives a real
// browser against the live site. Run it explicitly with
//
//	go test ./internal/caixin -run TestSignerProbe -v -tags probe
//
// It stays skipped by default so `go test ./...` never reaches the network.
func TestSignerProbe(t *testing.T) {
	// Off unless asked for: `go test ./...` must never reach the network, or CI
	// becomes a flaky uptime monitor for someone else's site.
	if os.Getenv("CAIXIN_LIVE_PROBE") == "" {
		t.Skip("set CAIXIN_LIVE_PROBE=1 to run the live browser probe")
	}
	browser := FindBrowser()
	if browser == "" {
		t.Skip("no browser installed")
	}
	t.Logf("browser: %s", browser)

	options := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(browser),
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
	)
	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), options...)
	defer cancelAlloc()
	browserCtx, cancelBrowser := chromedp.NewContext(alloc)
	defer cancelBrowser()
	ctx, cancelTimeout := context.WithTimeout(browserCtx, 90*time.Second)
	defer cancelTimeout()

	var title string
	var hasSign bool
	started := time.Now()
	err := chromedp.Run(ctx,
		chromedp.Navigate("https://www.caixin.com/2026-08-07/102472114.html"),
		chromedp.Sleep(10*time.Second),
		chromedp.Title(&title),
		chromedp.Evaluate(`typeof window.createSign === 'function'`, &hasSign),
	)
	t.Logf("elapsed=%v err=%v", time.Since(started).Round(time.Second), err)
	t.Logf("title=%q createSign=%v", title, hasSign)
}
