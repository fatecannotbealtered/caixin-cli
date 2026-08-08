package cmd

import (
	"os"
	"testing"
)

// TestMain pins the secret backend to the encrypted file for the whole package.
//
// Without this, `go test` would create and delete entries in the developer's
// real OS credential store -- on Windows that is Credential Manager, and on
// macOS it can raise an unlock prompt mid-run. The file backend exercises the
// same seal/open path, so coverage is unaffected.
func TestMain(m *testing.M) {
	os.Setenv("CAIXIN_SECRET_BACKEND", "file")
	// REPO-SPEC §4: a compiled test binary must not read or write the real user
	// update cache. Without this, running the suite would plant an advisory in
	// the developer's config directory.
	cache, err := os.MkdirTemp("", "caixin-update-cache")
	if err != nil {
		panic(err)
	}
	os.Setenv("CAIXIN_UPDATE_CACHE_DIR", cache)
	code := m.Run()
	os.RemoveAll(cache)
	os.Exit(code)
}
