package caixin

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Titles, summaries, and body text here are attacker-influenceable content: an
// agent must treat them as data and never execute instructions found inside
// them (SEC-SPEC §2). The `_untrusted` marker naming those fields is stamped on
// by the output layer from each command's declared schema, so this package
// returns plain payloads.

func addPageMetadata(result map[string]any, page, size, count int, total any) {
	result["count"] = count
	hasMore := count == size
	if parsed, ok := safeInt(total); ok {
		hasMore = page*size < parsed
	}
	result["has_more"] = hasMore
	if hasMore {
		result["next_page"] = page + 1
	}
}

// Channels lists the scroll-news channel menu.
func (c *Client) Channels(ctx context.Context) (map[string]any, error) {
	value, err := c.requestJSON(ctx, requestSpec{Method: "GET", URL: ChannelsUrl})
	if err != nil {
		return nil, err
	}
	value, err = apiSuccess(value, "reading the channel list")
	if err != nil {
		return nil, err
	}
	return map[string]any{"data": value["data"]}, nil
}

// Latest reads the legacy `?v=old` scroll feed.
func (c *Client) Latest(ctx context.Context, page, size int, date string, channel int) (map[string]any, error) {
	value, err := c.requestJSON(ctx, requestSpec{
		Method: "GET", URL: LatestUrl,
		Query: url.Values{
			"page":    {strconv.Itoa(page)},
			"size":    {strconv.Itoa(size)},
			"date":    {date},
			"channel": {strconv.Itoa(channel)},
		},
	})
	if err != nil {
		return nil, err
	}
	value, err = apiSuccess(value, "reading the latest scroll feed")
	if err != nil {
		return nil, err
	}
	data, _ := value["data"].(map[string]any)
	articles := []any{}
	if rows, ok := data["articleList"].([]any); ok {
		for _, row := range rows {
			if item, ok := row.(map[string]any); ok {
				articles = append(articles, NormalizeArticle(item))
			}
		}
	}
	result := map[string]any{
		"page":           data["currentPage"],
		"page_size":      data["pageSize"],
		"total":          data["totalRecords"],
		"session_loaded": c.Authenticated(),
		"articles":       articles,
	}
	addPageMetadata(result, page, size, len(articles), data["totalRecords"])
	return result, nil
}

// searchCategories fetches one of the two scope menus.
func (c *Client) searchCategories(ctx context.Context, menuType string, anonymous bool, headers map[string]string) ([]any, error) {
	value, err := c.requestJSON(ctx, requestSpec{
		Method: "GET", URL: SearchCategoriesUrl,
		Query:     url.Values{"type": {menuType}},
		Headers:   headers,
		Anonymous: anonymous,
	})
	if err != nil {
		return nil, err
	}
	value, err = apiSuccess(value, "reading the search scope menu")
	if err != nil {
		return nil, err
	}
	return NormalizeSearchCategories(value["data"], menuType == "PC_SEARCH")
}

// SearchMenu reports the live scope menu so an agent filters by what the site
// actually offers today rather than by a hardcoded list.
func (c *Client) SearchMenu(ctx context.Context) (map[string]any, error) {
	categories, err := c.searchCategories(ctx, "PC_SEARCH", false, nil)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"categories": categories,
		"time_ranges": []any{
			map[string]any{"name": "时间不限", "value": 0},
			map[string]any{"name": "一天内", "value": 1},
			map[string]any{"name": "一周内", "value": 2},
			map[string]any{"name": "一月内", "value": 3},
			map[string]any{"name": "一年内", "value": 4},
			map[string]any{"name": "选择时间", "value": 5},
		},
		"filters": []any{
			map[string]any{"name": "全文", "value": "all"},
			map[string]any{"name": "正文", "value": "text"},
			map[string]any{"name": "作者", "value": "author"},
			map[string]any{"name": "标题", "value": "title"},
		},
	}, nil
}

// SearchOptions are the validated inputs to Search.
type SearchOptions struct {
	Keyword      string
	Page         int
	Size         int
	CategoryCode string
	Sort         int
	TimeRange    int
	StartTime    string
	EndTime      string
	FilterCode   string
}

