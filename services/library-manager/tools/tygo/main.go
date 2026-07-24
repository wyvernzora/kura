// Command tygo-gen runs tygo and post-processes its output.
//
// Tygo emits Go `type X string` aliases as `export type X = string;`,
// which loses literal-value narrowing on the TS side. This wrapper
// substitutes a literal-union form for the closed enums in
// pkg/api. Keep the constants in sync with the Go source —
// `make check-gen` will fail if a new value is added without updating
// either the Go enum or this wrapper.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type enumOverride struct {
	from string
	to   string
}

var enumOverrides = []enumOverride{
	{
		from: "export type ListStatus = string;",
		to:   `export type ListStatus = "untracked" | "complete" | "incomplete" | "error";`,
	},
	{
		from: "export type Status = string;",
		to:   `export type Status = "pending" | "missing" | "present" | "staged" | "staged_replacement";`,
	},
	{
		from: "export type ScanStatus = string;",
		to:   `export type ScanStatus = "added" | "updated" | "unchanged" | "removed";`,
	},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "tygo-gen:", err)
		os.Exit(1)
	}
}

func run() error {
	cmd := exec.Command("go", "tool", "tygo", "generate")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tygo generate: %w", err)
	}
	return postProcess(filepath.Join("..", "..", "..", "webui", "src", "api", "types.gen.ts"))
}

func postProcess(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for _, o := range enumOverrides {
		from := []byte(o.from)
		if !bytes.Contains(data, from) {
			return fmt.Errorf("enum override not found in generated output: %q", o.from)
		}
		data = bytes.Replace(data, from, []byte(o.to), 1)
	}
	return os.WriteFile(path, data, 0o644)
}
