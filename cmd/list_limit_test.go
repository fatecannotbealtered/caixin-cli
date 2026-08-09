package cmd

import "testing"

func TestRemainingListCommandsExposeLimit(t *testing.T) {
	root := (&application{}).rootCommand()
	for _, name := range []string{
		"newscroll",
		"bloggers-directory",
		"opinion-author",
		"opinion-author-directory",
		"opinion-columns",
		"opinion-upfront",
		"video-section",
	} {
		command, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatalf("find %s: %v", name, err)
		}
		if command.Flags().Lookup("limit") == nil {
			t.Errorf("%s has no --limit flag", name)
		}
	}
}

func TestLimitListResultRejectsNonPositiveLimit(t *testing.T) {
	for _, limit := range []int{0, -1} {
		err := limitListResult(map[string]any{"items": []any{}}, limit)
		if got := asCLIError(err); got.Code != "E_VALIDATION" {
			t.Errorf("limit %d: code = %s, want E_VALIDATION", limit, got.Code)
		}
	}
}

func TestRemainingListCommandsRejectNonPositiveLimitBeforeReading(t *testing.T) {
	for _, args := range [][]string{
		{"newscroll", "--limit", "0"},
		{"bloggers-directory", "--limit", "0"},
		{"opinion-author", "https://opinion.caixin.com/wuqianli_mjxx/", "--limit", "0"},
		{"opinion-author-directory", "https://opinion.caixin.com/columns-test/", "--limit", "0"},
		{"opinion-columns", "https://opinion.caixin.com/columns/", "--limit", "0"},
		{"opinion-upfront", "https://opinion.caixin.com/upfront/", "--limit", "0"},
		{"video-section", "https://video.caixin.com/dr/", "--limit", "0"},
	} {
		got := run(t, newMockUpstream(t), args...)
		if got.Exit != 2 || got.ErrorCode(t) != "E_VALIDATION" {
			t.Errorf("%v = exit %d/code %s, want 2/E_VALIDATION", args, got.Exit, got.ErrorCode(t))
		}
	}
}

func TestLimitListResultTruncatesTopLevelItems(t *testing.T) {
	result := map[string]any{
		"page":     2,
		"articles": []any{"a", "b", "c"},
	}
	if err := limitListResult(result, 2); err != nil {
		t.Fatal(err)
	}
	articles, _ := result["articles"].([]any)
	if len(articles) != 2 || result["count"] != 2 || result["has_more"] != true ||
		result["next_page"] != 3 || result["truncated"] != true {
		t.Errorf("limited result = %#v", result)
	}
}

func TestLimitListResultTruncatesModulesAcrossTheResult(t *testing.T) {
	result := map[string]any{
		"page": 1,
		"modules": []any{
			map[string]any{"items": []any{"a", "b"}},
			map[string]any{"items": []any{"c", "d"}},
		},
	}
	if err := limitListResult(result, 3); err != nil {
		t.Fatal(err)
	}
	modules := result["modules"].([]any)
	first := modules[0].(map[string]any)["items"].([]any)
	second := modules[1].(map[string]any)["items"].([]any)
	if len(first) != 2 || len(second) != 1 || result["count"] != 3 || result["has_more"] != true {
		t.Errorf("limited result = %#v", result)
	}
}
