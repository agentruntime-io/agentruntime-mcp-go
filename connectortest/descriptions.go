package connectortest

import (
	"fmt"
	"regexp"
	"strings"
)

var descriptionEllipsisRE = regexp.MustCompile(`Description:\s*"[^"]*\.\.\.[^"]*"`)

// ValidateDescriptionsNoEllipsis fails when register.go tool descriptions use "..." in API paths.
// Agents rely on full HTTP paths in catalog descriptions (see CONNECTOR_QUALITY_SOP §9.2).
func ValidateDescriptionsNoEllipsis(registerGoSource string) error {
	matches := descriptionEllipsisRE.FindAllString(registerGoSource, -1)
	if len(matches) == 0 {
		return nil
	}
	return fmt.Errorf("descriptions must not use ... ellipsis in API paths:\n  - %s", strings.Join(matches, "\n  - "))
}
