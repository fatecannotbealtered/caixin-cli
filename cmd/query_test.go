package cmd

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fatecannotbealtered/caixin-cli/internal/secret"
)

// mountAll registers every endpoint the command surface touches, so a single
// mock serves the whole table and a command cannot pass by accidentally hitting
// nothing.
func mountAll(t *testing.T) *mockUpstream {
	t.Helper()
	mock := newMockUpstream(t)
	mock.OK("/api/dataplatform/scroll/category", []any{map[string]any{"id": "0", "name": "全部滚动"}})
	mock.OK("/api/dataplatform/scroll/index", map[string]any{
		"currentPage": 1, "pageSize": 20, "totalRecords": 1,
		"articleList": []any{map[string]any{"contentId": "1", "title": "t", "url": "https://www.caixin.com/a.html"}},
	})
	mock.OK("/api/dataplatform/common/search/category", searchMenuData())
	mock.OK("/api/dataplatform/common/search", map[string]any{
		"currentPage": 1, "pageSize": 10, "totalRecords": 1,
		"articleList": []any{map[string]any{"contentId": "2", "title": "s"}},
	})
	mock.Raw("/api/dataplus/sjtPc/news",
		`{"success":true,"data":{"total":1,"data":[{"title":"c","url":"https://cxdata.caixin.com/x.html"}]}}`)
	mock.Raw("/api/companiesNew/app",
		`{"success":true,"code":"0","data":[{"orgUniCode":"1","orgChiName":"某公司"}]}`)
	mock.Raw("/api/extapi/homeInterface.jsp",
		`{"maxes":1,"datas":[{"nid":"9","desc":"标题","link":"https://www.caixin.com/2026-08-06/1.html","attr":0}]}`)
	mock.Raw("/app/v1/list",
		`__CALLBACK__({"data":{"list":[{"oneline_news_code":"`+strings.Repeat("a", 32)+`","title":"f"}]}})`)
	mock.Raw("/app/getNewsByCode",
		`__CALLBACK__({"data":{"oneline_news_code":"`+strings.Repeat("a", 32)+`","title":"f"}})`)
	mock.Raw("/json/blogger-date-1.json",
		`{"code":0,"page":1,"size":20,"totalPages":2,"totalElements":30,"data":[{"id":1,"name":"n","authorUrl":"https://n.blog.caixin.com/","lastestTime":1785997408}]}`)
	return mock
}

// queryCases drive every leaf command through the real boundary.
var queryCases = []struct {
	name string
	args []string
}{
	{"channels", []string{"channels"}},
	{"latest", []string{"latest", "--size", "20"}},
	{"newscroll", []string{"newscroll"}},
	{"search-menu", []string{"search-menu"}},
	{"search", []string{"search", "经济", "--size", "10"}},
	{"cxdata-feed", []string{"cxdata-feed", "latest", "--size", "25"}},
	{"entities-preview", []string{"entities-preview", "companies"}},
	{"topics", []string{"topics", "https://topics.caixin.com/economy/", "--page", "1", "--size", "25"}},
	{"frontline", []string{"frontline", "--size", "20"}},
	{"frontline-detail", []string{"frontline-detail", strings.Repeat("a", 32)}},
	{"bloggers-directory", []string{"bloggers-directory", "--page", "1", "--sort", "latest"}},
	{"route", []string{"route", "https://www.caixin.com/2026-08-06/1.html"}},
	{"status", []string{"status"}},
}

func TestQueryCommands_SuccessEnvelope(t *testing.T) {
	for _, testCase := range queryCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := run(t, mountAll(t), testCase.args...)
			if got.Exit != 0 {
				t.Fatalf("exit = %d, want 0\nstdout: %s\nstderr: %s", got.Exit, got.Stdout, got.Stderr)
			}
			envelope := got.Envelope(t)
			for _, key := range []string{"ok", "schema_version", "data", "meta"} {
				if _, ok := envelope[key]; !ok {
					t.Errorf("envelope is missing %q: %s", key, got.Stdout)
				}
			}
			if envelope["schema_version"] != "1.0" {
				t.Errorf("schema_version = %v, want 1.0", envelope["schema_version"])
			}
			meta, _ := envelope["meta"].(map[string]any)
			if _, ok := meta["duration_ms"]; !ok {
				t.Error("meta.duration_ms must always be present, even as 0")
			}
		})
	}
}

