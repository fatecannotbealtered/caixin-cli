package cmd

import (
	"net/http"
	"testing"
)

const culturePage = `<html><head><title>专栏-财新文化</title></head><body>
<div class="comMain">
 <div class="conlf"><div class="stitXtuwen_list">
  <dl><dd><h4><a href="https://culture.caixin.com/2026-01-01/102000001.html">一篇专栏</a><i class="icon_key" title="收费文章"></i></h4>
      <p>摘要。</p><span>2026-01-01</span></dd></dl>
 </div></div>
 <div class="conri">
  <div class="columnBox"><h3>编辑推荐</h3>
   <div class="listWithPic"><a href="https://culture.caixin.com/2026-01-02/102000002.html"><img src="https://img.caixin.com/a.jpg"><span>推荐一篇</span></a></div>
   <ul class="list"><li><a href="https://culture.caixin.com/novel/">文学</a><a href="https://weekly.caixin.com/m/2026-01-03/102000003.html">移动版一篇</a></li></ul>
  </div>
  <div class="columnBox"><h3>最新文章</h3><ul class="list"><li><a href="https://culture.caixin.com/2026-01-04/102000004.html">最新一篇</a><span>2026-01-04</span></li></ul></div>
 </div>
</div></body></html>`

func cultureMock(t *testing.T, path, page string) *mockUpstream {
	t.Helper()
	mock := newMockUpstream(t)
	mock.handlers[path] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}
	return mock
}

func TestCultureSection_ReadsMainAndSharedSidebar(t *testing.T) {
	got := run(t, cultureMock(t, "/zhuanlan/", culturePage),
		"culture-section", "https://culture.caixin.com/zhuanlan/")
	if got.Exit != 0 {
		t.Fatalf("exit = %d: %s", got.Exit, got.Stdout)
	}
	data := got.Data(t)
	if key, _ := data["section_key"].(string); key != "zhuanlan" {
		t.Errorf("section_key = %q", key)
	}
	if name, _ := data["name"].(string); name != "专栏" {
		t.Errorf("name = %q", name)
	}
	// The `/m/` mobile alias is folded, so the weekly piece counts once.
	if count, _ := data["items_count"].(float64); count != 4 {
		t.Errorf("items_count = %v, want 4", data["items_count"])
	}
	modules, _ := data["modules"].([]any)
	sidebar, _ := modules[1].(map[string]any)
	if shared, _ := sidebar["shared_sidebar"].(bool); !shared {
		t.Error("the sidebar block is not marked shared_sidebar")
	}
}

// The sidebar is shared across the channel, so letting it set the section's
// freshness would make a stale section look current.
func TestCultureSection_FreshnessComesFromTheMainLane(t *testing.T) {
	data := run(t, cultureMock(t, "/zhuanlan/", culturePage),
		"culture-section", "https://culture.caixin.com/zhuanlan/").Data(t)
	if latest, _ := data["latest_article_date"].(string); latest != "2026-01-01" {
		t.Errorf("latest_article_date = %q; it must come from the main list only", latest)
	}
	if sidebar, _ := data["sidebar_latest_article_date"].(string); sidebar != "2026-01-04" {
		t.Errorf("sidebar_latest_article_date = %q", sidebar)
	}
}

func TestCultureSection_DeclaresWhatItSkipped(t *testing.T) {
	data := run(t, cultureMock(t, "/zhuanlan/", culturePage),
		"culture-section", "https://culture.caixin.com/zhuanlan/").Data(t)
	pagination, _ := data["pagination"].(map[string]any)
	if called, _ := pagination["load_more_not_called"].(bool); !called {
		t.Error("load_more_not_called = false")
	}
	if started, _ := data["transactions_not_started"].(bool); !started {
		t.Error("transactions_not_started = false; this client starts no purchase")
	}
}

func TestCultureSection_RejectsUnmeasuredSections(t *testing.T) {
	for _, bad := range []string{
		"https://culture.caixin.com/nosuchsection/",
		"https://example.com/zhuanlan/",
	} {
		got := run(t, newMockUpstream(t), "culture-section", bad)
		if code := got.ErrorCode(t); code != "E_VALIDATION" {
			t.Errorf("%s: code = %s, want E_VALIDATION", bad, code)
		}
	}
}

