// Package caixincli is the module root. It exists to embed CHANGELOG.md, which
// The embed directive can only read from the directory holding this file.
package caixincli

import _ "embed"

// ChangelogMarkdown is embedded from CHANGELOG.md, the single hand-maintained
// changelog source. Release notes and the runtime `changelog` command are both
// derived from it, so the two can never disagree (REPO-SPEC §4).
//
//go:embed CHANGELOG.md
var ChangelogMarkdown string
