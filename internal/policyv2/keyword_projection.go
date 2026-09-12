package policyv2

import "strings"

// escapeRouterOSDNSRegexLiteral escapes the RouterOS DNS Static regexp
// metacharacters without relying on Go's regexp dialect. DNS Static uses a
// RouterOS regular-expression engine, so the keyword remains a literal while
// the surrounding .* expression supplies substring semantics.
func escapeRouterOSDNSRegexLiteral(value string) string {
	var builder strings.Builder
	for _, character := range value {
		if strings.ContainsRune(`\\.+*?()[]{}^$|`, character) {
			builder.WriteByte('\\')
		}
		builder.WriteRune(character)
	}
	return builder.String()
}

func routerOSDNSKeywordRegexp(keyword string) string {
	return ".*" + escapeRouterOSDNSRegexLiteral(keyword) + ".*"
}