var dateOnly = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// Search runs a keyword search. Every filter is validated against the live menu
// before the request, so an unsupported scope fails as a usage error instead of
// silently returning results from a different one.
func (c *Client) Search(ctx context.Context, options SearchOptions) (map[string]any, error) {
	keyword := strings.TrimSpace(options.Keyword)
	if keyword == "" {
		return nil, invalid("the search keyword cannot be empty")
	}
	if options.Page < 1 || options.Size < 1 || options.Size > 20 {
		return nil, invalid("search page must be positive and size at most 20")
	}
	if options.TimeRange < 0 || options.TimeRange > 5 {
		return nil, invalid("search time range must be between 0 and 5")
	}
	if !SearchFilterCodes[options.FilterCode] {
		return nil, invalid("unknown search filter scope")
	}
	if options.TimeRange == 5 {
		if !dateOnly.MatchString(options.StartTime) || !dateOnly.MatchString(options.EndTime) {
			return nil, invalid("a custom time range needs start and end dates as YYYY-MM-DD")
		}
		if options.StartTime > options.EndTime {
			return nil, invalid("the custom range start date is after its end date")
		}
	} else if options.StartTime != "" || options.EndTime != "" {
		return nil, invalid("custom dates are only allowed with --time-range 5")
	}

	categories, err := c.searchCategories(ctx, "PC_SEARCH", false, nil)
	if err != nil {
		return nil, err
	}
	category, sorts := findCategory(categories, options.CategoryCode)
	if category == nil {
		return nil, invalid("that category is not in the current search menu")
	}
	if !sortAllowed(sorts, options.Sort) {
		return nil, invalid("the selected category does not support that sort order")
	}

	payload := map[string]any{
		"categoryId":   category["id"],
		"categoryCode": category["code"],
		"currentPage":  options.Page,
		"pageSize":     options.Size,
		"sort":         options.Sort,
		"timeRange":    options.TimeRange,
		"keyword":      keyword,
		"sysType":      "pc",
	}
	if options.TimeRange == 5 {
		payload["startTime"] = options.StartTime
		payload["endTime"] = options.EndTime
	}
	if options.FilterCode != "all" {
		payload["filterCode"] = options.FilterCode
	}

	value, err := c.requestJSON(ctx, requestSpec{Method: "POST", URL: SearchUrl, Body: payload})
	if err != nil {
		return nil, err
	}
	value, err = apiSuccess(value, "searching")
	if err != nil {
		return nil, err
	}
	data, _ := value["data"].(map[string]any)
	articles := []any{}
	if rows, ok := data["articleList"].([]any); ok {
		for _, row := range rows {
			if item, ok := row.(map[string]any); ok {
				article := NormalizeArticle(item)
				// Search results are tagged so an agent can tell a content hit
				// from the other record kinds this endpoint can return.
				article["kind"] = "content"
				articles = append(articles, article)
			}
		}
	}
	result := map[string]any{
		// The resolved menu entries are echoed back, not just the codes the
		// caller passed: an agent can then see which scope and sort the search
		// actually ran under without a second round trip.
		// Echoed without `sorts`: the caller asked which scope ran, not for
		// the whole menu back.
		"category":    withoutSorts(category),
		"page":        data["currentPage"],
		"page_size":   data["pageSize"],
		"total":       data["totalRecords"],
		"sort":        resolvedSort(sorts, options.Sort),
		"time_range":  options.TimeRange,
		"start_time":  emptyToNil(options.StartTime),
		"end_time":    emptyToNil(options.EndTime),
		"filter_code": options.FilterCode,
		"articles":    articles,
	}
	addPageMetadata(result, options.Page, options.Size, len(articles), data["totalRecords"])
	return result, nil
}

func findCategory(categories []any, code string) (map[string]any, []any) {
	for _, raw := range categories {
		category, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if asString(category["code"]) == code {
			sorts, _ := category["sorts"].([]any)
			return category, sorts
		}
	}
	return nil, nil
}

func sortAllowed(sorts []any, want int) bool {
	if len(sorts) == 0 {
		return want == 0
	}
	for _, raw := range sorts {
		if sortRow, ok := raw.(map[string]any); ok {
			if value, ok := safeInt(sortRow["value"]); ok && value == want {
				return true
			}
		}
	}
	return false
}

