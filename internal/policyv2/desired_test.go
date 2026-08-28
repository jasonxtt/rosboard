package policyv2

import (
	"strconv"
	"strings"
	"testing"

	"rosboard/internal/routeros"
)

func TestAppendDNSCacheWarningOnlyAboveRuleThreshold(t *testing.T) {
	for _, test := range []struct {
		name      string
		ruleCount int
		warnings  int
	}{
		{name: "threshold", ruleCount: 1000, warnings: 0},
		{name: "above threshold", ruleCount: 1001, warnings: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := DesiredResult{Objects: make([]DesiredObject, 0, test.ruleCount)}
			for index := 0; index < test.ruleCount; index++ {
				result.Objects = append(result.Objects, DesiredObject{
					Menu:   string(routeros.MenuIPDNSStatic),
					Fields: map[string]string{"name": "domain-" + strconv.Itoa(index) + ".example"},
				})
			}

			appendDNSCacheWarning(&result)
			if len(result.Warnings) != test.warnings {
				t.Fatalf("warnings=%d, want %d: %#v", len(result.Warnings), test.warnings, result.Warnings)
			}
			if test.warnings == 1 && !strings.Contains(result.Warnings[0].Reason, "32MiB") {
				t.Fatalf("warning does not mention the default cache size: %#v", result.Warnings[0])
			}
		})
	}
}
