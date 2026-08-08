package cmd

import (
	"net/http"
	"testing"
)

const esg30ParentPage = `<html><head><title>ESG30-财新网</title></head><body>
<section class="main">
 <ul><li><a href="https://index.caixin.com/news/">资讯</a></li></ul>
</section></body></html>`

const esg30NewsPage = `<html><head><title>ESG30 资讯-财新网</title></head><body>
<div class="news-list"><ul>
 <li><p class="news-title"><a href="https://www.caixin.com/2026-01-01/102000001.html">一篇 ESG 报道</a></p>
     <span class="news-date">2026-01-01</span><div class="news-tag"><span>ESG</span></div></li>
 <li style="display:none"><p class="news-title"><a href="https://www.caixin.com/2026-01-02/102000002.html">被隐藏的条目</a></p></li>
</ul></div></body></html>`

func esg30Mock(t *testing.T, parent, news string) *mockUpstream {
	t.Helper()
	mock := newMockUpstream(t)
	mock.handlers["/esg30/"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(parent))
	}
	mock.handlers["/news/"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(news))
	}
	return mock
}

func TestESG30Subdirectory_ReadsAListedSubIndex(t *testing.T) {
	got := run(t, esg30Mock(t, esg30ParentPage, esg30NewsPage),
		"esg30-subdirectory", "https://index.caixin.com/news/")
	if got.Exit != 0 {
		t.Fatalf("exit = %d: %s", got.Exit, got.Stdout)
	}
	data := got.Data(t)
	// The hidden card is reported as absent, not silently included.
	if count, _ := data["items_count"].(float64); count != 1 {
		t.Errorf("items_count = %v, want 1 (the hidden entry must be dropped)", data["items_count"])
	}
	profile, _ := data["section_profile"].(map[string]any)
	if title, _ := profile["title"].(string); title != "资讯" {
		t.Errorf("section_profile.title = %q; it comes from the parent listing", title)
	}
	source, _ := data["source"].(map[string]any)
	if from, _ := source["discovered_from"].(string); from == "" {
		t.Error("the result does not record which directory it was discovered from")
	}
}

// The discovery rule is the point: a sub-index the parent is not currently
// listing must not be fetched just because its url has the right shape.
func TestESG30Subdirectory_RefusesAnUnlistedSubIndex(t *testing.T) {
	empty := `<html><head><title>ESG30</title></head><body><section class="main"><ul></ul></section></body></html>`
	got := run(t, esg30Mock(t, empty, esg30NewsPage),
		"esg30-subdirectory", "https://index.caixin.com/news/")
	if code := got.ErrorCode(t); code != "E_NOT_FOUND" {
		t.Errorf("code = %s, want E_NOT_FOUND", code)
	}
	if got.Exit != 3 {
		t.Errorf("exit = %d, want 3", got.Exit)
	}
}

func TestESG30Subdirectory_RejectsForeignURLs(t *testing.T) {
	for _, bad := range []string{
		"https://index.caixin.com/somewhere/",
		"https://example.com/news/",
	} {
		got := run(t, newMockUpstream(t), "esg30-subdirectory", bad)
		if code := got.ErrorCode(t); code != "E_VALIDATION" {
			t.Errorf("%s: code = %s, want E_VALIDATION", bad, code)
		}
	}
}
