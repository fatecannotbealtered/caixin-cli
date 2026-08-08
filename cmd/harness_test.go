package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// result is one captured CLI invocation: the two streams and the exit code an
// agent would actually observe.
type result struct {
	Stdout string
	Stderr string
	Exit   int
}

// Envelope decodes stdout as the machine contract, failing if stdout is not
// exactly one JSON document.
func (r result) Envelope(t *testing.T) map[string]any {
	t.Helper()
	var envelope map[string]any
	decoder := json.NewDecoder(strings.NewReader(r.Stdout))
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, r.Stdout)
	}
	if decoder.More() {
		t.Fatalf("stdout carried more than one JSON document:\n%s", r.Stdout)
	}
	return envelope
}

func (r result) Data(t *testing.T) map[string]any {
	t.Helper()
	envelope := r.Envelope(t)
	if ok, _ := envelope["ok"].(bool); !ok {
		t.Fatalf("expected a success envelope, got: %s", r.Stdout)
	}
	data, _ := envelope["data"].(map[string]any)
	return data
}

func (r result) ErrorCode(t *testing.T) string {
	t.Helper()
	envelope := r.Envelope(t)
	if ok, _ := envelope["ok"].(bool); ok {
		t.Fatalf("expected a failure envelope, got: %s", r.Stdout)
	}
	errorObject, _ := envelope["error"].(map[string]any)
	code, _ := errorObject["code"].(string)
	return code
}

// mockUpstream stands in for Caixin. Handlers are keyed by path, so a test
// declares only the endpoints it cares about; anything else is a 404 the test
// can assert on rather than a silent pass.
type mockUpstream struct {
	server   *httptest.Server
	handlers map[string]http.HandlerFunc
	Requests []string
}

func newMockUpstream(t *testing.T) *mockUpstream {
	t.Helper()
	mock := &mockUpstream{handlers: map[string]http.HandlerFunc{}}
	mock.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mock.Requests = append(mock.Requests, r.Method+" "+r.URL.Path)
		if handler, ok := mock.handlers[r.URL.Path]; ok {
			handler(w, r)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":404,"msg":"no handler"}`))
	}))
	t.Cleanup(mock.server.Close)
	return mock
}

// OK registers a gateway-shaped success: code 0 with `data`.
func (m *mockUpstream) OK(path string, data any) *mockUpstream {
	m.handlers[path] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "ok", "data": data})
	}
	return m
}

// Raw registers a verbatim body, for endpoints that do not use the gateway
// envelope (JSONP, the blogger directory, the entity previews).
func (m *mockUpstream) Raw(path string, body string) *mockUpstream {
	m.handlers[path] = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		out := body
		if callback := r.URL.Query().Get("callback"); callback != "" {
			out = strings.ReplaceAll(out, "__CALLBACK__", callback)
		}
		_, _ = w.Write([]byte(out))
	}
	return m
}

// Status registers a raw HTTP failure so status -> code -> exit is exercised
// end to end.
func (m *mockUpstream) Status(path string, status int) *mockUpstream {
	m.handlers[path] = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":"upstream failure"}`))
	}
	return m
}

// BusinessError registers a 200 carrying a non-zero gateway code.
func (m *mockUpstream) BusinessError(path string, code int, message string) *mockUpstream {
	m.handlers[path] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "msg": message})
	}
	return m
}

// searchMenuData is the scope menu several commands resolve filters against.
func searchMenuData() []any {
	return []any{
		map[string]any{
			"id": 119, "code": "20", "name": "综合", "isShow": 1,
			"sortList": []any{map[string]any{"name": "时间倒序", "value": 0}},
		},
	}
}

// runCLI drives the real command boundary, which is what FCC coverage requires:
// a test that calls an internal helper proves nothing about the contract.
func runCLI(t *testing.T, mock *mockUpstream, args ...string) result {
	t.Helper()
	previous := baseURLOverride
	if mock != nil {
		baseURLOverride = mock.server.URL
	}
	t.Cleanup(func() { baseURLOverride = previous })

	var stdout, stderr bytes.Buffer
	exit := ExecuteArgs(context.Background(), args, strings.NewReader(""), &stdout, &stderr)
	return result{Stdout: stdout.String(), Stderr: stderr.String(), Exit: exit}
}

// run is the common case: an isolated state dir pointed at a mock upstream.
func run(t *testing.T, mock *mockUpstream, args ...string) result {
	t.Helper()
	return runCLI(t, mock, append(args, "--state-dir", t.TempDir(), "--compact")...)
}
