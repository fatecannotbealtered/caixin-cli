package caixin

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/antchfx/htmlquery"
	xhtml "golang.org/x/net/html"
)

// The public directories are the standing index pages Caixin publishes outside
// the news channels: the data-topic list, the photo columns, the ESG30 index,
// the sponsored-content front. They share an envelope but not a layout, so each
// kind gets its own extractor and the shared parts live in directory.go.

// PublicDirectory reads one public directory page.
func (c *Client) PublicDirectory(ctx context.Context, pageURL string) (map[string]any, error) {
	canonical := publicDirectoryURL(pageURL)
	if canonical == "" {
		return nil, invalid("public-directory only accepts the exact entry points this build " +
			"has measured; run `caixin-cli reference` for the list")
	}
	kind := PublicDirectoryEntrypoints[canonical]

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
		return nil, &APIError{Message: "could not parse the directory page"}
	}

	var result map[string]any
	switch kind {
	case "datanews_topics":
		result, err = datanewsTopicsDirectory(doc, canonical)
	case "photo_week", "photo_sight":
		result, err = photoDirectory(doc, canonical, kind)
	case "esg30":
		result, err = esg30Directory(doc, canonical)
	case "promote_home", "promote_topics":
		result, err = promoteDirectory(doc, canonical, kind)
	default:
		return nil, &APIError{
			Message: "this build does not yet extract the " + kind + " directory",
		}
	}
	if err != nil {
		return nil, err
	}

	result["kind"] = kind
	result["title"] = emptyToNil(firstXPathText(doc, "//title"))
	result["source_mode"] = "server_html"
	result["session_used"] = false
	result["page"] = 1
	result["source"] = map[string]any{
		"requested_url": pageURL,
		"canonical_url": canonical,
		"final_url":     canonical,
		"fetched_at":    time.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	return attachPaginationConsumers(attachClickConsumers(result)), nil
}

// topicSegment bounds one path segment of a topic destination.
var topicSegment = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// datanewsTopicInteractiveURL accepts a visualisation destination.
//
// The directory links to three shapes that all live on the data host:
// `/interactive/<year>/...`, `/mobile/...`, and a bare `/<year>/...`. Accepting
// only the first would silently drop more than half the listing -- which is
// exactly what happened before this was ported in full.
func datanewsTopicInteractiveURL(base, value string) string {
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
	if parsed.RawQuery != "" || strings.Contains(parsed.Path, "//") {
		return ""
	}

	host := strings.ToLower(parsed.Hostname())
	// Two entries in this directory predate the data host and still live where
	// they were first published. They are allowlisted by exact path rather than
	// by loosening the host rule, which would let anything on those hosts in.
	for _, legacy := range []struct{ host, path string }{
		{"file.caixin.com", "/datanews_mobile/notobacco/"},
		{"china.caixin.com", "/2015/hstjl/index.html"},
	} {
		if host == legacy.host && parsed.Path == legacy.path && parsed.Fragment == "" {
			parsed.Scheme = "https"
			parsed.Host = host
			return parsed.String()
		}
	}
	if host != "datanews.caixin.com" {
		return ""
	}

	var segments []string
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	if len(segments) < 2 || len(segments) > 10 {
		return ""
	}

	var content []string
	switch {
	case segments[0] == "interactive" && len(segments) >= 3 && interactiveYear.MatchString(segments[1]):
		content = segments[2:]
	case segments[0] == "mobile":
		content = segments[1:]
	case interactiveYear.MatchString(segments[0]):
		content = segments[1:]
	default:
		return ""
	}
	if len(content) == 0 {
		return ""
	}
	for _, segment := range content[:len(content)-1] {
		if !topicSegment.MatchString(segment) {
			return ""
		}
	}
	last := content[len(content)-1]
	if !topicSegment.MatchString(last) && last != "index.html" {
		return ""
	}
	// One documented exception carries a fragment; everything else must not.
	if parsed.Fragment != "" &&
		(parsed.Path != "/mobile/AntiCorruption/pc/" || parsed.Fragment != "/") {
		return ""
	}

	parsed.Scheme = "https"
	parsed.Host = host
	return parsed.String()
}

