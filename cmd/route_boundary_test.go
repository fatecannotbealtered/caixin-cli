package cmd

import "testing"

// An unsupported url is not all one thing. "there is no adapter for this"
// invites a bug report; "this is a PDF" or "this is the app download page" is a
// final answer. The boundary code is what lets an agent tell them apart, so
// each one is pinned.
func TestRoute_ClassifiesNonContentBoundaries(t *testing.T) {
	cases := map[string]string{
		"https://img.caixin.com/2024-01-01/1.jpg":       "media_asset",
		"https://www.caixin.com/reports/annual.pdf":     "download_asset",
		"https://www.caixin.com/reports/notes.docx":     "download_asset",
		"https://mall.caixin.com/mall/web/product.html": "transaction_or_product_detail",
		"https://cxdata.caixin.com/dashboard/":          "independent_product",
		"https://mobile.caixin.com/home/":               "mobile_app",
		"https://example.com/anything":                  "external",
		"https://conferences.caixin.com/2025/summit/":   "unsupported_caixin_url",
	}
	for input, want := range cases {
		got := run(t, newMockUpstream(t), "route", input)
		if got.Exit != 0 {
			t.Fatalf("%s: exit = %d: %s", input, got.Exit, got.Stdout)
		}
		data := got.Data(t)
		if supported, _ := data["supported"].(bool); supported {
			t.Errorf("%s: reported as supported", input)
			continue
		}
		if boundary, _ := data["boundary"].(string); boundary != want {
			t.Errorf("%s: boundary = %q, want %q", input, boundary, want)
		}
		// A boundary verdict has nothing to consume, so it must not pretend to.
		if _, present := data["command"]; present {
			t.Errorf("%s: a boundary verdict carried a command", input)
		}
	}
}

// section-directory is an adapter for real channel sections, not a catch-all.
// Routing every unknown Caixin url to it would hand an agent a command that can
// only fail.
func TestRoute_SectionDirectoryIsNotACatchAll(t *testing.T) {
	data := run(t, newMockUpstream(t), "route", "https://finance.caixin.com/finance/").Data(t)
	if adapter, _ := data["adapter"].(string); adapter != "section-directory" {
		t.Errorf("a real channel section routed to %q", adapter)
	}

	data = run(t, newMockUpstream(t), "route", "https://weekly.caixin.com/some/deep/path/").Data(t)
	if supported, _ := data["supported"].(bool); supported {
		t.Errorf("an unrecognized path was routed to an adapter: %v", data["adapter"])
	}
}

// snapshot only accepts entry points whose markup has been measured; matching
// any bare host would route unknown subdomains to a command that refuses them.
func TestRoute_SnapshotHonoursItsAllowlist(t *testing.T) {
	data := run(t, newMockUpstream(t), "route", "https://datanews.caixin.com/").Data(t)
	if adapter, _ := data["adapter"].(string); adapter != "snapshot" {
		t.Errorf("a measured entry point routed to %q", adapter)
	}

	data = run(t, newMockUpstream(t), "route", "https://conferences.caixin.com/").Data(t)
	if supported, _ := data["supported"].(bool); supported {
		t.Error("an unmeasured bare host routed to snapshot")
	}
}

// The data-topic directory links three path shapes; recognizing only one of
// them silently dropped more than half the listing.
func TestRoute_AcceptsEveryDataTopicShape(t *testing.T) {
	for _, input := range []string{
		"https://datanews.caixin.com/interactive/2020/us-president-election/",
		"https://datanews.caixin.com/mobile/lxzj/",
		"https://datanews.caixin.com/2016/fang",
	} {
		data := run(t, newMockUpstream(t), "route", input).Data(t)
		if adapter, _ := data["adapter"].(string); adapter != "datanews-interactive" {
			t.Errorf("%s: adapter = %q, want datanews-interactive", input, adapter)
		}
	}
}
