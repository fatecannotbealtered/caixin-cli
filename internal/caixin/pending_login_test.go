package caixin

import (
	"os"
	"path/filepath"
	"testing"
)

// The QR code is what `login-resume` presents to claim a session, so it is a
// credential and belongs in the sealed store rather than a plaintext file.
func TestPendingLogin_IsSealedAndLeavesNoPlaintext(t *testing.T) {
	dir := t.TempDir()
	if err := savePendingLogin(dir, pendingLogin{QRCode: "qr-secret", CreatedAt: 1}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(pendingLoginPath(dir)); !os.IsNotExist(err) {
		t.Error("the plaintext handshake file is still present")
	}
	for _, entry := range mustReadDir(t, dir) {
		raw, err := os.ReadFile(filepath.Join(dir, entry))
		if err != nil {
			continue
		}
		if containsSubstring(string(raw), "qr-secret") {
			t.Errorf("%s holds the QR code in the clear", entry)
		}
	}
	loaded, ok := loadPendingLogin(dir)
	if !ok || loaded.QRCode != "qr-secret" {
		t.Fatalf("loaded = %#v, ok = %v", loaded, ok)
	}
	clearPendingLogin(dir)
	if _, ok := loadPendingLogin(dir); ok {
		t.Error("the handshake survived clearPendingLogin")
	}
}

// A handshake started by an older build still completes; consuming it is what
// migrates it.
func TestPendingLogin_ReadsALegacyPlaintextHandshake(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(pendingLoginPath(dir),
		[]byte(`{"qr_code":"legacy","created_at":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, ok := loadPendingLogin(dir)
	if !ok || loaded.QRCode != "legacy" {
		t.Fatalf("loaded = %#v, ok = %v", loaded, ok)
	}
	clearPendingLogin(dir)
	if _, err := os.Stat(pendingLoginPath(dir)); !os.IsNotExist(err) {
		t.Error("the legacy plaintext file was left behind")
	}
}

func mustReadDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func containsSubstring(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		func() bool {
			for i := 0; i+len(needle) <= len(haystack); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		}()
}