const cultureAuthorPage = `<html><head><title>刀尔登-财新文化</title></head><body>
<div class="comMainCon">
 <div class="leftbox">
  <div class="columnToutiao"><div class="columnToutiaoCon"><dl>
   <dt><img src="https://img.caixin.com/author.jpg">刀尔登</dt><dd><p>专栏作家</p></dd>
  </dl></div></div>
  <div class="channelBox"><div class="channelBoxCon"><div class="demolNews">
   <dl><dt><a href="https://culture.caixin.com/2026-01-01/102000001.html">我的一篇</a></dt>
       <dd><p><a href="https://culture.caixin.com/2026-01-01/102000001.html">导读文字</a></p><span>2026-01-01</span></dd></dl>
   <dl><dt><a href="https://ucwap.caixin.com/2026-01-02/102000002.html">移动端一篇</a></dt><dd><span>2026-01-02</span></dd></dl>
  </div></div></div>
  <div class="pageNav"><a href="https://culture.caixin.com/daoerdeng/index-9.html">下一页</a></div>
 </div>
 <div class="comMainConri">
  <div class="top10"><div class="top10Con"><div class="top10">
   <dl><dt>1</dt><dd><h4><a href="https://culture.caixin.com/2026-01-03/102000003.html">热门一篇</a></h4></dd></dl>
  </div></div></div>
  <div class="columnist"><div class="channelBoxCon"><div class="demolNews"><ul>
   <li><span><a href="http://culture.caixin.com/liangwendao/"><img src="//img.caixin.com/b.jpg"></a></span><p><a href="http://culture.caixin.com/liangwendao/">梁文道</a></p></li>
   <li><span><a href="http://culture.caixin.com/2012/yangxiaoyan/"><img src="//img.caixin.com/c.jpg"></a></span><p><a href="http://culture.caixin.com/2012/yangxiaoyan/">杨小彦</a></p></li>
  </ul></div></div></div>
 </div>
</div></body></html>`

func TestCultureAuthor_SeparatesTheAuthorFromTheChannel(t *testing.T) {
	got := run(t, cultureMock(t, "/daoerdeng/", cultureAuthorPage),
		"culture-author", "https://culture.caixin.com/daoerdeng/")
	if got.Exit != 0 {
		t.Fatalf("exit = %d: %s", got.Exit, got.Stdout)
	}
	data := got.Data(t)
	// Two of the author's own plus one ranking entry; the roster is counted
	// separately because it is other people.
	if count, _ := data["article_items_count"].(float64); count != 3 {
		t.Errorf("article_items_count = %v, want 3", data["article_items_count"])
	}
	if count, _ := data["author_links_count"].(float64); count != 2 {
		t.Errorf("author_links_count = %v, want 2", data["author_links_count"])
	}
	profile, _ := data["profile"].(map[string]any)
	if bio, _ := profile["bio"].(string); bio != "专栏作家" {
		t.Errorf("profile.bio = %q", bio)
	}
}

// A piece that only exists on the mobile reader host is still listed, flagged
// rather than dropped -- silently shortening a bibliography would be worse.
func TestCultureAuthor_FlagsUnreadableLinksInsteadOfDropping(t *testing.T) {
	data := run(t, cultureMock(t, "/daoerdeng/", cultureAuthorPage),
		"culture-author", "https://culture.caixin.com/daoerdeng/").Data(t)
	modules, _ := data["modules"].([]any)
	main, _ := modules[0].(map[string]any)
	items, _ := main["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("main items = %d, want 2", len(items))
	}
	second, _ := items[1].(map[string]any)
	if available, present := second["article_adapter_available"]; !present || available != false {
		t.Error("the mobile-host entry is not flagged as unreadable")
	}
	if status, _ := second["link_status"].(string); status != "mobile_reader_host" {
		t.Errorf("link_status = %q", status)
	}
}

// Older columnists live at a dated path; rejecting that shape dropped a fifth
// of the roster.
func TestCultureAuthor_AcceptsDatedColumnistPaths(t *testing.T) {
	data := run(t, cultureMock(t, "/daoerdeng/", cultureAuthorPage),
		"culture-author", "https://culture.caixin.com/daoerdeng/").Data(t)
	modules, _ := data["modules"].([]any)
	roster, _ := modules[2].(map[string]any)
	items, _ := roster["items"].([]any)
	found := false
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if link, _ := item["url"].(string); link == "https://culture.caixin.com/2012/yangxiaoyan/" {
			found = true
		}
	}
	if !found {
		t.Error("the dated columnist path was dropped from the roster")
	}
}

func TestCultureAuthor_RejectsSectionURLs(t *testing.T) {
	got := run(t, newMockUpstream(t), "culture-author", "https://culture.caixin.com/zhuanlan/")
	if code := got.ErrorCode(t); code != "E_VALIDATION" {
		t.Errorf("code = %s, want E_VALIDATION (a section is not an author)", code)
	}
}
