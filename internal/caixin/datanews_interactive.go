package caixin

import (
	"context"
	"net/http"
	neturl "net/url"
	"regexp"
	"strings"
	"time"

	"github.com/antchfx/htmlquery"
	xhtml "golang.org/x/net/html"
)

// A 数字说 interactive is a visualisation: the story is drawn by scripts this
// client does not run. What can honestly be reported is its framing -- the
// heading, the standfirst, the articles it links -- and an explicit statement
// that the visualisation itself was not rendered.

// datanewsInteractiveAliases fold the spellings the directory publishes to the
// page that actually serves the project.
var datanewsInteractiveAliases = map[string]string{
	"https://datanews.caixin.com/interactive/2018/wenchuan/index.html": "https://datanews.caixin.com/interactive/2018/wenchuan/",
	"https://datanews.caixin.com/interactive/2018/water_resources":     "https://file.caixin.com/datanews_mobile/interactive/2018/water_resources/",
	"https://datanews.caixin.com/interactive/2018/tb2018":              "https://file.caixin.com/datanews_mobile/interactive/2018/tb2018/",
	"https://datanews.caixin.com/mobile/europe":                        "https://file.caixin.com/datanews_mobile/europe/",
	"https://datanews.caixin.com/2016/fang":                            "https://datanews.caixin.com/2016/fang/",
	"https://datanews.caixin.com/2015/tower/index.html":                "https://datanews.caixin.com/2015/tower/",
	"https://china.caixin.com/2015/hstjl/index.html":                   "https://china.caixin.com/2015/hstjl/",
	"https://datanews.caixin.com/2013/biange2013/index.html":           "https://datanews.caixin.com/2013/biange2013/",
	"https://datanews.caixin.com/2013/nobel/index.html":                "https://datanews.caixin.com/2013/nobel/",
	"https://datanews.caixin.com/2014/jindaoming/index.html":           "https://datanews.caixin.com/2014/jindaoming/",
}

// datanewsInteractiveTarget resolves a project url to the page to fetch.
func datanewsInteractiveTarget(canonical string) string {
	if mapped, ok := datanewsInteractiveAliases[canonical]; ok {
		return mapped
	}
	parsed, err := neturl.Parse(canonical)
	if err != nil {
		return canonical
	}
	parsed.Fragment = ""
	return parsed.String()
}

// numericOnly matches a "heading" that is really a number or punctuation.
var numericOnly = regexp.MustCompile(`^[\d\W_]+$`)

// datanewsInteractiveDocument reads one visualisation's framing.
func datanewsInteractiveDocument(
	doc *xhtml.Node,
	pageURL string,
	directoryTitle any,
) (map[string]any, error) {
	body := firstElement(doc, "//body")
	if body == nil {
		return nil, &APIError{Message: "the interactive project carries no HTML body"}
	}
	hidden := cssHiddenNodes(doc)
	if hidden.HideAll {
		// Without a reliable read of the stylesheet there is no honest way to
		// say what was on screen, so nothing is reported.
		return nil, &APIError{
			Message: "the interactive project's inline CSS visibility cannot be resolved safely",
		}
	}
	context := contentVisibilityContext(body)
	paywallPresent := context.Cutoff >= 0

	title := firstXPathText(doc, "//meta[@property='og:title']/@content", "//title")
	description := firstXPathText(doc,
		"//meta[translate(@name,'ABCDEFGHIJKLMNOPQRSTUVWXYZ',"+
			"'abcdefghijklmnopqrstuvwxyz')='description']/@content")

	heading := ""
	for _, node := range htmlquery.Find(body, ".//h1") {
		if !micrositeNodeVisible(node, body, context, hidden) || micrositeSharedChrome(node, body) {
			continue
		}
		value := micrositeVisibleText(node, body, context, hidden)
		if value != "" && len([]rune(value)) <= 300 && !numericOnly.MatchString(value) {
			heading = value
			break
		}
	}

	intro := ""
	for _, node := range htmlquery.Find(body,
		".//*["+classPredicate("cover-intro")+" or "+classPredicate("cover-subtitle")+"]") {
		if !micrositeNodeVisible(node, body, context, hidden) || micrositeSharedChrome(node, body) {
			continue
		}
		if value := micrositeVisibleText(node, body, context, hidden); value != "" {
			if runes := []rune(value); len(runes) > 1000 {
				value = string(runes[:1000])
			}
			intro = value
			break
		}
	}

	var items []map[string]any
	seen := map[string]bool{}
	for _, anchor := range htmlquery.Find(body, ".//a[@href]") {
		if !micrositeNodeVisible(anchor, body, context, hidden) ||
			micrositeSharedChrome(anchor, body) {
			continue
		}
		link := validateArticleURL(absoluteURL(pageURL, attr(anchor, "href")))
		if link == "" || seen[link] {
			continue
		}
		seen[link] = true
		label := micrositeVisibleText(anchor, body, context, hidden)
		item := map[string]any{
			"title":     emptyToNil(label),
			"url":       link,
			"image":     nil,
			"summary":   nil,
			"badges":    []any{},
			"item_kind": "article",
		}
		if label == "" {
			item["title_missing"] = true
		}
		items = append(items, item)
	}

	var modules []snapshotModule
	if len(items) > 0 {
		modules = append(modules, snapshotModule{
			Key: "datanews-interactive.article-links", Name: "SSR 文章链接",
			Lane: "main", Order: 0, State: "server_rendered", Items: items,
		})
	}
	result := attachClickConsumers(directoryResult(modules))
	for _, item := range items {
		item["access"] = "ssr_directory_candidate"
		item["rendered_visibility_not_verified"] = true
	}

	reportedTitle := emptyToNil(title)
	if reportedTitle == nil {
		reportedTitle = directoryTitle
	}
	count := func(xpath string) int { return len(htmlquery.Find(doc, xpath)) }
	result["kind"] = "datanews_interactive"
	result["directory_title"] = directoryTitle
	result["title"] = reportedTitle
	result["heading_candidate"] = emptyToNil(heading)
	result["description"] = emptyToNil(description)
	result["public_intro"] = emptyToNil(intro)
	result["paywall_present"] = paywallPresent
	result["paywall_content_not_extracted"] = paywallPresent
	result["javascript_executed"] = false
	result["rendered_visibility_verified"] = false
	result["external_stylesheets_applied"] = false
	result["complete_listing_verified"] = false
	result["interactive_content_not_rendered"] = true
	result["external_dependencies_not_fetched"] = true
	result["scripts_count"] = count("//script")
	result["scripts_not_executed"] = true
	result["forms_count"] = count("//form")
	result["forms_not_submitted"] = true
	result["iframes_count"] = count("//iframe")
	result["iframes_not_loaded"] = true
	result["canvas_count"] = count("//canvas")
	result["svg_count"] = count("//svg")
	result["linked_pages_not_fetched"] = true
	result["shared_navigation_not_included"] = true
	return result, nil
}

