package caixin

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/antchfx/htmlquery"
	xhtml "golang.org/x/net/html"
)

// The 文化 channel has its own two layouts: a section index and a columnist's
// page. Both server-render one screen and load the rest from an API behind a
// button, so both report `load_more_not_called` rather than implying the list
// is everything.

// cultureSection describes one measured 文化 section. A zero Subject means the
// section has no continuation feed -- the two serial-fiction directories are
// static pages, and inventing a subject id for them would imply an api call
// that does not exist.
type cultureSection struct {
	Name    string
	Subject int
}

// CultureSections is the exact set of measured sections.
var CultureSections = map[string]cultureSection{
	"zhuanlan":      {"专栏", 100798791},
	"novel":         {"文学", 100226579},
	"art":           {"艺术", 100000163},
	"books":         {"阅读", 100226577},
	"wh_philosophy": {"评论", 100319752},
	"newculture":    {"文化风向", 100319764},
	"xiaoshuojie":   {"小说界", 0},
	"chilong":       {"小说界·赤龙", 0},
	"dead":          {"逝者", 100000162},
	"columns":       {"专栏作家", 100000169},
}

var cultureSectionPath = regexp.MustCompile(`^/([a-z_]{1,32})(/|/index\.html)$`)

// cultureSectionURL canonicalizes a section url and names its key.
func cultureSectionURL(raw string) (canonical, key string, section cultureSection, ok bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil {
		return "", "", section, false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", "", section, false
	}
	if strings.ToLower(parsed.Hostname()) != "culture.caixin.com" {
		return "", "", section, false
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", section, false
	}
	match := cultureSectionPath.FindStringSubmatch(parsed.Path)
	if match == nil {
		return "", "", section, false
	}
	section, ok = CultureSections[match[1]]
	if !ok {
		return "", "", section, false
	}
	return "https://culture.caixin.com/" + match[1] + "/", match[1], section, true
}

// cultureArticleURL accepts an article link from a 文化 page.
func cultureArticleURL(base, value string) string {
	raw := absoluteURL(base, value)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	return articleURL(base, value)
}

// CultureSection reads one 文化 section index.
func (c *Client) CultureSection(ctx context.Context, pageURL string) (map[string]any, error) {
	canonical, key, section, ok := cultureSectionURL(pageURL)
	if !ok {
		return nil, invalid("culture-section only accepts the measured 文化 sections; " +
			"run `caixin-cli reference` for the list")
	}

	raw, err := c.do(ctx, requestSpec{
		Method:    http.MethodGet,
		URL:       canonical,
		Headers:   map[string]string{"Accept": "text/html,application/xhtml+xml"},
		Anonymous: true,
	})
	if err != nil {
		return nil, err
	}
	doc, err := xhtml.Parse(strings.NewReader(string(raw)))
	if err != nil {
		return nil, &APIError{Message: "could not parse the 文化 section page"}
	}

	modules, err := cultureSectionModules(doc, canonical, key)
	if err != nil {
		return nil, err
	}
	result := directoryResult(modules)
	result["section_key"] = key
	result["name"] = section.Name
	result["title"] = emptyToNil(firstXPathText(doc, "//title"))
	result["page"] = 1
	result["pagination"] = map[string]any{
		"page":                 1,
		"page_size":            25,
		"subject":              subjectOrNil(section.Subject),
		"explicit_only":        true,
		"load_more_not_called": true,
	}
	// Recomputed over the main lane only. The sidebar is shared across the
	// channel, so letting it set `latest_article_date` would make a stale
	// section look current; it is reported separately instead.
	if newest := laneLatestDate(modules, "main"); newest != nil {
		result["latest_article_date"] = newest
		if parsed, err := time.Parse("2006-01-02", newest.(string)); err == nil {
			result["stale_content"] = timeNow().Sub(parsed) > 30*24*time.Hour
		}
	}
	result["sidebar_latest_article_date"] = laneLatestDate(modules, "sidebar")
	result["scripts_not_executed"] = true
	result["linked_pages_not_fetched"] = true
	// The 文化 pages carry purchase widgets; this client never starts one.
	result["transactions_not_started"] = true
	result["session_used"] = false
	result["source"] = map[string]any{
		"requested_url": pageURL,
		"final_url":     canonical,
		"fetched_at":    time.Now().UTC().Format("2006-01-02T15:04:05+00:00"),
	}
	return attachClickConsumers(result), nil
}