// datanewsTopicsDirectory extracts the 数字专题 index.
//
// Entries are a mix of articles and standalone visualisations, so each carries
// `item_kind`: an agent picking a consumer needs to know which it is looking at.
func datanewsTopicsDirectory(doc *xhtml.Node, pageURL string) (map[string]any, error) {
	root := firstElement(doc,
		"//div["+classPredicate("hotWord02")+"]//ul["+classPredicate("szztCon02")+"][1]")
	if root == nil {
		return nil, &APIError{Message: "the data-topic directory is missing its szztCon02 list"}
	}

	var items []map[string]any
	seen := map[string]bool{}
	for _, anchor := range htmlquery.Find(root, ".//li/a[@href]") {
		title := firstXPathText(anchor, ".//span[1]")
		if title == "" {
			title = nodeText(anchor)
		}
		if title == "" {
			continue
		}
		href := strings.TrimSpace(attr(anchor, "href"))
		link := articleURL(pageURL, href)
		itemKind := "article"
		if link == "" {
			link = datanewsTopicInteractiveURL(pageURL, href)
			itemKind = "interactive_directory"
		}
		if link == "" || seen[link] {
			continue
		}
		seen[link] = true
		items = append(items, map[string]any{
			"title":                            title,
			"url":                              link,
			"image":                            nil,
			"summary":                          nil,
			"badges":                           []any{},
			"item_kind":                        itemKind,
			"rendered_visibility_not_verified": true,
		})
	}

	result := directoryResult([]snapshotModule{{
		Key: "public-directory.datanews-topics", Name: "数字专题",
		Lane: "main", Order: 0, State: "server_rendered", Items: items,
	}})
	// These entries come from server markup this client never rendered, so the
	// access label is a candidate rather than a confirmed visible entry.
	for _, item := range items {
		item["access"] = "ssr_directory_candidate"
	}
	result["interactive_projects_not_fetched"] = true
	result["rendered_visibility_not_verified"] = true
	result["scripts_not_executed"] = true
	return result, nil
}

// photoWeekPagePattern bounds the static pagination the photo column publishes.
var photoWeekPagePattern = regexp.MustCompile(`^/photoreport/index-[1-9]\d{0,3}\.html$`)

// photoDirectory extracts the 图片 columns.
func photoDirectory(doc *xhtml.Node, pageURL, kind string) (map[string]any, error) {
	root := firstElement(doc,
		"//div["+classPredicate("comMain")+"]//div["+classPredicate("conlf")+"]//div["+
			classPredicate("picListBox")+"][1]")
	if root == nil {
		return nil, &APIError{Message: "the photo directory is missing its picListBox"}
	}

	allowedHosts := map[string]bool{"photos.caixin.com": true}
	if kind == "photo_week" {
		// 一周天下 republishes some entries under other.caixin.com.
		allowedHosts["other.caixin.com"] = true
	}

	var items []map[string]any
	for _, node := range htmlquery.Find(root, ".//div["+classPredicate("picList")+"][1]/dl") {
		anchor := firstElement(node, ".//dd/p/a[@href][1]")
		if anchor == nil {
			continue
		}
		title := nodeText(anchor)
		if title == "" {
			continue
		}
		link := articleURL(pageURL, attr(anchor, "href"))
		if link == "" {
			continue
		}
		parsed, err := url.Parse(link)
		if err != nil || !allowedHosts[strings.ToLower(parsed.Hostname())] {
			continue
		}
		// The image and the headline must point at the same article; a card
		// whose picture links elsewhere is an ad slot dressed as an entry.
		if imageAnchor := firstElement(node, ".//dt/a[@href][not(ancestor::i)][1]"); imageAnchor != nil {
			if articleURL(pageURL, attr(imageAnchor, "href")) != link {
				continue
			}
		}
		image := ""
		if imageNode := firstElement(node, ".//dt//img[1]"); imageNode != nil {
			source := attr(imageNode, "data-src")
			if source == "" {
				source = attr(imageNode, "src")
			}
			image = cultureImageURL(pageURL, source)
		}
		items = append(items, map[string]any{
			"title":                            title,
			"url":                              link,
			"image":                            emptyToNil(image),
			"summary":                          nil,
			"badges":                           []any{},
			"published_at":                     emptyToNil(firstXPathText(node, ".//dd/span[1]")),
			"item_kind":                        "article",
			"rendered_visibility_not_verified": true,
		})
	}

	name := "视线"
	if kind == "photo_week" {
		name = "一周天下"
	}
	result := directoryResult([]snapshotModule{{
		Key: "public-directory." + kind, Name: name,
		Lane: "main", Order: 0, State: "server_rendered", Items: items,
	}})
	for _, item := range items {
		item["access"] = "ssr_directory_candidate"
	}

	// Pagination is reported but never followed: a directory command lists one
	// page, and walking the whole column would be a crawl.
	links := []any{}
	if kind == "photo_week" {
		seen := map[string]bool{}
		for _, anchor := range htmlquery.Find(doc, "//div["+classPredicate("pageNav")+"]//a[@href]") {
			candidate := absoluteURL(pageURL, attr(anchor, "href"))
			if candidate == "" {
				continue
			}
			candidate = upgradeCaixinScheme(candidate)
			parsed, err := url.Parse(candidate)
			if err != nil || strings.ToLower(parsed.Hostname()) != "photos.caixin.com" {
				continue
			}
			if !photoWeekPagePattern.MatchString(parsed.Path) ||
				parsed.RawQuery != "" || parsed.Fragment != "" {
				continue
			}
			if candidate == pageURL || seen[candidate] {
				continue
			}
			label := nodeText(anchor)
			if label == "" {
				continue
			}
			seen[candidate] = true
			links = append(links, map[string]any{"label": label, "url": candidate})
		}
	}
	result["pagination"] = map[string]any{
		"links":        links,
		"not_followed": len(links) > 0,
	}
	result["rendered_visibility_not_verified"] = true
	result["scripts_not_executed"] = true
	return result, nil
}

