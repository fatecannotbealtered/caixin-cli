package caixin

import (
	"regexp"
	"strings"
)

// A page's continuation parameters live in a script. This client reads that
// script as text and checks it still says what this build was written against;
// it never executes it. The scan below is a real brace matcher rather than a
// regexp because a function body contains braces, strings, and comments, and a
// regexp that stops at the first `}` would read half a function and call it a
// match.

var javascriptIdentifier = regexp.MustCompile(`^[A-Za-z_$][\w$]*$`)

// javascriptNamedFunction finds one named function and returns its parameters
// and body.
//
// Exactly one definition must exist: two would mean the script defines the same
// name twice and there is no safe way to know which one runs.
func javascriptNamedFunction(source, name string) ([]string, string, error) {
	pattern := regexp.MustCompile(`\bfunction\s+` + regexp.QuoteMeta(name) + `\s*\(([^)]*)\)\s*\{`)
	matches := pattern.FindAllStringSubmatchIndex(source, -1)
	if len(matches) != 1 {
		return nil, "", &APIError{
			Message: "the continuation script does not define " + name + " exactly once",
		}
	}
	match := matches[0]
	var parameters []string
	for _, value := range strings.Split(source[match[2]:match[3]], ",") {
		value = strings.TrimSpace(value)
		if !javascriptIdentifier.MatchString(value) {
			return nil, "", &APIError{
				Message: "the continuation script's " + name + " parameters changed",
			}
		}
		parameters = append(parameters, value)
	}

	opening := match[1] - 1
	depth := 1
	quote := byte(0)
	escaped := false
	lineComment := false
	blockComment := false
	for index := opening + 1; index < len(source); index++ {
		character := source[index]
		var following byte
		if index+1 < len(source) {
			following = source[index+1]
		}
		switch {
		case lineComment:
			if character == '\r' || character == '\n' {
				lineComment = false
			}
		case blockComment:
			if character == '*' && following == '/' {
				blockComment = false
				index++
			}
		case quote != 0:
			switch {
			case escaped:
				escaped = false
			case character == '\\':
				escaped = true
			case character == quote:
				quote = 0
			}
		case character == '\'' || character == '"' || character == '`':
			quote = character
		case character == '/' && following == '/':
			lineComment = true
			index++
		case character == '/' && following == '*':
			blockComment = true
			index++
		case character == '{':
			depth++
		case character == '}':
			depth--
			if depth == 0 {
				return parameters, source[opening+1 : index], nil
			}
		}
	}
	return nil, "", &APIError{
		Message: "the continuation script's " + name + " is not closed",
	}
}
