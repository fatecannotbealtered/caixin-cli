package caixin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/fatecannotbealtered/caixin-cli/internal/secret"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// QR login needs a human with the Caixin app, so it is split in two commands
// (CLI-SPEC §16.3): `login` mints the code and stops, `login-resume` checks once
// and returns.
//
// Neither polls. A command that sat in a loop waiting for a scan would block an
// agent for minutes, hide the human action behind a spinner, and make the
// timeout the tool's decision rather than the caller's. The reference
// implementation's `qr-wait` did poll with a deadline; that is the behaviour
// this deliberately does not port.

// pendingLoginFile holds the handshake between the two commands.
const pendingLoginFile = "login-pending.json"

// pendingLogin is the state `login` leaves for `login-resume`.
type pendingLogin struct {
	QRCode    string `json:"qr_code"`
	CreatedAt int64  `json:"created_at"`
	ImagePath string `json:"image_path"`
}

func pendingLoginPath(stateDir string) string {
	return filepath.Join(stateDir, pendingLoginFile)
}

// pendingLoginSecret is the name the handshake is sealed under.
//
// The code in it is a credential for the length of the handshake -- it is what
// `login-resume` presents to claim the session -- so it belongs in the same
// sealed store as the session, not in a plaintext file beside it. The old
// plaintext file is removed on sight, because leaving a credential behind after
// a build stops reading it is pure exposure.
const pendingLoginSecret = "login-pending"

func savePendingLogin(stateDir string, pending pendingLogin) error {
	encoded, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	if err := secret.New(stateDir).Save(pendingLoginSecret, encoded); err != nil {
		return err
	}
	_ = os.Remove(pendingLoginPath(stateDir))
	return nil
}

func loadPendingLogin(stateDir string) (pendingLogin, bool) {
	var pending pendingLogin
	raw, err := secret.New(stateDir).Load(pendingLoginSecret)
	if err == nil && json.Unmarshal(raw, &pending) == nil && pending.QRCode != "" {
		return pending, true
	}
	// A handshake started by an older build is still usable; it is migrated by
	// being consumed, and the plaintext file goes with it.
	legacy, err := os.ReadFile(pendingLoginPath(stateDir))
	if err != nil {
		return pendingLogin{}, false
	}
	if json.Unmarshal(legacy, &pending) != nil || pending.QRCode == "" {
		return pendingLogin{}, false
	}
	return pending, true
}

func clearPendingLogin(stateDir string) {
	_ = secret.New(stateDir).Delete(pendingLoginSecret)
	_ = os.Remove(pendingLoginPath(stateDir))
}

// LoginStart mints a QR code and returns where the image was written.
func (c *Client) LoginStart(ctx context.Context, imagePath string) (map[string]any, error) {
	extend, err := json.Marshal(map[string]any{"resource_article": ""})
	if err != nil {
		return nil, err
	}
	raw, err := c.do(ctx, requestSpec{
		Method: http.MethodPost,
		URL:    QRStartURL,
		Body: map[string]any{
			"bisCode":    1001,
			"unit":       1,
			"deviceType": 5,
			"extend":     string(extend),
		},
		Headers: map[string]string{
			"Origin":  "https://u.caixin.com",
			"Referer": "https://u.caixin.com/web/login",
		},
		Anonymous: true,
	})
	if err != nil {
		return nil, err
	}
	value, err := decodeJSONObject(raw)
	if err != nil {
		return nil, err
	}
	value, err = apiSuccess(value, "generating the login QR code")
	if err != nil {
		return nil, err
	}
	data, _ := value["data"].(map[string]any)
	qrCode := asString(data["qrCode"])
	image := asString(data["image"])
	if qrCode == "" || image == "" {
		return nil, &APIError{Message: "the QR response carried no qrCode or image"}
	}

	if imagePath == "" {
		imagePath = filepath.Join(c.stateDir, "login-qr.png")
	}
	written, err := c.saveQRImage(ctx, image, imagePath)
	if err != nil {
		return nil, err
	}

	if err := savePendingLogin(c.stateDir, pendingLogin{
		QRCode:    qrCode,
		CreatedAt: time.Now().Unix(),
		ImagePath: written,
	}); err != nil {
		return nil, err
	}

	result := map[string]any{
		"status":       "NEW",
		"qr_image":     written,
		"action":       "open the Caixin app, tap 我的 → 扫一扫, and scan this image",
		"resume":       "caixin-cli login-resume",
		"human_action": true,
	}
	// The code is good for about a minute, which is short enough that a caller
	// pacing itself normally will miss the window and read the expiry as a bug.
	// The deadline is read from the service rather than hardcoded, so a change
	// upstream does not turn this field into a confident lie.
	if expires, ok := c.qrExpiry(ctx, qrCode); ok {
		result["expires_at"] = expires.UTC().Format(time.RFC3339)
		result["expires_in_seconds"] = int(time.Until(expires).Seconds())
	}
	return result, nil
}

