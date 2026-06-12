package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// ---- doctor ----

func (a App) doctor(ctx context.Context, args []string) error {
	fs := newFlagSet("doctor", a.Stderr)
	provider := fs.String("provider", "apple-container", "provider to check")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	cfg, rt, err := a.providerConfigRuntime(*provider)
	if err != nil {
		return exit(2, "doctor: %v", err)
	}

	p, err := ProviderFor(*provider)
	if err != nil {
		return exit(2, "doctor: %v", err)
	}

	backend, err := p.Configure(cfg, rt)
	if err != nil {
		return exit(2, "doctor: %v", err)
	}

	doctor, ok := backend.(DoctorBackend)
	if !ok {
		return exit(2, "doctor: provider %q does not support doctor", *provider)
	}

	result, err := doctor.Doctor(ctx, DoctorRequest{ProbeSSH: true})
	if err != nil {
		return exit(2, "doctor: %v", err)
	}

	fmt.Fprintf(a.Stdout, "provider=%s status=%s\n", result.Provider, blank(result.Status, "ok"))
	if result.Message != "" {
		fmt.Fprintf(a.Stdout, "%s\n", result.Message)
	}
	if len(result.Checks) > 0 {
		fmt.Fprintln(a.Stdout, "checks:")
		for _, check := range result.Checks {
			fmt.Fprintf(a.Stdout, "  [%s] %s", check.Status, check.Check)
			if check.Message != "" {
				fmt.Fprintf(a.Stdout, ": %s", check.Message)
			}
			fmt.Fprintln(a.Stdout)
		}
	}
	return nil
}

// ---- list ----

func (a App) list(ctx context.Context, args []string) error {
	fs := newFlagSet("list", a.Stderr)
	provider := fs.String("provider", "apple-container", "provider name")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	runtime, err := a.loadContainerRuntime(ctx, *provider)
	if err != nil {
		return exit(2, "list: %v", err)
	}

	containers, err := runtime.List(ctx, nil)
	if err != nil {
		return exit(2, "list: %v", err)
	}

	if len(containers) == 0 {
		fmt.Fprintln(a.Stdout, "no active ciderbox containers")
		return nil
	}

	var ours []ContainerInfo
	for _, c := range containers {
		if isCiderboxContainer(c) {
			ours = append(ours, c)
		}
	}

	if len(ours) == 0 {
		fmt.Fprintln(a.Stdout, "no active ciderbox containers")
		return nil
	}

	sort.Slice(ours, func(i, j int) bool {
		return ours[i].ID < ours[j].ID
	})

	fmt.Fprintf(a.Stdout, "%-28s %-10s %-16s %s\n", "ID", "STATUS", "IP", "IMAGE")
	fmt.Fprintln(a.Stdout, strings.Repeat("-", 64))
	for _, c := range ours {
		ip := c.IP
		if ip == "" {
			ip = "-"
		}
		status := c.Status
		if status == "" {
			status = "running"
		}
		fmt.Fprintf(a.Stdout, "%-28s %-10s %-16s %s\n", shortCiderboxID(c.ID), status, ip, c.Image)
	}
	return nil
}

func isCiderboxContainer(c ContainerInfo) bool {
	labels := c.Labels
	if labels == nil {
		return strings.HasPrefix(c.ID, "cbx_") || strings.HasPrefix(c.Name, "cbx_")
	}
	if v, ok := labels["ciderbox-protected"]; ok && v == "true" {
		return false
	}
	if v, ok := labels["ciderbox"]; ok && v == "true" {
		return true
	}
	return strings.HasPrefix(labels["lease_id"], "cbx_") || strings.HasPrefix(c.ID, "cbx_") || strings.HasPrefix(c.Name, "cbx_")
}

func shortCiderboxID(id string) string {
	if len(id) > 20 {
		return id[:20] + "..."
	}
	return id
}

// ---- run ----

