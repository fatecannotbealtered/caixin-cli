package cmd

import (
	"net/http"
	"testing"
)

// The 观点 directories all refuse a url outside their own entry point rather
// than fetching it: each reads one exact page, and pointing one at another's
// entry would parse the wrong template into a confidently wrong listing.
func TestOpinionDirectories_RefuseEachOthersEntrypoints(t *testing.T) {
	for _, probe := range []struct{ command, url string }{
		{"opinion-columns", "https://opinion.caixin.com/upfront/"},
		{"opinion-upfront", "https://opinion.caixin.com/columns/"},
		{"opinion-author-directory", "https://opinion.caixin.com/columns/"},
		{"opinion-columns", "https://opinion.caixin.com/nosuchdirectory/"},
	} {
		got := run(t, newMockUpstream(t), probe.command, probe.url)
		if code := got.ErrorCode(t); code != "E_VALIDATION" {
			t.Errorf("%s %s: code = %s, want E_VALIDATION", probe.command, probe.url, code)
		}
	}
}

// A page number is a caller's explicit request. Zero is not one.
func TestOpinionDirectories_RejectNonPositivePages(t *testing.T) {
	for _, command := range []string{
		"opinion-columns", "opinion-upfront", "opinion-author-directory",
	} {
		got := run(t, newMockUpstream(t), command,
			"https://opinion.caixin.com/columns/", "--page", "0")
		if code := got.ErrorCode(t); code != "E_VALIDATION" {
			t.Errorf("%s: code = %s, want E_VALIDATION", command, code)
		}
	}
}

// The 观点 front page is the discovery gate. When it does not list a directory,
// the command stops instead of fetching the url anyway.
func TestOpinionColumns_RequiresTheFrontPageToListIt(t *testing.T) {
	mock := newMockUpstream(t)
	mock.handlers["/"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><div class="gdNav"></div></body></html>`))
	}
	got := run(t, mock, "opinion-columns", "https://opinion.caixin.com/columns/")
	if code := got.ErrorCode(t); code != "E_NOT_FOUND" {
		t.Errorf("code = %s, want E_NOT_FOUND", code)
	}
}

// An author page is only read after something listed the author, and the caller
// says which listing. A directory page number without that source, or a source
// that needs one without it, is refused before any request is made.
func TestOpinionAuthor_RequiresACoherentDiscoverySource(t *testing.T) {
	const author = "https://opinion.caixin.com/wuqianli_mjxx/"
	for _, probe := range []struct {
		name string
		args []string
	}{
		{"unknown source", []string{author, "--discovery-source", "guessing"}},
		{"directory without page", []string{author, "--discovery-source", "author-directory"}},
		{"page without directory", []string{author, "--directory-page", "2"}},
		{"page zero", []string{author, "--page", "0"}},
	} {
		args := append([]string{"opinion-author"}, probe.args...)
		got := run(t, newMockUpstream(t), args...)
		if code := got.ErrorCode(t); code != "E_VALIDATION" {
			t.Errorf("%s: code = %s, want E_VALIDATION", probe.name, code)
		}
	}
}

// A section url is not an author page, even though the two share a path shape.
func TestOpinionAuthor_RejectsADirectoryURL(t *testing.T) {
	got := run(t, newMockUpstream(t), "opinion-author", "https://opinion.caixin.com/")
	if code := got.ErrorCode(t); code != "E_VALIDATION" {
		t.Errorf("code = %s, want E_VALIDATION", code)
	}
}
