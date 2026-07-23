package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/wyvernzora/kura/services/library/internal/fsop"
)

const envUmask = "KURA_UMASK"

func configureUmask(getenv func(string) string) error {
	raw := strings.TrimSpace(getenv(envUmask))
	if raw == "" {
		fsop.SetPermissionMask(currentProcessUmask())
		return nil
	}
	mode, err := parseUmask(raw)
	if err != nil {
		return err
	}
	if err := setProcessUmask(mode); err != nil {
		return err
	}
	fsop.SetPermissionMask(mode)
	return nil
}

func parseUmask(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("%s must be an octal mode between 0000 and 0777", envUmask)
	}
	parsed, err := strconv.ParseUint(raw, 8, 12)
	if err != nil || parsed > 0o777 {
		return 0, fmt.Errorf("%s must be an octal mode between 0000 and 0777", envUmask)
	}
	return int(parsed), nil
}
