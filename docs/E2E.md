# End-to-end testing

## There is no disposable environment

Caixin has no sandbox, no test tenant, and no way to create disposable data. The
only environment is the live site, read with a real subscription. That shapes
everything below.

The good news is that the blast radius is nil: every upstream command is
read-only, and the tool has no write commands at all. An E2E run cannot mutate
anything at Caixin. The risk is entirely on the other side — rate limiting, and
exposing the account session.

## Layers

| Layer | What it covers | Network | Runs in CI |
|---|---|---|---|
| Unit | parsing, routing, signature construction, schema guards | none | yes |
| Mock upstream | every command's success, bad-args, auth-failure, and upstream-failure paths against an in-process HTTP server | none | yes |
| Fixture replay | twelve commands replayed against a recorded cassette corpus and compared byte-for-byte with the reference implementation | none | yes |
| Live smoke | every declared command against the real site with a signed-in account | yes | **no** |

Only the last one needs a real account, and it is the reason
`release_readiness.level` is `beta`. The `reason` field in `caixin-cli
reference` says so in the same words.

## Running the live smoke

Requires a signed-in state directory. It is a manual step, deliberately:

```bash
caixin-cli status --compact          # confirm a session is loaded
caixin-cli doctor --compact          # confirm signing_key if --full is in scope

# Then, per command, compare the payload keys against the declared schema:
caixin-cli reference --compact       # read output_schema.fields per command
caixin-cli latest --size 2 --compact
caixin-cli article <url> --full --compact
```

A command whose live `data` keys differ from its declared `output_schema.fields`
is a finding in the tool, not in the site: fix the schema to what was measured,
and record the date in [COMPATIBILITY.md](COMPATIBILITY.md).

## Rules for a live run

- **Never in CI.** It would need the account session as a secret, and would turn
  a third party's uptime into this repo's build status.
- **Keep it small.** A handful of items per command with `--size 2`. This is a
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