func (a App) runCommand(ctx context.Context, args []string) error {
	opts, command, err := parseRunFlags(args)
	if err != nil {
		return err
	}
	if len(command) == 0 {
		return exit(2, "usage: ciderbox run [flags] -- <command...>")
	}

	// Resolve image
	if opts.image == "" {
		cfg, _, err := a.providerConfigRuntime(opts.provider)
		if err == nil && cfg.AppleContainer.Image != "" {
			opts.image = cfg.AppleContainer.Image
		}
	}
	if opts.image == "" {
		opts.image = "debian:bookworm"
	}

	// Resolve work root
	if opts.workRoot == "" {
		opts.workRoot = "/work/ciderbox"
	}

	// Get project name from cwd
	cwd, err := os.Getwd()
	if err != nil {
		return exit(2, "run: getwd: %v", err)
	}
	projectName := filepath.Base(cwd)
	if projectName == "." || projectName == "" {
		projectName = "project"
	}
	workDir := filepath.Join(opts.workRoot, projectName)

	// 1. Start container in detached mode with a long-lived process
	keepContainer := opts.keep || opts.noSync
	containerID, err := a.startContainer(ctx, opts, keepContainer)
	if err != nil {
		return exit(2, "run: start container: %v", err)
	}
	cleanupID := containerID // track for cleanup

	defer func() {
		if !keepContainer && cleanupID != "" {
			_ = a.removeContainer(ctx, cleanupID)
		}
	}()

	// 2. Create work root inside container
	if !opts.noSync {
		if err := a.containerExec(ctx, containerID, nil, a.Stderr,
			"mkdir", "-p", opts.workRoot); err != nil {
			return exit(2, "run: create workdir: %v", err)
		}
	}

	// 3. Sync workspace into container via tar pipe
	// container cp on macOS fails on root-owned files (e.g. /usr/bin/sudo),
	// so we tar the project dir (excluding large/binary artifacts) and pipe
	// it into the container via container exec -i.
	if !opts.noSync {
		if err := a.ensureSyncTools(ctx, containerID); err != nil {
			return exit(2, "run: install sync tools: %v", err)
		}
		fmt.Fprintf(a.Stderr, "syncing %s -> %s...\n", cwd, workDir)
		if err := a.syncWorkspace(ctx, containerID, cwd, workDir); err != nil {
			return exit(2, "run: sync workspace: %v", err)
		}
	}

	// 4. Install dependencies
	if len(opts.dependencies) > 0 {
		depsDir := "/tmp/ciderbox-deps"
		if err := a.containerExec(ctx, containerID, nil, a.Stderr,
			"mkdir", "-p", depsDir); err != nil {
			return exit(2, "run: create deps dir: %v", err)
		}
		depCmd := packageInstallScript(opts.dependencies)
		fmt.Fprintf(a.Stderr, "installing dependencies: %s\n", strings.Join(opts.dependencies, " "))
		if err := a.containerExec(ctx, containerID, nil, a.Stderr,
			"sh", "-c", depCmd); err != nil {
			// Non-fatal: dep install might fail on some images
			fmt.Fprintf(a.Stderr, "warning: dep install failed (continuing): %v\n", err)
		}
	}

	// 5. Build and exec the command in the workdir
	var runCmd []string
	if !opts.noSync {
		// Wrap command to cd into workdir first
		runCmd = []string{"sh", "-c", fmt.Sprintf("cd %s && exec %s",
			shQuote(workDir), strings.Join(shQuoteAll(command), " "))}
	} else {
		runCmd = command
	}

	fmt.Fprintf(a.Stderr, "running in %s @ %s\n", workDir, containerID)
	if err := a.containerExec(ctx, containerID, a.Stdout, a.Stderr, runCmd...); err != nil {
		return exit(1, "run: command exited with error: %v", err)
	}

	return nil
}

// ---- compile-test ----

func (a App) compileTest(ctx context.Context, args []string) error {
	// Read .ciderbox.yaml for compile-test config
	cfg, err := loadConfig()
	if err == nil && len(cfg.CompileTest.Distros) > 0 {
		return a.runMultiDistro(ctx, args, cfg)
	}

	// Fallback: single run with default test command
	command := "go test ./..."
	if err == nil && cfg.Commands.Test != "" {
		command = cfg.Commands.Test
	}

	fmt.Fprintf(a.Stderr, "compile-test: running %s\n", command)

	// Auto-install Go if the command starts with 'go'
	flags := []string{"--provider", "apple-container"}
	if command == "go test ./..." || strings.HasPrefix(command, "go ") {
		flags = append(flags, "--dep", "golang")
	}

	return a.runCommand(ctx, append(flags, shellCommand(command)...))
}

