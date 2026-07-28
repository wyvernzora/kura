//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dockercontainer "github.com/moby/moby/api/types/container"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/testcontainers/testcontainers-go"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	n8nCustomPath = "/opt/n8n/custom"
	n8nStatePath  = "/home/node/.n8n"
	n8nFixtureDir = "/fixtures"
)

func testN8NActionWorkflow(t *testing.T) {
	stack := startProductStack(t)
	session := connectMCP(t, stack.gatewayURL)
	addSeries(t, session)
	waitQueueAvailable(t, stack.gatewayURL)

	runtime := stack.prepareN8N(t)
	stack.importN8NWorkflow(t, runtime, "action-workflow.json")
	output := stack.runN8NOneShot(t, "n8n-execute-kura-product-action", n8nImage,
		[]string{"execute", "--id=kura-product-action", "--rawOutput"},
		runtime.env, runtime.binds,
	)
	if strings.TrimSpace(output) == "" {
		t.Fatal("n8n workflow execution returned no output")
	}

	release := callTool(t, session, "get_release", map[string]any{"infohash": productInfohash})
	if stringField(t, release, "matchStatus") != "matched" ||
		stringField(t, release, "ref") != productRef {
		t.Fatalf("n8n action release result = %#v, want matched %s", release, productRef)
	}
	series := callTool(t, session, "get_series", map[string]any{"ref": productRef})
	if !containsString(series["tags"], "n8n:e2e") {
		t.Fatalf("n8n action series tags = %#v, want n8n:e2e", series["tags"])
	}
}

func testN8NTriggerWorkflow(t *testing.T) {
	stack := startProductStack(t)
	session := connectMCP(t, stack.gatewayURL)
	addSeries(t, session)
	waitQueueAvailable(t, stack.gatewayURL)

	runtime := stack.prepareN8N(t)
	stack.importN8NWorkflow(t, runtime, "trigger-workflow.json")
	stack.publishN8NWorkflow(t, runtime, "kura-product-trigger")
	stack.startN8N(t, runtime)

	release := waitN8NReleaseMatched(t, session)
	if stringField(t, release, "matchStatus") != "matched" ||
		stringField(t, release, "ref") != productRef {
		t.Fatalf("n8n trigger release result = %#v, want matched %s", release, productRef)
	}
	series := callTool(t, session, "get_series", map[string]any{"ref": productRef})
	if !containsString(series["tags"], "n8n:trigger-e2e") {
		t.Fatalf("n8n trigger series tags = %#v, want n8n:trigger-e2e", series["tags"])
	}
}

type n8nRuntime struct {
	env   map[string]string
	binds []string
}

func (s *productStack) prepareN8N(t *testing.T) n8nRuntime {
	t.Helper()
	customDir := writableDir(t, "n8n-custom")
	stateDir := writableDir(t, "n8n-state")
	fixtureDir := filepath.Join(s.repoRoot, "e2e", "testdata", "n8n")

	s.runN8NOneShot(t, "n8n-install-nodes", n8nNodesImage, nil,
		map[string]string{"KURA_NODES_TARGET": n8nCustomPath},
		[]string{customDir + ":" + n8nCustomPath},
	)

	runtime := n8nRuntime{
		binds: []string{
			customDir + ":" + n8nCustomPath + ":ro",
			stateDir + ":" + n8nStatePath,
			fixtureDir + ":" + n8nFixtureDir + ":ro",
		},
		env: map[string]string{
			"DB_TYPE":                               "sqlite",
			"N8N_CUSTOM_EXTENSIONS":                 n8nCustomPath,
			"N8N_DIAGNOSTICS_ENABLED":               "false",
			"N8N_ENCRYPTION_KEY":                    "kura-product-e2e-encryption-key",
			"N8N_ENFORCE_SETTINGS_FILE_PERMISSIONS": "false",
			"N8N_PERSONALIZATION_ENABLED":           "false",
			"N8N_RUNNERS_ENABLED":                   "false",
			"N8N_VERSION_NOTIFICATIONS_ENABLED":     "false",
		},
	}

	s.runN8NOneShot(t, "n8n-import-credentials", n8nImage,
		[]string{"import:credentials", "--input=" + n8nFixtureDir + "/credentials.json"},
		runtime.env, runtime.binds,
	)
	return runtime
}

