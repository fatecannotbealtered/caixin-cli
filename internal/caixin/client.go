package caixin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// UserAgent identifies this client honestly rather than impersonating a
	// browser build we are not.
	UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) " +
		"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36"

	// maxResponseBytes bounds every response so a broken or hostile upstream
	// cannot exhaust memory.
	maxResponseBytes = 8 << 20

	// minRequestInterval throttles this client to a courteous rate. Caixin is
	// someone else's service; the tool is for reading, not for scraping hard.
	minRequestInterval = 500 * time.Millisecond
)

// APIError carries the upstream failure in structured form. The status is a
// field, never interpolated into the message, because CLI-SPEC §6 derives the
// error code from the status and message sniffing misclassifies bodies that
// merely contain words like "not found".
type APIError struct {
	StatusCode int
	Method     string
	URL        string
	Message    string
	Code       any
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("%s %s failed", e.Method, e.URL)
}

// ValidationError is a caller mistake: a bad flag value, an unknown category,
// a url off the allowlist.
//
// It is deliberately a separate type from APIError. Both used to be the same
// thing, which made every bad argument surface as E_SERVER -- retryable, exit
// 7 -- so an agent that mistyped --size would back off and retry forever
// instead of fixing the call.
type ValidationError struct {
	Message string
	Details map[string]any
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func invalid(message string) error { return &ValidationError{Message: message} }

// Client is a read-only Caixin web client.
type Client struct {
	http     *http.Client
	jar      *cookiejar.Jar
	stateDir string
	baseHost string

	throttleMu sync.Mutex
	lastCall   time.Time

	callbackMu sync.Mutex
	callbackID int
}

// nextCallback yields a deterministic JSONP callback id.
//
// Deriving it from the wall clock is the obvious choice and the wrong one:
// upstream echoes the id back and the client then matches it exactly, so two
// identical invocations would produce different requests and no recorded
// response could ever be replayed. A per-client counter keeps the handshake
// unique per call while staying reproducible.
func (c *Client) nextCallback() int {
	c.callbackMu.Lock()
	defer c.callbackMu.Unlock()
	c.callbackID++
	return c.callbackID
}

// jsonUnmarshal is a thin alias so the JSONP path reads the same as the rest.
func jsonUnmarshal(data []byte, target any) error { return json.Unmarshal(data, target) }

// Options configures a Client.
type Options struct {
	StateDir string
	Timeout  time.Duration
	// BaseHost rewrites every upstream host to this origin. Production leaves
	// it empty; it exists so contract tests can drive the real transport
	// against a mock upstream instead of stubbing the client out.
	BaseHost string
}

// StateDir resolves the session directory: flag, then CAIXIN_STATE_DIR, then
// the per-user default.
func StateDir(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if env := os.Getenv("CAIXIN_STATE_DIR"); env != "" {
		return env, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".caixin-fetch"), nil
}

func New(options Options) (*Client, error) {
	stateDir, err := StateDir(options.StateDir)
	if err != nil {
		return nil, err
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	loadCookies(jar, stateDir)

	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return &Client{
		http: &http.Client{
			Jar:     jar,
			Timeout: timeout,
			// Proxy is deliberately unset: the session cookie is a paid-account
			// credential and must not follow an inherited proxy.
			Transport: &http.Transport{},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				// Redirects are validated per hop by the callers that allow
				// them; the transport never follows one silently.
				return http.ErrUseLastResponse
			},
		},
		jar:      jar,
		stateDir: stateDir,
		baseHost: strings.TrimSuffix(options.BaseHost, "/"),
	}, nil
}

// StateDirectory reports where the session is persisted.
func (c *Client) StateDirectory() string { return c.stateDir }

// Authenticated reports whether a session cookie is loaded, without touching
// the network.
func (c *Client) Authenticated() bool {
	parsed, err := url.Parse("https://www.caixin.com/")
	if err != nil {
		return false
	}
	return len(c.jar.Cookies(parsed)) > 0
}

// Logout drops the session in memory and on disk.
func (c *Client) Logout() error {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	c.jar = jar
	c.http.Jar = jar
	return clearCookies(c.stateDir)
}

// rewriteHost points a request at the mock upstream when one is configured,
// preserving the path and query so tests still match on the real request shape.
func (c *Client) rewriteHost(target string) string {
	if c.baseHost == "" {
		return target
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return target
	}
	base, err := url.Parse(c.baseHost)
	if err != nil {
		return target
	}
	parsed.Scheme, parsed.Host = base.Scheme, base.Host
	return parsed.String()
}

// throttle enforces the minimum gap between upstream calls.
func (c *Client) throttle() {
	c.throttleMu.Lock()
	defer c.throttleMu.Unlock()
	if wait := minRequestInterval - time.Since(c.lastCall); wait > 0 && !c.lastCall.IsZero() {
		time.Sleep(wait)
	}
	c.lastCall = time.Now()
}

type requestSpec struct {
	Method  string
	URL     string
	Query   url.Values
	Body    any
	Headers map[string]string
	// Anonymous omits the session cookie: public endpoints are read without
	// presenting a paid credential they do not need.
	Anonymous bool
}

func (c *Client) do(ctx context.Context, spec requestSpec) ([]byte, error) {
	target := c.rewriteHost(spec.URL)
	if len(spec.Query) > 0 {
		separator := "?"
		if strings.Contains(target, "?") {
			separator = "&"
		}
		target += separator + spec.Query.Encode()
	}

	var reader io.Reader
	if spec.Body != nil {
		encoded, err := json.Marshal(spec.Body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, spec.Method, target, reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json, text/plain, */*")
	request.Header.Set("User-Agent", UserAgent)
	if spec.Body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range spec.Headers {
		request.Header.Set(key, value)
	}
	// The account's cookies are issued for `www.caixin.com`, but the account's
	// own APIs -- entitlements, the signed body -- live on `gateway.caixin.com`.
	// A cookie jar sends nothing across that boundary, so those endpoints
	// answered "not logged in" to a signed-in caller for as long as they have
	// existed. Anonymous requests are left alone: a public read must not present
	// a paid credential it does not need.
	if !spec.Anonymous && request.Header.Get("Cookie") == "" && needsExplicitSession(target) {
		if header := sessionCookieHeader(c.SessionCookies()); header != "" {
			request.Header.Set("Cookie", header)
		}
	}

	client := c.http
	if spec.Anonymous {
		anonymous := *c.http
		anonymous.Jar = nil
		client = &anonymous
	}

	c.throttle()
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 400 {
		return nil, &APIError{
			StatusCode: response.StatusCode,
			Method:     spec.Method,
			URL:        spec.URL,
			Message:    fmt.Sprintf("Caixin returned HTTP %d for %s", response.StatusCode, urlPath(spec.URL)),
		}
	}
	if response.StatusCode >= 300 {
		return nil, &APIError{
			StatusCode: response.StatusCode,
			Method:     spec.Method,
			URL:        spec.URL,
			Message:    fmt.Sprintf("Caixin returned an unexpected redirect for %s", urlPath(spec.URL)),
		}
	}
	return payload, nil
}

func urlPath(raw string) string {
	if parsed, err := url.Parse(raw); err == nil {
		return parsed.Path
	}
	return raw
}

// requestJSON performs one call and decodes the body as a JSON object.
func (c *Client) requestJSON(ctx context.Context, spec requestSpec) (map[string]any, error) {
	raw, err := c.do(ctx, spec)
	if err != nil {
		return nil, err
	}
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))

	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, &APIError{
			Method:  spec.Method,
			URL:     spec.URL,
			Message: fmt.Sprintf("Caixin did not return JSON for %s", urlPath(spec.URL)),
		}
	}
	return value, nil
}

// apiSuccess enforces the gateway's `code == 0` convention.
func apiSuccess(value map[string]any, action string) (map[string]any, error) {
	code := value["code"]
	if code == nil {
		return value, nil
	}
	if asString(code) == "0" {
		return value, nil
	}
	message, _ := value["msg"].(string)
	if strings.TrimSpace(message) == "" {
		message = fmt.Sprintf("%s failed with code %v", action, code)
	}
	return nil, &APIError{Message: fmt.Sprintf("%s: %s", action, message), Code: code}
}

// needsExplicitSession reports whether a target is a Caixin host the cookie jar
// will not cover on its own.
func needsExplicitSession(target string) bool {
	parsed, err := url.Parse(target)
	if err != nil {
		return false
	}
	return parsed.Hostname() == "gateway.caixin.com"
}

// sessionCookieHeader serialises the stored session for one request.
func sessionCookieHeader(cookies []*http.Cookie) string {
	parts := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		parts = append(parts, cookie.Name+"="+cookie.Value)
	}
	return strings.Join(parts, "; ")
}