func (a App) runMultiDistro(ctx context.Context, args []string, cfg Config) error {
	// Build dep flags from config
	depFlags := []string{"--provider", "apple-container"}
	if len(args) > 0 {
		// User provided a command override
		depFlags = append(depFlags, "--")
		depFlags = append(depFlags, args...)
	} else {
		command := cfg.CompileTest.Command
		if command == "" {
			command = cfg.Commands.Test
		}
		if command == "" {
			command = "go test ./..."
		}
		for _, dep := range compileTestDependencies(cfg.CompileTest) {
			depFlags = append(depFlags, "--dep", dep)
		}
		depFlags = append(depFlags, "--")
		depFlags = append(depFlags, shellCommand(command)...)
	}

	if cfg.CompileTest.Parallel {
		fmt.Fprintf(a.Stderr, "compile-test: running across %d distros (parallel)\n", len(cfg.CompileTest.Distros))
		return a.runDistrosParallel(ctx, cfg.CompileTest.Distros, depFlags)
	}

	// Sequential across distros
	fmt.Fprintf(a.Stderr, "compile-test: running across %d distros (sequential)\n", len(cfg.CompileTest.Distros))
	for _, distro := range cfg.CompileTest.Distros {
		fmt.Fprintf(a.Stderr, "\n--- %s (%s) ---\n", distro.Name, distro.Image)
		distroFlags := make([]string, len(depFlags))
		copy(distroFlags, depFlags)
		distroFlags = distroRunFlags(distroFlags, distro.Image)
		if err := a.runCommand(ctx, distroFlags); err != nil {
			return fmt.Errorf("distro %s failed: %w", distro.Name, err)
		}
	}
	return nil
}

func compileTestDependencies(cfg CompileTestConfig) []string {
	deps := make([]string, 0, len(cfg.Deps)+len(cfg.Dependencies))
	deps = append(deps, cfg.Deps...)
	deps = append(deps, cfg.Dependencies...)
	return deps
}

func distroRunFlags(baseFlags []string, image string) []string {
	flags := []string{"--apple-container-image", image}
	return append(flags, baseFlags...)
}

