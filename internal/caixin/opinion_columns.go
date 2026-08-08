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

// The three 观点 directories each server-render one screen and load the rest
// from a gateway endpoint whose parameters live in an external script. Rather
// than hard-code those parameters, each command reads them off the page and
// then checks the script still says what this build was written against. If
// either has changed, the read stops -- a continuation request built from a
// stale contract would return a plausible list of the wrong thing.

const (
	opinionColumnsScriptURL         = "https://file.caixin.com/webjs/channel/index_cxmingjia_loadMore.js"
	opinionUpfrontScriptURL         = "https://file.caixin.com/webchannel/newBlog/column/index.js"
	opinionAuthorDirectoryScriptURL = "https://file.caixin.com/webchannel/newBlog/writerlist/index.js"
	opinionAuthorDirectoryAPI       = "https://gateway.caixin.com/api/data//cms-data/columnAuthorList"
)

// opinionAuthorPage is the author page shape the directories link.
var opinionAuthorPage = regexp.MustCompile(`^/([A-Za-z0-9_-]{1,64})(?:/|/index\.html)$`)

// opinionAuthorURL canonicalizes a 观点 columnist page.
func opinionAuthorURL(raw string) (canonical, authorKey string) {
	if raw == "" || strings.ContainsAny(raw, " \t\n\r") {
		return "", ""
	}
	parsed, err := neturl.Parse(raw)
	if err != nil || parsed.User != nil {
		return "", ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", ""
	}
	if strings.ToLower(parsed.Hostname()) != "opinion.caixin.com" {
		return "", ""
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ""
	}
	if port := parsed.Port(); port != "" && port != "80" && port != "443" {
		return "", ""
	}
	match := opinionAuthorPage.FindStringSubmatch(parsed.Path)
	if match == nil {
		return "", ""
	}
	return "https://opinion.caixin.com/" + match[1] + "/", match[1]
}

// opinionAuthorSourceConsumer names the command that reads an author page, and
// records how it was found.
//
// The discovery source is part of the command, not decoration: an author page
// is only read after a directory listed it, so the caller has to pass on which
// directory that was.
func opinionAuthorSourceConsumer(raw, source string, directoryPage int) map[string]any {
	canonical, _ := opinionAuthorURL(raw)
	if canonical == "" {
		return nil
	}
	command := []string{"opinion-author", canonical, "--discovery-source", source}
	route := Route{
		InputURL:                raw,
		CanonicalURL:            canonical,
		ContentAccessNotImplied: true,
		DiscoveryRequired:       true,
	}
	if directoryPage > 0 {
		command = append(command, "--directory-page", strconv.Itoa(directoryPage))
	}
	route.set("opinion-author", command)
	fields := route.AsEmbeddedMap()
	fields["discovery_source"] = source
	if directoryPage > 0 {
		fields["directory_page"] = directoryPage
	}
	return fields
}

// opinionDirectoryScriptURL confirms the page still loads the exact script this
// build knows how to read.
func opinionDirectoryScriptURL(doc *xhtml.Node, pageURL, expected, label string) (string, error) {
	matches := 0
	for _, node := range htmlquery.Find(doc, "//script[@src]") {
		if absoluteURL(pageURL, attr(node, "src")) == expected {
			matches++
		}
	}
	if matches != 1 {
		return "", &APIError{Message: "the " + label + " page's external script changed"}
	}
	return expected, nil
}

// inlineScriptSource concatenates the page's inline scripts.
func inlineScriptSource(doc *xhtml.Node) string {
	var parts []string
	for _, node := range htmlquery.Find(doc, "//script") {
		if attr(node, "src") == "" {
			parts = append(parts, htmlquery.InnerText(node))
		}
	}
	return strings.Join(parts, "\n")
}

var opinionColumnsControl = regexp.MustCompile(
	`^\s*loadMoreNewses\(\s*0\s*,\s*100000091\s*,\s*1\s*,\s*7\s*\)\s*;?\s*$`)

