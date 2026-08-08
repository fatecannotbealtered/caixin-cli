package caixin

import (
	"context"
	"net/http"
	neturl "net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/antchfx/htmlquery"
	xhtml "golang.org/x/net/html"
)

// A 视频 channel directory uses whichever of four card templates its editors
// picked, so the extractor reads all four and reports which ones it found in
// `template_classes`. A page whose template is none of them is refused rather
// than half-read -- but only when it actually has visible articles, so an
// empty directory reads as empty instead of as a failure.

// videoRootURL is the 视频 channel front, the page that lists its directories.
const videoRootURL = "https://video.caixin.com/"

// videoSectionCardClasses are the container classes the directories use.
// "bussiness_list" is upstream's spelling, not a typo here.
//
//nolint:misspell // the class name is the site's own
var videoSectionCardClasses = []string{"topiclist", "subjectlist", "bussiness_list", "poster-list"}

// videoSectionCardParents are the classes that mark one card.
var videoSectionCardParents = map[string]bool{
	"poster-item": true, "business_box": true, "box": true,
}

// videoContinuationControl is the "load more" hook the directories carry.
var videoContinuationControl = regexp.MustCompile(
	`^\s*loadMoreNewses\(\s*0\s*,\s*(\d{3}|\d{9})\s*,\s*1\s*,\s*(\d{1,2})\s*,\s*this\s*\)\s*;?\s*$`)

// videoSectionBadges are the access labels a card may carry.
var videoSectionBadges = map[string]bool{
	"收费文章": true, "限时免费": true, "免费阅读": true, "注册阅读": true,
}

// videoSectionContinuation reads the page's own pagination contract.
//
// It is reported, not followed: the caller asks for page 2 explicitly, and the
// contract is what makes that request reproducible.
func videoSectionContinuation(doc *xhtml.Node) (map[string]any, error) {
	var controls []string
	for _, node := range htmlquery.Find(doc, "//*[@id='moreArticle']//a[@onclick]") {
		controls = append(controls, attr(node, "onclick"))
	}
	if len(controls) == 0 {
		return nil, nil
	}
	if len(controls) > 1 {
		return nil, &APIError{Message: "the video directory carries more than one load-more control"}
	}
	match := videoContinuationControl.FindStringSubmatch(controls[0])
	if match == nil {
		return nil, &APIError{Message: "the video directory's load-more control has an unknown shape"}
	}
	identifier, _ := strconv.Atoi(match[1])
	size, _ := strconv.Atoi(match[2])
	if size < 1 || size > 50 {
		return nil, &APIError{Message: "the video directory's page size is outside the safe range"}
	}
	parameter := "channel"
	if len(match[1]) == 9 {
		parameter = "subject"
	}
	return map[string]any{
		"parameter":  parameter,
		"identifier": identifier,
		"page_size":  size,
		"evidence":   "server_rendered_loadMoreNewses_control",
	}, nil
}

// videoSectionItem builds one card from whichever template holds it.
func videoSectionItem(anchor *xhtml.Node, pageURL string) map[string]any {
	link := validateArticleURL(absoluteURL(pageURL, attr(anchor, "href")))
	if link == "" || serverSourceState(anchor) != "visible" {
		return nil
	}
	title := nodeText(anchor)
	if title == "" {
		return nil
	}
	if len([]rune(title)) > 300 {
		title = string([]rune(title)[:300])
	}

	container := firstElement(anchor, "ancestor::div["+classPredicate("topic")+"][1]")
	var card *xhtml.Node
	for ancestor := anchor.Parent; ancestor != nil; ancestor = ancestor.Parent {
		if ancestor.Data == "li" {
			card = ancestor
			break
		}
		matched := false
		for _, class := range strings.Fields(attr(ancestor, "class")) {
			if videoSectionCardParents[class] {
				matched = true
			}
		}
		if matched {
			card = ancestor
			break
		}
	}
	if container == nil {
		if card != nil {
			container = card
		} else {
			container = anchor
		}
	}
	imageRoot := card
	if imageRoot == nil {
		imageRoot = container
	}
	image := ""
	if imageNode := firstElement(imageRoot, ".//img[1]"); imageNode != nil {
		source := attr(imageNode, "data-src")
		if source == "" {
			source = attr(imageNode, "src")
		}
		image = imageURL(pageURL, source)
	}

	// The summary is the first paragraph that is neither the kicker nor the
	// timestamp row; both live in <p> as well.
	summary := firstXPathText(container,
		".//p[not("+classPredicate("littletitle")+") and not(.//span["+
			classPredicate("datetime")+" or "+classPredicate("date")+" or "+
			classPredicate("time")+" or "+classPredicate("laiyan")+"])][1]")
	category := firstXPathText(container,
		".//span["+classPredicate("laiyan")+"][1]",
		".//em["+classPredicate("flagbg")+"][1]")
	published := firstXPathText(container, ".//span["+classPredicate("datetime")+"][1]")
	if published == "" {
		date := firstXPathText(container, ".//span["+classPredicate("date")+"][1]")
		clock := firstXPathText(container, ".//span["+classPredicate("time")+"][1]")
		published = strings.Join(nonEmpty(date, clock), " ")
	}

	var badges []string
	seen := map[string]bool{}
	for _, node := range htmlquery.Find(container, ".//*[@title]") {
		badge := plainText(attr(node, "title"))
		if videoSectionBadges[badge] && !seen[badge] {
			seen[badge] = true
			badges = append(badges, badge)
		}
	}
	sort.Strings(badges)
	labels := make([]any, 0, len(badges))
	for _, badge := range badges {
		labels = append(labels, badge)
	}

	kind := "article"
	if hostOf(link) == "video.caixin.com" {
		kind = "video_article"
	}
	return map[string]any{
		"title":        title,
		"url":          link,
		"image":        emptyToNil(image),
		"summary":      emptyToNil(summary),
		"published_at": emptyToNil(published),
		"category":     emptyToNil(category),
		"badges":       labels,
		"item_kind":    kind,
		"consumer":     ClassifyURL(link).AsEmbeddedMap(),
	}
}

