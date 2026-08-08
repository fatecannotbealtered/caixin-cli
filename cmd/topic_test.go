package cmd

import "testing"

// `topic` covers three products that share one word. The url decides which, so
// a url none of them serves is refused rather than sent to whichever adapter
// happens to be last.
func TestTopic_RejectsURLsNoSurfaceServes(t *testing.T) {
	for _, bad := range []string{
		"https://deepview.caixin.com/topic/not-a-code.html",
		"https://key.caixin.com/topic/BQ02",
		"https://example.com/topic/BQ02.000000368",
		"https://key.caixin.com/topic/BQ02.000000368?tracking=everything",
	} {
		got := run(t, newMockUpstream(t), "topic", bad)
		if code := got.ErrorCode(t); code != "E_VALIDATION" {
			t.Errorf("%s: code = %s, want E_VALIDATION", bad, code)
		}
	}
}

// The app's static topic pages are a fourth surface this build does not read.
// Saying so plainly is more useful than parsing them into something wrong.
func TestTopic_SaysSoAboutTheAppSurface(t *testing.T) {
	got := run(t, newMockUpstream(t), "topic",
		"https://mappv5.caixin.com/m_topic_detail/1630.html")
	// A capability gap is not retryable, so it must not arrive as a 7.
	if code := got.ErrorCode(t); code != "E_VALIDATION" {
		t.Errorf("code = %s, want E_VALIDATION", code)
	}
}
