//go:build e2e

// Package e2e contains end-to-end tests for the kura library manager.
// Each scenario boots its own kura-e2e daemon, exercises CLI verbs as
// subprocesses against it via rsc.io/script, and shuts the daemon
// down on test cleanup. The kura-e2e binary is built once per process
// with -tags=e2e_stub so the in-memory provider/inspector stubs are
// wired in via --use-test-stubs.
//
// Run with:
//
//	go test -tags=e2e -v ./e2e/...
//
// Or via make:
//
//	make e2e
package e2e

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/tools/txtar"
	"rsc.io/script"
)

// TestScenarios globs scenarios/*.txtar and runs each as a sub-test.
// Each scenario runs in parallel in its own TempDir against its own
// daemon.
func TestScenarios(t *testing.T) {
	files, err := filepath.Glob("scenarios/*.txtar")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no scenario files found in scenarios/")
	}
	for _, f := range files {
		f := f
		t.Run(strings.TrimSuffix(filepath.Base(f), ".txtar"), func(t *testing.T) {
			t.Parallel()
			runScenario(t, f)
		})
	}
}

func runScenario(t *testing.T, path string) {
	t.Helper()

	name := strings.TrimSuffix(filepath.Base(path), ".txtar")
	fmt.Fprintf(os.Stderr, "e2e: %-42s START\n", name)
	t.Cleanup(func() {
		status := "PASS"
		if t.Failed() {
			status = "FAIL"
		}
		fmt.Fprintf(os.Stderr, "e2e: %-42s %s\n", name, status)
	})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// ar.Comment holds the script body (everything before the first
	// "-- file --" section). ar.Files holds any embedded test fixtures.
	ar := txtar.Parse(data)
	config, err := parseScenarioConfig(string(ar.Comment))
	if err != nil {
		t.Fatal(err)
	}
	if config.unixOnly && !isUnixGOOS(runtime.GOOS) {
		t.Skip("scenario requires Unix permissions")
	}

	libRoot := t.TempDir()
	inboxRoot := t.TempDir()
	b := startDaemon(t, libRoot, inboxRoot, config.umask)
	eng := newEngine(t, b)

	workdir := inboxRoot
	// Pin workdir with a sentinel file so pruneEmptyAncestors in
	// reconcile/apply never empties and deletes the script working
	// directory after kura_apply moves staged files out of it.
	if err := os.WriteFile(filepath.Join(workdir, ".keep"), nil, 0o644); err != nil {
		t.Fatalf("pin workdir: %v", err)
	}
	// script.NewState's third arg replaces the environment for any
	// `exec` directive inside the scenario. Inherit os.Environ() so
	// PATH (and other shell-essential vars) reach scenario-level
	// `exec find/grep/...` calls; layer the scenario-specific vars
	// on top.
	scriptEnv := append([]string(nil), os.Environ()...)
	scriptEnv = append(scriptEnv,
		"KURA_LIB_ROOT="+libRoot,
		"KURA_SERVER_URL="+b.url,
	)
	state, err := script.NewState(t.Context(), workdir, scriptEnv)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := state.CloseAndWait(io.Discard); err != nil {
			t.Logf("state cleanup error: %v", err)
		}
	})

	// Extract any embedded fixture files into the script's workdir.
	if len(ar.Files) > 0 {
		if err := state.ExtractFiles(ar); err != nil {
			t.Fatalf("extract fixture files: %v", err)
		}
	}

	var log bytes.Buffer
	if err := eng.Execute(state, path, bufio.NewReader(bytes.NewReader(ar.Comment)), &log); err != nil {
		t.Logf("script output:\n%s", log.String())
		b.dumpStderr()
		t.Fatal(err)
	}
}

type scenarioConfig struct {
	umask    string
	unixOnly bool
}

func parseScenarioConfig(scriptBody string) (scenarioConfig, error) {
	var config scenarioConfig
	scanner := bufio.NewScanner(strings.NewReader(scriptBody))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		const configPrefix = "# kura_e2e_config "
		if !strings.HasPrefix(line, configPrefix) {
			if line == "# kura_e2e_unix" {
				config.unixOnly = true
			}
			continue
		}
		entry := strings.TrimSpace(strings.TrimPrefix(line, configPrefix))
		key, value, ok := strings.Cut(entry, "=")
		if !ok || strings.TrimSpace(key) != "server.umask" || strings.TrimSpace(value) == "" {
			return scenarioConfig{}, fmt.Errorf("invalid kura_e2e_config directive %q", line)
		}
		if config.umask != "" {
			return scenarioConfig{}, fmt.Errorf("duplicate server.umask directive")
		}
		config.umask = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return scenarioConfig{}, err
	}
	return config, nil
}

func isUnixGOOS(goos string) bool {
	switch goos {
	case "aix", "android", "darwin", "dragonfly", "freebsd", "hurd", "illumos", "ios", "linux", "netbsd", "openbsd", "solaris":
		return true
	default:
		return false
	}
}
