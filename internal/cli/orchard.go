package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// OrchardConfig defines a swarm of AI agent trees.
type OrchardConfig struct {
	Name      string          `yaml:"name"`
	Trees     int             `yaml:"trees"`
	Template  TreeTemplate    `yaml:"template"`
	Agent     AgentConfig     `yaml:"agent"`
	Secrets   SecretsConfig   `yaml:"secrets,omitempty"`
	Workspace WorkspaceConfig `yaml:"workspace,omitempty"`
}

type SecretsConfig struct {
	EnvFile     string   `yaml:"envFile,omitempty"`
	PassThrough []string `yaml:"passThrough,omitempty"`
	Required    []string `yaml:"required,omitempty"`
}

type WorkspaceConfig struct {
	Sync bool   `yaml:"sync,omitempty"`
	Path string `yaml:"path,omitempty"`
}

// TreeTemplate defines the container spec for each tree.
type TreeTemplate struct {
	Image  string `yaml:"image"`
	CPUs   int    `yaml:"cpus"`
	Memory string `yaml:"memory"`
	Distro string `yaml:"distro"`
}

// AgentConfig defines the OpenClaw agent to graft onto each tree.
type AgentConfig struct {
	Identity       string   `yaml:"identity"`
	Skills         []string `yaml:"skills"`
	MemoryProvider string   `yaml:"memory_provider,omitempty"`
	Model          string   `yaml:"model"`
	Command        string   `yaml:"command,omitempty"`
}

// TreeState tracks a running tree in the orchard.
type TreeState struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	IP          string    `json:"ip"`
	LeaseID     string    `json:"lease_id"`
	ContainerID string    `json:"container_id"`
	CreatedAt   time.Time `json:"created_at"`
}

// OrchardState is the persisted record of a planted orchard.
type OrchardState struct {
	Name      string      `json:"name"`
	PlantedAt time.Time   `json:"planted_at"`
	Trees     []TreeState `json:"trees"`
}

const orchardDefaultFile = ".orchard.yaml"

// orchardResultPath is where trees write their work output for harvest.
const orchardResultPath = "/tmp/orchard-result.json"

// ContainerRuntimeBackend is implemented by providers that can expose a native
// container lifecycle runtime. This keeps cli from importing applecontainer
// directly, which would create an import cycle.
type ContainerRuntimeBackend interface {
	ContainerRuntime() (ContainerRuntime, error)
}

// orchardCommand is the entry point for orchard swarm management.
func (a App) orchardCommand(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return a.orchardHelp()
	}
	if args[0] == "--help" || args[0] == "help" {
		return a.orchardHelp()
	}

	switch args[0] {
	case "init":
		return a.orchardInit(ctx, args[1:])
	case "plant":
		return a.orchardPlant(ctx, args[1:])
	case "tend":
		return a.orchardTend(ctx, args[1:])
	case "harvest":
		return a.orchardHarvest(ctx, args[1:])
	case "press":
		return a.orchardPress(ctx, args[1:])
	case "graft":
		return a.orchardGraft(ctx, args[1:])
	case "run":
		return a.orchardRun(ctx, args[1:])
	case "secrets":
		return a.orchardSecretsCommand(ctx, args[1:])
	case "login":
		return a.orchardLogin(ctx, args[1:])
	case "doctor":
		return a.orchardDoctor(ctx, args[1:])
	case "logs":
		return a.orchardLogs(ctx, args[1:])
	case "chop":
		return a.orchardChop(ctx, args[1:])
	case "list", "ls":
		return a.orchardList(ctx, args[1:])
	default:
		return exit(2, "unknown orchard subcommand: %q", args[0])
	}
}

