package caixin

import (
	"context"
	"net/http"
	neturl "net/url"
	"regexp"
	"strings"
)

// A Key topic is served as tabs, and each tab names its own API url. Those urls
// come from the response, so they are checked as strictly as a caller's input:
// host, path, and parameters must all be ones this build already knows, or the
// read stops. A response that can name its own next request is a response that
// can redirect the client anywhere.

// keyTopicTabPaths are the three tab endpoints.
const (
	keyTopicNewsPath  = "/apientry/newTopic/getNewsTabContent"
	keyTopicJXPath    = "/apientry/newTopic/getJxTabContent"
	keyTopicOtherPath = "/apientry/newTopic/getOtherTabContent"
)

var keyTopicTabPaths = map[string]bool{
	keyTopicNewsPath: true, keyTopicJXPath: true, keyTopicOtherPath: true,
}

var keyTopicNumeric = regexp.MustCompile(`^\d{1,20}$`)
var keyTopicRelationCode = regexp.MustCompile(`^(\d{1,20}|[A-Z0-9]+(\.[A-Z0-9]+)+)$`)

// keyTopicAPIURL accepts a tab url the response supplied.
func keyTopicAPIURL(raw string, allowed map[string]bool) (string, bool) {
	if raw == "" || strings.ContainsAny(raw, " \t\n\r") {
		return "", false
	}
	parsed, err := neturl.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" {
		return "", false
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return "", false
	}
	if strings.ToLower(parsed.Hostname()) != "entities.caixin.com" || !allowed[parsed.Path] {
		return "", false
	}
	query, err := neturl.ParseQuery(parsed.RawQuery)
	if err != nil {
		return "", false
	}
	expected := map[string]bool{"tabId": true, "pageNum": true, "pageSize": true}
	if parsed.Path == keyTopicJXPath {
		expected = map[string]bool{"tabId": true}
	}
	if len(query) != len(expected) {
		return "", false
	}
	for key, values := range query {
		if !expected[key] || len(values) != 1 {
			return "", false
		}
	}
	if !keyTopicNumeric.MatchString(query.Get("tabId")) {
		return "", false
	}
	if parsed.Path != keyTopicJXPath {
		pageNum, hasNum := safeIntString(query.Get("pageNum"))
		pageSize, hasSize := safeIntString(query.Get("pageSize"))
		if !hasNum || pageNum < 1 || !hasSize || pageSize < 1 || pageSize > 100 {
			return "", false
		}
	}
	return parsed.Path, true
}

// safeIntString parses a decimal parameter.
func safeIntString(value string) (int, bool) {
	if !keyTopicNumeric.MatchString(value) {
		return 0, false
	}
	number, present := safeInt(value)
	return number, present
}

