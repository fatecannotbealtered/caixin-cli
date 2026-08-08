package caixin

import (
	"context"
	"net/http"
	neturl "net/url"
	"regexp"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"
)

// "Topic" is three different products wearing one word. deepview builds its
// page from a config document, key.caixin.com serves tabs from an entity API,
// and mappv5 is a static app page. The url says which, so the caller does not
// have to.
//
// All three list; none of them fetch an article body. `topic_listing_only`
// says that on the way out.

// deepviewAPI is the host that serves a deepview topic's data.
const deepviewAPI = "https://deepview-dynamic.caixin.com"

// keyTopicInfoURL is the entity service that describes a Key topic.
const keyTopicInfoURL = "https://entities.caixin.com/apientry/newTopic/topicInfo/v2"

// Topic reads one Caixin topic page under whichever surface publishes it.
func (c *Client) Topic(ctx context.Context, pageURL string) (map[string]any, error) {
	parsed, err := neturl.Parse(strings.TrimSpace(pageURL))
	if err != nil {
		return nil, invalid("topic reads a Caixin topic page, " +
			"for example https://key.caixin.com/topic/BQ02.000000368")
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "mappv5.caixin.com", "mappsv5.caixin.com", "m.app.caixin.com":
		// A capability gap, not an upstream failure: retrying will not help, so
		// it is reported as a rejected input with the alternative named.
		return nil, invalid("the app's static topic pages are not read by this build; " +
			"the same topic is usually published on key.caixin.com")
	case "key.caixin.com", "tag.caixin.com":
		return c.keyTopic(ctx, pageURL)
	default:
		return c.deepviewTopic(ctx, pageURL)
	}
}

// deepviewSuccess unwraps a deepview envelope.
func deepviewSuccess(value map[string]any, action string) (map[string]any, error) {
	if plainText(value["code"]) != "200" {
		return nil, &APIError{Message: action + " failed (code=" + plainText(value["code"]) + ")"}
	}
	return value, nil
}

// keyTopicSuccess unwraps a Key topic envelope.
func keyTopicSuccess(value map[string]any, action string) (map[string]any, error) {
	success, _ := value["success"].(bool)
	if !success || plainText(value["code"]) != "0" {
		return nil, &APIError{Message: action + " failed (code=" + plainText(value["code"]) + ")"}
	}
	return value, nil
}

// httpsOutputURL echoes an absolute https url, or "" when it is not one.
func httpsOutputURL(base string, value any) string {
	raw := absoluteURL(base, plainText(value))
	if raw == "" || strings.ContainsAny(raw, " \t\n\r") {
		return ""
	}
	parsed, err := neturl.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return ""
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return ""
	}
	return raw
}

var contentIDPattern = regexp.MustCompile(`^\d{1,20}$`)

// deepviewArticle normalizes one row of a deepview flow.
func deepviewArticle(row any) map[string]any {
	fields, ok := row.(map[string]any)
	if !ok {
		return nil
	}
	const base = "https://deepview.caixin.com/"
	link := caixinOutputURL(base, plainText(fields["url"]))
	title := plainText(fields["title"])
	if link == "" || title == "" {
		return nil
	}
	var contentID any
	if raw := plainText(fields["id"]); contentIDPattern.MatchString(raw) {
		contentID = raw
	}
	return map[string]any{
		"content_id":   contentID,
		"title":        title,
		"url":          link,
		"summary":      plainText(fields["subhead"]),
		"memo":         plainText(fields["memo"]),
		"image":        emptyToNil(httpsOutputURL(base, fields["pic"])),
		"published_at": plainText(fields["time"]),
		// The flag is what the API said, not a permission this client verified.
		"reported_vip_flag": plainText(fields["vip"]) == "1",
		"access":            "topic_listing_visible",
	}
}

// jsonBool reads a flag that arrives as a bool, a number, or a word.
func jsonBool(value any, fallback bool) bool {
	if value == nil {
		return fallback
	}
	if flag, ok := value.(bool); ok {
		return flag
	}
	switch strings.ToLower(strings.TrimSpace(plainText(value))) {
	case "1", "true", "yes":
		return true
	}
	return false
}

var (
	deepviewLabelID     = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
	deepviewRequestType = regexp.MustCompile(`^[A-Z0-9_]{1,32}$`)
	deepviewRequestCode = regexp.MustCompile(`^[A-Z0-9]+(\.[A-Z0-9]+)+$`)
	componentName       = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)
)