// Newscroll reads the site's default rolling news list for one channel.
func (c *Client) Newscroll(ctx context.Context, page int, date, categoryCode string) (map[string]any, error) {
	if page < 1 {
		return nil, invalid("newscroll page must be positive")
	}
	if date != "" {
		if !dateOnly.MatchString(date) {
			return nil, invalid("newscroll date must be YYYY-MM-DD")
		}
		parsed, err := time.Parse("2006-01-02", date)
		if err != nil || parsed.Before(time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC)) || parsed.After(time.Now().AddDate(0, 0, 1)) {
			return nil, invalid("newscroll date must fall between 2010-01-01 and today")
		}
	}

	headers := map[string]string{"Origin": "https://www.caixin.com", "Referer": NewscrollPageUrl}
	categories, err := c.searchCategories(ctx, NewscrollMenuType, true, headers)
	if err != nil {
		return nil, err
	}
	if len(categories) == 0 {
		return nil, &APIError{Message: "the newscroll channel menu is empty"}
	}
	category, _ := categories[0].(map[string]any)
	if categoryCode != "" {
		category, _ = findCategory(categories, strings.TrimSpace(categoryCode))
		if category == nil {
			return nil, invalid("that channel is not in the current newscroll menu")
		}
	}

	payload := map[string]any{
		"currentPage": page,
		"pageSize":    NewscrollPageSize,
		"categoryId":  category["id"],
		"keyword":     "",
	}
	if date != "" {
		payload["startTime"] = date
		payload["endTime"] = date
	}
	value, err := c.requestJSON(ctx, requestSpec{
		Method: "POST", URL: SearchUrl, Body: payload, Headers: headers, Anonymous: true,
	})
	if err != nil {
		return nil, err
	}
	value, err = apiSuccess(value, "reading the newscroll list")
	if err != nil {
		return nil, err
	}
	data, ok := value["data"].(map[string]any)
	if !ok {
		return nil, &APIError{Message: "the newscroll response is missing data"}
	}
	articles := []any{}
	rows, _ := data["articleList"].([]any)
	for _, row := range rows {
		if item, ok := row.(map[string]any); ok {
			articles = append(articles, NormalizeArticle(item))
		}
	}
	total, _ := safeInt(data["totalRecords"])
	result := map[string]any{
		"listing_only": true,
		"session_used": false,
		"category":     category,
		"date":         emptyToNil(date),
		"page":         data["currentPage"],
		"page_size":    data["pageSize"],
		"total":        data["totalRecords"],
		"returned":     len(articles),
		"articles":     articles,
	}
	addPageMetadata(result, page, NewscrollPageSize, len(articles), total)
	result["returned"] = len(articles)
	return result, nil
}

// CXDataFeedItems reads one of the nine public Caixin Data feeds.
func (c *Client) CXDataFeedItems(ctx context.Context, category string, page, size int) (map[string]any, error) {
	contract, ok := CXDataFeeds[category]
	if !ok {
		return nil, invalid("cxdata-feed only covers the documented public feeds")
	}
	if page < 1 || size < 1 || size > 25 {
		return nil, invalid("cxdata-feed page must be positive and size at most 25")
	}
	query := url.Values{"pageNum": {strconv.Itoa(page)}}
	if contract.PageSize {
		query.Set("pageSize", strconv.Itoa(size))
	}
	if contract.HasLabels {
		// Upstream expects Python-style capitalized booleans here.
		if contract.ShowLabels {
			query.Set("showLabels", "True")
		} else {
			query.Set("showLabels", "False")
		}
	}

	value, err := c.requestJSON(ctx, requestSpec{
		Method: "GET", URL: contract.URL, Query: query,
		Headers:   map[string]string{"Origin": CxdataOrigin, "Referer": contract.Referer},
		Anonymous: true,
	})
	if err != nil {
		return nil, err
	}

	var rows []any
	result := map[string]any{
		"listing_only": true, "session_used": false,
		"category": category, "name": contract.Name,
		"page": page, "page_size": size,
	}
	switch contract.Shape {
	case "latest", "hot":
		data, ok := value["data"].(map[string]any)
		if value["success"] != true || !ok {
			return nil, &APIError{Message: "the Caixin Data feed contract changed"}
		}
		key := "data"
		if contract.Shape == "hot" {
			key = "list"
		}
		if rows, ok = data[key].([]any); !ok {
			return nil, &APIError{Message: "the Caixin Data feed contract changed"}
		}
		result["total"] = intOrNil(data["total"])
		result["total_pages"] = intOrNil(data["totalPage"])
		result["upstream_page_size"] = intOrNil(data["pageSize"])
	default:
		if _, err := apiSuccess(value, "reading the Caixin Data feed"); err != nil {
			return nil, err
		}
		if rows, ok = value["data"].([]any); !ok {
			return nil, &APIError{Message: "the Caixin Data feed list contract changed"}
		}
	}

	items := []any{}
	for _, row := range rows {
		normalized := NormalizeCXDataItem(row, category)
		if normalized == nil {
			continue
		}
		items = append(items, normalized)
		if len(items) >= size {
			break
		}
	}
	result["items"] = items
	result["returned"] = len(items)
	result["count"] = len(items)
	if total, ok := result["total"].(int); ok {
		result["has_more"] = page*size < total
	} else {
		result["has_more"] = len(items) == size
	}
	if hasMore, _ := result["has_more"].(bool); hasMore {
		result["next_page"] = page + 1
	}
	return result, nil
}

