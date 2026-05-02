package main

import (
	"fmt"
	"os"
)

type cli struct {
	TVDBBaseURL string `name:"tvdb-base-url" hidden:"" help:"Override the TVDB API base URL."`

	Add       addCmd       `cmd:"" help:"Add a brand new series to the library."`
	Import    importCmd    `cmd:"" help:"Import an existing directory as a tracked series."`
	List      listCmd      `cmd:"" help:"List library contents."`
	Resolve   resolveCmd   `cmd:"" help:"Resolve selector terms to metadata candidates."`
	Scan      scanCmd      `cmd:"" help:"Scan a tracked series for episode files."`
	Show      showCmd      `cmd:"" help:"Show tracked series library state."`
	Reindex   reindexCmd   `cmd:"" help:"Rebuild the library metadata index."`
	Reconcile reconcileCmd `cmd:"" help:"Plan and apply filesystem reconciliation for tracked files."`
	Reset     resetCmd     `cmd:"" help:"Remove staged media from a tracked episode."`
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