func (a App) runDistrosParallel(ctx context.Context, distros []DistroConfig, baseFlags []string) error {
	type result struct {
		index  int
		name   string
		image  string
		stdout string
		stderr string
		err    error
	}

	results := make([]result, len(distros))
	var wg sync.WaitGroup
	for i, distro := range distros {
		i, distro := i, distro
		wg.Add(1)
		go func() {
			defer wg.Done()
			var stdout, stderr bytes.Buffer
			distroFlags := make([]string, len(baseFlags))
			copy(distroFlags, baseFlags)
			distroFlags = distroRunFlags(distroFlags, distro.Image)
			err := (App{Stdout: &stdout, Stderr: &stderr, Stdin: a.Stdin}).runCommand(ctx, distroFlags)
			results[i] = result{
				index:  i,
				name:   distro.Name,
				image:  distro.Image,
				stdout: stdout.String(),
				stderr: stderr.String(),
				err:    err,
			}
		}()
	}
	wg.Wait()

	var failed []string
	for _, r := range results {
		fmt.Fprintf(a.Stderr, "\n--- %s (%s) ---\n", r.name, r.image)
		if r.stderr != "" {
			fmt.Fprint(a.Stderr, r.stderr)
		}
		if r.stdout != "" {
			fmt.Fprint(a.Stdout, r.stdout)
		}
		if r.err != nil {
			fmt.Fprintf(a.Stderr, "FAILED %s: %v\n", r.name, r.err)
			failed = append(failed, r.name)
		} else {
			fmt.Fprintf(a.Stderr, "PASS %s\n", r.name)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("distros failed: %s", strings.Join(failed, ", "))
	}
	return nil
}

// ---- build ----

func (a App) buildCommand(ctx context.Context, args []string) error {
	cfg, err := loadConfig()
	command := "go build ./..."
	if err == nil && cfg.Commands.Build != "" {
		command = cfg.Commands.Build
	}

	fmt.Fprintf(a.Stderr, "build: running %s\n", command)

	// Auto-install Go if the command starts with 'go'
	flags := []string{"--provider", "apple-container"}
	if command == "go build ./..." || strings.HasPrefix(command, "go ") {
		flags = append(flags, "--dep", "golang")
	}

	return a.runCommand(ctx, append(flags, shellCommand(command)...))
}

// ---- chop ----

func (a App) chopCommand(ctx context.Context, args []string) error {
	return a.cleanup(ctx, append(args, "--provider", "apple-container"))
}

// ---- cleanup ----

func (a App) cleanup(ctx context.Context, args []string) error {
	fs := newFlagSet("cleanup", a.Stderr)
	provider := fs.String("provider", "apple-container", "provider name")
	dryRun := fs.Bool("dry-run", false, "show what would be removed")
	force := fs.Bool("force", false, "remove protected containers too")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	runtime, err := a.loadContainerRuntime(ctx, *provider)
	if err != nil {
		return exit(2, "cleanup: %v", err)
	}

	containers, err := runtime.List(ctx, nil)
	if err != nil {
		return exit(2, "cleanup: %v", err)
	}

	removed := 0
	skipped := 0
	for _, c := range containers {
		if !isCiderboxContainer(c) {
			// Check if it's protected and we're not forcing
			labels := c.Labels
			if labels != nil {
				if v, ok := labels["ciderbox-protected"]; ok && v == "true" {
					if !*force {
						if *dryRun {
							fmt.Fprintf(a.Stdout, "would skip protected container=%s (use --force to remove)\n", c.ID)
						} else {
							fmt.Fprintf(a.Stdout, "skipping protected container=%s\n", c.ID)
						}
						skipped++
						continue
					}
					// Force: treat as ciderbox container and remove it
				} else {
					continue
				}
			} else {
				continue
			}
		}
		if *dryRun {
			fmt.Fprintf(a.Stdout, "would remove container=%s image=%s\n", c.ID, c.Image)
			continue
		}
		if err := runtime.Remove(ctx, c.ID, true); err != nil {
			fmt.Fprintf(a.Stderr, "remove container=%s: %v\n", c.ID, err)
			continue
		}
		fmt.Fprintf(a.Stdout, "removed container=%s image=%s\n", c.ID, c.Image)
		removed++
	}

	if !*dryRun {
		fmt.Fprintf(a.Stdout, "cleanup: removed %d container(s), skipped %d protected, checked %d\n", removed, skipped, len(containers))
	}
	return nil
}

// ---- helpers ----

type runOptions struct {
	provider     string
	image        string
	memory       string
	cpus         int
	user         string
	workRoot     string
	keep         bool
	noSync       bool
	dependencies []string
	name         string
	extraLabels  map[string]string
	volumes      []string
	ports        []string
}

func parseRunFlags(args []string) (runOptions, []string, error) {
	var opts runOptions
	opts.provider = "apple-container"

	// Simple manual flag parsing since flag.FlagSet can't handle
	// the `-- <command>` pattern cleanly
	var command []string
	afterSep := false
	for i := 0; i < len(args); i++ {
		if afterSep {
			command = append(command, args[i])
			continue
		}
		if args[i] == "--" {
			afterSep = true
			continue
		}
		switch {
		case args[i] == "--provider" || args[i] == "-p":
			if i+1 < len(args) {
				i++
				opts.provider = args[i]
			}
		case args[i] == "--apple-container-image" || strings.HasPrefix(args[i], "--apple-container-image="):
			if v, ok := splitFlag(args[i]); ok {
				opts.image = v
			} else if i+1 < len(args) {
				i++
				opts.image = args[i]
			}
		case args[i] == "--apple-container-memory" || strings.HasPrefix(args[i], "--apple-container-memory="):
			if v, ok := splitFlag(args[i]); ok {
				opts.memory = v
			} else if i+1 < len(args) {
				i++
				opts.memory = args[i]
			}
		case args[i] == "--apple-container-cpus" || strings.HasPrefix(args[i], "--apple-container-cpus="):
			if v, ok := splitFlag(args[i]); ok {
				opts.cpus, _ = strconv.Atoi(v)
			} else if i+1 < len(args) {
				i++
				opts.cpus, _ = strconv.Atoi(args[i])
			}
		case args[i] == "--apple-container-user" || strings.HasPrefix(args[i], "--apple-container-user="):
			if v, ok := splitFlag(args[i]); ok {
				opts.user = v
			} else if i+1 < len(args) {
				i++
				opts.user = args[i]
			}
		case args[i] == "--apple-container-work-root" || strings.HasPrefix(args[i], "--apple-container-work-root="):
			if v, ok := splitFlag(args[i]); ok {
				opts.workRoot = v
			} else if i+1 < len(args) {
				i++
				opts.workRoot = args[i]
			}
		case args[i] == "--keep":
			opts.keep = true
		case args[i] == "--name":
			if i+1 < len(args) {
				i++
				opts.name = args[i]
			}
		case args[i] == "--label":
			if i+1 < len(args) {
				i++
				parts := strings.SplitN(args[i], "=", 2)
				if len(parts) == 2 {
					if opts.extraLabels == nil {
						opts.extraLabels = make(map[string]string)
					}
					opts.extraLabels[parts[0]] = parts[1]
				}
			}
		case args[i] == "--no-sync":
			opts.noSync = true
		case args[i] == "--dependency" || args[i] == "-d" || args[i] == "--dep":
			if i+1 < len(args) {
				i++
				opts.dependencies = append(opts.dependencies, args[i])
			}
		case args[i] == "--volume" || args[i] == "-v":
			if i+1 < len(args) {
				i++
				opts.volumes = append(opts.volumes, args[i])
			}
		case args[i] == "--port" || args[i] == "-p":
			if i+1 < len(args) {
				i++
				opts.ports = append(opts.ports, args[i])
			}
		case args[i] == "--env" || args[i] == "-e":
			i++ // skip, not implemented for run yet
		case args[i] == "-h" || args[i] == "--help":
			printRunHelp()
			return opts, nil, ExitError{Code: 0}
		default:
			// Assume remaining args are the command
			command = append(command, args[i:]...)
			return opts, command, nil
		}
	}
	return opts, command, nil
}

func splitFlag(flag string) (string, bool) {
	idx := strings.Index(flag, "=")
	if idx > 0 && idx < len(flag)-1 {
		return flag[idx+1:], true
	}
	return "", false
}

func printRunHelp() {
	fmt.Fprintln(os.Stderr, `Usage of run:
  -provider string                provider name (default "apple-container")
  --apple-container-image string  container image override
  --apple-container-memory string memory limit, e.g. 4G
  --apple-container-cpus int      CPU limit; 0 leaves runtime default
  --apple-container-user string   container user
  --apple-container-work-root string
                                  container workspace root (default "/work/ciderbox")
  --keep                          keep container after command exits
  --name string                   container name
  --label string                  add a label (key=value, repeatable)
  --volume string                 mount a volume (host:container, repeatable)
  --port string                   publish a port (host:container, repeatable)
  --no-sync                       do not copy current directory into container
  --dep, --dependency string      system package to install (repeatable)
  -h, --help                      show this help`)
}

// startContainer runs a detached container with a long-lived process.
func (a App) startContainer(ctx context.Context, opts runOptions, keepContainer bool) (string, error) {
	runArgs := []string{"run", "-d"}
	if !keepContainer {
		runArgs = append(runArgs, "--rm")
	}
	if opts.name != "" {
		runArgs = append(runArgs, "--name", opts.name)
	}
	if opts.memory != "" {
		runArgs = append(runArgs, "--memory", opts.memory)
	}
	if opts.cpus > 0 {
		runArgs = append(runArgs, "--cpus", strconv.Itoa(opts.cpus))
	}
	if opts.user != "" {
		runArgs = append(runArgs, "--user", opts.user)
	}
	for _, vol := range opts.volumes {
		runArgs = append(runArgs, "--volume", vol)
	}
	for _, port := range opts.ports {
		runArgs = append(runArgs, "-p", port)
	}
	for k, v := range opts.extraLabels {
		runArgs = append(runArgs, "--label", k+"="+v)
	}
	runArgs = append(runArgs, "--label", "ciderbox=true")
	runArgs = append(runArgs, opts.image)
	runArgs = append(runArgs, "sleep", "infinity")

	fmt.Fprintf(a.Stderr, "starting container image=%s\n", opts.image)
	cmd := exec.CommandContext(ctx, "container", runArgs...)
	cmd.Stderr = a.Stderr
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("container run: %w", err)
	}
	id := strings.TrimSpace(string(output))
	if id == "" {
		return "", fmt.Errorf("container run succeeded but no container ID returned")
	}
	fmt.Fprintf(a.Stderr, "started container=%s\n", id)
	return id, nil
}

