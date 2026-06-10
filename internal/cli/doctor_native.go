package cli

import (
	"context"
	"fmt"
)

func (a App) doctor(ctx context.Context, args []string) error {
	fs := newFlagSet("doctor", a.Stderr)
	providerName := fs.String("provider", "apple-container", "provider to check")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	cfg, rt, err := a.providerConfigRuntime(*providerName)
	if err != nil {
		return exit(2, "doctor: config: %v", err)
	}

	provider, err := ProviderFor(*providerName)
	if err != nil {
		return exit(2, "doctor: provider %q not found", *providerName)
	}

	backend, err := provider.Configure(cfg, rt)
	if err != nil {
		return exit(2, "doctor: configure provider: %v", err)
	}

	doctor, ok := backend.(DoctorBackend)
	if !ok {
		return exit(2, "doctor: provider %q does not support doctor", *providerName)
	}

	result, err := doctor.Doctor(ctx, DoctorRequest{})
	if err != nil {
		return err
	}

	fmt.Fprintf(a.Stdout, "%s: %s\n", result.Provider, result.Message)
	return nil
}

func (a App) list(ctx context.Context, args []string) error {
	fs := newFlagSet("list", a.Stderr)
	providerName := fs.String("provider", "apple-container", "provider to list")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	cfg, rt, err := a.providerConfigRuntime(*providerName)
	if err != nil {
		return exit(2, "list: config: %v", err)
	}

	containerRuntime, err := a.nativeContainerRuntime(*providerName, cfg, rt)
	if err != nil {
		return exit(2, "list: runtime: %v", err)
	}

	containers, err := containerRuntime.List(ctx, map[string]string{"ciderbox": "true"})
	if err != nil {
		return exit(2, "list: %v", err)
	}

	if len(containers) == 0 {
		fmt.Fprintln(a.Stdout, "No active ciderbox containers found.")
		return nil
	}

	fmt.Fprintf(a.Stdout, "%-32s %-12s %-16s %s\n", "ID", "STATUS", "IP", "NAME")
	for _, c := range containers {
		name := c.Labels["project"]
		if name == "" {
			name = c.Labels["tree.name"]
		}
		if name == "" {
			name = c.Name
		}
		fmt.Fprintf(a.Stdout, "%-32s %-12s %-16s %s\n", c.ID, c.Status, c.IP, name)
	}

	return nil
}

func (a App) cleanup(ctx context.Context, args []string) error {
	fs := newFlagSet("cleanup", a.Stderr)
	providerName := fs.String("provider", "apple-container", "provider to clean")
	dryRun := fs.Bool("dry-run", false, "print what would be removed")
	force := fs.Bool("force", false, "remove protected containers too")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	cfg, rt, err := a.providerConfigRuntime(*providerName)
	if err != nil {
		return exit(2, "cleanup: config: %v", err)
	}

	containerRuntime, err := a.nativeContainerRuntime(*providerName, cfg, rt)
	if err != nil {
		return exit(2, "cleanup: runtime: %v", err)
	}

	containers, err := containerRuntime.List(ctx, map[string]string{"ciderbox": "true"})
	if err != nil {
		return exit(2, "cleanup: list: %v", err)
	}

	removed := 0
	for _, c := range containers {
		if c.Labels[ciderboxProtectedLabel] == "true" && !*force {
			fmt.Fprintf(a.Stdout, "skip protected %s\n", c.ID)
			continue
		}
		if *dryRun {
			fmt.Fprintf(a.Stdout, "would remove %s\n", c.ID)
			continue
		}
		if err := containerRuntime.Remove(ctx, c.ID, true); err != nil {
			return exit(1, "cleanup: remove %s: %v", c.ID, err)
		}
		fmt.Fprintf(a.Stdout, "removed %s\n", c.ID)
		removed++
	}

	if !*dryRun {
		fmt.Fprintf(a.Stdout, "removed=%d checked=%d\n", removed, len(containers))
	}

	return nil
}
