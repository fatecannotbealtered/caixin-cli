package caixin

import (
	"context"
	"net"
	"net/http"
	neturl "net/url"
	"regexp"
	"strings"
	"time"

	"github.com/antchfx/htmlquery"
	xhtml "golang.org/x/net/html"
)

// A microsite is a hand-built campaign page: no template, no module structure,
// just whatever its designer wrote. So it is read as a link surface rather than
// as an article listing -- what it links to, what it offers for download, and
// how much of it is machinery this client did not run.
//
// Nothing here reads prose. `arbitrary_text_not_extracted` says so on the way
// out, and the only text kept is the label on a link.

// micrositeEntrypoints is the exact set of measured microsites, ported verbatim.
var micrositeEntrypoints = buildMicrositeEntrypoints()

func buildMicrositeEntrypoints() map[string]string {
	entries := map[string]string{}
	for _, group := range []struct {
		host, kind string
		paths      []string
	}{
		{"topics.caixin.com", "legacy_topics", []string{
			"/2021/2021cxss/", "/2021/caixin_summit2021/", "/2021/caixin_summit2021sz/",
			"/2022/2022cxss/", "/2022/caixin_summit2022/", "/2023/2023cxss/",
			"/2023/asia-new-vision-forum/", "/2023/caixin_summit2023/", "/2024/2024cxss/",
			"/2024/asia-new-vision-forum_2024/", "/2024/caixin_summit_2024/",
			"/2025/2025cxss/", "/2025/asia-new-vision-forum_2025/",
			"/2025/caixin_summit2025/", "/2026/2026cxss/",
		}},
		{"opinion.caixin.com", "legacy_opinion", []string{
			"/2018/gaigekaifang/", "/2015/huyaobang/index.html", "/2015/guoqi/index.html",
			"/2015/jihuashengyu/", "/2014/xianfa/index.html", "/2014/tengxunqihu/index.html",
			"/2014/tiruoer/index.html", "/2014/zibenlun/index.html",
			"/2014/fanlongduan/index.html", "/2014/dengxiaoping/index.html",
			"/2014/caichan/index.html", "/2014/yuwaifanfu/index.html",
		}},
		{"economy.caixin.com", "event_directory", []string{
			"/2024/boao2024/", "/2024/developmentforum2024/", "/2025/boao2025/",
			"/2025/developmentforum2025/", "/2026/boao2026/", "/2026/developmentforum2026/",
		}},
		{"international.caixin.com", "event_directory", []string{
			"/2025/2025xjdws/", "/2026/2026djdws/", "/2026/2026xjdws/",
		}},
		{"promote.caixin.com", "promote_microsite", []string{
			"/esg2024-march/", "/esg30-young-scholars-2025/", "/renbenzhineng/", "/szsd/",
		}},
	} {
		for _, path := range group.paths {
			entries["https://"+group.host+path] = group.kind
		}
	}
	entries["https://topics.caixin.com/2024/75zn/"] = "topic_landing"
	entries["https://topics.caixin.com/2025/2025lhzt/"] = "topic_landing"
	entries["https://topics.caixin.com/2026/2026lhzt/"] = "topic_landing"
	return entries
}

// micrositeURL canonicalizes a microsite url and names its kind.
//
// The tracking aliases are allowed one by one rather than by rule: the site
// links its own microsites with a handful of exact query strings, and accepting
// an arbitrary query would let a caller smuggle parameters into the fetch.
func micrositeURL(raw string) (canonical, kind string) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, " \t\n\r#") {
		return "", ""
	}
	parsed, err := neturl.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return "", ""
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return "", ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "caixin.com" && !strings.HasSuffix(host, ".caixin.com") {
		return "", ""
	}
	if query := parsed.RawQuery; query != "" {
		conferenceAlias := host == "topics.caixin.com" &&
			parsed.Path == "/2025/asia-new-vision-forum_2025/" && query == "from=conference"
		trackingHost := host == "topics.caixin.com" || host == "economy.caixin.com" ||
			host == "international.caixin.com"
		if !conferenceAlias && (!trackingHost || query != "cxapp_link=true") {
			return "", ""
		}
	}
	canonical = "https://" + host + parsed.Path
	kind = micrositeEntrypoints[canonical]
	if kind == "" {
		return "", ""
	}
	return canonical, kind
}

