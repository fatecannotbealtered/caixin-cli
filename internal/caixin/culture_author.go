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

// A 文化 columnist's page carries three blocks: the author's own pieces, the
// channel's click ranking, and the roster of other columnists. Only the first
// is about this author, so the counts are reported separately -- an agent
// summarising "how much has this person written" must not count the ranking.

// A columnist page is `/<name>/` or, for older ones, `/<year>/<name>/`.
var cultureAuthorPath = regexp.MustCompile(`^/(?:20\d{2}/)?([A-Za-z0-9_-]{1,64})(?:/index\.html|/)?$`)

// cultureAuthorURL canonicalizes a columnist page and returns its key.
func cultureAuthorURL(raw string) (canonical, authorKey string) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil {
		return "", ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", ""
	}
	if strings.ToLower(parsed.Hostname()) != "culture.caixin.com" {
		return "", ""
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ""
	}
	match := cultureAuthorPath.FindStringSubmatch(parsed.Path)
	if match == nil {
		return "", ""
	}
	// A section and an author page share this path shape; the section list wins.
	if _, isSection := CultureSections[match[1]]; isSection {
		return "", ""
	}
	// The dated spelling is preserved: it is where the page actually lives, and
	// rewriting it to the bare form would point at a url that does not exist.
	parsed.Scheme = "https"
	parsed.Host = "culture.caixin.com"
	return parsed.String(), match[1]
}

// CultureAuthor reads one 文化 columnist page.
func (c *Client) CultureAuthor(ctx context.Context, pageURL string) (map[string]any, error) {
	canonical, authorKey := cultureAuthorURL(pageURL)
	if canonical == "" {
		return nil, invalid("culture-author reads a Caixin 文化 columnist page, " +
			"for example https://culture.caixin.com/daoerdeng/")
	}

	raw, err := c.do(ctx, requestSpec{
		Method:    http.MethodGet,
		URL:       canonical,
		Headers:   map[string]string{"Accept": "text/html,application/xhtml+xml"},
		Anonymous: true,
	})
	if err != nil {
		return nil, err
	}
	doc, err := xhtml.Parse(strings.NewReader(string(raw)))
	if err != nil {
		return nil, &APIError{Message: "could not parse the columnist page"}
	}

	result, err := cultureAuthorDocument(doc, canonical, authorKey)
	if err != nil {
		return nil, err
	}
	result["author_key"] = authorKey
	result["title"] = emptyToNil(firstXPathText(doc, "//title"))
	result["source"] = map[string]any{
		"requested_url": pageURL,
		"final_url":     canonical,
		"fetched_at":    time.Now().UTC().Format("2006-01-02T15:04:05+00:00"),
	}
	return attachClickConsumers(result), nil
}

// cultureAuthorArticleLink resolves a link on a columnist page.
//
// Some entries point at the mobile reader host instead of the article. Those
// are still listed, flagged with `article_adapter_available: false`, because
// dropping them would silently shorten the author's bibliography.
func cultureAuthorArticleLink(base, value string) (link string, adapterAvailable bool, status string, ok bool) {
	if article := cultureArticleURL(base, value); article != "" {
		return article, true, "", true
	}
	raw := absoluteURL(base, value)
	if raw == "" || strings.ContainsAny(raw, " \t\n\r") {
		return "", false, "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil {
		return "", false, "", false
	}
	if strings.ToLower(parsed.Hostname()) != "ucwap.caixin.com" {
		return "", false, "", false
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false, "", false
	}
	if !articlePathPattern.MatchString(parsed.Path) {
		return "", false, "", false
	}
	return raw, false, "mobile_reader_host", true
}

// cultureAuthorItem builds one entry from a columnist block.
func cultureAuthorItem(node *xhtml.Node, pageURL, anchorXPath, summaryXPath, publishedXPath, rankXPath string) map[string]any {
	anchor := firstElement(node, anchorXPath)
	if anchor == nil {
		return nil
	}
	link, adapterAvailable, status, ok := cultureAuthorArticleLink(pageURL, attr(anchor, "href"))
	title := nodeText(anchor)
	if !ok || title == "" {
		return nil
	}

	summary := ""
	if summaryXPath != "" {
		if summaryAnchor := firstElement(node, summaryXPath); summaryAnchor != nil {
			candidate, _, _, candidateOK := cultureAuthorArticleLink(pageURL, attr(summaryAnchor, "href"))
			if candidateOK && candidate == link {
				summary = nodeText(summaryAnchor)
			}
		}
	}
	published := ""
	if publishedXPath != "" {
		published = firstXPathText(node, publishedXPath)
	}

	item := map[string]any{
		"title":        title,
		"url":          link,
		"image":        nil,
		"summary":      emptyToNil(summary),
		"published_at": emptyToNil(published),
		"badges":       []any{},
		"item_kind":    "article",
	}
	if !adapterAvailable {
		item["article_adapter_available"] = false
		item["link_status"] = status
	}
	if rankXPath != "" {
		if value, err := strconv.Atoi(strings.TrimSpace(firstXPathText(node, rankXPath))); err == nil {
			item["rank"] = value
		}
	}
	return item
}

