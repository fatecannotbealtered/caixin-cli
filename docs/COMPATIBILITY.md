# Compatibility

`caixin-cli` talks to Caixin's public web endpoints. Caixin publishes no API
contract and no versioning, so "the verified backend version" is a **date**: the
day each endpoint was last observed answering in the shape this tool parses.

## Verified surface

Every declared command was exercised against the live upstream on **2026-08-07**
with a paid, signed-in account, and each `output_schema` in `reference` was
corrected to the payload actually measured that day.

| Area | Endpoint host | Last verified | Notes |
|---|---|---|---|
| Scroll and news feeds | `www.caixin.com`, `gateway.caixin.com` | 2026-08-07 | `latest`, `newscroll` |
| Search | `search.caixin.com` | 2026-08-07 | `search`, `search-menu`; the category and time-range menu is read live, never hardcoded |
| Flash news | `gateway.caixin.com` | 2026-08-07 | `frontline`, `frontline-detail` |
| Topic directories | `topics.caixin.com` | 2026-08-07 | `topics` |
| Caixin Data feeds | `cxdata.caixin.com` | 2026-08-07 | `cxdata-feed`, `entities-preview` |
| Blogger directory | `blog.caixin.com` | 2026-08-07 | `bloggers-directory` |
| Article body | `www.caixin.com`, `gateway.caixin.com` | 2026-08-07 | `article`; `--full` additionally needs the signing key, see [FULL-TEXT.md](FULL-TEXT.md) |

## What "compatible" means here

This tool is a **compatibility client**, not an integration. Caixin can change
any of these endpoints without notice and owes this project nothing. Two
consequences worth stating plainly:

- A shape change surfaces as `E_SERVER` or a parse failure, not as silently
  wrong data. Commands validate the envelope they expect and fail loudly when it
  is not there.
- `reference` is generated from this build, so it always describes what the
  binary does. It cannot describe what the upstream does today. When a command
  starts failing, assume the upstream moved before assuming the tool is broken.

## Re-verifying

`npm run live-smoke` is the automated live gate: it runs every command that does
not need a human against the real site and fails on any payload carrying a field
the contract does not declare. Command-level FCC is enforced by a guard that
enumerates the same command list, so `release_readiness.level` is `stable` (see
the `reason` in `caixin-cli reference` for exactly what that covers).

To re-verify by hand instead, run each command against the live upstream and
compare the top-level keys of `data` against the command's `output_schema.fields`
from `reference`. A mismatch in either direction is a finding.

## Platform support

| Platform | Status |
|---|---|
| Linux x64 / arm64 | supported |
| macOS x64 / arm64 | supported |
| Windows x64 | supported |

Every command is plain HTTP and needs nothing installed. The single exception is
the one-time signing-key bootstrap for `article --full`, which uses a local
Chrome or Edge if no key is configured; `CAIXIN_SIGNING_KEY` avoids it entirely.