func (a App) orchardHelp() error {
	fmt.Fprintln(a.Stdout, `Orchard — AI agent swarm management

Subcommands:
  init       Scaffold .orchard.yaml
  plant      Spin up N trees from manifest
  tend       Show swarm status / health from live container state
  graft      Install the OpenClaw agent runtime on a tree (--all, --upgrade)
  run        Execute a task across one tree or the whole orchard (--sync)
  secrets    Manage secrets (.orchid.env): init, check, push
  login      Configure provider authentication
  harvest    Collect `+orchardResultPath+` from every tree
  press      Aggregate harvested outputs into one report
  doctor     Check host/runtime/tree readiness
  logs       Show logs for a tree
  chop       Tear down the entire orchard
  list       Show active trees across all orchards

Examples:
  ciderbox orchard init
  ciderbox orchard plant --config .orchard.yaml
  ciderbox orchard graft --tree tree-0
  ciderbox orchard run --task "review this repo and write JSON findings"
  ciderbox orchard harvest --output results.json
  ciderbox orchard press --input results.json
  ciderbox orchard doctor
  ciderbox orchard logs --tree tree-0 --tail 200
  ciderbox orchard chop --yes`)
	return nil
}

// orchardInit scaffolds a default .orchard.yaml.
func (a App) orchardInit(ctx context.Context, args []string) error {
	fs := newFlagSet("orchard init", a.Stderr)
	force := fs.Bool("force", false, "overwrite existing .orchard.yaml")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	path := filepath.Join(".", orchardDefaultFile)
	if _, err := os.Stat(path); err == nil && !*force {
		return exit(2, "%s already exists; use --force to overwrite", path)
	}

	config := OrchardConfig{
		Name:  "my-orchard",
		Trees: 3,
		Template: TreeTemplate{
			Image:  "ubuntu:26.04",
			CPUs:   2,
			Memory: "4G",
			Distro: "ubuntu",
		},
		Agent: AgentConfig{
			Identity: "tree-agent",
			Skills:   []string{"github", "web-search"},
			Model:    "CHANGE_ME",
			Command:  `openclaw run "$ORCHARD_TASK"`,
		},
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return exit(2, "marshal orchard config: %v", err)
	}

	header := `# Orchard — AI agent swarm manifest
# Each tree is an isolated Apple Container VM.
# Run "ciderbox orchard graft" after "ciderbox orchard plant" to install
# the OpenClaw runtime on each tree.
#
# agent.model is a placeholder. Set it to a real provider/model string
# before grafting/running.
#
# agent.command is executed inside each tree by "orchard run".
# The task is available as $ORCHARD_TASK. Command stdout is written to
# /tmp/orchard-result.json and later collected by "orchard harvest".
#
# Quick start:
#   ciderbox orchard plant
#   ciderbox orchard graft --tree tree-0
#   ciderbox orchard run --task "inspect this tree"
#   ciderbox orchard harvest --output results.json
`
	if err := os.WriteFile(path, []byte(header+string(data)), 0644); err != nil {
		return exit(2, "write %s: %v", path, err)
	}

	fmt.Fprintf(a.Stdout, "Created %s\n\n", path)
	fmt.Fprintln(a.Stdout, "Next steps:")
	fmt.Fprintln(a.Stdout, "  1. Edit the manifest with your tree specs and agent model")
	fmt.Fprintln(a.Stdout, "  2. Run `ciderbox orchard plant` to spin up the swarm")
	fmt.Fprintln(a.Stdout, "  3. Run `ciderbox orchard graft --tree tree-0` to install OpenClaw")
	fmt.Fprintln(a.Stdout, "  4. Run `ciderbox orchard run --task \"...\"` to execute work")
	return nil
}

// orchardRuntime returns the native container runtime from the apple-container
// provider. It intentionally avoids SSHLeaseBackend.
func (a App) orchardRuntime() (ContainerRuntime, Config, error) {
	cfg, hostRuntime, err := a.providerConfigRuntime("apple-container")
	if err != nil {
		return nil, Config{}, err
	}

	provider, err := ProviderFor("apple-container")
	if err != nil {
		return nil, Config{}, err
	}

	backend, err := provider.Configure(cfg, hostRuntime)
	if err != nil {
		return nil, Config{}, err
	}

	runtimeBackend, ok := backend.(ContainerRuntimeBackend)
	if !ok {
		return nil, Config{}, fmt.Errorf("apple-container provider does not expose native ContainerRuntime")
	}

	containerRuntime, err := runtimeBackend.ContainerRuntime()
	if err != nil {
		return nil, Config{}, err
	}

	return containerRuntime, cfg, nil
}

