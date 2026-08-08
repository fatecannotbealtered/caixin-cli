package caixin

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/antchfx/htmlquery"
	xhtml "golang.org/x/net/html"
)

// A channel section is the one page in this tool with strict cardinality
// assertions: exactly one comMain, one article list, one sidebar, one 编辑推荐
// block and one 最新文章 block.
//
// That strictness is deliberate. Every other extractor degrades gracefully
// because a missing card just means one fewer entry. Here the layout is the
// contract -- if the page grows a second list or loses the sidebar, the
// template changed underneath us, and silently returning half a directory would
// be worse than failing.

// SectionDirectory reads one channel section index.
func (c *Client) SectionDirectory(ctx context.Context, pageURL string) (map[string]any, error) {
	route := ClassifyURL(pageURL)
	if route.Adapter != "section-directory" {
		return nil, invalid("section-directory reads a single-level channel section on a Caixin " +
			"news host, for example https://finance.caixin.com/regulation/")
	}
	canonical := route.CanonicalURL

	// The section must be reachable from a front page that publishes it. That
	// is the discovery rule the route verdict advertises, and enforcing it here
	// stops a caller deep-linking into a section the site has dropped. A few
	// sections are published by two fronts, so each is tried in turn.
	var parentURL, discoveredTitle string
	var err error
	for _, parent := range sectionDirectoryParents(canonical) {
		discoveredTitle, err = c.sectionIsListedBy(ctx, parent, canonical)
		if err == nil {
			parentURL = parent
			break
		}
	}
	if parentURL == "" {
		if err == nil {
			err = &APIError{StatusCode: 404, Message: "that section has no front page to be discovered from"}
		}
		return nil, err
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
		return nil, &APIError{Message: "could not parse the section page"}
	}

	result, err := sectionDirectoryDocument(doc, canonical)
	if err != nil {
		return nil, err
	}
	result["title"] = emptyToNil(firstXPathText(doc, "//title"))
	result["directory_title"] = emptyToNil(discoveredTitle)
	result["source_mode"] = "server_html"
	result["session_used"] = false
	result["page"] = 1
	result["source"] = map[string]any{
		"requested_url":   pageURL,
		"canonical_url":   canonical,
		"final_url":       canonical,
		"discovered_from": parentURL,
		"fetched_at":      time.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	return attachClickConsumers(result), nil
}

func mustHost(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

// sectionIsListedBy fetches the channel front and returns the label it uses for
// this section, or an error when the front does not link to it at all.
func (c *Client) sectionIsListedBy(ctx context.Context, parentURL, canonical string) (string, error) {
	raw, err := c.do(ctx, requestSpec{
		Method:    http.MethodGet,
		URL:       parentURL,
		Headers:   map[string]string{"Accept": "text/html,application/xhtml+xml"},
		Anonymous: true,
	})
	if err != nil {
		return "", err
	}
	doc, err := xhtml.Parse(strings.NewReader(string(raw)))
	if err != nil {
		return "", &APIError{Message: "could not parse the channel front page"}
	}
	pageKey, measured := snapshotPageKey(parentURL)
	if !measured {
		return "", &APIError{Message: "the parent page " + parentURL + " is not a measured front page"}
	}
	// Read through the same extractor the snapshot uses, so "listed on the
	// front page" means the same thing in both commands.
	subdirectories, _ := snapshotSubdirectories(doc, parentURL, pageKey)
	for _, raw := range subdirectories {
		item, ok := raw.(map[string]any)
		if !ok || asString(item["url"]) != canonical {
			continue
		}
		if title := asString(item["title"]); title != "" {
			return title, nil
		}
	}
	return "", &APIError{
		StatusCode: 404,
		Message:    "that section is not linked from " + parentURL + " right now",
	}
}

// editorialPathPatterns recognize a link that leads to readable editorial
// content rather than to more navigation.
var editorialPathPatterns = []*regexp.Regexp{
	regexp.MustCompile(`/20\d{2}-\d{2}-\d{2}/\d+\.html$`),
	regexp.MustCompile(`/archives/\d+/?$`),
	regexp.MustCompile(`/20\d{2}/(cw|cr|cs_)\d+/(index\.html)?$`),
	regexp.MustCompile(`/topic/[^/]+\.html$`),
}

func looksLikeEditorialURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	for _, pattern := range editorialPathPatterns {
		if pattern.MatchString(parsed.Path) {
			return true
		}
	}
	return false
}

// mobileArticleAlias is the `/m/` spelling Caixin uses for the same article.
var mobileArticleAlias = regexp.MustCompile(`^/m(/20\d{2}-\d{2}-\d{2}/\d{1,20}\.html)$`)

// caixinOutputURL normalizes any Caixin link for emission.
//
// The `/m/` mobile alias is folded to the canonical path: it is the same
// article, and emitting both spellings would make one piece look like two in a
// listing and in any dedup a caller does downstream.
func caixinOutputURL(base, value string) string {
	raw := absoluteURL(base, value)
	if raw == "" || strings.ContainsAny(raw, " \t\n\r") {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return ""
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return ""
	}
	if !caixinHost(parsed.Hostname()) {
		return ""
	}
	// Emitted exactly as the page published it. The `/m/` mobile spelling is
	// folded where a listing must not show one article twice; here the url is
	// only being echoed, and rewriting it would misreport what the page said.
	return raw
}

// sectionDirectoryDocument extracts the three blocks a section page publishes.
func sectionDirectoryDocument(doc *xhtml.Node, pageURL string) (map[string]any, error) {
	roots := htmlquery.Find(doc, classXPath("comMain"))
	if len(roots) != 1 {
		return nil, &APIError{Message: "the section page does not have exactly one comMain"}
	}
	lists := htmlquery.Find(roots[0],
		".//div["+classPredicate("conlf")+"]/*["+classPredicate("stitXtuwen_list")+"]")
	if len(lists) != 1 {
		return nil, &APIError{Message: "the section page does not have exactly one article list"}
	}

	var articles []map[string]any
	for _, node := range htmlquery.Find(lists[0], "./dl") {
		item, err := sectionCardItem(node, pageURL)
		if err != nil {
			return nil, err
		}
		articles = append(articles, item)
	}
	modules := []snapshotModule{{
		Key: "section-directory.articles", Name: "栏目文章",
		Lane: "main", Order: 0, State: "server_rendered", Items: articles,
	}}

	sidebars := htmlquery.Find(roots[0], ".//div["+classPredicate("conri")+"]")
	if len(sidebars) != 1 {
		return nil, &APIError{Message: "the section page does not have exactly one sidebar"}
	}
	containers := htmlquery.Find(sidebars[0], "./div["+classPredicate("columnBox")+"]")
	for order, block := range []struct{ label, key string }{
		{"编辑推荐", "recommended"}, {"最新文章", "latest"},
	} {
		var matched []*xhtml.Node
		for _, container := range containers {
			if strings.Contains(firstXPathText(container, "./h3"), block.label) {
				matched = append(matched, container)
			}
		}
		if len(matched) != 1 {
			return nil, &APIError{
				Message: "the section page does not have exactly one " + block.label + " block",
			}
		}
		items := sidebarItems(matched[0], pageURL)
		if len(items) == 0 {
			return nil, &APIError{
				Message: "the " + block.label + " block carried no usable article",
			}
		}
		modules = append(modules, snapshotModule{
			Key: "section-directory." + block.key, Name: block.label,
			Lane: "sidebar", Order: order, State: "server_rendered", Items: items,
		})
	}

	result := directoryResult(modules)
	result["kind"] = "section_directory"
	// The continuation endpoint is reported, never called: following it would
	// turn one directory read into a crawl the caller did not ask for.
	contract := htmlquery.FindOne(doc, "//script[contains(text(),'subject')]") != nil
	result["continuation_available"] = contract
	result["continuation_not_followed"] = contract
	result["scripts_not_executed"] = true
	result["linked_pages_not_fetched"] = true
	return result, nil
}

// sectionCardItem builds one main-list card, refusing anything malformed.
func sectionCardItem(node *xhtml.Node, pageURL string) (map[string]any, error) {
	anchors := htmlquery.Find(node, "./dd/h4/a[1][@href]")
	if len(anchors) != 1 {
		return nil, &APIError{Message: "a section card has no single article link"}
	}
	anchor := anchors[0]
	link := articleURL(pageURL, attr(anchor, "href"))
	if link == "" {
		return nil, &APIError{Message: "a section card links to a url this build will not consume"}
	}
	title := nodeText(anchor)
	if title == "" {
		return nil, &APIError{Message: "a section card has no title"}
	}

	image := ""
	if imageNode := firstElement(node, ".//*["+classPredicate("pic")+"]//img[1]"); imageNode != nil {
		source := attr(imageNode, "data-src")
		if source == "" {
			source = attr(imageNode, "src")
		}
		image = cultureImageURL(pageURL, source)
	}
	summary := firstXPathText(node, "./dd/p[1]")
	if summary == title {
		summary = ""
	}

	badges := []any{}
	for _, badgeNode := range htmlquery.Find(node, "./dd/h4//*[contains(@class,'icon_')]") {
		badge := plainText(attr(badgeNode, "title"))
		if badge == "" {
			classes := strings.Fields(attr(badgeNode, "class"))
			for _, class := range classes {
				switch class {
				case "icon_key":
					badge = "收费文章"
				case "icon_free":
					badge = "免费文章"
				}
			}
		}
		if badge == "" {
			continue
		}
		duplicate := false
		for _, existing := range badges {
			if existing == badge {
				duplicate = true
			}
		}
		if !duplicate {
			badges = append(badges, badge)
		}
	}

	return map[string]any{
		"title":        title,
		"url":          link,
		"image":        emptyToNil(image),
		"summary":      emptyToNil(summary),
		"published_at": emptyToNil(firstXPathText(node, "./dd/span[1]")),
		"badges":       badges,
	}, nil
}

// sidebarItems extracts one sidebar block, keeping only editorial destinations.
func sidebarItems(container *xhtml.Node, pageURL string) []map[string]any {
	var items []map[string]any
	seen := map[string]bool{}

	for _, anchor := range htmlquery.Find(container, ".//a[@href]") {
		// The block heading is a link too; it is navigation, not an entry.
		if htmlquery.FindOne(anchor, "ancestor::h3") != nil {
			continue
		}
		link := cultureArticleURL(pageURL, attr(anchor, "href"))
		if link == "" {
			link = caixinOutputURL(pageURL, attr(anchor, "href"))
		}
		if link == "" || !looksLikeEditorialURL(link) || seen[link] {
			continue
		}
		title := nodeText(anchor)
		if title == "" {
			if image := firstElement(anchor, ".//img"); image != nil {
				title = strings.TrimSpace(attr(image, "alt"))
				if title == "" {
					title = strings.TrimSpace(attr(image, "title"))
				}
			}
		}
		if title == "" {
			continue
		}
		seen[link] = true

		itemRoot := firstElement(anchor, "ancestor::dl[1]", "ancestor::li[1]", "ancestor::p[1]")
		if itemRoot == nil {
			itemRoot = anchor.Parent
		}
		image := ""
		if imageNode := firstElement(itemRoot, ".//img"); imageNode != nil {
			source := attr(imageNode, "data-src")
			if source == "" {
				source = attr(imageNode, "src")
			}
			image = caixinOutputURL(pageURL, source)
		}
		summary := ""
		for _, xpath := range []string{".//dd", ".//p", ".//span"} {
			value := firstXPathText(itemRoot, xpath)
			// A "summary" that merely repeats the headline is not a summary.
			if value != "" && value != title && !strings.Contains(value, title) {
				summary = value
				break
			}
		}
		badges := []any{}
		for _, candidate := range htmlquery.Find(itemRoot, ".//a[@href]") {
			if candidate == anchor {
				break
			}
			if badge := nodeText(candidate); badge != "" && badge != title && !containsAny(badges, badge) {
				badges = append(badges, badge)
			}
		}
		for _, icon := range htmlquery.Find(itemRoot, ".//*[contains(@class,'icon_')]") {
			if badge := attr(icon, "title"); badge != "" && !containsAny(badges, badge) {
				badges = append(badges, badge)
			}
		}

		item := map[string]any{
			"title":        title,
			"url":          link,
			"image":        emptyToNil(image),
			"summary":      emptyToNil(summary),
			"badges":       badges,
			"source_state": serverSourceState(anchor),
		}
		// When the card's picture points somewhere other than its headline, both
		// destinations are reported and the disagreement is flagged. Picking one
		// silently would send a caller to a page the card did not promise.
		if imageAnchor := firstElement(itemRoot, ".//a[@href][.//img][1]"); imageAnchor != nil {
			imageRoute := ClassifyURL(absoluteURL(pageURL, attr(imageAnchor, "href")))
			if imageRoute.Supported && imageRoute.CanonicalURL != link {
				item["image_click_url"] = imageRoute.CanonicalURL
				item["link_mismatch"] = true
			}
		}
		items = append(items, item)
	}
	return items
}
