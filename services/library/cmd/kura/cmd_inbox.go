package main

import (
	"github.com/wyvernzora/kura/services/library/internal/cli/client"
	"github.com/wyvernzora/kura/services/library/internal/cli/render"
)

type inboxCmd struct {
	List inboxListCmd `cmd:"" help:"List entries under the inbox root."`
}

type inboxListCmd struct {
	JSON          bool   `name:"json" help:"Print machine-readable JSON instead of a styled table."`
	Recursive     bool   `name:"recursive" short:"r" help:"Walk subdirectories up to --depth levels deep."`
	Depth         int    `name:"depth" help:"Recursive depth cap (default 3, max 5)."`
	Limit         int    `name:"limit" help:"Cap on entries returned (default 500, max 5000)."`
	Kind          string `name:"kind" short:"k" help:"Filter by entry kind: file, dir, or symlink."`
	NameGlob      string `name:"name" help:"filepath.Match glob filtered against basename (e.g. '*.mkv')."`
	IncludeHidden bool   `name:"all" short:"a" help:"Surface dotfiles and download-in-flight markers."`
	Path          string `arg:"" optional:"" help:"Directory or exact file under the inbox root (default: inbox root)."`
}

func (cmd *inboxListCmd) Run(rt *runContext) error {
	c := clientFromRT(rt)
	result, err := c.InboxList(rt.Context, client.InboxListOptions{
		Path:          cmd.Path,
		Recursive:     cmd.Recursive,
		Depth:         cmd.Depth,
		Limit:         cmd.Limit,
		Kind:          cmd.Kind,
		NameGlob:      cmd.NameGlob,
		IncludeHidden: cmd.IncludeHidden,
	})
	if err != nil {
		return err
	}
	return render.InboxList(rt.Stdout, result, cmd.JSON)
}
