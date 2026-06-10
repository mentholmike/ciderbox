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

// CiderboxConfig is the full repo-level config for ciderbox projects.
// Lives at .ciderbox.yaml in the project root.
type CiderboxConfig struct {
	Project     string            `yaml:"project"`
	CompileTest CompileTestConfig `yaml:"compileTest"`
	Build       BuildConfig       `yaml:"build"`
	Run         CiderboxRunSettings `yaml:"run"`
}

// CompileTestConfig defines the compile-test matrix for a ciderbox repo.
type CompileTestConfig struct {
	Distros  []DistroConfig `yaml:"distros"`
	Command  string         `yaml:"command"`
	Parallel bool           `yaml:"parallel"`
}

// BuildConfig defines how to build the project in a container.
type BuildConfig struct {
	Image        string   `yaml:"image"`
	Command      string   `yaml:"command"`
	Dependencies []string `yaml:"dependencies,omitempty"`
	CachePaths   []string `yaml:"cachePaths,omitempty"`
}

// RunConfig defines default run settings for ciderbox projects.
type CiderboxRunSettings struct {
	Provider string `yaml:"provider,omitempty"`
	Image    string `yaml:"image,omitempty"`
}

// DistroConfig defines a single target distro in the compile-test matrix.
type DistroConfig struct {
	Name  string `yaml:"name"`
	Image string `yaml:"image"`
}

// CompileTestResult holds the outcome for one distro run.
type CompileTestResult struct {
	Distro   string
	Image    string
	Success  bool
	Duration time.Duration
	ExitCode int
	Error    string
}

func (a App) compileTest(ctx context.Context, args []string) error {
	fs := newFlagSet("compile-test", a.Stderr)
	provider := fs.String("provider", "apple-container", "provider for compile-test leases")
	parallel := fs.Bool("parallel", true, "run distro tests in parallel")
	configFile := fs.String("config", ".ciderbox.yaml", "path to ciderbox config")

	if err := parseFlags(fs, args); err != nil {
		return err
	}

	// Read config file
	cfg, err := readCiderboxConfig(*configFile)
	if err != nil {
		return exit(2, "compile-test config: %v", err)
	}

	if len(cfg.CompileTest.Distros) == 0 {
		return exit(2, "no distros configured in %s", *configFile)
	}

	command := cfg.CompileTest.Command
	if command == "" {
		// Fall back to build command
		command = cfg.Build.Command
	}
	if command == "" {
		return exit(2, "no compile command configured in %s", *configFile)
	}

	fmt.Fprintf(a.Stdout, "=== Ciderbox Compile Test ===\n")
	fmt.Fprintf(a.Stdout, "Project:  %s\n", cfg.Project)
	fmt.Fprintf(a.Stdout, "Command:  %s\n", command)
	fmt.Fprintf(a.Stdout, "Distros:  %d\n", len(cfg.CompileTest.Distros))
	fmt.Fprintf(a.Stdout, "Mode:     %s\n\n", map[bool]string{true: "parallel", false: "sequential"}[*parallel])

	// Run tests
	results := make([]CompileTestResult, len(cfg.CompileTest.Distros))

	if *parallel {
		var wg sync.WaitGroup
		for i, distro := range cfg.CompileTest.Distros {
			wg.Add(1)
			go func(idx int, d DistroConfig) {
				defer wg.Done()
				results[idx] = a.runCompileTest(ctx, *provider, d, command)
			}(i, distro)
		}
		wg.Wait()
	} else {
		for i, distro := range cfg.CompileTest.Distros {
			results[i] = a.runCompileTest(ctx, *provider, distro, command)
		}
	}

	// Display results
	a.displayCompileTestResults(results)

	return nil
}

func (a App) runCompileTest(ctx context.Context, provider string, distro DistroConfig, command string) CompileTestResult {
	result := CompileTestResult{
		Distro: distro.Name,
		Image:  distro.Image,
	}

	start := time.Now()

	// Build ciderbox run args (don't include "run" — runCommand handles that)
	args := []string{
		"--provider", provider,
		"--apple-container-image", distro.Image,
		"--keep",
		"--",
	}

	// Split command by shell
	cmdParts := strings.Fields(command)
	args = append(args, cmdParts...)

	fmt.Fprintf(a.Stderr, "[%s] starting...\n", distro.Name)

	// Run the command
	err := a.runCommand(ctx, args)

	result.Duration = time.Since(start)

	if err != nil {
		var exitErr ExitError
		if AsExitError(err, &exitErr) {
			result.ExitCode = exitErr.Code
		}
		result.Success = false
		result.Error = err.Error()
		fmt.Fprintf(a.Stderr, "[%s] FAILED (%s)\n", distro.Name, result.Duration)
	} else {
		result.Success = true
		result.ExitCode = 0
		fmt.Fprintf(a.Stderr, "[%s] PASSED (%s)\n", distro.Name, result.Duration)
	}

	return result
}

func (a App) displayCompileTestResults(results []CompileTestResult) {
	fmt.Fprintf(a.Stdout, "\n=== Results ===\n")
	fmt.Fprintf(a.Stdout, "%-20s %-15s %-10s %-15s %s\n", "DISTRO", "IMAGE", "STATUS", "DURATION", "EXIT")
	fmt.Fprintf(a.Stdout, "%s\n", strings.Repeat("-", 80))

	passed := 0
	failed := 0

	for _, r := range results {
		status := "PASS"
		if !r.Success {
			status = "FAIL"
			failed++
		} else {
			passed++
		}
		fmt.Fprintf(a.Stdout, "%-20s %-15s %-10s %-15s %d\n",
			r.Distro, r.Image, status, r.Duration, r.ExitCode)
	}

	fmt.Fprintf(a.Stdout, "%s\n", strings.Repeat("-", 80))
	fmt.Fprintf(a.Stdout, "Total: %d | Passed: %d | Failed: %d\n", len(results), passed, failed)
}

