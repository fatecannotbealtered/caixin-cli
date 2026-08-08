package caixin

import (
	"context"
	"net/http"
	neturl "net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/antchfx/htmlquery"
	xhtml "golang.org/x/net/html"
)

// 火线评论 renders twenty cards and loads the rest from the same gateway the
// other directories use, with the subject id printed into an inline script. The
// id is read off the page and then checked against the one this build was
// written against: a different subject would return a different column entirely.

var opinionUpfrontSubject = regexp.MustCompile(
	`\b(?:window\.)?(?:var\s+|let\s+|const\s+)?subjectId\s*=\s*['"]?([1-9]\d{8})['"]?`)

// opinionUpfrontItem builds one 火线评论 card.
func opinionUpfrontItem(node *xhtml.Node, pageURL string) (map[string]any, error) {
	link := validateArticleURL(absoluteURL(pageURL, attr(node, "href")))
	if link == "" {
		return nil, &APIError{Message: "a 火线评论 card links something that is not an article"}
	}
	titles := htmlquery.Find(node, ".//*["+classPredicate("newsRightConTitle")+"]")
	if len(titles) != 1 {
		return nil, &APIError{Message: "a 火线评论 card has no single title"}
	}
	title := nodeText(titles[0])
	if title == "" {
		return nil, &APIError{Message: "a 火线评论 card has no single title"}
	}
	image := ""
	if imageNode := firstElement(node,
		"./img["+classPredicate("newsLeftPic")+"][1]"); imageNode != nil {
		source := attr(imageNode, "data-src")
		if source == "" {
			source = attr(imageNode, "src")
		}
		image = cultureImageURL(pageURL, source)
	}
	badges := []any{}
	if len(htmlquery.Find(titles[0], ".//*[contains(@class,'icon_key')]")) > 0 {
		badges = append(badges, "收费文章")
	}
	return map[string]any{
		"title":        title,
		"url":          link,
		"image":        emptyToNil(image),
		"summary":      emptyToNil(firstXPathText(node, ".//*["+classPredicate("newsRightConText")+"]")),
		"published_at": emptyToNil(firstXPathText(node, ".//*["+classPredicate("newsRightConTime")+"]")),
		"badges":       badges,
	}, nil
}

// extractOpinionUpfront reads the 火线评论 directory's first screen.
func extractOpinionUpfront(doc *xhtml.Node, pageURL string) (map[string]any, map[string]any, error) {
	roots := htmlquery.Find(doc,
		"//div["+classPredicate("mainContent")+"][.//div["+classPredicate("NewsList")+"]]")
	if len(roots) != 1 {
		return nil, nil, &APIError{Message: "the 火线评论 directory has no single populated mainContent"}
	}
	lists := htmlquery.Find(roots[0], ".//div["+classPredicate("NewsList")+"]")
	if len(lists) != 1 {
		return nil, nil, &APIError{Message: "the 火线评论 directory has no single NewsList"}
	}
	nodes := htmlquery.Find(lists[0], "./a["+classPredicate("newsItem")+"][@href]")
	// The screen size feeds the continuation's paging maths, so a different
	// count means the contract no longer holds.
	if len(nodes) != 20 {
		return nil, nil, &APIError{Message: "the 火线评论 first screen is no longer twenty cards"}
	}
	controls := htmlquery.Find(lists[0], ".//*[@id='loadmore']")
	if len(controls) != 1 || nodeText(controls[0]) == "" {
		return nil, nil, &APIError{Message: "the 火线评论 load-more control changed"}
	}
	subjects := opinionUpfrontSubject.FindAllStringSubmatch(inlineScriptSource(doc), -1)
	if len(subjects) != 1 || subjects[0][1] != "100239120" {
		return nil, nil, &APIError{Message: "the 火线评论 subject contract changed"}
	}
	scriptURL, err := opinionDirectoryScriptURL(doc, pageURL, opinionUpfrontScriptURL, "火线评论")
	if err != nil {
		return nil, nil, err
	}

	var items []map[string]any
	for _, node := range nodes {
		item, err := opinionUpfrontItem(node, pageURL)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, item)
	}
	details := attachClickConsumers(directoryResult([]snapshotModule{{
		Key: "opinion-upfront.articles", Name: "火线评论文章",
		Lane: "main", Order: 0, State: "server_rendered", Items: items,
	}}))
	for _, item := range items {
		if consumer, ok := item["consumer"].(map[string]any); ok {
			if supported, _ := consumer["supported"].(bool); !supported {
				return nil, nil, &APIError{
					Message: "a 火线评论 card links an article this build cannot read",
				}
			}
		}
	}
	details["kind"] = "opinion_upfront"
	details["title"] = emptyToNil(firstXPathText(doc, "//title"))
	details["scripts_not_executed"] = true
	details["linked_pages_not_fetched"] = true

	return details, map[string]any{
		"endpoint":   TopicDirectoryApi,
		"parameter":  "subject",
		"identifier": "100239120",
		"count":      20,
		"picdim":     "_266_177",
		"type":       0,
		"control":    "loadmore",
		"script_url": scriptURL,
	}, nil
}

