# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.0.0] - 2026-08-09

### Fixed

- Login worked for nobody. The QR status endpoint will not find a code it is not
  also handed as a cookie: the login page sets `LOGIN_QR_CODE` the moment it
  mints one, and the query parameter alone answers "二维码不存在" however fresh
  the code is. Reproduced against the endpoint directly, same code with and
  without the cookie. The mock layer has no cookies, so it could not have caught
  this.

### Changed

- Semantic timestamp fields are normalized recursively to RFC3339 UTC. Partial
  display text without enough date information now uses a `*_label` field.
- `release_readiness` now reports `beta` with `fcc_status: verified` instead of
  `unpublishable`. The command-level coverage guard enumerates all 40 leaf
  commands from `reference` and finds none uncovered; because that guard only
  enforces while the claim says `verified`, the claim and the evidence now hold
  each other up. Live smoke remains a one-off record rather than a repeatable
  gate, which is what keeps this short of `stable`.

### Fixed

- `newscroll`, `bloggers-directory`, the three opinion directories,
  `opinion-author`, and `video-section` now support positive `--limit` values
  and report `count`, `has_more`, `next_page`, and `truncated` when applicable.

### Security

- `doctor` gained a `plaintext_credentials` check, and `logout` now removes
  every plaintext credential file it knows about rather than only the one the
  current build writes. A state directory carried over from the reference
  implementation still held the session, the browser storage state, and a
  pending QR login in the clear; nothing read them any more, so they were pure
  exposure and neither command would have cleared them.

### Added

- Nine page-reading commands complete the HTML surface: `topic` (the deepview
  and Key surfaces), `microsite`, `datanews-interactive`, `esg30-resource`,
  `video-section`, `opinion-columns`, `opinion-upfront`,
  `opinion-author-directory`, and `opinion-author`. Each works from an allowlist
  of measured entry points; five of them additionally require the parent
  directory to be currently listing the url and stop with `E_NOT_FOUND` when it
  is not.
- `snapshot` now extracts every entry point in its allowlist rather than the two
  it had measured: the front page, the six news channels, the nineteen category
  pages, mini, ESG, the topic directory, the newsletter, 金融我闻, the English
  edition, the blog index, the three magazine fronts, and the culture, opinion,
  photo and video channels.
- Microsites and campaign pages are read through the page's own CSS. Inline
  stylesheets are parsed to work out what a reader would have seen, and a rule
  the parser cannot resolve is treated as hiding everything rather than as
  visible -- claiming a hidden block was on screen is a worse error than
  reporting nothing.
- Continuation pages (`video-section --page`, the two 观点 directories,
  `opinion-author --page`) are fetched only when asked for by number. Each first
  reads the page's own parameters and checks the external script still builds
  the request this build reproduces; a changed contract stops the read instead
  of returning a plausible list of the wrong thing.

### Changed

- The browser is now a one-time bootstrap rather than a per-request dependency.
  `--browser-ws` is only consulted when no key is configured.
- `article --full` without a session now fails `E_AUTH` instead of `E_CONFIG`:
  the signature binds to the account id, so a browser would not have helped.

### Fixed

- Self-update could never have found its own release. `.goreleaser.yml`
  publishes `<tool>-<version>-<os>-<arch>` but the updater looked for
  `<tool>_<version>_<os>_<arch>`; the two are now pinned together by a test that
  reads the release template rather than restating it. The previous test
  asserted the wrong convention, so it had locked the bug in.
- `update --check` reported a repository with no published release as
  `E_NETWORK` (retryable). GitHub answering 404 is a definite answer, so the
  check now succeeds with nothing available, and `update` itself reports
  `E_NOT_FOUND` rather than a retryable failure.
- The npm packaging step could not be rehearsed outside CI. It shelled out to
  `unzip` or to whichever `tar` was first on PATH, and an absolute Windows path
  reads as a remote-host spec to GNU tar. Release zips are now read with Node's
  own zlib, so the step needs no external tool and runs anywhere.


- `route` named several adapters with an internal spelling (`section_directory`,
  `topic_directory`, `blog_author`, `culture`) that did not match the command a
  caller has to run. Every verdict now names the command itself.
- `route` misclassified four url families: magazine issues written with
  `/index.html`, the `/m/` mobile spelling of an article, the app's three topic
  host aliases, and the rolling-news page. The economic data page now reports the
  `independent_product` boundary instead of being routed to `section-directory`,
  which could only have failed.
- Text extraction treated `&nbsp;` as a visible character, so summaries carrying
  one differed from what the page shows.
- `section-directory` discovered its parent by scanning every link on the front
  page. It now reads the same section list `snapshot` publishes, so "listed on
  the front page" means one thing in both commands.

