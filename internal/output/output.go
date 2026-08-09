// Package output implements the CLI's machine-readable stdout contract.
//
// stdout carries exactly one JSON document per invocation; progress and
// human-readable error text go to stderr. Success and failure share one
// envelope shape so an agent only has to branch on `ok` (CLI-SPEC §3-§4).
package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fatecannotbealtered/caixin-cli/internal/contract"
)

const (
	FormatJSON = "json"
	FormatText = "text"
	FormatRaw  = "raw"
)

// Hints is the actionable next step per error code. Agents branch on
// `error.code`; the hint is for the human reading stderr and rides along in
// `error.details.hint`.
var Hints = map[string]string{
	// This build has no login command: every declared command reads a public
	// endpoint. A 401/403 therefore means the endpoint started requiring a
	// session, not that the caller forgot to log in.
	"E_AUTH": "This endpoint now requires a Caixin session. This build reads public " +
		"endpoints only and has no login command; use an already-populated state " +
		"directory, or the reference implementation.",
	"E_FORBIDDEN":      "The subscription does not entitle this account to that content. A successful login does not imply paid full-text access.",
	"E_NOT_FOUND":      "Verify the url or code from a fresh directory, search, or list result.",
	"E_VALIDATION":     "Check command arguments; entry urls must match the documented allowlist.",
	"E_USAGE":          "Check command arguments.",
	"E_CONFLICT":       "State changed since the preview. Re-read the resource and retry.",
	"E_NETWORK":        "Check network connectivity and proxy settings.",
	"E_RATE_LIMITED":   "Too many requests. Back off; do not poll faster than every 2 seconds.",
	"E_SERVER":         "Caixin returned a server error. Retry later.",
	"E_TIMEOUT":        "The request timed out. Back off before retrying.",
	"E_HUMAN_REQUIRED": "A person must act: scan the login QR, or clear a CAPTCHA/device check on Caixin's own site. Relay it; do not retry automatically.",
	"E_INTEGRITY":      "Release integrity verification failed; do not retry. Report a possible supply-chain issue.",
	"E_IO":             "Local filesystem failure. Fix the environment, then retry.",
	"E_INTERRUPTED":    "Cancelled by signal or user. Nothing is left half-applied; re-run the command.",
	"E_UNKNOWN":        "Inspect error.details for more context.",
}

// Options controls one command invocation's output.
type Options struct {
	Format    string
	Compact   bool
	Fields    []string
	StartedAt time.Time
	Notices   []map[string]any
	// Untrusted names the payload fields carrying externally-controlled
	// content. It is stamped onto `data._untrusted` so an agent can tell which
	// values are data rather than instructions (SEC-SPEC §2).
	Untrusted []string
}

// Printer writes exactly one command result to stdout.
type Printer struct {
	out     io.Writer
	options Options
}

func NewPrinter(out io.Writer, options Options) *Printer {
	if options.Format == "" {
		options.Format = FormatJSON
	}
	return &Printer{out: out, options: options}
}

// meta is emitted on every response, success or failure. duration_ms is never
// omitted: 0 is a legal value and an agent must always be able to read it.
type meta struct {
	DurationMS int64            `json:"duration_ms"`
	Notices    []map[string]any `json:"notices,omitempty"`
}

type successEnvelope struct {
	OK            bool   `json:"ok"`
	SchemaVersion string `json:"schema_version"`
	Data          any    `json:"data"`
	Meta          meta   `json:"meta"`
}

type errorObject struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details"`
	Retryable bool           `json:"retryable"`
}

type errorEnvelope struct {
	OK            bool        `json:"ok"`
	SchemaVersion string      `json:"schema_version"`
	Error         errorObject `json:"error"`
	Meta          meta        `json:"meta"`
}

// CLIError is a stable E_* error carrying its canonical exit and retry semantics.
type CLIError struct {
	Code    string
	Message string
	Details map[string]any
	Cause   error
}

func NewError(code, message string, details map[string]any) *CLIError {
	if _, ok := contract.Codes[code]; !ok {
		code = "E_UNKNOWN"
	}
	if details == nil {
		details = map[string]any{}
	}
	if hint, ok := Hints[code]; ok {
		if _, exists := details["hint"]; !exists {
			details["hint"] = hint
		}
	}
	return &CLIError{Code: code, Message: message, Details: details}
}

func WrapError(code, message string, cause error, details map[string]any) *CLIError {
	err := NewError(code, message, details)
	err.Cause = cause
	return err
}

func (e *CLIError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *CLIError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *CLIError) ExitCode() int {
	if e == nil {
		return 0
	}
	return contract.ExitFor(e.Code)
}