// readCiderboxConfig reads the full .ciderbox.yaml config file.
func readCiderboxConfig(path string) (*CiderboxConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config CiderboxConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// findCiderboxConfig looks for .ciderbox.yaml in current dir and parents.
func findCiderboxConfig() (*CiderboxConfig, string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, "", err
	}

	for {
		path := filepath.Join(dir, ".ciderbox.yaml")
		if _, err := os.Stat(path); err == nil {
			cfg, err := readCiderboxConfig(path)
			return cfg, path, err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return nil, "", fmt.Errorf("no .ciderbox.yaml found")
}

// ciderboxInit creates a new .ciderbox.yaml in the current directory.
func (a App) ciderboxInit(ctx context.Context, args []string) error {
	fs := newFlagSet("init", a.Stderr)
	force := fs.Bool("force", false, "overwrite existing .ciderbox.yaml")

	if err := parseFlags(fs, args); err != nil {
		return err
	}

	configPath := filepath.Join(".", ".ciderbox.yaml")

	if _, err := os.Stat(configPath); err == nil && !*force {
		return exit(2, "%s already exists; use --force to overwrite", configPath)
	}

	// Detect project name from current directory
	dir, _ := os.Getwd()
	projectName := filepath.Base(dir)

	config := CiderboxConfig{
		Project: projectName,
		CompileTest: CompileTestConfig{
			Distros: []DistroConfig{
				{Name: "ubuntu", Image: "ubuntu:26.04"},
				{Name: "debian", Image: "debian:bookworm"},
			},
			Command:  "make test",
			Parallel: true,
		},
		Build: BuildConfig{
			Image:        "ubuntu:26.04",
			Command:      "make build",
			Dependencies: []string{"build-essential", "git"},
		},
		Run: CiderboxRunSettings{
			Provider: "apple-container",
			Image:    "ubuntu:26.04",
		},
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return exit(2, "marshal config: %v", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return exit(2, "write %s: %v", configPath, err)
	}

	fmt.Fprintf(a.Stdout, "Created %s\n\n", configPath)
	fmt.Fprintf(a.Stdout, "Next steps:\n")
	fmt.Fprintf(a.Stdout, "  1. Edit %s with your project settings\n", configPath)
	fmt.Fprintf(a.Stdout, "  2. Run `ciderbox compile-test` to test across distros\n")
	fmt.Fprintf(a.Stdout, "  3. Run `ciderbox build` to build in a container\n")

	return nil
}

// buildCommand runs the configured build command in a container.
func (a App) buildCommand(ctx context.Context, args []string) error {
	fs := newFlagSet("build", a.Stderr)
	configFile := fs.String("config", ".ciderbox.yaml", "path to ciderbox config")
	provider := fs.String("provider", "", "provider override")
	image := fs.String("image", "", "image override")

	if err := parseFlags(fs, args); err != nil {
		return err
	}

	cfg, err := readCiderboxConfig(*configFile)
	if err != nil {
		return exit(2, "build config: %v", err)
	}

	if cfg.Build.Command == "" {
		return exit(2, "no build command configured in %s", *configFile)
	}

	// Use configured provider/image or fallbacks
	p := cfg.Run.Provider
	if *provider != "" {
		p = *provider
	}
	if p == "" {
		p = "apple-container"
	}

	img := cfg.Build.Image
	if *image != "" {
		img = *image
	}
	if img == "" {
		img = cfg.Run.Image
	}
	if img == "" {
		img = "ubuntu:26.04"
	}

	fmt.Fprintf(a.Stdout, "=== Ciderbox Build ===\n")
	fmt.Fprintf(a.Stdout, "Project: %s\n", cfg.Project)
	fmt.Fprintf(a.Stdout, "Image:   %s\n", img)
	fmt.Fprintf(a.Stdout, "Command: %s\n\n", cfg.Build.Command)

	// Build run args
	runArgs := []string{
		"--provider", p,
		"--apple-container-image", img,
		"--keep",
		"--",
	}
	cmdParts := strings.Fields(cfg.Build.Command)
	runArgs = append(runArgs, cmdParts...)

	return a.runCommand(ctx, runArgs)
}

// runInContainer runs a command in a container with project defaults.
func (a App) runInContainer(ctx context.Context, args []string) error {
	fs := newFlagSet("run-container", a.Stderr)
	configFile := fs.String("config", ".ciderbox.yaml", "path to ciderbox config")

	if err := parseFlags(fs, args); err != nil {
		return err
	}

	// Read config for defaults
	cfg, err := readCiderboxConfig(*configFile)
	if err != nil {
		// Config optional for run-container
		cfg = &CiderboxConfig{}
	}

	// Build run args with project defaults
	var runArgs []string
	if cfg.Run.Provider != "" {
		runArgs = append(runArgs, "--provider", cfg.Run.Provider)
	}
	if cfg.Run.Image != "" {
		runArgs = append(runArgs, "--apple-container-image", cfg.Run.Image)
	}

	// Append user args
	runArgs = append(runArgs, args...)

	return a.runCommand(ctx, runArgs)
}
