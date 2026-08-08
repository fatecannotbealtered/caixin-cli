# Reading full article text

Verified 2026-08-07 against a paid account.

## What the plain HTTP path gets

Server HTML for a paid article carries the opening only: 3 paragraphs / 303
characters on the article measured, against 57 / 6560 for the full body. This is
true with or without a session — an entitled session gets the same short HTML,
with `#chargeWall` rendered empty instead of carrying a subscribe prompt.

So `caixin-cli article <url>` returns an **excerpt** and says so
(`complete: false`, `body_delivered_by_script: true`).

## Where the rest comes from

The remainder is fetched by page scripts:

```
GET https://gateway.caixin.com/api/newauth/checkAuthByIdJsonp
    ?type=0&id=<articleId>&page=<n>&rand=<random>
```

with two request headers:

| header | value |
|---|---|
| `X-Sign` | url-encoded base64 RSA signature, 344 base64 chars |
| `X-Nonce` | 32 uppercase hex characters, fresh per request |

Without them the endpoint answers `401 request error`.

Response is `{code, data}` where `data` is a `resetContentInfo({...})` call whose
`content` field holds the body HTML and whose `attr` carries the entitlement
marker. Long articles paginate; `totalPage` says how far to go.

## The signature

`window.createSign(articleId)` returns the header pair. It is a plain
RSASSA-PKCS1-v1_5 SHA-256 signature over a fixed plaintext:

```
plaintext = "id=" + articleId + "&uid=" + uid + "&" + nonce + "=nonce"
X-Sign    = urlencode(base64(sign(plaintext)))
X-Nonce   = nonce
```

`uid` is the account's `SA_USER_UID` cookie. That is why a signature minted in
an anonymous browser is rejected even when the article request itself carries a
session: the signature is bound to the reader, not just to the article.

Two properties that cost time to find, recorded so they do not have to be found
again:

- **`X-Sign` must be url-encoded.** Sending raw base64 is answered with `401`,
  which looks exactly like a bad key or an expired session.
- The signature is **deterministic** given the plaintext. Earlier notes in this
  file called it randomized; that was the fresh nonce, not the padding.

Reproduced in Go with `crypto/rsa` and checked byte-for-byte against the
browser's own `createSign` output for the same plaintext. So the CLI computes
the signature itself and `--full` is plain HTTP.

## The signing key

The key is an RSA-2048 **private** key shipped inside Caixin's own front-end
bundle. It is not a literal there: it is split into ten-character pieces across
an obfuscated string table and joined at runtime.

It is therefore public in effect — anyone who loads an article page has it — but
it is still key material belonging to someone else, and `SEC-SPEC.md §4` plus
`docs/OPEN_SOURCE_CHECKLIST.md` are unambiguous that credentials do not live in
the working tree. **It is not in this repo and must not be added to it.** It is
held the way the session is: in the state directory, or supplied through the
environment.

Resolution order:

| source | where |
|---|---|
| `CAIXIN_SIGNING_KEY` | PEM (PKCS#8 or PKCS#1) or bare base64; `\n` escapes are accepted, for env vars |
| state directory | `<state-dir>/signing-key.pem`, written `0600` |
| browser bootstrap | extracted once from the page and cached to the file above |

`context` and `doctor` report `configured`, `storage`, and `browserless` so a
host that would need a browser says so before `--full` is reached. Neither ever
emits the key.

## How `--full` works

1. `article` fetches the page over plain HTTP and parses the excerpt as usual.
2. With `--full`, it reads `SA_USER_UID` from the stored session, signs the
   plaintext in-process, and calls `checkAuthByIdJsonp` over its own HTTP client,
   following `totalPage` to the end.
3. It parses `content` into paragraphs.

No browser, no page scraping, no bundled Chromium. Measured 3.1s end to end.

### The one-time bootstrap

If no key is configured, `--full` falls back to driving an installed Chrome or
Edge once: it loads the article page with the session, lifts the key out of the
bundle, caches it, and every later run is plain HTTP. The extraction hooks the
place where the pieces are joined, so it is tied to how the bundle is built and
may stop matching; when that happens the page's own `createSign` still answers
and is used for that run, so `--full` degrades to the old behaviour instead of
breaking.

| situation | what happens |
|---|---|
| key configured (env or cached) | plain HTTP, no browser, any platform |
| no key, Chrome or Edge installed | found automatically including the standard Windows and macOS install paths, which are not on `PATH`; key cached for next time |
| no key, browser in an unusual place | set `CAIXIN_BROWSER=/path/to/chrome` |
| no key, container or headless server | set `CAIXIN_SIGNING_KEY`, or point `CAIXIN_BROWSER_WS` / `--browser-ws` at a running browser |
| none of the above | `E_CONFIG` (exit 4) naming every fix — never a silent downgrade to the excerpt |

`CAIXIN_SIGNING_KEY` is the better answer for containers: the bootstrap loads an
article page with your session, so a remote browser necessarily receives those
cookies. Supplying the key directly avoids the question. Point `CAIXIN_BROWSER_WS`
at your own sidecar only, never a shared endpoint.

## Honesty rules this path must keep

- `complete` is true only when the full-text fetch actually succeeded.
- `source_mode` states which path produced the body: `server_html` or
  `signed_api`.
- When `--full` is requested and no key can be obtained, the command fails with
  an actionable error rather than quietly returning the excerpt — a caller who
  asked for the full article must never be handed the lede without being told.
- Without a session there is no `uid` to bind to, so `--full` fails `E_AUTH`
  rather than `E_CONFIG`: sending the user to install a browser they do not need
  would be the wrong fix.
- The `attr` marker from the response is surfaced, so an unentitled article is
  reported as such rather than inferred from a short body.
