package caixin

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/antchfx/htmlquery"
	xhtml "golang.org/x/net/html"
)

// The remaining fronts each have their own layout: mini's tabbed lanes, the ESG
// hub, the topic directory, the newsletter, 金融我闻, the English edition, and
// the blog index. They share nothing but the module shape, so each is read by
// its own extractor rather than by a rule general enough to misread all of them.

// miniLanes are mini's seven tabs, in page order.
var miniLanes = []struct{ slug, name, moreURL string }{
	{"all", "全部", ""},
	{"briefing", "财新闻", "https://mini.caixin.com/briefing/"},
	{"film", "影视", "https://mini.caixin.com/film/"},
	{"reading", "读书", "https://mini.caixin.com/reading/"},
	{"story", "故事", "https://mini.caixin.com/story/"},
	{"health", "健保", "https://mini.caixin.com/health/"},
	{"discussion", "热议", "https://mini.caixin.com/discussion/"},
}

// miniHomeModules extracts the mini front page.
func miniHomeModules(doc *xhtml.Node, pageURL string) ([]snapshotModule, error) {
	root := firstElement(doc,
		"//div["+classPredicate("indexMainCon")+"]/div["+classPredicate("mainConLeft")+"]")
	if root == nil {
		return nil, &APIError{Message: "the mini front page is missing its indexMainCon/mainConLeft"}
	}

	var modules []snapshotModule
	var focus []map[string]any
	for _, node := range firstN(htmlquery.Find(root,
		"./div["+classPredicate("topNews")+"]//div[@id='zyqh']/dl"), 3) {
		if item := extractItem(node, pageURL, itemOptions{
			AnchorXPaths:  []string{"./div[contains(@class,'txt')]/dt/a[@href]"},
			ImageXPaths:   []string{"./div[contains(@class,'pic')]//img"},
			SummaryXPaths: []string{"./div[contains(@class,'txt')]/p"},
		}); item != nil {
			focus = append(focus, item)
		}
	}
	if len(focus) > 0 {
		active := 0
		modules = append(modules, snapshotModule{Key: "mini.focus", Name: "焦点",
			Lane: "lead", Order: 0, State: "hidden", ActiveItemIndex: &active, Items: focus})
	}

	laneRoot := firstElement(root,
		"./div["+classPredicate("yaowen")+"]//div["+classPredicate("ywList")+"]")
	if laneRoot == nil {
		return nil, &APIError{Message: "the mini front page is missing its ywList"}
	}
	for order, lane := range miniLanes {
		container := firstElement(laneRoot, "./div[@id='container_"+strconv.Itoa(order)+"']")
		if container == nil {
			continue
		}
		var items []map[string]any
		seen := map[string]bool{}
		for _, node := range htmlquery.Find(container,
			"./div["+classPredicate("ywListCon")+"]/div["+classPredicate("boxa")+"]") {
			item := extractItem(node, pageURL, itemOptions{
				AnchorXPaths:  []string{"./h4/a[@href]"},
				ImageXPaths:   []string{"./div[contains(@class,'pic')]//img"},
				SummaryXPaths: []string{"./p"},
				BadgeXPaths: []string{
					"./h4/i/a[@href]",
					"./h4//*[" + classPredicate("icon_key") + " or " + classPredicate("icon_free") + "]",
				},
			})
			if item == nil || seen[asString(item["url"])] {
				continue
			}
			seen[asString(item["url"])] = true
			items = append(items, item)
			if len(items) == 28 {
				break
			}
		}
		if len(items) == 0 {
			continue
		}
		module := snapshotModule{Key: "mini." + lane.slug, Name: lane.name, Lane: "main",
			Order: order, State: "hidden", Items: items}
		if order == 0 {
			module.State = "visible"
		}
		if lane.moreURL != "" {
			if anchor := firstElement(container,
				"./div["+classPredicate("moreArt")+"]/a[@href]"); anchor != nil {
				if channelURL(pageURL, attr(anchor, "href")) == lane.moreURL {
					module.MoreURL = lane.moreURL
				}
			}
		}
		modules = append(modules, module)
	}

	if sidebar := firstElement(root, "./div["+classPredicate("conri")+"]/div["+
		classPredicate("columnBox")+"][h3[contains(normalize-space(.),'编辑推荐')]]"); sidebar != nil {
		var items []map[string]any
		for _, anchor := range firstN(htmlquery.Find(sidebar,
			"./div["+classPredicate("listWithPic")+"]/a[@href]"), 2) {
			if item := extractItem(anchor, pageURL, itemOptions{
				AnchorXPaths: []string{"self::a[@href]"},
				ImageXPaths:  []string{".//img"},
			}); item != nil {
				items = append(items, item)
			}
		}
		for _, row := range firstN(htmlquery.Find(sidebar, "./ul["+classPredicate("list")+"]/li"), 5) {
			if item := extractItem(row, pageURL, itemOptions{
				AnchorXPaths: []string{"./a[@href]"},
				ImageXPaths:  []string{},
				BadgeXPaths:  []string{"./a[@href]"},
			}); item != nil {
				items = append(items, item)
			}
		}
		if len(items) > 0 {
			modules = append(modules, snapshotModule{Key: "mini.editor-recommended",
				Name: "编辑推荐", Lane: "sidebar", Order: 0, State: "visible", Items: items})
		}
	}
	return modules, nil
}