// Every content result must be marked untrusted: Caixin text is publisher- and
// user-supplied, and an agent has to treat it as data.
func TestQueryCommands_ResultsAreMarkedUntrusted(t *testing.T) {
	for _, testCase := range queryCases {
		if testCase.name == "status" {
			continue // local state, no upstream content
		}
		t.Run(testCase.name, func(t *testing.T) {
			data := run(t, mountAll(t), testCase.args...).Data(t)
			// SEC-SPEC §2: the marker names the externally-controlled fields.
			// A bare `true` would tell an agent that something in the payload is
			// untrusted without telling it what to quarantine, which is the one
			// thing the marker exists to answer.
			marked, ok := data["_untrusted"].([]any)
			if !ok {
				t.Fatalf("%s: _untrusted = %#v, want an array of field names",
					testCase.name, data["_untrusted"])
			}
			if len(marked) == 0 {
				t.Fatalf("%s: _untrusted is empty; this command returns upstream content",
					testCase.name)
			}
			for _, field := range marked {
				name, _ := field.(string)
				if _, present := data[name]; !present {
					t.Errorf("%s: _untrusted names %q, which is not in the payload",
						testCase.name, name)
				}
			}
		})
	}
}

func TestQueryCommands_UpstreamStatusMapping(t *testing.T) {
	cases := []struct {
		status int
		code   string
		exit   int
	}{
		{401, "E_AUTH", 4},
		{403, "E_FORBIDDEN", 4},
		{404, "E_NOT_FOUND", 3},
		{429, "E_RATE_LIMITED", 7},
		{500, "E_SERVER", 7},
	}
	for _, testCase := range cases {
		t.Run(testCase.code, func(t *testing.T) {
			mock := newMockUpstream(t)
			mock.Status("/api/dataplatform/scroll/category", testCase.status)
			got := run(t, mock, "channels")
			if code := got.ErrorCode(t); code != testCase.code {
				t.Errorf("code = %s, want %s", code, testCase.code)
			}
			if got.Exit != testCase.exit {
				t.Errorf("exit = %d, want %d", got.Exit, testCase.exit)
			}
			errorObject, _ := got.Envelope(t)["error"].(map[string]any)
			retryable, _ := errorObject["retryable"].(bool)
			if want := testCase.exit == 7 || testCase.exit == 8; retryable != want {
				t.Errorf("retryable = %v, want %v", retryable, want)
			}
		})
	}
}

func TestQueryCommands_BusinessErrorIsServerError(t *testing.T) {
	mock := newMockUpstream(t)
	mock.BusinessError("/api/dataplatform/scroll/category", 40001, "上游拒绝")
	got := run(t, mock, "channels")
	if code := got.ErrorCode(t); code != "E_SERVER" {
		t.Errorf("code = %s, want E_SERVER", code)
	}
}

func TestQueryCommands_EmptyResultStillSucceeds(t *testing.T) {
	mock := newMockUpstream(t)
	mock.OK("/api/dataplatform/scroll/index", map[string]any{
		"currentPage": 1, "pageSize": 20, "totalRecords": 0, "articleList": []any{},
	})
	got := run(t, mock, "latest")
	if got.Exit != 0 {
		t.Fatalf("an empty result is a success, got exit %d: %s", got.Exit, got.Stdout)
	}
	articles, ok := got.Data(t)["articles"].([]any)
	if !ok || len(articles) != 0 {
		t.Errorf("expected an empty article list, got %v", got.Data(t)["articles"])
	}
}

func TestListCommands_LimitMapsUpstreamAndReturnsPagination(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		args   []string
		verify func(*http.Request) bool
	}{
		{"latest", "/api/dataplatform/scroll/index", []string{"latest", "--limit", "1"}, func(r *http.Request) bool { return r.URL.Query().Get("size") == "1" }},
		{"search", "/api/dataplatform/common/search", []string{"search", "经济", "--limit", "1"}, func(r *http.Request) bool {
			var body map[string]any
			return json.NewDecoder(r.Body).Decode(&body) == nil && body["pageSize"] == float64(1)
		}},
		{"cxdata-feed", "/api/dataplus/sjtPc/news", []string{"cxdata-feed", "latest", "--limit", "1"}, func(r *http.Request) bool { return r.URL.Query().Get("pageSize") == "1" }},
		{"topics", "/api/extapi/homeInterface.jsp", []string{"topics", "https://topics.caixin.com/economy/", "--limit", "1"}, func(r *http.Request) bool { return r.URL.Query().Get("count") == "1" }},
		{"frontline", "/app/v1/list", []string{"frontline", "--limit", "1"}, func(r *http.Request) bool { return r.URL.Query().Get("c") == "1" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			mock := mountAll(t)
			original := mock.handlers[testCase.path]
			mapped := false
			mock.handlers[testCase.path] = func(w http.ResponseWriter, r *http.Request) {
				mapped = testCase.verify(r)
				original(w, r)
			}
			got := run(t, mock, testCase.args...)
			if got.Exit != 0 {
				t.Fatalf("exit = %d: %s", got.Exit, got.Stdout)
			}
			if !mapped {
				t.Error("--limit was not mapped to the upstream page-size parameter")
			}
			data := got.Data(t)
			if _, ok := data["count"]; !ok {
				t.Error("list result is missing count")
			}
			if _, ok := data["has_more"].(bool); !ok {
				t.Error("list result is missing boolean has_more")
			}
		})
	}
}

