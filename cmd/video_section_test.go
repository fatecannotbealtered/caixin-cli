package cmd

import (
	"net/http"
	"testing"
)

// The video channel front is the discovery gate: a directory it does not list
// is not fetched, however well-formed the url looks.
func TestVideoSection_RequiresTheChannelFrontToListIt(t *testing.T) {
	mock := newMockUpstream(t)
	mock.handlers["/"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><div class="navlist"></div></body></html>`))
	}
	got := run(t, mock, "video-section", "https://video.caixin.com/dr/")
	if code := got.ErrorCode(t); code != "E_NOT_FOUND" {
		t.Errorf("code = %s, want E_NOT_FOUND", code)
	}
}

func TestVideoSection_RejectsWhatIsNotAChannelDirectory(t *testing.T) {
	for _, bad := range []string{
		"https://video.caixin.com/2022-11-17/101966223.html",
		"https://culture.caixin.com/zhuanlan/",
		"https://video.caixin.com/dr/?page=2",
		"https://video.caixin.com/dr/deep/path/",
	} {
		got := run(t, newMockUpstream(t), "video-section", bad)
		if code := got.ErrorCode(t); code != "E_VALIDATION" {
			t.Errorf("%s: code = %s, want E_VALIDATION", bad, code)
		}
	}
}

func TestVideoSection_RejectsNonPositivePages(t *testing.T) {
	got := run(t, newMockUpstream(t), "video-section",
		"https://video.caixin.com/dr/", "--page", "0")
	if code := got.ErrorCode(t); code != "E_VALIDATION" {
		t.Errorf("code = %s, want E_VALIDATION", code)
	}
}