// laneLatestDate reports the newest article date within one lane.
func laneLatestDate(modules []snapshotModule, lane string) any {
	newest := ""
	for _, module := range modules {
		if module.Lane != lane {
			continue
		}
		for _, item := range module.Items {
			link, _ := item["url"].(string)
			parsed, err := url.Parse(link)
			if err != nil {
				continue
			}
			if match := articleDateInPath.FindStringSubmatch(parsed.Path); match != nil {
				if match[1] > newest {
					newest = match[1]
				}
			}
		}
	}
	return emptyToNil(newest)
}

// cultureSectionModules extracts the main list and the shared sidebar.
func cultureSectionModules(doc *xhtml.Node, pageURL, key string) ([]snapshotModule, error) {
	root := firstElement(doc, classXPath("comMain"))
	if root == nil {
		return nil, &APIError{Message: "the 文化 section page is missing its comMain"}
	}
	left := firstElement(root, "./div["+classPredicate("conlf")+"]")
	right := firstElement(root, "./div["+classPredicate("conri")+"]")
	if left == nil || right == nil {
		return nil, &APIError{Message: "the 文化 section page is missing a content column"}
	}

	var modules []snapshotModule
	var articles []map[string]any
	for _, node := range htmlquery.Find(left, "./div["+classPredicate("stitXtuwen_list")+"]/dl") {
		if item := cultureSectionMainItem(node, pageURL); item != nil {
			articles = append(articles, item)
		}
	}
	if len(articles) > 0 {
		modules = append(modules, snapshotModule{
			Key: "culture-section." + key + ".articles", Name: "栏目文章",
			Lane: "main", Order: 0, State: "visible", Items: articles,
		})
	}

	if node := firstElement(right,
		"./div["+classPredicate("columnBox")+"][normalize-space(h3)='编辑推荐']"); node != nil {
		if items := cultureRecommendedItems(node, pageURL); len(items) > 0 {
			modules = append(modules, snapshotModule{
				Key: "culture-section." + key + ".recommended", Name: "编辑推荐",
				Lane: "sidebar", Order: 0, State: "visible",
				SharedSidebar: true, Items: items,
			})
		}
	}

	if node := firstElement(right,
		"./div["+classPredicate("columnBox")+"][normalize-space(h3)='最新文章']"); node != nil {
		var latest []map[string]any
		for _, row := range htmlquery.Find(node, "./ul["+classPredicate("list")+"]/li") {
			anchor := firstElement(row, "./a[@href][1]")
			if anchor == nil {
				continue
			}
			link := cultureArticleURL(pageURL, attr(anchor, "href"))
			title := nodeText(anchor)
			if link == "" || title == "" {
				continue
			}
			latest = append(latest, map[string]any{
				"title":        title,
				"url":          link,
				"image":        nil,
				"summary":      nil,
				"published_at": emptyToNil(firstXPathText(row, "./span[1]")),
				"badges":       []any{},
				"item_kind":    "article",
			})
		}
		if len(latest) > 0 {
			modules = append(modules, snapshotModule{
				Key: "culture-section." + key + ".latest", Name: "最新文章",
				Lane: "sidebar", Order: 1, State: "visible",
				SharedSidebar: true, Items: latest,
			})
		}
	}
	return modules, nil
}

