package caixin

import (
	"net/url"
	"strings"

	"github.com/antchfx/htmlquery"
	xhtml "golang.org/x/net/html"
)

// The 观点 channel front is a hub: most of what it links is another directory,
// not an article. Each one is named with the adapter that reads it, so a caller
// walking the channel knows which command to run next instead of guessing from
// the url.

// opinionDirectoryEntrypoints maps each measured 观点 directory to its adapter.
var opinionDirectoryEntrypoints = map[string]string{
	opinionRootURL + "columns-test/":   "opinion-author-directory",
	opinionRootURL + "columns/":        "opinion-columns",
	opinionRootURL + "upfront/":        "opinion-upfront",
	opinionRootURL + "editorial/":      "section-directory",
	opinionRootURL + "opinion_leader/": "section-directory",
	opinionRootURL + "opinion_video/":  "section-directory",
	opinionRootURL + "sxjx/":           "section-directory",
	opinionRootURL + "think_tank/":     "section-directory",
	opinionRootURL + "wyll/":           "section-directory",
}

// opinionDirectoryURL accepts one of the measured 观点 directories.
func opinionDirectoryURL(raw string) (canonical, adapter string) {
	if raw == "" || strings.ContainsAny(raw, " \t\n\r?#") {
		return "", ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil {
		return "", ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", ""
	}
	if strings.ToLower(parsed.Hostname()) != "opinion.caixin.com" {
		return "", ""
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ""
	}
	parsed.Scheme = "https"
	parsed.Host = "opinion.caixin.com"
	canonical = parsed.String()
	// `/columns/index.html` and `/columns/` are the same directory; fold the
	// alias so one destination is not offered twice.
	if alias := strings.TrimSuffix(canonical, "index.html"); alias != canonical {
		if _, ok := opinionDirectoryEntrypoints[alias]; ok {
			canonical = alias
		}
	}
	adapter = opinionDirectoryEntrypoints[canonical]
	if adapter == "" {
		return "", ""
	}
	return canonical, adapter
}

// opinionDirectoryNavigation lists the directories the 观点 front links.
func opinionDirectoryNavigation(doc *xhtml.Node, pageURL string) []map[string]any {
	items := []map[string]any{}
	seen := map[string]bool{}
	roots := append(
		htmlquery.Find(doc, classXPath("gdNav")),
		htmlquery.Find(doc, "//div["+classPredicate("indexMain")+"]")...)
	for _, root := range roots {
		for _, anchor := range htmlquery.Find(root, ".//a[@href]") {
			canonical, adapter := opinionDirectoryURL(absoluteURL(pageURL, attr(anchor, "href")))
			title := nodeText(anchor)
			if canonical == "" || title == "" || seen[canonical] {
				continue
			}
			seen[canonical] = true
			items = append(items, map[string]any{
				"title":               title,
				"url":                 canonical,
				"adapter":             adapter,
				"item_kind":           "opinion_directory",
				"content_not_fetched": true,
			})
		}
	}
	return items
}

// opinionDirectoryIsEntrypoint reports whether a url is a measured 观点
// directory.
func opinionDirectoryIsEntrypoint(raw string) bool {
	canonical, _ := opinionDirectoryURL(raw)
	return canonical != ""
}
