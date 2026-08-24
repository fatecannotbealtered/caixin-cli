package caixin

import (
	"net/url"
	"regexp"
	"strings"
)

// Route is the local verdict for a clicked url: which command consumes it, or
// an explicit "not supported".
//
// Classification is entirely local -- `route` never fetches the url. An agent
// hands it a link, gets back the argv to run, and runs exactly that. It must
// not string-concatenate the result into a shell.
type Route struct {
	InputURL     string `json:"input_url"`
	CanonicalURL string `json:"canonical_url"`
	Supported    bool   `json:"supported"`
	// Command is the argv to execute, or nil when unsupported.
	Command []string `json:"command"`
	Adapter string   `json:"adapter"`
	// DiscoveryRequired marks targets that may only be read after the parent
	// page has listed them, so an agent cannot deep-link past a directory.
	DiscoveryRequired bool `json:"discovery_required"`
	// ContentAccessNotImplied is always true: routing says which command reads a
	// url, never that the account is entitled to its content.
	ContentAccessNotImplied bool   `json:"content_access_not_implied"`
	Reason                  string `json:"reason,omitempty"`
	// Boundary names why an unsupported url is unsupported. "no adapter" and
	// "this is a PDF" call for different agent behaviour, so the distinction is
	// part of the contract rather than prose in the reason.
	Boundary string `json:"boundary,omitempty"`
}

var (
	articlePath    = regexp.MustCompile(`^/\d{4}-\d{2}-\d{2}/\d+\.html$`)
	blogAuthorHost = regexp.MustCompile(`^[a-z0-9-]+\.blog\.caixin\.com$`)
	// Topic detail is served from several shapes: /topic/<id>, /special/<id>,
	// the deepview front/static variants, and mappv5's numeric detail pages.
	// Topic detail is served under several path shapes. The /front/static
	// variants are canonicalized away before matching, so only the bare forms
	// are listed here.
	topicDetailPath = regexp.MustCompile(
		`^/(topic|special|event)/[A-Za-z0-9.]+(\.html)?$` +
			`|^/m_topic_detail/\d+(\.html)?$`)
	micrositePath     = regexp.MustCompile(`^/\d{4}/[A-Za-z0-9_-]+/?$`)
	frontlineCodeURL  = regexp.MustCompile(`^/detail/([0-9a-f]{32})`)
	frontStaticPrefix = regexp.MustCompile(`^/front/static/`)
	// mappTopicPath is the app's numeric topic detail page.
	mappTopicPath = regexp.MustCompile(`^/m_topic_detail/[1-9]\d{0,19}\.html$`)
)

// mappTopicHosts are the spellings the app publishes its topic pages under.
var mappTopicHosts = map[string]bool{
	"mappv5.caixin.com":  true,
	"mappsv5.caixin.com": true,
	"m.app.caixin.com":   true,
}

// ClassifyURL decides which command consumes a clicked Caixin url.
// datanewsInteractiveEntrypoints are destinations whose path shape falls
// outside the general topic rule but which the directory genuinely links to.
var datanewsInteractiveEntrypoints = map[string]bool{
	"https://datanews.caixin.com/interactive/2025/tabacco-tax/index_wall.html": true,
}

