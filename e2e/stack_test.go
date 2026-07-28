//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dockercontainer "github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	containerPort  = "8080/tcp"
	testVersion    = "product-e2e"
	upstreamsImage = "kura-product-e2e-upstreams:latest"
	libraryImage   = "kura-product-e2e-library:latest"
	releaseImage   = "kura-product-e2e-release:latest"
	gatewayImage   = "kura-product-e2e-gateway:latest"
	n8nNodesImage  = "kura-product-e2e-n8n-nodes:latest"
	n8nImage       = "n8nio/n8n:2.28.3"
)

type productStack struct {
	ctx         context.Context
	repoRoot    string
	libraryRoot string
	inboxRoot   string
	gatewayURL  string
	network     *testcontainers.DockerNetwork

	library testcontainers.Container
	release testcontainers.Container
	gateway testcontainers.Container
}

func startProductStack(t *testing.T) *productStack {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Minute)
	t.Cleanup(cancel)
	root := repositoryRoot(t)
	libraryRoot := writableDir(t, "library")
	inboxRoot := writableDir(t, "inbox")

	nw, err := tcnetwork.New(ctx)
	if err != nil {
		t.Fatalf("create product network: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := nw.Remove(cleanupCtx); err != nil {
			t.Logf("remove product network: %v", err)
		}
	})

	startPostgres(t, ctx, nw)
	startUpstreams(t, ctx, nw)
	library := startLibrary(t, ctx, nw, libraryRoot, inboxRoot)
	release := startRelease(t, ctx, nw)
	gateway, gatewayURL := startGateway(t, ctx, nw)

	stack := &productStack{
		ctx:         ctx,
		repoRoot:    root,
		libraryRoot: libraryRoot,
		inboxRoot:   inboxRoot,
		gatewayURL:  gatewayURL,
		network:     nw,
		library:     library,
		release:     release,
		gateway:     gateway,
	}
	stack.waitSystemStatus(t, http.StatusOK, "ok", 30*time.Second)
	return stack
}

func buildProductImages(t *testing.T) {
	t.Helper()
	root := repositoryRoot(t)
	builds := []struct {
		name string
		args []string
	}{
		{
			name: "fake external upstreams",
			args: []string{"buildx", "build", "--load",
				"--tag", upstreamsImage,
				"--file", filepath.Join(root, "e2e", "testdata", "upstreams.Dockerfile"),
				filepath.Join(root, "e2e"),
			},
		},
		{
			name: "library-manager",
			args: []string{"buildx", "build", "--load",
				"--build-arg", "VERSION=" + testVersion,
				"--tag", libraryImage,
				filepath.Join(root, "services", "library-manager"),
			},
		},
		{
			name: "release-indexer",
			args: []string{"buildx", "build", "--load",
				"--build-arg", "VERSION=" + testVersion,
				"--build-arg", "COMMIT=product-e2e",
				"--tag", releaseImage,
				filepath.Join(root, "services", "release-indexer"),
			},
		},
		{
			name: "gateway",
			args: []string{"buildx", "build", "--load",
				"--build-arg", "VERSION=" + testVersion,
				"--tag", gatewayImage,
				filepath.Join(root, "services", "gateway"),
			},
		},
		{
			name: "n8n nodes",
			args: []string{"buildx", "build", "--load",
				"--build-arg", "VERSION=" + testVersion,
				"--tag", n8nNodesImage,
				"--file", filepath.Join(root, "integrations", "n8n", "Dockerfile"),
				root,
			},
		},
	}
	for _, build := range builds {
		cmd := exec.CommandContext(t.Context(), "docker", build.args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("build production %s image: %v", build.name, err)
		}
	}
}

