package main

import (
	"fmt"
	"os"
)

type cli struct {
	TVDBBaseURL string `name:"tvdb-base-url" hidden:"" help:"Override the TVDB API base URL."`

	Add       addCmd             `cmd:"" help:"Add a brand new series to the library."`
	Import    importCmd          `cmd:"" help:"Import an existing directory as a tracked series."`
	Scan      scanCmd            `cmd:"" help:"Scan a tracked series for episode files."`
	Show      showCmd            `cmd:"" help:"Show tracked series library state."`
	Reconcile seriesReconcileCmd `cmd:"" help:"Rename tracked files to match Kura metadata."`
	Reset     resetCmd           `cmd:"" help:"Remove staged media from a tracked episode."`
	Stage     stageCmd           `cmd:"" help:"Stage an external episode file for a series."`
	Meta      metaCmd            `cmd:"" help:"Metadata helper commands."`
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
