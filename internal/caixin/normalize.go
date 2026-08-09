package caixin

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var whitespaceRun = regexp.MustCompile(`\s+`)

// asString renders a decoded JSON scalar the way the reference implementation
// does, so ids compare equal whether upstream sent 12 or "12".
func asString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	default:
		return fmt.Sprint(typed)
	}
}

// safeInt returns the integer value and whether one was present, tolerating the
// string-encoded numbers Caixin mixes into its payloads.
func safeInt(value any) (int, bool) {
	switch typed := value.(type) {
	case float64:
		return int(typed), true
	case int:
		return typed, true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func intOrNil(value any) any {
	if parsed, ok := safeInt(value); ok {
		return parsed
	}
	return nil
}

// isoTimestamp accepts the millisecond values used by the JSON feeds (and the
// occasional second-valued or string-encoded timestamp) and emits one stable
// ISO-8601 UTC representation.
func isoTimestamp(value any) any {
	parsed, ok := safeInt(value)
	if !ok || parsed <= 0 {
		return nil
	}
	if parsed < 1_000_000_000_000 {
		return time.Unix(int64(parsed), 0).UTC().Format(time.RFC3339)
	}
	return time.UnixMilli(int64(parsed)).UTC().Format(time.RFC3339)
}

// plainText unescapes entities, strips tags, and collapses whitespace. Caixin
// embeds markup in title and summary fields.
//
// Non-string scalars are stringified rather than discarded: upstream sends some
// fields (timestamps, ids) as numbers, and dropping them silently loses data the
// reference implementation emits.
func plainText(value any) string {
	if value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		switch value.(type) {
		case float64, int, int64, bool:
			text = asString(value)
		default:
			return ""
		}
	}
	if text == "" {
		return ""
	}
	text = html.UnescapeString(text)
	text = tagRun.ReplaceAllString(text, "")
	return strings.TrimSpace(whitespaceRun.ReplaceAllString(text, " "))
}

var tagRun = regexp.MustCompile(`<[^>]*>`)

// caixinHost reports whether a host belongs to Caixin. Emitted urls are held to
// this so a hijacked upstream field cannot point an agent somewhere else.
func caixinHost(host string) bool {
	host = strings.ToLower(host)
	return host == "caixin.com" || strings.HasSuffix(host, ".caixin.com")
}

// outputURL resolves a possibly relative url against base and returns it only
// if it stays on Caixin over http(s). Anything else becomes empty rather than
// being passed through to the agent.
func outputURL(base string, value any) string {
	raw, ok := value.(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return ""
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return ""
	}
	resolved, err := baseURL.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return ""
	}
	if !caixinHost(resolved.Hostname()) {
		return ""
	}
	return resolved.String()
}

// NormalizeArticle maps one gateway article record onto the stable output shape.
func NormalizeArticle(item map[string]any) map[string]any {
	contentID := item["contentId"]
	if contentID == nil {
		contentID = item["articleId"]
	}
	title := item["titleNoFont"]
	if plainText(title) == "" {
		title = item["title"]
	}
	channelName := item["parentName"]
	if channelName == nil {
		channelName = item["cornerMark"]
	}
	return map[string]any{
		"content_id":   contentID,
		"title":        plainText(title),
		"summary":      plainText(item["summary"]),
		"snippet":      plainText(item["text"]),
		"author":       plainText(item["author"]),
		"channel":      item["channel"],
		"channel_name": channelName,
		"url":          item["url"],
		"picture":      emptyToNil(asString(item["picture"])),
		"published_at": isoTimestamp(item["time"]),
		"updated_at":   isoTimestamp(item["updateTime"]),
	}
}

var hexCode32 = regexp.MustCompile(`^[0-9a-f]{32}$`)

