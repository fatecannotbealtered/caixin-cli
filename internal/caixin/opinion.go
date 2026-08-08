package caixin

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/antchfx/htmlquery"
	xhtml "golang.org/x/net/html"
)

// The 观点 front is a channel front with a roster attached: three article lanes
// down the left, then the recommended columnists, the two sister magazines, and
// the hot topics down the side.
//
// The two magazine blocks are server-rendered but only one is shown at a time,
// so each carries its own `state`. Reporting the hidden one as visible would
// claim the reader saw a list that was behind a tab.

// opinionModules extracts the 观点 channel front.
func opinionModules(doc *xhtml.Node, pageURL string) ([]snapshotModule, error) {
	root := firstElement(doc,
		"//div["+classPredicate("indexMain")+"]/div["+classPredicate("indexMainCon")+"]")
	if root == nil {
		return nil, &APIError{Message: "the 观点 front page is missing its indexMainCon"}
	}

	var modules []snapshotModule
	laneOrders := map[string]int{"main": 0, "sidebar": 0}
	appendModule := func(key, name, lane, state string, items []map[string]any) {
		if len(items) == 0 {
			return
		}
		modules = append(modules, snapshotModule{
			Key: key, Name: name, Lane: lane, Order: laneOrders[lane],
			State: state, Items: items,
		})
		laneOrders[lane]++
	}

	// parsedItems additionally resolves the card picture's own destination,
	// which on this page is not always the headline's.
	parsedItems := func(nodes []*xhtml.Node, options itemOptions, imageClickXPaths []string) []map[string]any {
		var items []map[string]any
		for _, node := range nodes {
			item := editorialItem(node, pageURL, options)
			if item == nil {
				continue
			}
			if len(imageClickXPaths) > 0 {
				if anchor := firstElement(node, imageClickXPaths...); anchor != nil {
					route := ClassifyURL(absoluteURL(pageURL, attr(anchor, "href")))
					if route.Supported && route.CanonicalURL != item["url"] {
						item["image_click_url"] = route.CanonicalURL
						item["link_mismatch"] = true
					}
				}
			}
			items = append(items, item)
		}
		return items
	}

	if left := firstElement(root, ".//div["+classPredicate("mainConLeft")+"]"); left != nil {
		appendModule("opinion.focus", "焦点", "main", "visible", parsedItems(
			htmlquery.Find(left, ".//*["+classPredicate("gdTop")+"]//dl"),
			itemOptions{
				AnchorXPaths:  []string{".//dd/h4/a[@href]"},
				ImageXPaths:   []string{".//dt//img"},
				SummaryXPaths: []string{".//dd/p"},
			},
			[]string{".//dt/a[@href]"}))

		appendModule("opinion.recommended", "推荐", "main", "visible", parsedItems(
			htmlquery.Find(left, ".//*["+classPredicate("tuijianCon")+"]/div[.//dl]"),
			itemOptions{
				AnchorXPaths:  []string{".//dl/dt/a[@href]"},
				ImageXPaths:   []string{".//dl/dd//img", ".//img"},
				SummaryXPaths: []string{".//dl/dd/p", ".//p"},
			}, nil))

		appendModule("opinion.latest", "最新", "main", "visible", parsedItems(
			htmlquery.Find(left, ".//*[@id='listArticle']/*"),
			itemOptions{
				AnchorXPaths:  []string{".//h4/a[1][@href]"},
				ImageXPaths:   []string{".//*[contains(@class,'pic')]//img", ".//img"},
				SummaryXPaths: []string{".//p"},
				BadgeXPaths:   []string{".//h4//*[contains(@class,'icon_')]"},
			},
			[]string{".//*[contains(@class,'pic')]/a[@href]"}))
	}

	var authors []map[string]any
	if container := firstElement(root, ".//div["+classPredicate("zuozhe")+"]"); container != nil {
		for _, node := range htmlquery.Find(container, "./ul/li") {
			anchor := firstElement(node, "./a[@href]")
			if anchor == nil {
				continue
			}
			link := legacyOpinionURL(pageURL, attr(anchor, "href"),
				"opinion.caixin.com", opinionAuthorLegacyPath)
			title := firstXPathText(anchor, ".//span")
			if link == "" || title == "" {
				continue
			}
			image := ""
			if node := firstElement(anchor, ".//img"); node != nil {
				image = legacyOpinionURL(pageURL, attr(node, "src"),
					"www.caixin.com", opinionAuthorPortraitPath)
			}
			authors = append(authors, map[string]any{
				"title":   title,
				"url":     link,
				"image":   emptyToNil(image),
				"summary": emptyToNil(firstXPathText(anchor, ".//p")),
				"badges":  []any{},
			})
		}
	}
	appendModule("opinion.authors", "推荐作者", "sidebar", "visible", authors)

	if magazine := firstElement(root, ".//div["+classPredicate("magBox")+"]"); magazine != nil {
		for _, block := range []struct{ id, key, name string }{
			{"col_mag_1", "opinion.cnreform", "中国改革"},
			{"col_mag_2", "opinion.bijiao", "比较"},
		} {
			container := firstElement(magazine, ".//*[@id='"+block.id+"']")
			if container == nil {
				continue
			}
			state := "visible"
			style := strings.ToLower(strings.ReplaceAll(attr(container, "style"), " ", ""))
			if strings.Contains(style, "display:none") {
				state = "hidden"
			}
			appendModule(block.key, block.name, "sidebar", state, sidebarItems(container, pageURL))
		}
	}

	var topics []map[string]any
	if container := firstElement(root, ".//div["+classPredicate("redian")+"]"); container != nil {
		// The block mixes a stray `<li>` into its `<dl>`, which is invalid
		// markup that different parsers place differently. The list is
		// therefore addressed positionally across the whole block rather than
		// among one parent's children.
		var anchors []*xhtml.Node
		if rows := htmlquery.Find(container, ".//li"); len(rows) > 0 {
			// The lead topic is the first row's first link; the rest of the
			// block is the trailing row, whose first link is its own heading.
			if first := htmlquery.Find(rows[0], "./a[1][@href]"); len(first) > 0 {
				anchors = append(anchors, first[0])
			}
			anchors = append(anchors,
				htmlquery.Find(rows[len(rows)-1], "./a[position()>1][@href]")...)
		}
		topicImage := firstElement(container, "./dl/dt//img")
		seen := map[string]bool{}
		for index, anchor := range anchors {
			link := caixinOutputURL(pageURL, attr(anchor, "href"))
			title := nodeText(anchor)
			if link == "" || title == "" || seen[link] {
				continue
			}
			// Only the lead topic has a picture; the rest are text links.
			image := ""
			if index == 0 && topicImage != nil {
				source := attr(topicImage, "data-src")
				if source == "" {
					source = attr(topicImage, "src")
				}
				image = caixinOutputURL(pageURL, source)
			}
			seen[link] = true
			topics = append(topics, map[string]any{
				"title":   title,
				"url":     link,
				"image":   emptyToNil(image),
				"summary": nil,
				"badges":  []any{},
			})
		}
	}
	appendModule("opinion.topics", "热点专题", "sidebar", "visible", topics)
	return modules, nil
}

// opinionAuthorLegacyPath is the columnist page shape the roster links to.
var opinionAuthorLegacyPath = regexp.MustCompile(`^/[A-Za-z0-9_-]+/?$`)

// opinionAuthorPortraitPath is where the roster's portraits are published.
var opinionAuthorPortraitPath = regexp.MustCompile(`(?i)^/upload/zhuanlan/[^/]+\.(jpe?g|png|webp)$`)

// legacyOpinionURL accepts a link on one exact host with one exact path shape.
//
// The 观点 roster predates the site's current markup and mixes portraits,
// author pages, and campaign links in the same list, so each is admitted by the
// shape it is known to have rather than by a general url check.
func legacyOpinionURL(base, value, host string, path *regexp.Regexp) string {
	raw := absoluteURL(base, value)
	if raw == "" || strings.ContainsAny(raw, " \t\n\r") {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	if strings.ToLower(parsed.Hostname()) != host {
		return ""
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	if port := parsed.Port(); port != "" && port != "80" && port != "443" {
		return ""
	}
	if !path.MatchString(parsed.Path) {
		return ""
	}
	parsed.Scheme = "https"
	parsed.Host = host
	return parsed.String()
}