// cultureAuthorDocument extracts the three blocks.
func cultureAuthorDocument(doc *xhtml.Node, pageURL, authorKey string) (map[string]any, error) {
	root := firstElement(doc, "/html/body/div["+classPredicate("comMainCon")+"]")
	if root == nil {
		return nil, &APIError{Message: "the columnist page is missing its comMainCon"}
	}
	left := firstElement(root, "./div["+classPredicate("leftbox")+"]")
	right := firstElement(root, "./div["+classPredicate("comMainConri")+"]")
	if left == nil || right == nil {
		return nil, &APIError{Message: "the columnist page is missing a content column"}
	}

	profileRoot := firstElement(left,
		"./div["+classPredicate("columnToutiao")+"]/div["+classPredicate("columnToutiaoCon")+"]/dl")
	if profileRoot == nil {
		return nil, &APIError{Message: "the columnist page is missing its author profile"}
	}
	profileName := firstXPathText(profileRoot, "./dt[1]")
	if profileName == "" {
		return nil, &APIError{Message: "the columnist page carries no author name"}
	}
	image := ""
	if node := firstElement(profileRoot, "./dt/img[1]"); node != nil {
		source := attr(node, "data-src")
		if source == "" {
			source = attr(node, "src")
		}
		image = cultureImageURL(pageURL, source)
	}
	profile := map[string]any{
		"name":  profileName,
		"bio":   emptyToNil(firstXPathText(profileRoot, "./dd/p[1]")),
		"image": emptyToNil(image),
	}

	var articles []map[string]any
	if node := firstElement(left, "./div["+classPredicate("channelBox")+"]/div["+
		classPredicate("channelBoxCon")+"]/div["+classPredicate("demolNews")+"]"); node != nil {
		for _, row := range htmlquery.Find(node, "./dl") {
			if item := cultureAuthorItem(row, pageURL,
				"./dt/a[@href][1]", "./dd/p/a[@href][1]", "./dd/span[1]", ""); item != nil {
				articles = append(articles, item)
			}
		}
	}

	var ranking []map[string]any
	if node := firstElement(right, "./div["+classPredicate("top10")+"]/div["+
		classPredicate("top10Con")+"]//div["+classPredicate("top10")+"]"); node != nil {
		for _, row := range htmlquery.Find(node, "./dl") {
			if item := cultureAuthorItem(row, pageURL,
				"./dd/h4/a[@href][1]", "", "", "./dt[1]"); item != nil {
				ranking = append(ranking, item)
			}
		}
	}

	var columnists []map[string]any
	seenAuthors := map[string]bool{}
	if node := firstElement(right, "./div["+classPredicate("columnist")+"]/div["+
		classPredicate("channelBoxCon")+"]/div["+classPredicate("demolNews")+"]/ul"); node != nil {
		for _, row := range htmlquery.Find(node, "./li") {
			anchor := firstElement(row, "./p/a[@href][1]")
			if anchor == nil {
				anchor = firstElement(row, ".//a[@href][img][1]")
			}
			if anchor == nil {
				continue
			}
			link, _ := cultureAuthorURL(absoluteURL(pageURL, attr(anchor, "href")))
			name := nodeText(anchor)
			if link == "" || name == "" || seenAuthors[link] {
				continue
			}
			authorImage := ""
			if imageNode := firstElement(row, ".//img[1]"); imageNode != nil {
				source := attr(imageNode, "data-src")
				if source == "" {
					source = attr(imageNode, "src")
				}
				authorImage = cultureImageURL(pageURL, source)
			}
			seenAuthors[link] = true
			columnists = append(columnists, map[string]any{
				"title":     name,
				"url":       link,
				"image":     emptyToNil(authorImage),
				"summary":   nil,
				"badges":    []any{},
				"item_kind": "author_profile",
			})
		}
	}

	links := []any{}
	if node := firstElement(left, ".//div["+classPredicate("pageNav")+"]"); node != nil {
		for _, anchor := range htmlquery.Find(node, "./a[@href]") {
			href := strings.TrimSpace(attr(anchor, "href"))
			if href == "" || strings.HasPrefix(href, "#") {
				continue
			}
			link := cultureAuthorPaginationURL(pageURL, href, authorKey)
			label := nodeText(anchor)
			if link != "" && label != "" {
				links = append(links, map[string]any{"label": label, "url": link})
			}
		}
	}

	var modules []snapshotModule
	for _, block := range []struct {
		key, name, lane string
		order           int
		items           []map[string]any
	}{
		{"culture-author.articles", "专栏文章", "main", 0, articles},
		{"culture-author.ranking", "文化频道点击排行榜", "sidebar", 0, ranking},
		{"culture-author.columnists", "专栏作家", "sidebar", 1, columnists},
	} {
		if len(block.items) == 0 {
			continue
		}
		modules = append(modules, snapshotModule{
			Key: block.key, Name: block.name, Lane: block.lane,
			Order: block.order, State: "visible", Items: block.items,
		})
	}

	result := directoryResult(modules)
	result["profile"] = profile
	// Counted separately: the roster is other people, and the ranking is the
	// channel's, not this author's.
	result["article_items_count"] = len(articles) + len(ranking)
	result["author_links_count"] = len(columnists)
	result["pagination"] = map[string]any{
		"links":        links,
		"not_followed": true,
	}
	result["linked_pages_not_fetched"] = true
	result["scripts_not_executed"] = true
	result["load_more_not_called"] = true
	result["session_used"] = false
	return result, nil
}

var cultureAuthorPagePath = regexp.MustCompile(`^/(?:20\d{2}/)?([A-Za-z0-9_-]{1,64})/index-(\d{1,4})\.html$`)

// cultureAuthorPaginationURL accepts this author's own static page links.
func cultureAuthorPaginationURL(base, value, authorKey string) string {
	raw := absoluteURL(base, value)
	if raw == "" || strings.ContainsAny(raw, " \t\n\r") {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil {
		return ""
	}
	if strings.ToLower(parsed.Hostname()) != "culture.caixin.com" {
		return ""
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	if match := cultureAuthorPagePath.FindStringSubmatch(parsed.Path); match != nil {
		if match[1] != authorKey {
			return ""
		}
		return "https://culture.caixin.com" + parsed.Path
	}
	if canonical, key := cultureAuthorURL(raw); key == authorKey {
		return canonical
	}
	return ""
}
