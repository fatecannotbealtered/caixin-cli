package caixin

import (
	"testing"
	"time"
)

func TestPublicTimeFieldsUseISO8601(t *testing.T) {
	cases := []map[string]any{
		NormalizeFrontlineItem(map[string]any{
			"oneline_news_code": "0123456789abcdef0123456789abcdef",
			"ts":                1785913260000,
		}),
		relatedNews(map[string]any{"extra": map[string]any{"newsList": []any{
			map[string]any{"title": "news", "url": "https://companies.caixin.com/a", "time": 1786007795000},
		}}}),
		bloggersDirectoryItem(map[string]any{
			"id": 1, "name": "author", "authorUrl": "https://n.blog.caixin.com/", "lastestTime": 1785997408,
		}),
	}
	for index, value := range cases {
		if _, exists := value["published_at_ms"]; exists {
			t.Errorf("case %d still exposes published_at_ms", index)
		}
		published, _ := value["published_at"].(string)
		if _, err := time.Parse(time.RFC3339, published); err != nil {
			t.Errorf("case %d published_at = %q, want RFC3339: %v", index, published, err)
		}
	}
}

func TestDisplayOnlyTimeFieldsUseLabels(t *testing.T) {
	frontline := NormalizeFrontlineItem(map[string]any{
		"oneline_news_code": "0123456789abcdef0123456789abcdef",
		"ts":                1785913260000,
		"date":              "2026/08/05",
		"time":              "15:01",
	})
	if frontline["date_label"] != "2026/08/05" || frontline["time_label"] != "15:01" {
		t.Errorf("frontline labels = %#v", frontline)
	}
	if _, exists := frontline["date"]; exists {
		t.Error("frontline still exposes display text as date")
	}
	if _, exists := frontline["time"]; exists {
		t.Error("frontline still exposes display text as time")
	}

	feed := NormalizeCXDataItem(map[string]any{
		"title": "item", "text": "text", "time": 1785988285,
		"date": "2026/08/06", "intervalTime": "3分钟前",
	}, "frontline")
	if feed["date_label"] != "2026/08/06" || feed["interval_label"] != "3分钟前" {
		t.Errorf("feed labels = %#v", feed)
	}
	for _, key := range []string{"date", "display_time", "interval_time"} {
		if _, exists := feed[key]; exists {
			t.Errorf("feed still exposes display text as %s", key)
		}
	}
}
