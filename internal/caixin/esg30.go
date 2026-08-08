package caixin

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/antchfx/htmlquery"
	xhtml "golang.org/x/net/html"
)

// ESG30Subdirectory reads one sub-index of the ESG30 front.
//
// It fetches the parent directory first and refuses to continue unless the
// requested url is actually listed there. That is the discovery rule the route
// verdict advertises with `discovery_required`: a caller may not deep-link past
// a directory into a page the site is not currently publishing, and enforcing
// it here is what makes the flag mean something.
func (c *Client) ESG30Subdirectory(ctx context.Context, pageURL string) (map[string]any, error) {
	canonical := esg30SubdirectoryCanonical(pageURL)
	if canonical == "" {
		return nil, invalid("esg30-subdirectory only accepts the ESG30 sub-index entry points; " +
			"read `public-directory https://index.caixin.com/esg30/` first")
	}
	const parentURL = "https://index.caixin.com/esg30/"

	parent, err := c.PublicDirectory(ctx, parentURL)
	if err != nil {
		return nil, err
	}
	var listed map[string]any
	for _, raw := range asList(parent["subdirectories"]) {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if asString(entry["url"]) == canonical {
			listed = entry
			break
		}
	}
	if listed == nil {
		return nil, &APIError{
			StatusCode: 404,
			Message: "that sub-index is not in the ESG30 directory right now; " +
				"read `public-directory " + parentURL + "` for what is",
		}
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
		return nil, &APIError{Message: "could not parse the ESG30 sub-index"}
	}

	result, err := esg30NewsDocument(doc, canonical)
	if err != nil {
		return nil, err
	}
	result["title"] = emptyToNil(firstXPathText(doc, "//title"))
	result["source_mode"] = "server_html"
	result["session_used"] = false
	result["section_profile"] = map[string]any{
		"title": listed["title"],
		"url":   canonical,
	}
	result["source"] = map[string]any{
		"requested_url":   pageURL,
		"canonical_url":   canonical,
		"final_url":       canonical,
		"discovered_from": parentURL,
		"fetched_at":      time.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	return attachClickConsumers(result), nil
}

// esg30SubdirectoryCanonical normalizes a sub-index url.
func esg30SubdirectoryCanonical(raw string) string {
	route := ClassifyURL(raw)
	if route.Adapter != "esg30-subdirectory" {
		return ""
	}
	return route.CanonicalURL
}

// esg30NewsDocument extracts the article list from a sub-index.
func esg30NewsDocument(doc *xhtml.Node, pageURL string) (map[string]any, error) {
	root := firstElement(doc, "//div["+classPredicate("news-list")+"][1]")
	if root == nil {
		return nil, &APIError{Message: "the ESG30 sub-index is missing its news-list"}
	}

	var items []map[string]any
	for _, node := range htmlquery.Find(root, ".//li") {
		// Server-declared visibility only: a card the markup hides is reported
		// as absent rather than silently included.
		if serverSourceState(node) != "visible" {
			continue
		}
		anchor := firstElement(node, ".//p["+classPredicate("news-title")+"]/a[@href][1]")
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
		badges := []any{}
		for _, badge := range htmlquery.Find(node, ".//*["+classPredicate("news-tag")+"]/span") {
			if text := nodeText(badge); text != "" {
				badges = append(badges, text)
			}
		}
		items = append(items, map[string]any{
			"title":                            title,
			"url":                              link,
			"image":                            nil,
			"summary":                          nil,
			"published_at":                     emptyToNil(firstXPathText(node, ".//*["+classPredicate("news-date")+"][1]")),
			"badges":                           badges,
			"item_kind":                        "article",
			"rendered_visibility_not_verified": true,
		})
	}

	var modules []snapshotModule
	if len(items) > 0 {
		modules = append(modules, snapshotModule{
			Key: "esg30-subdirectory.news", Name: "ESG30 资讯",
			Lane: "main", Order: 0, State: "server_rendered", Items: items,
		})
	}
	result := directoryResult(modules)
	for _, item := range items {
		item["access"] = "ssr_directory_candidate"
	}
	result["kind"] = "esg30_news"
	result["rendered_visibility_not_verified"] = true
	result["external_stylesheets_not_fetched"] = true
	result["pagination"] = "none_static_html"
	result["scripts_not_executed"] = true
	result["linked_pages_not_fetched"] = true
	return result, nil
}
