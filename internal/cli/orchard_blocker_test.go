package cli

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestGenerateOpenClawJSONUsesCurrentSchemaShape(t *testing.T) {
	cfg := &OrchardConfig{
		Name: "schema-smoke",
		Agent: AgentConfig{
			Model: "openrouter/anthropic/claude-sonnet-4.6",
		},
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(generateOpenClawJSON(cfg, "tree-0", "/work/ciderbox/repo")), &got); err != nil {
		t.Fatalf("generated openclaw.json is not valid JSON: %v", err)
	}

	if _, ok := got["model"]; ok {
		t.Fatalf("generated config still uses old top-level model object")
	}
	if _, ok := got["security"]; ok {
		t.Fatalf("generated config still uses old top-level security object")
	}
	if _, ok := got["orchid"]; ok {
		t.Fatalf("generated config includes unsupported top-level orchid metadata")
	}

	tools, ok := got["tools"].(map[string]any)
	if !ok {
		t.Fatalf("generated config missing tools object")
	}
	for _, oldKey := range []string{"shell", "filesystem", "browser"} {
		if _, ok := tools[oldKey]; ok {
			t.Fatalf("generated config still uses old tools.%s shape", oldKey)
		}
	}

	agents, ok := got["agents"].(map[string]any)
	if !ok {
		t.Fatalf("generated config missing agents object")
	}
	defaults, ok := agents["defaults"].(map[string]any)
	if !ok {
		t.Fatalf("generated config missing agents.defaults object")
	}
	model, ok := defaults["model"].(map[string]any)
	if !ok {
		t.Fatalf("generated config missing agents.defaults.model object")
	}
	if model["primary"] != "openrouter/anthropic/claude-sonnet-4.6" {
		t.Fatalf("primary model = %v", model["primary"])
	}
	if defaults["workspace"] != "/work/ciderbox/repo" {
		t.Fatalf("workspace = %v", defaults["workspace"])
	}
}

func TestPlantTreeRunsContainerAsRoot(t *testing.T) {
	runtime := &captureContainerRuntime{}
	cfg := Config{}
	cfg.AppleContainer.User = "crabbox"
	orchard := &OrchardConfig{
		Name: "plant-smoke",
		Template: TreeTemplate{
			Image:  "debian:bookworm",
			CPUs:   2,
			Memory: "2g",
		},
	}

	app := App{Stdout: io.Discard, Stderr: io.Discard}
	state := app.plantTree(context.Background(), runtime, cfg, orchard, 0)
	if state.Status != "ready" {
		t.Fatalf("tree status = %q", state.Status)
	}
	if runtime.spec.User != "root" {
		t.Fatalf("container user = %q, want root", runtime.spec.User)
	}
}

func TestDefaultOrchardAgentCommandUsesSupportedOpenClawCLI(t *testing.T) {
	if strings.Contains(defaultOrchardAgentCommand, "openclaw run") {
		t.Fatalf("default orchard command uses removed OpenClaw run command: %s", defaultOrchardAgentCommand)
	}
	for _, want := range []string{"cd \"${ORCHARD_WORKSPACE:-/root/.openclaw/workspace}\"", "openclaw", "agent", "--local", "--agent main", "--message \"$ORCHARD_TASK\""} {
		if !strings.Contains(defaultOrchardAgentCommand, want) {
			t.Fatalf("default orchard command missing %q: %s", want, defaultOrchardAgentCommand)
		}
	}
}

func TestOrchardRunTaskTextAcceptsFlagOrPositionalTask(t *testing.T) {
	tests := []struct {
		name       string
		flagTask   string
		positional []string
		want       string
	}{
		{name: "flag wins", flagTask: " review repo ", positional: []string{"ignored"}, want: "review repo"},
		{name: "positional", positional: []string{"review", "this", "repo"}, want: "review this repo"},
		{name: "blank", flagTask: " ", positional: []string{" "}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := orchardRunTaskText(tt.flagTask, tt.positional); got != tt.want {
				t.Fatalf("task text = %q, want %q", got, tt.want)
			}
		})
	}
}

type captureContainerRuntime struct {
	spec ContainerSpec
}

func (r *captureContainerRuntime) Run(_ context.Context, spec ContainerSpec) (ContainerInfo, error) {
	r.spec = spec
	return ContainerInfo{ID: "cid", Name: spec.Name, IP: "127.0.0.1"}, nil
}

func (r *captureContainerRuntime) List(context.Context, map[string]string) ([]ContainerInfo, error) {
	return nil, nil
}

func (r *captureContainerRuntime) Inspect(context.Context, string) (ContainerInfo, error) {
	return ContainerInfo{}, nil
}

func (r *captureContainerRuntime) Exec(context.Context, string, []string, io.Writer, io.Writer) error {
	return nil
}

func (r *captureContainerRuntime) Copy(context.Context, string, string) error {
	return nil
}

func (r *captureContainerRuntime) Stop(context.Context, string) error {
	return nil
}

func (r *captureContainerRuntime) Remove(context.Context, string, bool) error {
	return nil
}

func (r *captureContainerRuntime) Logs(context.Context, string, int) (string, error) {
	return "", nil
}
