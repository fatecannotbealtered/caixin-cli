package caixin

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/antchfx/htmlquery"
	xhtml "golang.org/x/net/html"
)

// The picture and video fronts are the two pages that promise media rather than
// prose. Both say so on the way out -- `media_not_downloaded` and
// `media_streams_not_fetched` -- because a listing of galleries reads as if the
// pictures came with it, and they did not.

// photosSectionURL accepts the two standing picture directories, or a topic.
func photosSectionURL(base, value string) string {
	raw := absoluteURL(base, value)
	if raw == "" {
		return ""
	}
	if raw == "https://photos.caixin.com/sx/" || raw == "https://photos.caixin.com/photoreport/" {
		return raw
	}
	if route := ClassifyURL(raw); route.Supported && route.Adapter == "topic" {
		return route.CanonicalURL
	}
	return ""
}

// gocommentArticleURL accepts a photo-set link written with its comment anchor.
//
// The rolling block links each gallery at its comment section; the fragment is
// dropped, but its presence is what tells these links apart from the ads in the
// same list.
func gocommentArticleURL(base, value string) string {
	raw := absoluteURL(base, value)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Fragment != "gocomment" || parsed.RawQuery != "" {
		return ""
	}
	parsed.Fragment = ""
	link := articleURL(base, parsed.String())
	if link == "" {
		return ""
	}
	if hostOf(link) != "photos.caixin.com" {
		return ""
	}
	return link
}

