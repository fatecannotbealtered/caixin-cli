package caixin

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/andybalholm/cascadia"
	"github.com/antchfx/htmlquery"
	xhtml "golang.org/x/net/html"
)

// What a reader can see is decided by CSS, and this client does not run a
// browser. So it reads the page's own stylesheets and inline styles and works
// out which elements were hidden -- and when a rule is too complex to be sure
// about, it treats the whole page as hidden rather than guess.
//
// The asymmetry is deliberate. Reporting a hidden block as visible would tell a
// caller the page said something it did not; reporting nothing is merely
// unhelpful.

var (
	cssComment       = regexp.MustCompile(`(?s)/\*.*?\*/`)
	cssRule          = regexp.MustCompile(`([^{}]+)\{([^{}]*)\}`)
	cssHexEscape     = regexp.MustCompile(`\\([0-9A-Fa-f]{1,6})\s?`)
	cssCharEscape    = regexp.MustCompile(`\\([^\r\n\f])`)
	cssWhitespace    = regexp.MustCompile(`\s+`)
	cssKeyframeStop  = regexp.MustCompile(`^(\d+(\.\d+)?|\.\d+)%$`)
	cssDisplayNone   = regexp.MustCompile(`(^|;)display:none(!important)?(;|$)`)
	cssDynamicHide   = regexp.MustCompile(`(^|;)(display|visibility|content-visibility):(var|env|attr)\(`)
	cssVisibility    = regexp.MustCompile(`(^|;)visibility:(hidden|collapse)(!important)?(;|$)`)
	cssContentHide   = regexp.MustCompile(`(^|;)content-visibility:hidden(!important)?(;|$)`)
	cssOpacity       = regexp.MustCompile(`(?:^|;)opacity:([^;]+)`)
	cssTransform     = regexp.MustCompile(`(?:^|;)transform:([^;]+)`)
	cssScale         = regexp.MustCompile(`scale(x|y|3d)?\(([^()]*)\)`)
	cssCalc          = regexp.MustCompile(`(var|env|attr|calc)\(`)
	cssAttributeFlag = regexp.MustCompile(
		`\[\s*((?:\\[0-9A-Fa-f]{1,6}\s?|\\[^\r\n\f]|[A-Za-z0-9_:-])+)` +
			`\s*([~|^$*]?=)\s*("[^"]*"|'[^']*'|[^\]\s]+)\s+([iIsS])\s*\]`)
)

// cssUnescape resolves the escapes a stylesheet may use in a value.
func cssUnescape(value string) string {
	value = cssHexEscape.ReplaceAllStringFunc(value, func(match string) string {
		digits := cssHexEscape.FindStringSubmatch(match)[1]
		codepoint, err := strconv.ParseInt(digits, 16, 64)
		if err != nil || codepoint == 0 || codepoint > 0x10FFFF {
			return "�"
		}
		return string(rune(codepoint))
	})
	return cssCharEscape.ReplaceAllString(value, "$1")
}

// cssDeclarationsHide reports whether a declaration block hides its subject.
//
// Zero opacity and a zero scale count as hidden: they are how a page keeps an
// element in the markup while showing nothing.
func cssDeclarationsHide(value string) bool {
	value = cssComment.ReplaceAllString(value, "")
	declarations := cssWhitespace.ReplaceAllString(strings.ToLower(cssUnescape(value)), "")

	for _, match := range cssOpacity.FindAllStringSubmatch(declarations, -1) {
		opacity := strings.TrimSuffix(match[1], "!important")
		if strings.HasPrefix(opacity, "calc(") && strings.HasSuffix(opacity, ")") {
			return true
		}
		opacity = strings.TrimSuffix(opacity, "%")
		if number, err := strconv.ParseFloat(opacity, 64); err == nil {
			if number <= 0 {
				return true
			}
		} else if cssCalc.MatchString(opacity) {
			return true
		}
	}
	for _, match := range cssTransform.FindAllStringSubmatch(declarations, -1) {
		for _, scale := range cssScale.FindAllStringSubmatch(match[1], -1) {
			for _, argument := range strings.Split(scale[2], ",") {
				if number, err := strconv.ParseFloat(argument, 64); err == nil {
					if number == 0 {
						return true
					}
				} else if cssCalc.MatchString(argument) {
					return true
				}
			}
		}
	}
	return cssDisplayNone.MatchString(declarations) ||
		cssDynamicHide.MatchString(declarations) ||
		cssVisibility.MatchString(declarations) ||
		cssContentHide.MatchString(declarations)
}

