package caixin

import (
	"context"
	"net/http"
	neturl "net/url"
	"regexp"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"
)

// A sponsored campaign page is read only after the directory that listed it.
// The rule is not ceremony: these pages have no shared template and no url
// shape of their own, so the card that links one is the only evidence it is a
// campaign page rather than an arbitrary path on the promote host.
//
// Which directory found it is reported too, because a page reached from the
// ESG30 index and the same page reached from the promote directory are two
// different claims about what it is.

var esg30ResourceAssetSuffix = regexp.MustCompile(`(?i)\.(gif|jpe?g|png|webp|svg|pdf)$`)
var esg30ResourceArticlePath = regexp.MustCompile(`^/20\d{2}-\d{2}-\d{2}/\d{1,20}\.html$`)
var esg30ResourcePathShape = regexp.MustCompile(
	`^/[A-Za-z0-9._~%+@,-]+(/[A-Za-z0-9._~%+@,-]+)*/?$`)

// esg30ResourceCanonical accepts a campaign page on the promote host.
//
// A url that some other adapter already reads is refused here, so one page has
// one reader.
func esg30ResourceCanonical(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, " \t\n\r") {
		return ""
	}
	parsed, err := neturl.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return ""
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return ""
	}
	if strings.ToLower(parsed.Hostname()) != "promote.caixin.com" {
		return ""
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	if esg30ResourceAssetSuffix.MatchString(parsed.Path) ||
		esg30ResourceArticlePath.MatchString(parsed.Path) ||
		!esg30ResourcePathShape.MatchString(parsed.Path) {
		return ""
	}
	segments := strings.Split(parsed.Path, "/")
	for _, segment := range segments[1 : len(segments)-1] {
		if segment == "" || segment == "." || segment == ".." {
			return ""
		}
	}
	canonical := "https://promote.caixin.com" + parsed.Path
	if validateArticleURL(canonical) != "" {
		return ""
	}
	if url, _ := micrositeURL(canonical); url != "" {
		return ""
	}
	if publicDirectoryURL(canonical) != "" {
		return ""
	}
	return canonical
}

// ESG30Resource reads one sponsored campaign page.
func (c *Client) ESG30Resource(ctx context.Context, pageURL string) (map[string]any, error) {
	canonical := esg30ResourceCanonical(pageURL)
	if canonical == "" {
		return nil, invalid("esg30-resource reads a sponsored campaign page on " +
			"promote.caixin.com, discovered from the ESG30 or promote directory")
	}

	const esg30Root = "https://index.caixin.com/esg30/"
	discoveryRoot := esg30Root
	discoveredFrom := ""
	var card map[string]any

	// The promote directories are checked first: a page they list belongs to
	// them, and reporting it as an ESG30 resource would overstate its standing.
	for _, parentURL := range []string{
		"https://promote.caixin.com/", "https://promote.caixin.com/topic/",
	} {
		parent, err := c.PublicDirectory(ctx, parentURL)
		if err != nil {
			return nil, err
		}
		if item := findDirectoryCard(parent, canonical); item != nil {
			card, discoveredFrom, discoveryRoot = item, parentURL, parentURL
			break
		}
	}

	if card == nil {
		parent, err := c.PublicDirectory(ctx, esg30Root)
		if err != nil {
			return nil, err
		}
		for _, raw := range asList(parent["subdirectories"]) {
			subdirectory, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			link := asString(subdirectory["url"])
			if link == "" || link == "https://index.caixin.com/news/" {
				continue
			}
			child, err := c.ESG30Subdirectory(ctx, link)
			if err != nil {
				continue
			}
			if item := findDirectoryCard(child, canonical); item != nil {
				card, discoveredFrom = item, link
				break
			}
		}
	}
	if card == nil {
		return nil, &APIError{
			StatusCode: 404,
			Message:    "that resource is not on a card in the ESG30 or promote directories right now",
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
		return nil, &APIError{Message: "could not parse the campaign page"}
	}
	result, err := micrositeDocument(doc, canonical, "esg30_resource")
	if err != nil {
		return nil, err
	}

	result["kind"] = "esg30_resource"
	result["sponsored"] = true
	result["esg30_branded_resource"] = discoveryRoot == esg30Root
	result["promote_directory_resource"] = discoveryRoot != esg30Root
	// The campaign copy is the sponsor's, not Caixin's reporting; the page is
	// reported as a link surface and its body is left alone.
	result["resource_body_not_extracted"] = true
	if asString(result["title"]) == "" {
		result["title"] = card["title"]
		result["title_source"] = "parent_directory_card"
	}
	result["source_mode"] = "server_html"
	result["source"] = map[string]any{
		"requested_url":   pageURL,
		"canonical_url":   canonical,
		"final_url":       canonical,
		"discovered_from": discoveredFrom,
		"discovery_root":  discoveryRoot,
		"fetched_at":      time.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	// The card that led here is echoed back, so a caller can see the claim the
	// directory made about this page alongside the page itself.
	result["section_profile"] = map[string]any{
		"title": card["title"],
		"url":   canonical,
	}
	result["session_used"] = false
	return result, nil
}

// findDirectoryCard locates the card a directory published for one url.
func findDirectoryCard(directory map[string]any, canonical string) map[string]any {
	for _, raw := range asList(directory["modules"]) {
		module, _ := raw.(map[string]any)
		for _, entry := range asList(module["items"]) {
			item, ok := entry.(map[string]any)
			if ok && asString(item["url"]) == canonical {
				return item
			}
		}
	}
	return nil
}