// validateOpinionUpfrontScript checks the external script still builds the
// request this build reproduces.
func validateOpinionUpfrontScript(source string) error {
	compact := cssWhitespace.ReplaceAllString(source, "")
	endpoint := regexp.MustCompile(regexp.QuoteMeta(TopicDirectoryApi) +
		`\?start=\$\{num\}&subject=\$\{subject\}&type=0&count=\$\{count\}&picdim=_266_177&_=\d+`)
	if !regexp.MustCompile(`(?:let|var|const)start=20;`).MatchString(compact) ||
		!regexp.MustCompile(`(?:let|var|const)count=20;`).MatchString(compact) ||
		len(endpoint.FindAllString(compact, -1)) != 1 ||
		!strings.Contains(compact, "start+=count;") ||
		!strings.Contains(compact, "getList(start,subjectId)") {
		return &APIError{Message: "the 火线评论 continuation script changed"}
	}
	return nil
}

// OpinionUpfront lists the 火线评论 directory.
func (c *Client) OpinionUpfront(ctx context.Context, pageURL string, page int) (map[string]any, error) {
	return c.opinionDirectory(ctx, pageURL, page, opinionDirectorySpec{
		Adapter:    "opinion-upfront",
		Kind:       "opinion_upfront",
		ScriptURL:  opinionUpfrontScriptURL,
		Label:      "火线评论",
		ModuleKey:  "opinion-upfront.continuation",
		ModuleName: "火线评论续载",
		Extract:    extractOpinionUpfront,
		Validate:   validateOpinionUpfrontScript,
	})
}

// validateOpinionAuthorDirectoryScript checks the roster's script still calls
// the endpoint this build calls, one page at a time.
func validateOpinionAuthorDirectoryScript(source string) error {
	compact := cssWhitespace.ReplaceAllString(source, "")
	api := regexp.MustCompile(regexp.QuoteMeta(opinionAuthorDirectoryAPI) + `\?page=\$\{page\}&size=20`)
	if !regexp.MustCompile(`(?:let|var|const)page=1;`).MatchString(compact) ||
		len(api.FindAllString(compact, -1)) != 1 ||
		!strings.Contains(compact, "page++;") ||
		!regexp.MustCompile(`document\.getElementById\(["']loadmore["']\)\.click\(\);`).
			MatchString(compact) {
		return &APIError{Message: "the 观点作者 directory script changed"}
	}
	return nil
}

// opinionAuthorDirectoryTemplate confirms the roster is the empty shell this
// build expects, and names the script that fills it.
//
// The list arrives from an API, so a server-rendered entry here would mean the
// page changed shape and the API is no longer the whole story.
func opinionAuthorDirectoryTemplate(doc *xhtml.Node, pageURL string) (string, error) {
	roots := htmlquery.Find(doc, "//div["+classPredicate("comMainCon")+"]")
	if len(roots) != 1 {
		return "", &APIError{Message: "the 观点作者 directory has no single comMainCon"}
	}
	lists := htmlquery.Find(roots[0],
		".//div["+classPredicate("leftContent")+"]//div["+classPredicate("SearchWriterList")+"]")
	if len(lists) != 1 || len(htmlquery.Find(lists[0], "./*")) > 0 {
		return "", &APIError{Message: "the 观点作者 directory's dynamic list template changed"}
	}
	controls := htmlquery.Find(roots[0], ".//*[@id='loadmore']")
	if len(controls) != 1 || nodeText(controls[0]) == "" {
		return "", &APIError{Message: "the 观点作者 directory's load control changed"}
	}
	return opinionDirectoryScriptURL(doc, pageURL, opinionAuthorDirectoryScriptURL, "观点作者目录")
}