func TestLatest_ReturnsNextPageAndISOTimestamps(t *testing.T) {
	mock := newMockUpstream(t)
	mock.OK("/api/dataplatform/scroll/index", map[string]any{
		"currentPage": 1, "pageSize": 1, "totalRecords": 3,
		"articleList": []any{map[string]any{
			"contentId": "1", "title": "t", "url": "https://www.caixin.com/a.html",
			"time": 1785981475000, "updateTime": "1786026261000",
		}},
	})
	data := run(t, mock, "latest", "--limit", "1").Data(t)
	if data["next_page"] != float64(2) {
		t.Errorf("next_page = %v, want 2", data["next_page"])
	}
	articles, _ := data["articles"].([]any)
	article, _ := articles[0].(map[string]any)
	if article["published_at"] != "2026-08-06T01:57:55Z" || article["updated_at"] != "2026-08-06T14:24:21Z" {
		t.Errorf("article timestamps = %v/%v, want ISO-8601 UTC", article["published_at"], article["updated_at"])
	}
	if _, ok := article["published_at_ms"]; ok {
		t.Error("article still exposes published_at_ms")
	}
}

func TestListCommands_RejectSizeAndLimitTogether(t *testing.T) {
	for _, args := range [][]string{
		{"latest", "--size", "1", "--limit", "1"},
		{"search", "经济", "--size", "1", "--limit", "1"},
		{"cxdata-feed", "latest", "--size", "1", "--limit", "1"},
		{"topics", "https://topics.caixin.com/economy/", "--size", "1", "--limit", "1"},
		{"frontline", "--size", "1", "--limit", "1"},
	} {
		got := run(t, mountAll(t), args...)
		if got.Exit != 2 || got.ErrorCode(t) != "E_USAGE" {
			t.Errorf("%v = exit %d/code %s, want 2/E_USAGE", args, got.Exit, got.ErrorCode(t))
		}
	}
}

// Arguments are validated before any request: a bad scope must be a usage error,
// never a request that quietly returns the wrong scope's results.
func TestQueryCommands_InvalidArguments(t *testing.T) {
	cases := []struct {
		name string
		args []string
		code string
		exit int
	}{
		{"unknown cxdata category", []string{"cxdata-feed", "bogus"}, "E_VALIDATION", 2},
		{"cxdata size over cap", []string{"cxdata-feed", "latest", "--size", "99"}, "E_VALIDATION", 2},
		{"unknown entity library", []string{"entities-preview", "bogus"}, "E_VALIDATION", 2},
		{"topics url off allowlist", []string{"topics", "https://topics.caixin.com/nope/"}, "E_VALIDATION", 2},
		{"empty search keyword", []string{"search", "  "}, "E_VALIDATION", 2},
		{"search size over cap", []string{"search", "x", "--size", "99"}, "E_VALIDATION", 2},
		{"bad time range", []string{"search", "x", "--time-range", "9"}, "E_VALIDATION", 2},
		{"custom dates without range 5", []string{"search", "x", "--start-date", "2026-01-01"}, "E_VALIDATION", 2},
		{"unknown filter", []string{"search", "x", "--filter", "bogus"}, "E_VALIDATION", 2},
		{"malformed frontline code", []string{"frontline-detail", "nothex"}, "E_VALIDATION", 2},
		{"bad blogger sort", []string{"bloggers-directory", "--sort", "bogus"}, "E_VALIDATION", 2},
		{"newscroll page zero", []string{"newscroll", "--page", "0"}, "E_VALIDATION", 2},
		{"newscroll bad date", []string{"newscroll", "--date", "08/06/2026"}, "E_VALIDATION", 2},
		{"missing search keyword", []string{"search"}, "E_USAGE", 2},
		{"extra argument", []string{"channels", "extra"}, "E_USAGE", 2},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := run(t, mountAll(t), testCase.args...)
			if code := got.ErrorCode(t); code != testCase.code {
				t.Errorf("code = %s, want %s", code, testCase.code)
			}
			if got.Exit != testCase.exit {
				t.Errorf("exit = %d, want %d", got.Exit, testCase.exit)
			}
		})
	}
}