// keyTopicItem normalizes one row, which is either an article or a related
// entity. Both appear in the same list, so `kind` says which.
func keyTopicItem(row any) map[string]any {
	fields, ok := row.(map[string]any)
	if !ok {
		return nil
	}
	const base = "https://key.caixin.com/"
	title := plainText(fields["title"])
	rawURL, isText := fields["web_url"].(string)
	if title != "" && isText {
		link := caixinOutputURL(base, rawURL)
		if link == "" {
			return nil
		}
		var contentID any
		if raw := plainText(fields["id"]); contentIDPattern.MatchString(raw) {
			contentID = raw
		}
		image := ""
		if pics, ok := fields["pics"].(string); ok {
			image = httpsOutputURL(base, pics)
		}
		free, hasFree := safeInt(fields["isFree"])
		login, hasLogin := safeInt(fields["need_login"])
		var isFree, needLogin any
		if hasFree && (free == 0 || free == 1) {
			isFree = free == 1
		}
		if hasLogin && (login == 0 || login == 1) {
			needLogin = login == 1
		}
		// The access label is what the listing claims, and it is reported as
		// such: this client has not tried to open the article.
		access := "unknown"
		switch {
		case needLogin == true:
			access = "login_required"
		case isFree == true:
			access = "free"
		case isFree == false:
			access = "subscription_required"
		}
		return map[string]any{
			"kind":              "article",
			"content_id":        contentID,
			"title":             title,
			"url":               link,
			"summary":           plainText(fields["subhead"]),
			"author":            plainText(fields["author"]),
			"image":             emptyToNil(image),
			"published_at_unix": intOrNil(fields["time"]),
			"is_free":           isFree,
			"need_login":        needLogin,
			"ui_type":           intOrNil(fields["ui_type"]),
			"access":            access,
		}
	}

	dataCode := plainText(fields["dataCode"])
	relatedTitle := plainText(fields["name"])
	if relatedTitle == "" {
		relatedTitle = plainText(fields["showName"])
	}
	if relatedTitle == "" || !keyTopicRelationCode.MatchString(dataCode) {
		return nil
	}
	link := ""
	if rawURL, ok := fields["web_url"].(string); ok {
		link = caixinOutputURL(base, rawURL)
	}
	image := ""
	if pics, ok := fields["pics"].(string); ok {
		image = httpsOutputURL(base, pics)
	}
	summary := plainText(fields["info"])
	if summary == "" {
		summary = plainText(fields["induSmaPar"])
	}
	if summary == "" {
		summary = plainText(fields["operCond"])
	}
	var relationType any
	switch strings.ToLower(plainText(fields["type"])) {
	case "company", "people", "person", "topic":
		relationType = strings.ToLower(plainText(fields["type"]))
	}
	return map[string]any{
		"kind":          "related",
		"data_code":     dataCode,
		"title":         relatedTitle,
		"url":           emptyToNil(link),
		"summary":       summary,
		"image":         emptyToNil(image),
		"relation_type": relationType,
		"ui_type":       intOrNil(fields["ui_type"]),
		"access":        "directory_visible",
	}
}

