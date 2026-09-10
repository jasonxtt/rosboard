package diagnostics

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

const (
	redactedDeviceID = "[REDACTED_DEVICE_ID]"
	redactedPath     = "[REDACTED_PATH]"
)

var preservedIPv4Prefixes = [...]netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
}

func normalizedJSONKey(key string) string {
	return strings.ToLower(strings.NewReplacer("-", "", "_", "", " ", "", ".", "").Replace(key))
}

func sanitizeExportString(value, deviceID string) string {
	value = redactSensitiveText(value)
	if deviceID != "" {
		value = strings.ReplaceAll(value, deviceID, redactedDeviceID)
	}
	return redactNetworkPrivacy(value)
}

func redactDataDir(value string) string {
	trimmed := strings.TrimRight(value, "/\\")
	if trimmed == "" {
		return redactedPath
	}
	separator := strings.LastIndexAny(trimmed, "/\\")
	base := trimmed
	if separator >= 0 {
		base = trimmed[separator+1:]
	}
	if base == "" || base == "." || base == ".." {
		return redactedPath
	}
	return redactedPath + "/" + base
}

// redactNetworkPrivacy masks network identifiers only at the export boundary.
// It deliberately discovers narrow candidate tokens and lets netip validate
// them instead of using a permissive IPv6 regular expression.
func redactNetworkPrivacy(value string) string {
	var output strings.Builder
	last := 0
	for index := 0; index < len(value); {
		if !isNetworkTokenStart(value, index) || !networkTokenBoundary(value, index) {
			index++
			continue
		}
		end := index
		for end < len(value) && isNetworkTokenChar(value[end]) {
			end++
		}
		if isMetadataVersionValue(value, index) {
			index = end
			continue
		}
		masked, ok := maskNetworkToken(value[index:end])
		if !ok {
			index++
			continue
		}
		output.WriteString(value[last:index])
		output.WriteString(masked)
		last = end
		index = end
	}
	if last == 0 {
		return value
	}
	output.WriteString(value[last:])
	return output.String()
}

func isNetworkTokenStart(value string, index int) bool {
	if isHexDigit(value[index]) {
		return true
	}
	return value[index] == ':' && index+1 < len(value) && value[index+1] == ':'
}

func networkTokenBoundary(value string, index int) bool {
	if index == 0 {
		return true
	}
	// Avoid treating a version, identifier, or other word suffix as an IP
	// candidate. Separators such as '=', '[', '/', and whitespace are valid
	// starts for an address embedded in a diagnostic message.
	return !isAlphaNumeric(value[index-1])
}

func isNetworkTokenChar(value byte) bool {
	return isAlphaNumeric(value) || strings.ContainsRune(":./%_-", rune(value))
}