// opinionColumnsContract reads the continuation parameters off the page.
func opinionColumnsContract(doc *xhtml.Node, pageURL string) (map[string]any, error) {
	var controls []*xhtml.Node
	for _, node := range htmlquery.Find(doc, "//*[@onclick]") {
		if serverSourceState(node) == "visible" &&
			strings.Contains(attr(node, "onclick"), "loadMoreNewses(") {
			controls = append(controls, node)
		}
	}
	if len(controls) != 1 || !opinionColumnsControl.MatchString(attr(controls[0], "onclick")) {
		return nil, &APIError{Message: "the 名家 directory's load-more control changed"}
	}
	scriptURL, err := opinionDirectoryScriptURL(doc, pageURL, opinionColumnsScriptURL, "名家")
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"endpoint":   TopicDirectoryApi,
		"parameter":  "subject",
		"identifier": "100000091",
		"count":      7,
		"picdim":     "_145_97",
		"type":       2,
		"control":    "loadMoreNewses",
		"script_url": scriptURL,
	}, nil
}

// opinionColumnsItem builds one card, which names its author as well as its
// article. The picture links the author, not the piece, so the disagreement is
// recorded rather than resolved.
func opinionColumnsItem(node *xhtml.Node, pageURL string) (map[string]any, error) {
	item, err := sectionCardItem(node, pageURL)
	if err != nil {
		return nil, err
	}
	authorName := firstXPathText(node, "./dt/span/a[1]")
	authors := map[string]bool{}
	for _, anchor := range htmlquery.Find(node, "./dt//a[@href]") {
		canonical, _ := opinionAuthorURL(absoluteURL(pageURL, attr(anchor, "href")))
		if canonical == "" {
			return nil, &APIError{Message: "a 名家 card carries an author link this build cannot read"}
		}
		authors[canonical] = true
	}
	if len(authors) > 1 {
		return nil, &APIError{Message: "a 名家 card names more than one author"}
	}
	for canonical := range authors {
		item["author"] = emptyToNil(authorName)
		item["author_url"] = canonical
		item["image_click_url"] = canonical
	}
	return item, nil
}

// OpinionColumns lists the 名家 directory.
func (c *Client) OpinionColumns(ctx context.Context, pageURL string, page int) (map[string]any, error) {
	return c.opinionDirectory(ctx, pageURL, page, opinionDirectorySpec{
		Adapter:    "opinion-columns",
		Kind:       "opinion_columns",
		ScriptURL:  opinionColumnsScriptURL,
		Label:      "名家专栏",
		ModuleKey:  "opinion-columns.continuation",
		ModuleName: "名家专栏续载",
		Extract:    extractOpinionColumns,
		Validate:   validateOpinionColumnsScript,
	})
}

// extractOpinionColumns reads the 名家 directory's first screen.
func extractOpinionColumns(doc *xhtml.Node, pageURL string) (map[string]any, map[string]any, error) {
	roots := htmlquery.Find(doc, "//div["+classPredicate("indexMain")+"]")
	if len(roots) != 1 {
		return nil, nil, &APIError{Message: "the 名家 directory has no single indexMain"}
	}
	lists := htmlquery.Find(roots[0], ".//*[@id='listArticle']")
	if len(lists) != 1 {
		return nil, nil, &APIError{Message: "the 名家 directory has no single article list"}
	}
	nodes := htmlquery.Find(lists[0], "./dl")
	// The screen size is part of the contract the continuation depends on, so a
	// different count means the page changed and the paging maths would be off.
	if len(nodes) != 7 {
		return nil, nil, &APIError{Message: "the 名家 directory's first screen is no longer seven cards"}
	}
	var items []map[string]any
	for _, node := range nodes {
		item, err := opinionColumnsItem(node, pageURL)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, item)
	}
	details := attachClickConsumers(directoryResult([]snapshotModule{{
		Key: "opinion-columns.articles", Name: "财新名家文章",
		Lane: "main", Order: 0, State: "server_rendered", Items: items,
	}}))
	for _, item := range items {
		if authorURL, ok := item["author_url"].(string); ok {
			consumer := opinionAuthorSourceConsumer(authorURL, "columns", 0)
			item["author_consumer"] = consumer
			item["image_click_consumer"] = consumer
		}
		if consumer, ok := item["consumer"].(map[string]any); ok {
			if supported, _ := consumer["supported"].(bool); !supported {
				return nil, nil, &APIError{
					Message: "a 名家 card links an article this build cannot read",
				}
			}
		}
	}
	details["kind"] = "opinion_columns"
	details["title"] = emptyToNil(firstXPathText(doc, "//title"))
	details["scripts_not_executed"] = true
	details["linked_pages_not_fetched"] = true

	contract, err := opinionColumnsContract(doc, pageURL)
	if err != nil {
		return nil, nil, err
	}
	return details, contract, nil
}

