package caixin

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/antchfx/htmlquery"
	xhtml "golang.org/x/net/html"
)

// The 文化 front is tabbed: six lists are server-rendered, one is shown and
// five wait behind a tab. All six are reported, each with the `state` it was
// served in, so a caller can tell "on screen" from "in the markup".
//
// The first tab additionally continues through a script. That is recorded as
// evidence rather than followed -- this client does not run the page's scripts.

// cultureTabs are the six tabs the front page renders, in page order. The
// `more` url is what each tab's "see all" link is allowed to point at.
var cultureTabs = []struct{ slug, name, moreURL string }{
	{"all", "全部", ""},
	{"column", "专栏", "https://culture.caixin.com/zhuanlan/"},
	{"literature", "文学", "https://culture.caixin.com/novel/"},
	{"art", "艺术", "https://culture.caixin.com/art/"},
	{"reading", "阅读", "https://culture.caixin.com/books/"},
	{"comment", "评论", "https://culture.caixin.com/wh_philosophy/"},
}

// cultureContinuationEvidence names what was seen, not what was done.
const cultureContinuationEvidence = "server_rendered_javascript_load_more_control"

// cultureLoadMore is the script hook behind the first tab's "more" control.
var cultureLoadMore = regexp.MustCompile(`^loadMoreNewses\(\d+,\d+,\d+,\d+\);?$`)

// cultureSectionURLs are the section pages a tab or a card may link to.
func cultureSectionURLs() map[string]bool {
	allowed := map[string]bool{}
	for _, tab := range cultureTabs[1:] {
		allowed[tab.moreURL] = true
		allowed[tab.moreURL+"index.html"] = true
	}
	return allowed
}

