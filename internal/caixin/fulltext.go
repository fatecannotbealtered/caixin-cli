package caixin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/antchfx/htmlquery"
	"golang.org/x/net/html"
)

// Full text lives behind a signed request. See docs/FULL-TEXT.md for how the
// signature is obtained and why it is not reimplemented here.

const fullTextEndpoint = "https://gateway.caixin.com/api/newauth/checkAuthByIdJsonp"

// articleIDPattern pulls the numeric id out of a canonical article url.
var articleIDPattern = regexp.MustCompile(`/(\d{6,})\.html$`)

// resetContentPattern unwraps the `resetContentInfo({...})` call the endpoint
// returns instead of a bare object.
var resetContentPattern = regexp.MustCompile(`(?s)resetContentInfo\((.*)\)\s*;?\s*$`)

// contentPage is one page of the signed body response.
type contentPage struct {
	Attr    int    `json:"attr"`
	Content string `json:"content"`
	// TotalPage is absent on single-page articles.
	TotalPage int `json:"totalPage"`
}

// ArticleID extracts the numeric id a signature must be bound to.
func ArticleID(canonicalURL string) (string, bool) {
	match := articleIDPattern.FindStringSubmatch(canonicalURL)
	if match == nil {
		return "", false
	}
	return match[1], true
}

// FetchFullText retrieves the complete body using a signature the browser
// minted, then parses it into paragraphs.
//
// Pages are followed to the end, because the endpoint paginates long articles
// and stopping at page one would silently truncate exactly the articles most
// worth reading.
func (c *Client) FetchFullText(ctx context.Context, canonicalURL string, signer *Signer) ([]string, int, error) {
	articleID, ok := ArticleID(canonicalURL)
	if !ok {
		return nil, 0, invalid("could not read an article id out of that url")
	}

	signature, err := signer.Sign(ctx, canonicalURL, articleID, c.SessionCookies())
	if err != nil {
		return nil, 0, err
	}

	var paragraphs []string
	attr := 0
	for page := 1; page <= 50; page++ {
		body, pageAttr, totalPages, err := c.fetchContentPage(ctx, articleID, page, signature)
		if err != nil {
			return nil, attr, err
		}
		attr = pageAttr
		paragraphs = append(paragraphs, body...)
		if totalPages <= page {
			break
		}
	}
	return paragraphs, attr, nil
}

func (c *Client) fetchContentPage(ctx context.Context, articleID string, page int, signature *Signature) ([]string, int, int, error) {
	query := url.Values{
		"type": {"0"},
		"id":   {articleID},
		"page": {strconv.Itoa(page)},
		// The upstream expects a cache-buster; a fixed value keeps the request
		// reproducible for replay tests.
		"rand": {"0.1"},
	}
	raw, err := c.do(ctx, requestSpec{
		Method: http.MethodGet,
		URL:    fullTextEndpoint,
		Query:  query,
		Headers: map[string]string{
			"X-Sign":  signature.Sign,
			"X-Nonce": signature.Nonce,
			"Referer": "https://www.caixin.com/",
		},
	})
	if err != nil {
		return nil, 0, 0, err
	}

	var envelope struct {
		Code int    `json:"code"`
		Data string `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, 0, 0, &APIError{Message: "the full-text endpoint did not return JSON"}
	}

	match := resetContentPattern.FindStringSubmatch(envelope.Data)
	if match == nil {
		return nil, 0, 0, &APIError{Message: "the full-text payload was not in the expected wrapper"}
	}
	var content contentPage
	if err := json.Unmarshal([]byte(match[1]), &content); err != nil {
		return nil, 0, 0, &APIError{Message: "the full-text payload could not be decoded"}
	}

	paragraphs, err := paragraphsFromHTML(content.Content)
	if err != nil {
		return nil, content.Attr, content.TotalPage, err
	}
	return paragraphs, content.Attr, content.TotalPage, nil
}

// paragraphsFromHTML flattens a body fragment into paragraph text.
func paragraphsFromHTML(fragment string) ([]string, error) {
	doc, err := html.Parse(strings.NewReader(fragment))
	if err != nil {
		return nil, &APIError{Message: fmt.Sprintf("could not parse the body fragment: %v", err)}
	}
	var paragraphs []string
	for _, node := range htmlquery.Find(doc, "//p") {
		text := strings.TrimSpace(whitespaceRun.ReplaceAllString(htmlquery.InnerText(node), " "))
		if text != "" {
			paragraphs = append(paragraphs, text)
		}
	}
	return paragraphs, nil
}