func (a App) ensureSyncTools(ctx context.Context, containerID string) error {
	script := strings.Join([]string{
		"if command -v tar >/dev/null 2>&1 && command -v gzip >/dev/null 2>&1; then exit 0; fi",
		packageInstallScript([]string{"tar", "gzip"}),
	}, "\n")
	return a.containerExec(ctx, containerID, nil, a.Stderr, "sh", "-c", script)
}

func shellCommand(command string) []string {
	return []string{"sh", "-c", command}
}

func packageInstallScript(packages []string) string {
	return strings.Join([]string{
		"set -e",
		"if command -v apt-get >/dev/null 2>&1; then",
		fmt.Sprintf("  apt-get update && apt-get install -y --no-install-recommends %s", packageList("apt", packages)),
		"elif command -v apk >/dev/null 2>&1; then",
		fmt.Sprintf("  apk add --no-cache %s", packageList("apk", packages)),
		"elif command -v dnf >/dev/null 2>&1; then",
		fmt.Sprintf("  dnf install -y %s", packageList("dnf", packages)),
		"elif command -v microdnf >/dev/null 2>&1; then",
		fmt.Sprintf("  microdnf install -y %s", packageList("microdnf", packages)),
		"elif command -v yum >/dev/null 2>&1; then",
		fmt.Sprintf("  yum install -y %s", packageList("yum", packages)),
		"elif command -v pacman >/dev/null 2>&1; then",
		fmt.Sprintf("  pacman -Sy --noconfirm %s", packageList("pacman", packages)),
		"elif command -v zypper >/dev/null 2>&1; then",
		fmt.Sprintf("  zypper --non-interactive refresh && zypper --non-interactive install --no-recommends %s", packageList("zypper", packages)),
		"else",
		"  echo 'no supported package manager found (apt-get, apk, dnf, microdnf, yum, pacman, zypper)' >&2",
		"  exit 127",
		"fi",
	}, "\n")
}

