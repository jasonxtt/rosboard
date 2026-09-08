package applicationpreset

import (
	"errors"
	"fmt"
	"strings"
)

var ErrRequestedKindUnavailable = errors.New("requested application preset kind is unavailable")

// ResolveRequestedKinds turns the selector's requested kind set into a
// deterministic materialization order. An empty request follows the product
// default: Domain when present, otherwise IP. A requested kind must be
// available in the selected YAML; this keeps an accidental empty TargetList
// from being created.
func ResolveRequestedKinds(requested []string, hasDomain, hasIP bool) ([]string, error) {
	available := map[string]bool{
		"domain": hasDomain,
		"ip":     hasIP,
	}
	if len(requested) == 0 {
		switch {
		case hasDomain:
			return []string{"domain"}, nil
		case hasIP:
			return []string{"ip"}, nil
		default:
			return nil, ErrRequestedKindUnavailable
		}
	}

	selected := map[string]bool{}
	for _, value := range requested {
		kind := strings.ToLower(strings.TrimSpace(value))
		if kind != "domain" && kind != "ip" {
			return nil, fmt.Errorf("unsupported application preset kind %q", value)
		}
		if !available[kind] {
			return nil, fmt.Errorf("%w: %s", ErrRequestedKindUnavailable, kind)
		}
		selected[kind] = true
	}

	result := make([]string, 0, 2)
	for _, kind := range []string{"domain", "ip"} {
		if selected[kind] {
			result = append(result, kind)
		}
	}
	return result, nil
}
