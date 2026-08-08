package caixin

import (
	"context"
	"net/http"
	neturl "net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/antchfx/htmlquery"
	xhtml "golang.org/x/net/html"
)

// A 观点 columnist page is read only after something listed the columnist, and
// the caller says which listing that was. That is not bureaucracy: the three
// listings describe the same person differently -- the front page gives a bio,
// the roster gives a portrait, the 名家 cards give only a name -- and the
// payload reports which description it is echoing back.

// reportedOpinionAuthorID reads an id the page printed into a script.
//
// It is reported as `reported_*` because it is the page's own claim: nothing
// here has checked that the id belongs to this columnist.
func reportedOpinionAuthorID(doc *xhtml.Node, variable string) any {
	pattern := regexp.MustCompile(`\b(?:const|let|var)\s+` + regexp.QuoteMeta(variable) +
		`\s*=\s*['"]([1-9]\d{0,19})['"]`)
	match := pattern.FindStringSubmatch(inlineScriptSource(doc))
	if match == nil {
		return nil
	}
	number, err := strconv.Atoi(match[1])
	if err != nil {
		return nil
	}
	return number
}

// opinionAuthorArticleURL accepts an article on the columnist's own host.
func opinionAuthorArticleURL(base, value string) string {
	raw := absoluteURL(base, value)
	if raw == "" {
		return ""
	}
	if parsed, err := neturl.Parse(raw); err != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	link := validateArticleURL(raw)
	if link == "" || hostOf(link) != "opinion.caixin.com" {
		return ""
	}
	return link
}

// opinionAuthorDocument reads one columnist page's first screen.
func opinionAuthorDocument(doc *xhtml.Node, pageURL, authorKey string) (map[string]any, error) {
	root := firstElement(doc, "//div["+classPredicate("mainContent")+"]")
	if root == nil {
		return nil, &APIError{Message: "the 观点 columnist page is missing its mainContent"}
	}
	newsRoot := firstElement(root, ".//div["+classPredicate("NewsList")+"]")
	profileRoot := firstElement(root, ".//div["+classPredicate("rightWriterBox")+"]")
	if newsRoot == nil || profileRoot == nil {
		return nil, &APIError{Message: "the 观点 columnist page is missing its profile or its list"}
	}

	// The profile block links the columnist it describes. When that link is not
	// this columnist, the page is somebody else's and the read stops.
	profileAnchor := firstElement(profileRoot,
		".//a[@href][.//*["+classPredicate("writerName")+"]][1]")
	profileURL, profileKey := "", ""
	if profileAnchor != nil {
		profileURL, profileKey = opinionAuthorURL(absoluteURL(pageURL, attr(profileAnchor, "href")))
	}
	if profileURL != pageURL || profileKey != authorKey {
		return nil, &APIError{Message: "the 观点 columnist page describes a different columnist"}
	}

	profileImage := ""
	if node := firstElement(profileRoot,
		".//img["+classPredicate("writerAvatar")+"][1]"); node != nil {
		profileImage = legacyOpinionURL(pageURL, attr(node, "src"),
			"www.caixin.com", opinionAuthorPortraitPath)
	}
	profile := map[string]any{
		"name":  emptyToNil(firstXPathText(profileRoot, ".//*["+classPredicate("writerName")+"][1]")),
		"bio":   emptyToNil(firstXPathText(profileRoot, ".//*["+classPredicate("writerDescription")+"][1]")),
		"image": emptyToNil(profileImage),
	}

	var articles []map[string]any
	for _, node := range htmlquery.Find(newsRoot, "./a["+classPredicate("newsItem")+"][@href]") {
		link := opinionAuthorArticleURL(pageURL, attr(node, "href"))
		titleNode := firstElement(node, ".//*["+classPredicate("newsRightConTitle")+"][1]")
		title := ""
		if titleNode != nil {
			title = directText(titleNode)
			if title == "" {
				title = nodeText(titleNode)
			}
		}
		if link == "" || title == "" {
			continue
		}
		image := ""
		if imageNode := firstElement(node,
			".//img["+classPredicate("newsLeftPic")+"][1]"); imageNode != nil {
			source := attr(imageNode, "data-src")
			if source == "" {
				source = attr(imageNode, "src")
			}
			image = cultureImageURL(pageURL, source)
		}
		badges := []any{}
		for _, badgeNode := range htmlquery.Find(node, ".//*[contains(@class,'icon_')]") {
			badge := attr(badgeNode, "title")
			if badge == "" {
				badge = nodeText(badgeNode)
			}
			if badge != "" && !containsAny(badges, badge) {
				badges = append(badges, badge)
			}
		}
		articles = append(articles, map[string]any{
			"title":        title,
			"url":          link,
			"image":        emptyToNil(image),
			"summary":      emptyToNil(firstXPathText(node, ".//*["+classPredicate("newsRightConText")+"][1]")),
			"published_at": emptyToNil(firstXPathText(node, ".//*["+classPredicate("newsRightConTime")+"]/span[1]")),
			"badges":       badges,
			"item_kind":    "article",
		})
	}

	var modules []snapshotModule
	if len(articles) > 0 {
		modules = append(modules, snapshotModule{Key: "opinion-author.articles", Name: "作者文章",
			Lane: "main", Order: 0, State: "visible", Items: articles})
	}
	result := attachClickConsumers(directoryResult(modules))
	result["profile"] = profile
	result["reported_author_id"] = reportedOpinionAuthorID(doc, "authorId")
	result["reported_subject_id"] = reportedOpinionAuthorID(doc, "subjectId")
	result["pagination"] = "ssr_first_screen_only"
	result["load_more_available"] = len(htmlquery.Find(doc, "//*[@id='loadmore']")) > 0
	result["load_more_not_called"] = true
	result["author_search_not_called"] = true
	result["article_details_not_fetched"] = true
	result["linked_pages_not_fetched"] = true
	result["scripts_not_executed"] = true
	result["session_used"] = false
	return result, nil
}

