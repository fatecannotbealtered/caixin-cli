package caixin

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/antchfx/htmlquery"
	xhtml "golang.org/x/net/html"
)

// An issue page is the magazine's table of contents: the cover package plus the
// three printed columns. It lists what is in the issue and nothing more --
// `directory_only` is always true, and no article body is fetched.

// issuePathPattern matches a magazine issue path, e.g. `/2026/cw1217/`.
var issuePathPattern = regexp.MustCompile(`^/20\d{2}/(cw|cr|cs_)(\d+)(/index\.html|/)?$`)

// issuePaths is the path shape each publication uses. The prefix is checked
// against the host's own publication: the three magazines share a template but
// not a numbering, so `/2021/cr948/` on the weekly host is not an issue.
var issuePaths = map[string]*regexp.Regexp{
	"weekly":   regexp.MustCompile(`^/20\d{2}/cw\d+(?:/index\.html|/)?$`),
	"cnreform": regexp.MustCompile(`^/20\d{2}/cr\d+(?:/index\.html|/)?$`),
	"bijiao":   regexp.MustCompile(`^/20\d{2}/cs_\d+(?:/index\.html|/)?$`),
}

// issuePublications maps a magazine host to its publication name.
var issuePublications = map[string]string{
	"weekly.caixin.com":   "weekly",
	"cnreform.caixin.com": "cnreform",
	"bijiao.caixin.com":   "bijiao",
}

// issueURL canonicalizes a magazine issue url and names its publication.
func issueURL(raw string) (canonical, publication string) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil {
		return "", ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", ""
	}
	host := strings.ToLower(parsed.Hostname())
	publication = issuePublications[host]
	if host == "magazine.caixin.com" {
		// The shared magazine host serves all three; the path says which.
		for name, pattern := range issuePaths {
			if pattern.MatchString(parsed.Path) {
				publication = name
				break
			}
		}
	}
	if publication == "" {
		return "", ""
	}
	if !issuePaths[publication].MatchString(parsed.Path) {
		return "", ""
	}
	parsed.Scheme = "https"
	parsed.Host = host
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), publication
}

// issueNumber pulls the printed issue number out of the path.
func issueNumber(raw string) any {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	match := issuePathPattern.FindStringSubmatch(parsed.Path)
	if match == nil {
		return nil
	}
	number, err := strconv.Atoi(match[2])
	if err != nil {
		return nil
	}
	return number
}

// Issue reads one magazine issue's table of contents.
func (c *Client) Issue(ctx context.Context, pageURL string) (map[string]any, error) {
	canonical, publication := issueURL(pageURL)
	if canonical == "" {
		return nil, invalid("issue reads a Caixin magazine issue page, " +
			"for example https://weekly.caixin.com/2026/cw1217/")
	}

	raw, err := c.do(ctx, requestSpec{
		Method:  http.MethodGet,
		URL:     canonical,
		Headers: map[string]string{"Accept": "text/html,application/xhtml+xml"},
	})
	if err != nil {
		return nil, err
	}
	source := string(raw)
	doc, err := xhtml.Parse(strings.NewReader(source))
	if err != nil {
		return nil, &APIError{Message: "could not parse the issue page"}
	}

	result, err := issueDocument(doc, source, canonical, publication)
	if err != nil {
		return nil, err
	}
	result["url"] = canonical
	return result, nil
}

var totalIssuePattern = regexp.MustCompile(`总第\s*(\d+)\s*期`)
var magazineTotalPattern = regexp.MustCompile(`\bmagazineTotalNum\s*=\s*(\d+)`)
var magazineIDPattern = regexp.MustCompile(`\bthisMagazineId\s*=\s*(\d+)`)
var issueSourcePattern = regexp.MustCompile(`(\d{4})年第0*(\d+)期\s+出版日期[:：]\s*(\d{4}-\d{2}-\d{2})`)

// issueColumns maps the printed column classes to their position.
var issueColumns = map[string]string{
	"magContentlf2": "left",
	"magContentce":  "center",
	"magContentri2": "right",
}