func ClassifyURL(raw string) Route {
	route := Route{InputURL: raw, ContentAccessNotImplied: true}

	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		route.Reason = "only absolute http(s) urls can be routed"
		return route
	}
	host := strings.ToLower(parsed.Hostname())
	if !caixinHost(host) {
		route.setBoundary("external", "the target is not on the Chinese Caixin site")
		return route
	}
	if port := parsed.Port(); port != "" && port != "80" && port != "443" {
		route.setBoundary("invalid_url", "the url carries a non-standard port")
		return route
	}

	// Query strings are tracking noise for routing purposes; the canonical form
	// is what the consuming command receives.
	canonical := *parsed
	canonical.RawQuery = ""
	canonical.Fragment = ""
	// deepview serves the same topic under a /front/static prefix; the bare
	// path is the canonical one, so both spellings route to one identity.
	canonical.Path = frontStaticPrefix.ReplaceAllString(canonical.Path, "/")
	route.CanonicalURL = canonical.String()
	path := canonical.Path

	switch {
	case host == "www.caixin.com" &&
		(path == "/search/newscroll" || path == "/search/newscroll/"):
		// The rolling feed is read by its own command, which takes no url --
		// there is exactly one such page, and its legacy view is a second one.
		switch {
		case parsed.RawQuery == "" && parsed.Fragment == "":
			route.CanonicalURL = NewscrollPageUrl
			route.set("newscroll", []string{"newscroll"})
		case parsed.RawQuery == "v=old" && parsed.Fragment == "":
			route.CanonicalURL = NewscrollPageUrl + "?v=old"
			route.set("latest", []string{"latest"})
		default:
			route.setBoundary("unsupported_caixin_url",
				"the rolling feed accepts only its default page or the exact v=old legacy view")
		}

	case opinionDirectoryIsEntrypoint(route.CanonicalURL):
		// The 观点 hub links three directories of its own plus its sections;
		// each has a different reader, so the url alone names the command.
		canonicalDirectory, adapter := opinionDirectoryURL(route.CanonicalURL)
		route.CanonicalURL = canonicalDirectory
		route.set(adapter, []string{adapter, canonicalDirectory})
		route.DiscoveryRequired = true

	case blogAuthorHost.MatchString(host) && blogArchivePath.MatchString(path):
		// A blog post lives at /archives/<n> rather than the dated article path
		// the rest of the site uses; it is still an article to a reader.
		route.set("article", []string{"article", route.CanonicalURL})

	case articlePath.MatchString(path):
		route.set("article", []string{"article", route.CanonicalURL})

	case mobileArticleAlias.MatchString(path):
		// The `/m/` spelling is the same article. It is canonicalized here, so
		// a caller that clicked the mobile link still gets one identity back.
		canonical.Path = mobileArticleAlias.FindStringSubmatch(path)[1]
		route.CanonicalURL = canonical.String()
		route.set("article", []string{"article", route.CanonicalURL})

	case issueIsPage(route.CanonicalURL):
		route.set("issue", []string{"issue", route.CanonicalURL})

	case route.CanonicalURL == BloggersDirectoryUrl:
		// The roster is one fixed page, so its command takes no url.
		route.set("bloggers-directory", []string{"bloggers-directory"})

	case blogAuthorHost.MatchString(host) && (path == "" || path == "/"):
		route.set("blog-author", []string{"blog-author", route.CanonicalURL})

	case host == "topics.caixin.com" && TopicCategories[route.CanonicalURL].Name != "":
		route.set("topics", []string{"topics", route.CanonicalURL})

	case mappTopicPath.MatchString(path) && mappTopicHosts[host]:
		// The app serves one topic under three host spellings. They are folded
		// to the canonical one so a caller does not fetch the same topic twice
		// under two names.
		route.CanonicalURL = "https://mappv5.caixin.com" + path
		route.set("topic", []string{"topic", route.CanonicalURL})

	case topicDetailPath.MatchString(path):
		route.set("topic", []string{"topic", route.CanonicalURL})

	case datanewsInteractiveEntrypoints[route.CanonicalURL] || datanewsTopicInteractiveURL(raw, raw) != "":
		// A visualisation is a standalone page, not an article; it has to be
		// classified before the section-directory fallback claims it.
		//
		// Evaluated against the original url, not the stripped canonical one:
		// one allowlisted destination carries a meaningful `#/` fragment, and
		// the generic normalization above would have removed it.
		if !datanewsInteractiveEntrypoints[route.CanonicalURL] {
			route.CanonicalURL = datanewsTopicInteractiveURL(raw, raw)
		}
		route.set("datanews-interactive",
			[]string{"datanews-interactive", route.CanonicalURL})
		route.DiscoveryRequired = true

	case MicrositePaths[route.CanonicalURL] ||
		(micrositeHosts[host] && micrositePath.MatchString(path)):
		// Two ways in: the exact allowlist, and the dated `/<year>/<name>/`
		// shape on the hosts that publish them. The list alone misses live
		// microsites; the shape alone swallows campaign pages that belong to
		// other adapters.
		// A dated topics path is a standalone microsite, not a directory entry.
		route.set("microsite", []string{"microsite", route.CanonicalURL})

	case host == "k.caixin.com" && frontlineCodeURL.MatchString(path):
		code := frontlineCodeURL.FindStringSubmatch(path)[1]
		route.set("frontline-detail", []string{"frontline-detail", code})

	case videoSectionURL(route.CanonicalURL) != "":
		// The video channel's own directories. They are read after the channel
		// front lists them, which is what discovery_required says.
		route.CanonicalURL = videoSectionURL(route.CanonicalURL)
		route.set("video-section", []string{"video-section", route.CanonicalURL})
		route.DiscoveryRequired = true

	case host == "culture.caixin.com" && cultureSectionIsMeasured(route.CanonicalURL):
		// A 文化 section is a standing url the channel always publishes, so it
		// is readable without being discovered first.
		route.set("culture-section", []string{"culture-section", route.CanonicalURL})

	case host == "culture.caixin.com" && cultureAuthorIsPage(route.CanonicalURL):
		// A columnist page and a section share the path shape, so the measured
		// section list is checked first and this is what is left.
		route.set("culture-author",
			[]string{"culture-author", route.CanonicalURL})

	case snapshotIsEntrypoint(route.CanonicalURL):
		route.set("snapshot", []string{"snapshot", route.CanonicalURL})

	case esg30SubdirectoryPath.MatchString(canonical.Path) && host == "index.caixin.com":
		route.set("esg30-subdirectory",
			[]string{"esg30-subdirectory", route.CanonicalURL})
		route.DiscoveryRequired = true

	case photoWeekStaticPage(route.CanonicalURL) != "":
		// The photo column's static pagination is consumed by the same command
		// as its root, so a caller can follow a page link directly.
		route.CanonicalURL = photoWeekStaticPage(route.CanonicalURL)
		route.set("public-directory",
			[]string{"public-directory", route.CanonicalURL})
		route.DiscoveryRequired = true

	case publicDirectoryURL(route.CanonicalURL) != "":
		canonical, kind := publicDirectoryURL(route.CanonicalURL), publicDirectoryKind(route.CanonicalURL)
		route.CanonicalURL = canonical
		route.set("public-directory",
			[]string{"public-directory", canonical})
		route.DiscoveryRequired = kind == "anticorruption" || kind == "anticorruption_list"

	case host == "opinion.caixin.com" && opinionAuthorPath.MatchString(canonical.Path) &&
		!sectionDirectorySpecial(route.CanonicalURL):
		// The 观点 sections share the author-page path shape, so they are
		// excluded explicitly rather than by pattern.
		route.CanonicalURL = "https://opinion.caixin.com/" +
			opinionAuthorPath.FindStringSubmatch(canonical.Path)[1] + "/"
		route.set("opinion-author",
			[]string{"opinion-author", route.CanonicalURL})
		route.DiscoveryRequired = true

	case esg30ResourceURL(route.CanonicalURL) != "":
		// Sponsored campaign pages live one level under the promote host. They
		// are read by their own adapter, and only after the directory that
		// listed them -- deep-linking past it is what discovery_required stops.
		route.set("esg30-resource",
			[]string{"esg30-resource", route.CanonicalURL})
		route.DiscoveryRequired = true

	default:
		if boundary, reason := nonContentBoundary(host, canonical.Path); boundary != "" {
			route.setBoundary(boundary, reason)
			break
		}
		// section-directory is a real adapter, not a catch-all. It reads
		// single-level channel sections on the news hosts; routing anything
		// else to it would hand an agent a command that can only fail.
		section := sectionDirectoryCandidate(route.CanonicalURL)
		if section == "" {
			route.setBoundary("unsupported_caixin_url",
				"no verified adapter reads this Caixin url")
			break
		}
		// A section-shaped url can still be a dead end. Saying which kind of
		// dead end is what stops a caller from retrying an adapter that will
		// never read it.
		if terminal, ok := sectionDirectoryTerminal[section]; ok {
			route.setBoundary(terminal[0], terminal[1])
			break
		}
		if reason, ok := sectionDirectoryUnsupported[section]; ok {
			route.setBoundary("unsupported_template", reason)
			break
		}
		route.set("section-directory", []string{"section-directory", route.CanonicalURL})
		route.DiscoveryRequired = true
		route.Reason = "read the parent homepage first; this entry must be discovered before it is fetched"
	}
	return route
}