func isAlphaNumeric(value byte) bool {
	return (value >= '0' && value <= '9') || (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z')
}

func isHexDigit(value byte) bool {
	return (value >= '0' && value <= '9') || (value >= 'a' && value <= 'f') || (value >= 'A' && value <= 'F')
}

func isMetadataVersionValue(value string, index int) bool {
	separator := -1
	for cursor := index - 1; cursor >= 0; cursor-- {
		if value[cursor] == '=' || value[cursor] == ':' {
			separator = cursor
			break
		}
		if value[cursor] == '\n' || value[cursor] == ',' || value[cursor] == ';' {
			break
		}
	}
	if separator < 0 {
		return false
	}
	keyEnd := separator
	for keyEnd > 0 && (value[keyEnd-1] == ' ' || value[keyEnd-1] == '\t') {
		keyEnd--
	}
	keyStart := keyEnd
	for keyStart > 0 && (isAlphaNumeric(value[keyStart-1]) || value[keyStart-1] == '-' || value[keyStart-1] == '_') {
		keyStart--
	}
	key := normalizedJSONKey(value[keyStart:keyEnd])
	switch key {
	case "version", "build", "buildat", "commit", "sha", "sha256", "hash", "timestamp", "generatedat", "datetime", "date", "time":
		return true
	default:
		return false
	}
}

func maskNetworkToken(token string) (string, bool) {
	if isMACAddressToken(token) {
		// The current snapshot proplists do not request MAC addresses. Keep this
		// guard so a MAC-like value cannot be mistaken for an IPv6 address if a
		// future RouterOS response includes one unexpectedly.
		return token, false
	}
	addressText, prefixLength, hasPrefix, ok := splitNetworkToken(token)
	if !ok {
		return maskEmbeddedIPv4Token(token)
	}
	address, err := netip.ParseAddr(addressText)
	if err != nil {
		return maskEmbeddedIPv4Token(token)
	}
	if address.Is4() && !strings.Contains(addressText, ".") {
		return token, false
	}
	if !address.Is4() && !strings.Contains(addressText, ":") {
		return token, false
	}
	return maskNetworkAddress(token, address, prefixLength, hasPrefix), true
}

func maskEmbeddedIPv4Token(token string) (string, bool) {
	addressEnd := 0
	for addressEnd < len(token) && ((token[addressEnd] >= '0' && token[addressEnd] <= '9') || token[addressEnd] == '.') {
		addressEnd++
	}
	if addressEnd == 0 || addressEnd == len(token) {
		return token, false
	}
	address, err := netip.ParseAddr(token[:addressEnd])
	if err != nil || !address.Is4() {
		return token, false
	}
	suffix := token[addressEnd:]
	if suffix[0] != ':' && suffix[0] != '%' && suffix[0] != '/' {
		return token, false
	}
	return maskNetworkAddress(token[:addressEnd], address, 0, false) + suffix, true
}

func splitNetworkToken(token string) (string, int, bool, bool) {
	addressText := token
	prefixLength := 0
	hasPrefix := false
	if slash := strings.LastIndexByte(token, '/'); slash >= 0 {
		if slash == 0 || strings.Contains(token[:slash], "/") {
			return "", 0, false, false
		}
		prefixText := token[slash+1:]
		if prefixText == "" {
			return "", 0, false, false
		}
		for index := range prefixText {
			if prefixText[index] < '0' || prefixText[index] > '9' {
				return "", 0, false, false
			}
		}
		parsed, err := strconv.Atoi(prefixText)
		if err != nil {
			return "", 0, false, false
		}
		addressText = token[:slash]
		prefixLength = parsed
		hasPrefix = true
	}
	address, err := netip.ParseAddr(addressText)
	if err != nil {
		return "", 0, false, false
	}
	maxBits := 128
	if address.Is4() {
		maxBits = 32
	}
	if hasPrefix && prefixLength > maxBits {
		return "", 0, false, false
	}
	return addressText, prefixLength, hasPrefix, true
}

func maskNetworkAddress(original string, address netip.Addr, prefixLength int, hasPrefix bool) string {
	if address.Is4() {
		if shouldPreserveIPv4(address) {
			return original
		}
		bytes := address.As4()
		masked := fmt.Sprintf("%d.%d.x.x", bytes[0], bytes[1])
		return appendNetworkPrefix(masked, prefixLength, hasPrefix)
	}

	if address.IsUnspecified() || address.IsLoopback() || address.IsMulticast() {
		return original
	}
	addressBytes := address.As16()
	firstHextet := fmt.Sprintf("%x", uint16(addressBytes[0])<<8|uint16(addressBytes[1]))
	masked := ""
	switch {
	case address.IsLinkLocalUnicast():
		masked = firstHextet + "::xxxx"
	case isULA(address):
		masked = firstHextet + "::xxxx"
	case address.IsGlobalUnicast():
		masked = firstHextet + ":xxxx:xxxx:xxxx::"
	default:
		return original
	}
	if zone := address.Zone(); zone != "" {
		masked += "%" + zone
	}
	return appendNetworkPrefix(masked, prefixLength, hasPrefix)
}

func appendNetworkPrefix(value string, prefixLength int, hasPrefix bool) string {
	if !hasPrefix {
		return value
	}
	return value + "/" + strconv.Itoa(prefixLength)
}

func shouldPreserveIPv4(address netip.Addr) bool {
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsUnspecified() || address.IsMulticast() {
		return true
	}
	for _, prefix := range preservedIPv4Prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func isULA(address netip.Addr) bool {
	if !address.Is6() {
		return false
	}
	bytes := address.As16()
	return bytes[0]&0xfe == 0xfc
}

func isMACAddressToken(value string) bool {
	separator := ':'
	if strings.Contains(value, "-") && !strings.Contains(value, ":") {
		separator = '-'
	}
	parts := strings.Split(value, string(separator))
	if len(parts) != 6 {
		return false
	}
	for _, part := range parts {
		if len(part) != 2 || !isHexDigit(part[0]) || !isHexDigit(part[1]) {
			return false
		}
	}
	return true
}
