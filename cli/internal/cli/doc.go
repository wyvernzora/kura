// Package cli holds reusable building blocks for the kura REST CLI.
//
// The cmd/kura subcommand structs parse flags, call the REST client in
// cli/client, and render output through cli/render.
//
// Subpackages:
//
//   - cli/client — REST discovery, request, and response handling.
//   - cli/render — human-readable and JSON output.
//   - cli/prompt — interactive disambiguation prompts.
//   - cli/stdio — terminal input and output helpers.
//   - cli/style — ANSI styling helpers.
//
// Top-level helpers parse shared flags and compose resolution with commands.
package cli
