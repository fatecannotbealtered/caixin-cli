package caixin

// The "load more" button behind several directories calls one gateway endpoint
// with the parameters the page itself printed. This client never presses the
// button on the caller's behalf; when a caller asks for page 2 explicitly, the
// same request is made and its answer is checked against what was asked for.

// gatewayRows unwraps a continuation response and verifies it answers the
// request that was made.
//
// The window is checked because the endpoint silently ignores an out-of-range
// start: without the check, page 9 of a 3-page list would return page 1 and read
// as if it were page 9.
func gatewayRows(value map[string]any, start, count int, label string) ([]any, any, any, error) {
	rows, ok := value["datas"].([]any)
	if !ok || len(rows) > count {
		return nil, nil, nil, &APIError{Message: "the " + label + " continuation response changed shape"}
	}
	if reported, present := safeInt(value["start"]); present && reported != start {
		return nil, nil, nil, &APIError{Message: "the " + label + " continuation answered a different start"}
	}
	if reported, present := safeInt(value["count"]); present && reported != count {
		return nil, nil, nil, &APIError{Message: "the " + label + " continuation answered a different count"}
	}
	return rows, intOrNil(value["maxes"]), emptyToNil(plainText(value["version"])), nil
}

// gatewayItemOptions selects the parts of a continuation row a caller wants.
type gatewayItemOptions struct {
	ExpectedHost   string
	IncludeSummary bool
	MaxImages      int
	// BadgeMode names how this endpoint reports access. The two vocabularies
	// disagree, so guessing one would mislabel paid articles as free.
	BadgeMode string
}

// gatewayItem normalizes one continuation row.
func gatewayItem(row any, pageURL string, options gatewayItemOptions) map[string]any {
	fields, ok := row.(map[string]any)
	if !ok {
		return nil
	}
	link := validateArticleURL(plainText(fields["link"]))
	if link == "" {
		return nil
	}
	if options.ExpectedHost != "" && hostOf(link) != options.ExpectedHost {
		return nil
	}
	title := plainText(fields["desc"])
	if title == "" {
		return nil
	}

	images := []any{}
	seen := map[string]bool{}
	if pict, ok := fields["pict"].(map[string]any); ok && options.MaxImages > 0 {
		candidates, _ := pict["imgs"].([]any)
		for _, candidate := range candidates {
			if len(images) >= options.MaxImages {
				break
			}
			entry, ok := candidate.(map[string]any)
			if !ok {
				continue
			}
			image := cultureImageURL(pageURL, plainText(entry["url"]))
			if image == "" || seen[image] {
				continue
			}
			seen[image] = true
			images = append(images, image)
		}
	}

	access, hasAccess := safeInt(fields["attr"])
	freeUntil := plainText(fields["freeTime"])
	badges := []any{}
	switch options.BadgeMode {
	case "home":
		switch {
		case hasAccess && access == 5:
			badges = append(badges, "收费文章")
		case freeUntil != "":
			badges = append(badges, "限时免费")
		default:
			badges = append(badges, "免费文章")
		}
	case "opinion":
		if hasAccess && (access == 0 || access == 4) {
			badges = append(badges, "免费文章")
		}
	}

	var lead any
	if len(images) > 0 {
		lead = images[0]
	}
	item := map[string]any{
		"title":        title,
		"url":          link,
		"image":        lead,
		"published_at": emptyToNil(plainText(fields["time"])),
		"badges":       badges,
		"item_kind":    "article",
	}
	if options.MaxImages > 1 {
		item["images"] = images
	}
	if options.IncludeSummary {
		item["summary"] = emptyToNil(plainText(fields["summ"]))
	}
	return item
}
