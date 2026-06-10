package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// bufferedWriter captures output for a single distro and flushes on completion.
type bufferedWriter struct {
	name   string
	prefix string
	buf    *strings.Builder
	mu     sync.Mutex
}

func newBufferedWriter(name string) *bufferedWriter {
	return &bufferedWriter{
		name:   name,
		prefix: fmt.Sprintf("[%s] ", name),
		buf:    &strings.Builder{},
	}
}

func (w *bufferedWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	// Prefix each line with the distro name for clean interleaved output
	lines := strings.Split(string(p), "\n")
	for i, line := range lines {
		if i > 0 {
			w.buf.WriteByte('\n')
		}
		if line != "" {
			w.buf.WriteString(w.prefix)
			w.buf.WriteString(line)
		}
	}
	return len(p), nil
}

func (w *bufferedWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// CiderboxConfig is the full repo-level config for ciderbox projects.
// Lives at .ciderbox.yaml in the project root.
type CiderboxConfig struct {
	Project     string              `yaml:"project"`
	CompileTest CompileTestConfig   `yaml:"compileTest"`
	Build       BuildConfig         `yaml:"build"`
	Run         CiderboxRunSettings `yaml:"run"`
}

// CompileTestConfig defines the compile-test matrix for a ciderbox repo.
type CompileTestConfig struct {
	Distros      []DistroConfig `yaml:"distros"`
	Command      string         `yaml:"command"`
	Parallel     bool           `yaml:"parallel"`
	Dependencies []string       `yaml:"dependencies,omitempty"`
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

// ciderboxProtectedLabel is the container label that prevents chop from
// removing a lease unless --force is passed. Think of it as the "spiced"
// cider — too precious to waste.
const ciderboxProtectedLabel = "ciderbox-protected"

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
	// Per-distro output buffers for clean parallel output
	buffers := make([]*bufferedWriter, len(cfg.CompileTest.Distros))
	for i := range cfg.CompileTest.Distros {
		buffers[i] = newBufferedWriter(cfg.CompileTest.Distros[i].Name)
	}
	if *parallel {
		var wg sync.WaitGroup
		for i, distro := range cfg.CompileTest.Distros {
			wg.Add(1)
			go func(idx int, d DistroConfig) {
				defer wg.Done()
				results[idx] = a.runCompileTest(ctx, *provider, d, command, cfg.CompileTest.Dependencies, buffers[idx])
			}(i, distro)
		}
		wg.Wait()
		// Flush all buffered output after completion
		for _, buf := range buffers {
			if s := buf.String(); s != "" {
				fmt.Fprintln(a.Stderr, s)
			}
		}
	} else {
		for i, distro := range cfg.CompileTest.Distros {
			results[i] = a.runCompileTest(ctx, *provider, distro, command, cfg.CompileTest.Dependencies, nil)
		}
	}
	// Display results
	a.displayCompileTestResults(results)
	return nil
}

func (a App) runCompileTest(ctx context.Context, provider string, distro DistroConfig, command string, dependencies []string, buf *bufferedWriter) CompileTestResult {
	result := CompileTestResult{
		Distro: distro.Name,
		Image:  distro.Image,
	}
	start := time.Now()
	// Build ciderbox run args (don't include "run" — runCommand handles that)
	cmdStr := command

	// If dependencies are configured, prepend a package-manager-aware install
	// step so non-Debian distros (Alpine, Fedora, Arch) work too.
	if len(dependencies) > 0 {
		cmdStr = depInstallSnippet(dependencies) + " && " + command
	}

	// No --keep and no protection label: `run`'s normal lifecycle acquires a
	// fresh lease and releases it when the command finishes (success or
	// failure), so compile-test never leaks VMs and never has to discover
	// and delete leases after the fact.
	args := []string{
		"--provider", provider,
		"--apple-container-image", distro.Image,
		"--",
		"/bin/sh", "-lc", cmdStr,
	}
	// Use buffered writer for parallel mode, direct stderr for sequential
	var outWriter io.Writer
	if buf != nil {
		outWriter = buf
	} else {
		outWriter = a.Stderr
	}
	fmt.Fprintf(outWriter, "[%s] starting...\n", distro.Name)
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
		fmt.Fprintf(outWriter, "[%s] FAILED (%s)\n", distro.Name, result.Duration)
	} else {
		result.Success = true
		result.ExitCode = 0
		fmt.Fprintf(outWriter, "[%s] PASSED (%s)\n", distro.Name, result.Duration)
	}
	return result
}