// DatanewsInteractive reads one 数字说 interactive project.
//
// The project must be one the 数字说 directory or front page currently lists.
// That is not ceremony: these are one-off pages with no shared template, and
// the directory card is the only evidence that a given url is one of them.
func (c *Client) DatanewsInteractive(ctx context.Context, pageURL string) (map[string]any, error) {
	canonical := strings.TrimSpace(pageURL)
	if !datanewsInteractiveEntrypoints[canonical] {
		if datanewsTopicInteractiveURL(canonical, canonical) != canonical {
			return nil, invalid("datanews-interactive reads a 数字说 interactive project " +
				"listed by https://datanews.caixin.com/datatopic/")
		}
	}

	title, discoveredFrom, err := c.datanewsInteractiveDiscovery(ctx, canonical)
	if err != nil {
		return nil, err
	}

	target := datanewsInteractiveTarget(canonical)
	raw, err := c.do(ctx, requestSpec{
		Method:    http.MethodGet,
		URL:       target,
		Headers:   map[string]string{"Accept": "text/html,application/xhtml+xml"},
		Anonymous: true,
	})
	if err != nil {
		return nil, err
	}
	doc, err := xhtml.Parse(strings.NewReader(string(raw)))
	if err != nil {
		return nil, &APIError{Message: "could not parse the interactive project page"}
	}
	result, err := datanewsInteractiveDocument(doc, target, title)
	if err != nil {
		return nil, err
	}
	result["source_mode"] = "server_html"
	result["source"] = map[string]any{
		"requested_url":         pageURL,
		"canonical_url":         canonical,
		"normalized_target_url": target,
		"final_url":             target,
		"discovered_from":       discoveredFrom,
		"fetched_at":            time.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	result["session_used"] = false
	return result, nil
}

// datanewsInteractiveDiscovery finds the card that lists this project, and the
// page that carried it.
func (c *Client) datanewsInteractiveDiscovery(
	ctx context.Context,
	canonical string,
) (title any, discoveredFrom string, err error) {
	const directory = "https://datanews.caixin.com/datatopic/"
	parent, err := c.PublicDirectory(ctx, directory)
	if err != nil {
		return nil, "", err
	}
	for _, raw := range asList(parent["modules"]) {
		module, _ := raw.(map[string]any)
		for _, entry := range asList(module["items"]) {
			item, ok := entry.(map[string]any)
			if !ok || item["item_kind"] != "interactive_directory" ||
				asString(item["url"]) != canonical {
				continue
			}
			return emptyToNil(asString(item["title"])), directory, nil
		}
	}

	// Not in the directory: the front page also links a few of them directly.
	const front = "https://datanews.caixin.com/"
	snapshot, err := c.Snapshot(ctx, front)
	if err != nil {
		return nil, "", err
	}
	for _, raw := range asList(snapshot["modules"]) {
		module, _ := raw.(map[string]any)
		for _, entry := range asList(module["items"]) {
			item, ok := entry.(map[string]any)
			if !ok || asString(item["url"]) != canonical {
				continue
			}
			return emptyToNil(asString(item["title"])), front, nil
		}
	}
	return nil, "", &APIError{
		StatusCode: 404,
		Message:    "that interactive project is not listed by 数字说 right now",
	}
}
