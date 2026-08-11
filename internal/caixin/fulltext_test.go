package caixin

import (
	"net/http"
	"strings"
	"testing"
)

// The signed body lives on a different host than the login cookies are scoped
// to, so a cookie jar sends nothing and the endpoint answers 401. Serialising
// the session onto the request is what makes full text reachable at all.
func TestSessionCookieHeader_SerialisesEveryCookie(t *testing.T) {
	header := sessionCookieHeader([]*http.Cookie{
		{Name: "SA_USER_UID", Value: "1234567", Domain: "www.caixin.com"},
		{Name: "SA_USER_auth", Value: "token", Domain: "www.caixin.com"},
	})
	for _, want := range []string{"SA_USER_UID=1234567", "SA_USER_auth=token"} {
		if !strings.Contains(header, want) {
			t.Errorf("header %q is missing %q", header, want)
		}
	}
}

func TestSessionCookieHeader_EmptySessionIsEmpty(t *testing.T) {
	if header := sessionCookieHeader(nil); header != "" {
		t.Errorf("header = %q, want empty for a session with no cookies", header)
	}
}
