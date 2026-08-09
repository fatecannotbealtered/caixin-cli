package cmd

import (
	"net/http"
	"testing"
)

const issuePage = `<html><head><title>财新周刊 2026年第01期</title></head><body>
<div class="mainMagContent">
 <div class="cover"><img src="http://img.caixin.com/2026-01-01/cover.jpg"></div>
 <div class="report"><div class="title">封面报道 总第 1217 期</div>
  <div class="source">2026年第01期 出版日期：2026-01-05</div>
  <div class="magazine-container"><div class="reportTit">封面文章</div>
   <dl><dt><a href="https://weekly.caixin.com/2026-01-05/102000001.html">封面一篇</a></dt>
       <dd class="date">记者 某某</dd><dd>一段导读。</dd></dl>
  </div>
 </div>
 <div class="magIntro2">
  <div class="magContentlf2">
   <div class="magIntrotit"><span>经济</span></div>
   <dl><dt><a href="https://weekly.caixin.com/2026-01-05/102000002.html">经济一篇</a></dt><dd>导读。</dd></dl>
  </div>
  <div class="magContentce">
   <div class="magIntrotit"><span>空栏目</span></div>
  </div>
 </div>
</div>
<script>var thisMagazineId = 4242; var magazineTotalNum = 1217;</script>
</body></html>`

const issueURL = "https://weekly.caixin.com/2026/cw1217/"

func issueMock(t *testing.T, page string) *mockUpstream {
	t.Helper()
	mock := newMockUpstream(t)
	mock.handlers["/2026/cw1217/"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}
	return mock
}

func TestIssue_ReadsContentsAndMetadata(t *testing.T) {
	got := run(t, issueMock(t, issuePage), "issue", issueURL)
	if got.Exit != 0 {
		t.Fatalf("exit = %d: %s", got.Exit, got.Stdout)
	}
	data := got.Data(t)

	for field, want := range map[string]any{
		"publication":    "weekly",
		"issue_number":   float64(1217),
		"total_issue":    float64(1217),
		"annual_issue":   float64(1),
		"published_at":   "2026-01-05T00:00:00Z",
		"magazine_id":    "4242",
		"articles_count": float64(2),
	} {
		if data[field] != want {
			t.Errorf("%s = %v, want %v", field, data[field], want)
		}
	}
	// A column heading with nothing under it is layout, not content.
	sections, _ := data["sections"].([]any)
	if len(sections) != 2 {
		t.Errorf("sections = %d, want 2 (the empty column must be dropped)", len(sections))
	}
}

// The issue lists; it never fetches a body.
func TestIssue_IsDirectoryOnly(t *testing.T) {
	data := run(t, issueMock(t, issuePage), "issue", issueURL).Data(t)
	if only, _ := data["directory_only"].(bool); !only {
		t.Error("directory_only = false; this command fetches no article body")
	}
	sections, _ := data["sections"].([]any)
	first, _ := sections[0].(map[string]any)
	articles, _ := first["articles"].([]any)
	article, _ := articles[0].(map[string]any)
	if access, _ := article["access"].(string); access != "directory_visible" {
		t.Errorf("access = %q, want directory_visible", access)
	}
	if byline, _ := article["byline"].(string); byline != "记者 某某" {
		t.Errorf("byline = %q", byline)
	}
}

func TestIssue_RejectsNonIssueURLs(t *testing.T) {
	for _, bad := range []string{
		"https://weekly.caixin.com/",
		"https://www.caixin.com/2026/cw1217/",
		"https://example.com/2026/cw1217/",
	} {
		got := run(t, newMockUpstream(t), "issue", bad)
		if code := got.ErrorCode(t); code != "E_VALIDATION" {
			t.Errorf("%s: code = %s, want E_VALIDATION", bad, code)
		}
	}
}

func TestIssue_MissingContainerIsReported(t *testing.T) {
	got := run(t, issueMock(t, "<html><body><p>nothing</p></body></html>"), "issue", issueURL)
	if got.Exit == 0 {
		t.Fatalf("a page with no contents returned success: %s", got.Stdout)
	}
}
