package cmd

import (
	"fmt"
	"strings"
	"testing"
)

func freeMock(t *testing.T) *mockUpstream {
	t.Helper()
	mock := newMockUpstream(t)
	mock.OK("/api/dataplatform/scroll/category", []any{map[string]any{"name": "a"}})
	return mock
}

func TestGlobalFlags_FieldsProjectsData(t *testing.T) {
	got := run(t, freeMock(t), "channels", "--fields", "data")
	if got.Exit != 0 {
		t.Fatalf("exit = %d: %s", got.Exit, got.Stdout)
	}
	data := got.Data(t)
	if _, ok := data["data"]; !ok {
		t.Error("--fields data dropped the requested field")
	}
	marked, ok := data["_untrusted"].([]any)
	if !ok || len(marked) != 1 || marked[0] != "data" {
		t.Errorf("--fields data must preserve _untrusted=[data], got %#v", data["_untrusted"])
	}
}

// An unknown field is a usage error, not a silently empty result: an agent that
// mistypes a projection must find out rather than conclude the data is missing.
func TestGlobalFlags_UnknownFieldIsValidationError(t *testing.T) {
	got := run(t, freeMock(t), "channels", "--fields", "no_such_field")
	if code := got.ErrorCode(t); code != "E_VALIDATION" {
		t.Errorf("code = %s, want E_VALIDATION", code)
	}
	if got.Exit != 2 {
		t.Errorf("exit = %d, want 2", got.Exit)
	}
}

func TestGlobalFlags_CompactRemovesWhitespace(t *testing.T) {
	dir := t.TempDir()
	mock := freeMock(t)

	compact := runCLI(t, mock, "channels", "--state-dir", dir, "--compact")
	pretty := runCLI(t, freeMock(t), "channels", "--state-dir", dir)

	if strings.Contains(compact.Stdout, "\n  ") {
		t.Errorf("--compact still emitted indentation:\n%s", compact.Stdout)
	}
	if !strings.Contains(pretty.Stdout, "\n  ") {
		t.Errorf("default output should be indented:\n%s", pretty.Stdout)
	}
}

func TestGlobalFlags_JSONAliasMatchesFormatJSON(t *testing.T) {
	dir := t.TempDir()
	viaAlias := runCLI(t, freeMock(t), "channels", "--state-dir", dir, "--json", "--compact")
	viaFormat := runCLI(t, freeMock(t), "channels", "--state-dir", dir, "--format", "json", "--compact")

	// duration_ms legitimately differs between runs; everything else must not.
	normalize := func(r result) string {
		envelope := r.Envelope(t)
		delete(envelope, "meta")
		return fmt.Sprint(envelope)
	}
	if normalize(viaAlias) != normalize(viaFormat) {
		t.Errorf("--json and --format json disagree:\n%s\n%s", viaAlias.Stdout, viaFormat.Stdout)
	}
}

func TestGlobalFlags_JSONAliasConflictsWithOtherFormat(t *testing.T) {
	got := run(t, freeMock(t), "channels", "--json", "--format", "text")
	if code := got.ErrorCode(t); code != "E_USAGE" {
		t.Errorf("code = %s, want E_USAGE", code)
	}
	if got.Exit != 2 {
		t.Errorf("exit = %d, want 2", got.Exit)
	}
}

func TestGlobalFlags_RejectsUnknownFormat(t *testing.T) {
	got := run(t, freeMock(t), "channels", "--format", "yaml")
	if code := got.ErrorCode(t); code != "E_USAGE" {
		t.Errorf("code = %s, want E_USAGE", code)
	}
}

// raw is only legal where a command declares it, so an agent cannot ask for an
// unwrapped payload from a command that has none.
func TestGlobalFlags_RawRejectedWhereUnsupported(t *testing.T) {
	got := run(t, freeMock(t), "channels", "--format", "raw")
	if code := got.ErrorCode(t); code != "E_USAGE" {
		t.Errorf("code = %s, want E_USAGE", code)
	}
	if got.Exit != 2 {
		t.Errorf("exit = %d, want 2", got.Exit)
	}
}

func TestGlobalFlags_TextFormatIsNotEnveloped(t *testing.T) {
	got := runCLI(t, freeMock(t), "channels", "--state-dir", t.TempDir(), "--format", "text")
	if got.Exit != 0 {
		t.Fatalf("exit = %d: %s", got.Exit, got.Stdout)
	}
	if strings.Contains(got.Stdout, `"schema_version"`) {
		t.Error("text output must not carry the machine envelope")
	}
}

func TestGlobalFlags_CompactRequiresJSON(t *testing.T) {
	got := run(t, freeMock(t), "channels", "--format", "text", "--compact")
	if code := got.ErrorCode(t); code != "E_USAGE" {
		t.Errorf("code = %s, want E_USAGE", code)
	}
}

// Nothing may precede or follow the JSON document on stdout: no banner, no
// progress line, no trailing note.
func TestGlobalFlags_StdoutIsOnlyTheEnvelope(t *testing.T) {
	got := run(t, freeMock(t), "channels")
	trimmed := strings.TrimSpace(got.Stdout)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		t.Errorf("stdout is polluted around the envelope:\n%q", got.Stdout)
	}
	got.Envelope(t)
}