// esg30SubdirectoryPath bounds the sub-index pages the ESG30 front links to.
var esg30SubdirectoryPath = regexp.MustCompile(`^/(news|esg30focus|esg30event|esg30report)/$`)

// directoryPDFItem builds a download candidate from an anchor.
//
// It is metadata only: the file is never fetched, and `content_not_fetched`
// says so. A directory command that quietly downloaded PDFs would be doing
// something a caller never asked for.
func directoryPDFItem(anchor *xhtml.Node, pageURL string) map[string]any {
	raw := absoluteURL(pageURL, attr(anchor, "href"))
	if raw == "" {
		return nil
	}
	raw = upgradeCaixinScheme(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return nil
	}
	if !caixinHost(parsed.Hostname()) || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil
	}
	if !strings.HasSuffix(strings.ToLower(parsed.Path), ".pdf") {
		return nil
	}
	return map[string]any{
		"title":                            emptyToNil(nodeText(anchor)),
		"url":                              parsed.String(),
		"expected_mime_type":               "application/pdf",
		"access":                           "ssr_download_candidate",
		"content_not_fetched":              true,
		"rendered_visibility_not_verified": true,
	}
}

// esg30Directory extracts the ESG30 index: articles, sub-indexes, and the
// report PDFs it publishes.
func esg30Directory(doc *xhtml.Node, pageURL string) (map[string]any, error) {
	root := firstElement(doc, "//section["+classPredicate("main")+"][1]")
	if root == nil {
		return nil, &APIError{Message: "the ESG30 directory is missing its main section"}
	}

	articles := map[string]map[string]any{}
	var articleOrder []string
	downloads := map[string]map[string]any{}
	var downloadOrder []string
	var subdirectories []any
	seenSub := map[string]bool{}

	for _, anchor := range htmlquery.Find(root, ".//a[@href]") {
		href := strings.TrimSpace(attr(anchor, "href"))
		if link := articleURL(pageURL, href); link != "" {
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
			itemRoot := anchor
			if ancestor := firstElement(anchor, "ancestor::li[1]"); ancestor != nil {
				itemRoot = ancestor
			}
			image := ""
			if imageNode := firstElement(itemRoot, ".//img[1]"); imageNode != nil {
				source := attr(imageNode, "data-src")
				if source == "" {
					source = attr(imageNode, "src")
				}
				image = cultureImageURL(pageURL, source)
			}
			badges := []any{}
			for _, tag := range htmlquery.Find(itemRoot, ".//*["+classPredicate("news-tag")+"]/span") {
				if text := nodeText(tag); text != "" {
					badges = append(badges, text)
				}
			}
			item := map[string]any{
				"title":                            title,
				"url":                              link,
				"image":                            emptyToNil(image),
				"summary":                          nil,
				"published_at":                     emptyToNil(firstXPathText(itemRoot, ".//*["+classPredicate("news-date")+"][1]")),
				"badges":                           badges,
				"item_kind":                        "article",
				"rendered_visibility_not_verified": true,
			}
			// The same article can appear as both a headline and a thumbnail;
			// keep whichever anchor carried the longer title.
			if current, ok := articles[link]; ok {
				if len(title) > len(asString(current["title"])) {
					articles[link] = item
				}
				continue
			}
			articles[link] = item
			articleOrder = append(articleOrder, link)
			continue
		}

		if pdf := directoryPDFItem(anchor, href); pdf != nil {
			link := asString(pdf["url"])
			if _, seen := downloads[link]; !seen {
				downloads[link] = pdf
				downloadOrder = append(downloadOrder, link)
			}
			continue
		}

		candidate := absoluteURL(pageURL, href)
		if candidate == "" {
			continue
		}
		candidate = upgradeCaixinScheme(candidate)
		parsed, err := url.Parse(candidate)
		if err != nil || strings.ToLower(parsed.Hostname()) != "index.caixin.com" {
			continue
		}
		if !esg30SubdirectoryPath.MatchString(parsed.Path) ||
			parsed.RawQuery != "" || parsed.Fragment != "" {
			continue
		}
		if seenSub[candidate] {
			continue
		}
		title := nodeText(anchor)
		if title == "" {
			continue
		}
		seenSub[candidate] = true
		subdirectories = append(subdirectories, map[string]any{
			"title":                            title,
			"url":                              candidate,
			"content_not_fetched":              true,
			"rendered_visibility_not_verified": true,
		})
	}

	items := make([]map[string]any, 0, len(articleOrder))
	for _, link := range articleOrder {
		items = append(items, articles[link])
	}
	result := directoryResult([]snapshotModule{{
		Key: "public-directory.esg30-articles", Name: "ESG30 资讯",
		Lane: "main", Order: 0, State: "server_rendered", Items: items,
	}})
	for _, item := range items {
		item["access"] = "ssr_directory_candidate"
	}
	downloadList := []any{}
	for _, link := range downloadOrder {
		downloadList = append(downloadList, downloads[link])
	}
	if subdirectories == nil {
		subdirectories = []any{}
	}
	result["subdirectories"] = subdirectories
	result["downloads"] = downloadList
	result["downloads_count"] = len(downloadList)
	result["rendered_visibility_not_verified"] = true
	result["scripts_not_executed"] = true
	return result, nil
}