// esgSectionKeys are the three named sections of the ESG hub.
var esgSectionKeys = map[string]string{
	"绿色经济":  "green-economy",
	"社会·共益": "social-good",
	"治理追踪":  "governance",
}

// esgModules extracts the ESG hub.
func esgModules(doc *xhtml.Node, pageURL string) ([]snapshotModule, error) {
	root := firstElement(doc, classXPath("mainConBox"))
	if root == nil {
		return nil, &APIError{Message: "the ESG page is missing its mainConBox"}
	}

	var modules []snapshotModule
	laneOrders := map[string]int{"lead": 0, "main": 0, "sidebar": 0}
	appendModule := func(key, name, lane string, items []map[string]any) {
		if len(items) == 0 {
			return
		}
		modules = append(modules, snapshotModule{Key: key, Name: name, Lane: lane,
			Order: laneOrders[lane], State: "visible", Items: items})
		laneOrders[lane]++
	}

	// The hub's slots are sponsored as often as editorial, and the sponsored
	// ones carry tracking parameters. A tracked destination is dropped rather
	// than cleaned: the parameters are part of what that link is.
	esgItem := func(node *xhtml.Node, options itemOptions) map[string]any {
		item := editorialItem(node, pageURL, options)
		if item == nil {
			return nil
		}
		if hasQueryOrFragment(asString(item["url"])) {
			return nil
		}
		if image, ok := item["image"].(string); ok && hasQueryOrFragment(image) {
			item["image"] = nil
		}
		return item
	}
	esgItems := func(nodes []*xhtml.Node, options itemOptions) []map[string]any {
		var items []map[string]any
		for _, node := range nodes {
			if item := esgItem(node, options); item != nil {
				items = append(items, item)
			}
		}
		return items
	}

	appendModule("esg.focus", "焦点", "lead", esgItems(
		htmlquery.Find(root, ".//div["+classPredicate("ESGfocus")+
			"]//div["+classPredicate("swiper-slide")+"]"),
		itemOptions{
			AnchorXPaths: []string{".//div[" + classPredicate("img_txt") + "]//dd/a[@href]"},
			ImageXPaths:  []string{"./a/img", ".//img"},
			BadgeXPaths: []string{
				".//div[" + classPredicate("img_txt") + "]//dt/parent::a",
				".//*[contains(@class,'icon_')]",
			},
		}))

	for _, section := range htmlquery.Find(root, ".//div["+classPredicate("esg_news")+"]/ul/li") {
		name := firstXPathText(section, "./h3/a", "./h3")
		slug := esgSectionKeys[name]
		if slug == "" || name == "" {
			continue
		}
		var items []map[string]any
		if item := esgItem(section, itemOptions{
			AnchorXPaths:  []string{"./p/a[@href]"},
			ImageXPaths:   []string{"./p//img"},
			SummaryXPaths: []string{"./span"},
			BadgeXPaths:   []string{"./p//*[contains(@class,'icon_')]"},
		}); item != nil {
			items = append(items, item)
		}
		appendModule("esg."+slug, name, "main", items)
	}

	if latest := firstElement(root, ".//div["+classPredicate("esg_zxwz")+"]"); latest != nil {
		name := firstXPathText(latest, "./h2")
		if name == "" {
			name = "最新文章"
		}
		appendModule("esg.latest", name, "main", esgItems(
			htmlquery.Find(latest, "./div["+classPredicate("list")+"]"),
			itemOptions{
				AnchorXPaths:  []string{".//h4/a[@href]"},
				ImageXPaths:   []string{".//dt//img"},
				SummaryXPaths: []string{".//dd/p"},
				BadgeXPaths:   []string{"./h3/a", ".//h4//*[contains(@class,'icon_')]"},
			}))
	}

	if esg30 := firstElement(root, ".//div["+classPredicate("new_con")+"]"); esg30 != nil {
		var items []map[string]any
		if item := esgItem(esg30, itemOptions{
			AnchorXPaths:  []string{".//dl[" + classPredicate("about_ESG_txt") + "]/dd[1]/a[@href]"},
			ImageXPaths:   []string{},
			SummaryXPaths: []string{".//dl[" + classPredicate("about_ESG_txt") + "]/dt"},
		}); item != nil {
			items = append(items, item)
		}
		name := firstXPathText(esg30, ".//*[contains(@class,'tit02')]/a")
		if name == "" {
			name = "ESG30专题"
		}
		appendModule("esg.esg30", name, "sidebar", items)
	}

	if special := firstElement(root, ".//div["+classPredicate("tbcx_ad")+"]"); special != nil {
		name := firstXPathText(special, "./h3")
		if name == "" {
			name = "特别呈现"
		}
		appendModule("esg.special", name, "sidebar", esgItems(
			htmlquery.Find(special, ".//div["+classPredicate("swiper-slide")+"]"),
			itemOptions{AnchorXPaths: []string{"./a[@href]"}, ImageXPaths: []string{"./a/img"}}))
	}

	if video := firstElement(root, ".//div["+classPredicate("esg_video")+"]"); video != nil {
		var items []map[string]any
		if item := esgItem(video, itemOptions{
			AnchorXPaths: []string{".//dl/dd/a[@href]"},
			ImageXPaths:  []string{".//dl//img"},
		}); item != nil {
			items = append(items, item)
		}
		name := firstXPathText(video, "./a/h3", "./h3")
		if name == "" {
			name = "影像说"
		}
		appendModule("esg.video", name, "sidebar", items)
	}

	if reports := firstElement(root, ".//div["+classPredicate("esg_hqbg")+"]"); reports != nil {
		name := firstXPathText(reports, "./a/h4", "./h4")
		if name == "" {
			name = "ESG报告"
		}
		appendModule("esg.reports", name, "sidebar", esgItems(
			htmlquery.Find(reports, "./dl"),
			itemOptions{
				AnchorXPaths:  []string{".//dd/h5/a[@href]"},
				ImageXPaths:   []string{".//dt//img"},
				SummaryXPaths: []string{".//dd/p"},
			}))
	}
	return modules, nil
}

