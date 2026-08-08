package cmd

import (
	"net/http"
	"testing"
)

const datatopicPage = `<html><head><title>数字专题-财新网</title></head><body>
<div class="hotWord02"><ul class="szztCon02">
 <li><a href="http://datanews.caixin.com/interactive/2020/demo/"><span>可视化专题</span></a></li>
 <li><a href="http://datanews.caixin.com/mobile/legacy/"><span>移动端专题</span></a></li>
 <li><a href="https://datanews.caixin.com/2024-01-01/102000001.html"><span>一篇报道</span></a></li>
 <li><a href="https://example.com/off-site/"><span>站外链接</span></a></li>
</ul></div></body></html>`

const datatopicURL = "https://datanews.caixin.com/datatopic/"

func directoryMock(t *testing.T, page string) *mockUpstream {
	t.Helper()
	mock := newMockUpstream(t)
	mock.handlers["/datatopic/"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}
	return mock
}

func TestPublicDirectory_ClassifiesEachEntryKind(t *testing.T) {
	got := run(t, directoryMock(t, datatopicPage), "public-directory", datatopicURL)
	if got.Exit != 0 {
		t.Fatalf("exit = %d: %s", got.Exit, got.Stdout)
	}
	data := got.Data(t)
	if kind, _ := data["kind"].(string); kind != "datanews_topics" {
		t.Errorf("kind = %q", kind)
	}
	// The off-site link is dropped; the other three are kept and labelled.
	if count, _ := data["items_count"].(float64); count != 3 {
		t.Fatalf("items_count = %v, want 3 (the off-site link must be dropped)", data["items_count"])
	}
	modules, _ := data["modules"].([]any)
	module, _ := modules[0].(map[string]any)
	items, _ := module["items"].([]any)
	kinds := map[string]int{}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		kind, _ := item["item_kind"].(string)
		kinds[kind]++
	}
	if kinds["interactive_directory"] != 2 || kinds["article"] != 1 {
		t.Errorf("item kinds = %v, want 2 interactive and 1 article", kinds)
	}
}

// A directory lists; it never fetches. Saying otherwise would let a caller
// treat a title here as having read the piece.
func TestPublicDirectory_NeverClaimsToHaveFetched(t *testing.T) {
	data := run(t, directoryMock(t, datatopicPage), "public-directory", datatopicURL).Data(t)
	for _, field := range []string{"directory_only", "article_details_not_fetched", "scripts_not_executed"} {
		if value, _ := data[field].(bool); !value {
			t.Errorf("%s = false; a directory listing fetches nothing", field)
		}
	}
	if executed, _ := data["javascript_executed"].(bool); executed {
		t.Error("javascript_executed = true; this client runs no scripts")
	}
	modules, _ := data["modules"].([]any)
	module, _ := modules[0].(map[string]any)
	items, _ := module["items"].([]any)
	first, _ := items[0].(map[string]any)
	if fetched, _ := first["content_not_fetched"].(bool); !fetched {
		t.Error("an item does not declare content_not_fetched")
	}
}

func TestPublicDirectory_RejectsUnlistedEntryPoints(t *testing.T) {
	for _, bad := range []string{
		"https://datanews.caixin.com/",
		"https://example.com/",
		"https://www.caixin.com/2026-08-07/102472114.html",
	} {
		got := run(t, newMockUpstream(t), "public-directory", bad)
		if code := got.ErrorCode(t); code != "E_VALIDATION" {
			t.Errorf("%s: code = %s, want E_VALIDATION", bad, code)
		}
	}
}

func TestPublicDirectory_MissingListIsReported(t *testing.T) {
	got := run(t, directoryMock(t, "<html><body><p>nothing</p></body></html>"),
		"public-directory", datatopicURL)
	if got.Exit == 0 {
		t.Fatalf("a page with no list returned success: %s", got.Stdout)
	}
}

func TestPublicDirectory_UpstreamFailureIsMapped(t *testing.T) {
	mock := newMockUpstream(t)
	mock.Status("/datatopic/", http.StatusInternalServerError)
	got := run(t, mock, "public-directory", datatopicURL)
	if code := got.ErrorCode(t); code != "E_SERVER" {
		t.Errorf("code = %s, want E_SERVER", code)
	}
}