// depInstallSnippet returns a shell snippet that installs the given packages
// using whichever package manager the distro provides. Falls back to a clear
// error when none is found instead of failing on a missing apt-get.
func depInstallSnippet(deps []string) string {
	pkgs := strings.Join(deps, " ")
	return fmt.Sprintf(
		`if command -v apt-get >/dev/null 2>&1; then apt-get update && apt-get install -y --no-install-recommends %[1]s; `+
			`elif command -v apk >/dev/null 2>&1; then apk add --no-cache %[1]s; `+
			`elif command -v dnf >/dev/null 2>&1; then dnf install -y %[1]s; `+
			`elif command -v pacman >/dev/null 2>&1; then pacman -Sy --noconfirm %[1]s; `+
			`else echo "ciderbox: no supported package manager (apt-get/apk/dnf/pacman) found" >&2; exit 1; fi`,
		pkgs)
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

// providerConfigRuntime returns a Config and Runtime for a given provider name.
// Uses the same loading logic as `run` command.
func (a App) providerConfigRuntime(provider string) (Config, Runtime, error) {
	cfg, err := loadConfig()
	if err != nil {
		return Config{}, Runtime{}, err
	}
	cfg.Provider = provider
	canonicalizeConfigProvider(&cfg)
	rt := runtimeForApp(a)
	return cfg, rt, nil
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
			Command:      "make test",
			Parallel:     true,
			Dependencies: []string{"build-essential", "git"},
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
	keep := fs.Bool("keep", false, "keep the build container after completion")

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
	fmt.Fprintf(a.Stdout, "Command: %s\n", cfg.Build.Command)
	if len(cfg.Build.Dependencies) > 0 {
		fmt.Fprintf(a.Stdout, "Deps:    %s\n", strings.Join(cfg.Build.Dependencies, ", "))
	}
	fmt.Fprintln(a.Stdout)

	// Build run args
	cmdStr := cfg.Build.Command
	if len(cfg.Build.Dependencies) > 0 {
		cmdStr = depInstallSnippet(cfg.Build.Dependencies) + " && " + cfg.Build.Command
	}

	var runArgs []string
	runArgs = append(runArgs, "--provider", p)
	runArgs = append(runArgs, "--apple-container-image", img)
	if *keep {
		// Keep the build container and mark it protected so `chop` skips it
		// unless --force is passed. The protection label must be a real
		// container label, so it goes through the provider's extra run args.
		runArgs = append(runArgs, "--keep")
		runArgs = append(runArgs, "--apple-container-extra-run-args", "--label "+ciderboxProtectedLabel+"=true")
	}
	// Default (no --keep): run's normal lifecycle releases the lease when the
	// command finishes, so ephemeral build containers never accumulate.
	runArgs = append(runArgs, "--")
	runArgs = append(runArgs, "/bin/sh", "-lc", cmdStr)

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

// chopCommand terminates active ciderbox leases.
// By default, protected leases (ciderbox-protected=true) are preserved
// unless --force is passed. Named after the cider-making process.
func (a App) chopCommand(ctx context.Context, args []string) error {
	fs := newFlagSet("chop", a.Stderr)
	provider := fs.String("provider", "apple-container", "provider to chop")
	yes := fs.Bool("yes", false, "skip confirmation prompt")
	force := fs.Bool("force", false, "chop protected leases too (ciderbox-protected)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	return a.chopViaBackend(ctx, *provider, *yes, *force)
}

// chopViaBackend uses the provider's CleanupBackend to properly release leases.
// Respects the ciderboxProtectedLabel unless --force is passed.
func (a App) chopViaBackend(ctx context.Context, providerName string, yes, force bool) error {
	cfg, rt, err := a.providerConfigRuntime(providerName)
	if err != nil {
		return exit(2, "chop: %v", err)
	}
	p, err := ProviderFor(providerName)
	if err != nil {
		return exit(2, "chop: provider %q not found", providerName)
	}
	backend, err := p.Configure(cfg, rt)
	if err != nil {
		return exit(2, "chop: configure provider: %v", err)
	}
	sshBackend, ok := backend.(SSHLeaseBackend)
	if !ok {
		return exit(2, "chop: provider %q does not support SSH leases", providerName)
	}
	leases, err := sshBackend.List(ctx, ListRequest{})
	if err != nil {
		return exit(2, "chop: list leases: %v", err)
	}
	var toChop []LeaseView
	var protected []LeaseView
	for _, lease := range leases {
		if lease.Labels["crabbox"] != "true" {
			continue
		}
		if lease.Labels[ciderboxProtectedLabel] == "true" && !force {
			protected = append(protected, lease)
			continue
		}
		toChop = append(toChop, lease)
	}
	if len(toChop) == 0 && len(protected) == 0 {
		fmt.Fprintf(a.Stdout, "No active ciderbox containers found.\n")
		return nil
	}
	fmt.Fprintf(a.Stdout, "=== Ciderbox Chop ===\n")
	if len(protected) > 0 {
		fmt.Fprintf(a.Stdout, "Protected %d container(s) (use --force to chop):\n", len(protected))
		for _, lease := range protected {
			fmt.Fprintf(a.Stdout, "  🍎 %s (protected)\n", lease.Name)
		}
	}
	if len(toChop) == 0 {
		fmt.Fprintf(a.Stdout, "\nNothing to chop. Protected leases remain.\n")
		return nil
	}
	fmt.Fprintf(a.Stdout, "\nChopping %d container(s):\n", len(toChop))
	for _, lease := range toChop {
		fmt.Fprintf(a.Stdout, "  - %s\n", lease.Name)
	}
	if !yes {
		fmt.Fprintf(a.Stdout, "\nChop all listed containers? [y/N] ")
		var response string
		fmt.Fscanln(a.Stdin, &response)
		if response != "y" && response != "Y" {
			fmt.Fprintf(a.Stdout, "Aborted.\n")
			return nil
		}
	}
	fmt.Fprintf(a.Stdout, "\nChopping...\n")
	chopped := 0
	failed := 0
	for _, lease := range toChop {
		target := LeaseTarget{
			Server:  lease,
			LeaseID: lease.Labels["lease"],
		}
		if err := sshBackend.ReleaseLease(ctx, ReleaseLeaseRequest{Lease: target}); err != nil {
			fmt.Fprintf(a.Stderr, "  ✗ %s: %v\n", lease.Name, err)
			failed++
		} else {
			fmt.Fprintf(a.Stdout, "  ✓ %s chopped\n", lease.Name)
			chopped++
		}
	}
	fmt.Fprintf(a.Stdout, "\nChopped %d/%d containers.", chopped, len(toChop))
	if len(protected) > 0 {
		fmt.Fprintf(a.Stdout, " %d protected.", len(protected))
	}
	fmt.Fprintln(a.Stdout)
	if failed > 0 {
		return exit(1, "%d container(s) failed to chop", failed)
	}
	return nil
}