func startPostgres(
	t *testing.T,
	ctx context.Context,
	nw *testcontainers.DockerNetwork,
) {
	t.Helper()
	pg, err := tcpostgres.Run(ctx,
		"postgres:18-alpine",
		tcpostgres.WithDatabase("kura"),
		tcpostgres.WithUsername("kura"),
		tcpostgres.WithPassword("kura"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(2*time.Minute),
		),
		tcnetwork.WithNetwork([]string{"postgres"}, nw),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL 18: %v", err)
	}
	registerContainer(t, ctx, "postgres", pg)
}

func startUpstreams(
	t *testing.T,
	ctx context.Context,
	nw *testcontainers.DockerNetwork,
) testcontainers.Container {
	t.Helper()
	container, err := testcontainers.Run(ctx, upstreamsImage,
		tcnetwork.WithNetwork([]string{"upstreams"}, nw),
		testcontainers.WithExposedPorts(containerPort),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/healthz").
				WithPort(containerPort).
				WithStartupTimeout(2*time.Minute),
		),
	)
	if err != nil {
		t.Fatalf("start fake external upstreams: %v", err)
	}
	registerContainer(t, ctx, "upstreams", container)
	return container
}

func startLibrary(
	t *testing.T,
	ctx context.Context,
	nw *testcontainers.DockerNetwork,
	libraryRoot, inboxRoot string,
) testcontainers.Container {
	t.Helper()
	config := `[server]
rest = ":8080"
log_level = "debug"
shutdown_timeout = "5s"

[library]
root = "/library"
inbox = "/inbox"
airing_tail_days = 7

[metadata]
preferred_languages = []
mediainfo_command = "mediainfo"
tvdb_url = "http://upstreams:8080"

[jobs]
timeout = "30s"
retention = "30m"
reaper_interval = "5m"

[index]
probe_interval = "100ms"
rebuild_interval = "1h"
library_root_debounce = "100ms"

[sweep]
interval = "1h"
log_retention_days = 7

[coordination]
conflict_retries = 1
`
	container, err := testcontainers.Run(ctx, libraryImage,
		tcnetwork.WithNetwork([]string{"library"}, nw),
		testcontainers.WithEnv(map[string]string{
			"KURA_TVDB_KEY": "product-e2e-key",
			"KURA_HOST_ID":  "product-e2e-library",
		}),
		testcontainers.WithFiles(testcontainers.ContainerFile{
			Reader:            strings.NewReader(config),
			ContainerFilePath: "/etc/kura/library-manager.toml",
			FileMode:          0o644,
		}),
		testcontainers.WithHostConfigModifier(func(hostConfig *dockercontainer.HostConfig) {
			hostConfig.Binds = append(hostConfig.Binds,
				libraryRoot+":/library",
				inboxRoot+":/inbox",
			)
		}),
		testcontainers.WithExposedPorts(containerPort),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/healthz").
				WithPort(containerPort).
				WithStartupTimeout(3*time.Minute),
		),
	)
	if err != nil {
		t.Fatalf("start production library-manager: %v", err)
	}
	registerContainer(t, ctx, "library-manager", container)
	return container
}

func startRelease(
	t *testing.T,
	ctx context.Context,
	nw *testcontainers.DockerNetwork,
) testcontainers.Container {
	t.Helper()
	config := `[database]
schema = "releases"

[server]
addr = ":8080"
metrics_addr = ":9090"
log_level = "debug"

[queue]
max_attempts = 3

[sources.dmhy]
enabled = true
interval = "1h"
timeout = "30s"
url = "http://upstreams:8080"
category = "2"
max_rps = 0
cache_ttl = "0s"
`
	container, err := testcontainers.Run(ctx, releaseImage,
		tcnetwork.WithNetwork([]string{"releases"}, nw),
		testcontainers.WithEnv(map[string]string{
			"KURA_RELEASES_DATABASE_URL": "postgres://kura:kura@postgres:5432/kura?sslmode=disable",
		}),
		testcontainers.WithFiles(testcontainers.ContainerFile{
			Reader:            strings.NewReader(config),
			ContainerFilePath: "/etc/kura/release-indexer.toml",
			FileMode:          0o644,
		}),
		testcontainers.WithExposedPorts(containerPort),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/healthz").
				WithPort(containerPort).
				WithStartupTimeout(3*time.Minute),
		),
	)
	if err != nil {
		t.Fatalf("start production release-indexer: %v", err)
	}
	registerContainer(t, ctx, "release-indexer", container)
	return container
}

