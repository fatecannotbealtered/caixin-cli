package caixin

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fatecannotbealtered/caixin-cli/internal/secret"
)

// The full-text endpoint authenticates callers with an RSA signature. The key
// it is minted with is shipped inside Caixin's own front-end bundle, so it is
// not a secret in any useful sense -- but it is still key material belonging to
// someone else, and SEC-SPEC 4 plus the open-source checklist are unambiguous
// that credentials do not live in the working tree. So it is held the same way
// the session is: in the state directory, never in the repo, and overridable
// through the environment for non-interactive hosts.
//
// signingKeyEnv is the recommended channel for headless installs, per SEC-SPEC
// 4's "env vars are the recommended non-interactive secret channel".
const signingKeyEnv = "CAIXIN_SIGNING_KEY"

func signingKeyPath(stateDir string) string {
	return filepath.Join(stateDir, "signing-key.pem")
}

// signingKeySecret is the name the key is sealed under in the secret store.
const signingKeySecret = "signing-key"

// signingKeySource names where a key came from, so `context` can report the
// backend and a degraded setup stays visible rather than silent.
type signingKeySource string

const (
	signingKeyFromEnv     signingKeySource = "env"
	signingKeyFromFile    signingKeySource = "file"
	signingKeyFromBrowser signingKeySource = "browser"
	signingKeyAbsent      signingKeySource = "none"
)

// loadSigningKey resolves the key from the environment first, then the state
// directory. A missing key is not an error: the caller falls back to extracting
// one with a browser.
func loadSigningKey(stateDir string) (*rsa.PrivateKey, signingKeySource) {
	if raw := os.Getenv(signingKeyEnv); strings.TrimSpace(raw) != "" {
		if key, err := parseSigningKey(raw); err == nil {
			return key, signingKeyFromEnv
		}
		// A malformed override is worth surfacing, but not by failing here --
		// the browser path can still recover. `doctor` reports it instead.
		return nil, signingKeyAbsent
	}
	store := secret.New(stateDir)
	raw, err := store.Load(signingKeySecret)
	if err != nil {
		// A key written by an earlier build, or dropped in by hand, is a
		// plaintext PEM. Adopt it into the store and remove the original.
		adopted, ok := store.AdoptPlaintext(signingKeySecret, signingKeyPath(stateDir))
		if !ok {
			return nil, signingKeyAbsent
		}
		raw = adopted
	}
	key, err := parseSigningKey(string(raw))
	if err != nil {
		return nil, signingKeyAbsent
	}
	return key, signingKeyFromFile
}

// SigningKeyStatus reports whether a full-text signing key is available and
// which backend holds it, so `context` and `doctor` can show the degradation
// without ever emitting the key itself.
func SigningKeyStatus(stateDir string) (bool, string) {
	key, source := loadSigningKey(stateDir)
	if source == signingKeyFromFile {
		// Name the storage backend rather than the generic "file", so a keyring
		// install and an encrypted-file fallback are distinguishable.
		return key != nil, secret.New(stateDir).Backend()
	}
	return key != nil, string(source)
}

// parseSigningKey accepts a PEM block (PKCS#8 or PKCS#1) or a bare base64 DER
// body, because the value is copied by hand often enough that being strict
// about the wrapper would only produce avoidable support questions.
func parseSigningKey(raw string) (*rsa.PrivateKey, error) {
	text := strings.TrimSpace(raw)
	// An env var carrying a PEM usually arrives with literal backslash-n.
	text = strings.ReplaceAll(text, `\n`, "\n")

	if block, _ := pem.Decode([]byte(text)); block != nil {
		return parseSigningKeyDER(block.Bytes)
	}
	// Bare base64: strip the whitespace a wrapped paste leaves behind.
	compact := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || r == ' ' {
			return -1
		}
		return r
	}, text)
	der, err := base64.StdEncoding.DecodeString(compact)
	if err != nil {
		return nil, fmt.Errorf("the signing key is neither a PEM block nor base64")
	}
	return parseSigningKeyDER(der)
}

func parseSigningKeyDER(der []byte) (*rsa.PrivateKey, error) {
	if key, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("the signing key is not an RSA key")
		}
		return rsaKey, nil
	}
	key, err := x509.ParsePKCS1PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("the signing key could not be decoded")
	}
	return key, nil
}

// saveSigningKey caches a browser-extracted key so later runs need no browser.
//
// The 0600 is a POSIX statement only: on Windows the protection comes from the
// user-profile ACL, which is the same protection the session already relies on
// (SEC-SPEC 4).
func saveSigningKey(stateDir string, key *rsa.PrivateKey) error {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	encoded := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	// Sealed, not written as a PEM: it is key material, and SEC-SPEC §4 makes
	// no exception for key material that happens to be someone else's.
	return secret.New(stateDir).Save(signingKeySecret, encoded)
}