// cssKeyframeSelectors reports whether a selector group is an animation stop
// rather than a real selector.
func cssKeyframeSelectors(value string) bool {
	selectors := strings.Split(value, ",")
	if len(selectors) == 0 {
		return false
	}
	for _, selector := range selectors {
		selector = strings.ToLower(strings.TrimSpace(selector))
		if selector != "from" && selector != "to" && !cssKeyframeStop.MatchString(selector) {
			return false
		}
	}
	return true
}

// cssConservativeSelectors rewrites the attribute-match flags the selector
// parser does not accept. A case-insensitive match inside `:not()` cannot be
// weakened safely, so it gives up instead.
func cssConservativeSelectors(value string) (string, bool) {
	if strings.Contains(strings.ToLower(value), ":not(") {
		for _, match := range cssAttributeFlag.FindAllStringSubmatch(value, -1) {
			if strings.ToLower(match[4]) == "i" {
				return "", false
			}
		}
	}
	return cssAttributeFlag.ReplaceAllStringFunc(value, func(match string) string {
		parts := cssAttributeFlag.FindStringSubmatch(match)
		if strings.ToLower(parts[4]) == "i" {
			return "[" + parts[1] + "]"
		}
		return "[" + parts[1] + parts[2] + parts[3] + "]"
	}), true
}

// stylesheetHidden is what the page's own stylesheets hide.
type stylesheetHidden struct {
	Nodes map[*xhtml.Node]bool
	// HideAll is set when a rule could not be understood. Everything is then
	// treated as hidden, because a partly-understood stylesheet cannot support
	// a claim about what was on screen.
	HideAll bool
}

// cssHiddenNodes evaluates the page's inline stylesheets.
func cssHiddenNodes(doc *xhtml.Node) stylesheetHidden {
	hidden := stylesheetHidden{Nodes: map[*xhtml.Node]bool{}}
	var sheets []string
	for _, node := range htmlquery.Find(doc, "//style") {
		sheets = append(sheets, htmlquery.InnerText(node))
	}
	css := cssComment.ReplaceAllString(strings.Join(sheets, "\n"), "")
	for _, rule := range cssRule.FindAllStringSubmatch(css, -1) {
		if !cssDeclarationsHide(rule[2]) {
			continue
		}
		group := strings.TrimSpace(rule[1])
		if cssKeyframeSelectors(group) {
			continue
		}
		group, ok := cssConservativeSelectors(group)
		if !ok {
			hidden.HideAll = true
			return hidden
		}
		selectors, err := cascadia.ParseGroupWithPseudoElements(group)
		if err != nil {
			hidden.HideAll = true
			return hidden
		}
		for _, selector := range selectors {
			if selector.PseudoElement() != "" {
				continue
			}
			for _, node := range cascadia.QueryAll(doc, selector) {
				hidden.Nodes[node] = true
			}
		}
	}
	return hidden
}

// paywallElement reports whether an element is the page's paywall marker.
func paywallElement(node *xhtml.Node) bool {
	identity := strings.ToLower(attr(node, "id") + " " + attr(node, "class"))
	return strings.Contains(identity, "chargewall") || strings.Contains(identity, "paywall")
}

