package cli

import (
	"fmt"
	"strings"

	"github.com/wyvernzora/kura/services/library-manager/pkg/api"
)

// ParseListStatus converts a CLI flag value into a api.ListStatus,
// rejecting unknown values with a helpful error.
func ParseListStatus(value string) (api.ListStatus, error) {
	status := api.ListStatus(strings.TrimSpace(value))
	switch status {
	case api.ListStatusUntracked, api.ListStatusComplete,
		api.ListStatusIncomplete, api.ListStatusError:
		return status, nil
	default:
		return "", fmt.Errorf("invalid list status %q; expected one of: untracked, complete, incomplete, error", value)
	}
}
