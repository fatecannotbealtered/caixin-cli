package caixin

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/chromedp/chromedp"
)

// Signature is the pair of headers the full-text endpoint requires.
type Signature struct {
	Sign  string
	Nonce string
}

// ErrNoSigningKey means --full was asked for and neither a stored signing key
// nor a browser to extract one was available. It is deliberately fatal: a
// caller who asked for the full article must never be handed the excerpt
// without being told.
var ErrNoSigningKey = fmt.Errorf(
	"full text needs the request signing key, and none was found; set " +
		signingKeyEnv + ", place it at <state-dir>/signing-key.pem, or run once " +
		"on a machine with Chrome or Edge (or point CAIXIN_BROWSER_WS at a " +
		"running browser) to extract and cache it")

// ErrNoBrowser is kept as an alias so callers that classify this failure keep
// working; the condition they care about is unchanged.
var ErrNoBrowser = ErrNoSigningKey

// Signer mints the X-Sign / X-Nonce pair for one article.
//
// The signature is computed here, in Go: it is an RSASSA-PKCS1-v1_5 SHA-256
// signature over "id=<id>&uid=<uid>&<nonce>=nonce", verified byte-for-byte
// against the browser's own createSign output (see docs/FULL-TEXT.md). So the
// normal path needs no browser at all.
//
// A browser is used only once, to lift the key out of Caixin's bundle on a
// machine that has one. After that the key is cached in the state directory and
// every later run is plain HTTP.
type Signer struct {
	stateDir string
	timeout  time.Duration
	// RemoteWS points at an already-running Chrome's DevTools websocket, for
	// bootstrapping on hosts with no local browser.
	//
	// Note what this implies: the extraction loads an article page with the
	// account's session, so a remote browser receives those cookies. Point this
	// at your own sidecar, never a shared endpoint. Supplying the key through
	// the environment avoids the question entirely.
	RemoteWS string

	// keySource records where the key came from, so `context` can report the
	// backend and a degraded setup stays visible.
	keySource signingKeySource
}

func NewSigner(stateDir string, timeout time.Duration) *Signer {
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	return &Signer{
		stateDir:  stateDir,
		timeout:   timeout,
		RemoteWS:  os.Getenv("CAIXIN_BROWSER_WS"),
		keySource: signingKeyAbsent,
	}
}

// KeySource names the backend the last Sign used: env, file, browser, or none.
func (s *Signer) KeySource() string { return string(s.keySource) }

// Sign returns the headers for one article, without a browser whenever a key is
// already available.
func (s *Signer) Sign(ctx context.Context, articleURL, articleID string, cookies []*http.Cookie) (*Signature, error) {
	uid := sessionUID(cookies)
	if uid == "" {
		return nil, &APIError{
			StatusCode: 401,
			Message: "the full body is signed against your account id, and this " +
				"session carries none; run `caixin-cli login` first",
		}
	}

	if key, source := loadSigningKey(s.stateDir); key != nil {
		s.keySource = source
		return signLocally(key, articleID, uid)
	}

	key, signature, err := s.bootstrapFromBrowser(ctx, articleURL, articleID, cookies)
	if err != nil {
		return nil, err
	}
	s.keySource = signingKeyFromBrowser
	if key != nil {
		// Best effort: a cache that cannot be written costs a browser next time,
		// which is not worth failing a successful read over.
		_ = saveSigningKey(s.stateDir, key)
		return signLocally(key, articleID, uid)
	}
	// The bundle changed shape and the key could not be lifted, but its own
	// createSign still answered. Use that for this run rather than failing.
	if signature == nil {
		return nil, ErrNoSigningKey
	}
	return signature, nil
}

