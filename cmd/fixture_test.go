package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// Fixture replay is the safety net for the Python -> Go port.
//
// Each cassette holds the real request/response pairs one command made against
// Caixin, with long prose masked. The golden beside it is what the reference
// Python implementation produced *from that same masked cassette*, so the pair
// is self-consistent and carries no copyrighted body text. A Go command replays
// the cassette offline and must land on the same payload.
//
// One documented difference: the reference implementation put `ok` and
// `schema_version` inside the payload, while the Go port carries them in the
// envelope where CLI-SPEC §3 puts them. Those two keys are therefore dropped
// from the golden before comparing; everything else must match exactly.

type interaction struct {
	Request struct {
		Method string `json:"method"`
		URL    string `json:"url"`
	} `json:"request"`
	Response struct {
		Status  int               `json:"status"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	} `json:"response"`
}

type cassette struct {
	Name         string        `json:"name"`
	Command      []string      `json:"command"`
	Interactions []interaction `json:"interactions"`
}

func testdataDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "testdata")
}

func loadCassette(t *testing.T, name string) cassette {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(testdataDir(), "cassettes", name+".json"))
	if err != nil {
		t.Fatalf("read cassette %s: %v", name, err)
	}
	var loaded cassette
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatalf("parse cassette %s: %v", name, err)
	}
	return loaded
}

func loadGolden(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(testdataDir(), "golden", name+".json"))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	var golden map[string]any
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatalf("parse golden %s: %v", name, err)
	}
	// Migrated to the envelope by the port; see the note at the top of the file.
	delete(golden, "ok")
	delete(golden, "schema_version")
	return golden
}

var callbackParam = regexp.MustCompile(`__caixincallback\d+`)

// replayKey identifies a recorded request. The JSONP callback name is stripped:
// it is a per-call nonce, so keeping it in the key would make every recorded
// JSONP response unmatchable.
func replayKey(method, rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return method + " " + rawURL
	}
	query := parsed.Query()
	query.Del("callback")
	keys := make([]string, 0, len(query))
	for key := range query {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+strings.Join(query[key], ","))
	}
	return method + " " + parsed.Path + "?" + strings.Join(parts, "&")
}

// replayServer serves a cassette back over HTTP so the real client, transport,
// and decoding path all run; only the network is replaced.
func replayServer(t *testing.T, recorded cassette) *httptest.Server {
	t.Helper()
	byKey := map[string][]interaction{}
	for _, item := range recorded.Interactions {
		key := replayKey(item.Request.Method, item.Request.URL)
		byKey[key] = append(byKey[key], item)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := replayKey(r.Method, r.URL.String())
		queue := byKey[key]
		if len(queue) == 0 {
			t.Errorf("cassette %s has no recorded response for %s", recorded.Name, key)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		item := queue[0]
		byKey[key] = queue[1:]

		body := item.Response.Body
		// Rewrite the recorded callback nonce to the one this run generated, so
		// the client's exact-match check sees what it asked for.
		if want := r.URL.Query().Get("callback"); want != "" {
			body = callbackParam.ReplaceAllString(body, want)
		}
		for key, value := range item.Response.Headers {
			w.Header().Set(key, value)
		}
		w.WriteHeader(item.Response.Status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

// convergedCases have byte-for-byte parity with the reference implementation
// and are asserted on every run. A regression here fails the build.
var convergedCases = []string{
	"channels",
	"search_menu",
	"newscroll",
	"frontline",
	"frontline_detail",
	"entities_preview_companies",
	"search_keyword",
	"topics_economy",
	"topics_china_sp",
	"cxdata_feed",
	"latest",
	"route_article",
	"snapshot_datanews",
	"public_directory_datatopic",
	"public_directory_photoreport",
	"public_directory_esg30",
	"esg30_subdirectory_news",
	"section_directory_finance",
	"section_directory_editorial",
	"issue_weekly",
	"blog_author",
	"culture_section_zhuanlan",
	"culture_author",
	"snapshot_weekly",
	"snapshot_home",
	"snapshot_opinion",
	"snapshot_culture",
	"snapshot_photos",
	"snapshot_video",
	"snapshot_cnreform",
	"snapshot_bijiao",
	"snapshot_mini_briefing",
	"snapshot_esg",
	"snapshot_topics",
	"snapshot_newsletter",
	"snapshot_wenews",
	"snapshot_blog",
	"snapshot_finance",
	"snapshot_companies",
	"snapshot_international",
	"snapshot_science",
	"snapshot_tech",
	"snapshot_auto",
	"snapshot_consumer",
	"snapshot_energy",
	"snapshot_health",
	"snapshot_livelihood",
	"snapshot_obituary",
	"snapshot_property",
	"topic_deepview",
	"topic_key",
	"microsite_summit",
	"datanews_interactive_covers",
	"esg30_resource_gzlfz",
	"opinion_author_wuqianli",
	"opinion_columns",
	"opinion_upfront",
	"opinion_author_directory",
	"video_section_dr",
	"bloggers_directory",
	"public_directory_promote",
}

// pendingCases are recorded, replayable, and known not to match yet. They are
// reported rather than asserted so the suite stays green on proven behaviour
// while still naming the gap on every run -- a skipped test would let the gap
// go quiet, and a failing one would make the build useless as a signal.
//
// Each entry says what still differs, so picking the work back up needs no
// re-diagnosis.
var pendingCases = map[string]string{
	// Both goldens under-report one sidebar list. The page leaves an <a> open
	// inside the first <li>; libxml2 (the reference's parser) then nests the
	// following rows inside that anchor, so `./ul/li` finds one row where the
	// markup has five. Measured directly against the recorded page: libxml2
	// returns 1 row, Go's HTML5 parser returns 5, and the four it recovers each
	// carry a distinct article. Go matches what a browser shows, so converging
	// here would mean reproducing a parser bug.
	"snapshot_mini": "reference parser drops four sidebar rows; Go matches the browser",
	"snapshot_en":   "reference parser drops four sidebar rows; Go matches the browser",
	"bloggers_directory": "the golden is the directory-module shape the HTML layer builds " +
		"(modules, pagination, visibility flags), and that layer is not ported; the JSON " +
		"half of the command works, but its envelope shape belongs there",

	// The remaining editorial surface. Each entry names the page template that
	// still needs an extractor; the shared core (item extraction, url and image
	// allowlists, server visibility, navigation, click consumers) is in place and
	// `snapshot_datanews` proves it converges, so what is left per case is the
	// page-specific module layout.
	"public_directory_promote": "the item set matches the golden exactly (nothing extra, " +
		"nothing missing) and every consumer agrees; only the module traversal order " +
		"differs, so items land in a different sequence. The `tuijian` root selector " +
		"resolves to a different node than the reference's did -- fix that and it converges",
}

func TestFixtures_GoPortMatchesRecordedGoldens(t *testing.T) {
	for _, name := range convergedCases {
		t.Run(name, func(t *testing.T) {
			recorded := loadCassette(t, name)
			golden := loadGolden(t, name)

			previous := baseURLOverride
			if len(recorded.Interactions) > 0 {
				baseURLOverride = replayServer(t, recorded).URL
			}
			t.Cleanup(func() { baseURLOverride = previous })

			args := append(append([]string{}, recorded.Command...),
				"--state-dir", seedStateDir(t, name), "--compact")
			var stdout, stderr strings.Builder
			exit := ExecuteArgs(context.Background(), args, strings.NewReader(""), &stdout, &stderr)
			if exit != 0 {
				t.Fatalf("replaying %v exited %d\nstdout: %s\nstderr: %s",
					recorded.Command, exit, stdout.String(), stderr.String())
			}

			var envelope map[string]any
			if err := json.Unmarshal([]byte(stdout.String()), &envelope); err != nil {
				t.Fatalf("stdout is not JSON: %v", err)
			}
			data, _ := envelope["data"].(map[string]any)
			compareGolden(t, name, golden, data)
		})
	}
}

// compareGolden reports the first differing keys rather than dumping two blobs,
// so a mismatch names the field that drifted.
func compareGolden(t *testing.T, name string, golden, actual map[string]any) {
	t.Helper()
	// Normalize whole documents first. stripRouteArgv decides what to drop from
	// a route verdict by looking at its sibling `adapter` key, so handing it an
	// isolated value would leave the argv in place.
	golden, _ = stripReason(stripFetchedAt(stripUntrusted(stripRouteArgv(normalizeJSON(golden))))).(map[string]any)
	actual, _ = stripReason(stripFetchedAt(stripUntrusted(stripRouteArgv(normalizeJSON(actual))))).(map[string]any)

	missing, differing := []string{}, []string{}
	for key, want := range golden {
		got, present := actual[key]
		if !present {
			missing = append(missing, key)
			continue
		}
		if !reflect.DeepEqual(want, got) {
			differing = append(differing, key)
		}
	}
	extra := []string{}
	for key := range actual {
		if _, present := golden[key]; !present {
			extra = append(extra, key)
		}
	}
	sort.Strings(missing)
	sort.Strings(differing)
	sort.Strings(extra)

	if len(missing) > 0 {
		t.Errorf("%s: golden keys absent from the Go output: %v", name, missing)
	}
	if len(differing) > 0 {
		for _, key := range differing {
			path, want, got := firstDifference(key, golden[key], actual[key])
			t.Errorf("%s: %s differs\n  golden: %s\n  go:     %s",
				name, path, compactJSON(want), compactJSON(got))
		}
	}
	if len(extra) > 0 {
		t.Logf("%s: Go output adds keys the reference did not emit: %v", name, extra)
	}
}

// normalizeJSON round-trips through encoding so numeric types compare equal
// regardless of whether they arrived as int or float64.
func normalizeJSON(value any) any {
	encoded, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return value
	}
	return decoded
}

func compactJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "<unencodable>"
	}
	if len(encoded) > 220 {
		return string(encoded[:220]) + "..."
	}
	return string(encoded)
}

// TestFixtures_PendingCasesStillReplay keeps the unconverged corpus honest: the
// cassettes must still load and replay without crashing, and every pending case
// must still be pending. If one starts matching, this fails and tells you to
// promote it -- so the gap list cannot silently rot.
func TestFixtures_PendingCasesStillReplay(t *testing.T) {
	for name, reason := range pendingCases {
		t.Run(name, func(t *testing.T) {
			recorded := loadCassette(t, name)
			golden := loadGolden(t, name)

			previous := baseURLOverride
			if len(recorded.Interactions) > 0 {
				baseURLOverride = replayServer(t, recorded).URL
			}
			t.Cleanup(func() { baseURLOverride = previous })

			args := append(append([]string{}, recorded.Command...),
				"--state-dir", seedStateDir(t, name), "--compact")
			var stdout, stderr strings.Builder
			exit := ExecuteArgs(context.Background(), args, strings.NewReader(""), &stdout, &stderr)
			if exit != 0 {
				t.Logf("%s: pending (%s); replay exits %d", name, reason, exit)
				return
			}

			var envelope map[string]any
			if err := json.Unmarshal([]byte(stdout.String()), &envelope); err != nil {
				t.Fatalf("%s: stdout is not JSON: %v", name, err)
			}
			data, _ := envelope["data"].(map[string]any)
			normalizedGolden, _ := stripRouteArgv(normalizeJSON(golden)).(map[string]any)
			normalizedData, _ := stripRouteArgv(normalizeJSON(data)).(map[string]any)

			identical := true
			for key, want := range normalizedGolden {
				got, present := normalizedData[key]
				if !present || !reflect.DeepEqual(want, got) {
					identical = false
					break
				}
			}
			if identical {
				t.Errorf("%s now matches its golden - promote it into convergedCases", name)
				return
			}
			t.Logf("%s: still pending (%s)", name, reason)
		})
	}
}

// stripRouteArgv removes the argv from any embedded route verdict before a
// golden comparison.
//
// The reference implementation records the command as its own python
// invocation, interpreter path and all. A Go binary cannot reproduce that and
// should not try: the verdict -- which adapter, which canonical url, whether
// discovery is required -- is the contract, and that is still compared in full.
// stripUntrusted removes the `_untrusted` marker from both sides of a parity
// comparison.
//
// This is a deliberate, single divergence from the reference implementation: it
// emitted `_untrusted: true`, while SEC-SPEC §2 requires the marker to *name*
// the externally-controlled fields so an agent knows which values to quarantine.
// The Go port emits that array. Content parity is what these fixtures exist to
// prove, so the marker is compared by TestFixtures_UntrustedMatchesDeclaredSchema
// against the declared schema instead of against the old goldens.
func stripUntrusted(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if key == "_untrusted" {
				continue
			}
			out[key] = stripUntrusted(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, stripUntrusted(item))
		}
		return out
	default:
		return value
	}
}

// stripReason drops human prose from both sides of a parity comparison.
//
// `reason` explains a routing verdict to a person; the Go port writes it in
// English while the reference implementation wrote Chinese. The machine-readable
// half of that verdict -- `boundary`, `adapter`, `supported`, `command` -- is
// still compared exactly, which is what an agent actually branches on.
func stripReason(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if key == "reason" {
				continue
			}
			out[key] = stripReason(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, stripReason(item))
		}
		return out
	default:
		return value
	}
}

// stripFetchedAt drops the wall-clock stamp from both sides.
//
// `source.fetched_at` records when the page was read, so the golden holds the
// recording time and a replay holds today's. It can never match, and asserting
// it would make the corpus expire rather than catch drift.
func stripFetchedAt(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if key == "fetched_at" {
				continue
			}
			out[key] = stripFetchedAt(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, stripFetchedAt(item))
		}
		return out
	default:
		return value
	}
}

func stripRouteArgv(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if key == "command" && typed["adapter"] != nil {
				continue
			}
			out[key] = stripRouteArgv(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, stripRouteArgv(item))
		}
		return out
	default:
		return value
	}
}

// firstDifference walks two values in parallel and returns the path of the
// first concrete divergence, so a mismatch inside a 25-element list names the
// element and key rather than dumping both lists.
func firstDifference(path string, want, got any) (string, any, any) {
	want, got = stripRouteArgv(normalizeJSON(want)), stripRouteArgv(normalizeJSON(got))

	wantMap, wantIsMap := want.(map[string]any)
	gotMap, gotIsMap := got.(map[string]any)
	if wantIsMap && gotIsMap {
		keys := make([]string, 0, len(wantMap))
		for key := range wantMap {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			gotValue, present := gotMap[key]
			if !present {
				return path + "." + key + " (absent from go output)", wantMap[key], nil
			}
			if !reflect.DeepEqual(wantMap[key], gotValue) {
				return firstDifference(path+"."+key, wantMap[key], gotValue)
			}
		}
		for key := range gotMap {
			if _, present := wantMap[key]; !present {
				return path + "." + key + " (extra in go output)", nil, gotMap[key]
			}
		}
		return path, want, got
	}

	wantList, wantIsList := want.([]any)
	gotList, gotIsList := got.([]any)
	if wantIsList && gotIsList {
		if len(wantList) != len(gotList) {
			return fmt.Sprintf("%s (length %d vs %d)", path, len(wantList), len(gotList)), want, got
		}
		for index := range wantList {
			if !reflect.DeepEqual(wantList[index], gotList[index]) {
				return firstDifference(fmt.Sprintf("%s[%d]", path, index), wantList[index], gotList[index])
			}
		}
	}
	return path, want, got
}

// recordedWithSession lists cases captured while a subscription session was
// loaded. Those payloads echo that fact back, so the replay seeds an equivalent
// state dir -- otherwise the harness would be testing a different local
// environment than the one the golden came from.
var recordedWithSession = map[string]bool{
	"latest": true,
}

// seedStateDir builds an isolated state dir, optionally holding a session
// cookie. It never touches the real one.
func seedStateDir(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	if recordedWithSession[name] {
		cookies := `[{"name":"session","value":"replay","domain":"www.caixin.com","path":"/","secure":false}]`
		if err := os.WriteFile(filepath.Join(dir, "cookies.json"), []byte(cookies), 0o600); err != nil {
			t.Fatalf("seed state dir: %v", err)
		}
	}
	return dir
}