// route is offline by contract: it must classify without touching the network.
func TestRoute_ClassifiesWithoutNetwork(t *testing.T) {
	mock := newMockUpstream(t)
	got := run(t, mock, "route", "https://finance.caixin.com/2026-08-06/102472081.html")
	if got.Exit != 0 {
		t.Fatalf("exit = %d: %s", got.Exit, got.Stdout)
	}
	if len(mock.Requests) != 0 {
		t.Errorf("route made %d upstream request(s); it must be purely local", len(mock.Requests))
	}
	data := got.Data(t)
	if supported, _ := data["supported"].(bool); !supported {
		t.Error("a standard article url should be supported")
	}
	if data["adapter"] != "article" {
		t.Errorf("adapter = %v, want article", data["adapter"])
	}
	// Routing never claims entitlement.
	if implied, _ := data["content_access_not_implied"].(bool); !implied {
		t.Error("content_access_not_implied must stay true")
	}
}

func TestRoute_RejectsForeignHosts(t *testing.T) {
	data := run(t, newMockUpstream(t), "route", "https://example.com/x").Data(t)
	if supported, _ := data["supported"].(bool); supported {
		t.Error("a non-Caixin url must not be routed as supported")
	}
	if reason, _ := data["reason"].(string); reason == "" {
		t.Error("an unsupported url must say why")
	}
}

func TestStatus_ReportsSessionWithoutImplyingEntitlement(t *testing.T) {
	data := run(t, nil, "status").Data(t)
	if authenticated, _ := data["authenticated"].(bool); authenticated {
		t.Error("authenticated = true for an empty state dir")
	}
	// A login is not a subscription; the payload has to say so.
	if implied, _ := data["entitlement_not_implied"].(bool); !implied {
		t.Error("status must state that entitlement is not implied")
	}
}

func TestLogout_RequiresDryRunAndConfirm(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cookies.json"), []byte(`[{"name":"session","value":"test","domain":"www.caixin.com","path":"/"}]`), 0o600); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	bare := runCLI(t, nil, "logout", "--state-dir", dir, "--compact")
	if bare.Exit != 5 || bare.ErrorCode(t) != "E_CONFIRMATION_REQUIRED" {
		t.Fatalf("bare logout = exit %d, code %s; want exit 5/E_CONFIRMATION_REQUIRED",
			bare.Exit, bare.ErrorCode(t))
	}
	fabricated := runCLI(t, nil, "logout", "--confirm", "ct_fabricated_0000", "--state-dir", dir, "--compact")
	if fabricated.Exit != 6 || fabricated.ErrorCode(t) != "E_CONFLICT" {
		t.Fatalf("fabricated confirm = exit %d, code %s; want exit 6/E_CONFLICT",
			fabricated.Exit, fabricated.ErrorCode(t))
	}

	preview := runCLI(t, nil, "logout", "--dry-run", "--state-dir", dir, "--compact")
	if preview.Exit != 0 {
		t.Fatalf("dry-run exit = %d: %s", preview.Exit, preview.Stdout)
	}
	token, _ := preview.Data(t)["confirm_token"].(string)
	if token == "" {
		t.Fatal("dry-run returned no confirm_token")
	}
	changes, _ := preview.Data(t)["preview"].(map[string]any)["changes"].([]any)
	first, _ := changes[0].(map[string]any)
	before, _ := first["before"].(map[string]any)
	if configured, _ := before["configured"].(bool); !configured {
		t.Fatal("bare/fabricated/dry-run logout removed the configured session")
	}

	confirmed := runCLI(t, nil, "logout", "--confirm", token, "--state-dir", dir, "--compact")
	if confirmed.Exit != 0 {
		t.Fatalf("confirmed logout exit = %d: %s", confirmed.Exit, confirmed.Stdout)
	}
	if cleared, _ := confirmed.Data(t)["logged_out"].(bool); !cleared {
		t.Error("confirmed logout did not report logged_out=true")
	}
	after := runCLI(t, nil, "logout", "--dry-run", "--state-dir", dir, "--compact")
	afterChanges, _ := after.Data(t)["preview"].(map[string]any)["changes"].([]any)
	afterFirst, _ := afterChanges[0].(map[string]any)
	afterBefore, _ := afterFirst["before"].(map[string]any)
	if configured, _ := afterBefore["configured"].(bool); configured {
		t.Fatal("confirmed logout left the configured session behind")
	}
}