// promoteImagePathPattern bounds a sponsored-content image path.
var promoteImagePathPattern = regexp.MustCompile(`(?i)^/{1,2}[A-Za-z0-9_/%+.,@~-]+\.(gif|jpe?g|png|webp)$`)

// promoteImageURL accepts the two hosts the sponsored front serves art from.
func promoteImageURL(base, value string) string {
	raw := absoluteURL(base, value)
	if raw == "" || strings.ContainsAny(raw, " \t\n\r") {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "img.caixin.com" && host != "promote.caixin.com" {
		return ""
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment == "." || segment == ".." {
			return ""
		}
	}
	if !promoteImagePathPattern.MatchString(parsed.Path) {
		return ""
	}
	parsed.Scheme = "https"
	parsed.Host = host
	return parsed.String()
}

// promoteBoundaries are the verdicts whose targets are still worth listing.
//
// A sponsored front links out to campaign sites and product pages. Those are
// not readable by any adapter here, but dropping them would misrepresent the
// page -- so they are listed with the boundary that says why, and the caller
// decides.
var promoteBoundaries = map[string]bool{
	"external": true, "independent_product": true, "media_asset": true,
	"transaction_or_product_detail": true,
}

// promoteDirectory extracts 特别呈现, Caixin's sponsored-content front.
//
// Everything here is commercial: `sponsored` and `commercial_editorial` are set
// on the result so an agent summarising it cannot present the contents as
// newsroom output.
func promoteDirectory(doc *xhtml.Node, pageURL, kind string) (map[string]any, error) {
	var selectors []string
	if kind == "promote_home" {
		selectors = []string{
			"//div[" + classPredicate("bannerlist_v2") + "]",
			"//div[" + classPredicate("tuijian") + "]",
			"//div[" + classPredicate("anli_box") + "]",
			"//div[@id='shuzi' or " + classPredicate("shuzi") + "]",
			"//div[@id='zixunyanjiu' or " + classPredicate("zixunyanjiu") + "]",
		}
	} else {
		selectors = []string{"//div[" + classPredicate("anli_box") + "]"}
	}
	var roots []*xhtml.Node
	for _, selector := range selectors {
		roots = append(roots, htmlquery.Find(doc, selector)...)
	}
	if len(roots) == 0 {
		return nil, &APIError{Message: "the sponsored directory is missing its content roots"}
	}

	items := map[string]map[string]any{}
	var order []string
	downloads := map[string]map[string]any{}
	var downloadOrder []string

	for _, root := range roots {
		// Document order, explicitly: the xpath engine returns shallower matches
		// before deeper ones, and this block's "see more" link is a direct child
		// of the section while the cards are nested. Left alone, that one link
		// would be reported ahead of the cards it follows on the page.
		for _, anchor := range findInDocumentOrder(root, ".//a[@href]") {
			href := strings.TrimSpace(attr(anchor, "href"))
			if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "javascript:") {
				continue
			}
			absolute := absoluteURL(pageURL, href)
			if absolute == "" {
				continue
			}
			route := ClassifyURL(absolute)

			var outputURL, itemKind string
			switch {
			case route.Boundary == "download_asset":
				if pdf := directoryPDFItem(anchor, href); pdf != nil {
					link := asString(pdf["url"])
					if _, seen := downloads[link]; !seen {
						downloads[link] = pdf
						downloadOrder = append(downloadOrder, link)
					}
				}
				continue
			case route.Supported:
				outputURL = route.CanonicalURL
				if outputURL == pageURL {
					continue
				}
				itemKind = "directory_or_boundary"
				if route.Adapter == "article" {
					itemKind = "article"
				}
			case promoteBoundaries[route.Boundary]:
				outputURL = publicHTTPSOutputURL(pageURL, href)
				if outputURL == "" {
					continue
				}
				itemKind = "directory_or_boundary"
			default:
				continue
			}

			title := nodeText(anchor)
			if len([]rune(title)) > 300 {
				title = string([]rune(title)[:300])
			}
			image := firstElement(anchor, ".//img[1]")
			if title == "" && image != nil {
				title = plainText(attr(image, "alt"))
				if len([]rune(title)) > 300 {
					title = string([]rune(title)[:300])
				}
			}
			if existing, ok := items[outputURL]; ok {
				if title != "" && len(title) > len(asString(existing["title"])) {
					existing["title"] = title
				}
				continue
			}
			imageURL := ""
			if image != nil {
				source := attr(image, "data-src")
				if source == "" {
					source = attr(image, "src")
				}
				imageURL = promoteImageURL(pageURL, source)
			}
			items[outputURL] = map[string]any{
				"title":     emptyToNil(title),
				"url":       outputURL,
				"image":     emptyToNil(imageURL),
				"summary":   nil,
				"badges":    []any{},
				"item_kind": itemKind,
				// Per item, not just per page: an agent that lifts one entry out
				// of this listing must carry the commercial label with it.
				"sponsored":                        true,
				"commercial_editorial":             true,
				"rendered_visibility_not_verified": true,
			}
			order = append(order, outputURL)
		}
	}

	list := make([]map[string]any, 0, len(order))
	for _, link := range order {
		list = append(list, items[link])
	}
	var modules []snapshotModule
	if len(list) > 0 {
		modules = append(modules, snapshotModule{
			Key: "public-directory." + kind, Name: "特别呈现资讯与案例",
			Lane: "main", Order: 0, State: "server_rendered", Items: list,
		})
	}
	result := directoryResult(modules)
	for _, item := range list {
		item["access"] = "ssr_directory_candidate"
	}
	downloadList := []any{}
	for _, link := range downloadOrder {
		downloadList = append(downloadList, downloads[link])
	}
	result["sponsored"] = true
	result["commercial_editorial"] = true
	result["downloads"] = downloadList
	result["downloads_count"] = len(downloadList)
	result["pagination"] = "none_static_html"
	result["scripts_not_executed"] = true
	result["linked_pages_not_fetched"] = true
	result["downloads_not_fetched"] = true
	return result, nil
}

