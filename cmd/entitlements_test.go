package cmd

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func entitlementsMock(t *testing.T, mainCode string, catalogActive bool, purchases int) *mockUpstream {
	t.Helper()
	mock := newMockUpstream(t)
	mock.handlers["/api/ucenter/userinfo/get"] = func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0, "data": map[string]any{"uid": "8547219", "nickname": "某读者"},
		})
	}
	mock.handlers["/api/app-api/userAuth/getUserUseGoodsTypeV2"] = func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0, "data": map[string]any{
				"goodsTypeCode": mainCode, "goodsTypeName": "财新通",
				"endTime": "2099-01-01 00:00:00",
			},
		})
	}
	mock.handlers["/api/app-api/auth/findPowerByReq"] = func(w http.ResponseWriter, _ *http.Request) {
		use := "0"
		if catalogActive {
			use = "1"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0, "data": []any{map[string]any{
				"goodsCode": "PRO", "goodsTypeName": "数据通", "isuseruse": use,
				"permanent": "0",
				"details": []any{map[string]any{
					"status": "1", "startTime": "2020-01-01 00:00:00", "endTime": "2099-01-01 00:00:00",
				}},
			}},
		})
	}
	mock.handlers["/api/app-api/auth/getBuyArticleAuthLog"] = func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0, "data": map[string]any{"total": purchases},
		})
	}
	return mock
}

func entitledDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cookies := `[{"name":"SA_USER_UID","value":"8547219","domain":"www.caixin.com","path":"/","secure":false}]`
	if err := os.WriteFile(filepath.Join(dir, "cookies.json"), []byte(cookies), 0o600); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return dir
}

func TestEntitlements_ReportsNewsAccessFromTheMainProduct(t *testing.T) {
	got := runCLI(t, entitlementsMock(t, "QZSF", false, 0),
		"entitlements", "--state-dir", entitledDir(t), "--compact")
	if got.Exit != 0 {
		t.Fatalf("exit = %d: %s", got.Exit, got.Stdout)
	}
	data := got.Data(t)
	if news, _ := data["has_news_subscription"].(bool); !news {
		t.Error("an active 财新通 did not count as news access")
	}
	main, _ := data["main_products"].(map[string]any)
	if code, _ := main["product_code"].(string); code != "QZSF" {
		t.Errorf("product_code = %q", code)
	}
	if recognized, _ := main["recognized"].(bool); !recognized {
		t.Error("a known product code was reported as unrecognized")
	}
}

// A data subscription still grants full news text, so it must count -- the
// classification and the access question have different answers.
func TestEntitlements_DataSubscriptionStillGrantsNews(t *testing.T) {
	got := runCLI(t, entitlementsMock(t, "MINI", true, 0),
		"entitlements", "--state-dir", entitledDir(t), "--compact")
	data := got.Data(t)
	if news, _ := data["has_news_subscription"].(bool); !news {
		t.Error("an active PRO catalog grant did not count as news access")
	}
}

// An account with nothing must say so plainly rather than hedging.
func TestEntitlements_NoGrantsReportsNoAccess(t *testing.T) {
	got := runCLI(t, entitlementsMock(t, "MINI", false, 0),
		"entitlements", "--state-dir", entitledDir(t), "--compact")
	data := got.Data(t)
	if news, _ := data["has_news_subscription"].(bool); news {
		t.Error("has_news_subscription = true with no qualifying grant")
	}
	if active, _ := data["has_active_products"].(bool); !active {
		// MINI itself is active (endTime is far future), so this stays true.
		t.Error("an active mini membership was reported as no products at all")
	}
}

// One-off article purchases are an entitlement too.
func TestEntitlements_CountsSingleArticlePurchases(t *testing.T) {
	got := runCLI(t, entitlementsMock(t, "MINI", false, 7),
		"entitlements", "--state-dir", entitledDir(t), "--compact")
	data := got.Data(t)
	if count, _ := data["single_article_purchases"].(float64); count != 7 {
		t.Errorf("single_article_purchases = %v, want 7", data["single_article_purchases"])
	}
}

func TestEntitlements_WithoutASessionIsAuthError(t *testing.T) {
	got := run(t, entitlementsMock(t, "QZSF", false, 0), "entitlements")
	if code := got.ErrorCode(t); code != "E_AUTH" {
		t.Errorf("code = %s, want E_AUTH", code)
	}
	if got.Exit != 4 {
		t.Errorf("exit = %d, want 4", got.Exit)
	}
}
