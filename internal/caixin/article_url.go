package caixin

import (
	"net/url"
	"regexp"
	"strings"
)

// One url shape, checked once. The listing helpers each accept a narrower slice
// of this (a 文化 card, a magazine feature), but the commands that take a url
// from a caller check it here, so "is this an article" has one answer.

// articleHosts are the hosts that publish articles.
var articleHosts = map[string]bool{
	"www.caixin.com": true, "economy.caixin.com": true, "finance.caixin.com": true,
	"companies.caixin.com": true, "china.caixin.com": true, "international.caixin.com": true,
	"opinion.caixin.com": true, "science.caixin.com": true, "special.caixin.com": true,
	"culture.caixin.com": true, "enjoy.caixin.com": true, "weekly.caixin.com": true,
	"cnreform.caixin.com": true, "bijiao.caixin.com": true, "magazine.caixin.com": true,
	"mini.caixin.com": true, "photos.caixin.com": true, "video.caixin.com": true,
	"promote.caixin.com": true, "datanews.caixin.com": true, "database.caixin.com": true,
	"wenews.caixin.com": true, "conferences.caixin.com": true, "en.caixin.com": true,
	"other.caixin.com": true, "topics.caixin.com": true, "index.caixin.com": true,
	"pmi.caixin.com": true,
}

// articleTrackingKeys are the query parameters an article link may carry. They
// are dropped from the canonical form: they say where the reader came from,
// not which article this is.
var articleTrackingKeys = map[string]bool{
	"channel": true, "channelSource": true, "cxapp_link": true, "originReferrer": true,
}

var (
	standardArticlePath = regexp.MustCompile(`^/20\d{2}-\d{2}-\d{2}/\d{1,20}\.html$`)
	pagedArticlePath    = regexp.MustCompile(`^/20\d{2}-\d{2}-\d{2}/\d{1,20}_[1-9]\d{0,3}\.html$`)
	mobileArticlePath   = regexp.MustCompile(`^/m/20\d{2}-\d{2}-\d{2}/\d{1,20}\.html$`)
	blogArticlePath     = regexp.MustCompile(`^/archives/\d{1,20}/?$`)
	articleTrackingText = regexp.MustCompile(`^[A-Za-z0-9._~-]{0,256}$`)
)

// articleExactAliases are the handful of legacy spellings the site still links.
// They are listed rather than pattern-matched because their shapes overlap with
// urls that are not articles.
var articleExactAliases = buildArticleAliases()

func buildArticleAliases() map[string]string {
	aliases := map[string]string{}
	for _, base := range []string{
		"https://magazine.caixin.com/2012-01-13/100348431.html",
		"https://magazine.caixin.com/2012-07-06/100408160.html",
		"https://magazine.caixin.com/2013-11-22/100608215.html",
	} {
		for page := 1; page <= 6; page++ {
			aliases[base+"?p"+itoa(page)] = base
		}
	}
	const finance = "https://finance.caixin.com/2012-10-26/100450842.html"
	for fragment := 2; fragment <= 10; fragment++ {
		aliases[finance+"#"+pad3(fragment)] = finance
	}
	return aliases
}

// pad3 renders a fragment index the way the page writes it.
func pad3(value int) string {
	text := itoa(value)
	for len(text) < 3 {
		text = "0" + text
	}
	return text
}

// validateArticleURL canonicalizes an article url, or returns "" if the url is
// not one this build reads.
//
// The `#gocomment` anchor is accepted: the site links its own articles that way
// and it names the same piece.
func validateArticleURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, " \t\n\r") {
		return ""
	}
	if alias, ok := articleExactAliases[raw]; ok {
		raw = alias
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil {
		return ""
	}
	fragmentQuery := url.Values{}
	switch {
	case parsed.Fragment == "gocomment":
		parsed.Fragment = ""
	case strings.HasPrefix(parsed.Fragment, "gocomment?"):
		rest := strings.TrimPrefix(parsed.Fragment, "gocomment?")
		fragmentQuery, err = url.ParseQuery(rest)
		if err != nil || rest == "" || len(fragmentQuery) == 0 {
			return ""
		}
		parsed.Fragment = ""
	}
	host := strings.ToLower(parsed.Hostname())
	isBlog := blogAuthorHost.MatchString(host)
	if parsed.Scheme == "http" {
		if port := parsed.Port(); port != "" && port != "80" {
			return ""
		}
		if !articleHosts[host] && !isBlog {
			return ""
		}
		parsed.Scheme = "https"
	}
	if parsed.Scheme != "https" {
		return ""
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return ""
	}
	if host != "caixin.com" && !strings.HasSuffix(host, ".caixin.com") {
		return ""
	}
	if parsed.Fragment != "" {
		return ""
	}

	standard := articleHosts[host] && (standardArticlePath.MatchString(parsed.Path) ||
		(host == "other.caixin.com" && pagedArticlePath.MatchString(parsed.Path)))
	mobile := articleHosts[host] && mobileArticlePath.MatchString(parsed.Path)
	blog := isBlog && blogArticlePath.MatchString(parsed.Path)
	if !standard && !mobile && !blog {
		return ""
	}

	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return ""
	}
	for key, values := range fragmentQuery {
		query[key] = append(query[key], values...)
	}
	for key, values := range query {
		if len(values) != 1 || !articleTrackingKeys[key] ||
			!articleTrackingText.MatchString(values[0]) {
			return ""
		}
	}

	path := parsed.Path
	if mobile {
		// The `/m/` spelling is the same article.
		path = path[2:]
	}
	return "https://" + host + path
}