// NormalizeFrontlineItem maps one 财新一线 record. Records without a well-formed
// code are dropped rather than emitted half-built.
func NormalizeFrontlineItem(value any) map[string]any {
	item, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	code := strings.ToLower(asString(item["oneline_news_code"]))
	if !hexCode32.MatchString(code) {
		return nil
	}

	selectedAudio := outputURL("https://k.caixin.com/", item["audio_url"])
	media := []any{}
	seen := map[string]bool{}
	if audios, ok := item["audios"].(map[string]any); ok {
		for _, pair := range []struct{ field, voice string }{
			{"man_audio_url", "male"},
			{"woman_audio_url", "female"},
		} {
			mediaURL := outputURL("https://k.caixin.com/", audios[pair.field])
			if mediaURL == "" {
				continue
			}
			media = append(media, map[string]any{
				"type": "audio", "voice": pair.voice, "url": mediaURL,
				"selected": mediaURL == selectedAudio, "access": "visible",
			})
			seen[mediaURL] = true
		}
	}
	if selectedAudio != "" && !seen[selectedAudio] {
		media = append(media, map[string]any{
			"type": "audio", "voice": "default", "url": selectedAudio,
			"selected": true, "access": "visible",
		})
	}

	var related any
	relatedURL := outputURL("https://k.caixin.com/", item["url"])
	relatedTitle := plainText(item["urlTitle"])
	if relatedURL != "" && relatedTitle != "" {
		related = map[string]any{"title": relatedTitle, "url": relatedURL}
	}

	return map[string]any{
		"code":              code,
		"detail_url":        "https://k.caixin.com/web/detail_" + code,
		"title":             plainText(item["title"]),
		"text":              plainText(item["text"]),
		"published_at":      isoTimestamp(item["ts"]),
		"date_label":        plainText(item["date"]),
		"time_label":        plainText(item["time"]),
		"type":              intOrNil(item["type"]),
		"highlighted":       truthy(item["attr"]),
		"audio_title":       plainText(item["audio_title"]),
		"media":             media,
		"image":             emptyToNil(outputURL("https://k.caixin.com/", item["img"])),
		"thumbnail":         emptyToNil(outputURL("https://k.caixin.com/", item["thumb"])),
		"image_description": emptyToNil(plainText(item["imgDesc"])),
		"related":           related,
	}
}

// emptyToNil renders an absent value as JSON null rather than "". The reference
// implementation distinguishes the two and agents branch on null.
func emptyToNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func truthy(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case float64:
		return typed != 0
	case string:
		return typed != "" && typed != "0"
	default:
		return true
	}
}

var dateOnlyRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// NormalizeCompanyPreview maps the single anonymous company preview record.
func NormalizeCompanyPreview(value any) (map[string]any, error) {
	item, ok := value.(map[string]any)
	if !ok {
		return nil, &APIError{Message: "the company preview contract changed"}
	}
	code := strings.TrimSpace(asString(item["orgUniCode"]))
	name := plainText(item["orgChiName"])
	if code == "" || name == "" {
		return nil, &APIError{Message: "the company preview record has no identifier or name"}
	}
	result := map[string]any{"entity_code": code, "name": name}
	for _, pair := range []struct{ upstream, output string }{
		{"operCond", "status"}, {"induSmaPar", "industry"}, {"text", "description"},
	} {
		if text := plainText(item[pair.upstream]); text != "" {
			result[pair.output] = text
		}
	}
	repName := plainText(item["orgDele"])
	repRole := plainText(item["orgDeleParName"])
	if repName != "" || repRole != "" {
		result["representative"] = map[string]any{"name": repName, "role": repRole}
	}
	if capital, ok := item["regCap"].(float64); ok {
		result["registered_capital"] = map[string]any{
			"value":    capital,
			"currency": emptyToNil(plainText(item["curyChiName"])),
		}
	}
	if established := plainText(item["orgEstDate"]); dateOnlyRE.MatchString(established) {
		result["established_at"] = established
	}
	if news := relatedNews(item); news != nil {
		result["related_news"] = news
	}
	return result, nil
}

// NormalizePersonPreview maps the single anonymous person preview record.
func NormalizePersonPreview(value any) (map[string]any, error) {
	item, ok := value.(map[string]any)
	if !ok {
		return nil, &APIError{Message: "the person preview contract changed"}
	}
	name := plainText(firstText(item["personName"], item["name"]))
	if name == "" {
		return nil, &APIError{Message: "the person preview record has no name"}
	}
	result := map[string]any{
		"entity_code": emptyToNil(strings.TrimSpace(asString(item["personCode"]))),
		"name":        name,
	}
	for _, pair := range []struct{ upstream, output string }{
		{"position", "position"}, {"orgChiName", "organization"}, {"text", "description"},
	} {
		if text := plainText(item[pair.upstream]); text != "" {
			result[pair.output] = text
		}
	}
	if news := relatedNews(item); news != nil {
		result["related_news"] = news
	}
	return result, nil
}

func firstText(values ...any) any {
	for _, value := range values {
		if plainText(value) != "" {
			return value
		}
	}
	return nil
}

