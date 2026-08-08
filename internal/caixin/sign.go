package caixin

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
)

// The signature Caixin's front end mints is a plain RSASSA-PKCS1-v1_5 SHA-256
// signature over a fixed plaintext layout, verified byte-for-byte against the
// browser's own output:
//
//	plaintext = "id=<articleId>&uid=<uid>&<nonce>=nonce"
//	X-Sign    = urlencode(base64(sign(plaintext)))
//	X-Nonce   = nonce
//
// uid is the account's SA_USER_UID cookie, which is why a signature minted in
// an anonymous context is rejected: it is bound to the reader, not just the
// article.
//
// The header must be url-encoded. Sending raw base64 is answered with 401,
// which is worth recording because the failure looks identical to a bad key.

// uidCookie carries the account id the plaintext is bound to.
const uidCookie = "SA_USER_UID"

// newNonce returns the 32 uppercase hex characters the front end uses.
func newNonce() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(raw)), nil
}

// signLocally reproduces createSign without a browser.
func signLocally(key *rsa.PrivateKey, articleID, uid string) (*Signature, error) {
	nonce, err := newNonce()
	if err != nil {
		return nil, err
	}
	plaintext := "id=" + articleID + "&uid=" + uid + "&" + nonce + "=nonce"
	digest := sha256.Sum256([]byte(plaintext))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return nil, err
	}
	return &Signature{
		Sign:  url.QueryEscape(base64.StdEncoding.EncodeToString(signature)),
		Nonce: nonce,
	}, nil
}

// sessionUID reads the account id the signature has to be bound to.
func sessionUID(cookies []*http.Cookie) string {
	for _, cookie := range cookies {
		if cookie.Name == uidCookie {
			return cookie.Value
		}
	}
	return ""
}
