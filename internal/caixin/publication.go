package caixin

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/antchfx/htmlquery"
	xhtml "golang.org/x/net/html"
)

// A magazine front is an archive: the current issue on top, then every back
// issue grouped by year. Most of those year columns are server-rendered but
// hidden behind a "load more" control, so each card records whether it was
// visible on arrival (`source_state`) and each module says how many were.
//
// That distinction is the whole point. The cards are all present in the markup,
// so reporting them is honest; claiming the reader would have *seen* them all
// is not.

// publicationContract describes one magazine front.
type publicationContract struct {
	Publication string
	Host        string
	CardKind    string
}

var publicationContracts = map[string]publicationContract{
	"weekly-index":   {"weekly", "weekly.caixin.com", "list"},
	"cnreform-index": {"cnreform", "cnreform.caixin.com", "list"},
	"bijiao-index":   {"bijiao", "bijiao.caixin.com", "comparison"},
}

var publicationYearID = regexp.MustCompile(`^wq(\d+)$`)
var publicationYear = regexp.MustCompile(`^20\d{2}$`)
var publishedCJK = regexp.MustCompile(`(20\d{2})年(\d{2})月(\d{2})日出版`)
var publishedPlain = regexp.MustCompile(`出版[:：]\s*(20\d{2}-\d{2}-\d{2})`)
var annualIssuePattern = regexp.MustCompile(`年度期号[:：]\s*0*(\d+)`)
var totalIssueLabel = regexp.MustCompile(`总期号[:：]\s*0*(\d+)`)
var displayNone = regexp.MustCompile(`(?i)display\s*:\s*none`)

// publicationIssueURL accepts an issue link on the expected magazine host.
func publicationIssueURL(base, value, expectedHost string) string {
	raw := absoluteURL(base, value)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	// The magazine host is an alias the pages still emit. It is kept as
	// published rather than rewritten: that is the url the reader would open,
	// and the issue adapter reads it either way.
	if host != expectedHost && host != "magazine.caixin.com" {
		return ""
	}
	parsed.Scheme = "https"
	parsed.Host = host
	canonical, publication := issueURL(parsed.String())
	if publication != publicationOf(expectedHost) {
		return ""
	}
	return canonical
}

// publicationOf names the magazine a host publishes.
func publicationOf(host string) string {
	return issuePublications[host]
}

// publicationArticleURL accepts a feature link on the magazine host.
func publicationArticleURL(base, value, expectedHost string) string {
	link := articleURL(base, value)
	if link == "" {
		return ""
	}
	parsed, err := url.Parse(link)
	if err != nil || strings.ToLower(parsed.Hostname()) != expectedHost {
		return ""
	}
	return link
}

// publicationRootItem builds one back-issue card.
func publicationRootItem(node *xhtml.Node, pageURL, expectedHost string) map[string]any {
	var link, title string
	// The bold link inside the description is the issue title; plain anchors
	// elsewhere in the card point at the same issue without a usable label.
	candidates := append(
		htmlquery.Find(node, ".//dd//b/a[@href]"),
		htmlquery.Find(node, ".//a[@href]")...)
	for _, anchor := range candidates {
		candidate := publicationIssueURL(pageURL, attr(anchor, "href"), expectedHost)
		if candidate == "" {
			continue
		}
		if link == "" {
			link = candidate
		}
		if text := nodeText(anchor); text != "" {
			link, title = candidate, text
			break
		}
	}
	if link == "" || title == "" {
		return nil
	}

	image := ""
	if imageNode := firstElement(node, ".//img"); imageNode != nil {
		source := attr(imageNode, "data-src")
		if source == "" {
			source = attr(imageNode, "src")
		}
		image = imageURL(pageURL, source)
	}

	text := nodeText(node)
	published := ""
	if match := publishedCJK.FindStringSubmatch(text); match != nil {
		published = match[1] + "-" + match[2] + "-" + match[3]
	} else if match := publishedPlain.FindStringSubmatch(text); match != nil {
		published = match[1]
	}
	var annual, total any
	if match := annualIssuePattern.FindStringSubmatch(text); match != nil {
		if value, err := strconv.Atoi(match[1]); err == nil {
			annual = value
		}
	}
	if match := totalIssueLabel.FindStringSubmatch(text); match != nil {
		if value, err := strconv.Atoi(match[1]); err == nil {
			total = value
		}
	}

	state := "initial"
	if displayNone.MatchString(attr(node, "style")) {
		state = "continuation-hidden"
	}
	return map[string]any{
		"title":           title,
		"url":             link,
		"image":           emptyToNil(image),
		"published_at":    emptyToNil(published),
		"annual_issue":    annual,
		"total_issue":     total,
		"source_state":    state,
		"fetch_supported": true,
		"access":          "directory_visible",
	}
}