// cultureSectionMainItem builds one card from the main list.
func cultureSectionMainItem(node *xhtml.Node, pageURL string) map[string]any {
	anchor := firstElement(node, "./dd/h4/a[@href][1]")
	if anchor == nil {
		return nil
	}
	link := cultureArticleURL(pageURL, attr(anchor, "href"))
	title := nodeText(anchor)
	if link == "" || title == "" {
		return nil
	}
	image := ""
	// The picture is used only when it points at the same article; a thumbnail
	// linking elsewhere belongs to an ad slot, not this card.
	if imageAnchor := firstElement(node,
		"./dd/div["+classPredicate("pic")+"]/a[@href][1]"); imageAnchor != nil {
		if cultureArticleURL(pageURL, attr(imageAnchor, "href")) == link {
			if imageNode := firstElement(imageAnchor, "./img[1]"); imageNode != nil {
				source := attr(imageNode, "data-src")
				if source == "" {
					source = attr(imageNode, "src")
				}
				image = cultureImageURL(pageURL, source)
			}
		}
	}
	badges := []any{}
	for _, badgeNode := range htmlquery.Find(node, "./dd/h4/*[contains(@class,'icon_')]") {
		badge := attr(badgeNode, "title")
		if badge == "" {
			badge = nodeText(badgeNode)
		}
		if badge == "" {
			continue
		}
		duplicate := false
		for _, existing := range badges {
			if existing == badge {
				duplicate = true
			}
		}
		if !duplicate {
			badges = append(badges, badge)
		}
	}
	return map[string]any{
		"title":        title,
		"url":          link,
		"image":        emptyToNil(image),
		"summary":      emptyToNil(firstXPathText(node, "./dd/p[1]")),
		"published_at": emptyToNil(firstXPathText(node, "./dd/span[1]")),
		"badges":       badges,
		"item_kind":    "article",
	}
}

// cultureRecommendedItems reads the shared 编辑推荐 block.
func cultureRecommendedItems(root *xhtml.Node, pageURL string) []map[string]any {
	var items []map[string]any
	seen := map[string]bool{}
	for _, anchor := range htmlquery.Find(root, "./div["+classPredicate("listWithPic")+"]/a[@href]") {
		link := cultureArticleURL(pageURL, attr(anchor, "href"))
		title := firstXPathText(anchor, "./span[1]")
		if link == "" || title == "" || seen[link] {
			continue
		}
		image := ""
		if imageNode := firstElement(anchor, "./img[1]"); imageNode != nil {
			source := attr(imageNode, "data-src")
			if source == "" {
				source = attr(imageNode, "src")
			}
			image = cultureImageURL(pageURL, source)
		}
		seen[link] = true
		items = append(items, map[string]any{
			"title":        title,
			"url":          link,
			"image":        emptyToNil(image),
			"summary":      nil,
			"published_at": nil,
			"badges":       []any{},
			"item_kind":    "article",
		})
	}

	// The block continues as a plain list below the picture cards. Each row may
	// carry two links: the section it came from, then the article. The article
	// is the last one; the first becomes the entry's section label.
	for _, row := range htmlquery.Find(root, "./ul["+classPredicate("list")+"]/li") {
		anchors := htmlquery.Find(row, "./a[@href]")
		if len(anchors) == 0 {
			continue
		}
		anchor := anchors[len(anchors)-1]
		link := cultureArticleURL(pageURL, attr(anchor, "href"))
		title := nodeText(anchor)
		if link == "" || title == "" || seen[link] {
			continue
		}
		item := map[string]any{
			"title":        title,
			"url":          link,
			"image":        nil,
			"summary":      nil,
			"published_at": nil,
			"badges":       []any{},
			"item_kind":    "article",
		}
		if len(anchors) > 1 {
			sectionURL, _, _, ok := cultureSectionURL(absoluteURL(pageURL, attr(anchors[0], "href")))
			sectionName := nodeText(anchors[0])
			if ok && sectionName != "" {
				item["section"] = map[string]any{"name": sectionName, "url": sectionURL}
			}
		}
		seen[link] = true
		items = append(items, item)
	}
	return items
}

// subjectOrNil reports a section's continuation subject, or nothing when the
// section has none.
func subjectOrNil(subject int) any {
	if subject == 0 {
		return nil
	}
	return subject
}
