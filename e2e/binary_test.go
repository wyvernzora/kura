//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// e2eBinaryPath is the path to the kura-e2e binary built once for the
// whole test process. Cached via buildOnce.
var (
	e2eBinaryPath string
	buildOnce     sync.Once
	buildErr      error
)

// buildKuraE2E compiles cmd/kura with -tags=e2e_stub once per process,
// returning the binary path. Subsequent calls return the cached path.
// On compile failure, every caller surfaces the same error.
func buildKuraE2E(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		// Place under os.TempDir so the binary survives across the
		// suite without being scrubbed by per-test t.TempDir cleanup.
		dir, err := os.MkdirTemp("", "kura-e2e-bin-")
		if err != nil {
			buildErr = fmt.Errorf("mktemp: %w", err)
			return
		}
		bin := filepath.Join(dir, "kura-e2e")
		cmd := exec.Command("go", "build", "-tags=e2e_stub", "-o", bin, "./cmd/kura")
		cmd.Dir = repoRoot()
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			buildErr = fmt.Errorf("go build kura-e2e: %w\nstderr: %s", err, stderr.String())
			return
		}
		e2eBinaryPath = bin
	})
	if buildErr != nil {
		t.Fatalf("buildKuraE2E: %v", buildErr)
	}
	return e2eBinaryPath
}

// repoRoot returns the kura repo root. Tests run from e2e/, so go up
// one level. Centralized so callers don't repeat the relative.
func repoRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return filepath.Dir(wd)
}

// e2eBinary represents one running kura-e2e daemon plus subprocess
// helpers for exercising it via CLI invocations.
type e2eBinary struct {
	t         *testing.T
	bin       string
	libRoot   string
	inboxRoot string
	port      int
	url       string
	env       []string
	cmd       *exec.Cmd
	stderr    *syncBuffer
	stopOnce  sync.Once
}

// syncBuffer is a goroutine-safe wrapper around bytes.Buffer. The
// exec.Cmd writer goroutine fills it concurrently with the test
// goroutine reading via dumpStderr; bare bytes.Buffer races under
// -race.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Len()
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// startDaemon builds (if needed) the kura-e2e binary and boots
// `kura-e2e serve --rest=:0 --rest-port-file=... --use-test-stubs`
// against libRoot. Blocks up to 5s waiting for the port file to
// appear and the /health endpoint to respond.
func startDaemon(t *testing.T, libRoot, inboxRoot string, extraEnv []string) *e2eBinary {
	t.Helper()
	bin := buildKuraE2E(t)

	dir := t.TempDir()
	portFile := filepath.Join(dir, "port")

	cmd := exec.Command(bin, "serve",
		"--rest=127.0.0.1:0",
		"--rest-port-file="+portFile,
		"--use-test-stubs",
	)
	cmd.Env = append(os.Environ(),
		"KURA_LIBRARY_ROOT="+libRoot,
		"KURA_INBOX_ROOT="+inboxRoot,
		"KURA_LOG_LEVEL=ERROR",
		// E2E doesn't exercise the auth path; the subprocess is
		// already isolated inside the test process group, and
		// generating per-scenario tokens would force harness commands
		// to plumb the secret into every kura-e2e invocation.
		"KURA_DISABLE_TOKEN=1",
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	stderr := &syncBuffer{}
	cmd.Stderr = stderr
	cmd.Stdout = io.Discard

	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}

	b := &e2eBinary{
		t:         t,
		bin:       bin,
		libRoot:   libRoot,
		inboxRoot: inboxRoot,
		env:       append([]string(nil), extraEnv...),
		cmd:       cmd,
		stderr:    stderr,
	}
	t.Cleanup(b.stop)

	port, err := waitForPortFile(portFile, 5*time.Second)
	if err != nil {
		b.dumpStderr()
		t.Fatalf("daemon port file: %v", err)
	}
	b.port = port
	b.url = fmt.Sprintf("http://127.0.0.1:%d", port)

	if err := waitForHealth(b.url, 5*time.Second); err != nil {
		b.dumpStderr()
		t.Fatalf("daemon health: %v", err)
	}
	return b
}

// stop sends SIGTERM, waits 5s for clean exit, and SIGKILLs on
// timeout. Idempotent via sync.Once so test cleanup + explicit call
// are both safe.
func (b *e2eBinary) stop() {
	b.stopOnce.Do(func() {
		if b.cmd == nil || b.cmd.Process == nil {
			return
		}
		_ = b.cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- b.cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = b.cmd.Process.Kill()
			<-done
		}
	})
}

// run invokes the kura-e2e binary as a subprocess with
// KURA_SERVER_URL pointing at this daemon. Returns stdout, stderr,
// and the exit error. On non-zero exit, err is wrapped with the
// stderr tail so harness command callers can surface it via the
// script error path without losing diagnostic detail.
func (b *e2eBinary) run(ctx context.Context, args ...string) (stdout, stderr string, err error) {
	cmd := exec.CommandContext(ctx, b.bin, args...)
	cmd.Env = append(os.Environ(),
		"KURA_LIBRARY_ROOT="+b.libRoot,
		"KURA_SERVER_URL="+b.url,
		"KURA_DISABLE_TOKEN=1",
	)
	cmd.Env = append(cmd.Env, b.env...)
	var so, se bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	err = cmd.Run()
	stderr = se.String()
	if err != nil {
		err = fmt.Errorf("%w (stderr: %s)", err, strings.TrimSpace(stderr))
	}
	return so.String(), stderr, err
}

// runJSON runs the binary with the given args and decodes stdout
// into dst. Surfaces both decode and exit errors.
func (b *e2eBinary) runJSON(ctx context.Context, dst any, args ...string) error {
	stdout, stderr, err := b.run(ctx, args...)
	if err != nil {
		return fmt.Errorf("%s %s: %w (stderr: %s)", b.bin, strings.Join(args, " "), err, stderr)
	}
	if dst == nil {
		return nil
	}
	if err := json.Unmarshal([]byte(stdout), dst); err != nil {
		return fmt.Errorf("decode %s output: %w (stdout: %s)", strings.Join(args, " "), err, stdout)
	}
	return nil
}

// dumpStderr writes the daemon's captured stderr to the test log.
// Useful when the daemon failed to start and the test is about to
// fatal.
func (b *e2eBinary) dumpStderr() {
	if b.stderr == nil || b.stderr.Len() == 0 {
		return
	}
	b.t.Logf("daemon stderr:\n%s", b.stderr.String())
}

// waitForPortFile polls path every 20ms until the port file contains
// a valid decimal port or timeout elapses.
func waitForPortFile(path string, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		buf, err := os.ReadFile(path)
		if err == nil && len(buf) > 0 {
			p, perr := strconv.Atoi(strings.TrimSpace(string(buf)))
			if perr == nil && p > 0 && p < 65536 {
				return p, nil
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return 0, fmt.Errorf("timeout waiting for %s", path)
}

// waitForHealth polls /api/v1/health until 200 OK or timeout. Confirms
// the daemon is fully ready beyond just bound to the port.
func waitForHealth(baseURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/api/v1/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s/api/v1/health", baseURL)
}