// culturePageURL accepts a 文化 page link, but only one already expected.
func culturePageURL(base, value string, allowed map[string]bool) string {
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
	if strings.ToLower(parsed.Hostname()) != "culture.caixin.com" {
		return ""
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	if port := parsed.Port(); port != "" && port != "80" && port != "443" {
		return ""
	}
	parsed.Scheme = "https"
	parsed.Host = "culture.caixin.com"
	canonical := parsed.String()
	if !allowed[canonical] {
		return ""
	}
	return canonical
}

// cultureItemOptions extends the shared card options with the extra fields the
// 文化 cards carry.
type cultureItemOptions struct {
	AnchorXPaths  []string
	ImageXPaths   []string
	SummaryXPaths []string
	MetaXPaths    []string
	SectionXPaths []string
	CommentXPaths []string
	BadgeXPaths   []string
}

// cultureItem builds one 文化 card.
func cultureItem(node *xhtml.Node, pageURL string, options cultureItemOptions) map[string]any {
	var link, title string
	for _, xpath := range options.AnchorXPaths {
		for _, candidate := range htmlquery.Find(node, xpath) {
			candidateURL := cultureArticleURL(pageURL, attr(candidate, "href"))
			candidateTitle := nodeText(candidate)
			if candidateURL != "" && candidateTitle != "" {
				link, title = candidateURL, candidateTitle
				break
			}
		}
		if link != "" {
			break
		}
	}
	if link == "" {
		return nil
	}

	imageXPaths := options.ImageXPaths
	if imageXPaths == nil {
		imageXPaths = []string{".//img"}
	}
	image := ""
	for _, xpath := range imageXPaths {
		imageNode := firstElement(node, xpath)
		if imageNode == nil {
			continue
		}
		source := attr(imageNode, "data-src")
		if source == "" {
			source = attr(imageNode, "src")
		}
		if image = cultureImageURL(pageURL, source); image != "" {
			break
		}
	}

	summary := ""
	for _, xpath := range options.SummaryXPaths {
		if value := firstXPathText(node, xpath); value != "" && value != title {
			summary = value
			break
		}
	}

	badges := []any{}
	for _, xpath := range options.BadgeXPaths {
		for _, candidate := range htmlquery.Find(node, xpath) {
			badge := attr(candidate, "title")
			if badge == "" {
				badge = nodeText(candidate)
			}
			if badge == "" {
				classes := map[string]bool{}
				for _, class := range strings.Fields(attr(candidate, "class")) {
					classes[class] = true
				}
				switch {
				case classes["icon_key"]:
					badge = "收费文章"
				case classes["icon_free"]:
					badge = "免费文章"
				}
			}
			if badge == "" || badge == title || containsAny(badges, badge) {
				continue
			}
			badges = append(badges, badge)
		}
	}

	item := map[string]any{
		"title":   title,
		"url":     link,
		"image":   emptyToNil(image),
		"summary": emptyToNil(summary),
		"badges":  badges,
		"access":  "directory_visible",
	}
	for _, xpath := range options.MetaXPaths {
		metaNode := firstElement(node, xpath)
		if metaNode == nil {
			continue
		}
		meta := directText(metaNode)
		if meta == "" {
			meta = nodeText(metaNode)
		}
		if meta != "" {
			item["meta_text"] = meta
			break
		}
	}
	sections := cultureSectionURLs()
	for _, xpath := range options.SectionXPaths {
		anchor := firstElement(node, xpath)
		if anchor == nil {
			continue
		}
		sectionURL := culturePageURL(pageURL, attr(anchor, "href"), sections)
		sectionName := nodeText(anchor)
		if sectionURL != "" && sectionName != "" {
			item["section"] = map[string]any{"name": sectionName, "url": sectionURL}
			break
		}
	}
	for _, xpath := range options.CommentXPaths {
		commentNode := firstElement(node, xpath)
		if commentNode == nil {
			continue
		}
		if tid := attr(commentNode, "tid"); tid != "" && isDigits(tid) {
			item["comment_tid"] = tid
			break
		}
	}
	return item
}

// isDigits reports whether a string is a bare decimal number.
func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

// cultureSnapshot extracts the 文化 channel front.
func cultureSnapshot(doc *xhtml.Node, pageURL string) (map[string]any, error) {
	root := firstElement(doc,
		"//div["+classPredicate("indexMain")+"]/div["+classPredicate("indexMainCon")+"]")
	if root == nil {
		return nil, &APIError{Message: "the 文化 front page is missing its indexMain/indexMainCon"}
	}

	var modules []snapshotModule
	appendModule := func(module snapshotModule) {
		if len(module.Items) > 0 {
			modules = append(modules, module)
		}
	}

	if top := firstElement(root, ".//div["+classPredicate("mainConLeft")+
		"]/div["+classPredicate("topNews")+"]"); top != nil {
		lead := cultureItem(top, pageURL, cultureItemOptions{
			AnchorXPaths:  []string{"./dl/dt/a[1][@href]"},
			ImageXPaths:   []string{"./dl/dd//*[contains(@class,'pic')]//img"},
			SummaryXPaths: []string{"./dl/dd//*[contains(@class,'txt')]/p[not(ancestor::span)][1]"},
			MetaXPaths:    []string{"./dl/dd//*[contains(@class,'txt')]/span"},
		})
		if lead != nil {
			appendModule(snapshotModule{Key: "culture.top", Name: "头条", Lane: "lead",
				Order: 0, State: "visible", Items: []map[string]any{lead}})
		}
		// Only the first related link is taken: the rest of that list is the
		// same headline repeated in the tab below.
		var related []map[string]any
		for _, node := range firstN(htmlquery.Find(top,
			"./dl/dd//*[contains(@class,'txt')]//ul/li"), 1) {
			if item := cultureItem(node, pageURL, cultureItemOptions{
				AnchorXPaths: []string{".//a[1][@href]"},
				ImageXPaths:  []string{},
			}); item != nil {
				related = append(related, item)
			}
		}
		appendModule(snapshotModule{Key: "culture.top-related", Name: "头条相关",
			Lane: "lead", Order: 1, State: "visible", Items: related})
	}

	laneRoot := firstElement(root, ".//div["+classPredicate("mainConLeft")+
		"]//div["+classPredicate("yaowen")+"]//div["+classPredicate("ywList")+"]")
	if laneRoot == nil {
		return nil, &APIError{Message: "the 文化 front page is missing its ywList"}
	}
	for order, tab := range cultureTabs {
		container := firstElement(laneRoot, "./div[@id='container_"+itoa(order)+"']")
		if container == nil {
			continue
		}
		var items []map[string]any
		for _, node := range firstN(htmlquery.Find(container,
			".//*["+classPredicate("boxa")+"]"), 28) {
			if item := cultureItem(node, pageURL, cultureItemOptions{
				AnchorXPaths:  []string{".//h4/a[1][@href]"},
				ImageXPaths:   []string{".//*[contains(@class,'pic')]//img", ".//img"},
				SummaryXPaths: []string{"./p", ".//p"},
				MetaXPaths:    []string{"./span"},
				SectionXPaths: []string{".//h4/i/a[1][@href]"},
				CommentXPaths: []string{".//*[@tid][1]"},
				BadgeXPaths:   []string{".//h4//*[contains(@class,'icon_')]"},
			}); item != nil {
				items = append(items, item)
			}
		}
		module := snapshotModule{
			Key: "culture.tab-" + tab.slug, Name: tab.name, Lane: "main",
			Order: order, State: "hidden", Items: items,
		}
		if order == 0 {
			module.State = "visible"
		}
		moreAnchor := firstElement(container,
			".//*["+classPredicate("moreArt")+"]/a[1][@href]")
		switch {
		case order == 0 && moreAnchor != nil:
			href := strings.ToLower(strings.TrimSpace(attr(moreAnchor, "href")))
			onclick := strings.TrimSpace(attr(moreAnchor, "onclick"))
			if href == "javascript:void(0);" && cultureLoadMore.MatchString(onclick) {
				module.ContinuationAvailable = true
				module.ContinuationEvidence = cultureContinuationEvidence
			}
		case tab.moreURL != "" && moreAnchor != nil:
			module.MoreURL = culturePageURL(pageURL, attr(moreAnchor, "href"),
				map[string]bool{tab.moreURL: true})
		}
		appendModule(module)
	}

	right := firstElement(root, ".//div["+classPredicate("mainConRig")+"]")
	if right != nil {
		var trends []map[string]any
		for _, node := range firstN(htmlquery.Find(right,
			"./div["+classPredicate("cailuCon")+"]//div["+classPredicate("dlCon")+"]/dl"), 3) {
			if item := cultureItem(node, pageURL, cultureItemOptions{
				AnchorXPaths:  []string{"./dt/a[1][@href]"},
				ImageXPaths:   []string{"./dd//*[contains(@class,'pic')]//img"},
				SummaryXPaths: []string{"./dd/p"},
				MetaXPaths:    []string{"./dd/span"},
				CommentXPaths: []string{".//*[@tid][1]"},
			}); item != nil {
				trends = append(trends, item)
			}
		}
		appendModule(snapshotModule{Key: "culture.trends", Name: "文化风向",
			Lane: "sidebar", Order: 0, State: "visible", Items: trends})

		if shizhe := firstElement(right, "./div["+classPredicate("shizhe")+"]"); shizhe != nil {
			containers := htmlquery.Find(shizhe, "./div["+classPredicate("shizheCon")+"]")
			if len(containers) > 0 {
				// The serial fiction block links one fixed directory; anything
				// else in that slot is an ad.
				anchor := firstElement(containers[0], ".//p/a[1][@href]")
				link := ""
				title := ""
				if anchor != nil {
					link = culturePageURL(pageURL, attr(anchor, "href"),
						map[string]bool{"https://culture.caixin.com/chilong/": true})
					title = nodeText(anchor)
				}
				image := ""
				if node := firstElement(containers[0], ".//img"); node != nil {
					source := attr(node, "data-src")
					if source == "" {
						source = attr(node, "src")
					}
					image = cultureImageURL(pageURL, source)
				}
				var fiction []map[string]any
				if link != "" && title != "" {
					fiction = append(fiction, map[string]any{
						"title":   title,
						"url":     link,
						"image":   emptyToNil(image),
						"summary": nil,
						"badges":  []any{},
						"access":  "directory_visible",
					})
				}
				appendModule(snapshotModule{Key: "culture.fiction", Name: "小说界",
					Lane: "sidebar", Order: 1, State: "visible", Items: fiction})
			}
			if len(containers) > 1 {
				var obituary []map[string]any
				if item := cultureItem(containers[1], pageURL, cultureItemOptions{
					AnchorXPaths:  []string{".//dt/a[1][@href]"},
					ImageXPaths:   []string{".//dd//img"},
					SummaryXPaths: []string{".//dd/p/a[1]", ".//dd/p"},
				}); item != nil {
					obituary = append(obituary, item)
				}
				appendModule(snapshotModule{Key: "culture.obituary", Name: "逝者",
					Lane: "sidebar", Order: 2, State: "visible", Items: obituary})
			}
		}
	}

	authors := []any{}
	if right != nil {
		if authorRoot := firstElement(right, "./div["+classPredicate("zhuanlan")+"]"); authorRoot != nil {
			for _, node := range firstN(htmlquery.Find(authorRoot,
				"./ul["+classPredicate("zhuanlanCon")+"]/li"), 48) {
				anchor := firstElement(node, "./a[1][@href]")
				if anchor == nil {
					continue
				}
				link, _ := cultureAuthorURL(absoluteURL(pageURL, attr(anchor, "href")))
				name := nodeText(anchor)
				if link != "" && name != "" {
					authors = append(authors, map[string]any{"name": name, "url": link})
				}
			}
		}
	}

	items := countItems(modules)
	return map[string]any{
		"modules":                modulesAsList(modules),
		"modules_count":          len(modules),
		"items_count":            items,
		"editorial_items_count":  items,
		"author_directory":       authors,
		"author_directory_count": len(authors),
		"total_entries_count":    items + len(authors),
	}, nil
}

// firstN caps a node list, so a runaway block cannot silently grow the payload.
func firstN(nodes []*xhtml.Node, limit int) []*xhtml.Node {
	if len(nodes) > limit {
		return nodes[:limit]
	}
	return nodes
}

// itoa renders a small non-negative index.
func itoa(value int) string {
	return strconv.Itoa(value)
}