// hostOf names a url's host in lower case, or "" when it has none.
func hostOf(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

// photosRollingItem builds one card from the rolling gallery strip.
func photosRollingItem(node *xhtml.Node, pageURL string) map[string]any {
	anchor := firstElement(node, "./div["+classPredicate("phoGroupsItemTitle")+"]/a[@href]")
	link := ""
	if anchor != nil {
		link = gocommentArticleURL(pageURL, attr(anchor, "href"))
	}
	title := firstXPathText(node, "./div["+classPredicate("phoGroupsItemTitle")+"]/span")
	if link == "" || title == "" {
		return nil
	}
	images := []any{}
	seen := map[string]bool{}
	for _, imageNode := range firstN(htmlquery.Find(node,
		"./div["+classPredicate("phoGroupsPics")+"]/img"), 4) {
		source := attr(imageNode, "data-src")
		if source == "" {
			source = attr(imageNode, "src")
		}
		image := imageURL(pageURL, source)
		if image == "" || seen[image] {
			continue
		}
		seen[image] = true
		images = append(images, image)
	}
	var lead any
	if len(images) > 0 {
		lead = images[0]
	}
	return map[string]any{
		"title":   title,
		"url":     link,
		"image":   lead,
		"images":  images,
		"summary": nil,
		"badges":  []any{},
		"meta":    emptyToNil(firstXPathText(node, "./div["+classPredicate("phoGroupsTime")+"]")),
	}
}

// photosSnapshot extracts the 图片 front page.
func photosSnapshot(doc *xhtml.Node, pageURL string) (map[string]any, error) {
	root := firstElement(doc, classXPath("comMain"))
	if root == nil {
		return nil, &APIError{Message: "the 图片 front page is missing its comMain"}
	}
	left := firstElement(root, "./div["+classPredicate("leftContent")+"]")
	right := firstElement(root, "./div["+classPredicate("rightContent")+"]")
	if left == nil || right == nil {
		return nil, &APIError{Message: "the 图片 front page is missing a content column"}
	}

	var modules []snapshotModule

	// The focus carousel shows one slide at a time; the rest are in the markup
	// but were never on screen, so the block is reported hidden with the slide
	// that was showing named explicitly.
	var focus []map[string]any
	for _, node := range htmlquery.Find(left, "./div["+classPredicate("swiperWrap")+
		"]/div["+classPredicate("leftbox01")+"]/div[@id='zyqh']/dl") {
		item := extractItem(node, pageURL, itemOptions{
			AnchorXPaths: []string{"./div[contains(@class,'wzdf')]/a[@href]"},
			ImageXPaths:  []string{"./span/a/img"},
		})
		if item != nil && hostOf(asString(item["url"])) == "photos.caixin.com" {
			focus = append(focus, item)
		}
	}
	if len(focus) > 0 {
		active := 0
		modules = append(modules, snapshotModule{
			Key: "photos.focus", Name: "焦点", Lane: "lead", Order: 0,
			State: "hidden", ActiveItemIndex: &active, Items: focus,
		})
	}

	if shadow := firstElement(left, "./div["+classPredicate("showShadow")+"]"); shadow != nil {
		var items []map[string]any
		for _, node := range htmlquery.Find(shadow,
			"./div["+classPredicate("shadowMainContent")+"]/a[@href]") {
			item := extractItem(node, pageURL, itemOptions{
				AnchorXPaths: []string{"./self::a[@href]"},
				ImageXPaths:  []string{"./div/img"},
			})
			if item != nil && hostOf(asString(item["url"])) == "weekly.caixin.com" {
				items = append(items, item)
			}
		}
		if len(items) > 0 {
			name := firstXPathText(shadow, "./div["+classPredicate("showShadowTitle")+"]/a")
			if name == "" {
				name = "显影"
			}
			module := snapshotModule{Key: "photos.shadow", Name: name, Lane: "main",
				Order: 0, State: "visible", Items: items}
			if anchor := firstElement(shadow,
				"./div["+classPredicate("showShadowTitle")+"]/a[@href]"); anchor != nil {
				module.MoreURL = photosSectionURL(pageURL, attr(anchor, "href"))
			}
			modules = append(modules, module)
		}
	}

	if rolling := firstElement(left, "./div["+classPredicate("scrollPhotoGroups")+"]"); rolling != nil {
		var items []map[string]any
		for _, node := range htmlquery.Find(rolling, "./div["+classPredicate("photoGroupsItem")+"]") {
			if item := photosRollingItem(node, pageURL); item != nil {
				items = append(items, item)
			}
		}
		if len(items) > 0 {
			name := firstXPathText(rolling, "./div["+classPredicate("photoGroupsTitle")+"]")
			if name == "" {
				name = "滚动图集"
			}
			modules = append(modules, snapshotModule{Key: "photos.rolling", Name: name,
				Lane: "main", Order: 1, State: "visible", Items: items})
		}
	}

	// The two sidebar blocks are addressed by their headings, because the
	// markup gives them the same class.
	sidebarKeys := map[string]struct {
		key   string
		order int
	}{
		"视线":   {"photos.sight", 0},
		"一周天下": {"photos.weekly-world", 1},
	}
	for _, node := range htmlquery.Find(right, "./div["+classPredicate("oneWeek")+"]") {
		anchor := firstElement(node, "./div["+classPredicate("oneWeekTitle")+"]/a[@href]")
		name := nodeText(anchor)
		contract, measured := sidebarKeys[name]
		if !measured {
			continue
		}
		item := extractItem(node, pageURL, itemOptions{
			AnchorXPaths: []string{"./a[1][@href]"},
			ImageXPaths:  []string{"./a[1]/img"},
		})
		if item == nil || hostOf(asString(item["url"])) != "photos.caixin.com" {
			continue
		}
		module := snapshotModule{Key: contract.key, Name: name, Lane: "sidebar",
			Order: contract.order, State: "visible", Items: []map[string]any{item}}
		if anchor != nil {
			module.MoreURL = photosSectionURL(pageURL, attr(anchor, "href"))
		}
		modules = append(modules, module)
	}

	if recent := firstElement(right, "./div["+classPredicate("rencentPic")+"]"); recent != nil {
		var items []map[string]any
		for _, node := range htmlquery.Find(recent, "./a[@href]") {
			item := extractItem(node, pageURL, itemOptions{
				AnchorXPaths: []string{"./self::a[@href]"},
				ImageXPaths:  []string{"./div/img"},
			})
			if item != nil && hostOf(asString(item["url"])) == "photos.caixin.com" {
				items = append(items, item)
			}
		}
		if len(items) > 0 {
			name := firstXPathText(recent, "./div["+classPredicate("rencentPicTitle")+"]")
			if name == "" {
				name = "近期热图"
			}
			modules = append(modules, snapshotModule{Key: "photos.recent", Name: name,
				Lane: "sidebar", Order: 2, State: "visible", Items: items})
		}
	}

	result := directoryResult(modules)
	result["media_not_downloaded"] = true
	return result, nil
}

// backgroundImage pulls a css background url out of an inline style.
var backgroundImage = regexp.MustCompile(
	`(?i)(?:^|;)\s*background-image\s*:\s*url\(\s*['"]?([^'"()\s;]+)['"]?\s*\)`)

// styleBackgroundImage reads the picture a card carries in css rather than in
// an <img>.
func styleBackgroundImage(pageURL, style string) string {
	match := backgroundImage.FindStringSubmatch(style)
	if match == nil {
		return ""
	}
	return imageURL(pageURL, match[1])
}

// videoArticleURL accepts a video page link.
func videoArticleURL(base, value string) string {
	link := contentURL(base, value)
	if link == "" || hostOf(link) != "video.caixin.com" {
		return ""
	}
	return link
}

// videoNavigationPaths are the two shapes the channel bar links: a video page,
// or one of the channel's own directories.
var videoNavigationArticle = regexp.MustCompile(`^/20\d{2}-\d{2}-\d{2}/\d{1,20}\.html$`)
var videoNavigationSection = regexp.MustCompile(`^/(?:20\d{2}/)?[A-Za-z0-9_-]{1,64}/$`)

// videoNavigationURL accepts a link from the video channel bar.
func videoNavigationURL(base, value string) string {
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
	if strings.ToLower(parsed.Hostname()) != "video.caixin.com" {
		return ""
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	if port := parsed.Port(); port != "" && port != "80" && port != "443" {
		return ""
	}
	if !videoNavigationArticle.MatchString(parsed.Path) &&
		!videoNavigationSection.MatchString(parsed.Path) {
		return ""
	}
	parsed.Scheme = "https"
	parsed.Host = "video.caixin.com"
	return parsed.String()
}

// videoListItem builds one card from a video list.
//
// A card is only accepted when its picture and its headline lead to the same
// video. In this layout a disagreement means the slot is an ad wearing a card's
// markup, not a video with two links.
func videoListItem(node *xhtml.Node, pageURL string, promoted bool) map[string]any {
	var imageAnchor, titleAnchor *xhtml.Node
	if promoted {
		imageAnchor = firstElement(node, "./a[.//img][1][@href]")
		titleAnchor = firstElement(node, "./a["+classPredicate("listbtm")+"][.//h3][@href]")
	} else {
		imageAnchor = firstElement(node, ".//div["+classPredicate("listop")+"]/a[.//img][1][@href]")
		titleAnchor = firstElement(node,
			".//a["+classPredicate("figure_caption")+"][.//h3][@href]",
			".//a["+classPredicate("listbtm")+"][.//h3][@href]")
	}
	imageLink := ""
	if imageAnchor != nil {
		imageLink = videoArticleURL(pageURL, attr(imageAnchor, "href"))
	}
	titleLink := ""
	title := ""
	if titleAnchor != nil {
		titleLink = videoArticleURL(pageURL, attr(titleAnchor, "href"))
		title = firstXPathText(titleAnchor, ".//h3")
	}
	if imageLink == "" || imageLink != titleLink || title == "" {
		return nil
	}

	image := ""
	if imageNode := firstElement(imageAnchor, ".//img"); imageNode != nil {
		source := attr(imageNode, "data-src")
		if source == "" {
			source = attr(imageNode, "src")
		}
		image = imageURL(pageURL, source)
	}
	item := map[string]any{
		"title":   title,
		"url":     imageLink,
		"image":   emptyToNil(image),
		"summary": nil,
		"badges":  []any{},
	}
	if promoted {
		item["promoted_slot"] = true
		return item
	}
	if category := firstXPathText(node,
		".//div["+classPredicate("listop")+"]/em["+classPredicate("flagbg")+"]"); category != "" {
		item["category"] = category
	}
	date := firstXPathText(node, ".//span["+classPredicate("date")+"][1]")
	clock := firstXPathText(node, ".//span["+classPredicate("time")+"][1]")
	meta := strings.TrimSpace(strings.Join(nonEmpty(date, clock), " "))
	if meta != "" {
		item["meta"] = meta
	}
	return item
}

// nonEmpty drops the blanks from a small list of strings.
func nonEmpty(values ...string) []string {
	var kept []string
	for _, value := range values {
		if value != "" {
			kept = append(kept, value)
		}
	}
	return kept
}

// videoSnapshot extracts the 视频 front page.
func videoSnapshot(doc *xhtml.Node, pageURL string) (map[string]any, error) {
	var modules []snapshotModule

	var hero []map[string]any
	for _, anchor := range htmlquery.Find(doc,
		"//div["+classPredicate("head")+"]/div["+classPredicate("headcenter")+
			"]/div["+classPredicate("fl")+"]/a[@href]") {
		link := videoArticleURL(pageURL, attr(anchor, "href"))
		title := firstXPathText(anchor, "./div["+classPredicate("newstit")+"]/h2")
		if link == "" || title == "" {
			continue
		}
		item := map[string]any{
			"title":   title,
			"url":     link,
			"image":   emptyToNil(styleBackgroundImage(pageURL, attr(anchor, "style"))),
			"summary": nil,
			"badges":  []any{},
		}
		if category := firstXPathText(anchor,
			"./div["+classPredicate("newstit")+"]/em["+classPredicate("flagbg")+"]"); category != "" {
			item["category"] = category
		}
		hero = append(hero, item)
	}
	if len(hero) > 0 {
		active := 0
		modules = append(modules, snapshotModule{Key: "video.hero", Name: "焦点视频",
			Lane: "lead", Order: 0, State: "visible", ActiveItemIndex: &active, Items: hero})
	}

	var navigation []map[string]any
	seen := map[string]bool{}
	for _, anchor := range htmlquery.Find(doc, "//div["+classPredicate("navlist")+"]//a[@href]") {
		link := videoNavigationURL(pageURL, attr(anchor, "href"))
		title := nodeText(anchor)
		if link == "" || title == "" || seen[link] {
			continue
		}
		kind := "section_navigation"
		if videoArticleURL(pageURL, link) != "" {
			kind = "article_navigation"
		}
		seen[link] = true
		navigation = append(navigation, map[string]any{
			"title":               title,
			"url":                 link,
			"image":               nil,
			"summary":             nil,
			"badges":              []any{},
			"item_kind":           kind,
			"content_not_fetched": true,
			"source_state":        serverSourceState(anchor),
		})
	}
	if len(navigation) > 0 {
		modules = append(modules, snapshotModule{Key: "video.navigation", Name: "频道导航",
			Lane: "navigation", Order: 0, State: "visible", Items: navigation})
	}

	for _, contract := range []struct {
		marker, key, name, boxClass string
		order                       int
	}{
		{"zuixin", "video.latest", "最新视频", "box2", 0},
		{"zuire", "video.hot", "最热视频", "box1", 1},
		{"hezuo", "video.partners", "合作专区", "box2", 2},
	} {
		root := firstElement(doc, "//div["+classPredicate(contract.boxClass)+
			"]/div["+classPredicate("newvideo")+"][div["+classPredicate("bigtit")+
			"]/span["+classPredicate(contract.marker)+"]]")
		if root == nil {
			continue
		}
		var items []map[string]any
		if contract.marker == "zuixin" {
			for _, node := range findInDocumentOrder(root,
				"./div["+classPredicate("videolist")+"]/ul["+classPredicate("fl")+"]/li",
				"./div["+classPredicate("videolist")+"]/div["+classPredicate("rightwarp")+
					"]/ul["+classPredicate("fright")+"]/li") {
				if item := videoListItem(node, pageURL, false); item != nil {
					items = append(items, item)
				}
			}
		} else {
			for _, node := range htmlquery.Find(root,
				"./div["+classPredicate("videolist")+"]/ul["+classPredicate("hotnew")+"]/li") {
				if item := videoListItem(node, pageURL, false); item != nil {
					items = append(items, item)
				}
			}
			if contract.marker == "zuire" {
				// The promoted slot sits in the same list and is labelled, not
				// dropped: it is real content, just paid for.
				for _, node := range htmlquery.Find(root,
					"./div["+classPredicate("videolist")+"]/div["+classPredicate("liad")+"]") {
					if item := videoListItem(node, pageURL, true); item != nil {
						items = append(items, item)
					}
				}
			}
		}
		if len(items) > 0 {
			modules = append(modules, snapshotModule{Key: contract.key, Name: contract.name,
				Lane: "main", Order: contract.order, State: "visible", Items: items})
		}
	}

	result := attachClickConsumers(directoryResult(modules))
	result["media_streams_not_fetched"] = true
	result["media_not_downloaded"] = true
	return result, nil
}

// videoSectionPath is the video channel's own directory shape.
var videoSectionPath = regexp.MustCompile(`^/(?:20\d{2}/)?[A-Za-z0-9_-]{1,64}/$`)

// videoSectionURL accepts a 视频 channel directory.
func videoSectionURL(raw string) string {
	if raw == "" || strings.ContainsAny(raw, " \t\n\r") {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return ""
	}
	if strings.ToLower(parsed.Hostname()) != "video.caixin.com" {
		return ""
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return ""
	}
	if !videoSectionPath.MatchString(parsed.Path) {
		return ""
	}
	parsed.Host = "video.caixin.com"
	return parsed.String()
}
