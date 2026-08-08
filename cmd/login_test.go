package cmd

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A 1x1 PNG, base64. Real enough to exercise the data-URI path.
const fakeQR = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func loginMock(t *testing.T, status string, confirmed bool) *mockUpstream {
	t.Helper()
	mock := newMockUpstream(t)
	mock.handlers["/api/ucenter/scan/v1/genQRCode"] = func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"qrCode": "qr-1", "image": fakeQR},
		})
	}
	mock.handlers["/api/ucenter/scan/v1/checkQRCodeStatus"] = func(w http.ResponseWriter, _ *http.Request) {
		payload := map[string]any{"status": status}
		if confirmed {
			payload["loginResult"] = map[string]any{
				"code": 0,
				"data": map[string]any{
					"uid": "8547219", "nickname": "某读者", "userAuth": "super-secret-auth",
					"mobile": "13800001234", "unit": 1, "deviceType": 5,
				},
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": payload})
	}
	return mock
}

// `login` must hand the code to a human and stop. Polling on the user's behalf
// is exactly what CLI-SPEC §16.3 forbids.
func TestLogin_ReturnsHumanRequiredWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	got := runCLI(t, loginMock(t, "NEW", false), "login", "--state-dir", dir, "--compact")

	if code := got.ErrorCode(t); code != "E_HUMAN_REQUIRED" {
		t.Fatalf("code = %s, want E_HUMAN_REQUIRED", code)
	}
	if got.Exit != 9 {
		t.Errorf("exit = %d, want 9", got.Exit)
	}
	envelope := got.Envelope(t)
	errorObject, _ := envelope["error"].(map[string]any)
	details, _ := errorObject["details"].(map[string]any)
	if resume, _ := details["resume"].(string); !strings.Contains(resume, "login-resume") {
		t.Errorf("the challenge does not name the resume command: %v", details)
	}
	image, _ := details["qr_image"].(string)
	if image == "" {
		t.Fatal("no QR image path was returned")
	}
	if _, err := os.Stat(image); err != nil {
		t.Errorf("the QR image was not written: %v", err)
	}
}

// Resuming before the scan lands is still the human's turn, not an error the
// agent should treat as failure.
func TestLoginResume_UnscannedStaysHumanRequired(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, loginMock(t, "NEW", false), "login", "--state-dir", dir, "--compact")

	got := runCLI(t, loginMock(t, "SCANED", false), "login-resume", "--state-dir", dir, "--compact")
	if code := got.ErrorCode(t); code != "E_HUMAN_REQUIRED" {
		t.Errorf("code = %s, want E_HUMAN_REQUIRED", code)
	}
	if got.Exit != 9 {
		t.Errorf("exit = %d, want 9", got.Exit)
	}
}

func TestLoginResume_ConfirmedPersistsSession(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, loginMock(t, "NEW", false), "login", "--state-dir", dir, "--compact")

	got := runCLI(t, loginMock(t, "CONFIRMED", true), "login-resume", "--state-dir", dir, "--compact")
	if got.Exit != 0 {
		t.Fatalf("exit = %d: %s", got.Exit, got.Stdout)
	}
	data := got.Data(t)
	if loggedIn, _ := data["logged_in"].(bool); !loggedIn {
		t.Error("logged_in = false after a confirmed scan")
	}
	// Sealed, and the pending handshake must not survive to be replayed.
	if _, err := os.Stat(filepath.Join(dir, "session.enc")); err != nil {
		t.Errorf("the session was not persisted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "login-pending.json")); !os.IsNotExist(err) {
		t.Error("the pending login record survived a completed login")
	}
}

// The login payload carries the auth token and the full mobile number. Neither
// may reach stdout (CLI-SPEC §10).
func TestLoginResume_NeverEchoesTheCredential(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, loginMock(t, "NEW", false), "login", "--state-dir", dir, "--compact")

	got := runCLI(t, loginMock(t, "CONFIRMED", true), "login-resume", "--state-dir", dir, "--compact")
	for _, secret := range []string{"super-secret-auth", "13800001234"} {
		if strings.Contains(got.Stdout, secret) {
			t.Errorf("%q leaked into stdout:\n%s", secret, got.Stdout)
		}
	}
	// The masked suffix is still useful for telling accounts apart.
	data := got.Data(t)
	user, _ := data["user"].(map[string]any)
	if suffix, _ := user["mobile_suffix"].(string); suffix != "1234" {
		t.Errorf("mobile_suffix = %q, want the last four digits only", suffix)
	}
}

func TestLoginResume_WithoutAPendingLoginIsValidation(t *testing.T) {
	got := runCLI(t, loginMock(t, "NEW", false), "login-resume", "--state-dir", t.TempDir(), "--compact")
	if code := got.ErrorCode(t); code != "E_VALIDATION" {
		t.Errorf("code = %s, want E_VALIDATION", code)
	}
}

// An expired code must clear the handshake so the next attempt starts clean.
func TestLoginResume_ExpiredClearsTheHandshake(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, loginMock(t, "NEW", false), "login", "--state-dir", dir, "--compact")

	got := runCLI(t, loginMock(t, "EXPIRED", false), "login-resume", "--state-dir", dir, "--compact")
	if got.Exit == 0 {
		t.Fatalf("an expired code reported success: %s", got.Stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "login-pending.json")); !os.IsNotExist(err) {
		t.Error("the expired handshake was left in place")
	}
}
