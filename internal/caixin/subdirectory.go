package caixin

import (
	"github.com/antchfx/htmlquery"
	xhtml "golang.org/x/net/html"
)

// A channel front also publishes its own sections. Listing them separately from
// the articles is what lets a caller walk the site: `subdirectories` is where
// to go next, `modules` is what is here now.
//
// A link is only offered when this page is one of the section's declared
// parents. Without that check the extractor would promote any section-shaped
// url the page happened to mention -- including ones it does not own.

// snapshotSubdirectories lists the sections a front page links.
//
// The second return says whether this page template publishes a section list at
// all; a page that does not gets no `subdirectories` key rather than an empty
// one, because "none here" and "not applicable" are different answers.
func snapshotSubdirectories(doc *xhtml.Node, pageURL, pageKey string) ([]any, bool) {
	var anchors []*xhtml.Node
	switch {
	case pageKey == "opinion":
		items := []any{}
		for _, entry := range opinionDirectoryNavigation(doc, pageURL) {
			if entry["adapter"] != "section-directory" {
				continue
			}
			items = append(items, map[string]any{
				"title":               entry["title"],
				"url":                 entry["url"],
				"item_kind":           "section_directory",
				"content_not_fetched": true,
			})
		}
		return items, true

	case snapshotChannelKeys[pageKey]:
		for _, root := range htmlquery.Find(doc, classXPath("indexMain")) {
			anchors = append(anchors, htmlquery.Find(root, ".//a[@href]")...)
		}
	case pageKey == "home":
		for _, root := range htmlquery.Find(doc, classXPath("ggbNav")) {
			anchors = append(anchors, htmlquery.Find(root, ".//a[@href]")...)
		}
	case pageKey == "weekly-index":
		anchors = htmlquery.Find(doc, "//a[@href]")
	default:
		return nil, false
	}

	items := []any{}
	seen := map[string]bool{}
	for _, anchor := range anchors {
		canonical := sectionDirectoryCandidate(absoluteURL(pageURL, attr(anchor, "href")))
		if canonical == "" || seen[canonical] {
			continue
		}
		if _, unsupported := sectionDirectoryUnsupported[canonical]; unsupported {
			continue
		}
		listed := false
		for _, parent := range sectionDirectoryParents(canonical) {
			if parent == pageURL {
				listed = true
			}
		}
		title := nodeText(anchor)
		if !listed || title == "" {
			continue
		}
		seen[canonical] = true
		items = append(items, map[string]any{
			"title":               title,
			"url":                 canonical,
			"item_kind":           "section_directory",
			"content_not_fetched": true,
		})
	}
	return items, true
}

// snapshotChannelKeys are the news channel fronts that carry a section bar.
var snapshotChannelKeys = map[string]bool{
	"economy": true, "finance": true, "companies": true,
	"china": true, "international": true, "science": true,
}
