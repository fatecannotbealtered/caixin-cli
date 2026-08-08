package caixin

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/antchfx/htmlquery"
	xhtml "golang.org/x/net/html"
)

// A blogger's home page server-renders only its most recent posts; the rest
// arrives from a paged API behind a "load more" button. This command reports
// what the server sent and says so -- `pagination` is
// `ssr_sidebar_latest_only`, and `load_more_available` states that more exists
// without pretending to have it.

// blogAuthorKeyHost captures the author key out of `<key>.blog.caixin.com`.
var blogAuthorKeyHost = regexp.MustCompile(`^([a-z0-9-]{1,63})\.blog\.caixin\.com$`)

// blogAuthorURL canonicalizes an author home page and returns its key.
func blogAuthorURL(raw string) (canonical, authorKey string) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil {
		return "", ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", ""
	}
	match := blogAuthorKeyHost.FindStringSubmatch(strings.ToLower(parsed.Hostname()))
	if match == nil {
		return "", ""
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ""
	}
	switch parsed.Path {
	case "", "/", "/index.html":
	default:
		return "", ""
	}
	return "https://" + match[0] + "/", match[1]
}

// BlogAuthor reads one blogger's home page.
func (c *Client) BlogAuthor(ctx context.Context, pageURL string) (map[string]any, error) {
	canonical, authorKey := blogAuthorURL(pageURL)
	if canonical == "" {
		return nil, invalid("blog-author reads a Caixin blogger home page, " +
			"for example https://zhangming.blog.caixin.com")
	}

	// The blogger directory is read first, both to confirm the author is listed
	// and to take the profile the directory publishes -- the author's own page
	// does not always carry the same name and bio.
	const directoryURL = "https://blog.caixin.com/"
	directoryProfile, err := c.blogDirectoryProfile(ctx, directoryURL, canonical)
	if err != nil {
		return nil, err
	}

	raw, err := c.do(ctx, requestSpec{
		Method: http.MethodGet,
		URL:    canonical,
		Headers: map[string]string{
			"Accept":     "text/html,application/xhtml+xml",
			"User-Agent": TopicsCompatUserAgent,
		},
		Anonymous: true,
	})
	if err != nil {
		return nil, err
	}
	doc, err := xhtml.Parse(strings.NewReader(string(raw)))
	if err != nil {
		return nil, &APIError{Message: "could not parse the blogger page"}
	}

	result, err := blogAuthorDocument(doc, canonical, authorKey)
	if err != nil {
		return nil, err
	}
	result["author_key"] = authorKey
	result["directory_profile"] = directoryProfile
	result["title"] = emptyToNil(firstXPathText(doc, "//title"))
	result["source_mode"] = "server_html"
	result["session_used"] = false
	result["source"] = map[string]any{
		"requested_url":   pageURL,
		"final_url":       canonical,
		"discovered_from": directoryURL,
		"fetched_at":      time.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	return attachClickConsumers(result), nil
}

// blogDirectoryProfile finds this author in the blogger directory.
func (c *Client) blogDirectoryProfile(ctx context.Context, directoryURL, canonical string) (map[string]any, error) {
	raw, err := c.do(ctx, requestSpec{
		Method: http.MethodGet,
		URL:    directoryURL,
		Headers: map[string]string{
			"Accept":     "text/html,application/xhtml+xml",
			"User-Agent": TopicsCompatUserAgent,
		},
		Anonymous: true,
	})
	if err != nil {
		return nil, err
	}
	doc, err := xhtml.Parse(strings.NewReader(string(raw)))
	if err != nil {
		return nil, &APIError{Message: "could not parse the blogger directory"}
	}
	for _, anchor := range htmlquery.Find(doc, "//a[@href]") {
		link, _ := blogAuthorURL(absoluteURL(directoryURL, attr(anchor, "href")))
		if link == "" || link != canonical {
			continue
		}
		// The profile sits inside the anchor: an avatar, a name in <p>, and the
		// one-line bio in <span> beside it.
		image := ""
		if node := firstElement(anchor, ".//img[1]"); node != nil {
			source := attr(node, "data-src")
			if source == "" {
				source = attr(node, "src")
			}
			image = blogImageURL(directoryURL, source)
		}
		name := firstXPathText(anchor, ".//p[1]")
		bio := firstXPathText(anchor, ".//span[1]")
		if name == "" {
			name = nodeText(anchor)
		}
		if bio == name {
			bio = ""
		}
		return map[string]any{
			"name":  emptyToNil(name),
			"bio":   emptyToNil(bio),
			"image": emptyToNil(image),
			"url":   canonical,
		}, nil
	}
	return nil, &APIError{
		StatusCode: 404,
		Message:    "that blogger is not listed in " + directoryURL + " right now",
	}
}

var blogArticleCountPattern = regexp.MustCompile(`^\s*([1-9]\d{0,8})\s*篇文章\s*$`)
var blogUserBlock = regexp.MustCompile(`(?s)\bwindow\.user\s*=\s*\{(.*?)\};`)
var blogUserDomain = regexp.MustCompile(`\bdomain\s*:\s*['"]([a-z0-9-]{1,63})['"]`)
var blogUserID = regexp.MustCompile(`\bid\s*:\s*['"]([1-9]\d{0,19})['"]`)
var blogUserLastTime = regexp.MustCompile(`\blasttime\s*:\s*([1-9]\d{9,12})`)

// blogAuthorDocument extracts the profile and the server-rendered post list.
func blogAuthorDocument(doc *xhtml.Node, pageURL, authorKey string) (map[string]any, error) {
	profileRoot := firstElement(doc, classXPath("author_detail"))
	latestRoot := firstElement(doc, classXPath("new-con"))
	if profileRoot == nil || latestRoot == nil {
		return nil, &APIError{
			Message: "the blogger page is missing its profile block or its post list",
		}
	}

	authorID, lastTime, err := blogUserMetadata(doc, authorKey)
	if err != nil {
		return nil, err
	}

	image := ""
	if node := firstElement(profileRoot, "./img[1]"); node != nil {
		source := attr(node, "data-src")
		if source == "" {
			source = attr(node, "src")
		}
		image = blogImageURL(pageURL, source)
	}
	profile := map[string]any{
		"name":  emptyToNil(firstXPathText(profileRoot, "./p["+classPredicate("name")+"][1]")),
		"bio":   emptyToNil(firstXPathText(profileRoot, "./p["+classPredicate("desc")+"][1]")),
		"image": emptyToNil(image),
	}

	var reportedCount any
	for _, node := range htmlquery.Find(profileRoot, "./p["+classPredicate("data")+"]/span") {
		if match := blogArticleCountPattern.FindStringSubmatch(htmlquery.InnerText(node)); match != nil {
			if value, err := strconv.Atoi(match[1]); err == nil {
				reportedCount = value
			}
			break
		}
	}

	var articles []map[string]any
	for _, anchor := range htmlquery.Find(latestRoot, "./p/a[@href]") {
		link := blogAuthorArticleURL(pageURL, attr(anchor, "href"), authorKey)
		title := nodeText(anchor)
		if link == "" || title == "" {
			continue
		}
		articles = append(articles, map[string]any{
			"title":        title,
			"url":          link,
			"image":        nil,
			"summary":      nil,
			"published_at": nil,
			"badges":       []any{},
			"item_kind":    "article",
		})
	}

	var modules []snapshotModule
	if len(articles) > 0 {
		modules = append(modules, snapshotModule{
			Key: "blog-author.latest", Name: "最新文章",
			Lane: "sidebar", Order: 0, State: "visible", Items: articles,
		})
	}
	result := directoryResult(modules)
	result["profile"] = profile
	result["reported_author_id"] = authorID
	result["reported_articles_count"] = reportedCount
	result["reported_last_published_at_ms"] = lastTime
	if value, ok := lastTime.(int); ok {
		result["reported_last_published_at"] =
			time.UnixMilli(int64(value)).UTC().Format("2006-01-02T15:04:05+00:00")
	} else {
		result["reported_last_published_at"] = nil
	}
	// The page renders only the newest posts; the rest is behind a button this
	// client does not press.
	result["pagination"] = "ssr_sidebar_latest_only"
	result["load_more_available"] = firstElement(doc, classXPath("author-more-blog")) != nil
	// Stated rather than implied: the button was not pressed, the paged API was
	// not called, and the archive links were not followed.
	result["load_more_not_called"] = true
	result["dynamic_api_not_called"] = true
	result["archive_links_not_followed"] = true
	result["linked_pages_not_fetched"] = true
	result["scripts_not_executed"] = true
	return result, nil
}

// blogUserMetadata reads the author id the page embeds for its own paging.
//
// It must belong to *this* author: a page can carry several user blocks, and
// picking the wrong one would page through somebody else's posts.
func blogUserMetadata(doc *xhtml.Node, authorKey string) (any, any, error) {
	var scripts strings.Builder
	for _, node := range htmlquery.Find(doc, "//script") {
		scripts.WriteString(htmlquery.InnerText(node))
		scripts.WriteString("\n")
	}
	for _, match := range blogUserBlock.FindAllStringSubmatch(scripts.String(), -1) {
		body := match[1]
		domain := blogUserDomain.FindStringSubmatch(body)
		if domain == nil || domain[1] != authorKey {
			continue
		}
		var authorID, lastTime any
		if found := blogUserID.FindStringSubmatch(body); found != nil {
			if value, err := strconv.Atoi(found[1]); err == nil {
				authorID = value
			}
		}
		if found := blogUserLastTime.FindStringSubmatch(body); found != nil {
			if value, err := strconv.Atoi(found[1]); err == nil {
				lastTime = value
			}
		}
		return authorID, lastTime, nil
	}
	return nil, nil, &APIError{
		Message: "the blogger page carries no metadata block for this author",
	}
}

// blogArchivePath is the shape a blog post uses: `/archives/<n>`.
var blogArchivePath = regexp.MustCompile(`^/archives/\d{1,12}/?$`)

// blogAuthorArticleURL accepts only posts on this author's own subdomain.
//
// Blog posts do not use the dated article path the rest of the site does, and
// requiring that shape here silently returned an empty post list.
func blogAuthorArticleURL(base, value, authorKey string) string {
	raw := absoluteURL(base, value)
	if raw == "" {
		return ""
	}
	raw = upgradeCaixinScheme(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil {
		return ""
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	if strings.ToLower(parsed.Hostname()) != authorKey+".blog.caixin.com" {
		return ""
	}
	if !blogArchivePath.MatchString(parsed.Path) && !articlePathPattern.MatchString(parsed.Path) {
		return ""
	}
	return parsed.String()
}

// blogImageURL accepts an avatar from the blog image hosts.
func blogImageURL(base, value string) string {
	if image := cultureImageURL(base, value); image != "" {
		return image
	}
	raw := absoluteURL(base, value)
	if raw == "" || strings.ContainsAny(raw, " \t\n\r") {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	if port := parsed.Port(); port != "" && port != "80" && port != "443" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	// Three hosts, three shapes: the blog's own picture host, the avatar
	// service, and the columnist portraits published under the main site. Each
	// is admitted by the path it is known to use.
	switch {
	case (host == "img.caixin.com" || host == "pic.caixin.com") &&
		imagePathPattern.MatchString(parsed.Path):
	case host == "getavatar.caixin.com" && avatarPathPattern.MatchString(parsed.Path):
	case host == "www.caixin.com" && opinionAuthorPortraitPath.MatchString(parsed.Path):
	default:
		return ""
	}
	parsed.Scheme = "https"
	parsed.Host = host
	return parsed.String()
}

// avatarPathPattern is the shape the avatar service publishes under.
var avatarPathPattern = regexp.MustCompile(
	`(?i)^/\d{3}/\d{2}/\d{2}/\d{2}_real_avatar_(small|middle|big)\.(jpe?g|png|webp)$`)

// blogPostURL accepts a blog post, which lives at `/archives/<n>` rather than
// the dated path the rest of the site uses.
func blogPostURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return ""
	}
	if !blogAuthorHost.MatchString(strings.ToLower(parsed.Hostname())) {
		return ""
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	if !blogArchivePath.MatchString(parsed.Path) {
		return ""
	}
	parsed.Host = strings.ToLower(parsed.Hostname())
	return parsed.String()
}