func (p *Printer) commandMeta() meta {
	duration := int64(0)
	if !p.options.StartedAt.IsZero() {
		duration = time.Since(p.options.StartedAt).Milliseconds()
	}
	return meta{DurationMS: duration, Notices: p.options.Notices}
}

func (p *Printer) Success(data any) error {
	if p.options.Format == FormatRaw {
		return errors.New("raw output requires Raw()")
	}
	normalized, err := normalizeIDFields(data)
	if err != nil {
		return err
	}
	normalized = normalizeSemanticTimes(normalized)
	annotated, err := annotateUntrusted(normalized, p.options.Untrusted)
	if err != nil {
		return err
	}
	projected, err := projectFields(annotated, p.options.Fields)
	if err != nil {
		return err
	}
	if p.options.Format == FormatText {
		return p.writeJSON(projected)
	}
	return p.writeJSON(successEnvelope{
		OK:            true,
		SchemaVersion: contract.SchemaVersion,
		Data:          projected,
		Meta:          p.commandMeta(),
	})
}

func (p *Printer) Failure(cliErr *CLIError) error {
	if cliErr == nil {
		cliErr = NewError("E_UNKNOWN", "unknown error", nil)
	}
	if p.options.Format == FormatText {
		_, err := fmt.Fprintf(p.out, "%s: %s\n", cliErr.Code, cliErr.Message)
		return err
	}
	normalized, err := normalizeIDFields(cliErr.Details)
	if err != nil {
		return err
	}
	normalized = normalizeSemanticTimes(normalized)
	details, _ := normalized.(map[string]any)
	return p.writeJSON(errorEnvelope{
		OK:            false,
		SchemaVersion: contract.SchemaVersion,
		Error: errorObject{
			Code:      cliErr.Code,
			Message:   cliErr.Message,
			Details:   details,
			Retryable: contract.Retryable(cliErr.Code),
		},
		Meta: p.commandMeta(),
	})
}

func normalizeIDFields(data any) (any, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return normalizeIDValue(value), nil
}

func normalizeIDValue(value any) any {
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			if isIDField(key) {
				child = stringifyIDValue(child)
			}
			current[key] = normalizeIDValue(child)
		}
	case []any:
		for index, child := range current {
			current[index] = normalizeIDValue(child)
		}
	}
	return value
}

func isIDField(key string) bool {
	switch key {
	case "id", "enid", "uid", "pid", "sid", "uuid":
		return true
	}
	return strings.HasSuffix(key, "_id") || strings.HasSuffix(key, "_enid") ||
		strings.HasSuffix(key, "_ids") || strings.HasSuffix(key, "Id") || strings.HasSuffix(key, "ID")
}

func stringifyIDValue(value any) any {
	switch current := value.(type) {
	case json.Number:
		return current.String()
	case []any:
		for index, item := range current {
			current[index] = stringifyIDValue(item)
		}
		return current
	default:
		return value
	}
}

var (
	fullChineseTime = regexp.MustCompile(`\d{4}年\d{2}月\d{2}日(?: \d{2}:\d{2}(?::\d{2})?)?`)
	fullPlainTime   = regexp.MustCompile(`\d{4}-\d{2}-\d{2}(?: \d{2}:\d{2}(?::\d{2})?)?`)
)

// normalizeSemanticTimes keeps machine timestamp fields unambiguous. Complete
// timestamps become RFC3339 UTC; partial labels without a year or date are
// retained under a non-time key instead of pretending to be timestamps.
func normalizeSemanticTimes(value any) any {
	switch current := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(current))
		for key := range current {
			keys = append(keys, key)
		}
		for _, key := range keys {
			current[key] = normalizeSemanticTimes(current[key])
		}
		for _, key := range keys {
			canonical, label, ok := semanticTimeField(key)
			if !ok {
				continue
			}
			raw := current[key]
			delete(current, key)
			if raw == nil {
				current[canonical] = nil
				continue
			}
			if timestamp, ok := semanticTimestamp(raw); ok {
				current[canonical] = timestamp
				continue
			}
			if _, exists := current[label]; !exists {
				current[label] = raw
			}
		}
	case []any:
		for index, child := range current {
			current[index] = normalizeSemanticTimes(child)
		}
	}
	return value
}

func semanticTimeField(key string) (canonical, label string, ok bool) {
	switch {
	case strings.HasSuffix(key, "_at_ms"):
		canonical = strings.TrimSuffix(key, "_ms")
	case strings.HasSuffix(key, "_at_unix"):
		canonical = strings.TrimSuffix(key, "_unix")
	case strings.HasSuffix(key, "_at"):
		canonical = key
	case key == "start_time" || key == "end_time":
		canonical = key
		return canonical, strings.TrimSuffix(key, "_time") + "_label", true
	default:
		return "", "", false
	}
	return canonical, strings.TrimSuffix(canonical, "_at") + "_label", true
}

