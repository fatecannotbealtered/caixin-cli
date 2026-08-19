package caixin

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/antchfx/htmlquery"
	xhtml "golang.org/x/net/html"
)

// The editorial surface is Caixin's server-rendered HTML: channel front pages,
// section and public directories, magazine issues, author pages. None of it is
// a JSON API, so every command here is a page fetch plus extraction.
//
// Two rules hold everywhere in this file:
//
//   - Nothing is executed. `javascript_executed` is false and stays false; what
//     a script would have added is simply absent, and the payload says so
//     rather than pretending the listing is complete.
//   - Visibility is read from the server markup only. `source_state` reports
//     what the HTML declares; it is not a claim about what a browser would
//     paint, which is why `rendered_visibility_verified` is false.

// classXPath matches an element carrying one class among several.
//
// The naive `@class='x'` fails on `class="a x b"`, and `contains(@class,'x')`
// matches `xylophone`. Padding both sides is the standard way to ask for a
// whole class token in XPath 1.0.
func classXPath(name string) string {
	return "//*[contains(concat(' ',normalize-space(@class),' '),' " + name + " ')]"
}

func classPredicate(name string) string {
	return "contains(concat(' ',normalize-space(@class),' '),' " + name + " ')"
}

// whitespaceRunEditorial collapses a run of spacing to one space. The Unicode
// space separators are included because Caixin's markup is full of `&nbsp;`,
// and treating one as a visible character would change the text.
var whitespaceRunEditorial = regexp.MustCompile(`[\s\p{Zs}]+`)

// attributeWhitespace matches the line wrapping markup puts inside a url.
var attributeWhitespace = regexp.MustCompile(`[\s\p{Zs}]+`)

// nodeText flattens an element to single-spaced text.
func nodeText(node *xhtml.Node) string {
	if node == nil {
		return ""
	}
	return strings.TrimSpace(whitespaceRunEditorial.ReplaceAllString(htmlquery.InnerText(node), " "))
}

// firstText returns the first non-empty text among several xpaths.
func firstXPathText(node *xhtml.Node, xpaths ...string) string {
	for _, xpath := range xpaths {
		for _, found := range htmlquery.Find(node, xpath) {
			if text := nodeText(found); text != "" {
				return text
			}
		}
	}
	return ""
}

func firstElement(node *xhtml.Node, xpaths ...string) *xhtml.Node {
	for _, xpath := range xpaths {
		if found := htmlquery.FindOne(node, xpath); found != nil {
			return found
		}
	}
	return nil
}