// orchardContainers lists live containers for one orchard. Empty orchardName
// means all orchard containers.
func (a App) orchardContainers(ctx context.Context, orchardName string) ([]ContainerInfo, Config, ContainerRuntime, error) {
	containerRuntime, cfg, err := a.orchardRuntime()
	if err != nil {
		return nil, Config{}, nil, err
	}

	filters := map[string]string{"orchard": "true"}
	if orchardName != "" {
		filters["orchard.name"] = orchardName
	}

	containers, err := containerRuntime.List(ctx, filters)
	if err != nil {
		return nil, Config{}, nil, err
	}

	sort.Slice(containers, func(i, j int) bool {
		leftOrchard := containers[i].Labels["orchard.name"]
		rightOrchard := containers[j].Labels["orchard.name"]
		if leftOrchard != rightOrchard {
			return leftOrchard < rightOrchard
		}
		return containers[i].Labels["tree.id"] < containers[j].Labels["tree.id"]
	})

	return containers, cfg, containerRuntime, nil
}

// orchardStatePath returns the persisted state file for an orchard.
func orchardStatePath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ciderbox", "orchards", name, "state.json"), nil
}

func writeOrchardState(state OrchardState) error {
	path, err := orchardStatePath(state.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func removeOrchardState(name string) error {
	path, err := orchardStatePath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// orchardPlant spins up N trees from the manifest.
func (a App) orchardPlant(ctx context.Context, args []string) error {
	fs := newFlagSet("orchard plant", a.Stderr)
	configFile := fs.String("config", orchardDefaultFile, "path to orchard manifest")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	config, err := readOrchardConfig(*configFile)
	if err != nil {
		return exit(2, "orchard config: %v", err)
	}

	fmt.Fprintf(a.Stdout, "=== Orchard Plant ===\n")
	fmt.Fprintf(a.Stdout, "Orchard: %s\n", config.Name)
	fmt.Fprintf(a.Stdout, "Trees: %d\n", config.Trees)
	fmt.Fprintf(a.Stdout, "Image: %s (%d CPUs, %s RAM)\n\n", config.Template.Image, config.Template.CPUs, config.Template.Memory)

	containerRuntime, cfg, err := a.orchardRuntime()
	if err != nil {
		return exit(2, "orchard runtime: %v", err)
	}

	var wg sync.WaitGroup
	trees := make([]TreeState, config.Trees)

	for i := 0; i < config.Trees; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			trees[idx] = a.plantTree(ctx, containerRuntime, cfg, config, idx)
		}(i)
	}
	wg.Wait()

	ready := 0
	failed := 0
	for _, tree := range trees {
		if tree.Status == "ready" {
			ready++
		} else {
			failed++
		}
	}

	state := OrchardState{
		Name:      config.Name,
		PlantedAt: time.Now().UTC(),
		Trees:     trees,
	}
	if err := writeOrchardState(state); err != nil {
		fmt.Fprintf(a.Stderr, "WARN: persist orchard state: %v\n", err)
	} else if path, perr := orchardStatePath(config.Name); perr == nil {
		fmt.Fprintf(a.Stdout, "State: %s\n", path)
	}

	fmt.Fprintf(a.Stdout, "Planted %d/%d trees.\n", ready, config.Trees)
	if failed > 0 {
		fmt.Fprintf(a.Stdout, "%d tree(s) failed to grow.\n", failed)
		return exit(1, "%d tree(s) failed", failed)
	}

	return nil
}

// plantTree provisions a single tree via the native Apple ContainerRuntime.
func (a App) plantTree(ctx context.Context, containerRuntime ContainerRuntime, cfg Config, config *OrchardConfig, idx int) TreeState {
	treeID := fmt.Sprintf("tree-%d", idx)
	treeName := fmt.Sprintf("%s-%s", config.Name, treeID)
	now := time.Now().UTC()
	leaseID := fmt.Sprintf("orchard-%s-%s-%d", config.Name, treeID, now.UnixNano())

	tree := TreeState{
		ID:        treeID,
		Name:      treeName,
		Status:    "planting",
		LeaseID:   leaseID,
		CreatedAt: now,
	}

	fmt.Fprintf(a.Stderr, "[%s] planting...\n", treeName)

	labels := map[string]string{
		"ciderbox":     "true",
		"provider":     "apple-container",
		"lease":        leaseID,
		"orchard":      "true",
		"orchard.name": config.Name,
		"tree.id":      treeID,
		"tree.name":    treeName,
		"created_at":   now.Format(time.RFC3339),
		"image":        config.Template.Image,
	}

	spec := ContainerSpec{
		Name:      treeName,
		Image:     config.Template.Image,
		CPUs:      config.Template.CPUs,
		Memory:    config.Template.Memory,
		User:      cfg.AppleContainer.User,
		Labels:    labels,
		ExtraArgs: append([]string(nil), cfg.AppleContainer.ExtraRunArgs...),
		Command:   []string{"sleep", "infinity"},
	}

	info, err := containerRuntime.Run(ctx, spec)
	if err != nil {
		fmt.Fprintf(a.Stderr, "[%s] run failed: %v\n", treeName, err)
		tree.Status = "wilted"
		return tree
	}

	tree.ContainerID = info.ID
	tree.IP = info.IP
	tree.Status = "ready"

	fmt.Fprintf(a.Stderr, "[%s] ready (lease=%s, container=%s, ip=%s)\n", treeName, leaseID, tree.ContainerID, blank(tree.IP, "-"))
	return tree
}

func containerAge(info ContainerInfo, now time.Time) string {
	if !info.StartedAt.IsZero() {
		return now.Sub(info.StartedAt).Truncate(time.Second).String()
	}
	createdAt, err := time.Parse(time.RFC3339, info.Labels["created_at"])
	if err != nil || createdAt.IsZero() {
		return "-"
	}
	return now.Sub(createdAt).Truncate(time.Second).String()
}

// orchardTend shows live swarm status derived from container labels.
func (a App) orchardTend(ctx context.Context, args []string) error {
	fs := newFlagSet("orchard tend", a.Stderr)
	configFile := fs.String("config", orchardDefaultFile, "path to orchard manifest")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	config, err := readOrchardConfig(*configFile)
	if err != nil {
		return exit(2, "orchard config: %v", err)
	}

	trees, _, _, err := a.orchardContainers(ctx, config.Name)
	if err != nil {
		return exit(2, "orchard tend: %v", err)
	}

	fmt.Fprintf(a.Stdout, "=== Orchard Tend ===\n")
	fmt.Fprintf(a.Stdout, "Orchard: %s\n", config.Name)
	fmt.Fprintf(a.Stdout, "Expected trees: %d | Running: %d\n\n", config.Trees, len(trees))

	if len(trees) == 0 {
		fmt.Fprintln(a.Stdout, "No trees running — plant an orchard first.")
		return nil
	}

	now := time.Now().UTC()
	fmt.Fprintf(a.Stdout, "%-12s %-10s %-16s %s\n", "TREE", "STATUS", "IP", "AGE")
	fmt.Fprintln(a.Stdout, strings.Repeat("-", 52))

	for _, tree := range trees {
		treeID := blank(tree.Labels["tree.id"], tree.Name)
		fmt.Fprintf(a.Stdout, "%-12s %-10s %-16s %s\n", treeID, blank(tree.Status, "unknown"), blank(tree.IP, "-"), containerAge(tree, now))
	}

	if len(trees) < config.Trees {
		fmt.Fprintf(a.Stdout, "\nWARNING: %d tree(s) missing.\n", config.Trees-len(trees))
	}

	return nil
}

func (a App) treeExec(ctx context.Context, containerRuntime ContainerRuntime, containerID string, command []string) error {
	return containerRuntime.Exec(ctx, containerID, command, a.Stdout, a.Stderr)
}

func (a App) treeExecCapture(ctx context.Context, containerRuntime ContainerRuntime, containerID string, command []string) (string, error) {
	var out bytes.Buffer
	var stderr bytes.Buffer
	if err := containerRuntime.Exec(ctx, containerID, command, &out, &stderr); err != nil {
		return "", fmt.Errorf("container exec: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), nil
}

// HarvestResult is one tree's collected output.
type HarvestResult struct {
	Tree   string          `json:"tree"`
	Status string          `json:"status"`
	Error  string          `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

// orchardHarvest collects /tmp/orchard-result.json from every live tree.
func (a App) orchardHarvest(ctx context.Context, args []string) error {
	fs := newFlagSet("orchard harvest", a.Stderr)
	configFile := fs.String("config", orchardDefaultFile, "path to orchard manifest")
	outputFile := fs.String("output", "", "write JSON results to file")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	config, err := readOrchardConfig(*configFile)
	if err != nil {
		return exit(2, "orchard config: %v", err)
	}

	trees, _, containerRuntime, err := a.orchardContainers(ctx, config.Name)
	if err != nil {
		return exit(2, "orchard harvest: %v", err)
	}

	fmt.Fprintf(a.Stdout, "=== Orchard Harvest ===\n")
	fmt.Fprintf(a.Stdout, "Orchard: %s\n", config.Name)
	fmt.Fprintf(a.Stdout, "Collecting %s from %d tree(s)...\n\n", orchardResultPath, len(trees))

	if len(trees) == 0 {
		fmt.Fprintln(a.Stdout, "No trees running — nothing to harvest.")
		return nil
	}

	results := make([]HarvestResult, 0, len(trees))
	for _, tree := range trees {
		name := blank(tree.Labels["tree.id"], tree.Name)
		containerID := tree.ID
		out, execErr := a.treeExecCapture(ctx, containerRuntime, containerID, []string{"cat", orchardResultPath})

		hr := HarvestResult{Tree: name}
		switch {
		case execErr != nil:
			hr.Status = "missing"
			hr.Error = execErr.Error()
			fmt.Fprintf(a.Stdout, "[%s] no result (%s not readable)\n", name, orchardResultPath)
		case json.Valid([]byte(out)):
			hr.Status = "ok"
			hr.Result = json.RawMessage(out)
			fmt.Fprintf(a.Stdout, "[%s] harvested %d bytes\n", name, len(out))
		default:
			quoted, _ := json.Marshal(out)
			hr.Status = "ok"
			hr.Result = quoted
			fmt.Fprintf(a.Stdout, "[%s] harvested %d bytes (non-JSON, stored as string)\n", name, len(out))
		}
		results = append(results, hr)
	}

	if *outputFile != "" {
		data, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			return exit(2, "marshal harvest results: %v", err)
		}
		if err := os.WriteFile(*outputFile, data, 0644); err != nil {
			return exit(2, "write %s: %v", *outputFile, err)
		}
		fmt.Fprintf(a.Stdout, "\nWrote results to %s\n", *outputFile)
	}

	return nil
}

// orchardPress aggregates harvested tree outputs into a summary.
func (a App) orchardPress(ctx context.Context, args []string) error {
	fs := newFlagSet("orchard press", a.Stderr)
	inputFile := fs.String("input", "", "harvest JSON file to aggregate (from `orchard harvest --output`)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *inputFile == "" {
		return exit(2, "press requires --input ; run `orchard harvest --output harvest.json` first")
	}

	data, err := os.ReadFile(*inputFile)
	if err != nil {
		return exit(2, "read %s: %v", *inputFile, err)
	}

	var results []HarvestResult
	if err := json.Unmarshal(data, &results); err != nil {
		return exit(2, "parse %s: %v", *inputFile, err)
	}

	fmt.Fprintf(a.Stdout, "=== Orchard Press ===\n")
	fmt.Fprintf(a.Stdout, "Input: %s (%d tree result(s))\n\n", *inputFile, len(results))

	ok, missing := 0, 0
	for _, r := range results {
		if r.Status == "ok" {
			ok++
		} else {
			missing++
		}
	}
	fmt.Fprintf(a.Stdout, "Collected: %d | Missing/failed: %d\n\n", ok, missing)

	for _, r := range results {
		fmt.Fprintf(a.Stdout, "--- %s (%s) ---\n", r.Tree, r.Status)
		if len(r.Result) > 0 {
			fmt.Fprintln(a.Stdout, strings.TrimSpace(string(r.Result)))
		} else if r.Error != "" {
			fmt.Fprintln(a.Stdout, r.Error)
		}
	}

	return nil
}

// orchardGraft installs the OpenClaw agent runtime on a specific tree.

func (a App) findTreeInSlice(trees []ContainerInfo, treeID string) (ContainerInfo, error) {
	for _, tree := range trees {
		if tree.Labels["tree.id"] == treeID || tree.Name == treeID || strings.Contains(tree.Name, treeID) {
			return tree, nil
		}
	}
	return ContainerInfo{}, fmt.Errorf("tree %q not found", treeID)
}

func (a App) graftTree(ctx context.Context, containerRuntime ContainerRuntime, config *OrchardConfig, tree ContainerInfo, name string, upgrade bool) error {
	identity := blank(config.Agent.Identity, name)
	identityDoc := fmt.Sprintf("# IDENTITY\n\nTree: %s\nIdentity: %s\nModel: %s\n", name, identity, config.Agent.Model)
	encodedIdentity := base64.StdEncoding.EncodeToString([]byte(identityDoc))

	installScript := strings.Join([]string{
		"set -e",
		"export DEBIAN_FRONTEND=noninteractive",
		"if ! command -v apt-get >/dev/null 2>&1; then echo 'graft currently supports Debian/Ubuntu trees only' >&2; exit 1; fi",
		"apt-get update -qq",
		"apt-get install -y -qq --no-install-recommends curl ca-certificates git gnupg",
		"if ! command -v node >/dev/null 2>&1 || [ \"$(node -e 'process.stdout.write(String(process.versions.node.split(\".\")[0]))')\" -lt 22 ]; then curl -fsSL https://deb.nodesource.com/setup_22.x | bash - && apt-get install -y -qq nodejs; fi",
		"npm install -g openclaw" + boolFlag(upgrade, " --upgrade", ""),
		"mkdir -p /root/.openclaw/workspace",
		fmt.Sprintf("printf %%s %q | base64 -d > /root/.openclaw/workspace/IDENTITY.md", encodedIdentity),
		"openclaw --version",
	}, " && ")

	if err := a.treeExec(ctx, containerRuntime, tree.ID, []string{"/bin/sh", "-lc", installScript}); err != nil {
		return err
	}

	fmt.Fprintf(a.Stdout, "[%s] grafted: Node 22 + openclaw installed, identity written\n", name)
	return nil
}

func boolFlag(v bool, ifTrue, ifFalse string) string {
	if v {
		return ifTrue
	}
	return ifFalse
}

// orchardRun moved to orchard_run.go (supports --sync, task IDs, structured results)

// orchardDoctor checks host/runtime/tree readiness.
func (a App) orchardDoctor(ctx context.Context, args []string) error {
	fs := newFlagSet("orchard doctor", a.Stderr)
	configFile := fs.String("config", orchardDefaultFile, "path to orchard manifest")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	fmt.Fprintln(a.Stdout, "=== Orchard Doctor ===")

	config, err := readOrchardConfig(*configFile)
	if err != nil {
		fmt.Fprintf(a.Stdout, "✗ config: %v\n", err)
		return exit(2, "orchard config: %v", err)
	}
	fmt.Fprintf(a.Stdout, "✓ config: %s\n", *configFile)

	if strings.TrimSpace(config.Agent.Model) == "" || config.Agent.Model == "CHANGE_ME" {
		fmt.Fprintln(a.Stdout, "! agent.model is not configured")
	} else {
		fmt.Fprintf(a.Stdout, "✓ agent.model: %s\n", config.Agent.Model)
	}

	trees, cfg, containerRuntime, err := a.orchardContainers(ctx, config.Name)
	if err != nil {
		fmt.Fprintf(a.Stdout, "✗ runtime: %v\n", err)
		return exit(2, "orchard runtime: %v", err)
	}
	fmt.Fprintf(a.Stdout, "✓ runtime: apple-container (%s)\n", blank(cfg.AppleContainer.CLIPath, "container"))

	if len(trees) == 0 {
		fmt.Fprintln(a.Stdout, "! trees: none running")
		return nil
	}

	failures := 0
	for _, tree := range trees {
		name := blank(tree.Labels["tree.id"], tree.Name)
		fmt.Fprintf(a.Stdout, "\n[%s]\n", name)
		fmt.Fprintf(a.Stdout, "  container: %s\n", tree.ID)
		fmt.Fprintf(a.Stdout, "  status: %s\n", blank(tree.Status, "unknown"))
		fmt.Fprintf(a.Stdout, "  ip: %s\n", blank(tree.IP, "-"))

		checks := []struct {
			name   string
			cmd    []string
			failOn bool // fail on success (used for negative checks)
		}{
			{"node", []string{"node", "--version"}, false},
			{"npm", []string{"npm", "--version"}, false},
			{"openclaw", []string{"openclaw", "--version"}, false},
			{"identity", []string{"test", "-f", "/root/.openclaw/workspace/IDENTITY.md"}, false},
			{"openclaw.json", []string{"test", "-f", "/root/.openclaw/openclaw.json"}, false},
			{".env", []string{"test", "-f", "/root/.openclaw/.env"}, false},
			{"config validate", []string{"openclaw", "config", "validate"}, false},
		}

		for _, check := range checks {
			out, err := a.treeExecCapture(ctx, containerRuntime, tree.ID, check.cmd)
			if err != nil {
				fmt.Fprintf(a.Stdout, "  ✗ %s: %v\n", check.name, err)
				failures++
				continue
			}
			out = strings.TrimSpace(out)
			if out == "" {
				out = "ok"
			}
			fmt.Fprintf(a.Stdout, "  ✓ %s: %s\n", check.name, out)
		}
	}

	if failures > 0 {
		return exit(1, "orchard doctor found %d failure(s)", failures)
	}

	return nil
}

// orchardLogs prints logs for one tree.
func (a App) orchardLogs(ctx context.Context, args []string) error {
	fs := newFlagSet("orchard logs", a.Stderr)
	configFile := fs.String("config", orchardDefaultFile, "path to orchard manifest")
	treeID := fs.String("tree", "", "tree ID")
	tailLines := fs.Int("tail", 200, "number of log lines to show")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *treeID == "" {
		return exit(2, "--tree required")
	}

	config, err := readOrchardConfig(*configFile)
	if err != nil {
		return exit(2, "orchard config: %v", err)
	}

	target, containerRuntime, err := a.findOrchardTree(ctx, config.Name, *treeID)
	if err != nil {
		return exit(2, "orchard logs: %v", err)
	}

	logs, err := containerRuntime.Logs(ctx, target.ID, *tailLines)
	if err != nil {
		return exit(1, "logs failed: %v", err)
	}

	fmt.Fprint(a.Stdout, logs)
	if !strings.HasSuffix(logs, "\n") {
		fmt.Fprintln(a.Stdout)
	}
	return nil
}

// orchardChop tears down the entire orchard.
func (a App) orchardChop(ctx context.Context, args []string) error {
	fs := newFlagSet("orchard chop", a.Stderr)
	configFile := fs.String("config", orchardDefaultFile, "path to orchard manifest")
	yes := fs.Bool("yes", false, "skip confirmation")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	config, err := readOrchardConfig(*configFile)
	if err != nil {
		return exit(2, "orchard config: %v", err)
	}

	fmt.Fprintf(a.Stdout, "=== Orchard Chop ===\n")
	fmt.Fprintf(a.Stdout, "This will destroy all trees in orchard %q.\n", config.Name)

	if !*yes {
		fmt.Fprint(a.Stdout, "\nChop entire orchard? [y/N] ")
		var response string
		fmt.Fscanln(a.Stdin, &response)
		if response != "y" && response != "Y" {
			fmt.Fprintln(a.Stdout, "Aborted.")
			return nil
		}
	}

	trees, _, containerRuntime, err := a.orchardContainers(ctx, config.Name)
	if err != nil {
		return exit(2, "orchard chop: %v", err)
	}

	fmt.Fprintln(a.Stdout, "\nChopping...")
	chopped := 0
	failed := 0

	for _, tree := range trees {
		treeName := blank(tree.Labels["tree.name"], blank(tree.Labels["tree.id"], tree.Name))
		if err := containerRuntime.Remove(ctx, tree.ID, true); err != nil {
			fmt.Fprintf(a.Stderr, "  ✗ %s: %v\n", treeName, err)
			failed++
			continue
		}

		fmt.Fprintf(a.Stdout, "  ✓ %s chopped\n", treeName)
		chopped++
	}

	fmt.Fprintf(a.Stdout, "Chopped %d/%d trees.", chopped, chopped+failed)
	if failed > 0 {
		fmt.Fprintf(a.Stdout, " %d failed.", failed)
	}
	fmt.Fprintln(a.Stdout)

	if failed == 0 {
		if err := removeOrchardState(config.Name); err != nil {
			fmt.Fprintf(a.Stderr, "WARN: remove orchard state: %v\n", err)
		}
	}

	return nil
}

// orchardList shows active trees across all orchards from live container labels.
func (a App) orchardList(ctx context.Context, args []string) error {
	fs := newFlagSet("orchard list", a.Stderr)
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	trees, _, _, err := a.orchardContainers(ctx, "")
	if err != nil {
		return exit(2, "orchard list: %v", err)
	}

	fmt.Fprintln(a.Stdout, "=== Active Trees ===")
	if len(trees) == 0 {
		fmt.Fprintln(a.Stdout, "(No trees running — plant an orchard first)")
		return nil
	}

	fmt.Fprintf(a.Stdout, "%-16s %-12s %-10s %s\n", "ORCHARD", "TREE", "STATUS", "IP")
	fmt.Fprintln(a.Stdout, strings.Repeat("-", 56))

	for _, tree := range trees {
		fmt.Fprintf(a.Stdout, "%-16s %-12s %-10s %s\n",
			blank(tree.Labels["orchard.name"], "-"),
			blank(tree.Labels["tree.id"], tree.Name),
			blank(tree.Status, "unknown"),
			blank(tree.IP, "-"),
		)
	}

	return nil
}

func (a App) findOrchardTree(ctx context.Context, orchardName, treeID string) (ContainerInfo, ContainerRuntime, error) {
	trees, _, containerRuntime, err := a.orchardContainers(ctx, orchardName)
	if err != nil {
		return ContainerInfo{}, nil, err
	}

	for _, tree := range trees {
		if tree.Labels["tree.id"] == treeID || tree.Name == treeID || strings.Contains(tree.Name, treeID) {
			return tree, containerRuntime, nil
		}
	}

	return ContainerInfo{}, nil, fmt.Errorf("tree %q not found", treeID)
}

// readOrchardConfig loads the orchard manifest.
func readOrchardConfig(path string) (*OrchardConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config OrchardConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	if config.Trees <= 0 {
		config.Trees = 1
	}
	if config.Template.Image == "" {
		config.Template.Image = "ubuntu:26.04"
	}
	if config.Template.CPUs <= 0 {
		config.Template.CPUs = 2
	}
	if config.Template.Memory == "" {
		config.Template.Memory = "4G"
	}
	if config.Agent.Command == "" {
		config.Agent.Command = `openclaw run "$ORCHARD_TASK"`
	}

	return &config, nil
}

func findOrchardConfig() (*OrchardConfig, string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, "", err
	}

	for {
		path := filepath.Join(dir, orchardDefaultFile)
		if _, err := os.Stat(path); err == nil {
			cfg, err := readOrchardConfig(path)
			return cfg, path, err
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return nil, "", fmt.Errorf("no %s found", orchardDefaultFile)
}
