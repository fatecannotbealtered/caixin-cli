package caixin

import (
	"context"
	"net/http"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// setCookies hands the stored session to the browser so the page it loads is
// the same signed-in one this client already has. Nothing new is authenticated
// here; the cookies come straight from the state directory.
func setCookies(cookies []*http.Cookie) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		for _, cookie := range cookies {
			domain := cookie.Domain
			if domain == "" {
				domain = ".caixin.com"
			}
			path := cookie.Path
			if path == "" {
				path = "/"
			}
			if err := network.SetCookie(cookie.Name, cookie.Value).
				WithDomain(domain).
				WithPath(path).
				WithSecure(cookie.Secure).
				Do(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}

// SessionCookies exposes the stored session for the signer. It is the only
// place the jar is read out, and the values go straight into the browser
// context -- never into output.
func (c *Client) SessionCookies() []*http.Cookie {
	return loadCookieList(c.stateDir)
}