func (s *productStack) importN8NWorkflow(
	t *testing.T,
	runtime n8nRuntime,
	fixture string,
) {
	t.Helper()
	command := []string{"import:workflow", "--input=" + n8nFixtureDir + "/" + fixture}
	s.runN8NOneShot(t, "n8n-import-workflow", n8nImage,
		command, runtime.env, runtime.binds,
	)
}

func (s *productStack) publishN8NWorkflow(
	t *testing.T,
	runtime n8nRuntime,
	workflowID string,
) {
	t.Helper()
	s.runN8NOneShot(t, "n8n-publish-workflow", n8nImage,
		[]string{"publish:workflow", "--id=" + workflowID},
		runtime.env, runtime.binds,
	)
}

func (s *productStack) startN8N(t *testing.T, runtime n8nRuntime) {
	t.Helper()
	container, err := testcontainers.Run(
		s.ctx,
		n8nImage,
		tcnetwork.WithNetwork([]string{"n8n"}, s.network),
		testcontainers.WithEnv(runtime.env),
		testcontainers.WithHostConfigModifier(func(hostConfig *dockercontainer.HostConfig) {
			hostConfig.Binds = append(hostConfig.Binds, runtime.binds...)
		}),
		testcontainers.WithCmd("start"),
		testcontainers.WithExposedPorts("5678/tcp"),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/healthz/readiness").
				WithPort("5678/tcp").
				WithStartupTimeout(2*time.Minute),
		),
	)
	if err != nil {
		t.Fatalf("start n8n server: %v", err)
	}
	registerContainer(t, s.ctx, "n8n", container)
}

func waitN8NReleaseMatched(t *testing.T, session *mcpsdk.ClientSession) map[string]any {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	var last map[string]any
	for time.Now().Before(deadline) {
		last = callTool(t, session, "get_release", map[string]any{"infohash": productInfohash})
		if last["matchStatus"] == "matched" {
			return last
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("n8n trigger did not match release within 45s; last result = %#v", last)
	return nil
}

func (s *productStack) runN8NOneShot(
	t *testing.T,
	name, image string,
	command []string,
	env map[string]string,
	binds []string,
) string {
	t.Helper()
	options := []testcontainers.ContainerCustomizer{
		tcnetwork.WithNetwork([]string{name}, s.network),
		testcontainers.WithEnv(env),
		testcontainers.WithHostConfigModifier(func(hostConfig *dockercontainer.HostConfig) {
			hostConfig.Binds = append(hostConfig.Binds, binds...)
		}),
		testcontainers.WithWaitStrategy(wait.ForExit().WithExitTimeout(3 * time.Minute)),
	}
	if command != nil {
		options = append(options, testcontainers.WithCmd(command...))
	}
	container, err := testcontainers.Run(s.ctx, image, options...)
	if err != nil {
		t.Fatalf("start %s: %v", name, err)
	}
	registerContainer(t, s.ctx, name, container)

	state, err := container.State(s.ctx)
	if err != nil {
		t.Fatalf("read %s state: %v", name, err)
	}
	output := containerLogs(t, s.ctx, container)
	if state.ExitCode != 0 {
		t.Fatalf("%s exited %d:\n%s", name, state.ExitCode, output)
	}
	return output
}

func containerLogs(
	t *testing.T,
	ctx context.Context,
	container testcontainers.Container,
) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	logs, err := container.Logs(ctx)
	if err != nil {
		t.Fatalf("read container logs: %v", err)
	}
	defer logs.Close()
	raw, err := io.ReadAll(logs)
	if err != nil {
		t.Fatalf("read container log body: %v", err)
	}
	return string(raw)
}

func containsString(value any, want string) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if fmt.Sprint(item) == want {
			return true
		}
	}
	return false
}
