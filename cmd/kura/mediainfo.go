package main

import (
	"strings"

	"github.com/wyvernzora/kura/internal/mediainfo"
)

func mediaInspector(rt runContext) mediainfo.Inspector {
	inspector := mediainfo.New()
	command := strings.TrimSpace(rt.Getenv("KURA_MEDIAINFO_COMMAND"))
	if command != "" {
		inspector.Command = command
	}
	return inspector
}
