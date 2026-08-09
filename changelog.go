// Package caixincli embeds repository metadata used by the runtime.
package caixincli

import (
	_ "embed"
	"encoding/json"
)

// packageJSON is the release manifest and the runtime version source.
//
//go:embed package.json
var packageJSON string

// Version is read from the embedded package manifest so source builds, release
// builds, context, doctor, and changelog all report the same value.
var Version = func() string {
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(packageJSON), &manifest); err != nil || manifest.Version == "" {
		panic("invalid embedded package.json version")
	}
	return manifest.Version
}()

// ContractJSON is the vendored canonical machine contract.
//
//go:embed contract/contract.json
var ContractJSON []byte

// ChangelogMarkdown is embedded from CHANGELOG.md, the single hand-maintained
// changelog source. Release notes and the runtime `changelog` command are both
// derived from it, so the two can never disagree (REPO-SPEC §4).
//
//go:embed CHANGELOG.md
var ChangelogMarkdown string
