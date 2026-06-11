package cli

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
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

	// Filter to ciderbox-managed containers only
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
	if v, ok := labels["ciderbox"]; ok && v == "true" {
		return true
	}
	return strings.HasPrefix(labels["lease_id"], "cbx_") || strings.HasPrefix(c.ID, "cbx_")
}

func shortCiderboxID(id string) string {
	if len(id) > 20 {
		return id[:20] + "..."
	}
	return id
}

// ---- run ----

type runFlags struct {
	provider string
	keep     bool
	image    string
	memory   string
	cpus     int
	user     string
	workRoot string
	noSync   bool
	env      map[string]string
}

func (a App) runCommand(ctx context.Context, args []string) error {
	fs := newFlagSet("run", a.Stderr)
	provider := fs.String("provider", "apple-container", "provider name")
	keep := fs.Bool("keep", false, "keep container after command")
	image := fs.String("apple-container-image", "", "container image override")
	memory := fs.String("apple-container-memory", "", "memory limit, e.g. 4G")
	cpus := fs.Int("apple-container-cpus", 0, "CPU limit; 0 leaves runtime default")
	user := fs.String("apple-container-user", "", "container user")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	command := fs.Args()
	if len(command) == 0 {
		return exit(2, "usage: ciderbox run [flags] -- <command...>")
	}

	// Resolve image
	runImage := *image
	if runImage == "" {
		cfg, _, err := a.providerConfigRuntime(*provider)
		if err == nil && cfg.AppleContainer.Image != "" {
			runImage = cfg.AppleContainer.Image
		}
	}
	if runImage == "" {
		runImage = "debian:bookworm"
	}

	// Build the container run args.
	// Use foreground mode (no -d) so output streams directly to the user.
	// --rm removes the container automatically after the command exits (unless --keep).
	runArgs := []string{"run"}
	if !*keep {
		runArgs = append(runArgs, "--rm")
	}
	runArgs = append(runArgs, buildContainerRunFlags(*memory, *cpus, *user)...)
	runArgs = append(runArgs, runImage)
	runArgs = append(runArgs, command...)

	fmt.Fprintf(a.Stderr, "running image=%s command=%s\n", runImage, strings.Join(command, " "))

	cmd := exec.CommandContext(ctx, "container", runArgs...)
	cmd.Stdout = a.Stdout
	cmd.Stderr = a.Stderr

	if err := cmd.Run(); err != nil {
		return exit(1, "run: command failed: %v", err)
	}
	return nil
}

func buildContainerRunFlags(memory string, cpus int, user string) []string {
	var flags []string
	if memory != "" {
		flags = append(flags, "--memory", memory)
	}
	if cpus > 0 {
		flags = append(flags, "--cpus", strconv.Itoa(cpus))
	}
	if user != "" {
		flags = append(flags, "--user", user)
	}
	flags = append(flags, "--label", "ciderbox=true")
	return flags
}

// ---- cleanup ----

func (a App) cleanup(ctx context.Context, args []string) error {
	fs := newFlagSet("cleanup", a.Stderr)
	provider := fs.String("provider", "apple-container", "provider name")
	dryRun := fs.Bool("dry-run", false, "show what would be removed")
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
	for _, c := range containers {
		if !isCiderboxContainer(c) {
			continue
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
		fmt.Fprintf(a.Stdout, "cleanup: removed %d container(s), checked %d\n", removed, len(containers))
	}
	return nil
}

// ---- compile-test ----

func (a App) compileTest(ctx context.Context, args []string) error {
	return exit(2, "compile-test: not implemented yet; use `ciderbox run` with your test command")
}

// ---- build ----

func (a App) buildCommand(ctx context.Context, args []string) error {
	return exit(2, "build: not implemented yet; use `ciderbox run` with your build command")
}

// ---- chop ----

func (a App) chopCommand(ctx context.Context, args []string) error {
	return a.cleanup(ctx, append(args, "--provider", "apple-container"))
}

// ---- helpers ----

// loadContainerRuntime loads the provider and returns its native ContainerRuntime.
// This is the primary execution path for local Apple containers, where SSH
// is unavailable due to Virtualization.framework network isolation.
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