// OpinionAuthorDirectory lists the 观点作者 roster.
func (c *Client) OpinionAuthorDirectory(
	ctx context.Context,
	pageURL string,
	page int,
) (map[string]any, error) {
	if page < 1 {
		return nil, invalid("opinion-author-directory page must be positive")
	}
	canonical, adapter := opinionDirectoryURL(strings.TrimSpace(pageURL))
	if canonical == "" || adapter != "opinion-author-directory" {
		return nil, invalid("opinion-author-directory reads the 观点作者 roster at " +
			"https://opinion.caixin.com/columns-test/")
	}
	discoveredFrom, card, err := c.opinionDirectoryDiscovery(ctx, canonical, adapter)
	if err != nil {
		return nil, err
	}
	doc, err := c.fetchHTML(ctx, canonical, "could not parse the 观点作者 directory")
	if err != nil {
		return nil, err
	}
	scriptURL, err := opinionAuthorDirectoryTemplate(doc, canonical)
	if err != nil {
		return nil, err
	}
	script, err := c.scriptSource(ctx, scriptURL)
	if err != nil {
		return nil, err
	}
	if err := validateOpinionAuthorDirectoryScript(script); err != nil {
		return nil, err
	}

	value, err := c.requestJSON(ctx, requestSpec{
		Method: http.MethodGet, URL: opinionAuthorDirectoryAPI,
		Query: neturl.Values{"page": {strconv.Itoa(page)}, "size": {"20"}},
		Headers: map[string]string{
			"User-Agent": TopicsCompatUserAgent,
			"Origin":     "https://opinion.caixin.com",
			"Referer":    canonical,
		},
		Anonymous: true,
	})
	if err != nil {
		return nil, err
	}
	if plainText(value["code"]) != "0" {
		return nil, &APIError{
			Message: "reading the 观点作者 directory failed (code=" + plainText(value["code"]) + ")",
		}
	}
	data, ok := value["data"].(map[string]any)
	if !ok {
		return nil, &APIError{Message: "the 观点作者 directory response changed shape"}
	}
	rows, isList := data["list"].([]any)
	total, hasTotal := safeInt(data["total"])
	if !isList || len(rows) > 20 || !hasTotal || total < 0 {
		return nil, &APIError{Message: "the 观点作者 directory response changed shape"}
	}

	var items []map[string]any
	seen := map[string]bool{}
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			return nil, &APIError{Message: "the 观点作者 directory returned an entry this build cannot read"}
		}
		title := plainText(row["title"])
		link, _ := opinionAuthorURL(plainText(row["url"]))
		if title == "" || link == "" || seen[link] {
			return nil, &APIError{Message: "the 观点作者 directory returned an entry this build cannot read"}
		}
		seen[link] = true
		items = append(items, map[string]any{
			"title":     title,
			"url":       link,
			"image":     emptyToNil(cultureImageURL(canonical, plainText(row["logo"]))),
			"summary":   emptyToNil(plainText(row["summary"])),
			"badges":    []any{},
			"item_kind": "author_profile",
		})
	}

	result := directoryResult([]snapshotModule{{
		Key: "opinion-author-directory.authors", Name: "观点作者",
		Lane: "main", Order: 0, State: "server_returned", Items: items,
	}})
	for _, item := range items {
		consumer := opinionAuthorSourceConsumer(asString(item["url"]), "author-directory", page)
		if consumer == nil {
			return nil, &APIError{Message: "the 观点作者 directory returned an entry this build cannot read"}
		}
		item["consumer"] = consumer
	}

	result["source_mode"] = "server_html_discovery_plus_json_api"
	result["source"] = map[string]any{
		"requested_url":   pageURL,
		"canonical_url":   canonical,
		"final_url":       canonical,
		"discovered_from": discoveredFrom,
		"api_url":         opinionAuthorDirectoryAPI,
		"script_url":      scriptURL,
		"fetched_at":      time.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	result["kind"] = "opinion_author_directory"
	result["directory_title"] = emptyToNil(asString(card["title"]))
	result["title"] = emptyToNil(firstXPathText(doc, "//title"))
	result["page"] = page
	result["session_used"] = false
	result["returned"] = len(items)
	result["reported_total"] = total
	result["pagination"] = map[string]any{
		"page":             page,
		"page_size":        20,
		"explicit_only":    true,
		"automatic_follow": false,
		"page_empty":       len(items) == 0,
	}
	result["linked_pages_not_fetched"] = true
	result["scripts_not_executed"] = true
	return result, nil
}
