package caixin

import (
	"net/url"
	"regexp"
	"strings"
	"time"
)

// A directory page lists what exists; it never fetches what it lists. Every
// result therefore carries `directory_only` and `article_details_not_fetched`,
// and each item says `content_not_fetched` -- an agent reading a title here has
// not read the article, and the payload must not let it think otherwise.

// articleDateInPath pulls the publication date out of an article url.
var articleDateInPath = regexp.MustCompile(`/(20\d{2}-\d{2}-\d{2})/\d+\.html$`)

// directoryResult wraps a module list in the shared directory envelope.
//
// It also computes `latest_article_date` and `stale_content`: a front page that
// has not moved in a month is usually a sign the extraction is reading a cached
// or fallback rendering, and saying so is cheaper than having a caller notice
// the dates by eye.
func directoryResult(modules []snapshotModule) map[string]any {
	var items []map[string]any
	for _, module := range modules {
		items = append(items, module.Items...)
	}
	unique := map[string]bool{}
	var dates []string
	for _, item := range items {
		item["access"] = "directory_visible"
		item["content_not_fetched"] = true
		link, _ := item["url"].(string)
		if link == "" {
			continue
		}
		unique[link] = true
		if parsed, err := url.Parse(link); err == nil {
			if match := articleDateInPath.FindStringSubmatch(parsed.Path); match != nil {
				dates = append(dates, match[1])
			}
		}
	}

	var latest any
	stale := false
	if len(dates) > 0 {
		newest := dates[0]
		for _, date := range dates[1:] {
			if date > newest {
				newest = date
			}
		}
		latest = newest
		if parsed, err := time.Parse("2006-01-02", newest); err == nil {
			stale = time.Since(parsed) > 30*24*time.Hour
		}
	}

	return map[string]any{
		"directory_only":               true,
		"article_details_not_fetched":  true,
		"javascript_executed":          false,
		"rendered_visibility_verified": false,
		"external_stylesheets_applied": false,
		"complete_listing_verified":    false,
		"modules":                      modulesAsList(modules),
		"modules_count":                len(modules),
		"items_count":                  len(items),
		"unique_urls_count":            len(unique),
		"latest_article_date":          latest,
		"stale_content":                stale,
	}
}

// cultureImageURL accepts the two image hosts the editorial pages publish to.
func cultureImageURL(base, value string) string {
	raw := absoluteURL(base, value)
	if raw == "" || strings.ContainsAny(raw, " \t\n\r") {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "img.caixin.com" && host != "image1.caixin.com" {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	if !imagePathPattern.MatchString(parsed.Path) {
		return ""
	}
	parsed.Scheme = "https"
	parsed.Host = host
	return parsed.String()
}

// articleURL accepts a canonical Caixin article link and nothing else.
//
// Directory pages mix article links with ads, microsites, and script hooks, so
// every candidate is checked against the article shape rather than assumed.
var articlePathPattern = regexp.MustCompile(`^/(20\d{2}-\d{2}-\d{2})/\d{1,20}\.html$`)

func articleURL(base, value string) string {
	raw := absoluteURL(base, value)
	if raw == "" || strings.ContainsAny(raw, " \t\n\r") {
		return ""
	}
	raw = upgradeCaixinScheme(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil {
		return ""
	}
	if parsed.Scheme != "https" || !caixinHost(parsed.Hostname()) {
		return ""
	}
	// ucwap is the mobile reader shell, not an article host. Accepting it here
	// would hand `article` a url it cannot parse, so callers that care about
	// that distinction handle it themselves.
	if strings.ToLower(parsed.Hostname()) == "ucwap.caixin.com" {
		return ""
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	// The `/m/` mobile spelling is the same article; fold it so one piece never
	// appears twice in a listing.
	if match := mobileArticleAlias.FindStringSubmatch(parsed.Path); match != nil {
		parsed.Path = match[1]
		return parsed.String()
	}
	if !articlePathPattern.MatchString(parsed.Path) {
		return ""
	}
	return parsed.String()
}