// OpinionAuthor reads one 观点 columnist page.
func (c *Client) OpinionAuthor(
	ctx context.Context,
	pageURL string,
	page int,
	discoverySource string,
	directoryPage int,
) (map[string]any, error) {
	if page < 1 {
		return nil, invalid("opinion-author page must be positive")
	}
	switch discoverySource {
	case "homepage", "author-directory", "columns":
	default:
		return nil, invalid("opinion-author discovery source must be homepage, " +
			"author-directory, or columns")
	}
	if discoverySource == "author-directory" {
		if directoryPage < 1 {
			return nil, invalid("opinion-author needs the directory page the author was found on")
		}
	} else if directoryPage > 0 {
		return nil, invalid("opinion-author takes a directory page only with --discovery-source author-directory")
	}
	canonical, authorKey := opinionAuthorURL(strings.TrimSpace(pageURL))
	if canonical == "" {
		return nil, invalid("opinion-author reads a Caixin 观点 columnist page, " +
			"for example https://opinion.caixin.com/wuqianli_mjxx/")
	}

	card, discoveredFrom, err := c.opinionAuthorDiscovery(
		ctx, canonical, discoverySource, directoryPage)
	if err != nil {
		return nil, err
	}

	doc, err := c.fetchHTML(ctx, canonical, "could not parse the 观点 columnist page")
	if err != nil {
		return nil, err
	}
	details, err := opinionAuthorDocument(doc, canonical, authorKey)
	if err != nil {
		return nil, err
	}

	// The directory's description of this columnist is reported alongside the
	// page's own, because the two can disagree and neither is authoritative.
	common := map[string]any{
		"javascript_executed":          false,
		"rendered_visibility_verified": false,
		"external_stylesheets_applied": false,
		"complete_listing_verified":    false,
		"author_key":                   authorKey,
		"title":                        emptyToNil(firstXPathText(doc, "//title")),
		"directory_profile": map[string]any{
			"name":  card["title"],
			"url":   canonical,
			"image": card["image"],
			"bio":   card["summary"],
		},
	}

	if page == 1 {
		result := details
		for key, value := range common {
			result[key] = value
		}
		result["source"] = map[string]any{
			"requested_url":   pageURL,
			"final_url":       canonical,
			"discovered_from": discoveredFrom,
			"fetched_at":      time.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
		}
		return result, nil
	}

	subject, hasSubject := details["reported_subject_id"].(int)
	if !hasSubject {
		return nil, &APIError{Message: "the columnist page prints no subject id to continue from"}
	}
	const count = 20
	start := count + (page-2)*count
	value, err := c.requestJSON(ctx, requestSpec{
		Method: http.MethodGet, URL: TopicDirectoryApi,
		Query: neturl.Values{
			"subject": {strconv.Itoa(subject)},
			"start":   {strconv.Itoa(start)},
			"count":   {strconv.Itoa(count)},
			"picdim":  {"_266_177"},
			"type":    {"0"},
		},
		Headers: map[string]string{
			"User-Agent": TopicsCompatUserAgent,
			"Origin":     "https://opinion.caixin.com",
			"Referer":    canonical,
		},
		Anonymous: true,
	})
	if err != nil {
		return nil, err
	}
	rows, reportedMaxes, upstreamVersion, err := gatewayRows(value, start, count, "columnist")
	if err != nil {
		return nil, err
	}
	var items []map[string]any
	for _, row := range rows {
		if item := gatewayItem(row, canonical, gatewayItemOptions{
			ExpectedHost: "opinion.caixin.com", IncludeSummary: true,
			MaxImages: 1, BadgeMode: "opinion",
		}); item != nil {
			items = append(items, item)
		}
	}

	result := directoryResult([]snapshotModule{{
		Key: "opinion-author.articles", Name: "作者续载文章",
		Lane: "main", Order: 0, State: "visible", Items: items,
	}})
	for key, value := range common {
		result[key] = value
	}
	result["source"] = map[string]any{
		"requested_url":   pageURL,
		"final_url":       canonical,
		"discovered_from": discoveredFrom,
		"api_url":         TopicDirectoryApi,
		"fetched_at":      time.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	result["profile"] = details["profile"]
	result["reported_author_id"] = details["reported_author_id"]
	result["reported_subject_id"] = subject
	result["page"] = page
	result["returned"] = len(items)
	result["reported_maxes"] = reportedMaxes
	result["upstream_version"] = upstreamVersion
	result["pagination"] = map[string]any{
		"page":               page,
		"page_size":          count,
		"start":              start,
		"discovered_subject": subject,
		"explicit_only":      true,
	}
	result["author_search_not_called"] = true
	result["session_used"] = false
	result["linked_pages_not_fetched"] = true
	result["scripts_not_executed"] = true
	return result, nil
}

// opinionAuthorDiscovery finds the card that named this columnist.
func (c *Client) opinionAuthorDiscovery(
	ctx context.Context,
	canonical, discoverySource string,
	directoryPage int,
) (map[string]any, string, error) {
	const front = opinionRootURL
	snapshot, err := c.Snapshot(ctx, front)
	if err != nil {
		return nil, "", err
	}
	for _, raw := range asList(snapshot["modules"]) {
		module, _ := raw.(map[string]any)
		roster := module["key"] == "opinion.authors"
		keys := []string{"author_url", "image_click_url"}
		if roster {
			keys = []string{"url"}
		}
		for _, entry := range asList(module["items"]) {
			item, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			for _, key := range keys {
				link, _ := opinionAuthorURL(asString(item[key]))
				if link != canonical {
					continue
				}
				card := map[string]any{"url": canonical, "image": item["image"]}
				if roster {
					// Only the roster block describes the person; elsewhere the
					// title belongs to an article, not to the columnist.
					card["title"] = item["title"]
					card["summary"] = item["summary"]
				}
				return card, front, nil
			}
		}
	}

	switch discoverySource {
	case "author-directory":
		directory, err := c.OpinionAuthorDirectory(ctx,
			opinionRootURL+"columns-test/", directoryPage)
		if err != nil {
			return nil, "", err
		}
		if card := findDirectoryCard(directory, canonical); card != nil {
			return card, opinionRootURL + "columns-test/?page=" + strconv.Itoa(directoryPage), nil
		}
	case "columns":
		columns, err := c.OpinionColumns(ctx, opinionRootURL+"columns/", 1)
		if err != nil {
			return nil, "", err
		}
		for _, raw := range asList(columns["modules"]) {
			module, _ := raw.(map[string]any)
			for _, entry := range asList(module["items"]) {
				item, ok := entry.(map[string]any)
				if !ok || asString(item["author_url"]) != canonical {
					continue
				}
				return map[string]any{
					"title":   item["author"],
					"url":     canonical,
					"image":   item["image"],
					"summary": nil,
				}, opinionRootURL + "columns/", nil
			}
		}
	}
	return nil, "", &APIError{
		StatusCode: 404,
		Message:    "that columnist is not in the discovery source you named right now",
	}
}
