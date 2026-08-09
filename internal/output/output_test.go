package output

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestPrinterSuccess_NormalizesIDFieldsRecursively(t *testing.T) {
	var out bytes.Buffer
	printer := NewPrinter(&out, Options{Format: FormatJSON, Compact: true})
	err := printer.Success(map[string]any{
		"id":        42,
		"nested":    map[string]any{"article_id": 7, "valid": true},
		"items":     []any{map[string]any{"content_id": 123}},
		"topic_ids": []any{1, 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	data, _ := envelope["data"].(map[string]any)
	if data["id"] != "42" {
		t.Errorf("id = %#v, want string 42", data["id"])
	}
	nested, _ := data["nested"].(map[string]any)
	if nested["article_id"] != "7" || nested["valid"] != true {
		t.Errorf("nested = %#v", nested)
	}
	items, _ := data["items"].([]any)
	first, _ := items[0].(map[string]any)
	if first["content_id"] != "123" {
		t.Errorf("items[0].content_id = %#v", first["content_id"])
	}
	ids, _ := data["topic_ids"].([]any)
	if len(ids) != 2 || ids[0] != "1" || ids[1] != "2" {
		t.Errorf("topic_ids = %#v", ids)
	}
}

func TestPrinterSuccess_NormalizesSemanticTimesRecursively(t *testing.T) {
	var out bytes.Buffer
	printer := NewPrinter(&out, Options{Format: FormatJSON, Compact: true})
	err := printer.Success(map[string]any{
		"published_at_unix": 1785988285,
		"nested": map[string]any{
			"published_at": "2026年08月03日 19:06",
			"updated_at":   "2026-08-06",
			"start_time":   "2026-01-01",
		},
		"items": []any{map[string]any{"published_at": "08月03日 19:00"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	data := envelope["data"].(map[string]any)
	if data["published_at"] != "2026-08-06T03:51:25Z" {
		t.Errorf("published_at = %#v", data["published_at"])
	}
	if _, exists := data["published_at_unix"]; exists {
		t.Error("published_at_unix was not removed")
	}
	nested := data["nested"].(map[string]any)
	if nested["published_at"] != "2026-08-03T11:06:00Z" ||
		nested["updated_at"] != "2026-08-06T00:00:00Z" ||
		nested["start_time"] != "2026-01-01T00:00:00Z" {
		t.Errorf("nested times = %#v", nested)
	}
	item := data["items"].([]any)[0].(map[string]any)
	if item["published_label"] != "08月03日 19:00" {
		t.Errorf("display-only date = %#v, want published_label", item)
	}
	if _, exists := item["published_at"]; exists {
		t.Error("display-only date still uses published_at")
	}
}