// bootstrapJS lifts the signing key out of the page and, as a fallback, the
// signature itself.
//
// The key is assembled at runtime from an obfuscated string table rather than
// stored as a literal, so it is captured where the pieces are joined. That hook
// is tied to how the bundle is built and may stop matching; the signature it
// returns alongside keeps --full working for the run if it does.
const bootstrapJS = `(() => {
  if (typeof window.createSign !== 'function') return null;
  let captured = '';
  const original = String.prototype.concat;
  String.prototype.concat = function () {
    const out = original.apply(this, arguments);
    if (typeof out === 'string' && out.indexOf('PRIVATE KEY') >= 0 && !captured) {
      captured = out;
    }
    return out;
  };
  let headers = null;
  try { headers = window.createSign(%q); } catch (e) { headers = null; }
  String.prototype.concat = original;
  return JSON.stringify({
    key: captured,
    sign: headers && headers['X-Sign'] ? headers['X-Sign'] : '',
    nonce: headers && headers['X-Nonce'] ? headers['X-Nonce'] : ''
  });
})()`

type bootstrapResult struct {
	Key   string `json:"key"`
	Sign  string `json:"sign"`
	Nonce string `json:"nonce"`
}

// bootstrapFromBrowser loads the article page in a browser carrying the session
// and returns the key it found, the signature its own script produced, or both.
func (s *Signer) bootstrapFromBrowser(ctx context.Context, articleURL, articleID string, cookies []*http.Cookie) (*rsa.PrivateKey, *Signature, error) {
	var allocCtx context.Context
	var cancelAlloc context.CancelFunc
	if s.RemoteWS != "" {
		allocCtx, cancelAlloc = chromedp.NewRemoteAllocator(ctx, s.RemoteWS)
	} else {
		browser := FindBrowser()
		if browser == "" {
			return nil, nil, ErrNoSigningKey
		}
		allocCtx, cancelAlloc = chromedp.NewExecAllocator(ctx,
			append(chromedp.DefaultExecAllocatorOptions[:],
				chromedp.ExecPath(browser),
				chromedp.Flag("headless", true),
				chromedp.Flag("disable-gpu", true),
				chromedp.Flag("no-sandbox", true),
				chromedp.Flag("blink-settings", "imagesEnabled=false"),
			)...)
	}
	defer cancelAlloc()

	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()

	// Overall budget covers navigation plus the wait for the signing bundle.
	timedCtx, cancelTimeout := context.WithTimeout(browserCtx, s.timeout+60*time.Second)
	defer cancelTimeout()

	if err := chromedp.Run(timedCtx,
		setCookies(cookies),
		chromedp.Navigate(articleURL),
	); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrNoSigningKey, err)
	}

	// The signing function arrives with a bundle that loads well after the
	// document -- measured at up to ~40s on a cold cache. Poll for it instead
	// of assuming a fixed wait, so a fast machine is not made to sit and a slow
	// one is not cut off early.
	deadline := time.Now().Add(s.timeout)
	for {
		var ready bool
		if err := chromedp.Run(timedCtx,
			chromedp.Evaluate(`typeof window.createSign === 'function'`, &ready),
		); err != nil {
			return nil, nil, fmt.Errorf("%w: %v", ErrNoSigningKey, err)
		}
		if ready {
			break
		}
		if time.Now().After(deadline) {
			return nil, nil, fmt.Errorf(
				"the article page never exposed its signing function within %s", s.timeout)
		}
		if err := chromedp.Run(timedCtx, chromedp.Sleep(time.Second)); err != nil {
			return nil, nil, fmt.Errorf("%w: %v", ErrNoSigningKey, err)
		}
	}

	var raw string
	if err := chromedp.Run(timedCtx,
		chromedp.Evaluate(fmt.Sprintf(bootstrapJS, articleID), &raw),
	); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrNoSigningKey, err)
	}
	var result bootstrapResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, nil, fmt.Errorf("the page did not produce a signature for %s", articleID)
	}

	var key *rsa.PrivateKey
	if result.Key != "" {
		if parsed, err := parseSigningKey(result.Key); err == nil {
			key = parsed
		}
	}
	var signature *Signature
	if result.Sign != "" && result.Nonce != "" {
		signature = &Signature{Sign: result.Sign, Nonce: result.Nonce}
	}
	if key == nil && signature == nil {
		return nil, nil, fmt.Errorf("the page did not produce a signature for %s", articleID)
	}
	return key, signature, nil
}
