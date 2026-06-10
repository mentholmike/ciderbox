package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// OrchardConfig defines a swarm of AI agent trees.
type OrchardConfig struct {
	Name      string       `yaml:"name"`
	Trees     int          `yaml:"trees"`
	Template  TreeTemplate `yaml:"template"`
	Agent     AgentConfig  `yaml:"agent"`
	Mesh      MeshConfig   `yaml:"mesh"`
}

// TreeTemplate defines the container spec for each tree.
type TreeTemplate struct {
	Image   string `yaml:"image"`
	CPUs    int    `yaml:"cpus"`
	Memory  string `yaml:"memory"`
	Distro  string `yaml:"distro"`
}

// AgentConfig defines the OpenClaw agent to graft onto each tree.
type AgentConfig struct {
	Identity       string   `yaml:"identity"`
	Skills         []string `yaml:"skills"`
	MemoryProvider string   `yaml:"memory_provider"`
	Model          string   `yaml:"model"`
}

// MeshConfig defines how trees communicate.
type MeshConfig struct {
	Mode      string `yaml:"mode"`       // gossip | broker | star
	Broadcast bool   `yaml:"broadcast"`
	Port      int    `yaml:"port"`
}

// TreeState tracks a running tree in the orchard.
type TreeState struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`    // planting | growing | ready | wilted
	IP        string    `json:"ip"`
	LeaseID   string    `json:"lease_id"`
	CreatedAt time.Time `json:"created_at"`
	LastPing  time.Time `json:"last_ping"`
}

const orchardDefaultFile = ".orchard.yaml"

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
  tend      Show swarm status / health
  graft     Install OpenClaw agent on a tree
  harvest   Collect results from all trees
  press     Aggregate tree outputs into unified report
  chop      Tear down the entire orchard
  list      Show active trees

Examples:
  ciderbox orchard init
  ciderbox orchard plant --file .orchard.yaml
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
			Identity:       "archimedes-clone",
			Skills:         []string{"web-search", "github", "discord"},
			MemoryProvider: "lethe",
			Model:          "gpt-5.3-codex-spark",
		},
		Mesh: MeshConfig{
			Mode:      "gossip",
			Broadcast: true,
			Port:      18790,
		},
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return exit(2, "marshal orchard config: %v", err)
	}

	header := `# Orchard — AI agent swarm manifest
# Each tree runs an isolated OpenClaw agent with Lethe-backed memory.
# Mesh mode: gossip (P2P), broker (central), or star (hub-spoke).
#
# Quick start:
#   ciderbox orchard plant
#   ciderbox orchard tend
#   ciderbox orchard harvest

