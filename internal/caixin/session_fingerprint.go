package caixin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sort"
)

// CredentialFingerprint returns an irreversible, stable identity for the
// credential material logout will remove. It is bound into the confirm token
// but never emitted in command output.
func (c *Client) CredentialFingerprint() string {
	base, _ := url.Parse("https://www.caixin.com/")
	records := []cookieRecord{}
	if base != nil {
		for _, cookie := range c.jar.Cookies(base) {
			records = append(records, cookieRecord{
				Name: cookie.Name, Value: cookie.Value, Domain: cookie.Domain,
				Path: cookie.Path, Secure: cookie.Secure,
			})
		}
	}
	sort.Slice(records, func(i, j int) bool {
		left, _ := json.Marshal(records[i])
		right, _ := json.Marshal(records[j])
		return string(left) < string(right)
	})

	hash := sha256.New()
	encoded, _ := json.Marshal(records)
	_, _ = hash.Write(encoded)
	for _, path := range LegacyPlaintextFiles(c.stateDir) {
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(filepath.Base(path)))
		_, _ = hash.Write([]byte{0})
		if raw, err := os.ReadFile(path); err == nil {
			digest := sha256.Sum256(raw)
			_, _ = hash.Write(digest[:])
		} else if info, statErr := os.Stat(path); statErr == nil {
			metadata, _ := json.Marshal([]any{info.Size(), info.ModTime().UTC().UnixNano()})
			_, _ = hash.Write(metadata)
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}
