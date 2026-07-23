// Package cli holds reusable CLI building blocks: the resolve+act
// composition helper, interactive disambiguation prompt, render helpers
// for human output, and styling/table primitives.
//
// cmd/kura-library-manager/ subcommand structs stay slim — they parse flags, build
// workflow.Deps, and call cli helpers + workflow.X + cli.RenderX.
//
// Subpackages:
//
//   - cli/render — one file per response shape; produces tables and
//     formatted text.
//   - cli/prompt — interactive disambiguation prompts (TTY only).
//   - cli/style  — ANSI styling helpers.
//   - cli/table  — table primitives.
//
// Top-level helpers (resolve.go, errors.go) live in this package.
package cli
