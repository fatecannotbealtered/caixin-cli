package caixin

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"

	"github.com/fatecannotbealtered/caixin-cli/internal/secret"
)

// cookieRecord is the on-disk shape of one persisted cookie.
type cookieRecord struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
	Path   string `json:"path"`
	Secure bool   `json:"secure"`
}

func cookiePath(stateDir string) string {
	return filepath.Join(stateDir, "cookies.json")
}

// sessionSecret is the name the session is sealed under in the secret store.
const sessionSecret = "session"

// readSession opens the stored session.
//
// Both readers go through here. They used not to: `loadCookieList` read the
// plaintext file directly, so once the session moved into the sealed store it
// silently returned nothing and `article --full` reported a missing account id.
func readSession(stateDir string) ([]cookieRecord, bool) {
	store := secret.New(stateDir)
	raw, err := store.Load(sessionSecret)
	if err != nil {
		adopted, ok := store.AdoptPlaintext(sessionSecret, cookiePath(stateDir))
		if !ok {
			return nil, false
		}
		raw = adopted
	}
	var records []cookieRecord
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil, false
	}
	return records, true
}

// loadCookies restores a persisted session. A missing or unreadable file is not
// an error: the caller just ends up anonymous, which most commands support.
//
// A session written by an earlier build is plaintext JSON; it is adopted into
// the encrypted store on first read and the plaintext original removed, so the
// upgrade needs no user action and leaves nothing readable behind.
func loadCookies(jar *cookiejar.Jar, stateDir string) {
	records, ok := readSession(stateDir)
	if !ok {
		return
	}
	byHost := map[string][]*http.Cookie{}
	for _, record := range records {
		host := record.Domain
		if len(host) > 0 && host[0] == '.' {
			host = host[1:]
		}
		if host == "" {
			host = "www.caixin.com"
		}
		byHost[host] = append(byHost[host], &http.Cookie{
			Name:   record.Name,
			Value:  record.Value,
			Path:   record.Path,
			Domain: record.Domain,
			Secure: record.Secure,
		})
	}
	for host, cookies := range byHost {
		if parsed, err := url.Parse("https://" + host + "/"); err == nil {
			jar.SetCookies(parsed, cookies)
		}
	}
}

// SaveSession persists the jar through a temp file so an interrupted write can
// never leave a truncated session behind.
func (c *Client) SaveSession() error {
	if err := os.MkdirAll(c.stateDir, 0o700); err != nil {
		return err
	}
	base, err := url.Parse("https://www.caixin.com/")
	if err != nil {
		return err
	}
	records := []cookieRecord{}
	for _, cookie := range c.jar.Cookies(base) {
		records = append(records, cookieRecord{
			Name:   cookie.Name,
			Value:  cookie.Value,
			Domain: base.Host,
			Path:   "/",
			Secure: cookie.Secure,
		})
	}
	encoded, err := json.Marshal(records)
	if err != nil {
		return err
	}
	// The session is an account-level credential, so it is sealed rather than
	// written as JSON (SEC-SPEC §4). The store owns the atomic write.
	return secret.New(c.stateDir).Save(sessionSecret, encoded)
}

func clearCookies(stateDir string) error {
	if err := secret.New(stateDir).Delete(sessionSecret); err != nil {
		return err
	}
	// Logout means "this machine no longer holds my credentials". A plaintext
	// copy left behind by an earlier build would make that false, so every
	// known plaintext location is removed too, not just the current one.
	for _, path := range LegacyPlaintextFiles(stateDir) {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// legacyPlaintextNames are the files that have ever held this account's
// credentials in the clear. The Python-era names are included because a state
// directory carried over from that implementation still has them, and nothing
// reads them any more -- so they are pure exposure.
var legacyPlaintextNames = []string{
	"cookies.json",    // the Go build's own pre-encryption session
	"signing-key.pem", // the Go build's own pre-encryption signing key
	"pw-state.json",   // browser storage state, with the full cookie jar
	"session.dat",     // the reference implementation's session
	"qr-state.dat",    // a pending QR login, which carries a login token
}

// LegacyPlaintextFiles lists the plaintext credential files present in a state
// directory. `doctor` reports them and `logout` removes them.
func LegacyPlaintextFiles(stateDir string) []string {
	var found []string
	for _, name := range legacyPlaintextNames {
		path := filepath.Join(stateDir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			found = append(found, path)
		}
	}
	return found
}

// SessionBackend names where the session is held, for `context` and `doctor`.
func SessionBackend(stateDir string) string {
	return secret.New(stateDir).Backend()
}

// loadCookieList reads the persisted session as plain http cookies.
func loadCookieList(stateDir string) []*http.Cookie {
	records, ok := readSession(stateDir)
	if !ok {
		return nil
	}
	cookies := make([]*http.Cookie, 0, len(records))
	for _, record := range records {
		cookies = append(cookies, &http.Cookie{
			Name:   record.Name,
			Value:  record.Value,
			Domain: record.Domain,
			Path:   record.Path,
			Secure: record.Secure,
		})
	}
	return cookies
}
