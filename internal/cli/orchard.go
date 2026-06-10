package cli

import (
	"context"
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
	Name     string       `yaml:"name"`
	Trees    int          `yaml:"trees"`
	Template TreeTemplate `yaml:"template"`
	Agent    AgentConfig  `yaml:"agent"`
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
}

// TreeState tracks a running tree in the orchard.
type TreeState struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Status      string    `json:"status"` // planting | growing | ready | wilted
	IP          string    `json:"ip"`
	LeaseID     string    `json:"lease_id"`
	ContainerID string    `json:"container_id"` // Apple Container ID for exec
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

// orchardCommand is the entry point for orchard swarm management.
func (a App) orchardCommand(ctx context.Context, args []string) error {
	if len(args) == 0 {
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
  init      Scaffold .orchard.yaml
  plant     Spin up N trees from manifest
  tend      Show swarm status / health (live container state)
  graft     Install the OpenClaw agent runtime on a tree
  harvest   Collect `+orchardResultPath+` from every tree
  press     Aggregate harvested outputs into one report
  chop      Tear down the entire orchard
  list      Show active trees (live container state)

Examples:
  ciderbox orchard init
  ciderbox orchard plant --config .orchard.yaml
  ciderbox orchard tend
  ciderbox orchard harvest --output results.json`)
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
			// Placeholder — set to a model your provider actually serves,
			// e.g. "anthropic/claude-sonnet-4-5" or "openai/gpt-4.1-mini".
			Model: "CHANGE_ME",
		},
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return exit(2, "marshal orchard config: %v", err)
	}

	header := `# Orchard — AI agent swarm manifest
# Each tree is an isolated Apple Container VM. Run "orchard graft" after
# "orchard plant" to install the OpenClaw runtime on each tree.
#
# agent.model is a placeholder — set it to a real model string for your
# provider before grafting.
#
# Quick start:
#   ciderbox orchard plant
#   ciderbox orchard graft --tree tree-0
#   ciderbox orchard tend

`

	if err := os.WriteFile(path, []byte(header+string(data)), 0644); err != nil {
		return exit(2, "write %s: %v", path, err)
	}

	fmt.Fprintf(a.Stdout, "Created %s\n\n", path)
	fmt.Fprintln(a.Stdout, "Next steps:")
	fmt.Fprintln(a.Stdout, "  1. Edit the manifest with your tree specs (set agent.model!)")
	fmt.Fprintln(a.Stdout, "  2. Run `ciderbox orchard plant` to spin up the swarm")
	fmt.Fprintln(a.Stdout, "  3. Run `ciderbox orchard tend` to check tree health")
	return nil
}

// orchardBackend configures the apple-container provider and returns its
// SSH lease backend plus the resolved config (for the container CLI path).
func (a App) orchardBackend() (SSHLeaseBackend, Config, error) {
	cfg, rt, err := a.providerConfigRuntime("apple-container")
	if err != nil {
		return nil, Config{}, err
	}
	provider, err := ProviderFor("apple-container")
	if err != nil {
		return nil, Config{}, err
	}
	backend, err := provider.Configure(cfg, rt)
	if err != nil {
		return nil, Config{}, err
	}
	sshBackend, ok := backend.(SSHLeaseBackend)
	if !ok {
		return nil, Config{}, fmt.Errorf("apple-container provider does not support SSH leases")
	}
	return sshBackend, cfg, nil
}

// orchardTrees lists live containers belonging to the named orchard.
func (a App) orchardTrees(ctx context.Context, orchardName string) ([]LeaseView, Config, error) {
	sshBackend, cfg, err := a.orchardBackend()
	if err != nil {
		return nil, Config{}, err
	}
	leases, err := sshBackend.List(ctx, ListRequest{})
	if err != nil {
		return nil, Config{}, err
	}
	var trees []LeaseView
	for _, lease := range leases {
		if lease.Labels["orchard.name"] == orchardName {
			trees = append(trees, lease)
		}
	}
	sort.Slice(trees, func(i, j int) bool {
		return trees[i].Labels["tree.id"] < trees[j].Labels["tree.id"]
	})
	return trees, cfg, nil
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
	fmt.Fprintf(a.Stdout, "Trees:   %d\n", config.Trees)
	fmt.Fprintf(a.Stdout, "Image:   %s (%d CPUs, %s RAM)\n\n", config.Template.Image, config.Template.CPUs, config.Template.Memory)

	var wg sync.WaitGroup
	trees := make([]TreeState, config.Trees)

	for i := 0; i < config.Trees; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			trees[idx] = a.plantTree(ctx, config, idx)
		}(i)
	}

	wg.Wait()

	// Show results
	ready := 0
	failed := 0
	for _, tree := range trees {
		if tree.Status == "ready" {
			ready++
		} else {
			failed++
		}
	}

	// Persist state so later commands (and humans) can find the orchard
	// even though tend/list/harvest derive live state from labels.
	state := OrchardState{Name: config.Name, PlantedAt: time.Now().UTC(), Trees: trees}
	if err := writeOrchardState(state); err != nil {
		fmt.Fprintf(a.Stderr, "WARN: persist orchard state: %v\n", err)
	} else if path, perr := orchardStatePath(config.Name); perr == nil {
		fmt.Fprintf(a.Stdout, "State:   %s\n", path)
	}

	fmt.Fprintf(a.Stdout, "Planted %d/%d trees.\n", ready, config.Trees)
	if failed > 0 {
		fmt.Fprintf(a.Stdout, "%d tree(s) failed to grow.\n", failed)
		return exit(1, "%d tree(s) failed", failed)
	}

	return nil
}

// plantTree provisions a single tree via the apple-container provider.
func (a App) plantTree(ctx context.Context, config *OrchardConfig, idx int) TreeState {
	treeName := fmt.Sprintf("%s-tree-%d", config.Name, idx)
	tree := TreeState{
		ID:        fmt.Sprintf("tree-%d", idx),
		Name:      treeName,
		Status:    "planting",
		CreatedAt: time.Now().UTC(),
	}

	fmt.Fprintf(a.Stderr, "[%s] planting...\n", treeName)

	// Configure provider
	cfg, rt, err := a.providerConfigRuntime("apple-container")
	if err != nil {
		fmt.Fprintf(a.Stderr, "[%s] config error: %v\n", treeName, err)
		tree.Status = "wilted"
		return tree
	}

	// Override with template specs
	cfg.AppleContainer.Image = config.Template.Image
	if config.Template.CPUs > 0 {
		cfg.AppleContainer.CPUs = config.Template.CPUs
	}
	if config.Template.Memory != "" {
		cfg.AppleContainer.Memory = config.Template.Memory
	}

	// Add orchard labels
	if cfg.AppleContainer.ExtraRunArgs == nil {
		cfg.AppleContainer.ExtraRunArgs = []string{}
	}
	cfg.AppleContainer.ExtraRunArgs = append(cfg.AppleContainer.ExtraRunArgs,
		"--label", "orchard=true",
		"--label", fmt.Sprintf("orchard.name=%s", config.Name),
		"--label", fmt.Sprintf("tree.id=%s", tree.ID),
		"--label", fmt.Sprintf("tree.name=%s", treeName),
	)

	// Get provider and acquire lease
	provider, err := ProviderFor("apple-container")
	if err != nil {
		fmt.Fprintf(a.Stderr, "[%s] provider error: %v\n", treeName, err)
		tree.Status = "wilted"
		return tree
	}

	backend, err := provider.Configure(cfg, rt)
	if err != nil {
		fmt.Fprintf(a.Stderr, "[%s] backend error: %v\n", treeName, err)
		tree.Status = "wilted"
		return tree
	}

	sshBackend, ok := backend.(SSHLeaseBackend)
	if !ok {
		fmt.Fprintf(a.Stderr, "[%s] provider does not support SSH leases\n", treeName)
		tree.Status = "wilted"
		return tree
	}

	// Acquire lease
	slug := fmt.Sprintf("%s-%d", config.Name, idx)
	lease, err := sshBackend.Acquire(ctx, AcquireRequest{
		Options:       LeaseOptions{TargetOS: cfg.TargetOS},
		Keep:          true,
		RequestedSlug: slug,
	})
	if err != nil {
		fmt.Fprintf(a.Stderr, "[%s] acquire failed: %v\n", treeName, err)
		tree.Status = "wilted"
		return tree
	}

	tree.LeaseID = lease.LeaseID
	tree.IP = lease.Server.PublicNet.IPv4.IP
	// Extract container ID from lease labels
	tree.ContainerID = lease.Server.Labels["container_id"]
	if tree.ContainerID == "" {
		// Fallback: container ID is usually the server name
		tree.ContainerID = lease.Server.Name
	}
	tree.Status = "ready"
	fmt.Fprintf(a.Stderr, "[%s] ready (lease=%s, container=%s, ip=%s)\n", treeName, lease.LeaseID, tree.ContainerID, tree.IP)
	return tree
}

// treeAge formats how long ago a tree was created based on its lease label.
func treeAge(labels map[string]string, now time.Time) string {
	createdAt, err := time.Parse(time.RFC3339, labels["created_at"])
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

	trees, _, err := a.orchardTrees(ctx, config.Name)
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
		id := blank(tree.Labels["tree.id"], tree.Name)
		fmt.Fprintf(a.Stdout, "%-12s %-10s %-16s %s\n", id, blank(tree.Status, "unknown"), blank(tree.PublicNet.IPv4.IP, "-"), treeAge(tree.Labels, now))
	}
	if len(trees) < config.Trees {
		fmt.Fprintf(a.Stdout, "\nWARNING: %d tree(s) missing.\n", config.Trees-len(trees))
	}
	return nil
}

// treeExec runs a command inside a tree using the configured container CLI
// through the runtime command runner (honors --apple-container-cli).
func (a App) treeExec(ctx context.Context, cfg Config, containerID string, command []string) error {
	rt := runtimeForApp(a)
	cli := blank(cfg.AppleContainer.CLIPath, "container")
	result, err := rt.Exec.Run(ctx, LocalCommandRequest{
		Name:   cli,
		Args:   append([]string{"exec", containerID}, command...),
		Stdout: a.Stdout,
		Stderr: a.Stderr,
	})
	if err != nil {
		return fmt.Errorf("container exec: %w (stderr: %s)", err, strings.TrimSpace(result.Stderr))
	}
	return nil
}

// treeExecCapture runs a command inside a tree and returns its stdout.
func (a App) treeExecCapture(ctx context.Context, cfg Config, containerID string, command []string) (string, error) {
	rt := runtimeForApp(a)
	cli := blank(cfg.AppleContainer.CLIPath, "container")
	var out strings.Builder
	result, err := rt.Exec.Run(ctx, LocalCommandRequest{
		Name:   cli,
		Args:   append([]string{"exec", containerID}, command...),
		Stdout: &out,
		Stderr: nil,
	})
	if err != nil {
		return "", fmt.Errorf("container exec: %w (stderr: %s)", err, strings.TrimSpace(result.Stderr))
	}
	return out.String(), nil
}

// treeCopy copies files between host and tree via the container CLI.
func (a App) treeCopy(ctx context.Context, cfg Config, src, dst string) error {
	rt := runtimeForApp(a)
	cli := blank(cfg.AppleContainer.CLIPath, "container")
	result, err := rt.Exec.Run(ctx, LocalCommandRequest{
		Name:   cli,
		Args:   []string{"cp", src, dst},
		Stdout: a.Stdout,
		Stderr: a.Stderr,
	})
	if err != nil {
		return fmt.Errorf("container cp: %w (stderr: %s)", err, strings.TrimSpace(result.Stderr))
	}
	return nil
}

// HarvestResult is one tree's collected output.
type HarvestResult struct {
	Tree   string          `json:"tree"`
	Status string          `json:"status"` // ok | missing | error
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

	trees, cfg, err := a.orchardTrees(ctx, config.Name)
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
		containerID := blank(tree.Labels["container_id"], tree.Name)
		out, execErr := a.treeExecCapture(ctx, cfg, containerID, []string{"cat", orchardResultPath})
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
			// Not JSON — still capture it as a quoted string.
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
		return exit(2, "press requires --input <harvest.json>; run `orchard harvest --output harvest.json` first")
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

// orchardGraft installs the OpenClaw agent runtime on a specific tree:
// Node.js 22, the openclaw npm package, and the tree's identity file.
func (a App) orchardGraft(ctx context.Context, args []string) error {
	fs := newFlagSet("orchard graft", a.Stderr)
	treeID := fs.String("tree", "", "tree ID to graft onto")
	configFile := fs.String("config", orchardDefaultFile, "path to orchard manifest")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	if *treeID == "" {
		return exit(2, "--tree required")
	}

	config, err := readOrchardConfig(*configFile)
	if err != nil {
		return exit(2, "orchard graft: config: %v", err)
	}

	sshBackend, cfg, err := a.orchardBackend()
	if err != nil {
		return exit(2, "orchard graft: %v", err)
	}

	leases, err := sshBackend.List(ctx, ListRequest{})
	if err != nil {
		return exit(2, "orchard graft: list: %v", err)
	}

	var targetLease LeaseView
	for _, lease := range leases {
		if lease.Labels["tree.id"] == *treeID || strings.Contains(lease.Name, *treeID) {
			targetLease = lease
			break
		}
	}

	if targetLease.Name == "" {
		return exit(2, "tree %q not found", *treeID)
	}

	containerID := blank(targetLease.Labels["container_id"], targetLease.Name)

	fmt.Fprintf(a.Stdout, "=== Orchard Graft ===\n")
	fmt.Fprintf(a.Stdout, "Tree:      %s\n", *treeID)
	fmt.Fprintf(a.Stdout, "Container: %s\n", containerID)
	fmt.Fprintln(a.Stdout, "Installing Node.js 22 + OpenClaw...")

	identity := blank(config.Agent.Identity, *treeID)
	installScript := strings.Join([]string{
		"set -e",
		"export DEBIAN_FRONTEND=noninteractive",
		"if ! command -v apt-get >/dev/null 2>&1; then echo 'graft currently supports Debian/Ubuntu trees only' >&2; exit 1; fi",
		"apt-get update -qq",
		"apt-get install -y -qq --no-install-recommends curl ca-certificates git gnupg",
		"if ! command -v node >/dev/null 2>&1 || [ \"$(node -e 'process.stdout.write(String(process.versions.node.split(\".\")[0]))')\" -lt 22 ]; then curl -fsSL https://deb.nodesource.com/setup_22.x | bash - && apt-get install -y -qq nodejs; fi",
		"npm install -g openclaw",
		"mkdir -p /root/.openclaw/workspace",
		fmt.Sprintf("printf '# IDENTITY\\n\\nTree: %s\\nIdentity: %s\\nModel: %s\\n' > /root/.openclaw/workspace/IDENTITY.md", *treeID, identity, config.Agent.Model),
		"openclaw --version",
	}, " && ")

	if err := a.treeExec(ctx, cfg, containerID, []string{"/bin/sh", "-lc", installScript}); err != nil {
		return exit(1, "graft failed: %v", err)
	}

	fmt.Fprintln(a.Stdout, "\nAgent grafted: Node 22 + openclaw installed, identity written.")
	fmt.Fprintln(a.Stdout, "Note: model/provider credentials are NOT configured — set them up inside the tree before running the agent.")

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

	sshBackend, _, err := a.orchardBackend()
	if err != nil {
		return exit(2, "orchard chop: %v", err)
	}

	// List all leases and find orchard trees
	leases, err := sshBackend.List(ctx, ListRequest{})
	if err != nil {
		return exit(2, "orchard chop: list: %v", err)
	}

	fmt.Fprintln(a.Stdout, "\nChopping...")
	chopped := 0
	failed := 0
	for _, lease := range leases {
		if lease.Labels["orchard.name"] != config.Name {
			continue
		}
		treeName := lease.Labels["tree.name"]
		if treeName == "" {
			treeName = lease.Labels["tree.id"]
		}
		if err := sshBackend.ReleaseLease(ctx, ReleaseLeaseRequest{Lease: LeaseTarget{Server: lease, LeaseID: lease.Labels["lease"]}}); err != nil {
			fmt.Fprintf(a.Stderr, "  ✗ %s: %v\n", treeName, err)
			failed++
		} else {
			fmt.Fprintf(a.Stdout, "  ✓ %s chopped\n", treeName)
			chopped++
		}
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

// orchardList shows active trees across all orchards, derived from live
// container labels.
func (a App) orchardList(ctx context.Context, args []string) error {
	fs := newFlagSet("orchard list", a.Stderr)
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	sshBackend, _, err := a.orchardBackend()
	if err != nil {
		return exit(2, "orchard list: %v", err)
	}
	leases, err := sshBackend.List(ctx, ListRequest{})
	if err != nil {
		return exit(2, "orchard list: %v", err)
	}

	var trees []LeaseView
	for _, lease := range leases {
		if lease.Labels["orchard"] == "true" {
			trees = append(trees, lease)
		}
	}
	sort.Slice(trees, func(i, j int) bool {
		if trees[i].Labels["orchard.name"] != trees[j].Labels["orchard.name"] {
			return trees[i].Labels["orchard.name"] < trees[j].Labels["orchard.name"]
		}
		return trees[i].Labels["tree.id"] < trees[j].Labels["tree.id"]
	})

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
			blank(tree.PublicNet.IPv4.IP, "-"))
	}

	return nil
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

	// Defaults
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