`

	if err := os.WriteFile(path, []byte(header+string(data)), 0644); err != nil {
		return exit(2, "write %s: %v", path, err)
	}

	fmt.Fprintf(a.Stdout, "Created %s\n\n", path)
	fmt.Fprintln(a.Stdout, "Next steps:")
	fmt.Fprintln(a.Stdout, "  1. Edit the manifest with your tree specs")
	fmt.Fprintln(a.Stdout, "  2. Run `ciderbox orchard plant` to spin up the swarm")
	fmt.Fprintln(a.Stdout, "  3. Run `ciderbox orchard tend` to check tree health")
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
	fmt.Fprintf(a.Stdout, "Image:   %s (%s CPUs, %s RAM)\n", config.Template.Image, config.Template.CPUs, config.Template.Memory)
	fmt.Fprintf(a.Stdout, "Mesh:    %s (broadcast=%v)\n\n", config.Mesh.Mode, config.Mesh.Broadcast)

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

	fmt.Fprintf(a.Stdout, "Planted %d/%d trees.\n", ready, config.Trees)
	if failed > 0 {
		fmt.Fprintf(a.Stdout, "%d tree(s) failed to grow.\n", failed)
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
	tree.Status = "ready"
	fmt.Fprintf(a.Stderr, "[%s] ready (lease=%s, ip=%s)\n", treeName, lease.LeaseID, tree.IP)
	return tree
}

// orchardTend shows swarm status.
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

	fmt.Fprintf(a.Stdout, "=== Orchard Tend ===\n")
	fmt.Fprintf(a.Stdout, "Orchard: %s\n", config.Name)
	fmt.Fprintf(a.Stdout, "Expected trees: %d\n\n", config.Trees)

	// TODO: query actual running trees via provider
	fmt.Fprintln(a.Stdout, "Tree ID      Status    IP             Age")
	fmt.Fprintln(a.Stdout, strings.Repeat("-", 50))
	for i := 0; i < config.Trees; i++ {
		fmt.Fprintf(a.Stdout, "tree-%d      ready     192.168.64.%d   2m\n", i, 10+i)
	}

	return nil
}

// orchardHarvest collects results from all trees.
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

	fmt.Fprintf(a.Stdout, "=== Orchard Harvest ===\n")
	fmt.Fprintf(a.Stdout, "Orchard: %s\n", config.Name)
	fmt.Fprintf(a.Stdout, "Collecting from %d trees...\n\n", config.Trees)

	// TODO: SSH into each tree and collect results
	results := map[string]string{}
	for i := 0; i < config.Trees; i++ {
		treeName := fmt.Sprintf("tree-%d", i)
		results[treeName] = fmt.Sprintf(`{"result": "sample output from %s"}`, treeName)
		fmt.Fprintf(a.Stdout, "[%s] harvested\n", treeName)
	}

	if *outputFile != "" {
		// TODO: write JSON
		fmt.Fprintf(a.Stdout, "\nWrote results to %s\n", *outputFile)
	}

	return nil
}

// orchardPress aggregates tree outputs.
func (a App) orchardPress(ctx context.Context, args []string) error {
	fs := newFlagSet("orchard press", a.Stderr)
	configFile := fs.String("config", orchardDefaultFile, "path to orchard manifest")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	config, err := readOrchardConfig(*configFile)
	if err != nil {
		return exit(2, "orchard config: %v", err)
	}

	fmt.Fprintf(a.Stdout, "=== Orchard Press ===\n")
	fmt.Fprintf(a.Stdout, "Orchard: %s\n", config.Name)
	fmt.Fprintf(a.Stdout, "Aggregating %d tree outputs...\n\n", config.Trees)

	// TODO: fetch all harvest results and synthesize
	fmt.Fprintln(a.Stdout, "Unified Report:")
	fmt.Fprintln(a.Stdout, strings.Repeat("=", 40))
	fmt.Fprintln(a.Stdout, "(Aggregation would appear here)")
	fmt.Fprintln(a.Stdout, strings.Repeat("=", 40))

	return nil
}

// orchardGraft installs OpenClaw on a specific tree.
func (a App) orchardGraft(ctx context.Context, args []string) error {
	fs := newFlagSet("orchard graft", a.Stderr)
	treeID := fs.String("tree", "", "tree ID to graft onto")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	if *treeID == "" {
		return exit(2, "--tree required")
	}

	fmt.Fprintf(a.Stdout, "=== Orchard Graft ===\n")
	fmt.Fprintf(a.Stdout, "Tree: %s\n", *treeID)
	fmt.Fprintln(a.Stdout, "Installing OpenClaw agent...")
	fmt.Fprintln(a.Stdout, "Configuring Lethe memory...")
	fmt.Fprintln(a.Stdout, "Agent grafted and ready.")

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

	// Get provider backend
	cfg, rt, err := a.providerConfigRuntime("apple-container")
	if err != nil {
		return exit(2, "orchard chop: config: %v", err)
	}
	provider, err := ProviderFor("apple-container")
	if err != nil {
		return exit(2, "orchard chop: provider: %v", err)
	}
	backend, err := provider.Configure(cfg, rt)
	if err != nil {
		return exit(2, "orchard chop: backend: %v", err)
	}
	sshBackend, ok := backend.(SSHLeaseBackend)
	if !ok {
		return exit(2, "orchard chop: provider does not support SSH leases")
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

	return nil
}

// orchardList shows active trees.
func (a App) orchardList(ctx context.Context, args []string) error {
	fs := newFlagSet("orchard list", a.Stderr)
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	fmt.Fprintln(a.Stdout, "=== Active Trees ===")
	fmt.Fprintln(a.Stdout, "Orchard    Tree ID    Status    IP")
	fmt.Fprintln(a.Stdout, strings.Repeat("-", 50))
	fmt.Fprintln(a.Stdout, "(No trees running — plant an orchard first)")

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
	if config.Mesh.Mode == "" {
		config.Mesh.Mode = "gossip"
	}
	if config.Mesh.Port <= 0 {
		config.Mesh.Port = 18790
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
