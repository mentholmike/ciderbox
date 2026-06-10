package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// CompileTestConfig defines the compile-test matrix for a ciderbox repo.
type CompileTestConfig struct {
	Distros  []DistroConfig `yaml:"distros"`
	Command  string         `yaml:"command"`
	Parallel bool           `yaml:"parallel"`
}

// DistroConfig defines a single target distro in the compile-test matrix.
type DistroConfig struct {
	Name  string `yaml:"name"`
	Image string `yaml:"image"`
}

// CompileTestResult holds the outcome for one distro run.
type CompileTestResult struct {
	Distro     string
	Image      string
	Success    bool
	Duration   time.Duration
	ExitCode   int
	Output     string
	Error      string
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
	cfg, err := readCompileTestConfig(*configFile)
	if err != nil {
		return exit(2, "compile-test config: %v", err)
	}

	if len(cfg.Distros) == 0 {
		return exit(2, "no distros configured in %s", *configFile)
	}

	if cfg.Command == "" {
		return exit(2, "no compile command configured in %s", *configFile)
	}

	fmt.Fprintf(a.Stdout, "=== Ciderbox Compile Test ===\n")
	fmt.Fprintf(a.Stdout, "Command: %s\n", cfg.Command)
	fmt.Fprintf(a.Stdout, "Distros:  %d\n", len(cfg.Distros))
	fmt.Fprintf(a.Stdout, "Mode:     %s\n\n", map[bool]string{true: "parallel", false: "sequential"}[*parallel])

	// Run tests
	results := make([]CompileTestResult, len(cfg.Distros))
	
	if *parallel {
		var wg sync.WaitGroup
		for i, distro := range cfg.Distros {
			wg.Add(1)
			go func(idx int, d DistroConfig) {
				defer wg.Done()
				results[idx] = a.runCompileTest(ctx, *provider, d, cfg.Command)
			}(i, distro)
		}
		wg.Wait()
	} else {
		for i, distro := range cfg.Distros {
			results[i] = a.runCompileTest(ctx, *provider, distro, cfg.Command)
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

func readCompileTestConfig(path string) (*CompileTestConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Parse only the compileTest section from the config
	var root struct {
		CompileTest CompileTestConfig `yaml:"compileTest"`
	}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}

	return &root.CompileTest, nil
}
