package cmd

import (
	"net/http"
	"testing"
)

const blogDirectoryPage = `<html><body>
<a href="https://tangya.blog.caixin.com/">
 <div class="line"><img src="https://getavatar.caixin.com/006/80/23/22_real_avatar_middle.jpg" alt="唐涯">
  <div class="right"><p>唐涯</p><span>金融学者</span></div></div>
</a></body></html>`

const blogAuthorPage = `<html><head><title>唐涯的博客</title></head><body>
<div class="author_detail"><img src="https://getavatar.caixin.com/006/80/23/22_real_avatar_middle.jpg">
 <p class="name">唐涯</p><p class="desc">金融学者</p>
 <p class="data"><span>128篇文章</span></p></div>
<div class="new-con">
 <p><a href="https://tangya.blog.caixin.com/archives/288123">最新一篇</a></p>
 <p><a href="https://other.blog.caixin.com/archives/1">别人的一篇</a></p>
</div>
<div class="author-more-blog">加载更多</div>
<script>window.user = {domain:'tangya', id:'12345', lasttime:1754280000000};</script>
</body></html>`

func blogMock(t *testing.T, directory, author string) *mockUpstream {
	t.Helper()
	mock := newMockUpstream(t)
	calls := 0
	mock.handlers["/"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// The directory is fetched first, then the author page; both are served
		// from "/" because the replay keys on path.
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(directory))
			return
		}
		_, _ = w.Write([]byte(author))
	}
	return mock
}

const blogAuthorURLValue = "https://tangya.blog.caixin.com/"

func TestBlogAuthor_ReadsProfileAndServerRenderedPosts(t *testing.T) {
	got := run(t, blogMock(t, blogDirectoryPage, blogAuthorPage), "blog-author", blogAuthorURLValue)
	if got.Exit != 0 {
		t.Fatalf("exit = %d: %s", got.Exit, got.Stdout)
	}
	data := got.Data(t)

	// Only this author's own posts; the neighbour's link is dropped.
	if count, _ := data["items_count"].(float64); count != 1 {
		t.Errorf("items_count = %v, want 1", data["items_count"])
	}
	if id, _ := data["reported_author_id"].(string); id != "12345" {
		t.Errorf("reported_author_id = %v", data["reported_author_id"])
	}
	profile, _ := data["directory_profile"].(map[string]any)
	if bio, _ := profile["bio"].(string); bio != "金融学者" {
		t.Errorf("directory_profile.bio = %q", bio)
	}
}

// The page renders only its newest posts. Saying the listing is complete would
// be the damaging claim here.
func TestBlogAuthor_DeclaresWhatItDidNotFetch(t *testing.T) {
	data := run(t, blogMock(t, blogDirectoryPage, blogAuthorPage),
		"blog-author", blogAuthorURLValue).Data(t)
	if page, _ := data["pagination"].(string); page != "ssr_sidebar_latest_only" {
		t.Errorf("pagination = %q", page)
	}
	for _, field := range []string{
		"load_more_available", "load_more_not_called",
		"dynamic_api_not_called", "archive_links_not_followed",
	} {
		if value, _ := data[field].(bool); !value {
			t.Errorf("%s = false", field)
		}
	}
	if complete, _ := data["complete_listing_verified"].(bool); complete {
		t.Error("complete_listing_verified = true for a partial server render")
	}
}

// A blogger the directory does not list must not be fetched by url alone.
func TestBlogAuthor_RefusesAnUnlistedBlogger(t *testing.T) {
	empty := `<html><body><p>no bloggers</p></body></html>`
	got := run(t, blogMock(t, empty, blogAuthorPage), "blog-author", blogAuthorURLValue)
	if code := got.ErrorCode(t); code != "E_NOT_FOUND" {
		t.Errorf("code = %s, want E_NOT_FOUND", code)
	}
}

func TestBlogAuthor_RejectsNonBloggerURLs(t *testing.T) {
	for _, bad := range []string{
		"https://blog.caixin.com/",
		"https://tangya.blog.caixin.com/archives/1",
		"https://example.com/",
	} {
		got := run(t, newMockUpstream(t), "blog-author", bad)
		if code := got.ErrorCode(t); code != "E_VALIDATION" {
			t.Errorf("%s: code = %s, want E_VALIDATION", bad, code)
		}
	}
}
