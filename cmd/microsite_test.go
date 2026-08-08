package cmd

import "testing"

// A microsite is read only from the measured allowlist: these pages share no
// template, so an unmeasured url would parse into a plausible, wrong surface.
func TestMicrosite_AcceptsOnlyMeasuredEntrypoints(t *testing.T) {
	for _, bad := range []string{
		"https://topics.caixin.com/2026/not-a-summit/",
		"https://example.com/2025/caixin_summit2025/",
		"https://topics.caixin.com/2025/caixin_summit2025/?utm_source=elsewhere",
		"https://topics.caixin.com/2025/caixin_summit2025/#section",
	} {
		got := run(t, newMockUpstream(t), "microsite", bad)
		if code := got.ErrorCode(t); code != "E_VALIDATION" {
			t.Errorf("%s: code = %s, want E_VALIDATION", bad, code)
		}
	}
}

// A campaign page has no url shape of its own, so it is refused unless it is on
// the promote host and is not something a more specific adapter already reads.
func TestESG30Resource_RefusesWhatOtherAdaptersOwn(t *testing.T) {
	for _, bad := range []string{
		"https://index.caixin.com/esg30/",
		"https://promote.caixin.com/2026-01-01/102000001.html",
		"https://promote.caixin.com/banner.pdf",
		"https://promote.caixin.com/szsd/",
	} {
		got := run(t, newMockUpstream(t), "esg30-resource", bad)
		if code := got.ErrorCode(t); code != "E_VALIDATION" {
			t.Errorf("%s: code = %s, want E_VALIDATION", bad, code)
		}
	}
}