// keyTopic reads a Key topic page.
func (c *Client) keyTopic(ctx context.Context, pageURL string) (map[string]any, error) {
	canonical := keyTopicURL(strings.TrimSpace(pageURL))
	if canonical == "" {
		return nil, invalid("topic reads a Caixin topic page, " +
			"for example https://key.caixin.com/topic/BQ02.000000368")
	}
	dataCode := strings.TrimPrefix(mustPath(canonical), "/topic/")

	headers := map[string]string{
		"Origin":  "https://key.caixin.com",
		"Referer": canonical,
	}
	infoValue, err := c.requestJSON(ctx, requestSpec{
		Method: http.MethodGet, URL: keyTopicInfoURL,
		Query:   neturl.Values{"dataCode": {dataCode}, "getLatestArticle": {"true"}},
		Headers: headers,
	})
	if err != nil {
		return nil, err
	}
	if infoValue, err = keyTopicSuccess(infoValue, "reading the Key topic"); err != nil {
		return nil, err
	}
	infoData, ok := infoValue["data"].(map[string]any)
	if !ok {
		return nil, &APIError{Message: "the Key topic response carries no data"}
	}
	if plainText(infoData["dataCode"]) != dataCode {
		return nil, &APIError{Message: "the Key topic endpoint answered about a different topic"}
	}
	tabs, ok := infoData["tabs"].([]any)
	if !ok || len(tabs) > 20 {
		return nil, &APIError{Message: "the Key topic tab list changed shape"}
	}

	sections := []any{}
	appendGroups := func(value map[string]any, tabID int, tabName, endpoint, fallback string) error {
		data, ok := value["data"].(map[string]any)
		if !ok {
			return &APIError{Message: "a Key topic tab carries no data"}
		}
		groups, ok := data["groupList"].([]any)
		if !ok {
			return &APIError{Message: "a Key topic tab carries no groupList"}
		}
		for _, raw := range groups {
			group, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			items := []any{}
			for _, listName := range []string{"mainList", "otherList"} {
				if group[listName] == nil {
					continue
				}
				rows, ok := group[listName].([]any)
				if !ok {
					return &APIError{Message: "a Key topic list changed shape"}
				}
				for _, row := range rows {
					if item := keyTopicItem(row); item != nil {
						items = append(items, item)
					}
				}
			}
			if len(items) == 0 {
				continue
			}
			title := plainText(group["name"])
			if title == "" {
				title = fallback
			}
			if title == "" {
				title = tabName
			}
			segments := strings.Split(endpoint, "/")
			sections = append(sections, map[string]any{
				"lane":         "main",
				"order":        len(sections),
				"component":    "KeyTopicTab",
				"type":         "listing",
				"title":        title,
				"tab_id":       tabID,
				"tab_name":     tabName,
				"tab_endpoint": segments[len(segments)-1],
				"access":       "listing_visible",
				"items":        items,
			})
		}
		return nil
	}

	seenTabs := map[int]bool{}
	seenURLs := map[string]bool{}
	for _, raw := range tabs {
		tab, ok := raw.(map[string]any)
		if !ok {
			return nil, &APIError{Message: "the Key topic tab list changed shape"}
		}
		tabID, hasID := safeInt(tab["id"])
		tabName := plainText(tab["name"])
		if tabName == "" {
			tabName = "专题"
		}
		tabURL, isText := tab["url"].(string)
		if !hasID || tabID < 1 || seenTabs[tabID] || !isText || seenURLs[tabURL] {
			return nil, &APIError{Message: "the Key topic tab list changed shape"}
		}
		endpoint, valid := keyTopicAPIURL(tabURL, keyTopicTabPaths)
		if !valid {
			return nil, &APIError{Message: "the Key topic named an endpoint this build does not call"}
		}
		parsedTab, _ := neturl.Parse(tabURL)
		if declared, present := safeIntString(parsedTab.Query().Get("tabId")); !present || declared != tabID {
			return nil, &APIError{Message: "a Key topic tab disagrees with its own id"}
		}
		seenTabs[tabID] = true
		seenURLs[tabURL] = true

		tabValue, err := c.requestJSON(ctx, requestSpec{
			Method: http.MethodGet, URL: tabURL, Headers: headers,
		})
		if err != nil {
			return nil, err
		}
		if tabValue, err = keyTopicSuccess(tabValue, "reading a Key topic tab"); err != nil {
			return nil, err
		}
		if err := appendGroups(tabValue, tabID, tabName, endpoint, tabName); err != nil {
			return nil, err
		}

		if endpoint != keyTopicJXPath {
			continue
		}
		tabData, _ := tabValue["data"].(map[string]any)
		next, present := tabData["nextPageUrl"]
		if !present || next == nil || next == "" {
			continue
		}
		nextURL, isText := next.(string)
		if !isText || seenURLs[nextURL] {
			return nil, &APIError{Message: "the Key topic returned an unsafe pagination url"}
		}
		nextPath, valid := keyTopicAPIURL(nextURL, map[string]bool{keyTopicNewsPath: true})
		if !valid {
			return nil, &APIError{Message: "the Key topic returned an unsafe pagination url"}
		}
		seenURLs[nextURL] = true
		nextValue, err := c.requestJSON(ctx, requestSpec{
			Method: http.MethodGet, URL: nextURL, Headers: headers,
		})
		if err != nil {
			return nil, err
		}
		if nextValue, err = keyTopicSuccess(nextValue, "reading a Key topic continuation"); err != nil {
			return nil, err
		}
		if err := appendGroups(nextValue, tabID, tabName, nextPath, "更多内容"); err != nil {
			return nil, err
		}
	}

	image := ""
	if pics, ok := infoData["pics"].(string); ok {
		image = httpsOutputURL(canonical, pics)
	}
	return map[string]any{
		"topic_listing_only": true,
		"topic_surface":      "key",
		"url":                canonical,
		"topic_type":         "TOPIC",
		"data_code":          dataCode,
		"title":              plainText(infoData["name"]),
		"description":        plainText(infoData["info"]),
		"image":              emptyToNil(image),
		"sections":           sections,
		"sections_count":     len(sections),
		"items_count":        countSectionItems(sections),
		"tabs_count":         len(tabs),
		// The tab endpoints declare their own paging; this client follows only
		// the one link the response supplied, and invents none.
		"pagination_not_guessed": true,
	}, nil
}