// validateOpinionColumnsScript checks the external script still builds the
// request this build reproduces.
func validateOpinionColumnsScript(source string) error {
	parameters, body, err := javascriptNamedFunction(source, "loadMoreNewses")
	if err != nil {
		return err
	}
	if len(parameters) != 4 {
		return &APIError{Message: "the 名家 continuation script changed"}
	}
	subject, pageNumber, count := parameters[1], parameters[2], parameters[3]
	compact := cssWhitespace.ReplaceAllString(body, "")
	pattern := regexp.MustCompile(
		`https://gateway\.caixin\.com/api/extapi/homeInterface\.jsp\?type=2&subject=` +
			`["']\+` + regexp.QuoteMeta(subject) + `\+["']&start=["']\+` +
			regexp.QuoteMeta(pageNumber) + `\*` + regexp.QuoteMeta(count) + `\+["']` +
			`&count=["']\+` + regexp.QuoteMeta(count) + `\+["']&picdim=_145_97&callback=\?`)
	if !pattern.MatchString(compact) {
		return &APIError{Message: "the 名家 continuation script changed"}
	}
	return nil
}

// opinionDirectorySpec describes one of the three 观点 directories.
type opinionDirectorySpec struct {
	Adapter    string
	Kind       string
	ScriptURL  string
	Label      string
	ModuleKey  string
	ModuleName string
	Extract    func(*xhtml.Node, string) (map[string]any, map[string]any, error)
	Validate   func(string) error
}