// hasQueryOrFragment reports whether a url carries tracking beyond its path.
func hasQueryOrFragment(raw string) bool {
	return strings.ContainsAny(raw, "?#")
}

// topicCategoryKeys name the topic directory's six category blocks.
var topicCategoryKeys = []struct{ label, slug string }{
	{"经济", "economy"},
	{"金融", "finance"},
	{"世界", "international"},
	{"商业", "business"},
	{"观点", "opinion"},
	{"政经", "china"},
}

// topicsDirectoryModules extracts the 专题 directory.
func topicsDirectoryModules(doc *xhtml.Node, pageURL string) ([]snapshotModule, error) {
	boxes := htmlquery.Find(doc,
		"//div["+classPredicate("leftJingjiBox")+" or "+classPredicate("rightJinrongBox")+"]")
	if len(boxes) == 0 {
		return nil, &APIError{Message: "the 专题 directory carries no category block"}
	}

	var modules []snapshotModule
	laneOrders := map[string]int{"main": 0, "sidebar": 0}
	for index, box := range boxes {
		lane := "sidebar"
		for _, class := range strings.Fields(attr(box, "class")) {
			if class == "leftJingjiBox" {
				lane = "main"
			}
		}
		name := firstXPathText(box, ".//*[contains(@class,'tit_lm')]//a")
		if name == "" {
			continue
		}
		var items []map[string]any
		for _, node := range htmlquery.Find(box,
			".//*["+classPredicate("channelBoxCon")+"]//*["+classPredicate("demolNews")+"]/dl") {
			if item := editorialItem(node, pageURL, itemOptions{
				AnchorXPaths: []string{".//dd/p/a[@href]"},
				ImageXPaths:  []string{".//dt//img"},
			}); item != nil {
				items = append(items, item)
			}
		}
		if len(items) == 0 {
			continue
		}
		slug := strconv.Itoa(index)
		for _, candidate := range topicCategoryKeys {
			if strings.Contains(name, candidate.label) {
				slug = candidate.slug
				break
			}
		}
		modules = append(modules, snapshotModule{Key: "topics." + slug, Name: name,
			Lane: lane, Order: laneOrders[lane], State: "visible", Items: items})
		laneOrders[lane]++
	}
	return modules, nil
}