// issueDocument extracts the contents listing.
func issueDocument(doc *xhtml.Node, source, pageURL, publication string) (map[string]any, error) {
	main := firstElement(doc, classXPath("mainMagContent"))
	if main == nil {
		return nil, &APIError{Message: "the issue page is missing its mainMagContent"}
	}
	host := mustHost(pageURL)

	issueTitle := firstXPathText(main, ".//*[contains(@class,'report')]//*[contains(@class,'title')]")
	sourceLabel := firstXPathText(main, ".//*[contains(@class,'report')]//*[contains(@class,'source')]")

	var totalIssue any
	if match := totalIssuePattern.FindStringSubmatch(issueTitle); match != nil {
		if value, err := strconv.Atoi(match[1]); err == nil {
			totalIssue = value
		}
	}
	if totalIssue == nil {
		if match := magazineTotalPattern.FindStringSubmatch(source); match != nil {
			if value, err := strconv.Atoi(match[1]); err == nil {
				totalIssue = value
			}
		}
	}
	var annualIssue, publishedAt any
	if match := issueSourcePattern.FindStringSubmatch(sourceLabel); match != nil {
		if value, err := strconv.Atoi(match[2]); err == nil {
			annualIssue = value
		}
		publishedAt = match[3]
	}
	var magazineID any
	if match := magazineIDPattern.FindStringSubmatch(source); match != nil {
		if value, err := strconv.Atoi(match[1]); err == nil {
			magazineID = value
		}
	}

	cover := ""
	if node := firstElement(main, "./div["+classPredicate("cover")+"]/img"); node != nil {
		cover = caixinOutputURL(pageURL, attr(node, "src"))
		if parsed, err := url.Parse(cover); err != nil || parsed.Hostname() != "img.caixin.com" {
			cover = ""
		}
	}

	sections := []any{}

	// The cover package comes first and is labelled separately from the columns.
	if report := firstElement(main,
		".//*[contains(@class,'report')]//*[contains(@class,'magazine-container')]"); report != nil {
		heading := firstElement(report, "./*[contains(@class,'reportTit')]")
		var articles []any
		for _, anchor := range htmlquery.Find(report, ".//dl/dt/a[@href] | .//p/a[@href]") {
			if article := issueArticle(anchor, pageURL, host); article != nil {
				articles = append(articles, article)
			}
		}
		if len(articles) > 0 {
			sections = append(sections, map[string]any{
				"kind":     "cover",
				"column":   "cover",
				"name":     emptyToNil(directText(heading)),
				"label":    emptyToNil(nodeText(heading)),
				"articles": articles,
			})
		}
	}

	for _, column := range htmlquery.Find(main, ".//*["+classPredicate("magIntro2")+"]/div") {
		position := ""
		for _, class := range strings.Fields(attr(column, "class")) {
			if name, ok := issueColumns[class]; ok {
				position = name
				break
			}
		}
		if position == "" {
			continue
		}
		var current map[string]any
		for child := column.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != xhtml.ElementNode {
				continue
			}
			classes := map[string]bool{}
			for _, class := range strings.Fields(attr(child, "class")) {
				classes[class] = true
			}
			if classes["magIntrotit"] {
				name := firstXPathText(child, "./span")
				if name == "" {
					name = directText(child)
				}
				current = map[string]any{
					"kind":     "section",
					"column":   position,
					"name":     emptyToNil(name),
					"label":    emptyToNil(nodeText(child)),
					"articles": []any{},
				}
				sections = append(sections, current)
				continue
			}
			if !strings.EqualFold(child.Data, "dl") || current == nil {
				continue
			}
			for _, anchor := range htmlquery.Find(child, "./dt//a[@href]") {
				if article := issueArticle(anchor, pageURL, host); article != nil {
					current["articles"] = append(current["articles"].([]any), article)
				}
			}
		}
	}

	// A heading with nothing under it is layout, not content.
	kept := []any{}
	total := 0
	for _, raw := range sections {
		section, _ := raw.(map[string]any)
		articles, _ := section["articles"].([]any)
		if len(articles) == 0 {
			continue
		}
		total += len(articles)
		kept = append(kept, section)
	}

	return map[string]any{
		"publication":    publication,
		"issue_number":   issueNumber(pageURL),
		"title":          emptyToNil(firstXPathText(doc, "//title")),
		"issue_title":    emptyToNil(issueTitle),
		"source_label":   emptyToNil(sourceLabel),
		"total_issue":    totalIssue,
		"annual_issue":   annualIssue,
		"published_at":   publishedAt,
		"magazine_id":    magazineID,
		"cover_image":    emptyToNil(cover),
		"sections":       kept,
		"articles_count": total,
		"directory_only": true,
	}, nil
}

// directText reads an element's own text, ignoring nested elements.
func directText(node *xhtml.Node) string {
	if node == nil {
		return ""
	}
	var parts []string
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == xhtml.TextNode {
			parts = append(parts, child.Data)
		}
	}
	return strings.TrimSpace(whitespaceRunEditorial.ReplaceAllString(strings.Join(parts, ""), " "))
}

// issueArticle builds one contents entry.
func issueArticle(anchor *xhtml.Node, pageURL, expectedHost string) map[string]any {
	link := articleURL(pageURL, attr(anchor, "href"))
	if link == "" {
		return nil
	}
	parsed, err := url.Parse(link)
	// An issue lists its own magazine's pieces; a link that escapes the host is
	// promotion, not part of the issue.
	if err != nil || parsed.Hostname() != expectedHost {
		return nil
	}
	title := nodeText(anchor)
	if title == "" {
		return nil
	}
	var articleID any
	if value := attr(anchor, "article-data-id"); value != "" {
		if _, err := strconv.Atoi(value); err == nil {
			articleID = value
		}
	}

	var detailNodes []*xhtml.Node
	if heading := firstElement(anchor, "ancestor::dt[1]"); heading != nil {
		for sibling := heading.NextSibling; sibling != nil; sibling = sibling.NextSibling {
			if sibling.Type != xhtml.ElementNode {
				continue
			}
			if strings.EqualFold(sibling.Data, "dt") {
				break
			}
			if strings.EqualFold(sibling.Data, "dd") {
				detailNodes = append(detailNodes, sibling)
			}
		}
	} else if container := firstElement(anchor, "ancestor::dl[1]"); container != nil {
		detailNodes = htmlquery.Find(container, ".//dd")
	}

	details := []any{}
	byline := ""
	for _, node := range detailNodes {
		value := nodeText(node)
		if value == "" {
			continue
		}
		duplicate := false
		for _, existing := range details {
			if existing == value {
				duplicate = true
			}
		}
		if !duplicate {
			details = append(details, value)
		}
		if byline == "" {
			for _, class := range strings.Fields(attr(node, "class")) {
				if class == "date" {
					byline = value
				}
			}
		}
	}
	summary := ""
	for _, value := range details {
		if text, _ := value.(string); text != byline {
			summary = text
			break
		}
	}

	return map[string]any{
		"article_id": articleID,
		"title":      title,
		"url":        link,
		"byline":     emptyToNil(byline),
		"summary":    emptyToNil(summary),
		"details":    details,
		"access":     "directory_visible",
	}
}

// issueIsPage reports whether a url is a magazine issue page.
func issueIsPage(raw string) bool {
	canonical, _ := issueURL(raw)
	return canonical != ""
}
