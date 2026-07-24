package main

import (
	"fmt"
	"os"
)

// cli is the kong root binding for kura-library-manager.
type cli struct {
	Add       addCmd       `cmd:"" help:"Add a brand new series to the library."`
	Alias     aliasCmd     `cmd:"" help:"Manage user-coined search aliases for a series."`
	Import    importCmd    `cmd:"" help:"Import an existing directory as a tracked series."`
	Inbox     inboxCmd     `cmd:"" help:"Inspect the inbox where new media drops before staging."`
	List      listCmd      `cmd:"" help:"List library contents."`
	Path      pathCmd      `cmd:"" help:"Print the absolute path of a tracked series's root directory."`
	Resolve   resolveCmd   `cmd:"" help:"Resolve selector terms to metadata candidates."`
	Scan      scanCmd      `cmd:"" help:"Scan a tracked series for episode files."`
	Show      showCmd      `cmd:"" help:"Show tracked series library state."`
	Reindex   reindexCmd   `cmd:"" help:"Rebuild the library metadata index."`
	Reconcile reconcileCmd `cmd:"" help:"Plan and apply filesystem reconciliation for tracked files."`
	Remove    removeCmd    `cmd:"" help:"Untrack a series; with --purge wholesale delete its directory."`
	Reset     resetCmd     `cmd:"" help:"Remove staged media from a tracked episode."`
	Serve     serveCmd     `cmd:"" help:"Run kura as a long-lived REST/MCP server."`
	Stage     stageCmd     `cmd:"" help:"Stage episode, trash, or extra intent for a series."`
	Tag       tagCmd       `cmd:"" help:"Update opaque workflow tags on a series."`
	Trash     trashCmd     `cmd:"" help:"Manage per-series trash entries."`
}

func main() {
	rt := runContext{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Getenv: os.Getenv,
	}
	if err := run(os.Args[1:], rt); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