// newsletterItem builds one newsletter entry.
//
// The newsletter publishes an excerpt inline. It is kept as `excerpt` under an
// access label of its own, because an excerpt the publisher chose to print is
// not the article and must not read as if it were.
func newsletterItem(node *xhtml.Node, pageURL string) map[string]any {
	anchor := firstElement(node,
		".//a[.//*["+classPredicate("cx-aggs-title")+"]][@href]",
		".//a["+classPredicate("cx-aggs-href")+"][@href]")
	if anchor == nil {
		return nil
	}
	link := caixinOutputURL(pageURL, attr(anchor, "href"))
	title := nodeText(anchor)
	if link == "" || title == "" {
		return nil
	}
	image := ""
	if imageNode := firstElement(node,
		".//*["+classPredicate("cx-aggs-img")+"]", ".//img"); imageNode != nil {
		source := attr(imageNode, "data-src")
		if source == "" {
			source = attr(imageNode, "src")
		}
		image = caixinOutputURL(pageURL, source)
	}
	excerpt := ""
	if contentNode := firstElement(node, ".//*["+classPredicate("cx-aggs-content")+"]"); contentNode != nil {
		excerpt = nodeText(contentNode)
	}
	excerpt = strings.TrimSpace(strings.TrimPrefix(excerpt, title))
	if len(excerpt) > 2000 {
		excerpt = excerpt[:2000]
	}
	return map[string]any{
		"title":   title,
		"url":     link,
		"image":   emptyToNil(image),
		"excerpt": excerpt,
		"access":  "newsletter_visible_excerpt",
	}
}

