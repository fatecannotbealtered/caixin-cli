package caixin

import (
	"strconv"
	"strings"

	"github.com/antchfx/htmlquery"
	xhtml "golang.org/x/net/html"
)

// A news channel front is one layout with per-channel exceptions: 中国 and 世界
// build their top block differently, and 中国 carries four sidebar blocks the
// others do not. Those exceptions are written out rather than generalized --
// a rule broad enough to cover all of them would also match blocks that mean
// something else.

// channelModules extracts one news channel front page.
func channelModules(doc *xhtml.Node, pageURL, pageKey string) ([]snapshotModule, error) {
	root := firstElement(doc, "//div["+classPredicate("indexMain")+"]")
	if root == nil {
		return nil, &APIError{Message: "the " + pageKey + " channel page is missing its indexMain"}
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

	// datedItem adds what a channel card carries beyond a headline: the date it
	// printed, and whether the card was on screen.
	datedItem := func(node *xhtml.Node, options itemOptions, publishedXPaths []string) map[string]any {
		item := editorialItem(node, pageURL, options)
		if item == nil {
			return nil
		}
		var published any
		if len(publishedXPaths) > 0 {
			published = emptyToNil(firstXPathText(node, publishedXPaths...))
		}
		item["published_at"] = published
		item["source_state"] = serverSourceState(node)
		return item
	}

	if top := firstElement(root, ".//*[contains(@class,'topNews')]"); top != nil {
		var items []map[string]any
		if pageKey == "international" {
			for _, node := range htmlquery.Find(top, ".//*["+classPredicate("lstjbd")+"]/dl") {
				gallery := datedItem(node, itemOptions{
					AnchorXPaths: []string{"./dd/h4/a[@href]"},
					ImageXPaths:  []string{"./dt/a/img"},
				}, nil)
				if gallery != nil {
					gallery["item_kind"] = "gallery"
					items = append(items, gallery)
				}
			}
		}
		leadImages := []string{".//*[contains(@class,'pic')]//img"}
		if pageKey == "international" {
			leadImages = []string{}
		}
		lead := datedItem(top, itemOptions{
			AnchorXPaths:  []string{".//*[contains(@class,'txt')]//h3/a[@href]"},
			ImageXPaths:   leadImages,
			SummaryXPaths: []string{".//*[contains(@class,'txt')]/p"},
			BadgeXPaths:   []string{".//*[contains(@class,'icon_')]"},
		}, []string{".//*[contains(@class,'txt')]/span"})
		switch {
		case lead != nil:
			items = append(items, lead)
		case pageKey == "china":
			// 中国 sometimes serves the older card shape for its lead.
			if legacy := datedItem(top, itemOptions{
				AnchorXPaths:  []string{"./dl/dt/a[@href]"},
				ImageXPaths:   []string{"./dl/dd//*[contains(@class,'pic')]//img"},
				SummaryXPaths: []string{"./dl/dd//*[contains(@class,'txt')]/p"},
			}, []string{"./dl/dd//*[contains(@class,'txt')]/span"}); legacy != nil {
				items = append(items, legacy)
			}
		}
		switch pageKey {
		case "china":
			for _, node := range htmlquery.Find(top, "./ul[contains(@class,'channel_china')]/li") {
				if related := datedItem(node, itemOptions{
					AnchorXPaths: []string{"./h4/a[@href]"},
					ImageXPaths:  []string{},
					BadgeXPaths:  []string{"./p[contains(@class,'title')]/a"},
				}, []string{"./span"}); related != nil {
					items = append(items, related)
				}
			}
		case "international":
			for _, node := range htmlquery.Find(top, ".//*["+classPredicate("txt")+"]/ul/li") {
				if related := datedItem(node, itemOptions{
					AnchorXPaths: []string{"./a[@href]"},
					ImageXPaths:  []string{},
				}, nil); related != nil {
					items = append(items, related)
				}
			}
		}
		appendModule(pageKey+".top", "头条", "main", "visible", items)
	}

	var news []map[string]any
	for _, node := range htmlquery.Find(root,
		".//*[contains(@class,'yaowen')]//*["+classPredicate("dis")+
			"]//*[@id='listArticle']/*["+classPredicate("boxa")+"]") {
		if item := editorialItem(node, pageURL, itemOptions{
			AnchorXPaths:  []string{".//h4/a[@href]"},
			ImageXPaths:   []string{".//*[contains(@class,'pic')]//img"},
			SummaryXPaths: []string{"./p", ".//p"},
			BadgeXPaths:   []string{".//h4//*[contains(@class,'icon_')]"},
		}); item != nil {
			news = append(news, item)
		}
	}
	appendModule(pageKey+".news", "要闻", "main", "visible", news)

	if economic := firstElement(root, ".//*["+classPredicate("mainConRig")+
		"]/div["+classPredicate("economic_data")+"]"); economic != nil {
		var items []map[string]any
		for _, node := range findInDocumentOrder(economic, "./dl", "./ul/li") {
			item := editorialItem(node, pageURL, itemOptions{
				AnchorXPaths: []string{"./a[@href]"},
				ImageXPaths:  []string{"./a/dt/img"},
			})
			if item == nil {
				continue
			}
			item["source_state"] = serverSourceState(node)
			items = append(items, item)
		}
		name := firstXPathText(economic, "./div[contains(@class,'title')]/a")
		if name == "" {
			name = "经济数据"
		}
		appendModule(pageKey+".economic-data", name, "sidebar", "visible", items)
	}

	if pageKey == "china" {
		if sidebar := firstElement(root, ".//*["+classPredicate("mainConRig")+"]"); sidebar != nil {
			channelChinaSidebar(sidebar, pageURL, datedItem, appendModule)
		}
	}

	for index, container := range htmlquery.Find(root,
		".//*["+classPredicate("mainConRig")+"]/div["+classPredicate("dlist")+"]") {
		name := firstXPathText(container, ".//*[contains(@class,'title')]/a")
		if name == "" {
			name = "侧栏"
		}
		var items []map[string]any
		for _, node := range htmlquery.Find(container, ".//*["+classPredicate("dlistCon")+"]/dl") {
			if item := editorialItem(node, pageURL, itemOptions{
				AnchorXPaths:  []string{".//dt/a[@href]"},
				SummaryXPaths: []string{".//dd/p"},
			}); item != nil {
				items = append(items, item)
			}
		}
		key := pageKey + ".list-" + strconv.Itoa(index)
		if strings.Contains(name, "市场") {
			key = pageKey + ".market"
		}
		appendModule(key, name, "sidebar", "visible", items)
	}

	for _, container := range htmlquery.Find(root,
		".//*["+classPredicate("mainConRig")+"]//*["+classPredicate("redianzhuanti")+"]") {
		name := firstXPathText(container, "./div[contains(@class,'title')]/a")
		if name == "" {
			name = "热点专题"
		}
		var items []map[string]any
		for _, node := range htmlquery.Find(container, ".//*["+classPredicate("rdztCon")+"]/dl") {
			if item := editorialItem(node, pageURL, itemOptions{
				AnchorXPaths:  []string{".//dd/h4/a[@href]"},
				ImageXPaths:   []string{".//dt//img"},
				SummaryXPaths: []string{".//dd/p"},
			}); item != nil {
				items = append(items, item)
			}
		}
		appendModule(pageKey+".topics", name, "sidebar", "visible", items)
	}

	for index, container := range htmlquery.Find(root,
		".//*["+classPredicate("mainConRig")+"]/div["+classPredicate("cailu")+"]") {
		name := firstXPathText(container, "./div[contains(@class,'title')]/a")
		if name == "" {
			name = "侧栏"
		}
		// The block has one pane per tab. The first is what was shown; when it
		// is empty the first pane that is not gets reported instead, marked so
		// the caller knows it was not the visible one.
		panes := htmlquery.Find(container, "./div["+classPredicate("cailuCon")+"]")
		var chosen *xhtml.Node
		state := "visible"
		if len(panes) > 0 {
			chosen = panes[0]
			if len(htmlquery.Find(chosen, ".//dl/dt/a[@href]")) == 0 {
				for _, pane := range panes[1:] {
					if len(htmlquery.Find(pane, ".//dl/dt/a[@href]")) > 0 {
						chosen, state = pane, "fallback-visible"
						break
					}
				}
			}
		}
		var items []map[string]any
		if chosen != nil {
			for _, node := range htmlquery.Find(chosen, ".//div[contains(@class,'dlCon')]/dl") {
				if item := editorialItem(node, pageURL, itemOptions{
					AnchorXPaths:  []string{".//dt/a[@href]"},
					ImageXPaths:   []string{".//*[contains(@class,'pic')]//img"},
					SummaryXPaths: []string{".//dd/p"},
				}); item != nil {
					items = append(items, item)
				}
			}
		}
		suffix := "rail-" + strconv.Itoa(index)
		switch {
		case strings.Contains(name, "科技"):
			suffix = "tech"
		case strings.Contains(name, "汽车"):
			suffix = "auto"
		}
		appendModule(pageKey+"."+suffix, name, "sidebar", state, items)
	}
	return modules, nil
}

// channelChinaSidebar reads the four blocks only the 中国 channel carries.
func channelChinaSidebar(
	sidebar *xhtml.Node,
	pageURL string,
	datedItem func(*xhtml.Node, itemOptions, []string) map[string]any,
	appendModule func(key, name, lane, state string, items []map[string]any),
) {
	if education := firstElement(sidebar, "./div["+classPredicate("youjiao")+"]"); education != nil {
		var items []map[string]any
		for _, node := range htmlquery.Find(education,
			".//div[contains(@class,'dlCon') or contains(@class,'dlcon')]/dl") {
			if item := datedItem(node, itemOptions{
				AnchorXPaths:  []string{"./dt/a[@href]"},
				ImageXPaths:   []string{"./dd//*[contains(@class,'pic')]//img"},
				SummaryXPaths: []string{"./dd/p"},
			}, []string{"./dd/span"}); item != nil {
				items = append(items, item)
			}
		}
		for _, node := range htmlquery.Find(education, ".//div[contains(@class,'cailuCon')]/ul/li") {
			if item := datedItem(node, itemOptions{
				AnchorXPaths: []string{"./a[@href]"},
				ImageXPaths:  []string{},
			}, nil); item != nil {
				items = append(items, item)
			}
		}
		name := firstXPathText(education, "./div[contains(@class,'title')]")
		if name == "" {
			name = "教育观察"
		}
		appendModule("china.education", name, "sidebar", "visible", items)
	}

	for _, block := range []struct{ class, key, fallback string }{
		{"fanfu", "china.anticorruption", "反腐纪事"},
		{"renshi", "china.personnel", "人事观察"},
	} {
		container := firstElement(sidebar, "./div["+classPredicate(block.class)+"]")
		if container == nil {
			continue
		}
		var items []map[string]any
		for _, node := range htmlquery.Find(container,
			".//div[contains(@class,'dlCon') or contains(@class,'dlcon')]/dl") {
			if item := datedItem(node, itemOptions{
				AnchorXPaths:  []string{"./dt/a[@href]"},
				ImageXPaths:   []string{"./dd//*[contains(@class,'pic')]//img"},
				SummaryXPaths: []string{"./dd/p"},
			}, []string{"./dd/span"}); item != nil {
				items = append(items, item)
			}
		}
		name := firstXPathText(container, "./div[contains(@class,'title')]/a")
		if name == "" {
			name = block.fallback
		}
		appendModule(block.key, name, "sidebar", "visible", items)
	}

	if briefs := firstElement(sidebar, "./div["+classPredicate("kuaixun")+"]"); briefs != nil {
		var items []map[string]any
		for _, node := range htmlquery.Find(briefs, "./div[@id='demo']/div[@id='demo1']/ul/li") {
			if item := datedItem(node, itemOptions{
				AnchorXPaths: []string{"./h4/a[@href]"},
				ImageXPaths:  []string{},
			}, []string{"./span"}); item != nil {
				items = append(items, item)
			}
		}
		name := firstXPathText(briefs, "./div[contains(@class,'title')]/a")
		if name == "" {
			name = "当日快讯"
		}
		// The block scrolls a fixed window; it is neither fully visible nor
		// hidden, and saying so is more useful than picking one.
		appendModule("china.briefs", name, "sidebar", "scroll-container", items)
	}
}

// categoryModules extracts a channel sub-page: one article list plus the two
// sidebar blocks the channel repeats across its categories.
func categoryModules(doc *xhtml.Node, pageURL, pageKey string) ([]snapshotModule, error) {
	root := firstElement(doc, classXPath("comMain"))
	if root == nil {
		return nil, &APIError{Message: "the " + pageKey + " category page is missing its comMain"}
	}

	var modules []snapshotModule
	laneOrders := map[string]int{"main": 0, "sidebar": 0}
	appendModule := func(key, name, lane string, items []map[string]any) {
		if len(items) == 0 {
			return
		}
		modules = append(modules, snapshotModule{
			Key: key, Name: name, Lane: lane, Order: laneOrders[lane],
			State: "visible", Items: items,
		})
		laneOrders[lane]++
	}

	var articles []map[string]any
	for _, node := range htmlquery.Find(root,
		".//div["+classPredicate("conlf")+"]/*["+classPredicate("stitXtuwen_list")+"]/dl") {
		if item := editorialItem(node, pageURL, itemOptions{
			AnchorXPaths:  []string{".//dd/h4/a[1][@href]"},
			ImageXPaths:   []string{".//dd//*[contains(@class,'pic')]//img"},
			SummaryXPaths: []string{".//dd/p"},
			BadgeXPaths:   []string{".//dd/h4//*[contains(@class,'icon_')]"},
		}); item != nil {
			articles = append(articles, item)
		}
	}
	appendModule(pageKey+".articles", "频道文章", "main", articles)

	if sidebar := firstElement(root, ".//div["+classPredicate("conri")+"]"); sidebar != nil {
		for _, container := range htmlquery.Find(sidebar, "./div["+classPredicate("columnBox")+"]") {
			name := firstXPathText(container, "./h3")
			var key string
			switch {
			case strings.Contains(name, "编辑推荐"):
				key = pageKey + ".recommended"
			case strings.Contains(name, "最新文章"):
				key = pageKey + ".latest"
			default:
				continue
			}
			appendModule(key, name, "sidebar", sidebarItems(container, pageURL))
		}
	}
	return modules, nil
}