// publicationFocus reads the current-issue block and its feature links.
func publicationFocus(doc *xhtml.Node, pageURL string, contract publicationContract) (map[string]any, []any) {
	focus := firstElement(doc, "("+classXPath("focus")+")[1]")
	if focus == nil {
		return nil, []any{}
	}
	issueAnchor := firstElement(focus, ".//div["+classPredicate("mi")+"]/a[@href]")
	if issueAnchor == nil {
		return nil, []any{}
	}
	issueLink := publicationIssueURL(pageURL, attr(issueAnchor, "href"), contract.Host)
	if issueLink == "" {
		return nil, []any{}
	}

	cover := ""
	// The cover lives inside the issue link itself; the focus block also holds
	// the feature thumbnails, and the first of those is not the cover.
	if imageNode := firstElement(issueAnchor, ".//img"); imageNode != nil {
		source := attr(imageNode, "data-src")
		if source == "" {
			source = attr(imageNode, "src")
		}
		cover = imageURL(pageURL, source)
	}
	published := ""
	if match := publishedPlain.FindStringSubmatch(nodeText(focus)); match != nil {
		published = match[1]
	}

	var lead map[string]any
	if leadNode := firstElement(focus, ".//div["+classPredicate("lf")+"]/dl"); leadNode != nil {
		leadAnchor := firstElement(leadNode, ".//dt/a[@href]")
		title := nodeText(leadAnchor)
		summary := firstXPathText(leadNode, ".//dd")
		var leadLink, kind string
		if contract.Publication == "bijiao" {
			// 比较 leads with another issue rather than an article.
			leadLink = publicationIssueURL(pageURL, attr(leadAnchor, "href"), contract.Host)
			kind = "issue_feature"
		} else {
			leadLink = publicationArticleURL(pageURL, attr(leadAnchor, "href"), contract.Host)
			kind = "article"
		}
		if title != "" && leadLink != "" {
			lead = map[string]any{
				"kind":    kind,
				"title":   title,
				"url":     leadLink,
				"summary": emptyToNil(summary),
				"access":  "directory_visible",
			}
		}
	}

	featured := []any{}
	for _, node := range htmlquery.Find(focus, ".//div["+classPredicate("ri")+"]/dl") {
		anchor := firstElement(node, ".//h4/a[@href]")
		link := publicationArticleURL(pageURL, attr(anchor, "href"), contract.Host)
		title := nodeText(anchor)
		if link == "" || title == "" {
			continue
		}
		featured = append(featured, map[string]any{
			"title":   title,
			"url":     link,
			"summary": emptyToNil(firstXPathText(node, ".//p")),
			"access":  "directory_visible",
		})
	}

	current := map[string]any{
		"url":             issueLink,
		"issue_number":    issueNumber(issueLink),
		"cover_image":     emptyToNil(cover),
		"published_at":    emptyToNil(published),
		"fetch_supported": true,
		"lead":            lead,
	}
	return current, featured
}