// relatedNews lifts the one attached article an entity preview carries.
//
// The article lives under extra.newsList, not the record's own `news` field --
// that one names a different, older piece. Reading the wrong field produced a
// well-formed result with the wrong article in it, which only the recorded
// corpus caught.
func relatedNews(item map[string]any) map[string]any {
	extra, ok := item["extra"].(map[string]any)
	if !ok {
		return nil
	}
	list, ok := extra["newsList"].([]any)
	if !ok || len(list) == 0 {
		return nil
	}
	entry, ok := list[0].(map[string]any)
	if !ok {
		return nil
	}

	title := plainText(entry["title"])
	newsURL := outputURL("https://entities.caixin.com/", entry["url"])
	if title == "" || newsURL == "" {
		return nil
	}
	news := map[string]any{"title": title, "url": newsURL}
	if summary := plainText(entry["guide"]); summary != "" {
		news["summary"] = summary
	}
	if image := outputURL("https://entities.caixin.com/", entry["picture"]); image != "" {
		news["image"] = image
	}
	if published := isoTimestamp(entry["time"]); published != nil {
		news["published_at"] = published
	}
	return news
}

// NormalizeCXDataItem maps one Caixin Data feed row, keeping the reference
// implementation's full field set rather than a lossy subset.
func NormalizeCXDataItem(value any, category string) map[string]any {
	item, ok := value.(map[string]any)
	if !ok || strings.EqualFold(asString(item["flag"]), "ad") {
		return nil
	}
	titleSource := item["title"]
	if category == "hot" {
		titleSource = item["name"]
	}
	if category == "selected" && plainText(titleSource) == "" {
		titleSource = item["tag"]
	}
	title := plainText(titleSource)
	text := plainText(item["text"])
	itemURL := outputURL(CxdataOrigin+"/", item["url"])

	if category == "frontline" {
		if title == "" && text == "" {
			return nil
		}
	} else if title == "" || itemURL == "" {
		return nil
	}

	result := map[string]any{"title": title}
	if id := asString(firstNonEmptyValue(item["dataCode"], item["onlineNewsCode"], item["id"])); id != "" {
		result["content_id"] = id
	}
	if itemURL != "" {
		result["url"] = itemURL
	}
	summarySource := item["summary"]
	if category == "hot" {
		summarySource = item["info"]
	}
	if summary := plainText(summarySource); summary != "" {
		result["summary"] = summary
	}
	if text != "" {
		result["text"] = text
	}
	if image := outputURL(CxdataOrigin+"/", item["pic"]); image != "" {
		result["image"] = image
	}
	if published := firstNonEmptyValue(item["pubTime"], item["ts"], item["time"]); published != nil {
		result["published_at"] = published
	}
	for _, pair := range []struct{ upstream, output string }{
		{"tag", "tag"}, {"tagColor", "tag_color"}, {"date", "date_label"},
		{"intervalTime", "interval_label"},
		{"createTime", "created_at"}, {"updateTime", "updated_at"},
		{"type", "kind"}, {"flag", "flag"},
	} {
		if text := plainText(item[pair.upstream]); text != "" {
			result[pair.output] = text
		}
	}
	for _, pair := range []struct{ upstream, output string }{
		{"top", "top"}, {"hasVideo", "has_video"}, {"isToday", "is_today"},
		{"audioStatus", "audio_status"}, {"audioIsCheckAuth", "audio_auth_check"},
		{"auth", "auth"}, {"preview", "preview"},
	} {
		if value, present := item[pair.upstream]; present {
			result[pair.output] = value
		}
	}
	if labels, present := item["labels"]; present {
		result["labels"] = NormalizeCXDataLabels(labels)
	}
	return result
}

// NormalizeCXDataLabels maps the entity chips attached to a Caixin Data row.
// Passing them through raw kept upstream's `link` key and dropped the url the
// reference emits, which the recorded corpus caught.
func NormalizeCXDataLabels(value any) []any {
	rows, ok := value.([]any)
	if !ok {
		return []any{}
	}
	labels := []any{}
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		keyword := plainText(row["keyword"])
		labelURL := outputURL(CxdataOrigin+"/", row["link"])
		if keyword == "" && labelURL == "" {
			continue
		}
		label := map[string]any{"keyword": keyword}
		if labelURL != "" {
			label["url"] = labelURL
		}
		if labelType := plainText(row["type"]); labelTypeRE.MatchString(labelType) {
			label["type"] = labelType
		}
		labels = append(labels, label)
	}
	return labels
}

var labelTypeRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

func firstNonEmptyValue(values ...any) any {
	for _, value := range values {
		if value != nil && asString(value) != "" {
			return value
		}
	}
	return nil
}