// set marks a route supported. The adapter is the command that reads it: a
// caller runs `command` verbatim, so naming the adapter anything else would
// leave two spellings for one thing.
func (r *Route) set(adapter string, command []string) {
	r.Supported = true
	r.Adapter = adapter
	r.Command = command
}

// AsMap renders the route as the standalone `route` command's payload. `ok`
// belongs to the envelope here, so it is not repeated inside data.
func (r Route) AsMap() map[string]any {
	return r.asMap(false)
}

// AsEmbeddedMap renders the route as it appears nested inside another result --
// a topic card's `consumer`, for instance. There it is a self-contained verdict
// rather than the whole response, so it carries its own `ok`.
func (r Route) AsEmbeddedMap() map[string]any {
	return r.asMap(true)
}

func (r Route) asMap(embedded bool) map[string]any {
	// A boundary verdict answers "there is nothing to read here", so the
	// consuming fields would all be empty and saying "content access is not
	// implied" about a PDF is noise. Only the verdict is emitted.
	if r.Boundary != "" {
		fields := map[string]any{
			"input_url": r.InputURL,
			"supported": false,
			"boundary":  r.Boundary,
		}
		if r.Reason != "" {
			fields["reason"] = r.Reason
		}
		if embedded {
			fields["ok"] = true
		}
		return fields
	}
	fields := map[string]any{
		"input_url":                  r.InputURL,
		"canonical_url":              r.CanonicalURL,
		"supported":                  r.Supported,
		"command":                    r.Command,
		"adapter":                    r.Adapter,
		"discovery_required":         r.DiscoveryRequired,
		"content_access_not_implied": r.ContentAccessNotImplied,
	}
	// Omitted when empty: an absent reason and an empty-string reason are
	// different claims, so an absent one emits neither key nor value.
	if r.Reason != "" {
		fields["reason"] = r.Reason
	}
	if embedded {
		fields["ok"] = true
	}
	return fields
}

