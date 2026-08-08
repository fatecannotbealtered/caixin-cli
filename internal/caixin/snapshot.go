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

// snapshotPageKey resolves a url to the page template it is known to use.
//
// The allowlist is exact rather than pattern-based on purpose: each key selects
// a different extraction path, and pointing one of them at an unmeasured page
// would not fail -- it would quietly produce a plausible, wrong listing.
func snapshotPageKey(raw string) (string, bool) {
	key, ok := SnapshotEntrypoints[raw]
	return key, ok
}

// Snapshot reads one Caixin channel front page.
func (c *Client) Snapshot(ctx context.Context, pageURL string) (map[string]any, error) {
	pageKey, ok := snapshotPageKey(pageURL)
	if !ok {
		return nil, invalid("snapshot only accepts the exact Caixin entry points this build has measured; " +
			"run `caixin-cli reference` for the list")
	}

	headers := map[string]string{"Accept": "text/html,application/xhtml+xml"}
	// The blog front refuses the default agent.
	if pageKey == "blog-index" {
		headers["User-Agent"] = TopicsCompatUserAgent
	}
	raw, err := c.do(ctx, requestSpec{
		Method: http.MethodGet, URL: pageURL,
		Headers: headers,
		// A channel front carries no personal content, so it is read
		// anonymously: sending an account session to a page that does not need
		// it exposes a credential for nothing (SEC-SPEC §4).
		Anonymous: true,
	})
	if err != nil {
		return nil, err
	}
	doc, err := xhtml.Parse(strings.NewReader(string(raw)))
	if err != nil {
		return nil, &APIError{Message: "could not parse the channel page"}
	}

	result, err := extractSnapshot(doc, pageURL, pageKey)
	if err != nil {
		return nil, err
	}
	result["source"] = map[string]any{
		"requested_url": pageURL,
		"final_url":     pageURL,
		"fetched_at":    time.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	result["source_mode"] = "server_html"
	result["session_used"] = false
	// Stated on every snapshot, because the difference between "this is the
	// page" and "this is what the server sent before scripts ran" is the thing
	// a caller is most likely to get wrong.
	result["javascript_executed"] = false
	result["rendered_visibility_verified"] = false
	result["external_stylesheets_applied"] = false
	result["complete_listing_verified"] = false
	return result, nil
}

// extractSnapshot dispatches to the extractor for one page template.
func extractSnapshot(doc *xhtml.Node, pageURL, pageKey string) (map[string]any, error) {
	var modules []snapshotModule
	var err error

	switch {
	case publicationRoots[pageKey]:
		// A magazine front returns its own shape (current issue, features, year
		// columns) rather than the generic module list.
		result, err := publicationRootSnapshot(doc, pageURL, pageKey)
		if err != nil {
			return nil, err
		}
		result["page_kind"] = pageKey
		result["title"] = emptyToNil(firstXPathText(doc, "//title"))
		if subdirectories, applicable := snapshotSubdirectories(doc, pageURL, pageKey); applicable {
			result["subdirectories"] = subdirectories
			result["subdirectories_count"] = len(subdirectories)
		}
		return finalizeSnapshot(doc, pageURL, pageKey, result), nil

	case pageKey == "home":
		modules, err = homeModules(doc, pageURL)
	case pageKey == "wenews-index" || pageKey == "en-index" || pageKey == "blog-index" ||
		pageKey == "newsletter":
		// These four return their own shape rather than a plain module list.
		var result map[string]any
		var err error
		switch pageKey {
		case "wenews-index":
			result, err = wenewsSnapshot(doc, pageURL)
		case "en-index":
			result, err = englishSnapshot(doc, pageURL)
		case "blog-index":
			result, err = blogSnapshot(doc, pageURL)
		default:
			result, err = newsletterModules(doc, pageURL)
		}
		if err != nil {
			return nil, err
		}
		result["page_kind"] = pageKey
		result["title"] = emptyToNil(firstXPathText(doc, "//title"))
		return finalizeSnapshot(doc, pageURL, pageKey, result), nil

	case pageKey == "photos-index" || pageKey == "video-index":
		// The two media fronts return the directory shape plus the note that
		// nothing was downloaded.
		var result map[string]any
		var err error
		if pageKey == "photos-index" {
			result, err = photosSnapshot(doc, pageURL)
		} else {
			result, err = videoSnapshot(doc, pageURL)
		}
		if err != nil {
			return nil, err
		}
		result["page_kind"] = pageKey
		result["title"] = emptyToNil(firstXPathText(doc, "//title"))
		return finalizeSnapshot(doc, pageURL, pageKey, result), nil

	case pageKey == "culture":
		// The 文化 front returns its own shape: six tabbed lists plus the
		// columnist roster, which is people rather than articles.
		result, err := cultureSnapshot(doc, pageURL)
		if err != nil {
			return nil, err
		}
		result["page_kind"] = pageKey
		result["title"] = emptyToNil(firstXPathText(doc, "//title"))
		return finalizeSnapshot(doc, pageURL, pageKey, result), nil

	case pageKey == "opinion":
		modules, err = opinionModules(doc, pageURL)
	case pageKey == "datanews":
		modules, err = datanewsModules(doc, pageURL)
	case pageKey == "mini":
		modules, err = miniHomeModules(doc, pageURL)
	case pageKey == "esg":
		modules, err = esgModules(doc, pageURL)
	case pageKey == "topics":
		modules, err = topicsDirectoryModules(doc, pageURL)
	case categorySnapshotKeys[pageKey]:
		modules, err = categoryModules(doc, pageURL, pageKey)
	default:
		// Every remaining entry point uses the news channel layout. The list is
		// an allowlist already, so this is not a guess: an unmeasured url was
		// refused before it got here.
		modules, err = channelModules(doc, pageURL, pageKey)
	}
	if err != nil {
		return nil, err
	}

	result := map[string]any{
		"page_kind":     pageKey,
		"title":         emptyToNil(firstXPathText(doc, "//title")),
		"modules":       modulesAsList(modules),
		"modules_count": len(modules),
		"items_count":   countItems(modules),
	}
	if subdirectories, applicable := snapshotSubdirectories(doc, pageURL, pageKey); applicable {
		result["subdirectories"] = subdirectories
		result["subdirectories_count"] = len(subdirectories)
	}
	return finalizeSnapshot(doc, pageURL, pageKey, result), nil
}

// datanewsModules extracts the 数字说 front page: a focus carousel, the latest
// list, the interactive showcase, and a ranking.
func datanewsModules(doc *xhtml.Node, pageURL string) ([]snapshotModule, error) {
	root := firstElement(doc, classXPath("comMain"))
	if root == nil {
		return nil, &APIError{Message: "the 数字说 front page is missing its comMain container"}
	}
	var modules []snapshotModule

	focus := collectItems(root, pageURL,
		".//*["+classPredicate("ggbBox")+"]/div["+classPredicate("ggbCon")+"]",
		itemOptions{
			AnchorXPaths: []string{".//div[" + classPredicate("callBoard") + "]/h4/a[@href]"},
			ImageXPaths:  []string{".//div[" + classPredicate("callBoard") + "]/a/img"},
		})
	if len(focus) > 0 {
		active := 0
		modules = append(modules, snapshotModule{
			Key: "datanews.focus", Name: "焦点", Lane: "lead", Order: 0,
			State: "visible", ActiveItemIndex: &active, Items: focus,
		})
	}

	latest := collectItems(root, pageURL,
		".//*[@id='homeArticleList' and "+classPredicate("szxwList")+"]/dl",
		itemOptions{
			AnchorXPaths:  []string{".//dd/h3/a[1][@href]"},
			ImageXPaths:   []string{".//dt/a/img"},
			SummaryXPaths: []string{".//dd/p"},
			BadgeXPaths: []string{".//dd/h3//*[" + classPredicate("icon_key") +
				" or " + classPredicate("icon_free") + "]"},
		})
	if len(latest) > 0 {
		modules = append(modules, snapshotModule{
			Key: "datanews.latest", Name: "最新作品", Lane: "main", Order: 0,
			State: "visible", Items: latest,
		})
	}

	if showcase := firstElement(root, ".//*["+classPredicate("kshzp")+"]"); showcase != nil {
		var interactives []map[string]any
		for _, node := range htmlquery.Find(showcase, ".//*["+classPredicate("szztCon")+"]/li") {
			if item := datanewsInteractiveItem(node, pageURL); item != nil {
				interactives = append(interactives, item)
			}
		}
		if len(interactives) > 0 {
			module := snapshotModule{
				Key: "datanews.interactives", Name: "数据新闻与可视化作品",
				Lane: "sidebar", Order: 0, State: "visible", Items: interactives,
			}
			if more := firstElement(showcase, "./h2//a[@href]"); more != nil {
				module.MoreURL = datanewsDirectoryURL(pageURL, attr(more, "href"))
			}
			modules = append(modules, module)
		}
	}

	if ranking := firstElement(root, ".//*["+classPredicate("szxwphb")+"]"); ranking != nil {
		items := collectItems(ranking, pageURL,
			".//*["+classPredicate("phbList")+"]/li",
			itemOptions{
				AnchorXPaths: []string{".//p/a[@href]"},
				ImageXPaths:  []string{},
			})
		if len(items) > 0 {
			modules = append(modules, snapshotModule{
				Key: "datanews.most-viewed", Name: "数字说排行榜",
				Lane: "sidebar", Order: 1, State: "visible", Items: items,
			})
		}
	}
	return modules, nil
}

// collectItems runs one card selector and extracts every card that yields a
// usable entry.
func collectItems(root *xhtml.Node, pageURL, selector string, options itemOptions) []map[string]any {
	var items []map[string]any
	for _, node := range htmlquery.Find(root, selector) {
		if item := extractItem(node, pageURL, options); item != nil {
			items = append(items, item)
		}
	}
	return items
}

// interactiveSegment bounds the path segments an interactive url may carry.
var interactiveSegment = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
var interactiveYear = regexp.MustCompile(`^20\d{2}$`)

// datanewsInteractiveURL accepts only the interactive-visualisation path shape.
func datanewsInteractiveURL(base, value string) string {
	raw := absoluteURL(base, value)
	if raw == "" || strings.ContainsAny(raw, " \t\n\r") {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil {
		return ""
	}
	if strings.ToLower(parsed.Hostname()) != "datanews.caixin.com" {
		return ""
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	var segments []string
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	if len(segments) < 3 || segments[0] != "interactive" || !interactiveYear.MatchString(segments[1]) {
		return ""
	}
	for _, segment := range segments[2:] {
		if segment == "." || segment == ".." || !interactiveSegment.MatchString(segment) {
			return ""
		}
	}
	// Emitted as https so a caller is never handed a downgrade to follow.
	parsed.Scheme = "https"
	parsed.Host = strings.ToLower(parsed.Hostname())
	return parsed.String()
}

// datanewsDirectoryURL accepts the one "see all" destination this block links
// to. The page emits it over http; it is normalized rather than passed through.
func datanewsDirectoryURL(base, value string) string {
	raw := absoluteURL(base, value)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	if strings.ToLower(parsed.Hostname()) != "datanews.caixin.com" {
		return ""
	}
	if parsed.Path != "/datatopic/" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	parsed.Scheme = "https"
	parsed.Host = "datanews.caixin.com"
	return parsed.String()
}

// datanewsImageURL also accepts a cover served from the visualisation's own
// directory, which is where these are published rather than on the image host.
func datanewsImageURL(base, value string) string {
	if image := imageURL(base, value); image != "" {
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
	if strings.ToLower(parsed.Hostname()) != "datanews.caixin.com" {
		return ""
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	var segments []string
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	if len(segments) < 4 || segments[0] != "interactive" || !interactiveYear.MatchString(segments[1]) {
		return ""
	}
	for _, segment := range segments[2:] {
		if segment == "." || segment == ".." || !interactiveSegment.MatchString(segment) {
			return ""
		}
	}
	return raw
}

// datanewsInteractiveItem builds one visualisation entry. These are standalone
// pages rather than articles, so they carry `kind` and are marked as not
// fetched -- the listing proves they exist, nothing more.
func datanewsInteractiveItem(node *xhtml.Node, pageURL string) map[string]any {
	anchor := firstElement(node, "./a[@href]")
	if anchor == nil {
		return nil
	}
	link := datanewsInteractiveURL(pageURL, attr(anchor, "href"))
	title := nodeText(anchor)
	if link == "" || title == "" {
		return nil
	}
	image := ""
	if imageNode := firstElement(anchor, ".//img"); imageNode != nil {
		source := attr(imageNode, "data-src")
		if source == "" {
			source = attr(imageNode, "src")
		}
		image = datanewsImageURL(pageURL, source)
	}
	return map[string]any{
		"title":               title,
		"url":                 link,
		"image":               emptyToNil(image),
		"summary":             nil,
		"badges":              []any{},
		"kind":                "interactive",
		"access":              "directory_visible",
		"content_not_fetched": true,
	}
}