// qrExpiry asks the status endpoint when this code dies. A failure here is not
// worth failing `login` over: the code still works, the caller just does not
// learn the deadline.
func (c *Client) qrExpiry(ctx context.Context, qrCode string) (time.Time, bool) {
	raw, err := c.do(ctx, requestSpec{
		Method: http.MethodGet,
		URL:    QRStatusURL,
		Query:  url.Values{"qrCode": {qrCode}, "_t": {strconv.FormatInt(time.Now().UnixMilli(), 10)}},
		Headers: map[string]string{
			"Referer": "https://u.caixin.com/web/login",
			"Cookie":  "LOGIN_QR_CODE=" + qrCode,
		},
		Anonymous: true,
	})
	if err != nil {
		return time.Time{}, false
	}
	value, err := decodeJSONObject(raw)
	if err != nil {
		return time.Time{}, false
	}
	data, _ := value["data"].(map[string]any)
	// A millisecond epoch is ~1.8e12, far below the 2^53 where a JSON number
	// would start losing precision, so the shared int reader is safe here.
	milliseconds, ok := safeInt(data["expireTime"])
	if !ok || milliseconds <= 0 {
		return time.Time{}, false
	}
	return time.UnixMilli(int64(milliseconds)), true
}

// saveQRImage writes the QR bitmap, accepting either a data URI or a Caixin url.
func (c *Client) saveQRImage(ctx context.Context, source, target string) (string, error) {
	var content []byte

	switch {
	case strings.HasPrefix(source, "data:"):
		header, encoded, found := strings.Cut(source, ",")
		if !found {
			return "", &APIError{Message: "the QR image data URI is malformed"}
		}
		mediaType := strings.TrimPrefix(header, "data:")
		mediaType, _, _ = strings.Cut(mediaType, ";")
		switch strings.ToLower(mediaType) {
		case "image/png", "image/jpeg", "image/gif", "image/webp":
		default:
			// Only bitmaps: an SVG is a document that can carry script, and the
			// agent is about to hand this file to a human to open.
			return "", &APIError{Message: "the QR image is not a supported bitmap format"}
		}
		if strings.Contains(header, ";base64") {
			content, _ = base64.StdEncoding.DecodeString(encoded)
		} else {
			unescaped, err := url.QueryUnescape(encoded)
			if err != nil {
				return "", &APIError{Message: "the QR image data could not be decoded"}
			}
			content = []byte(unescaped)
		}
		if len(content) == 0 {
			return "", &APIError{Message: "the QR image data could not be decoded"}
		}

	default:
		imageURL := source
		if strings.HasPrefix(imageURL, "//") {
			imageURL = "https:" + imageURL
		}
		parsed, err := url.Parse(imageURL)
		if err != nil || !caixinHost(parsed.Hostname()) {
			return "", &APIError{Message: "the QR image url is not a Caixin url"}
		}
		content, err = c.do(ctx, requestSpec{
			Method: http.MethodGet, URL: imageURL, Anonymous: true,
		})
		if err != nil {
			return "", err
		}
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(target, content, 0o600); err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(target)
	if err != nil {
		return target, nil
	}
	return absolute, nil
}

// ErrLoginPending means the code exists but nobody has confirmed it yet.
var ErrLoginPending = fmt.Errorf("the QR code has not been confirmed yet")

// ErrNoPendingLogin means `login-resume` was called without a `login` first.
var ErrNoPendingLogin = fmt.Errorf("no login is in progress; run `caixin-cli login` first")

// LoginResume checks the outstanding code exactly once.
func (c *Client) LoginResume(ctx context.Context) (map[string]any, string, error) {
	pending, ok := loadPendingLogin(c.stateDir)
	if !ok {
		return nil, "", ErrNoPendingLogin
	}

	response, err := c.do(ctx, requestSpec{
		Method: http.MethodGet,
		URL:    QRStatusURL,
		Query: url.Values{
			"qrCode": {pending.QRCode},
			"_t":     {strconv.FormatInt(time.Now().UnixMilli(), 10)},
		},
		Headers: map[string]string{
			"Referer": "https://u.caixin.com/web/login",
			// The status endpoint will not find a code it is not also handed as a
			// cookie: the login page sets `LOGIN_QR_CODE` the moment it mints one,
			// and the query parameter alone answers "二维码不存在" every time,
			// however fresh the code is. Verified against the endpoint directly --
			// same code, one request with the cookie and one without.
			"Cookie": "LOGIN_QR_CODE=" + pending.QRCode,
		},
		Anonymous: true,
	})
	if err != nil {
		return nil, "", err
	}
	value, err := decodeJSONObject(response)
	if err != nil {
		return nil, "", err
	}
	value, err = apiSuccess(value, "checking the QR code")
	if err != nil {
		return nil, "", err
	}
	data, _ := value["data"].(map[string]any)
	status := asString(data["status"])
	if status == "" {
		status = "NEW"
	}

	switch status {
	case "CONFIRMED":
		result, _ := data["loginResult"].(map[string]any)
		if result == nil {
			return nil, status, &APIError{Message: "the confirmation carried no loginResult"}
		}
		if _, err := apiSuccess(result, "completing the scan login"); err != nil {
			return nil, status, err
		}
		user, _ := result["data"].(map[string]any)
		if user == nil {
			return nil, status, &APIError{Message: "the login response carried no user"}
		}
		c.applyLoginCookies(user)
		if err := c.SaveSession(); err != nil {
			return nil, status, err
		}
		// The handshake is finished; the code must not linger where a later run
		// could replay it.
		clearPendingLogin(c.stateDir)
		if pending.ImagePath != "" {
			_ = os.Remove(pending.ImagePath)
		}
		return map[string]any{
			"logged_in": true,
			"status":    status,
			"user":      publicUser(user),
		}, status, nil

	case "EXPIRED", "CANCELED":
		clearPendingLogin(c.stateDir)
		if pending.ImagePath != "" {
			_ = os.Remove(pending.ImagePath)
		}
		return nil, status, &APIError{
			Message: "the QR code is " + strings.ToLower(status) + "; run `caixin-cli login` again",
		}

	case "NEW", "SCANED":
		return nil, status, ErrLoginPending

	default:
		return nil, status, &APIError{
			Message: "the QR endpoint returned unknown status " + status,
		}
	}
}

// applyLoginCookies turns the login payload into the session cookie jar.
func (c *Client) applyLoginCookies(user map[string]any) {
	base, err := url.Parse("https://www.caixin.com/")
	if err != nil {
		return
	}
	var cookies []*http.Cookie
	for name, field := range loginCookieFields {
		value := asString(user[field])
		if value == "" {
			continue
		}
		cookies = append(cookies, &http.Cookie{
			Name: name, Value: url.QueryEscape(value), Path: "/", Secure: true,
		})
	}
	for _, field := range []string{"mobile", "email", "nickname"} {
		if value := asString(user[field]); value != "" {
			cookies = append(cookies, &http.Cookie{
				Name: "SA_USER_USER_NAME", Value: url.QueryEscape(value), Path: "/", Secure: true,
			})
			break
		}
	}
	c.jar.SetCookies(base, cookies)
}

// publicUser keeps the account fields an agent may see and drops the rest.
//
// The login payload carries the auth token and the raw mobile number; echoing
// either would put a credential and personal data into stdout, which CLI-SPEC
// §10 forbids.
func publicUser(user map[string]any) map[string]any {
	public := map[string]any{}
	for _, field := range []string{"nickname", "uid"} {
		if value := asString(user[field]); value != "" {
			public[field] = value
		}
	}
	if mobile := asString(user["mobile"]); len(mobile) >= 4 {
		public["mobile_suffix"] = mobile[len(mobile)-4:]
	}
	return public
}

// decodeJSONObject parses a response body the login flow already fetched.
func decodeJSONObject(raw []byte) (map[string]any, error) {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, &APIError{Message: "Caixin did not return JSON for the login request"}
	}
	return value, nil
}
