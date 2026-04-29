package main

import (
	"fmt"
	"os"
)

type cli struct {
	TVDBBaseURL string `name:"tvdb-base-url" hidden:"" help:"Override the TVDB API base URL."`

	Add       addCmd             `cmd:"" help:"Add a brand new series to the library."`
	Sync      seriesSyncCmd      `cmd:"" help:"Scan a series and sync it into Kura metadata."`
	Reconcile seriesReconcileCmd `cmd:"" help:"Rename tracked files to match Kura metadata."`
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