- `public-directory`, `section-directory` and `esg30-subdirectory` read the
  standing index pages. Both directory commands enforce the discovery rule
  their route verdict advertises: they fetch the parent listing first and
  refuse a target the parent is not currently publishing, rather than letting a
  caller deep-link past it.
- Routing gained a boundary taxonomy. An unsupported url now says *why*
  (`download_asset`, `media_asset`, `external`, `mobile_app`,
  `transaction_or_product_detail`, `independent_product`,
  `unsupported_caixin_url`), because "no adapter for this" and "this is a PDF"
  call for different agent behaviour.
- `login` / `login-resume` — QR sign-in. `login` writes the QR image and stops
  with `E_HUMAN_REQUIRED` (exit 9); `login-resume` checks once. Neither polls:
  the reference implementation's `qr-wait` looped on a deadline, which blocks an
  agent on a human's schedule and makes the timeout the tool's choice rather
  than the caller's (CLI-SPEC §16.3). This closes the gap where a fresh install
  had no way to obtain a session at all.
- `entitlements` — what the signed-in account may actually read, from the
  account service rather than inferred from a fetch succeeding. It reads the
  main subscription, the per-feature grant catalog, and the single-article
  purchase log, because news access can come from any of the three.
- Standalone-binary self-update verifies the release **in-process** before it
  replaces anything (SEC-SPEC §5): the Sigstore protobuf bundle over
  `checksums.txt` is checked with `sigstore-go` against a TUF-bootstrapped trust
  root, the signer identity is pinned by an anchored regexp to this repo's
  tagged release workflow and GitHub's OIDC issuer, then the archive SHA256 is
  checked against the now-trusted manifest. No external cosign, nothing
  preinstalled, and no skip path — a missing bundle, a bad signature, an
  unlisted asset, or a digest mismatch all fail closed as the non-retryable
  `E_INTEGRITY`. The installed binary is untouched until both links pass, and
  the swap is atomic.
- `caixin-cli update` — the single-command self-update the template treats as core
  equipment (CLI-SPEC §14, REPO-SPEC §4). No confirm token, no leaf subcommands:
  it resolves the release, drives the install method, and syncs the whole Skill
  directory in one call. `--check` and `--dry-run` are read-only probes.
- Version notifications as a structured contract: `update --check` refreshes a
  local notice cache, severity is graded from the embedded CHANGELOG delta
  (`warning` on a security entry or major bump, else `info`), and any command
  can surface the cached notice through `meta.notices` — read from a file,
  never a network call.
- npm-managed installs are upgraded by driving `npm install -g <pkg>@<version>`
  rather than mutating a file the manager owns or printing a command for the
  user to run. The idempotent no-op check runs first, so an already-current
  install never shells out.
- `snapshot` reads a Caixin channel front page as the server rendered it, with
  the `datanews` template converged byte-for-byte against the reference
  implementation's recorded golden. The shared editorial core it sits on --
  class-token XPath matching, item extraction, url and image allowlists with
  https normalization, server-declared visibility, navigation extraction, click
  consumers -- is what the remaining page templates will reuse.
- Routing gained the `datanews-interactive` and `public-directory` adapters.
  Both were previously misclassified as `section-directory`, so `route` returned
  the wrong command for those urls.
- Credentials are encrypted at rest (SEC-SPEC §4). Secrets are sealed with
  AES-256-GCM; the 32-byte data key comes from the OS keyring where one exists
  (Windows Credential Manager / macOS Keychain / Linux Secret Service) and from
  machine-bound PBKDF2-SHA256 derivation where none does. The keyring holds the
  key rather than the payload because a Windows credential blob caps at 2560
  bytes and a cookie jar is larger than that.
- `context.data.credentials.storage` and the `doctor` `credentials` check report
  the live backend (`keyring` / `encrypted-file`), so a degraded install is
  visible rather than silent.
- `CAIXIN_SECRET_BACKEND=file` forces the fallback backend; the test suites set it
  so `go test` never writes to a real credential store.

- `_untrusted` now names the externally-controlled fields (`["articles"]`,
  `["title","author","paragraphs","images"]`, …) instead of asserting a bare
  `true`, as SEC-SPEC §2 requires. The list is generated from each command's
  declared `output_schema`, so the runtime marker and `reference` cannot drift.
- `reference` declares positional arguments in `params[]`. Seven commands took a
  required positional (`article <url>`, `search <keyword>`, …) that appeared
  nowhere in the structured contract — an agent could only infer it from an
  example string.
- `bloggers-directory` reports `duplicates_dropped`.
- `docs/COMPATIBILITY.md` and `docs/E2E.md`.
- CI runs `scripts/check-spec.js`, so vendored spec drift and a stale
  `contract_gen.go` fail the build rather than only a local run.