// videoSectionDocument extracts one 视频 channel directory page.
func videoSectionDocument(doc *xhtml.Node, pageURL string) (map[string]any, error) {
	var selectors []string
	for _, class := range videoSectionCardClasses {
		selectors = append(selectors, classXPath(class))
	}
	roots := findInDocumentOrder(doc, selectors...)

	var items []map[string]any
	templates := []any{}
	seen := map[string]bool{}
	seenTemplate := map[string]bool{}
	for _, root := range roots {
		if classes := strings.Join(strings.Fields(attr(root, "class")), " "); classes != "" &&
			!seenTemplate[classes] {
			seenTemplate[classes] = true
			templates = append(templates, classes)
		}
		for _, anchor := range htmlquery.Find(root, ".//a[@href]") {
			item := videoSectionItem(anchor, pageURL)
			if item == nil || seen[asString(item["url"])] {
				continue
			}
			seen[asString(item["url"])] = true
			items = append(items, item)
		}
	}
	if len(roots) == 0 {
		// No known template. If the page nonetheless shows articles, the layout
		// changed and reporting an empty directory would be a lie.
		for _, anchor := range htmlquery.Find(doc, "//a[@href]") {
			if serverSourceState(anchor) != "visible" {
				continue
			}
			if validateArticleURL(absoluteURL(pageURL, attr(anchor, "href"))) != "" {
				return nil, &APIError{
					Message: "the video directory uses a template this build has not measured",
				}
			}
		}
	}

	var modules []snapshotModule
	if len(items) > 0 {
		modules = append(modules, snapshotModule{Key: "video.section.articles", Name: "频道资讯",
			Lane: "main", Order: 0, State: "visible", Items: items})
	}
	result := directoryResult(modules)
	result["directory_only"] = true
	result["empty_visible_directory"] = len(items) == 0
	result["template_classes"] = templates
	result["media_streams_not_fetched"] = true
	result["media_not_downloaded"] = true
	return result, nil
}

