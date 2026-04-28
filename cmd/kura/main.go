package main

import (
	"fmt"
	"os"
)

type cli struct {
	Sync      seriesSyncCmd      `cmd:"" help:"Scan a series and sync it into Kura metadata."`
	Reconcile seriesReconcileCmd `cmd:"" help:"Rename tracked files to match Kura metadata."`
	Stage     stageCmd           `cmd:"" help:"Stage an external episode file for a series."`
	Meta      metaCmd            `cmd:"" help:"Metadata provider commands."`
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
