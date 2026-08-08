package caixin

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The key here is generated per-run. Caixin's own key is deliberately absent
// from the tree (SEC-SPEC 4 / OPEN_SOURCE_CHECKLIST): what these tests pin is
// the algorithm and the wire format, which is what would silently break.

func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

// The plaintext layout and the signature scheme are what the upstream verifies.
// Pinning them here means a refactor that changes either fails locally instead
// of turning into a 401 that looks like an expired session.
func TestSignLocally_MatchesTheVerifiedScheme(t *testing.T) {
	key := testKey(t)
	signature, err := signLocally(key, "102472114", "8547219")
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	if !regexp.MustCompile(`^[0-9A-F]{32}$`).MatchString(signature.Nonce) {
		t.Errorf("nonce = %q, want 32 uppercase hex characters", signature.Nonce)
	}

	// X-Sign travels url-encoded. Sending raw base64 is answered with 401, and
	// that failure is indistinguishable from a bad key, so it is worth a test.
	decoded, err := url.QueryUnescape(signature.Sign)
	if err != nil {
		t.Fatalf("X-Sign is not url-encoded: %v", err)
	}
	if decoded == signature.Sign && strings.ContainsAny(decoded, "+/=") {
		t.Error("X-Sign must be url-encoded on the wire")
	}
	raw, err := base64.StdEncoding.DecodeString(decoded)
	if err != nil {
		t.Fatalf("X-Sign is not base64: %v", err)
	}

	plaintext := "id=102472114&uid=8547219&" + signature.Nonce + "=nonce"
	digest := sha256.Sum256([]byte(plaintext))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], raw); err != nil {
		t.Fatalf("signature does not verify over %q: %v", plaintext, err)
	}
}

// A fresh nonce per call is the upstream's replay defence; reusing one would
// work today and fail whenever they start tracking them.
func TestSignLocally_UsesAFreshNonce(t *testing.T) {
	key := testKey(t)
	first, err := signLocally(key, "102472114", "8547219")
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	second, err := signLocally(key, "102472114", "8547219")
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if first.Nonce == second.Nonce {
		t.Error("two signatures reused the same nonce")
	}
	if first.Sign == second.Sign {
		t.Error("two signatures were identical; the nonce is not reaching the plaintext")
	}
}

func TestSessionUID(t *testing.T) {
	cookies := []*http.Cookie{
		{Name: "other", Value: "x"},
		{Name: uidCookie, Value: "8547219"},
	}
	if got := sessionUID(cookies); got != "8547219" {
		t.Errorf("sessionUID = %q, want 8547219", got)
	}
	if got := sessionUID(nil); got != "" {
		t.Errorf("sessionUID with no session = %q, want empty", got)
	}
}

func pkcs8PEM(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

// The key is copied by hand on headless hosts, so the parser has to survive the
// shapes that arrive: PEM, PKCS#1, a bare base64 body, and an env var whose
// newlines came through as backslash-n.
func TestParseSigningKey_AcceptsTheShapesThatArrive(t *testing.T) {
	key := testKey(t)
	pkcs8 := pkcs8PEM(t, key)
	pkcs1 := string(pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))
	bare := strings.NewReplacer("-----BEGIN PRIVATE KEY-----", "", "-----END PRIVATE KEY-----", "").
		Replace(pkcs8)

	for name, raw := range map[string]string{
		"pkcs8 pem":   pkcs8,
		"pkcs1 pem":   pkcs1,
		"bare base64": bare,
		"escaped env": strings.ReplaceAll(pkcs8, "\n", `\n`),
	} {
		parsed, err := parseSigningKey(raw)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if parsed.N.Cmp(key.N) != 0 {
			t.Errorf("%s: parsed a different key", name)
		}
	}

	if _, err := parseSigningKey("not a key at all"); err == nil {
		t.Error("garbage was accepted as a signing key")
	}
}

// Where the key comes from has to be reportable, because a silent fallback is
// exactly the degradation SEC-SPEC 4 asks to keep visible.
func TestLoadSigningKey_PrefersEnvThenFile(t *testing.T) {
	key := testKey(t)
	dir := t.TempDir()

	t.Setenv(signingKeyEnv, "")
	if got, source := loadSigningKey(dir); got != nil || source != signingKeyAbsent {
		t.Errorf("empty state reported %q", source)
	}

	if err := saveSigningKey(dir, key); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, source := loadSigningKey(dir)
	if got == nil || source != signingKeyFromFile {
		t.Fatalf("after save: key=%v source=%q", got != nil, source)
	}
	if got.N.Cmp(key.N) != 0 {
		t.Error("the cached key round-tripped into a different key")
	}

	other := testKey(t)
	t.Setenv(signingKeyEnv, pkcs8PEM(t, other))
	got, source = loadSigningKey(dir)
	if source != signingKeyFromEnv {
		t.Fatalf("source = %q, want env to win over the cached file", source)
	}
	if got.N.Cmp(other.N) != 0 {
		t.Error("the environment did not override the cached key")
	}
}

// The key must never be readable on disk, and no temp copy may survive.
func TestSaveSigningKey_LeavesNothingReadable(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	if err := saveSigningKey(dir, key); err != nil {
		t.Fatalf("save: %v", err)
	}
	// No plaintext PEM, and no temp file, anywhere in the state directory.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Errorf("a temp copy survived: %s", entry.Name())
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		if strings.Contains(string(raw), "PRIVATE KEY") {
			t.Errorf("%s holds the key in the clear", entry.Name())
		}
	}
	// And it still round-trips.
	loaded, source := loadSigningKey(dir)
	if loaded == nil {
		t.Fatalf("the sealed key did not load back (source=%q)", source)
	}
	if loaded.N.Cmp(key.N) != 0 {
		t.Error("the sealed key round-tripped into a different key")
	}
}

// A plaintext key left by an earlier build must be adopted into the store and
// the original removed -- otherwise the encryption is cosmetic.
func TestLoadSigningKey_AdoptsAndRemovesPlaintext(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	legacy := signingKeyPath(dir)
	if err := os.WriteFile(legacy, []byte(pkcs8PEM(t, key)), 0o600); err != nil {
		t.Fatalf("seed legacy key: %v", err)
	}

	loaded, _ := loadSigningKey(dir)
	if loaded == nil || loaded.N.Cmp(key.N) != 0 {
		t.Fatal("the legacy key was not adopted")
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Error("the plaintext key was left in place after adoption")
	}
	// Still loadable on the next run, now from the sealed store.
	again, _ := loadSigningKey(dir)
	if again == nil || again.N.Cmp(key.N) != 0 {
		t.Error("the adopted key did not load back")
	}
}