// VideoSection reads one 视频 channel directory.
func (c *Client) VideoSection(ctx context.Context, pageURL string, page int) (map[string]any, error) {
	if page < 1 {
		return nil, invalid("video-section page must be positive")
	}
	canonical := videoSectionURL(strings.TrimSpace(pageURL))
	if canonical == "" {
		return nil, invalid("video-section reads a Caixin 视频 channel directory, " +
			"for example https://video.caixin.com/dr/")
	}

	// The directory must be one the channel front currently links. That is the
	// discovery rule the route verdict advertises, and it is also where the
	// directory's own title comes from.
	root, err := c.Snapshot(ctx, videoRootURL)
	if err != nil {
		return nil, err
	}
	title := ""
	found := false
	for _, raw := range asList(root["modules"]) {
		module, ok := raw.(map[string]any)
		if !ok || module["key"] != "video.navigation" {
			continue
		}
		for _, entry := range asList(module["items"]) {
			item, ok := entry.(map[string]any)
			if !ok || item["item_kind"] != "section_navigation" ||
				asString(item["url"]) != canonical {
				continue
			}
			title, found = asString(item["title"]), true
		}
	}
	if !found {
		return nil, &APIError{
			StatusCode: 404,
			Message:    "that video directory is not linked from " + videoRootURL + " right now",
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
		return nil, &APIError{Message: "could not parse the video directory page"}
	}
	contract, err := videoSectionContinuation(doc)
	if err != nil {
		return nil, err
	}

	source := map[string]any{
		"requested_url":        pageURL,
		"canonical_url":        canonical,
		"final_url":            canonical,
		"discovery_source_url": videoRootURL,
		"fetched_at":           time.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if page > 1 {
		return c.videoSectionPage(ctx, canonical, source, title, contract, page)
	}

	result, err := videoSectionDocument(doc, canonical)
	if err != nil {
		return nil, err
	}
	var nextPage any
	if contract != nil {
		nextPage = 2
	}
	result["source_mode"] = "server_html"
	result["source"] = source
	result["page_kind"] = "video-section"
	result["title"] = emptyToNil(title)
	result["page"] = 1
	result["session_used"] = false
	result["javascript_executed"] = false
	result["rendered_visibility_verified"] = false
	result["external_stylesheets_applied"] = false
	result["complete_listing_verified"] = false
	result["pagination"] = map[string]any{
		"control_present":  contract != nil,
		"supported":        contract != nil,
		"explicit_only":    true,
		"automatic_follow": false,
		"next_page":        nextPage,
		"contract":         contract,
	}
	return result, nil
}

// videoSectionPage fetches one continuation page of a 视频 directory.
func (c *Client) videoSectionPage(
	ctx context.Context,
	canonical string,
	source map[string]any,
	title string,
	contract map[string]any,
	page int,
) (map[string]any, error) {
	if contract == nil {
		return nil, &APIError{
			Message: "this video directory publishes no verified continuation contract",
		}
	}
	count, _ := safeInt(contract["page_size"])
	identifier, _ := safeInt(contract["identifier"])
	start := (page - 1) * count

	value, err := c.requestJSON(ctx, requestSpec{
		Method: http.MethodGet, URL: TopicDirectoryApi,
		Query: neturl.Values{
			asString(contract["parameter"]): {strconv.Itoa(identifier)},
			"start":                         {strconv.Itoa(start)},
			"count":                         {strconv.Itoa(count)},
			"picdim":                        {"_266_177"},
		},
		Headers: map[string]string{
			"User-Agent": TopicsCompatUserAgent,
			"Origin":     "https://video.caixin.com",
			"Referer":    canonical,
		},
		Anonymous: true,
	})
	if err != nil {
		return nil, err
	}
	rows, reportedMaxes, upstreamVersion, err := gatewayRows(value, start, count, "video directory")
	if err != nil {
		return nil, err
	}

	options := gatewayItemOptions{IncludeSummary: true, MaxImages: 1, BadgeMode: "home"}
	var items []map[string]any
	for _, row := range rows {
		main := gatewayItem(row, canonical, options)
		var parent any
		if main != nil {
			main["lane"] = "main"
			main["consumer"] = ClassifyURL(asString(main["url"])).AsEmbeddedMap()
			parent = main["url"]
			items = append(items, main)
		}
		fields, ok := row.(map[string]any)
		if !ok {
			continue
		}
		relations, _ := fields["relationNews"].([]any)
		for _, relation := range relations {
			related := gatewayItem(relation, canonical, options)
			if related == nil {
				continue
			}
			related["lane"] = "related"
			related["parent_url"] = parent
			related["consumer"] = ClassifyURL(asString(related["url"])).AsEmbeddedMap()
			items = append(items, related)
		}
	}

	var modules []snapshotModule
	modules = append(modules, snapshotModule{Key: "video.section.continuation", Name: "频道续载",
		Lane: "main", Order: 0, State: "visible", Items: items})
	result := directoryResult(modules)
	result["source_mode"] = "server_html_discovery_plus_json_api"
	source["api_url"] = TopicDirectoryApi
	result["source"] = source
	result["page_kind"] = "video-section"
	result["title"] = emptyToNil(title)
	result["page"] = page
	result["session_used"] = false
	result["javascript_executed"] = false
	result["rendered_visibility_verified"] = false
	result["external_stylesheets_applied"] = false
	result["complete_listing_verified"] = false
	result["reported_maxes"] = reportedMaxes
	result["upstream_version"] = upstreamVersion
	result["pagination"] = map[string]any{
		"page":             page,
		"page_size":        count,
		"start":            start,
		"explicit_only":    true,
		"automatic_follow": false,
		"contract":         contract,
	}
	return result, nil
}