// NormalizeSearchCategories validates and flattens the search scope menu.
//
// The upstream shape is checked rather than trusted: if Caixin changes the menu
// contract the command fails loudly instead of quietly searching the wrong
// scope and returning plausible but wrong results.
func NormalizeSearchCategories(value any, withSorts bool) ([]any, error) {
	rows, ok := value.([]any)
	if !ok || len(rows) < 1 || len(rows) > 50 {
		return nil, &APIError{Message: "the Caixin search menu contract changed"}
	}
	categories := []any{}
	seenIDs, seenCodes := map[int]bool{}, map[string]bool{}

	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			return nil, &APIError{Message: "the Caixin search menu contract changed"}
		}
		if shown, _ := safeInt(row["isShow"]); shown != 1 {
			continue
		}
		id, hasID := safeInt(row["id"])
		code := strings.TrimSpace(asString(row["code"]))
		name := plainText(row["name"])
		if !hasID || id < 1 || seenIDs[id] || code == "" || seenCodes[code] || name == "" {
			return nil, &APIError{Message: "the Caixin search menu contract changed"}
		}
		seenIDs[id], seenCodes[code] = true, true

		category := map[string]any{"id": id, "code": code, "name": name}
		if withSorts {
			rawSorts, ok := row["sortList"].([]any)
			if !ok || len(rawSorts) < 1 || len(rawSorts) > 10 {
				return nil, &APIError{Message: "the Caixin search sort contract changed"}
			}
			sorts := []any{}
			seenSorts := map[int]bool{}
			for _, rawSort := range rawSorts {
				sortRow, ok := rawSort.(map[string]any)
				if !ok {
					return nil, &APIError{Message: "the Caixin search sort contract changed"}
				}
				sortValue, hasValue := safeInt(sortRow["value"])
				sortName := plainText(sortRow["name"])
				if !hasValue || sortValue < 0 || sortValue > 9 || seenSorts[sortValue] || sortName == "" {
					return nil, &APIError{Message: "the Caixin search sort contract changed"}
				}
				seenSorts[sortValue] = true
				sorts = append(sorts, map[string]any{"name": sortName, "value": sortValue})
			}
			category["sorts"] = sorts
		}
		categories = append(categories, category)
	}
	return categories, nil
}

// NormalizeTopicDirectoryItem maps one topic-directory card.
func NormalizeTopicDirectoryItem(value any) map[string]any {
	row, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	title := plainText(row["desc"])
	if title == "" {
		return nil
	}
	freeTime := plainText(row["freeTime"])
	itemURL := outputURL("https://topics.caixin.com/", row["link"])
	item := map[string]any{
		"title":               title,
		"url":                 itemURL,
		"summary":             plainText(row["summ"]),
		"published_at":        plainText(row["time"]),
		"reported_attr":       intOrNil(row["attr"]),
		"reported_free_time":  emptyToNil(freeTime),
		"badge":               topicBadge(row["attr"], freeTime),
		"access":              "directory_visible",
		"content_not_fetched": true,
	}
	// Each card carries the command that would consume it, so an agent can walk
	// a directory into a read without a second round trip.
	if itemURL != "" {
		item["consumer"] = ClassifyURL(itemURL).AsEmbeddedMap()
	}
	if id := asString(row["nid"]); regexp.MustCompile(`^\d{1,20}$`).MatchString(id) {
		item["content_id"] = id
	} else {
		item["content_id"] = nil
	}
	if pict, ok := row["pict"].(map[string]any); ok {
		if images, ok := pict["imgs"].([]any); ok && len(images) > 0 {
			if first, ok := images[0].(map[string]any); ok {
				item["image"] = outputURL("https://topics.caixin.com/", first["url"])
			}
		}
	}
	if _, present := item["image"]; !present {
		item["image"] = nil
	}
	return item
}

// topicBadge renders the entitlement badge a topic card shows.
//
// The rule is upstream's, not ours: a free-until timestamp wins, then the attr
// enum. An unknown attr yields no badge rather than a guess, because claiming
// "free to read" about paid content would be worse than saying nothing.
func topicBadge(attr any, freeTime string) any {
	if freeTime != "" {
		return "限时免费"
	}
	value, ok := safeInt(attr)
	if !ok {
		return nil
	}
	switch value {
	case 0:
		return "免费阅读"
	case 4:
		return "注册阅读"
	case 5:
		return "收费文章"
	default:
		return nil
	}
}

// boolOrNil reports a flag only when upstream actually sent one, so a missing
// field is not reported as false.
func boolOrNil(value any) any {
	if flag, ok := value.(bool); ok {
		return flag
	}
	return nil
}