// EntitiesPreview reads the single anonymous preview record each entity library
// exposes. It is a fixed one-record probe, never a way to enumerate the库.
func (c *Client) EntitiesPreview(ctx context.Context, category string) (map[string]any, error) {
	contract, ok := EntityPreviews[category]
	if !ok {
		return nil, invalid("entities-preview only covers companies and persons")
	}
	query := url.Values{}
	for key, value := range contract.Params {
		query.Set(key, value)
	}
	value, err := c.requestJSON(ctx, requestSpec{
		Method: "GET", URL: contract.URL, Query: query,
		Headers:   map[string]string{"Origin": EntitiesOrigin, "Referer": contract.Referer},
		Anonymous: true,
	})
	if err != nil {
		return nil, err
	}
	data, ok := value["data"].([]any)
	if value["success"] != true || asString(value["code"]) != "0" || !ok || len(data) > 1 {
		return nil, &APIError{Message: "the entity preview contract changed"}
	}
	items := []any{}
	for _, row := range data {
		var (
			item map[string]any
			err  error
		)
		if category == "companies" {
			item, err = NormalizeCompanyPreview(row)
		} else {
			item, err = NormalizePersonPreview(row)
		}
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	result := map[string]any{
		"listing_only": true, "session_used": false,
		"category": category, "name": contract.Name,
		"returned":       len(items),
		"auth":           value["auth"],
		"is_enterprise":  value["isEnterprise"],
		"is_cxt_upgrade": value["isCxtUpgrade"],
		"access":         "preview_or_unknown",
		"items":          items,
	}
	return result, nil
}

// Topics lists one of the six topic-directory entry points. The url is checked
// against an allowlist: an arbitrary topics url is rejected, not fetched.
func (c *Client) Topics(ctx context.Context, entry string, page, size int) (map[string]any, error) {
	contract, ok := TopicCategories[entry]
	if !ok {
		return nil, invalid("that url is not one of the six topic directory entry points")
	}
	if page < 1 || size < 1 || size > 25 {
		return nil, invalid("topics page must be positive and size at most 25")
	}
	start := (page - 1) * size
	value, err := c.requestJSON(ctx, requestSpec{
		Method: "GET", URL: TopicDirectoryApi,
		Query: url.Values{
			"subject": {strconv.Itoa(contract.Subject)},
			"type":    {"0"},
			"count":   {strconv.Itoa(size)},
			"picdim":  {"_266_177"},
			"start":   {strconv.Itoa(start)},
		},
		Headers: map[string]string{
			"User-Agent": TopicsCompatUserAgent,
			"Origin":     "https://topics.caixin.com",
			"Referer":    entry,
		},
	})
	if err != nil {
		return nil, err
	}
	rows, _ := value["datas"].([]any)
	items := []any{}
	for _, row := range rows {
		if item := NormalizeTopicDirectoryItem(row); item != nil {
			items = append(items, item)
		}
	}
	total, hasTotal := safeInt(value["maxes"])
	hasMore := len(rows) == size
	if hasTotal {
		hasMore = start+len(rows) < total
	}
	result := map[string]any{
		"directory_only": true,
		"url":            entry,
		"category":       contract.Category,
		"name":           contract.Name,
		"subject":        contract.Subject,
		"page":           page,
		"page_size":      size,
		"start":          start,
		"total":          intOrNil(value["maxes"]),
		"returned":       len(items),
		"has_more":       hasMore,
		"items":          items,
	}
	result["count"] = len(items)
	if hasMore {
		result["next_page"] = page + 1
	}
	return result, nil
}

// frontlineJSONP calls one of the JSONP endpoints and unwraps the envelope.
//
// The callback name is derived from the request counter rather than the clock:
// upstream echoes it back and the client matches it exactly, so a value that
// changes between identical calls would make the response unverifiable.
func (c *Client) frontlineJSONP(ctx context.Context, endpoint string, query url.Values) (map[string]any, error) {
	callback := fmt.Sprintf("__caixincallback%d", c.nextCallback())
	query.Set("callback", callback)

	raw, err := c.do(ctx, requestSpec{Method: "GET", URL: endpoint, Query: query, Anonymous: true})
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(string(raw))
	prefix := callback + "("
	if !strings.HasPrefix(text, prefix) || !strings.HasSuffix(strings.TrimSuffix(text, ";"), ")") {
		return nil, &APIError{Message: "the Caixin frontline endpoint returned unrecognized JSONP"}
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(text, prefix), ";")
	inner = strings.TrimSuffix(inner, ")")

	var value map[string]any
	if err := jsonUnmarshal([]byte(inner), &value); err != nil {
		return nil, &APIError{Message: "the Caixin frontline payload was not JSON"}
	}
	return value, nil
}

// Frontline lists 财新一线 flash news.
func (c *Client) Frontline(ctx context.Context, page, size int) (map[string]any, error) {
	if page < 1 || size < 1 || size > 20 {
		return nil, invalid("frontline page must be positive and size at most 20")
	}
	value, err := c.frontlineJSONP(ctx, FrontlineListUrl, url.Values{
		"productIdList": {"8,28"}, "uid": {""}, "unit": {"1"}, "name": {""},
		"code": {""}, "deviceType": {""}, "device": {""}, "userTag": {""},
		"p": {strconv.Itoa(page)}, "c": {strconv.Itoa(size)},
	})
	if err != nil {
		return nil, err
	}
	data, _ := value["data"].(map[string]any)
	rows, _ := data["list"].([]any)
	items := []any{}
	for _, row := range rows {
		if item := NormalizeFrontlineItem(row); item != nil {
			items = append(items, item)
		}
	}
	result := map[string]any{
		"page": page, "page_size": size,
		"has_more": len(rows) == size,
		"items":    items,
	}
	result["count"] = len(items)
	if hasMore, _ := result["has_more"].(bool); hasMore {
		result["next_page"] = page + 1
	}
	return result, nil
}

// FrontlineDetail reads one flash-news item by its 32-hex code.
func (c *Client) FrontlineDetail(ctx context.Context, code string) (map[string]any, error) {
	normalized := strings.ToLower(strings.TrimSpace(code))
	if !hexCode32.MatchString(normalized) {
		return nil, invalid("a frontline code is 32 hexadecimal characters")
	}
	value, err := c.frontlineJSONP(ctx, FrontlineDetailUrl, url.Values{"code": {normalized}})
	if err != nil {
		return nil, err
	}
	item := NormalizeFrontlineItem(value["data"])
	if item == nil || item["code"] != normalized {
		return nil, &APIError{Message: "the frontline detail response held no matching item"}
	}
	return map[string]any{"item": item}, nil
}

// BloggersDirectory lists one explicit page of the public blogger directory.
func (c *Client) BloggersDirectory(ctx context.Context, page int, sort string) (map[string]any, error) {
	if page < 1 {
		return nil, invalid("bloggers-directory page must be positive")
	}
	sortKey, ok := BloggersDirectorySorts[sort]
	if !ok {
		return nil, invalid("bloggers-directory sort must be latest or pinyin")
	}
	apiURL := fmt.Sprintf("https://blog.caixin.com/json/blogger-%s-%d.json", sortKey, page)
	value, err := c.requestJSON(ctx, requestSpec{
		Method: "GET", URL: apiURL,
		Headers:   map[string]string{"User-Agent": TopicsCompatUserAgent, "Referer": BloggersDirectoryUrl},
		Anonymous: true,
	})
	if err != nil {
		return nil, err
	}
	value, err = apiSuccess(value, "reading the blogger directory")
	if err != nil {
		return nil, err
	}
	rows, ok := value["data"].([]any)
	reportedPage, hasPage := safeInt(value["page"])
	pageSize, hasSize := safeInt(value["size"])
	totalPages, hasPages := safeInt(value["totalPages"])
	totalElements, hasElements := safeInt(value["totalElements"])
	if !ok || len(rows) > 20 || !hasPage || reportedPage != page || !hasSize || pageSize != 20 ||
		!hasPages || totalPages < 0 || !hasElements || totalElements < 0 {
		return nil, &APIError{Message: "the blogger directory contract changed"}
	}

	var items []map[string]any
	seen := map[string]bool{}
	// The directory does repeat authors across a page. Failing the command over
	// it was wrong twice over: the data is perfectly usable once collapsed, and
	// the failure was reported as retryable, so an agent would retry a condition
	// that reproduces every time. Collapse instead, and say how many were
	// dropped so the count is never silently short.
	duplicates, malformed := 0, 0
	for _, row := range rows {
		record, ok := row.(map[string]any)
		if !ok {
			malformed++
			continue
		}
		// An entry that no longer has a usable name and page is dropped and
		// counted, for the same reason duplicates are: one bad row does not make
		// the other nineteen unreadable, and failing the whole page over it --
		// retryably, on a condition that reproduces -- helps nobody.
		item := bloggersDirectoryItem(record)
		if item == nil {
			malformed++
			continue
		}
		link := asString(item["url"])
		if seen[link] {
			duplicates++
			continue
		}
		seen[link] = true
		items = append(items, item)
	}

	result := attachClickConsumers(directoryResult([]snapshotModule{{
		Key: "bloggers-directory.authors", Name: "全部博主",
		Lane: "main", Order: 0, State: "server_returned", Items: items,
	}}))
	for _, item := range items {
		item["access"] = "public_author_directory"
		item["rendered_visibility_not_verified"] = true
	}

	result["source_mode"] = "server_html_discovery_plus_json_api"
	result["source"] = map[string]any{
		"directory_url": BloggersDirectoryUrl,
		"api_url":       apiURL,
		"fetched_at":    time.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	result["session_used"] = false
	result["sort"] = sort
	result["page"] = reportedPage
	result["returned"] = len(items)
	result["reported_total_pages"] = totalPages
	result["reported_total_elements"] = totalElements
	result["reported_first_page"] = boolOrNil(value["firstPage"])
	result["reported_last_page"] = boolOrNil(value["lastPage"])
	result["pagination"] = map[string]any{
		"page":          reportedPage,
		"page_size":     pageSize,
		"sort":          sort,
		"explicit_only": true,
	}
	result["automatic_continuation_followed"] = false
	result["search_not_called"] = true
	result["linked_pages_not_fetched"] = true
	result["scripts_not_executed"] = true
	// Reported only when it happened, so a clean page carries no field a caller
	// has to interpret.
	if duplicates > 0 {
		result["duplicates_dropped"] = duplicates
	}
	if malformed > 0 {
		result["unreadable_entries_dropped"] = malformed
	}
	return result, nil
}

// bloggersDirectoryItem normalizes one blogger record.
//
// Every field is checked rather than defaulted: this endpoint is the only
// listing of the whole roster, so a row that changed shape would otherwise be
// emitted as an author with no name or no page.
func bloggersDirectoryItem(record map[string]any) map[string]any {
	name := plainText(record["name"])
	// The field is `authorUrl`. Reading `url` yielded nothing for every row, so
	// each author was emitted with an empty url and every row after the first
	// looked like a duplicate of it.
	canonical, _ := blogAuthorURL(absoluteURL(BloggersDirectoryUrl, plainText(record["authorUrl"])))
	if name == "" || canonical == "" {
		return nil
	}
	authorID, hasID := safeInt(record["id"])
	latest, hasLatest := safeInt(record["lastestTime"])
	var identifier, published any
	if hasID && authorID > 0 {
		identifier = authorID
	}
	if hasLatest && latest > 0 {
		published = latest
	}
	return map[string]any{
		"author_id":           identifier,
		"title":               name,
		"url":                 canonical,
		"image":               emptyToNil(blogImageURL(BloggersDirectoryUrl, plainText(record["avatar"]))),
		"summary":             emptyToNil(plainText(record["introduce2"])),
		"published_at":        isoTimestamp(published),
		"badges":              []any{},
		"item_kind":           "author",
		"content_not_fetched": true,
	}
}

// resolvedSort returns the menu entry for the requested sort so the result
// echoes what the search actually applied.
func resolvedSort(sorts []any, want int) any {
	for _, raw := range sorts {
		if row, ok := raw.(map[string]any); ok {
			if value, ok := safeInt(row["value"]); ok && value == want {
				return row
			}
		}
	}
	return want
}

// withoutSorts copies a menu category minus its sort list.
func withoutSorts(category map[string]any) map[string]any {
	trimmed := make(map[string]any, len(category))
	for key, value := range category {
		if key == "sorts" {
			continue
		}
		trimmed[key] = value
	}
	return trimmed
}
