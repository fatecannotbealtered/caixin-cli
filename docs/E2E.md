# End-to-end testing

## There is no disposable environment

Caixin has no sandbox, no test tenant, and no way to create disposable data. The
only environment is the live site, read with a real subscription. That shapes
everything below.

Every upstream command is read-only, so an E2E run cannot mutate anything at
Caixin. `logout` is the one local credential write and requires the standard
dry-run/confirm pair. Live testing still risks rate limiting and exposure of the
account session.

## Layers

| Layer | What it covers | Network | Runs in CI |
|---|---|---|---|
| Unit | parsing, routing, signature construction, schema guards | none | yes |
| Mock upstream | every command's success, bad-args, auth-failure, and upstream-failure paths against an in-process HTTP server | none | yes |
| Fixture replay | the recorded cassette corpus replayed against declared commands and compared with the reference implementation | none | yes |
| Live smoke | declared commands against the real site with a signed-in account | yes | **no** |

Only the last one needs a real account. Both gates are now met, so
`release_readiness.level` is `stable`: the coverage guard enumerates every leaf
command and finds none uncovered, and `npm run live-smoke` reaches 37 of 40
against the real site.

Note what the mock layer cannot do. It answers with shapes this repo wrote
itself, so it proves the envelope, the error mapping, and the exit codes, but it
can only ever confirm its own assumptions. It stayed green while nobody could
sign in, because it has no cookies; and while the signed full-text endpoint
answered 401 to a signed-in caller, because it does not model two hosts.

## Running the live smoke

```bash
npm run live-smoke                        # needs a signed-in account
npm run live-smoke -- --bin ./caixin-cli  # against a locally built binary
```

The script reads the command list and the declared `output_schema` from
`reference`, harvests urls from live listings, classifies them with the tool's
own `route`, falls back to the runnable example `reference` declares, and fails
when a payload carries a field the contract does not declare. No url is ever
invented: an invented one would test the refusal path and be counted as
coverage. `E_NOT_FOUND` and `E_FORBIDDEN` are recorded as answers, not faults --
reporting them is what this tool is for.

Beyond shape, it asserts the capability the tool exists for: a paywalled article
read through the signed endpoint comes back `complete: true`.

It writes `live-smoke-report.json` — command names, outcomes, and field names
only, with no urls and no article text, so the report is safe to keep with a
release. It is gitignored: the script is the reproducible part, a report is one
run's evidence.

Not run: `login`, `login-resume`, and `logout`, which need a human at the app or
destroy the session the rest of the run depends on.

## Running it by hand

Requires a signed-in state directory. It is a manual step, deliberately:

```bash
caixin-cli status --compact          # confirm a session is loaded
caixin-cli doctor --compact          # confirm signing_key if --full is in scope

# Then, per command, compare the payload keys against the declared schema:
caixin-cli reference --compact       # read output_schema.fields per command
caixin-cli latest --limit 2 --compact
caixin-cli article <url> --full --compact
```

A command whose live `data` keys differ from its declared `output_schema.fields`
is a finding in the tool, not in the site: fix the schema to what was measured,
and record the date in [COMPATIBILITY.md](COMPATIBILITY.md).

## Rules for a live run

- **Never in CI.** It would need the account session as a secret, and would turn
  a third party's uptime into this repo's build status.
- **Keep it small.** A handful of items per command with `--limit 2`. This is a
  compatibility check, not a crawl; bulk archival is out of bounds.
- **Never paste output into an issue or a transcript.** Payloads carry article
  text and, in `context`, account-shaped fields. Sanitize before sharing.
- **The session never leaves the machine.** In particular, do not point
  `CAIXIN_BROWSER_WS` at a browser you do not control: the signing-key bootstrap
  loads a page with your cookies. `CAIXIN_SIGNING_KEY` avoids the question.

## What a failing live run means

An `E_SERVER`, a parse failure, or a schema mismatch usually means Caixin
changed a payload. Confirm by fetching the endpoint directly before touching the
code, then update the parser, the `output_schema`, and the verified date in
`COMPATIBILITY.md` together.
