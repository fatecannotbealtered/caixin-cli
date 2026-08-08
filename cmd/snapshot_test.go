package cmd

import (
	"net/http"
	"strings"
	"testing"
)

// datanewsPage is the shape the 数字说 front actually serves: a comMain wrapper
// holding a focus carousel and the latest list.
const datanewsPage = `<html><head><title>数字说频道-财新网</title></head><body>
<div class="comMain">
 <div class="ggbBox"><div class="ggbCon">
   <div class="callBoard"><a href="https://datanews.caixin.com/interactive/2024/x/"><img src="http://img.caixin.com/2024-01-01/1.jpg"></a>
   <h4><a href="https://datanews.caixin.com/2024-01-01/102000001.html">焦点标题</a></h4></div>
 </div></div>
 <div id="homeArticleList" class="szxwList">
  <dl><dt><a href="#"><img data-src="http://img.caixin.com/2024-01-02/2.png"></a></dt>
   <dd><h3><a href="https://datanews.caixin.com/2024-01-02/102000002.html">最新标题</a><i class="icon_key" title="收费文章"></i></h3><p>一段摘要。</p></dd></dl>
 </div>
 <div class="kshzp"><h2><a href="http://datanews.caixin.com/datatopic/">更多</a></h2>
  <ul class="szztCon"><li><a href="https://datanews.caixin.com/interactive/2023/demo/"><img src="https://datanews.caixin.com/interactive/2023/demo/cover.jpg">可视化作品</a></li></ul>
 </div>
</div></body></html>`

const datanewsURL = "https://datanews.caixin.com/"

func snapshotMock(t *testing.T, page string) *mockUpstream {
	t.Helper()
	mock := newMockUpstream(t)
	mock.handlers["/"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}
	return mock
}

func TestSnapshot_ExtractsModulesAndItems(t *testing.T) {
	got := run(t, snapshotMock(t, datanewsPage), "snapshot", datanewsURL)
	if got.Exit != 0 {
		t.Fatalf("exit = %d: %s", got.Exit, got.Stdout)
	}
	data := got.Data(t)

	if kind, _ := data["page_kind"].(string); kind != "datanews" {
		t.Errorf("page_kind = %q, want datanews", kind)
	}
	modules, _ := data["modules"].([]any)
	if len(modules) != 3 {
		t.Fatalf("modules = %d, want 3 (focus, latest, interactives)", len(modules))
	}
	if count, _ := data["items_count"].(float64); count != 3 {
		t.Errorf("items_count = %v, want 3", data["items_count"])
	}

	// The interactive block carries its own "see all" link.
	interactives, _ := modules[2].(map[string]any)
	if more, _ := interactives["more_url"].(string); more != "https://datanews.caixin.com/datatopic/" {
		t.Errorf("more_url = %q", more)
	}
}

// A page is only ever the server's HTML; claiming otherwise is the one thing
// this command must never do.
func TestSnapshot_NeverClaimsARenderedPage(t *testing.T) {
	data := run(t, snapshotMock(t, datanewsPage), "snapshot", datanewsURL).Data(t)
	for _, field := range []string{
		"javascript_executed", "rendered_visibility_verified",
		"external_stylesheets_applied", "complete_listing_verified",
	} {
		if value, _ := data[field].(bool); value {
			t.Errorf("%s = true; this client runs no scripts and paints nothing", field)
		}
	}
	if mode, _ := data["source_mode"].(string); mode != "server_html" {
		t.Errorf("source_mode = %q, want server_html", mode)
	}
}

// Every emitted url carries the command that consumes it, which is what makes
// the listing actionable rather than a pile of links.
func TestSnapshot_ItemsCarryTheirConsumer(t *testing.T) {
	data := run(t, snapshotMock(t, datanewsPage), "snapshot", datanewsURL).Data(t)
	modules, _ := data["modules"].([]any)
	focus, _ := modules[0].(map[string]any)
	items, _ := focus["items"].([]any)
	first, _ := items[0].(map[string]any)

	consumer, ok := first["consumer"].(map[string]any)
	if !ok {
		t.Fatalf("item has no consumer: %#v", first)
	}
	if adapter, _ := consumer["adapter"].(string); adapter != "article" {
		t.Errorf("adapter = %q, want article", adapter)
	}
	if command, _ := consumer["command"].([]any); len(command) == 0 {
		t.Error("consumer carries no runnable command")
	}
}

// The allowlist is the safety property: an unmeasured page would parse into a
// confidently wrong listing, so it is refused rather than attempted.
func TestSnapshot_RejectsUnmeasuredEntryPoints(t *testing.T) {
	for _, bad := range []string{
		"https://www.caixin.com/2026-08-07/102472114.html",
		"https://example.com/",
		"https://datanews.caixin.com/somewhere/",
	} {
		got := run(t, newMockUpstream(t), "snapshot", bad)
		if code := got.ErrorCode(t); code != "E_VALIDATION" {
			t.Errorf("%s: code = %s, want E_VALIDATION", bad, code)
		}
		if got.Exit != 2 {
			t.Errorf("%s: exit = %d, want 2", bad, got.Exit)
		}
	}
}

// A page missing the container it is keyed on is an upstream change, and must
// be reported rather than returned as an empty listing.
func TestSnapshot_MissingContainerIsReported(t *testing.T) {
	got := run(t, snapshotMock(t, "<html><body><p>nothing</p></body></html>"), "snapshot", datanewsURL)
	if got.Exit == 0 {
		t.Fatalf("a page with no comMain returned success: %s", got.Stdout)
	}
	if !strings.Contains(got.Stdout, "comMain") {
		t.Errorf("the error does not name what was missing: %s", got.Stdout)
	}
}

func TestSnapshot_UpstreamFailureIsMapped(t *testing.T) {
	mock := newMockUpstream(t)
	mock.Status("/", http.StatusInternalServerError)
	got := run(t, mock, "snapshot", datanewsURL)
	if code := got.ErrorCode(t); code != "E_SERVER" {
		t.Errorf("code = %s, want E_SERVER", code)
	}
	if got.Exit != 7 {
		t.Errorf("exit = %d, want 7", got.Exit)
	}
}