func packageList(manager string, packages []string) string {
	mapped := make([]string, 0, len(packages))
	seen := make(map[string]bool)
	for _, pkg := range packages {
		pkg = mapPackageName(manager, strings.TrimSpace(pkg))
		if pkg == "" || seen[pkg] {
			continue
		}
		seen[pkg] = true
		mapped = append(mapped, pkg)
	}
	return strings.Join(shQuoteAll(mapped), " ")
}

func mapPackageName(manager, pkg string) string {
	switch pkg {
	case "curl":
		switch manager {
		case "dnf", "microdnf":
			return "curl-minimal"
		}
	case "golang":
		switch manager {
		case "apk", "pacman", "zypper":
			return "go"
		}
	case "go":
		switch manager {
		case "apt":
			return "golang-go"
		case "dnf", "microdnf", "yum":
			return "golang"
		}
	case "node":
		switch manager {
		case "apt":
			return "nodejs"
		case "apk":
			return "nodejs"
		case "dnf", "microdnf", "yum":
			return "nodejs"
		}
	case "npm":
		switch manager {
		case "apt":
			return "npm"
		}
	case "cargo":
		switch manager {
		case "apt":
			return "cargo"
		case "dnf", "microdnf", "yum":
			return "rust-cargo"
		}
	case "make":
		switch manager {
		case "apt":
			return "build-essential"
		}
	case "python3":
		if manager == "pacman" {
			return "python"
		}
	case "xz":
		if manager == "apt" {
			return "xz-utils"
		}
	}
	return pkg
}