// publicationRootSnapshot extracts a magazine front.
func publicationRootSnapshot(doc *xhtml.Node, pageURL, pageKey string) (map[string]any, error) {
	contract := publicationContracts[pageKey]
	current, featured := publicationFocus(doc, pageURL, contract)

	root := firstElement(doc, classXPath("mainConLeft"))
	if root == nil {
		return nil, &APIError{Message: "the magazine front is missing its mainConLeft"}
	}

	years := map[string]string{}
	for _, node := range htmlquery.Find(root, ".//*["+classPredicate("wqNav")+"]/li[@id]") {
		identifier := attr(node, "id")
		year := nodeText(node)
		if publicationYearID.MatchString(identifier) && publicationYear.MatchString(year) {
			years["col_wq_"+strings.TrimPrefix(identifier, "wq")] = year
		}
	}

	columns := htmlquery.Find(root, ".//*["+classPredicate("wqCon")+"]/div[starts-with(@id,'col_wq_')]")
	if len(columns) == 0 {
		return nil, &APIError{Message: "the magazine front carries no yearly archive"}
	}

	var modules []any
	var allItems []map[string]any
	moduleCount := 0
	for order, column := range columns {
		selector := ".//div[" + classPredicate("xsjCon") + "]/ul/li"
		if contract.CardKind == "comparison" {
			selector = ".//div[" + classPredicate("gdqkCon") + "]/dl"
		}
		var items []map[string]any
		for _, node := range htmlquery.Find(column, selector) {
			if item := publicationRootItem(node, pageURL, contract.Host); item != nil {
				items = append(items, item)
			}
		}
		if len(items) == 0 {
			continue
		}
		year := years[attr(column, "id")]
		if year == "" {
			if parsed, err := url.Parse(asString(items[0]["url"])); err == nil {
				parts := strings.Split(parsed.Path, "/")
				if len(parts) > 1 {
					year = parts[1]
				}
			}
		}
		state := "visible"
		if strings.Contains(strings.ToLower(attr(column, "style")), "none") {
			state = "hidden"
		}
		columnID := attr(column, "id")
		control := regexp.MustCompile(`\bmore\(\s*['"]` + regexp.QuoteMeta(columnID) + `['"]\s*\)`)
		hasControl := false
		for _, node := range htmlquery.Find(column, ".//*["+classPredicate("load_more")+"]") {
			if control.MatchString(attr(node, "onclick")) {
				hasControl = true
				break
			}
		}
		visible := 0
		for _, item := range items {
			if item["source_state"] == "initial" {
				visible++
			}
		}
		continuation := visible < len(items) && hasControl

		entries := make([]any, 0, len(items))
		for _, item := range items {
			entries = append(entries, item)
		}
		module := map[string]any{
			"key":                     pageKey + "." + year,
			"name":                    year,
			"lane":                    "main",
			"order":                   order,
			"state":                   state,
			"initially_visible_count": visible,
			"continuation_available":  continuation,
			"items":                   entries,
		}
		if continuation {
			module["continuation_evidence"] = "server_rendered_hidden_cards_with_load_more_control"
			if pageKey == "weekly-index" {
				// The hidden cards are already here, so the "load more" batches
				// are satisfied from this page rather than by another request.
				hidden := len(items) - visible
				batches := []any{}
				for batch := 1; batch <= (hidden+11)/12; batch++ {
					batches = append(batches, batch)
				}
				module["continuation_consumed_locally"] = true
				module["continuation_batch_size"] = 12
				module["continuation_batches"] = batches
			}
		}
		modules = append(modules, module)
		moduleCount++
		allItems = append(allItems, items...)
	}

	var latest map[string]any
	unique := map[string]bool{}
	for _, item := range allItems {
		unique[asString(item["url"])] = true
		published := asString(item["published_at"])
		if published == "" {
			continue
		}
		if latest == nil || published > asString(latest["published_at"]) {
			latest = item
		}
	}

	issueEntries := len(allItems)
	if current != nil {
		issueEntries++
	}
	articleEntries := len(featured)
	if current != nil {
		if lead, ok := current["lead"].(map[string]any); ok && lead["kind"] == "article" {
			articleEntries++
		}
	}

	var latestDate, latestURL any
	switch {
	case current != nil:
		latestDate, latestURL = current["published_at"], current["url"]
	case latest != nil:
		latestDate, latestURL = latest["published_at"], latest["url"]
	}

	return map[string]any{
		"publication":                     contract.Publication,
		"directory_only":                  true,
		"current_issue":                   current,
		"featured_articles":               featured,
		"modules":                         modules,
		"modules_count":                   moduleCount,
		"items_count":                     len(allItems),
		"issues_count":                    len(allItems),
		"archive_cards_count":             len(allItems),
		"archive_unique_issue_urls_count": len(unique),
		"issue_entries_count":             issueEntries,
		"article_entries_count":           articleEntries,
		"total_entries_count":             issueEntries + articleEntries,
		"latest_issue_date":               latestDate,
		"latest_issue_url":                latestURL,
	}, nil
}