// opinionDirectory runs the shared read: discover, fetch, extract, then either
// return the first screen or the requested continuation page.
func (c *Client) opinionDirectory(
	ctx context.Context,
	pageURL string,
	page int,
	spec opinionDirectorySpec,
) (map[string]any, error) {
	if page < 1 {
		return nil, invalid(spec.Adapter + " page must be positive")
	}
	canonical, adapter := opinionDirectoryURL(strings.TrimSpace(pageURL))
	if canonical == "" || adapter != spec.Adapter {
		return nil, invalid(spec.Adapter + " reads its own 观点 directory entry point; " +
			"run `caixin-cli reference` for the list")
	}

	discoveredFrom, card, err := c.opinionDirectoryDiscovery(ctx, canonical, adapter)
	if err != nil {
		return nil, err
	}
	doc, err := c.fetchHTML(ctx, canonical, "could not parse the 观点 directory page")
	if err != nil {
		return nil, err
	}
	details, contract, err := spec.Extract(doc, canonical)
	if err != nil {
		return nil, err
	}
	script, err := c.scriptSource(ctx, asString(contract["script_url"]))
	if err != nil {
		return nil, err
	}
	if err := spec.Validate(script); err != nil {
		return nil, err
	}

	var result map[string]any
	if page == 1 {
		result = details
		result["source_mode"] = "server_html"
		result["pagination"] = map[string]any{
			"control_present":  true,
			"supported":        true,
			"explicit_only":    true,
			"automatic_follow": false,
			"next_page":        2,
			"contract":         contract,
		}
	} else {
		result, err = c.opinionDirectoryPage(ctx, canonical, contract, page,
			spec.Label, spec.ModuleKey, spec.ModuleName)
		if err != nil {
			return nil, err
		}
	}
	result["source"] = map[string]any{
		"requested_url":   pageURL,
		"canonical_url":   canonical,
		"final_url":       canonical,
		"discovered_from": discoveredFrom,
		"fetched_at":      time.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	result["kind"] = spec.Kind
	result["directory_title"] = emptyToNil(asString(card["title"]))
	result["page"] = page
	result["session_used"] = false
	return result, nil
}

// opinionDirectoryDiscovery confirms the 观点 front still lists this directory.
func (c *Client) opinionDirectoryDiscovery(
	ctx context.Context,
	canonical, adapter string,
) (string, map[string]any, error) {
	doc, err := c.fetchHTML(ctx, opinionRootURL, "could not parse the 观点 front page")
	if err != nil {
		return "", nil, err
	}
	for _, item := range opinionDirectoryNavigation(doc, opinionRootURL) {
		if item["url"] == canonical && item["adapter"] == adapter {
			return opinionRootURL, item, nil
		}
	}
	return "", nil, &APIError{
		StatusCode: 404,
		Message:    "that 观点 directory is not linked from " + opinionRootURL + " right now",
	}
}

// opinionDirectoryPage fetches one continuation page.
func (c *Client) opinionDirectoryPage(
	ctx context.Context,
	pageURL string,
	contract map[string]any,
	page int,
	label, moduleKey, moduleName string,
) (map[string]any, error) {
	count, _ := safeInt(contract["count"])
	kind, _ := safeInt(contract["type"])
	start := (page - 1) * count
	value, err := c.requestJSON(ctx, requestSpec{
		Method: http.MethodGet, URL: asString(contract["endpoint"]),
		Query: neturl.Values{
			asString(contract["parameter"]): {asString(contract["identifier"])},
			"start":                         {strconv.Itoa(start)},
			"count":                         {strconv.Itoa(count)},
			"type":                          {strconv.Itoa(kind)},
			"picdim":                        {asString(contract["picdim"])},
		},
		Headers: map[string]string{
			"User-Agent": TopicsCompatUserAgent,
			"Origin":     "https://opinion.caixin.com",
			"Referer":    pageURL,
		},
		Anonymous: true,
	})
	if err != nil {
		return nil, err
	}
	rows, reportedMaxes, upstreamVersion, err := gatewayRows(value, start, count, label)
	if err != nil {
		return nil, err
	}

	var items []map[string]any
	for _, row := range rows {
		item := gatewayItem(row, pageURL, gatewayItemOptions{
			IncludeSummary: true, MaxImages: 1, BadgeMode: "home",
		})
		if item == nil {
			return nil, &APIError{
				Message: "the " + label + " continuation returned an entry this build cannot read",
			}
		}
		consumer := ClassifyURL(asString(item["url"])).AsEmbeddedMap()
		if supported, _ := consumer["supported"].(bool); !supported {
			return nil, &APIError{
				Message: "the " + label + " continuation returned an entry this build cannot read",
			}
		}
		item["consumer"] = consumer
		items = append(items, item)
	}

	details := directoryResult([]snapshotModule{{
		Key: moduleKey, Name: moduleName, Lane: "main",
		Order: 0, State: "server_returned", Items: items,
	}})
	details["source_mode"] = "server_html_discovery_plus_json_api"
	details["returned"] = len(items)
	details["reported_maxes"] = reportedMaxes
	details["upstream_version"] = upstreamVersion
	details["pagination"] = map[string]any{
		"page":                page,
		"page_size":           count,
		"start":               start,
		"discovered_subject":  contract["identifier"],
		"discovered_endpoint": contract["endpoint"],
		"discovered_type":     contract["type"],
		"control":             contract["control"],
		"contract_source":     "external_script",
		"contract_script_url": contract["script_url"],
		"explicit_only":       true,
		"page_empty":          len(items) == 0,
	}
	details["linked_pages_not_fetched"] = true
	details["scripts_not_executed"] = true
	return details, nil
}

// fetchHTML reads and parses one anonymous page.
func (c *Client) fetchHTML(ctx context.Context, pageURL, failure string) (*xhtml.Node, error) {
	raw, err := c.do(ctx, requestSpec{
		Method:    http.MethodGet,
		URL:       pageURL,
		Headers:   map[string]string{"Accept": "text/html,application/xhtml+xml"},
		Anonymous: true,
	})
	if err != nil {
		return nil, err
	}
	doc, err := xhtml.Parse(strings.NewReader(string(raw)))
	if err != nil {
		return nil, &APIError{Message: failure}
	}
	return doc, nil
}

// scriptSource fetches one of the pages' own scripts, to read -- not to run.
func (c *Client) scriptSource(ctx context.Context, scriptURL string) (string, error) {
	if !anonymousScriptURLs[scriptURL] {
		return "", &APIError{Message: "that script is not one this build reads"}
	}
	raw, err := c.do(ctx, requestSpec{
		Method:    http.MethodGet,
		URL:       scriptURL,
		Headers:   map[string]string{"Accept": "application/javascript,text/javascript"},
		Anonymous: true,
	})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// anonymousScriptURLs is the exact set of scripts this build will fetch.
var anonymousScriptURLs = map[string]bool{
	opinionColumnsScriptURL:         true,
	opinionUpfrontScriptURL:         true,
	opinionAuthorDirectoryScriptURL: true,
}