func startGateway(
	t *testing.T,
	ctx context.Context,
	nw *testcontainers.DockerNetwork,
) (container testcontainers.Container, endpoint string) {
	t.Helper()
	var err error
	container, err = testcontainers.Run(ctx, gatewayImage,
		tcnetwork.WithNetwork([]string{"gateway"}, nw),
		testcontainers.WithEnv(map[string]string{
			"KURA_LIBRARY_UPSTREAM":  "http://library:8080",
			"KURA_RELEASES_UPSTREAM": "http://releases:8080",
		}),
		testcontainers.WithExposedPorts(containerPort),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/healthz").
				WithPort(containerPort).
				WithStartupTimeout(4*time.Minute),
		),
	)
	if err != nil {
		t.Fatalf("start production gateway: %v", err)
	}
	registerContainer(t, ctx, "gateway", container)
	endpoint, err = container.PortEndpoint(ctx, containerPort, "http")
	if err != nil {
		t.Fatalf("resolve gateway endpoint: %v", err)
	}
	return container, endpoint
}

func registerContainer(
	t *testing.T,
	ctx context.Context,
	name string,
	container testcontainers.Container,
) {
	t.Helper()
	t.Cleanup(func() {
		if t.Failed() {
			writeContainerLogs(t, context.WithoutCancel(ctx), name, container)
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
		defer cancel()
		if err := container.Terminate(cleanupCtx); err != nil {
			t.Logf("terminate %s: %v", name, err)
		}
	})
}

func writeContainerLogs(
	t *testing.T,
	ctx context.Context,
	name string,
	container testcontainers.Container,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	logs, err := container.Logs(ctx)
	if err != nil {
		t.Logf("read %s logs: %v", name, err)
		return
	}
	defer logs.Close()
	out, err := os.Create(filepath.Join(t.ArtifactDir(), name+".log"))
	if err != nil {
		t.Logf("create %s log artifact: %v", name, err)
		return
	}
	defer out.Close()
	if _, err := io.Copy(out, logs); err != nil {
		t.Logf("write %s log artifact: %v", name, err)
	}
}

func writableDir(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatalf("chmod %s: %v", name, err)
	}
	return dir
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	root := filepath.Clean(filepath.Join(wd, ".."))
	if _, err := os.Stat(filepath.Join(root, "go.work")); err != nil {
		t.Fatalf("resolve repository root from %s: %v", wd, err)
	}
	return root
}

func (s *productStack) waitSystemStatus(
	t *testing.T,
	wantHTTP int,
	wantStatus string,
	timeout time.Duration,
) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(s.ctx, http.MethodGet, s.gatewayURL+"/api/v1/health", http.NoBody)
		if err != nil {
			t.Fatalf("build system-health request: %v", err)
		}
		resp, err := productHTTPClient.Do(req)
		if err == nil {
			raw, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			last = fmt.Sprintf("status=%d body=%s", resp.StatusCode, raw)
			if readErr == nil && resp.StatusCode == wantHTTP &&
				strings.Contains(string(raw), `"status":"`+wantStatus+`"`) {
				return
			}
		} else {
			last = err.Error()
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("system health did not reach HTTP %d status %q: %s", wantHTTP, wantStatus, last)
}

func (s *productStack) refreshGatewayURL(t *testing.T) {
	t.Helper()
	endpoint, err := s.gateway.PortEndpoint(s.ctx, containerPort, "http")
	if err != nil {
		t.Fatalf("refresh gateway endpoint: %v", err)
	}
	s.gatewayURL = endpoint
}

var productHTTPClient = &http.Client{Timeout: 5 * time.Second}