// newsletterModules extracts the newsletter front and its own freshness.
func newsletterModules(doc *xhtml.Node, pageURL string) (map[string]any, error) {
	var modules []snapshotModule
	if leadNode := firstElement(doc, classXPath("cx-aggs-top")); leadNode != nil {
		if lead := newsletterItem(leadNode, pageURL); lead != nil {
			modules = append(modules, snapshotModule{Key: "newsletter.lead", Name: "主编精选",
				Lane: "lead", Order: 0, State: "visible", Items: []map[string]any{lead}})
		}
	}
	var visual, text []map[string]any
	for _, node := range htmlquery.Find(doc, classXPath("cx-aggs-news")) {
		item := newsletterItem(node, pageURL)
		if item == nil {
			continue
		}
		if len(htmlquery.Find(node, ".//img")) > 0 {
			visual = append(visual, item)
		} else {
			text = append(text, item)
		}
	}
	for _, block := range []struct {
		key, name string
		order     int
		items     []map[string]any
	}{
		{"newsletter.top", "重点阅读", 0, visual},
		{"newsletter.more", "更多阅读", 1, text},
	} {
		if len(block.items) == 0 {
			continue
		}
		modules = append(modules, snapshotModule{Key: block.key, Name: block.name,
			Lane: "main", Order: block.order, State: "visible", Items: block.items})
	}

	newest := ""
	for _, module := range modules {
		for _, item := range module.Items {
			// Matched against the path: newsletter links carry campaign
			// parameters, so the date is not at the end of the url.
			parsed, err := url.Parse(asString(item["url"]))
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
	stale := false
	if parsed, err := time.Parse("2006-01-02", newest); err == nil {
		stale = time.Since(parsed) > 30*24*time.Hour
	}
	return map[string]any{
		"modules":             modulesAsList(modules),
		"modules_count":       len(modules),
		"items_count":         countItems(modules),
		"latest_article_date": emptyToNil(newest),
		"stale_content":       stale,
	}, nil
}

// wenewsSnapshot extracts the 金融我闻 front page.
func wenewsSnapshot(doc *xhtml.Node, pageURL string) (map[string]any, error) {
	root := firstElement(doc,
		"//div["+classPredicate("indexMain")+"]/div["+classPredicate("indexMainCon")+"]")
	if root == nil {
		return nil, &APIError{Message: "the 金融我闻 front page is missing its indexMainCon"}
	}

	var modules []snapshotModule
	if top := firstElement(root, ".//div["+classPredicate("mainConLeft")+
		"]//div["+classPredicate("topNews")+" and "+classPredicate("wenews-top")+"]"); top != nil {
		content := ".//div[" + classPredicate("wenews-top-con") + "]/div[" + classPredicate("txt") + "]"
		if item := extractItem(top, pageURL, itemOptions{
			AnchorXPaths:  []string{content + "/h3/a[@href]"},
			ImageXPaths:   []string{".//div[" + classPredicate("wenews-top-con") + "]/div[" + classPredicate("pic") + "]//img"},
			SummaryXPaths: []string{content + "/p"},
		}); item != nil {
			if textNode := firstElement(top, content); textNode != nil {
				if meta := directText(textNode); meta != "" {
					item["meta"] = meta
				}
			}
			name := firstXPathText(top, "./div[@class='title']")
			if name == "" {
				name = "金融我闻头条"
			}
			modules = append(modules, snapshotModule{Key: "wenews.headline", Name: name,
				Lane: "lead", Order: 0, State: "visible", Items: []map[string]any{item}})
		}
	}

	if list := firstElement(root, ".//div["+classPredicate("mainConLeft")+
		"]//div[@id='listArticle' and "+classPredicate("ywListCon")+"]"); list != nil {
		var items []map[string]any
		for _, node := range htmlquery.Find(list, "./div["+classPredicate("boxa")+"]") {
			item := extractItem(node, pageURL, itemOptions{
				AnchorXPaths:  []string{"./h4/a[1][@href]"},
				ImageXPaths:   []string{"./div[" + classPredicate("pic") + "]//img"},
				SummaryXPaths: []string{"./p"},
				BadgeXPaths:   []string{"./h4/*[contains(@class,'icon_')]"},
			})
			if item == nil {
				continue
			}
			if meta := firstXPathText(node, "./span"); meta != "" {
				item["meta"] = meta
			}
			items = append(items, item)
		}
		if len(items) > 0 {
			modules = append(modules, snapshotModule{Key: "wenews.latest",
				Name: "金融我闻最新文章", Lane: "main", Order: 0, State: "visible", Items: items})
		}
	}

	if market := firstElement(root, ".//div["+classPredicate("mainConRig")+
		"]/div["+classPredicate("dlist")+"]"); market != nil {
		var items []map[string]any
		for _, node := range htmlquery.Find(market, "./div["+classPredicate("dlistCon")+"]/dl") {
			item := extractItem(node, pageURL, itemOptions{
				AnchorXPaths:  []string{"./dt/a[1][@href]"},
				ImageXPaths:   []string{},
				SummaryXPaths: []string{"./dd/p"},
			})
			if item == nil {
				continue
			}
			if span := firstElement(node, "./dd/span"); span != nil {
				if meta := directText(span); meta != "" {
					item["meta"] = meta
				}
			}
			items = append(items, item)
		}
		if len(items) > 0 {
			name := firstXPathText(market, "./div[@class='title']/a")
			if name == "" {
				name = "金融市场"
			}
			modules = append(modules, snapshotModule{Key: "wenews.market", Name: name,
				Lane: "sidebar", Order: 0, State: "visible", Items: items})
		}
	}
	return attachClickConsumers(directoryResult(modules)), nil
}

// englishSnapshot extracts the English edition front page.
func englishSnapshot(doc *xhtml.Node, pageURL string) (map[string]any, error) {
	root := firstElement(doc, classXPath("comMain"))
	if root == nil {
		return nil, &APIError{Message: "the English front page is missing its comMain"}
	}

	var modules []snapshotModule
	var features []map[string]any
	for _, node := range htmlquery.Find(root,
		"./div["+classPredicate("conlf")+"]/div["+classPredicate("stitXtuwen_list")+"]/dl") {
		item := extractItem(node, pageURL, itemOptions{
			AnchorXPaths:  []string{"./dd/h4/a[1][@href]"},
			ImageXPaths:   []string{"./dd/div[@class='pic']//img"},
			SummaryXPaths: []string{"./dd/p"},
			BadgeXPaths:   []string{"./dd/h4/*[contains(@class,'icon_')]"},
		})
		if item == nil {
			continue
		}
		if meta := firstXPathText(node, "./dd/span"); meta != "" {
			item["meta"] = meta
		}
		features = append(features, item)
	}
	if len(features) > 0 {
		modules = append(modules, snapshotModule{Key: "en.features", Name: "Caixin English",
			Lane: "main", Order: 0, State: "visible", Items: features})
	}

	if editor := firstElement(root, "./div["+classPredicate("conri")+"]/div["+
		classPredicate("columnBox")+"][normalize-space(h3)='编辑推荐']"); editor != nil {
		var items []map[string]any
		for _, node := range htmlquery.Find(editor, "./div["+classPredicate("listWithPic")+"]/a[@href]") {
			if item := extractItem(node, pageURL, itemOptions{
				AnchorXPaths: []string{"./self::a[@href]"},
				ImageXPaths:  []string{"./img"},
			}); item != nil {
				items = append(items, item)
			}
		}
		for _, node := range htmlquery.Find(editor, "./ul["+classPredicate("list")+"]/li") {
			item := extractItem(node, pageURL, itemOptions{
				AnchorXPaths: []string{"./a[last()][@href]"},
				ImageXPaths:  []string{},
			})
			if item == nil {
				continue
			}
			// The row opens with the channel the piece came from; it is a label
			// here, not a second entry.
			if channelAnchor := firstElement(node, "./a[1][@href]"); channelAnchor != nil {
				if link := channelURL(pageURL, attr(channelAnchor, "href")); link != "" {
					if channel := nodeText(channelAnchor); channel != "" {
						item["channel"] = channel
						item["channel_url"] = link
					}
				}
			}
			items = append(items, item)
		}
		if len(items) > 0 {
			modules = append(modules, snapshotModule{Key: "en.editor-picks", Name: "编辑推荐",
				Lane: "sidebar", Order: 0, State: "visible", Items: items})
		}
	}

	if latest := firstElement(root, "./div["+classPredicate("conri")+"]/div["+
		classPredicate("columnBox")+"][normalize-space(h3)='最新文章']"); latest != nil {
		var items []map[string]any
		for _, node := range htmlquery.Find(latest, "./ul["+classPredicate("list")+"]/li") {
			item := extractItem(node, pageURL, itemOptions{
				AnchorXPaths: []string{"./a[1][@href]"},
				ImageXPaths:  []string{},
			})
			if item == nil {
				continue
			}
			if meta := firstXPathText(node, "./span"); meta != "" {
				item["meta"] = meta
			}
			items = append(items, item)
		}
		if len(items) > 0 {
			modules = append(modules, snapshotModule{Key: "en.latest", Name: "最新文章",
				Lane: "sidebar", Order: 1, State: "visible", Items: items})
		}
	}
	return attachClickConsumers(directoryResult(modules)), nil
}

// blogHost is the shape of one blogger's own subdomain.
var blogHost = regexp.MustCompile(`^[a-z0-9-]{1,63}\.blog\.caixin\.com$`)

// blogCard builds one card from the blog index. A card is only kept when it
// leads to a blog of its own; the index also carries house ads.
func blogCard(node *xhtml.Node, pageURL, anchorXPath, imageXPath, titleXPath string) map[string]any {
	anchor := firstElement(node, anchorXPath)
	if anchor == nil {
		return nil
	}
	link := contentURL(pageURL, attr(anchor, "href"))
	title := ""
	if titleXPath != "" {
		title = firstXPathText(node, titleXPath)
	}
	if title == "" {
		title = strings.TrimSpace(attr(anchor, "title"))
		if title == "" {
			title = nodeText(anchor)
		}
	}
	if link == "" || title == "" || !blogHost.MatchString(hostOf(link)) {
		return nil
	}
	image := ""
	if imageXPath != "" {
		if imageNode := firstElement(node, imageXPath); imageNode != nil {
			source := attr(imageNode, "data-src")
			if source == "" {
				source = attr(imageNode, "src")
			}
			image = blogImageURL(pageURL, source)
		}
	}
	return map[string]any{
		"title":   title,
		"url":     link,
		"image":   emptyToNil(image),
		"summary": nil,
		"badges":  []any{},
	}
}

// blogSnapshot extracts the blog index.
func blogSnapshot(doc *xhtml.Node, pageURL string) (map[string]any, error) {
	root := firstElement(doc,
		"//div["+classPredicate("main-blog")+"]/div["+classPredicate("blog-index")+"]")
	if root == nil {
		return nil, &APIError{Message: "the blog index is missing its blog-index container"}
	}

	var modules []snapshotModule
	if focus := firstElement(root, "./div["+classPredicate("leftbox")+
		"]/div["+classPredicate("fcous-box")+"]"); focus != nil {
		var items []map[string]any
		for _, node := range findInDocumentOrder(focus,
			"./div["+classPredicate("fcous-content")+"]/div["+classPredicate("fcous-img")+"]/a[@href]",
			"./ul/li/a[@href]") {
			if item := blogCard(node, pageURL, "./self::a[@href]", ".//img[1]",
				".//div["+classPredicate("img-txt")+"]//dt[1] | .//span[1]/strong[1]"); item != nil {
				items = append(items, item)
			}
		}
		if len(items) > 0 {
			modules = append(modules, snapshotModule{Key: "blog.focus", Name: "焦点",
				Lane: "lead", Order: 0, State: "visible", Items: items})
		}
	}

	if recommended := firstElement(root, ".//*[@id='blog_recommend_list']"); recommended != nil {
		var items []map[string]any
		for _, node := range htmlquery.Find(recommended, "./p") {
			item := blogCard(node, pageURL, "./a[not("+classPredicate("author")+")][1][@href]", "", "")
			if item == nil {
				continue
			}
			if anchor := firstElement(node, "./a["+classPredicate("author")+"][1][@href]"); anchor != nil {
				link, _ := blogAuthorURL(absoluteURL(pageURL, attr(anchor, "href")))
				if author := nodeText(anchor); author != "" && link != "" {
					item["author"] = author
					item["author_url"] = link
				}
			}
			items = append(items, item)
		}
		if len(items) > 0 {
			modules = append(modules, snapshotModule{Key: "blog.recommended", Name: "读者推荐",
				Lane: "sidebar", Order: 0, State: "visible", Items: items})
		}
	}

	if authorRoot := firstElement(root, ".//div["+classPredicate("bztj-list")+"]"); authorRoot != nil {
		var items []map[string]any
		for _, anchor := range htmlquery.Find(authorRoot, "./a[@href]") {
			link, _ := blogAuthorURL(absoluteURL(pageURL, attr(anchor, "href")))
			title := firstXPathText(anchor, ".//div["+classPredicate("right")+"]/p[1]")
			if link == "" || title == "" {
				continue
			}
			image := ""
			if imageNode := firstElement(anchor, ".//img[1]"); imageNode != nil {
				source := attr(imageNode, "data-src")
				if source == "" {
					source = attr(imageNode, "src")
				}
				image = blogImageURL(pageURL, source)
			}
			items = append(items, map[string]any{
				"title":     title,
				"url":       link,
				"image":     emptyToNil(image),
				"summary":   emptyToNil(firstXPathText(anchor, ".//div["+classPredicate("right")+"]/span[1]")),
				"badges":    []any{},
				"item_kind": "author",
			})
		}
		if len(items) > 0 {
			modules = append(modules, snapshotModule{Key: "blog.authors", Name: "博主推荐",
				Lane: "sidebar", Order: 1, State: "visible", Items: items})
		}
	}

	if announcements := firstElement(root, ".//div["+classPredicate("gonggaoqu")+"]"); announcements != nil {
		var items []map[string]any
		for _, node := range htmlquery.Find(announcements, "./ul/a[@href]") {
			if item := blogCard(node, pageURL, "./self::a[@href]", "", ""); item != nil {
				items = append(items, item)
			}
		}
		if len(items) > 0 {
			modules = append(modules, snapshotModule{Key: "blog.announcements", Name: "博客公告",
				Lane: "sidebar", Order: 2, State: "visible", Items: items})
		}
	}

	result := directoryResult(modules)
	// The index dates nothing, so freshness cannot be judged from it. Reporting
	// `false` would be a claim; reporting nothing is the honest answer.
	result["stale_content"] = nil
	result["pagination"] = "ssr_first_screen_only"
	result["load_more_not_called"] = true
	return result, nil
}