// publicHTTPSURL accepts an https url on a real, public host.
//
// A microsite links out to conference sponsors and file hosts, so the target is
// checked for being addressable at all: a bare hostname or an IP literal in a
// campaign page is a mistake or a probe, not a destination worth reporting.
func publicHTTPSURL(base, value string) string {
	raw := httpsOutputURL(base, value)
	if raw == "" {
		return ""
	}
	parsed, err := neturl.Parse(raw)
	if err != nil {
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
	return raw
}

// micrositeArticleURL accepts an article link from a microsite.
func micrositeArticleURL(base, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	raw := absoluteURL(base, value)
	if raw == "" {
		return ""
	}
	if parsed, err := neturl.Parse(raw); err == nil && parsed.Fragment == "gocomment" {
		parsed.Fragment = ""
		raw = parsed.String()
	}
	return validateArticleURL(raw)
}

// micrositeSharedChrome reports whether a node sits in site furniture rather
// than in the page's own content.
func micrositeSharedChrome(node, root *xhtml.Node) bool {
	for current := node; current != nil; current = current.Parent {
		tag := strings.ToLower(current.Data)
		if tag == "aside" || tag == "footer" || tag == "header" || tag == "nav" {
			return true
		}
		identity := strings.ToLower(attr(current, "id") + " " + attr(current, "class"))
		for _, token := range regexp.MustCompile(`[^a-z0-9]+`).Split(identity, -1) {
			switch token {
			case "aside", "bottom", "copyright", "footer", "header", "menu",
				"navbar", "navigation", "nav", "sidebar", "sitefooter", "siteheader":
				return true
			}
		}
		if current == root {
			break
		}
	}
	return false
}

// micrositeAnchorText names a link the way a reader would have read it.
func micrositeAnchorText(
	anchor, root *xhtml.Node,
	context visibilityContext,
	hidden stylesheetHidden,
) string {
	candidates := []string{
		plainText(attr(anchor, "title")),
		micrositeVisibleText(anchor, root, context, hidden),
	}
	for _, image := range htmlquery.Find(anchor, ".//img[@alt]") {
		if micrositeNodeVisible(image, root, context, hidden) {
			candidates = append(candidates, plainText(attr(image, "alt")))
			break
		}
	}
	for _, node := range htmlquery.Find(anchor,
		".//*[contains(@class,'title') or contains(@class,'tit')]") {
		if micrositeNodeVisible(node, root, context, hidden) {
			candidates = append(candidates, micrositeVisibleText(node, root, context, hidden))
			break
		}
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if runes := []rune(candidate); len(runes) > 300 {
			return string(runes[:300])
		}
		return candidate
	}
	return ""
}

var micrositeSubdirectoryPath = regexp.MustCompile(`^/20\d{2}/\d{1,20}/\d{1,20}/$`)
var micrositeArticleDate = regexp.MustCompile(`/(20\d{2}-\d{2}-\d{2})/`)

// stripNonContent removes the elements that are machinery rather than content,
// after counting them. The counts are the honest answer to "what did you not
// run": scripts, forms, and frames are reported, never executed.
func stripNonContent(root *xhtml.Node) (scripts, forms, iframes, media int) {
	count := func(xpath string) int { return len(htmlquery.Find(root, xpath)) }
	scripts = count(".//script")
	forms = count(".//form")
	iframes = count(".//iframe")
	media = count(".//img|.//video|.//audio|.//object|.//embed|.//iframe")
	for _, node := range htmlquery.Find(root,
		".//script|.//style|.//noscript|.//template|.//form|.//iframe|"+
			".//video|.//audio|.//object|.//embed") {
		if node.Parent != nil {
			node.Parent.RemoveChild(node)
		}
	}
	return scripts, forms, iframes, media
}

// micrositeDocument reads one microsite page.
func micrositeDocument(doc *xhtml.Node, pageURL, kind string) (map[string]any, error) {
	hidden := cssHiddenNodes(doc)
	var root *xhtml.Node
	switch kind {
	case "legacy_opinion":
		root = firstElement(doc, "("+classXPath("comMain")+")[1]")
	case "event_directory":
		root = firstElement(doc, "("+classXPath("boaoBody")+")[1]")
	case "promote_microsite", "esg30_resource":
		root = firstElement(doc,
			"("+classXPath("conference_topic_main")+")[1]", "("+classXPath("main")+")[1]")
	case "topic_landing":
		root = firstElement(doc, "("+classXPath("index-container")+")[1]")
	}
	if root == nil {
		root = firstElement(doc, "//body")
	}
	if root == nil {
		return nil, &APIError{Message: "the microsite page carries no visible content root"}
	}

	scripts, forms, iframes, media := stripNonContent(root)
	context := contentVisibilityContext(root)
	page, _ := neturl.Parse(pageURL)

	itemsByURL := map[string]map[string]any{}
	var itemOrder []string
	subdirectories := []any{}
	seenSubdirectories := map[string]bool{}
	downloads := []any{}
	seenDownloads := map[string]bool{}
	externals := map[string]bool{}

	for _, anchor := range htmlquery.Find(root, ".//a[@href]") {
		if kind == "esg30_resource" && micrositeSharedChrome(anchor, root) {
			continue
		}
		if !micrositeNodeVisible(anchor, root, context, hidden) {
			continue
		}
		href := strings.TrimSpace(attr(anchor, "href"))
		if strings.HasPrefix(href, "#") {
			continue
		}
		title := micrositeAnchorText(anchor, root, context, hidden)
		absolute := absoluteURL(pageURL, href)
		if absolute == "" {
			continue
		}

		if article := micrositeArticleURL(pageURL, href); article != "" {
			item, present := itemsByURL[article]
			if !present {
				var published any
				if match := micrositeArticleDate.FindStringSubmatch(article); match != nil {
					published = match[1]
				}
				itemsByURL[article] = map[string]any{
					"title":        title,
					"url":          article,
					"summary":      nil,
					"published_at": published,
					"badges":       []any{},
					"item_kind":    "article",
				}
				itemOrder = append(itemOrder, article)
			} else if title != "" &&
				len([]rune(title)) > len([]rune(asString(item["title"]))) {
				// The same article is often linked twice, once from a picture
				// and once from its headline. The longer label is the headline;
				// measured in characters, so a Chinese label is not counted as
				// three times its length.
				item["title"] = title
			}
			continue
		}

		parsed, err := neturl.Parse(absolute)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
			continue
		}
		if strings.HasSuffix(strings.ToLower(parsed.Path), ".pdf") {
			download := publicHTTPSURL(pageURL, href)
			if download == "" || seenDownloads[download] {
				continue
			}
			target, err := neturl.Parse(download)
			if err != nil || target.RawQuery != "" || target.Fragment != "" {
				continue
			}
			seenDownloads[download] = true
			downloads = append(downloads, map[string]any{
				"title":                            emptyToNil(title),
				"url":                              download,
				"expected_mime_type":               "application/pdf",
				"access":                           "ssr_download_candidate",
				"content_not_fetched":              true,
				"rendered_visibility_not_verified": true,
			})
			continue
		}

		host := strings.ToLower(parsed.Hostname())
		if host != "caixin.com" && !strings.HasSuffix(host, ".caixin.com") {
			if publicHTTPSURL(pageURL, href) != "" {
				externals[absolute] = true
			}
			continue
		}
		safe := caixinOutputURL(pageURL, href)
		if safe == "" {
			continue
		}
		target, err := neturl.Parse(safe)
		if err != nil {
			continue
		}
		if (kind == "event_directory" || kind == "topic_landing") &&
			strings.EqualFold(target.Hostname(), page.Hostname()) &&
			micrositeSubdirectoryPath.MatchString(target.Path) &&
			target.RawQuery == "" && target.Fragment == "" && !seenSubdirectories[safe] {
			seenSubdirectories[safe] = true
			subdirectories = append(subdirectories, map[string]any{
				"title":                            title,
				"url":                              safe,
				"access":                           "ssr_directory_candidate",
				"rendered_visibility_not_verified": true,
				"content_not_fetched":              true,
			})
		}
	}

	var items []map[string]any
	for _, link := range itemOrder {
		item := itemsByURL[link]
		if asString(item["title"]) == "" {
			// The link was there but nothing readable named it. Saying so is
			// better than inventing a title from the url.
			item["title"] = nil
			item["title_missing"] = true
		}
		items = append(items, item)
	}

	var modules []snapshotModule
	if len(items) > 0 {
		modules = append(modules, snapshotModule{
			Key: "microsite." + kind + ".articles", Name: "SSR 文章目录候选",
			Lane: "main", Order: 0, State: "server_rendered", Items: items,
		})
	}
	result := attachClickConsumers(directoryResult(modules))
	for _, raw := range asList(result["modules"]) {
		module, _ := raw.(map[string]any)
		for _, entry := range asList(module["items"]) {
			if item, ok := entry.(map[string]any); ok {
				item["access"] = "ssr_directory_candidate"
			}
		}
	}

	sponsored := kind == "promote_microsite" ||
		(kind == "esg30_resource" && strings.ToLower(page.Hostname()) == "promote.caixin.com")
	result["kind"] = kind
	result["sponsored"] = sponsored
	result["title"] = firstXPathText(doc, "//meta[@property='og:title']/@content", "//title")
	result["title_source"] = "anonymous_ssr_metadata"
	result["description_not_extracted"] = true
	result["arbitrary_text_not_extracted"] = true
	result["rendered_visibility_not_verified"] = true
	result["external_stylesheets_not_fetched"] = true
	result["subdirectories"] = subdirectories
	result["page_navigation_not_extracted"] = true
	result["external_links_count"] = len(externals)
	result["downloads"] = downloads
	result["downloads_count"] = len(downloads)
	result["media_elements_count"] = media
	result["pagination"] = "none_static_html"
	result["scripts_count"] = scripts
	result["scripts_not_executed"] = true
	result["forms_count"] = forms
	result["forms_not_submitted"] = true
	result["iframes_count"] = iframes
	result["iframes_ignored"] = true
	result["media_not_downloaded"] = true
	result["downloads_not_fetched"] = true
	result["external_links_not_fetched"] = true
	result["linked_pages_not_fetched"] = true
	return result, nil
}

// Microsite reads one standalone Caixin microsite.
func (c *Client) Microsite(ctx context.Context, pageURL string) (map[string]any, error) {
	canonical, kind := micrositeURL(pageURL)
	if canonical == "" {
		return nil, invalid("microsite reads one of the measured Caixin microsites; " +
			"run `caixin-cli reference` for the list")
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
		return nil, &APIError{Message: "could not parse the microsite page"}
	}
	result, err := micrositeDocument(doc, canonical, kind)
	if err != nil {
		return nil, err
	}
	result["source"] = map[string]any{
		"requested_url": pageURL,
		"canonical_url": canonical,
		"final_url":     canonical,
		"fetched_at":    time.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	result["session_used"] = false
	return result, nil
}