// deepviewTopic reads a deepview topic or event page.
//
// The page is assembled from a config document, so the extraction follows that
// config rather than the markup: each component becomes one section, and a
// component this build does not know is named in `unsupported_components`
// instead of being skipped silently.
func (c *Client) deepviewTopic(ctx context.Context, pageURL string) (map[string]any, error) {
	canonical := deepviewURL(strings.TrimSpace(pageURL))
	if canonical == "" {
		return nil, invalid("topic reads a Caixin topic page, " +
			"for example https://deepview.caixin.com/topic/BQ02.000008008.html")
	}
	match := deepviewPath.FindStringSubmatch(mustPath(canonical))
	topicType := strings.ToUpper(match[1])
	dataCode := match[2]

	raw, err := c.do(ctx, requestSpec{
		Method:  http.MethodGet,
		URL:     canonical,
		Headers: map[string]string{"Accept": "text/html,application/xhtml+xml"},
	})
	if err != nil {
		return nil, err
	}
	doc, err := xhtml.Parse(strings.NewReader(string(raw)))
	if err != nil {
		return nil, &APIError{Message: "could not parse the deepview topic page"}
	}

	headers := map[string]string{
		"Origin":  "https://deepview.caixin.com",
		"Referer": canonical,
	}
	lower := strings.ToLower(topicType)
	infoValue, err := c.requestJSON(ctx, requestSpec{
		Method:  http.MethodGet,
		URL:     deepviewAPI + "/api/topic/associated/" + lower + "/" + dataCode + "/info",
		Headers: headers,
	})
	if err != nil {
		return nil, err
	}
	if infoValue, err = deepviewSuccess(infoValue, "reading the deepview topic"); err != nil {
		return nil, err
	}
	configValue, err := c.requestJSON(ctx, requestSpec{
		Method:  http.MethodGet,
		URL:     deepviewAPI + "/api/topic/config/" + lower + "/" + dataCode,
		Headers: headers,
	})
	if err != nil {
		return nil, err
	}
	if configValue, err = deepviewSuccess(configValue, "reading the deepview topic layout"); err != nil {
		return nil, err
	}

	infoData, _ := infoValue["data"].(map[string]any)
	configData, _ := configValue["data"].(map[string]any)
	config, ok := configData["configJSON"].(map[string]any)
	if !ok {
		text, isText := configData["configJSON"].(string)
		if !isText {
			return nil, &APIError{Message: "the deepview topic layout carries no configJSON"}
		}
		if err := jsonUnmarshal([]byte(text), &config); err != nil {
			return nil, &APIError{Message: "the deepview topic layout was not valid JSON"}
		}
	}

	sections := []any{}
	unsupported := []any{}
	detailsNotFetched := false
	paywallComponents := 0
	noteUnsupported := func(component string) {
		if !componentName.MatchString(component) {
			return
		}
		for _, existing := range unsupported {
			if existing == component {
				return
			}
		}
		unsupported = append(unsupported, component)
	}

	for _, lane := range []string{"left", "right"} {
		nodes, _ := config[lane].([]any)
		laneOrder := 0
		for _, node := range nodes {
			entry, ok := node.(map[string]any)
			if !ok {
				continue
			}
			component := plainText(entry["component"])
			props, _ := entry["props"].(map[string]any)
			if props == nil {
				props = map[string]any{}
			}
			if !jsonBool(props["show"], true) {
				continue
			}
			paywall := jsonBool(props["showPayWall"], false)
			if paywall {
				paywallComponents++
			}
			section := map[string]any{
				"lane":               lane,
				"order":              laneOrder,
				"component":          component,
				"paywall_configured": paywall,
			}
			laneOrder++

			switch component {
			case "TitleCom":
				if title := plainText(props["title"]); title != "" {
					section["title"] = title
					sections = append(sections, section)
				}
				continue
			case "RichTextCom":
				section["type"] = "rich_text"
				if paywall {
					section["access"] = "locked_by_topic_config"
				} else {
					section["access"] = "visible"
					section["text"] = plainText(props["content"])
				}
				sections = append(sections, section)
				continue
			case "ImageCom":
				section["type"] = "image"
				section["memo"] = plainText(props["memo"])
				if paywall {
					section["access"] = "locked_by_topic_config"
				} else {
					section["access"] = "visible"
					section["image"] = emptyToNil(httpsOutputURL(canonical, props["url"]))
					section["url"] = emptyToNil(caixinOutputURL(canonical, plainText(props["href"])))
				}
				sections = append(sections, section)
				continue
			case "TimeListCom":
			default:
				noteUnsupported(component)
				continue
			}

			flowType := strings.ToLower(plainText(props["type"]))
			if flowType == "" {
				flowType = "custom"
			}
			requestType, requestCode := topicType, dataCode
			labelID := plainText(props["labelId"])
			if labelID == "" {
				labelID = "labelId"
			}
			if flowType == "other" {
				// An "other" list borrows another topic's feed; the borrowed
				// identity comes from the config, so it is checked like input.
				other, _ := props["otherParams"].(map[string]any)
				requestType = strings.ToUpper(plainText(other["param1"]))
				requestCode = plainText(other["param2"])
				flowType = "default"
			}
			if (flowType != "custom" && flowType != "default") ||
				!deepviewLabelID.MatchString(labelID) ||
				!deepviewRequestType.MatchString(requestType) ||
				!deepviewRequestCode.MatchString(requestCode) {
				noteUnsupported(component)
				continue
			}

			requested, _ := safeInt(props["defalutSize"])
			if requested == 0 {
				requested = 5
			}
			// A paywalled list shows three teasers; an open one shows what the
			// page asked for, capped so a bad config cannot request the world.
			defaultSize := min(max(requested, 1), 20)
			pageSize := 20
			visibleLimit := defaultSize
			if paywall {
				defaultSize, pageSize, visibleLimit = 2, 3, 3
			}
			flowValue, err := c.requestJSON(ctx, requestSpec{
				Method: http.MethodGet,
				URL:    deepviewAPI + "/api/flow/important/" + flowType + "/" + labelID,
				Query: neturl.Values{
					"pageSize":    {strconv.Itoa(pageSize)},
					"pageNum":     {"1"},
					"type":        {requestType},
					"dataCode":    {requestCode},
					"defalutSize": {strconv.Itoa(defaultSize)},
				},
				Headers: headers,
			})
			if err != nil {
				return nil, err
			}
			if flowValue, err = deepviewSuccess(flowValue, "reading a deepview topic list"); err != nil {
				return nil, err
			}
			rows, _ := flowValue["rows"].([]any)
			if len(rows) > visibleLimit {
				rows = rows[:visibleLimit]
			}
			items := []any{}
			for _, row := range rows {
				if item := deepviewArticle(row); item != nil {
					items = append(items, item)
				}
			}
			total, hasTotal := safeInt(flowValue["total"])
			fullTotal, hasFullTotal := safeInt(flowValue["fullTotal"])
			if hasFullTotal && hasTotal && fullTotal > total {
				detailsNotFetched = true
			}
			access := "listing_visible"
			if paywall {
				access = "paywall_preview"
			}
			section["type"] = "article_list"
			section["title"] = plainText(props["title"])
			section["access"] = access
			section["visible_limit"] = visibleLimit
			section["total"] = intOrNil(flowValue["total"])
			section["full_total_with_details"] = intOrNil(flowValue["fullTotal"])
			section["items"] = items
			sections = append(sections, section)
		}
	}

	title := plainText(infoData["name"])
	if title == "" {
		title = firstXPathText(doc, "//title")
	}
	return map[string]any{
		"topic_listing_only": true,
		"url":                canonical,
		"topic_type":         topicType,
		"data_code":          dataCode,
		"title":              title,
		"description": firstXPathText(doc,
			"//*[@id='__cxnewsapp_sharewxtext']", "//meta[@name='description']/@content"),
		"image": emptyToNil(httpsOutputURL(canonical, firstXPathText(doc,
			"//*[@id='__cxnewsapp_sharewxthumburl']", "//meta[@property='og:image']/@content"))),
		"updated_at":             plainText(infoData["updateTime"]),
		"sections":               sections,
		"sections_count":         len(sections),
		"items_count":            countSectionItems(sections),
		"paywall_components":     paywallComponents,
		"details_not_fetched":    detailsNotFetched,
		"unsupported_components": unsupported,
	}, nil
}

// countSectionItems totals the entries across a topic's sections.
func countSectionItems(sections []any) int {
	total := 0
	for _, raw := range sections {
		if section, ok := raw.(map[string]any); ok {
			total += len(asList(section["items"]))
		}
	}
	return total
}

// mustPath returns a url's path, or "" if it cannot be parsed.
func mustPath(raw string) string {
	parsed, err := neturl.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Path
}