- `article --full` computes the request signature in-process, so full text no
  longer needs a browser. The scheme is RSASSA-PKCS1-v1_5 SHA-256 over
  `id=<id>&uid=<uid>&<nonce>=nonce`, verified byte-for-byte against the site's
  own `createSign`. See `docs/FULL-TEXT.md`.
- `CAIXIN_SIGNING_KEY` supplies the signing key on hosts with no browser;
  otherwise it is extracted once from an installed Chrome or Edge and cached to
  `<state-dir>/signing-key.pem`.
- `context.credentials.signing_key` and a `doctor` `signing_key` check report
  whether this host can read full text without a browser.


- Routing had four defects that `route` was already returning to callers:
  the data-topic directory links three url shapes and only one was recognized,
  dropping 41 of 78 entries; `/2016/fang` was claimed by the microsite pattern;
  a url whose `#/` fragment is meaningful had it stripped by generic
  normalization; and the photo column's static pagination had no adapter.
- `section-directory` was a catch-all for any unrecognized Caixin url, so an
  agent following `route` got a command that could only fail. It is now scoped
  to real channel sections, and `snapshot` likewise honours its own allowlist
  instead of matching any bare host.
- The `/m/` mobile article alias is folded to the canonical path, so one piece
  no longer appears as two entries in a listing.
- `package-lock.json` was missing while `SECURITY.md` claimed the lockfile was
  committed. It is committed now, and CI installs with `npm ci` so the audit
  resolves exactly the tree a release ships.
- `SECURITY_zh.md` had never been updated alongside its English counterpart: it
  still described a config file, environment variables, and an interactive
  secret prompt that do not exist. Rewritten to match.
- `reference` declared `update` as a `read` command. It replaces the binary and
  rewrites the Skill directory, so it now declares `self-update` and no longer
  understates its blast radius.
- `context` now carries the cached update notice in `data`, which the
  notification contract requires of active-check commands; it is still read
  from the local cache and never from the network.
- The template treats `update` as a core lifecycle command; this build had
  removed it from the docs instead of implementing it, so the Skill, both
  READMEs and SECURITY.md all told agents it did not exist. It exists now and
  those documents describe it again.
- The session was stored as plaintext JSON in the state directory, and
  `SECURITY.md` claimed it was encrypted. It is now genuinely encrypted, and a
  session left by an earlier build is sealed on first read with the plaintext
  original deleted. Any copy that left the machine before this upgrade should be
  treated as compromised.

- `bloggers-directory` returned no data at all: it read `url` from records whose
  field is `authorUrl`, so every author came back with an empty url, every row
  after the first looked like a duplicate, and the command failed the whole
  request with a **retryable** `E_SERVER` — an error that reproduced on every
  retry. It now returns all 20 authors with real urls.
- `release_readiness.reason` claimed `article` was not implemented and not
  declared, while it was both. That claim had also been copied into the Skill,
  where it told agents to refuse a supported request.
- `SKILL.md` said the build "cannot read article body text" and its eval
  scenarios told agents to say so. Both now describe `article` and `--full`.
- `SKILL.md`, `test-prompts.json`, `README.md`, `README_zh.md`, and
  `SECURITY.md` documented an `update` command that does not exist; running it
  returned `E_USAGE`.
- `README.md` / `README_zh.md`: the Agent Install block exported `CAIXIN_HOST`
  and `CAIXIN_TOKEN`, neither of which the tool reads; Configuration pointed at
  `~/.caixin-cli/config.json`, which does not exist; the Capabilities table was
  still the template's "Replace with the main command groups this tool exposes";
  and both described a `--dry-run` → `--confirm` write flow for a tool with no
  write commands. `README_zh.md` additionally had untranslated English spliced
  into Chinese sentences.
- `SECURITY.md` claimed credentials were encrypted at rest with AES-256-GCM.
  They are not: the session is plaintext JSON in the state directory. The
  document now says so plainly instead of overstating the protection.
- `docs/FULL-TEXT.md` described the signature as randomized and as not
  reimplementable. Both were wrong: the variation was the per-request nonce, and
  the scheme is standard.

### Deprecated


### Removed


### Security

- The `article --full` signing key is third-party key material and is
  deliberately absent from this repository (`SEC-SPEC.md` §4,
  `docs/OPEN_SOURCE_CHECKLIST.md`). `.gitignore` now also refuses `*.pem`,
  `*.key`, and `signing-key*` as a backstop.
- `SECURITY.md` no longer claims encryption at rest that is not implemented.
  Overstating a control is worse than declaring its absence.

<!--
Copy the block below for each release. Newest version first.
Keep the link references at the bottom of the file in sync.

## [1.0.0] - YYYY-MM-DD

### Added

- First public release.

### Changed

### Fixed

### Deprecated

### Removed

### Security

[Unreleased]: https://github.com/fatecannotbealtered/caixin-cli/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/fatecannotbealtered/caixin-cli/releases/tag/v1.0.0
-->
