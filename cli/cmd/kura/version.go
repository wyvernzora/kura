package main

// Version is the kura CLI build version. Stamped at link time via
// `-ldflags="-X main.Version=<value>"`. Defaults to "dev" so ad-hoc
// `go build` and `go run` invocations still produce a usable binary.
//
// It is surfaced by `kura --version`.
var Version = "dev"