func semanticTimestamp(value any) (string, bool) {
	text := strings.TrimSpace(fmt.Sprint(value))
	if number, ok := value.(json.Number); ok {
		text = number.String()
	}
	if len(text) >= 10 && len(text) <= 13 {
		if epoch, err := strconv.ParseInt(text, 10, 64); err == nil && epoch > 0 {
			if len(text) == 13 {
				return time.UnixMilli(epoch).UTC().Format(time.RFC3339), true
			}
			if len(text) == 10 {
				return time.Unix(epoch, 0).UTC().Format(time.RFC3339), true
			}
		}
	}
	if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
		return parsed.UTC().Format(time.RFC3339), true
	}

	candidate := text
	if matched := fullChineseTime.FindString(text); matched != "" {
		candidate = matched
	} else if matched := fullPlainTime.FindString(text); matched != "" {
		candidate = matched
	}
	for _, layout := range []string{"2006-01-02", "2006年01月02日"} {
		if parsed, err := time.Parse(layout, candidate); err == nil {
			return parsed.UTC().Format(time.RFC3339), true
		}
	}
	china := time.FixedZone("China Standard Time", 8*60*60)
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006年01月02日 15:04:05",
		"2006年01月02日 15:04",
	} {
		if parsed, err := time.ParseInLocation(layout, candidate, china); err == nil {
			return parsed.UTC().Format(time.RFC3339), true
		}
	}
	return "", false
}

func (p *Printer) Raw(data []byte) error {
	if p.options.Format != FormatRaw {
		return errors.New("Raw() requires --format raw")
	}
	_, err := p.out.Write(data)
	return err
}

func (p *Printer) writeJSON(value any) error {
	var (
		encoded []byte
		err     error
	)
	if p.options.Compact {
		encoded, err = json.Marshal(value)
	} else {
		encoded, err = json.MarshalIndent(value, "", "  ")
	}
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	_, err = p.out.Write(encoded)
	return err
}

// annotateUntrusted stamps the declared untrusted field list onto the payload.
//
// SEC-SPEC §2 wants `_untrusted` to *name* the externally-controlled fields, not
// merely assert that some exist: an agent has to know which values to quarantine.
// The list comes from the command's declared output schema, so what a caller
// reads here and what `reference` promises cannot drift apart.
func annotateUntrusted(data any, fields []string) (any, error) {
	if len(fields) == 0 {
		return data, nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		// A non-object payload has no place to carry the marker. That is a
		// schema declaration bug rather than a runtime one, and a guard test
		// catches it; returning the payload unchanged keeps the command usable.
		return data, nil
	}
	object["_untrusted"] = fields
	return object, nil
}

func projectFields(data any, fields []string) (any, error) {
	if len(fields) == 0 {
		return data, nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("--fields requires an object result: %w", err)
	}
	projected := make(map[string]any, len(fields))
	for _, rawField := range fields {
		field := strings.TrimSpace(rawField)
		if field == "" {
			continue
		}
		value, ok := object[field]
		if !ok {
			return nil, fmt.Errorf("unknown output field %q", field)
		}
		projected[field] = value
	}
	if marked, ok := object["_untrusted"].([]any); ok {
		kept := make([]any, 0, len(marked))
		for _, value := range marked {
			field, ok := value.(string)
			if !ok {
				continue
			}
			if _, present := projected[field]; present {
				kept = append(kept, field)
			}
		}
		if len(kept) > 0 {
			projected["_untrusted"] = kept
		} else {
			delete(projected, "_untrusted")
		}
	}
	return projected, nil
}

// CodeForStatus maps an upstream HTTP status onto a stable error code. One
// function owns this mapping so the status -> code -> exit contract cannot
// drift between the transport and command layers (CLI-SPEC §6).
func CodeForStatus(status int) string {
	switch status {
	case 401:
		return "E_AUTH"
	case 403:
		return "E_FORBIDDEN"
	case 404:
		return "E_NOT_FOUND"
	case 408:
		return "E_TIMEOUT"
	case 409:
		return "E_CONFLICT"
	case 429:
		return "E_RATE_LIMITED"
	}
	switch {
	case status >= 500 && status <= 599:
		return "E_SERVER"
	case status >= 400 && status <= 499:
		return "E_VALIDATION"
	default:
		return "E_SERVER"
	}
}