// removeContainer removes a container by ID.
func (a App) removeContainer(ctx context.Context, id string) error {
	rmArgs := []string{"rm", "-f", id}
	cmd := exec.CommandContext(ctx, "container", rmArgs...)
	cmd.Stderr = a.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("container rm %s: %w", id, err)
	}
	fmt.Fprintf(a.Stderr, "removed container=%s\n", id)
	return nil
}

// containerCopy copies a local path into a container.
// syncWorkspace tars the project directory (excluding .git, ciderbox binary,
// .crabbox, and other large artifacts) and pipes it into the container.
// Uses tar | container exec -i tar to avoid macOS file permission issues
// with the native container cp command.
func (a App) syncWorkspace(ctx context.Context, containerID, srcDir, dstDir string) error {
	// Create destination
	if err := a.containerExec(ctx, containerID, nil, a.Stderr,
		"mkdir", "-p", dstDir); err != nil {
		return fmt.Errorf("create dest %s: %w", dstDir, err)
	}

	// Build the tar command: archive srcDir contents to stdout, excluding big stuff
	// --no-xattrs suppresses macOS extended attribute noise
	tarArgs := []string{
		"czf", "-",
		"--no-xattrs",
		"--exclude", ".git",
		"--exclude", ".crabbox",
		"--exclude", ".agents",
		"--exclude", "node_modules",
		"--exclude", "vendor",
		"--exclude", "target",
		"--exclude", "ciderbox",
		"--exclude", "*.tar.gz",
		"--exclude", "*.tar",
		"--exclude", ".DS_Store",
		"-C", srcDir, ".",
	}
	tarCmd := exec.CommandContext(ctx, "tar", tarArgs...)

	// Pipe into container exec -i tar
	untarArgs := []string{"exec", "-i", containerID,
		"tar", "xzf", "-", "-C", dstDir}
	untarCmd := exec.CommandContext(ctx, "container", untarArgs...)

	// Wire: tar stdout -> container exec stdin
	untarCmd.Stdin, _ = tarCmd.StdoutPipe()
	untarCmd.Stderr = a.Stderr

	if err := untarCmd.Start(); err != nil {
		return fmt.Errorf("start untar: %w", err)
	}
	if err := tarCmd.Run(); err != nil {
		return fmt.Errorf("tar %s: %w", srcDir, err)
	}
	if err := untarCmd.Wait(); err != nil {
		return fmt.Errorf("untar to %s: %w", dstDir, err)
	}

	return nil
}

// containerExec runs a command inside a running container.
func (a App) containerExec(ctx context.Context, containerID string, stdout, stderr io.Writer, command ...string) error {
	execArgs := append([]string{"exec", containerID}, command...)
	cmd := exec.CommandContext(ctx, "container", execArgs...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// loadContainerRuntime loads the provider and returns its native ContainerRuntime.
func (a App) loadContainerRuntime(ctx context.Context, providerName string) (ContainerRuntime, error) {
	cfg, rt, err := a.providerConfigRuntime(providerName)
	if err != nil {
		return nil, err
	}
	p, err := ProviderFor(providerName)
	if err != nil {
		return nil, err
	}
	backend, err := p.Configure(cfg, rt)
	if err != nil {
		return nil, err
	}
	crBackend, ok := backend.(ContainerRuntimeBackend)
	if !ok {
		return nil, fmt.Errorf("provider %q does not expose native ContainerRuntime", providerName)
	}
	return crBackend.ContainerRuntime()
}

// ---- shell quoting helpers ----

func shQuote(s string) string {
	if strings.ContainsAny(s, " \t\n'\"!$`\\|&;<>(){}[]*?~") || s == "" {
		return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
	}
	return s
}

func shQuoteAll(parts []string) []string {
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = shQuote(p)
	}
	return out
}