// publicDirectoryURL canonicalizes a public-directory link, folding the
// alternate spellings the site links to, and returns "" for anything outside
// the measured allowlist.
func publicDirectoryURL(raw string) string {
	if target, ok := PublicDirectoryAliases[raw]; ok {
		raw = target
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return ""
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return ""
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	parsed.Host = strings.ToLower(parsed.Hostname())
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	canonical := parsed.String()
	if _, ok := PublicDirectoryEntrypoints[canonical]; !ok {
		return ""
	}
	return canonical
}

func publicDirectoryKind(raw string) string {
	return PublicDirectoryEntrypoints[publicDirectoryURL(raw)]
}

// photoWeekStaticPage canonicalizes one static page of the 一周天下 column.
//
// `index.html` is the root under another name; `index-N.html` is page N.
func photoWeekStaticPage(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil {
		return ""
	}
	if parsed.Scheme != "https" || strings.ToLower(parsed.Hostname()) != "photos.caixin.com" {
		return ""
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	if parsed.Path == "/photoreport/index.html" {
		return "https://photos.caixin.com/photoreport/"
	}
	if !photoWeekPagePattern.MatchString(parsed.Path) {
		return ""
	}
	return "https://photos.caixin.com" + parsed.Path
}

// setBoundary records why a url has no adapter.
func (r *Route) setBoundary(boundary, reason string) {
	r.Supported = false
	r.Boundary = boundary
	r.Reason = reason
}

// downloadExtensions are documents this tool reports but never fetches.
var downloadExtensions = []string{".pdf", ".doc", ".docx"}

// imageExtension matches a static bitmap or vector asset.
var imageExtension = regexp.MustCompile(`(?i)\.(gif|jpe?g|png|webp|svg)$`)

// mediaHosts serve assets rather than pages.
var mediaHosts = map[string]bool{
	"img.caixin.com": true, "file.caixin.com": true, "audio.caixin.com": true,
}

// productHosts are separate Caixin products with their own licensing.
var productHosts = map[string]bool{
	"cxdata.caixin.com": true, "entities.caixin.com": true, "stock.caixin.com": true,
	"bond.caixin.com": true, "ceic.caixin.com": true,
}

// nonContentBoundary classifies a Caixin url that is real but is not an article
// or a directory.
//
// The distinction matters to a caller: "there is no adapter for this" invites a
// bug report, while "this is a PDF" or "this is the app download page" is a
// final answer. Returning one generic unsupported for all of them would throw
// that away.
func nonContentBoundary(host, path string) (string, string) {
	lower := strings.ToLower(path)
	switch {
	case (host == "mobile.caixin.com" || host == "m.mobile.caixin.com") &&
		(path == "/home/" || path == "/m/home/"):
		return "mobile_app", "this is the Caixin app download page, not a news directory"
	case hasAnySuffix(lower, downloadExtensions):
		return "download_asset",
			"documents are reported as download candidates; their contents are not fetched"
	case imageExtension.MatchString(lower):
		return "media_asset", "a static image is not a readable page"
	case mediaHosts[host]:
		return "media_asset", "media assets are not readable pages"
	case host == "mall.caixin.com" || host == "course.caixin.com":
		return "transaction_or_product_detail",
			"product, course, and checkout pages are not read by the news adapters"
	case productHosts[host]:
		return "independent_product",
			"this entry belongs to a separate Caixin data product with its own licensing"
	}
	return "", ""
}

func hasAnySuffix(value string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(value, suffix) {
			return true
		}
	}
	return false
}

// snapshotIsEntrypoint reports whether a bare host is one this build measured.
//
// Matching any bare host would route every unknown Caixin subdomain to
// `snapshot`, which then refuses it -- an adapter that always fails is worse
// than an honest boundary.
func snapshotIsEntrypoint(canonical string) bool {
	_, ok := SnapshotEntrypoints[canonical]
	return ok
}

// micrositeHosts are the hosts that actually publish dated microsites. Without
// the restriction, a conference or campaign path on any subdomain matched the
// shape and was routed to an adapter that cannot read it.
var micrositeHosts = map[string]bool{
	"topics.caixin.com":        true,
	"opinion.caixin.com":       true,
	"economy.caixin.com":       true,
	"international.caixin.com": true,
	"promote.caixin.com":       true,
}

// sectionDirectoryHosts are the news hosts that publish channel sections.
var sectionDirectoryHosts = map[string]bool{
	"economy.caixin.com": true, "finance.caixin.com": true,
	"companies.caixin.com": true, "china.caixin.com": true,
	"international.caixin.com": true, "science.caixin.com": true,
}

// sectionPath is the single-level channel shape, e.g. `/finance/`.
var sectionPath = regexp.MustCompile(`^/[a-z][a-z0-9_]{0,63}/$`)

// esg30ResourcePath is the shape of a campaign page under the promote host.
var esg30ResourcePath = regexp.MustCompile(`^/[A-Za-z0-9._~%+@,-]+(/[A-Za-z0-9._~%+@,-]+)*/?$`)

// esg30ResourceAsset matches the file extensions that are assets, not pages.
var esg30ResourceAsset = regexp.MustCompile(`(?i)\.(gif|jpe?g|png|webp|svg|pdf)$`)

// esg30ResourceURL accepts a sponsored campaign page.
func esg30ResourceURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.Scheme != "https" {
		return ""
	}
	if strings.ToLower(parsed.Hostname()) != "promote.caixin.com" {
		return ""
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	if esg30ResourceAsset.MatchString(parsed.Path) {
		return ""
	}
	if articlePathPattern.MatchString(parsed.Path) {
		return ""
	}
	segments := strings.Split(parsed.Path, "/")
	for _, segment := range segments[1 : len(segments)-1] {
		if segment == "" || segment == "." || segment == ".." {
			return ""
		}
	}
	if !esg30ResourcePath.MatchString(parsed.Path) {
		return ""
	}
	return parsed.String()
}

// opinionAuthorPath is the single-level 观点 author page shape.
var opinionAuthorPath = regexp.MustCompile(`^/([A-Za-z0-9_-]{1,64})(?:/|/index\.html)$`)

// opinionRootURL is the 观点 channel front, the page that lists its sections.
const opinionRootURL = "https://opinion.caixin.com/"

// sectionDirectorySpecialParents are sections that use the same template but do
// not live on a news host, mapped to the pages allowed to list them.
//
// The parent list is what makes a section discoverable: a section is only
// offered to a caller when the page in hand actually links it, so a url the
// build knows about but the page never published is not silently promoted.
var sectionDirectorySpecialParents = map[string][]string{
	"https://opinion.caixin.com/editorial/":      {opinionRootURL},
	"https://opinion.caixin.com/opinion_leader/": {opinionRootURL},
	"https://opinion.caixin.com/opinion_video/":  {opinionRootURL},
	"https://opinion.caixin.com/sxjx/":           {opinionRootURL},
	"https://opinion.caixin.com/think_tank/":     {opinionRootURL},
	"https://opinion.caixin.com/wyll/":           {opinionRootURL},
	"https://www.caixin.com/announcement/":       {"https://www.caixin.com/"},
	"https://weekly.caixin.com/cw_correction/": {
		"https://www.caixin.com/", "https://weekly.caixin.com/",
	},
}

// sectionDirectoryUnsupported are urls that pass the section shape but do not
// use the section template.
var sectionDirectoryUnsupported = map[string]string{
	"https://economy.caixin.com/data/": "the economic data page is not a standard comMain section template",
}

// sectionDirectoryTerminal are section-shaped urls whose real answer is a
// boundary rather than an adapter, so a caller stops instead of retrying.
var sectionDirectoryTerminal = map[string][2]string{
	"https://economy.caixin.com/data/": {"independent_product",
		"the economic data entry is a JavaScript data product shell with no anonymously readable directory"},
}

// sectionDirectorySpecial reports whether a url is one of the off-host sections.
func sectionDirectorySpecial(canonical string) bool {
	_, ok := sectionDirectorySpecialParents[canonical]
	return ok
}

// sectionDirectoryCandidate canonicalizes a url that has the section shape,
// without judging whether the template is one this build reads.
func sectionDirectoryCandidate(raw string) string {
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
	if parsed.RawQuery != "" || parsed.Fragment != "" ||
		strings.ContainsAny(raw, "?#") {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if port := parsed.Port(); port != "" &&
		(parsed.Scheme != "http" || port != "80") && (parsed.Scheme != "https" || port != "443") {
		return ""
	}
	parsed.Scheme = "https"
	parsed.Host = host
	canonical := parsed.String()
	if sectionDirectoryHosts[host] {
		if !sectionPath.MatchString(parsed.Path) {
			return ""
		}
		return canonical
	}
	if sectionDirectorySpecial(canonical) {
		return canonical
	}
	return ""
}

// sectionDirectoryParents lists the pages allowed to publish a section link.
func sectionDirectoryParents(canonical string) []string {
	parsed, err := url.Parse(canonical)
	if err != nil {
		return nil
	}
	if host := strings.ToLower(parsed.Hostname()); sectionDirectoryHosts[host] {
		return []string{"https://" + host + "/"}
	}
	return sectionDirectorySpecialParents[canonical]
}

// cultureSectionIsMeasured reports whether a 文化 url is one of the sections.
func cultureSectionIsMeasured(raw string) bool {
	_, _, _, ok := cultureSectionURL(raw)
	return ok
}

// cultureAuthorIsPage reports whether a 文化 url is a columnist page.
func cultureAuthorIsPage(raw string) bool {
	canonical, _ := cultureAuthorURL(raw)
	return canonical != ""
}

// keyTopicPath and tagSpecialPath are the two spellings of a Key topic.
var keyTopicPath = regexp.MustCompile(`^/topic/(BQ\d{2}\.\d{8,9})$`)
var tagSpecialPath = regexp.MustCompile(`^/special/(BQ\d{2}\.\d{8,9})$`)

// keyTopicQuery is the tracking value a topic link is allowed to carry.
var keyTopicQuery = regexp.MustCompile(`^[A-Za-z0-9._-]{0,128}$`)

// keyTopicURL accepts a Key topic page under either of its two hosts.
//
// The tag host serves the same topic under a different path; both are folded to
// the key.caixin.com form so one topic has one identity.
func keyTopicURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" {
		return ""
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if !caixinHost(host) {
		return ""
	}
	if host == "tag.caixin.com" {
		match := tagSpecialPath.FindStringSubmatch(parsed.Path)
		if match == nil || parsed.RawQuery != "" {
			return ""
		}
		return "https://key.caixin.com/topic/" + match[1]
	}
	match := keyTopicPath.FindStringSubmatch(parsed.Path)
	if host != "key.caixin.com" || match == nil {
		return ""
	}
	code := match[1]
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return ""
	}
	for key, values := range query {
		if len(values) != 1 {
			return ""
		}
		value := values[0]
		switch {
		case key == "cxapp_topicCode" && value == code:
		case key == "cxapp_link" && value == "true":
		case (key == "channelSource" || key == "originReferrer") && keyTopicQuery.MatchString(value):
		default:
			return ""
		}
	}
	return "https://key.caixin.com/topic/" + code
}

// deepviewPath is the deepview topic and event shape, with or without the
// /front/static prefix the app links.
var deepviewPath = regexp.MustCompile(`^/(?:front/static/)?(topic|event)/([A-Z0-9]+(?:\.[A-Z0-9]+)+)\.html$`)

// deepviewURL accepts a deepview topic or event page.
func deepviewURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" {
		return ""
	}
	if strings.ToLower(parsed.Hostname()) != "deepview.caixin.com" {
		return ""
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return ""
	}
	match := deepviewPath.FindStringSubmatch(parsed.Path)
	if match == nil {
		return ""
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return ""
	}
	for key, values := range query {
		switch key {
		case "cxapp_link", "channelSource", "originReferrer":
			for _, value := range values {
				if len(value) > 128 {
					return ""
				}
			}
		default:
			return ""
		}
	}
	return "https://deepview.caixin.com/" + match[1] + "/" + match[2] + ".html"
}
