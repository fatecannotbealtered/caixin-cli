package caixin

import (
	"strconv"
	"strings"

	"github.com/antchfx/htmlquery"
	xhtml "golang.org/x/net/html"
)

// The front page is three lanes: the lead block at the top, the news feed down
// the middle, and the sidebar. Each card records `source_state`, because a good
// part of the page is server-rendered but hidden until a script runs -- and a
// listing that does not say which is which invites a caller to believe the page
// showed more than it did.

// homeModules extracts the 财新 front page.
func homeModules(doc *xhtml.Node, pageURL string) ([]snapshotModule, error) {
	root := firstElement(doc, classXPath("homepageCon"))
	if root == nil {
		return nil, &APIError{Message: "the Caixin front page is missing its homepageCon"}
	}

	var modules []snapshotModule
	laneOrders := map[string]int{"lead": 0, "main": 0, "sidebar": 0}
	add := func(key, name, lane string, nodes []*xhtml.Node, options itemOptions, sourceState bool) {
		var items []map[string]any
		for _, node := range nodes {
			item := editorialItem(node, pageURL, options)
			if item == nil {
				continue
			}
			if sourceState {
				item["source_state"] = serverSourceState(node)
			}
			items = append(items, item)
		}
		if len(items) == 0 {
			return
		}
		modules = append(modules, snapshotModule{
			Key: key, Name: name, Lane: lane, Order: laneOrders[lane],
			State: "visible", Items: items,
		})
		laneOrders[lane]++
	}

	add("home.rolling", "滚动新闻", "lead",
		htmlquery.Find(root, ".//*[contains(@class,'topSubNav')]//*[contains(@class,'scrollnews')]//li"),
		itemOptions{AnchorXPaths: []string{".//a[@href]"}, ImageXPaths: []string{}}, true)

	add("home.headlines", "头条", "lead",
		findInDocumentOrder(root,
			".//*[contains(@class,'toutiao_box')]//*[contains(@class,'demolNews')]//dl",
			".//*[contains(@class,'toutiao_box')]//*[contains(@class,'lstjbd')]//dl"),
		itemOptions{AnchorXPaths: []string{".//dt/a[@href]", ".//p/a[@href]", ".//a[@href]"}}, true)

	add("home.image-headlines", "图片头条", "lead",
		htmlquery.Find(root, ".//*[contains(@class,'img_list_box')]//li"),
		itemOptions{
			AnchorXPaths: []string{".//p/a[@href]"},
			ImageXPaths:  []string{".//span//img", ".//img"},
			BadgeXPaths:  []string{".//em/a"},
		}, true)

	var feed []map[string]any
	for _, node := range htmlquery.Find(root,
		".//*["+classPredicate("main_left")+"]/div["+classPredicate("news_list")+
			"]/*[self::dl or self::div["+classPredicate("news_img_box")+"]]") {
		if item := homeFeedItem(node, pageURL); item != nil {
			feed = append(feed, item)
		}
	}
	if len(feed) > 0 {
		modules = append(modules, snapshotModule{
			Key: "home.feed", Name: "新闻流", Lane: "main",
			Order: laneOrders["main"], State: "visible", Items: feed,
		})
		laneOrders["main"]++
	}

	if sidebar := firstElement(root, ".//*["+classPredicate("main_right")+"]"); sidebar != nil {
		for index, container := range htmlquery.Find(sidebar, "./div") {
			heading := firstElement(container,
				".//h3//*["+classPredicate("current")+"]//a", ".//h3/a", ".//h3")
			name := nodeText(heading)
			if name == "" {
				continue
			}
			items := sidebarItems(container, pageURL)
			if len(items) == 0 {
				continue
			}
			modules = append(modules, snapshotModule{
				Key: homeSidebarKey(name, index), Name: name, Lane: "sidebar",
				Order: laneOrders["sidebar"], State: "visible", Items: items,
			})
			laneOrders["sidebar"]++
		}
	}
	return modules, nil
}

// homeSidebarLabels name the sidebar blocks the front page is known to carry.
// The order is fixed because the labels overlap: the first match wins, exactly
// as in the reference.
var homeSidebarLabels = []struct{ label, key string }{
	{"热选", "home.hot"},
	{"观点", "home.opinion"},
	{"市场", "home.market"},
	{"数据通热点", "home.data-hot"},
	{"视频", "home.video"},
	{"博客", "home.blog"},
	{"财新周刊", "home.magazines"},
	{"反侵权", "home.notices"},
	{"特别呈现", "home.sponsored"},
	{"mini", "home.mini"},
}

// homeSidebarKey names a sidebar block, falling back to its position.
func homeSidebarKey(name string, index int) string {
	for _, entry := range homeSidebarLabels {
		if strings.Contains(name, entry.label) {
			return entry.key
		}
	}
	return "home.sidebar-" + strconv.Itoa(index)
}

// homeFeedItem builds one card from the middle news feed.
//
// The feed mixes plain articles with photo sets; the photo set keeps every
// thumbnail, because "how many pictures" is part of what that entry is.
func homeFeedItem(node *xhtml.Node, pageURL string) map[string]any {
	gallery := false
	for _, class := range strings.Fields(attr(node, "class")) {
		if class == "news_img_box" {
			gallery = true
		}
	}
	if !gallery {
		item := editorialItem(node, pageURL, itemOptions{
			AnchorXPaths: []string{".//dd/p/a[@href]"},
			ImageXPaths:  []string{".//dt//img"},
			BadgeXPaths: []string{
				".//dd/div[contains(@class,'tit')]/em/a",
				".//*[contains(@class,'icon_')]",
			},
		})
		if item != nil {
			item["source_state"] = serverSourceState(node)
		}
		return item
	}

	item := editorialItem(node, pageURL, itemOptions{
		AnchorXPaths: []string{"./div[contains(@class,'tit')]/p/a[@href]"},
		ImageXPaths:  []string{"./ul/li/a/img"},
		BadgeXPaths:  []string{".//*[contains(@class,'icon_')]"},
	})
	if item == nil {
		return nil
	}
	images := []any{}
	seen := map[string]bool{}
	for _, node := range htmlquery.Find(node, "./ul/li/a/img") {
		source := attr(node, "data-src")
		if source == "" {
			source = attr(node, "src")
		}
		link := cultureImageURL(pageURL, source)
		if link == "" || seen[link] {
			continue
		}
		seen[link] = true
		images = append(images, link)
	}
	if len(images) > 0 {
		item["image"] = images[0]
		item["images"] = images
	}
	item["published_at"] = emptyToNil(firstXPathText(node, "./span"))
	item["item_kind"] = "gallery"
	item["source_state"] = serverSourceState(node)
	return item
}

// containsAny reports whether a badge is already listed.
func containsAny(list []any, value string) bool {
	for _, existing := range list {
		if existing == value {
			return true
		}
	}
	return false
}