// contentHiddenElement reports whether one element hides itself.
func contentHiddenElement(node *xhtml.Node) bool {
	if paywallElement(node) {
		return true
	}
	tag := strings.ToLower(node.Data)
	if (tag == "dialog" || tag == "details") && !hasAttr(node, "open") {
		return true
	}
	if hasAttr(node, "popover") || hasAttr(node, "hidden") {
		return true
	}
	if strings.ToLower(attr(node, "aria-hidden")) == "true" {
		return true
	}
	for _, class := range strings.Fields(strings.ToLower(attr(node, "class"))) {
		if class == "hidden" || class == "hide" || class == "undis" {
			return true
		}
	}
	return cssDeclarationsHide(attr(node, "style"))
}

// visibilityContext is the document order of a subtree plus the point at which
// the paywall begins. Everything at or after that point is behind it.
type visibilityContext struct {
	Order  map[*xhtml.Node]int
	Cutoff int
}

// contentVisibilityContext walks a subtree once, in document order.
func contentVisibilityContext(root *xhtml.Node) visibilityContext {
	context := visibilityContext{Order: map[*xhtml.Node]int{}, Cutoff: -1}
	index := 0
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode {
			context.Order[node] = index
			if context.Cutoff < 0 && paywallElement(node) {
				context.Cutoff = index
			}
			index++
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return context
}

// micrositeHiddenElement adds the microsite rules to the shared ones: anything
// labelled as members-only is treated as hidden, because a campaign page marks
// its gated blocks by name rather than by style.
func micrositeHiddenElement(node *xhtml.Node, hidden stylesheetHidden) bool {
	if hidden.HideAll || hidden.Nodes[node] {
		return true
	}
	tag := strings.ToLower(node.Data)
	if (tag == "dialog" || tag == "details") && !hasAttr(node, "open") {
		return true
	}
	if hasAttr(node, "popover") {
		return true
	}
	if contentHiddenElement(node) {
		return true
	}
	var identity strings.Builder
	identity.WriteString(tag)
	for _, attribute := range node.Attr {
		identity.WriteString(" ")
		identity.WriteString(attribute.Key)
		identity.WriteString(" ")
		identity.WriteString(attribute.Val)
	}
	lower := strings.ToLower(identity.String())
	for _, marker := range []string{
		"paywall", "chargewall", "paid", "member", "subscriber",
		"subscription", "premium", "vip", "locked",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// micrositeNodeVisible reports whether an element was on screen.
func micrositeNodeVisible(
	node, root *xhtml.Node,
	context visibilityContext,
	hidden stylesheetHidden,
) bool {
	position, known := context.Order[node]
	if !known || (context.Cutoff >= 0 && position >= context.Cutoff) {
		return false
	}
	for current := node; current != nil; current = current.Parent {
		if micrositeHiddenElement(current, hidden) {
			return false
		}
		if current == root {
			// Beyond the root the ancestors still count: a hidden wrapper
			// higher up hides everything under it.
			for above := current.Parent; above != nil; above = above.Parent {
				if above.Type == xhtml.ElementNode && micrositeHiddenElement(above, hidden) {
					return false
				}
			}
			return true
		}
	}
	return false
}

// micrositeVisibleText flattens the text a reader would have seen.
func micrositeVisibleText(
	node, root *xhtml.Node,
	context visibilityContext,
	hidden stylesheetHidden,
) string {
	var parts []string
	var visit func(*xhtml.Node)
	visit = func(current *xhtml.Node) {
		if !micrositeNodeVisible(current, root, context, hidden) {
			return
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			switch child.Type {
			case xhtml.TextNode:
				parts = append(parts, child.Data)
			case xhtml.ElementNode:
				position, known := context.Order[child]
				if context.Cutoff < 0 || (known && position < context.Cutoff) {
					visit(child)
				}
			}
		}
	}
	visit(node)
	return strings.TrimSpace(cssWhitespace.ReplaceAllString(strings.Join(parts, ""), " "))
}