// publicHTTPSOutputURL emits an off-site destination, refusing anything that is
// not already a clean https url on a real public host.
//
// It deliberately does not upgrade or repair: a listing entry is a destination
// the page itself published, and rewriting `http://` to `https://` here would
// invent a url the site never linked to. Whitespace is checked on the raw
// attribute rather than after resolution, because resolving percent-encodes a
// space and would hide a malformed href.
func publicHTTPSOutputURL(base, value string) string {
	if strings.ContainsAny(value, " \t\n\r") {
		return ""
	}
	raw := absoluteURL(base, value)
	if raw == "" || strings.ContainsAny(raw, " \t\n\r") {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.Scheme != "https" {
		return ""
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return ""
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if !strings.Contains(host, ".") || host == "localhost" {
		return ""
	}
	for _, suffix := range []string{".localhost", ".local", ".internal", ".home.arpa"} {
		if strings.HasSuffix(host, suffix) {
			return ""
		}
	}
	if net.ParseIP(host) != nil {
		return ""
	}
	// Emitted in the form the page published. Resolving percent-encodes
	// non-ASCII path segments, and a listing should hand back the link the site
	// actually wrote rather than a re-spelled equivalent.
	if trimmed := strings.TrimSpace(value); strings.HasPrefix(trimmed, "https://") {
		return trimmed
	}
	return raw
}