func attr(node *xhtml.Node, name string) string {
	if node == nil {
		return ""
	}
	for _, a := range node.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

func hasAttr(node *xhtml.Node, name string) bool {
	if node == nil {
		return false
	}
	for _, a := range node.Attr {
		if a.Key == name {
			return true
		}
	}
	return false
}

// absoluteURL resolves a possibly-relative href against the page.
func absoluteURL(base, value string) string {
	// Markup wraps long attribute values across lines. A url cannot contain
	// whitespace, so the wrapping is removed instead of rejecting the value --
	// otherwise a legitimately-linked image silently disappears.
	value = attributeWhitespace.ReplaceAllString(html.UnescapeString(value), "")
	if value == "" {
		return ""
	}
	baseParsed, err := url.Parse(base)
	if err != nil {
		return ""
	}
	ref, err := url.Parse(value)
	if err != nil {
		return ""
	}
	return baseParsed.ResolveReference(ref).String()
}

// upgradeCaixinScheme rewrites a bare-http caixin.com link to https.
//
// The pages still emit http links; following them would downgrade a request
// that the site itself redirects, so they are normalized before use.
func upgradeCaixinScheme(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	host := strings.ToLower(parsed.Hostname())
	if parsed.Scheme != "http" || parsed.User != nil {
		return raw
	}
	if host != "caixin.com" && !strings.HasSuffix(host, ".caixin.com") {
		return raw
	}
	if port := parsed.Port(); port != "" && port != "80" {
		return raw
	}
	parsed.Scheme = "https"
	parsed.Host = host
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	return parsed.String()
}

// contentURL normalizes a link that is meant to point at readable content.
func contentURL(base, value string) string {
	raw := absoluteURL(base, value)
	if raw == "" {
		return ""
	}
	raw = upgradeCaixinScheme(raw)
	// A card links to exactly three kinds of readable thing: an article, a Key
	// topic, or a deepview topic. Anything else in a card slot is chrome or an
	// ad, and admitting it would put an unreadable url in a listing.
	if link := articleURL(base, raw); link != "" {
		return link
	}
	if link := blogPostURL(raw); link != "" {
		return link
	}
	if link := keyTopicURL(raw); link != "" {
		return link
	}
	return deepviewURL(raw)
}

// imageURL accepts only Caixin's image host, so a page cannot steer an agent
// at an arbitrary third-party fetch.
func imageURL(base, value string) string {
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
	if strings.ToLower(parsed.Hostname()) != "img.caixin.com" {
		return ""
	}
	// Only real image paths: a page can put anything in a src, and an agent
	// following one into a document or a script would be following the page's
	// choice rather than the user's.
	if !imagePathPattern.MatchString(parsed.Path) {
		return ""
	}
	// The markup still emits http; the host serves https and redirects, so the
	// emitted url is normalized rather than handing out a downgrade.
	parsed.Scheme = "https"
	parsed.Host = "img.caixin.com"
	return parsed.String()
}

var imagePathPattern = regexp.MustCompile(`(?i)^/[A-Za-z0-9_/%+.,@~-]+\.(gif|jpe?g|png|webp)$`)

// cssHidden reports whether an inline style hides the element.
var cssHiddenPattern = regexp.MustCompile(`(?i)(display\s*:\s*none|visibility\s*:\s*hidden)`)

// serverSourceState reports what the markup says about an element's visibility.
//
// It walks ancestors because a hidden container hides its children, and it
// treats a `tab-cons` list item as visible only when it carries `display` --
// that is how the site marks the active tab server-side.
func serverSourceState(node *xhtml.Node) string {
	for current := node; current != nil; current = current.Parent {
		if current.Type != xhtml.ElementNode {
			continue
		}
		if hasAttr(current, "hidden") {
			return "hidden"
		}
		if strings.EqualFold(attr(current, "aria-hidden"), "true") {
			return "hidden"
		}
		for _, class := range strings.Fields(attr(current, "class")) {
			if class == "hidden" || class == "hide" || class == "undis" {
				return "hidden"
			}
		}
		if cssHiddenPattern.MatchString(attr(current, "style")) {
			return "hidden"
		}
	}
	tab := htmlquery.FindOne(node, "ancestor::li[parent::ul["+classPredicate("tab-cons")+"]][1]")
	if tab != nil {
		for _, class := range strings.Fields(attr(tab, "class")) {
			if class == "display" {
				return "visible"
			}
		}
		return "hidden"
	}
	return "visible"
}

// navigationContextMarkers are the class and id fragments that mark a link as
// navigation rather than content.
var navigationContextMarkers = []string{
	"nav", "menu", "tab", "more", "title", "tit",
	"category", "channel", "column", "section",
}

// isNavigationContext reports whether an anchor sits in chrome rather than in
// the body of the page.
func isNavigationContext(node *xhtml.Node) bool {
	for current := node; current != nil; current = current.Parent {
		if current.Type != xhtml.ElementNode {
			continue
		}
		switch strings.ToLower(current.Data) {
		case "header", "nav", "footer":
			return true
		}
		marker := strings.ToLower(attr(current, "class") + " " + attr(current, "id"))
		for _, needle := range navigationContextMarkers {
			if strings.Contains(marker, needle) {
				return true
			}
		}
	}
	return false
}

// itemOptions configures one item extraction.
type itemOptions struct {
	AnchorXPaths  []string
	ImageXPaths   []string
	SummaryXPaths []string
	BadgeXPaths   []string
}

// historicalPattern spots a link to an article from a previous year, which is
// how a front page reveals it is serving stale filler.
var historicalPattern = regexp.MustCompile(`/(20\d{2})-\d{2}-\d{2}/\d+\.html$`)

// extractItem builds one listing entry from a card node.
//
// It returns nil when the card carries no usable link: a card without a
// resolvable url and a title is chrome, not content, and emitting it would pad
// `items_count` with entries an agent cannot act on.
func extractItem(node *xhtml.Node, pageURL string, options itemOptions) map[string]any {
	var anchor *xhtml.Node
	var link, title string
	for _, xpath := range options.AnchorXPaths {
		for _, candidate := range htmlquery.Find(node, xpath) {
			candidateURL := contentURL(pageURL, attr(candidate, "href"))
			candidateTitle := nodeText(candidate)
			if candidateURL != "" && candidateTitle != "" {
				anchor, link, title = candidate, candidateURL, candidateTitle
				break
			}
		}
		if anchor != nil {
			break
		}
	}
	if anchor == nil {
		return nil
	}

	imageXPaths := options.ImageXPaths
	if imageXPaths == nil {
		imageXPaths = []string{".//img"}
	}
	image := ""
	for _, xpath := range imageXPaths {
		imageNode := firstElement(node, xpath)
		if imageNode == nil {
			continue
		}
		source := attr(imageNode, "data-src")
		if source == "" {
			source = attr(imageNode, "src")
		}
		if image = imageURL(pageURL, source); image != "" {
			break
		}
	}

	summary := ""
	for _, xpath := range options.SummaryXPaths {
		if value := firstXPathText(node, xpath); value != "" && value != title {
			summary = value
			break
		}
	}

	badges := []any{}
	for _, xpath := range options.BadgeXPaths {
		for _, candidate := range htmlquery.Find(node, xpath) {
			badge := ""
			href := attr(candidate, "href")
			classes := map[string]bool{}
			for _, class := range strings.Fields(attr(candidate, "class")) {
				classes[class] = true
			}
			switch {
			case href != "":
				if channelURL(pageURL, href) == "" {
					continue
				}
				badge = nodeText(candidate)
			case classes["icon_key"] || classes["icon_free"]:
				badge = plainText(attr(candidate, "title"))
				if badge == "" {
					if classes["icon_key"] {
						badge = "收费文章"
					} else {
						badge = "免费文章"
					}
				}
			default:
				continue
			}
			if badge == "" || badge == title {
				continue
			}
			duplicate := false
			for _, existing := range badges {
				if existing == badge {
					duplicate = true
					break
				}
			}
			if !duplicate {
				badges = append(badges, badge)
			}
		}
	}

	item := map[string]any{
		"title":   title,
		"url":     link,
		"image":   emptyToNil(image),
		"summary": emptyToNil(summary),
		"badges":  badges,
	}
	if parsed, err := url.Parse(link); err == nil {
		if match := historicalPattern.FindStringSubmatch(parsed.Path); match != nil {
			if year := match[1]; year < fmt.Sprintf("%d", timeNow().Year()) {
				item["historical_fallback"] = true
			}
		}
	}
	return item
}

// channelURL accepts only links that are themselves snapshot entry points, so a
// badge cannot smuggle in an arbitrary destination.
func channelURL(base, value string) string {
	raw := absoluteURL(base, value)
	if raw == "" {
		return ""
	}
	if _, ok := snapshotPageKey(raw); ok {
		return raw
	}
	return ""
}

// snapshotModule is one titled block of a page.
type snapshotModule struct {
	Key             string
	Name            string
	Lane            string
	Order           int
	State           string
	ActiveItemIndex *int
	// MoreURL is the module's own "see all" link, when the block has one.
	MoreURL string
	// ContinuationAvailable marks a block that carries more entries behind a
	// control this client does not operate. The evidence names what was seen.
	ContinuationAvailable bool
	ContinuationEvidence  string
	// SharedSidebar marks a block the channel repeats across its sections, so a
	// caller can tell it apart from content specific to this page.
	SharedSidebar bool
	Items         []map[string]any
}

func (m snapshotModule) asMap() map[string]any {
	items := make([]any, 0, len(m.Items))
	for _, item := range m.Items {
		items = append(items, item)
	}
	out := map[string]any{
		"key":   m.Key,
		"name":  m.Name,
		"lane":  m.Lane,
		"order": m.Order,
		"state": m.State,
		"items": items,
	}
	if m.ActiveItemIndex != nil {
		out["active_item_index"] = *m.ActiveItemIndex
	}
	if m.MoreURL != "" {
		out["more_url"] = m.MoreURL
	}
	if m.ContinuationAvailable {
		out["continuation_available"] = true
		out["continuation_evidence"] = m.ContinuationEvidence
	}
	if m.SharedSidebar {
		out["shared_sidebar"] = true
	}
	return out
}

func modulesAsList(modules []snapshotModule) []any {
	out := make([]any, 0, len(modules))
	for _, module := range modules {
		out = append(out, module.asMap())
	}
	return out
}

// navigationRoots are the containers whose links count as navigation for one
// page kind. Each page template puts its chrome somewhere different.
func navigationRoots(doc *xhtml.Node, pageKey string) []*xhtml.Node {
	var selectors []string
	switch pageKey {
	case "home":
		selectors = []string{classXPath("homepageCon")}
	case "mini":
		selectors = []string{classXPath("indexMainCon") + "/div[" + classPredicate("mainConLeft") + "]"}
	case "datanews", "en-index", "photos-index":
		selectors = []string{classXPath("comMain")}
	case "culture", "opinion", "wenews-index":
		selectors = []string{classXPath("indexMain") + "/div[" + classPredicate("indexMainCon") + "]"}
	case "esg":
		selectors = []string{classXPath("mainConBox")}
	case "topics":
		selectors = []string{classXPath("leftJingjiBox"), classXPath("rightJinrongBox")}
	case "newsletter":
		selectors = []string{classXPath("cx-aggs-top"), classXPath("cx-aggs-news")}
	case "video-index":
		selectors = []string{
			classXPath("head") + "/div[" + classPredicate("headcenter") +
				"]/div[" + classPredicate("fl") + "]",
			classXPath("navlist"),
			"//*[" + classPredicate("box1") + " or " + classPredicate("box2") +
				"]/div[" + classPredicate("newvideo") + "]",
		}
	case "blog-index":
		selectors = []string{classXPath("main-blog") + "/div[" + classPredicate("blog-index") + "]"}
	case "economy", "finance", "companies", "china", "international", "science", "health":
		selectors = []string{classXPath("indexMain")}
	default:
		switch {
		case publicationRoots[pageKey]:
			selectors = []string{"(" + classXPath("focus") + ")[1]", classXPath("mainConLeft")}
		case categorySnapshotKeys[pageKey]:
			selectors = []string{classXPath("comMain")}
		}
	}
	var roots []*xhtml.Node
	seen := map[*xhtml.Node]bool{}
	for _, selector := range selectors {
		for _, root := range htmlquery.Find(doc, selector) {
			if seen[root] {
				continue
			}
			seen[root] = true
			roots = append(roots, root)
		}
	}
	return roots
}

// navigationConsumer resolves a chrome link into the command that would consume
// it, or nothing when this build cannot consume it at all.
func navigationConsumer(pageURL, value string) map[string]any {
	raw := absoluteURL(pageURL, value)
	if raw == "" {
		return nil
	}
	raw = upgradeCaixinScheme(raw)
	route := ClassifyURL(raw)
	if !route.Supported {
		return nil
	}
	return route.AsEmbeddedMap()
}

// contentNavigation lists the chrome links that are not already reachable
// through an item, so an agent sees where else it could go without being handed
// the same destination twice.
func contentNavigation(doc *xhtml.Node, pageURL, pageKey string, result map[string]any) []any {
	emitted := map[string]bool{}
	remember := func(value any) {
		item, ok := value.(map[string]any)
		if !ok {
			return
		}
		for _, key := range []string{"url", "image_click_url"} {
			link, ok := item[key].(string)
			if !ok {
				continue
			}
			if consumer := navigationConsumer(pageURL, link); consumer != nil {
				emitted[asString(consumer["canonical_url"])] = true
			}
		}
	}
	for _, raw := range asList(result["modules"]) {
		module, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		for _, item := range asList(module["items"]) {
			remember(item)
		}
	}
	for _, key := range []string{"subdirectories", "downloads", "featured_articles"} {
		for _, item := range asList(result[key]) {
			remember(item)
		}
	}
	if pagination, ok := result["pagination"].(map[string]any); ok {
		for _, item := range asList(pagination["links"]) {
			remember(item)
		}
	}
	if current, ok := result["current_issue"].(map[string]any); ok {
		remember(current)
		remember(current["lead"])
	}

	navigation := []any{}
	seen := map[string]bool{}
	for _, root := range navigationRoots(doc, pageKey) {
		for _, anchor := range htmlquery.Find(root, "self::a[@href] | .//a[@href]") {
			if serverSourceState(anchor) != "visible" || !isNavigationContext(anchor) {
				continue
			}
			title := nodeText(anchor)
			if title == "" {
				continue
			}
			consumer := navigationConsumer(pageURL, attr(anchor, "href"))
			if consumer == nil {
				continue
			}
			link := asString(consumer["canonical_url"])
			if emitted[link] || seen[link] {
				continue
			}
			navigation = append(navigation, map[string]any{
				"title":               title,
				"url":                 link,
				"consumer":            consumer,
				"content_not_fetched": true,
				"source_state":        "visible",
			})
			seen[link] = true
		}
	}
	return navigation
}

func asList(value any) []any {
	if list, ok := value.([]any); ok {
		return list
	}
	return nil
}

// attachClickConsumers gives every emitted url the command that consumes it.
//
// This is what makes a listing actionable: an agent reads `consumer.command`
// and runs it verbatim rather than guessing which command takes this url.
// attachPaginationConsumers additionally resolves the page links.
//
// Only the directories that publish real static pages do this; on pages whose
// "next" link is a script hook there is nothing to consume and emitting a
// boundary verdict for it would be noise.
func attachPaginationConsumers(result map[string]any) map[string]any {
	if pagination, ok := result["pagination"].(map[string]any); ok {
		for _, raw := range asList(pagination["links"]) {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if link, ok := item["url"].(string); ok {
				item["consumer"] = ClassifyURL(link).AsEmbeddedMap()
			}
		}
	}
	return result
}

func attachClickConsumers(result map[string]any) map[string]any {
	var items []any
	for _, raw := range asList(result["modules"]) {
		if module, ok := raw.(map[string]any); ok {
			items = append(items, asList(module["items"])...)
		}
	}
	for _, key := range []string{"subdirectories", "downloads", "navigation", "featured_articles"} {
		items = append(items, asList(result[key])...)
	}
	// The current issue and its lead are single objects rather than a list, but
	// they are just as clickable, so they get the same treatment.
	if current, ok := result["current_issue"].(map[string]any); ok {
		items = append(items, current)
		if lead, ok := current["lead"].(map[string]any); ok {
			items = append(items, lead)
		}
	}

	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if link, ok := item["url"].(string); ok {
			item["consumer"] = ClassifyURL(link).AsEmbeddedMap()
		}
		if link, ok := item["image_click_url"].(string); ok {
			item["image_click_consumer"] = ClassifyURL(link).AsEmbeddedMap()
		}
	}
	return result
}

