package cmd

import (
	"net/http"
	"strings"
	"testing"
)

// The channel front lists its sections inside `indexMain`; a link outside it is
// not a section the front publishes.
const sectionParentPage = `<html><body><div class="indexMain">
<a href="https://finance.caixin.com/regulation/">监管</a></div></body></html>`

func sectionPage(extraList string) string {
	return `<html><head><title>监管-财新网</title></head><body>
<div class="comMain">
 <div class="conlf"><div class="stitXtuwen_list">
  <dl><dd><h4><a href="https://finance.caixin.com/2026-01-01/102000001.html">一篇报道</a><i class="icon_key" title="收费文章"></i></h4>
      <p>摘要。</p><span>2026-01-01</span></dd></dl>
 </div>` + extraList + `</div>
 <div class="conri">
  <div class="columnBox"><h3>编辑推荐</h3><ul><li><a href="https://finance.caixin.com/2026-01-02/102000002.html">推荐一篇</a></li></ul></div>
  <div class="columnBox"><h3>最新文章</h3><ul><li><a href="https://finance.caixin.com/2026-01-03/102000003.html">最新一篇</a></li></ul></div>
 </div>
</div></body></html>`
}

func sectionMock(t *testing.T, page string) *mockUpstream {
	t.Helper()
	mock := newMockUpstream(t)
	mock.handlers["/"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(sectionParentPage))
	}
	mock.handlers["/regulation/"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}
	return mock
}

const sectionURL = "https://finance.caixin.com/regulation/"

func TestSectionDirectory_ReadsTheThreeBlocks(t *testing.T) {
	got := run(t, sectionMock(t, sectionPage("")), "section-directory", sectionURL)
	if got.Exit != 0 {
		t.Fatalf("exit = %d: %s", got.Exit, got.Stdout)
	}
	data := got.Data(t)
	if count, _ := data["modules_count"].(float64); count != 3 {
		t.Errorf("modules_count = %v, want 3", data["modules_count"])
	}
	if title, _ := data["directory_title"].(string); title != "监管" {
		t.Errorf("directory_title = %q; it comes from the parent front page", title)
	}
	source, _ := data["source"].(map[string]any)
	if from, _ := source["discovered_from"].(string); !strings.HasSuffix(from, "/") {
		t.Errorf("discovered_from = %q", from)
	}
}

// The layout is the contract here. A page that grew a second article list means
// the template moved, and half a directory returned confidently is worse than
// an error.
func TestSectionDirectory_RefusesAChangedTemplate(t *testing.T) {
	doubled := sectionPage(`<div class="stitXtuwen_list"><dl></dl></div>`)
	got := run(t, sectionMock(t, doubled), "section-directory", sectionURL)
	if got.Exit == 0 {
		t.Fatalf("a changed template returned success: %s", got.Stdout)
	}
	if !strings.Contains(got.Stdout, "exactly one article list") {
		t.Errorf("the error does not name what changed: %s", got.Stdout)
	}
}

// A section the front page no longer links to must not be fetched just because
// its url still has the right shape.
func TestSectionDirectory_RefusesAnUndiscoveredSection(t *testing.T) {
	mock := sectionMock(t, sectionPage(""))
	mock.handlers["/"] = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html><body><p>no sections</p></body></html>`))
	}
	got := run(t, mock, "section-directory", sectionURL)
	if code := got.ErrorCode(t); code != "E_NOT_FOUND" {
		t.Errorf("code = %s, want E_NOT_FOUND", code)
	}
}

func TestSectionDirectory_RejectsNonSectionURLs(t *testing.T) {
	for _, bad := range []string{
		"https://finance.caixin.com/",
		"https://conferences.caixin.com/summit/",
		"https://example.com/x/",
	} {
		got := run(t, newMockUpstream(t), "section-directory", bad)
		if code := got.ErrorCode(t); code != "E_VALIDATION" {
			t.Errorf("%s: code = %s, want E_VALIDATION", bad, code)
		}
	}
}
