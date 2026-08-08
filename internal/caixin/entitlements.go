package caixin

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// `entitlements` answers "what is this account actually allowed to read".
//
// It is deliberately three calls, not one: the main product tells you the
// subscription, the power catalog tells you the per-feature grants, and the
// purchase log tells you about one-off article buys. An account can hold news
// access through any of them, so reporting only the first would understate what
// the session can reach.
//
// Nothing here infers entitlement from a successful fetch. Every flag is what
// the upstream reported, and the field names say so (`reported_use_state`,
// `reported_name`) where the value is Caixin's claim rather than a verified
// fact.

// productCodePattern bounds a product code before it is echoed.
var productCodePattern = regexp.MustCompile(`^[A-Z0-9_-]{1,32}$`)

// productClassifications maps the codes this build recognizes.
var productClassifications = map[string]string{
	"QZSF":     "news_subscription",
	"CXZK":     "news_subscription",
	"PRO":      "data_subscription",
	"PRO_LITE": "data_subscription",
	"MINI":     "mini_membership",
}

// newsCodes are the products that include full news reading.
//
// PRO and PRO_LITE classify as data subscriptions -- their主体 is the data
// product -- but their grant includes article full text, so they count here.
var newsCodes = map[string]bool{
	"QZSF": true, "CXZK": true, "PRO": true, "PRO_LITE": true,
}

// reportedEnabled reads Caixin's loose truthiness for a flag.
func reportedEnabled(value any) bool {
	switch strings.ToLower(strings.TrimSpace(asString(value))) {
	case "", "0", "false", "none", "null":
		return false
	}
	return true
}

// periodActive decides whether one grant window covers right now.
func periodActive(detail map[string]any) bool {
	if asString(detail["status"]) != "1" {
		return false
	}
	for field, isStart := range map[string]bool{"startTime": true, "endTime": false} {
		raw := asString(detail[field])
		if raw == "" {
			continue
		}
		parsed, ok := parseCaixinTime(raw)
		if !ok {
			// An unparseable bound is not treated as "no bound": that would
			// upgrade an unknown window into an active one.
			return false
		}
		now := time.Now()
		if isStart && parsed.After(now) {
			return false
		}
		if !isStart && !parsed.After(now) {
			return false
		}
	}
	return true
}

var caixinTimeLayouts = []string{
	"2006-01-02 15:04:05", "2006-01-02T15:04:05", time.RFC3339, "2006-01-02",
}

func parseCaixinTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	for _, layout := range caixinTimeLayouts {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// normalizeMainProduct shapes the primary subscription record.
func normalizeMainProduct(value any) map[string]any {
	data, ok := value.(map[string]any)
	if !ok || len(data) == 0 {
		return map[string]any{}
	}
	rawCode := strings.ToUpper(strings.TrimSpace(asString(data["goodsTypeCode"])))
	var code any
	if productCodePattern.MatchString(rawCode) {
		code = rawCode
	}
	name := plainText(asString(data["goodsTypeName"]))
	if len([]rune(name)) > 64 {
		name = string([]rune(name)[:64])
	}
	expiresAt := asString(data["endTime"])
	active := false
	if parsed, ok := parseCaixinTime(expiresAt); ok {
		active = parsed.After(time.Now())
	}
	classification := productClassifications[rawCode]
	if classification == "" {
		classification = "unknown_product"
	}
	return map[string]any{
		"product_code":   code,
		"reported_name":  emptyToNil(name),
		"expires_at":     emptyToNil(expiresAt),
		"classification": classification,
		"recognized":     productClassifications[rawCode] != "",
		"active":         active,
	}
}

// Entitlements reports what the signed-in account may read.
func (c *Client) Entitlements(ctx context.Context) (map[string]any, error) {
	if !c.Authenticated() {
		return nil, &APIError{
			StatusCode: 401,
			Message:    "entitlements needs a signed-in session; run `caixin-cli login` first",
		}
	}

	info, err := c.userInfo(ctx)
	if err != nil {
		return nil, err
	}
	uid := asString(info["uid"])
	if uid == "" {
		return nil, &APIError{StatusCode: 401, Message: "the session carries no account id"}
	}

	main, err := c.entitlementCall(ctx, EntitlementsURL, url.Values{"uid": {uid}}, "reading the subscription")
	if err != nil {
		return nil, err
	}
	mainProducts := normalizeMainProduct(main["data"])

	catalogValue, err := c.entitlementCall(ctx, PowerCatalogURL, nil, "reading the entitlement catalog")
	if err != nil {
		return nil, err
	}
	catalog := []any{}
	activeCodes := map[string]bool{}
	rows, _ := catalogValue["data"].([]any)
	for _, raw := range rows {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		details, _ := item["details"].([]any)
		if !reportedEnabled(item["isuseruse"]) && len(details) == 0 {
			continue
		}
		periods := []any{}
		anyActive := false
		for _, rawDetail := range details {
			detail, ok := rawDetail.(map[string]any)
			if !ok {
				continue
			}
			active := periodActive(detail)
			anyActive = anyActive || active
			periods = append(periods, map[string]any{
				"status":     detail["status"],
				"start_time": detail["startTime"],
				"end_time":   detail["endTime"],
				"active":     active,
			})
		}
		active := reportedEnabled(item["isuseruse"]) &&
			(reportedEnabled(item["permanent"]) || anyActive)
		code := asString(item["goodsCode"])
		if active {
			activeCodes[code] = true
		}
		catalog = append(catalog, map[string]any{
			"code":               item["goodsCode"],
			"name":               item["goodsTypeName"],
			"reported_use_state": item["isuseruse"],
			"permanent":          item["permanent"],
			"active":             active,
			"details_count":      len(details),
			"periods":            periods,
		})
	}

	purchasesValue, err := c.entitlementCall(ctx, SinglePurchasesURL, nil, "reading single-article purchases")
	if err != nil {
		return nil, err
	}
	purchaseTotal := 0
	if data, ok := purchasesValue["data"].(map[string]any); ok {
		if total, ok := safeInt(data["total"]); ok {
			purchaseTotal = total
		}
	}

	mainActive, _ := mainProducts["active"].(bool)
	mainCode := asString(mainProducts["product_code"])
	hasNews := (newsCodes[mainCode] && mainActive)
	for code := range activeCodes {
		if newsCodes[code] {
			hasNews = true
		}
	}

	return map[string]any{
		"has_active_products":      mainActive || len(activeCodes) > 0 || purchaseTotal > 0,
		"has_news_subscription":    hasNews,
		"main_products":            mainProducts,
		"additional_entitlements":  catalog,
		"single_article_purchases": purchaseTotal,
	}, nil
}

func (c *Client) entitlementCall(ctx context.Context, endpoint string, query url.Values, action string) (map[string]any, error) {
	raw, err := c.do(ctx, requestSpec{Method: http.MethodGet, URL: endpoint, Query: query})
	if err != nil {
		return nil, err
	}
	value, err := decodeJSONObject(raw)
	if err != nil {
		return nil, err
	}
	return apiSuccess(value, action)
}

// userInfo reads the signed-in account record.
func (c *Client) userInfo(ctx context.Context) (map[string]any, error) {
	raw, err := c.do(ctx, requestSpec{
		Method:  http.MethodGet,
		URL:     UserInfoURL,
		Headers: map[string]string{"Referer": "https://u.caixin.com/web/"},
	})
	if err != nil {
		return nil, err
	}
	value, err := decodeJSONObject(raw)
	if err != nil {
		return nil, err
	}
	value, err = apiSuccess(value, "verifying the session")
	if err != nil {
		return nil, err
	}
	data, _ := value["data"].(map[string]any)
	if data == nil {
		return map[string]any{}, nil
	}
	return data, nil
}