func TestLogout_ConfirmTokenIsSingleUse(t *testing.T) {
	dir := t.TempDir()
	preview := runCLI(t, nil, "logout", "--dry-run", "--state-dir", dir, "--compact")
	token, _ := preview.Data(t)["confirm_token"].(string)
	first := runCLI(t, nil, "logout", "--confirm", token, "--state-dir", dir, "--compact")
	if first.Exit != 0 {
		t.Fatalf("first confirm exit = %d: %s", first.Exit, first.Stdout)
	}
	replay := runCLI(t, nil, "logout", "--confirm", token, "--state-dir", dir, "--compact")
	if replay.Exit != 6 || replay.ErrorCode(t) != "E_CONFLICT" {
		t.Fatalf("replayed confirm = exit %d/code %s, want 6/E_CONFLICT",
			replay.Exit, replay.ErrorCode(t))
	}
}

func TestLogout_RejectsTokenAfterCredentialChanges(t *testing.T) {
	dir := t.TempDir()
	initial := []byte(`[{"name":"session","value":"first","domain":"www.caixin.com","path":"/"}]`)
	if err := os.WriteFile(filepath.Join(dir, "cookies.json"), initial, 0o600); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	preview := runCLI(t, nil, "logout", "--dry-run", "--state-dir", dir, "--compact")
	token, _ := preview.Data(t)["confirm_token"].(string)

	changed := []byte(`[{"name":"session","value":"second","domain":"www.caixin.com","path":"/"}]`)
	if err := secret.New(dir).Save("session", changed); err != nil {
		t.Fatalf("replace session: %v", err)
	}
	confirmed := runCLI(t, nil, "logout", "--confirm", token, "--state-dir", dir, "--compact")
	if confirmed.Exit != 6 || confirmed.ErrorCode(t) != "E_CONFLICT" {
		t.Fatalf("confirm after credential change = exit %d/code %s, want 6/E_CONFLICT",
			confirmed.Exit, confirmed.ErrorCode(t))
	}
	if authenticated, _ := runCLI(t, nil, "status", "--state-dir", dir, "--compact").Data(t)["authenticated"].(bool); !authenticated {
		t.Fatal("rejected stale token removed the replacement session")
	}
}

func TestLogout_LedgerPersistenceFailureIsFailOpen(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cookies.json"), []byte(`[{"name":"session","value":"test","domain":"www.caixin.com","path":"/"}]`), 0o600); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	preview := runCLI(t, nil, "logout", "--dry-run", "--state-dir", dir, "--compact")
	token, _ := preview.Data(t)["confirm_token"].(string)
	if err := os.WriteFile(filepath.Join(dir, "confirm-consumed"), []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("block ledger directory: %v", err)
	}
	confirmed := runCLI(t, nil, "logout", "--confirm", token, "--state-dir", dir, "--compact")
	if confirmed.Exit != 0 {
		t.Fatalf("ledger failure blocked confirmed logout: exit %d: %s", confirmed.Exit, confirmed.Stdout)
	}
}

// Public endpoints must be read anonymously. Presenting a paid session cookie
// to a page that does not need it exposes an account-level credential for no
// benefit, so the client marks those calls anonymous and this proves it.
func TestAnonymousEndpointsDoNotSendCookies(t *testing.T) {
	mock := mountAll(t)
	seen := map[string]string{}
	for path, handler := range mock.handlers {
		path, handler := path, handler
		mock.handlers[path] = func(w http.ResponseWriter, r *http.Request) {
			seen[path] = r.Header.Get("Cookie")
			handler(w, r)
		}
	}

	dir := t.TempDir()
	cookies := `[{"name":"session","value":"paid-session","domain":"www.caixin.com","path":"/","secure":false}]`
	if err := os.WriteFile(filepath.Join(dir, "cookies.json"), []byte(cookies), 0o600); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	for _, args := range [][]string{
		{"cxdata-feed", "latest", "--size", "25"},
		{"entities-preview", "companies"},
		{"bloggers-directory"},
	} {
		if got := runCLI(t, mock, append(args, "--state-dir", dir, "--compact")...); got.Exit != 0 {
			t.Fatalf("%v exited %d: %s", args, got.Exit, got.Stdout)
		}
	}
	for path, cookie := range seen {
		if strings.Contains(cookie, "paid-session") {
			t.Errorf("%s received the session cookie on an anonymous read", path)
		}
	}
}