// finalizeSnapshot adds navigation and click consumers to a page result.
func finalizeSnapshot(doc *xhtml.Node, pageURL, pageKey string, result map[string]any) map[string]any {
	navigation := contentNavigation(doc, pageURL, pageKey, result)
	result["navigation"] = navigation
	result["navigation_count"] = len(navigation)
	return attachClickConsumers(result)
}

// countItems reports how many entries a module list carries.
func countItems(modules []snapshotModule) int {
	total := 0
	for _, module := range modules {
		total += len(module.Items)
	}
	return total
}

// findInDocumentOrder evaluates several xpaths and returns the union in the
// order the nodes appear in the page.
//
// A union expression is not enough: the xpath engine returns each branch's
// matches in turn, so two interleaved blocks come back grouped rather than in
// reading order -- and a front page's headline order is part of what it says.
func findInDocumentOrder(root *xhtml.Node, xpaths ...string) []*xhtml.Node {
	found := map[*xhtml.Node]bool{}
	for _, xpath := range xpaths {
		for _, node := range htmlquery.Find(root, xpath) {
			found[node] = true
		}
	}
	if len(found) == 0 {
		return nil
	}
	document := root
	for document.Parent != nil {
		document = document.Parent
	}
	var ordered []*xhtml.Node
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if found[node] {
			ordered = append(ordered, node)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	return ordered
}

// editorialItem builds one listing entry from an editorial card.
//
// It differs from extractItem in what it trusts: the editorial templates label
// their own badges, so a badge is whatever the markup says, while the mini
// templates carry channel links that have to be checked before they are shown
// as one.
func editorialItem(node *xhtml.Node, pageURL string, options itemOptions) map[string]any {
	var link, title string
	for _, xpath := range options.AnchorXPaths {
		for _, candidate := range htmlquery.Find(node, xpath) {
			candidateURL := caixinOutputURL(pageURL, attr(candidate, "href"))
			candidateTitle := nodeText(candidate)
			if candidateURL != "" && candidateTitle != "" {
				link, title = candidateURL, candidateTitle
				break
			}
		}
		if link != "" {
			break
		}
	}
	if link == "" {
		return nil
	}

	imageXPaths := options.ImageXPaths
	if imageXPaths == nil {
		imageXPaths = []string{".//img"}
	}
	image := ""
	for _, xpath := range imageXPaths {
		imageNode := firstElement(node, xpath)
		if imageNode == nil {
			continue
		}
		source := attr(imageNode, "data-src")
		if source == "" {
			source = attr(imageNode, "src")
		}
		if image = caixinOutputURL(pageURL, source); image != "" {
			break
		}
	}

	summary := ""
	for _, xpath := range options.SummaryXPaths {
		if value := firstXPathText(node, xpath); value != "" && value != title {
			summary = value
			break
		}
	}

	badges := []any{}
	for _, xpath := range options.BadgeXPaths {
		for _, candidate := range htmlquery.Find(node, xpath) {
			badge := attr(candidate, "title")
			if badge == "" {
				badge = nodeText(candidate)
			}
			if badge == "" {
				// A paywall marker carries no text of its own; it is an icon.
				classes := map[string]bool{}
				for _, class := range strings.Fields(attr(candidate, "class")) {
					classes[class] = true
				}
				switch {
				case classes["icon_key"]:
					badge = "收费文章"
				case classes["icon_free"]:
					badge = "免费文章"
				}
			}
			if badge == "" || badge == title || containsAny(badges, badge) {
				continue
			}
			badges = append(badges, badge)
		}
	}

	return map[string]any{
		"title":   title,
		"url":     link,
		"image":   emptyToNil(image),
		"summary": emptyToNil(summary),
		"badges":  badges,
	}
}
