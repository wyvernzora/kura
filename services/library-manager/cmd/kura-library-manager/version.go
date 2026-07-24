package main

// Version is the library-manager build version. Stamped at link time via
// `-ldflags="-X main.Version=<value>"`. Defaults to "dev" so ad-hoc
// `go build` and `go run` invocations still produce a usable binary.
var Version = "dev"
