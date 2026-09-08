package ownership

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

const (
	// Prefix is the current scoped comment identity. The rbs_ marker keeps an
	// upgraded installation distinguishable from old rb_<8> cleanup logic.
	Prefix       = "rbs_"
	LegacyPrefix = "rb_"
)

// NamespacePrefix is deliberately distinct from the legacy RouterOS names
// (rb_ac_, rbac_, and rosboard_access_). Older rosboard installations use
// those names as a broad cleanup boundary, so a current installation needs a
// physical namespace that they cannot mistake for its own graph.
const NamespacePrefix = "rbs_"

// Scope returns the installation/device namespace used by managed RouterOS
// comments. Length framing keeps the manager and device boundaries
// unambiguous even if either identifier contains the separator character.
func Scope(managerID, deviceID string) string {
	managerID = strings.TrimSpace(managerID)
	deviceID = strings.TrimSpace(deviceID)
	input := "scope:v1:" + strconv.Itoa(len(managerID)) + ":" + managerID + ":" + strconv.Itoa(len(deviceID)) + ":" + deviceID
	return shortHash(input, 8)
}

func Object(logicalID string) string {
	return shortHash(logicalID, 8)
}

func Identity(managerID, deviceID, logicalID string) string {
	return Prefix + Scope(managerID, deviceID) + "_" + Object(logicalID)
}

func LegacyScopedPrefix(managerID, deviceID string) string {
	return LegacyPrefix + Scope(managerID, deviceID) + "_"
}

func LegacyScopedIdentity(managerID, deviceID, logicalID string) string {
	return LegacyScopedPrefix(managerID, deviceID) + Object(logicalID)
}

// Namespace returns the current installation/device prefix for physical
// RouterOS references such as address-lists, chains, and DNS forwarders.
func Namespace(managerID, deviceID string) string {
	return NamespacePrefix + Scope(managerID, deviceID) + "_"
}

func IsNamespace(value string) bool {
	parts := strings.SplitN(strings.TrimSpace(value), "_", 3)
	return len(parts) == 3 && parts[0] == strings.TrimSuffix(NamespacePrefix, "_") && hasLowerHex(parts[1], 8) && strings.Contains(parts[2], "_")
}

func IsNamespaceFor(managerID, deviceID, value string) bool {
	return IsNamespace(value) && strings.HasPrefix(strings.TrimSpace(value), Namespace(managerID, deviceID))
}

func CommentIdentity(comment string) string {
	comment = strings.TrimSpace(comment)
	if index := strings.Index(comment, " | "); index >= 0 {
		return strings.TrimSpace(comment[:index])
	}
	return comment
}

func IsCanonical(identity string) bool {
	parts := strings.Split(CommentIdentity(identity), "_")
	return len(parts) == 3 && parts[0] == strings.TrimSuffix(Prefix, "_") && hasLowerHex(parts[1], 8) && hasLowerHex(parts[2], 8)
}

func IsCanonicalFor(managerID, deviceID, identity string) bool {
	identity = CommentIdentity(identity)
	return IsCanonical(identity) && strings.HasPrefix(identity, Prefix+Scope(managerID, deviceID)+"_")
}

func IsUnscopedLegacy(identity string) bool {
	identity = CommentIdentity(identity)
	return strings.HasPrefix(identity, LegacyPrefix) && !strings.Contains(strings.TrimPrefix(identity, LegacyPrefix), "_") && hasLowerHex(strings.TrimPrefix(identity, LegacyPrefix), 8)
}

func IsLegacyScoped(identity string) bool {
	parts := strings.Split(CommentIdentity(identity), "_")
	return len(parts) == 3 && parts[0] == strings.TrimSuffix(LegacyPrefix, "_") && hasLowerHex(parts[1], 8) && hasLowerHex(parts[2], 8)
}

func IsLegacyScopedFor(managerID, deviceID, identity string) bool {
	return IsLegacyScoped(identity) && strings.HasPrefix(CommentIdentity(identity), LegacyScopedPrefix(managerID, deviceID))
}

func hasLowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func shortHash(value string, length int) string {
	digest := sha256.Sum256([]byte(value))
	encoded := hex.EncodeToString(digest[:])
	if length > len(encoded) {
		return encoded
	}
	return encoded[:length]
}
