package main

import (
	"fmt"
	"os"
)

// cli is the kong root binding for `kura`. Hidden global flags (kong
// `hidden:""`) live here at the root rather than on individual command
// structs so they apply to every subcommand uniformly without
// per-command duplication. Add new global toggles here and document
// them in this comment.
type cli struct {
	// TVDBBaseURL overrides the TVDB API base URL. Hidden from --help
	// output; intended for tests and local-mock setups.
	TVDBBaseURL string `name:"tvdb-base-url" hidden:"" help:"Override the TVDB API base URL."`

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
	Serve     serveCmd     `cmd:"" help:"Run kura as a long-lived server (MCP transports)."`
	Stage     stageCmd     `cmd:"" help:"Stage an external episode file for a series."`
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
