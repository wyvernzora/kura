package cli

import (
	"fmt"
	"strings"

	"github.com/wyvernzora/kura/internal/response"
)

// ParseListStatus converts a CLI flag value into a response.ListStatus,
// rejecting unknown values with a helpful error.
func ParseListStatus(value string) (response.ListStatus, error) {
	status := response.ListStatus(strings.TrimSpace(value))
	switch status {
	case response.ListStatusUntracked, response.ListStatusComplete,
		response.ListStatusIncomplete, response.ListStatusError:
		return status, nil
	default:
		return "", fmt.Errorf("invalid list status %q; expected one of: untracked, complete, incomplete, error", value)
	}
}
