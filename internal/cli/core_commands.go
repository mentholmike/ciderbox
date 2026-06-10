package cli

import (
	"context"
	"fmt"
	"sort"
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

	leases, err := a.listLeases(ctx, *provider)
	if err != nil {
		return exit(2, "list: %v", err)
	}

	if len(leases) == 0 {
		fmt.Fprintln(a.Stdout, "no active leases")
		return nil
	}

	sort.Slice(leases, func(i, j int) bool {
		return leases[i].Slug < leases[j].Slug
	})

	fmt.Fprintf(a.Stdout, "%-24s %-12s %-16s %s\n", "LEASE", "STATUS", "IP", "SLUG")
	fmt.Fprintln(a.Stdout, strings.Repeat("-", 64))
	for _, l := range leases {
		ip := l.Server.PublicNet.IPv4.IP
		if ip == "" {
			ip = "-"
		}
		fmt.Fprintf(a.Stdout, "%-24s %-12s %-16s %s\n", l.ID, l.State, ip, l.Slug)
	}
	return nil
}

func (a App) listLeases(ctx context.Context, provider string) ([]LeaseView, error) {
	cfg, rt, err := a.providerConfigRuntime(provider)
	if err != nil {
		return nil, err
	}
	p, err := ProviderFor(provider)
	if err != nil {
		return nil, err
	}
	backend, err := p.Configure(cfg, rt)
	if err != nil {
		return nil, err
	}
	sshBackend, ok := backend.(SSHLeaseBackend)
	if !ok {
		return nil, fmt.Errorf("provider %q does not support SSH leases", provider)
	}
	return sshBackend.List(ctx, ListRequest{})
}

// ---- cleanup ----

func (a App) cleanup(ctx context.Context, args []string) error {
	fs := newFlagSet("cleanup", a.Stderr)
	provider := fs.String("provider", "apple-container", "provider name")
	dryRun := fs.Bool("dry-run", false, "show what would be removed")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	cfg, rt, err := a.providerConfigRuntime(*provider)
	if err != nil {
		return exit(2, "cleanup: %v", err)
	}

	p, err := ProviderFor(*provider)
	if err != nil {
		return exit(2, "cleanup: %v", err)
	}

	backend, err := p.Configure(cfg, rt)
	if err != nil {
		return exit(2, "cleanup: %v", err)
	}

	// Provider's backend.Cleanup isn't on the interface, so use List + ReleaseLease
	sshBackend, ok := backend.(SSHLeaseBackend)
	if !ok {
		return exit(2, "cleanup: provider %q does not support leases", *provider)
	}

	leases, err := sshBackend.List(ctx, ListRequest{})
	if err != nil {
		return exit(2, "cleanup: %v", err)
	}

	removed := 0
	for _, l := range leases {
		if !shouldCleanupLease(l) {
			continue
		}
		if *dryRun {
			fmt.Fprintf(a.Stdout, "would remove lease=%s slug=%s\n", l.ID, l.Slug)
			continue
		}
		if err := sshBackend.ReleaseLease(ctx, ReleaseLeaseRequest{LeaseID: l.ID, DeleteServer: true}); err != nil {
			fmt.Fprintf(a.Stderr, "remove lease=%s: %v\n", l.ID, err)
			continue
		}
		fmt.Fprintf(a.Stdout, "removed lease=%s slug=%s\n", l.ID, l.Slug)
		removed++
	}

	if !*dryRun {
		fmt.Fprintf(a.Stdout, "cleanup: removed %d lease(s), checked %d\n", removed, len(leases))
	}
	return nil
}

func shouldCleanupLease(l LeaseView) bool {
	if l.Labels == nil {
		return true
	}
	return !strings.EqualFold(l.Labels["keep"], "true") && !strings.EqualFold(l.Labels["ciderbox-protected"], "true")
}

// ---- run ----

func (a App) runCommand(ctx context.Context, args []string) error {
	fs := newFlagSet("run", a.Stderr)
	provider := fs.String("provider", "apple-container", "provider name")
	keep := fs.Bool("keep", false, "keep container after command")
	slug := fs.String("slug", "", "lease slug (auto-generated if empty)")
	image := fs.String("apple-container-image", "", "container image override")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	command := fs.Args()
	if len(command) == 0 {
		return exit(2, "usage: ciderbox run [flags] -- <command...>")
	}

	cfg, rt, err := a.providerConfigRuntime(*provider)
	if err != nil {
		return exit(2, "run: %v", err)
	}
	if *image != "" {
		cfg.AppleContainer.Image = *image
	}

	p, err := ProviderFor(*provider)
	if err != nil {
		return exit(2, "run: %v", err)
	}

	backend, err := p.Configure(cfg, rt)
	if err != nil {
		return exit(2, "run: %v", err)
	}

	sshBackend, ok := backend.(SSHLeaseBackend)
	if !ok {
		return exit(2, "run: provider %q does not support SSH leases", *provider)
	}

	lease, err := sshBackend.Acquire(ctx, AcquireRequest{
		Config:        cfg,
		Keep:          *keep,
		RequestedSlug: *slug,
	})
	if err != nil {
		return exit(2, "run: acquire lease: %v", err)
	}

	sshTarget := lease.SSH
	if sshTarget.Host == "" {
		sshTarget = lease.Target
	}
	if sshTarget.Host == "" {
		sshTarget.Host = lease.Server.PublicNet.IPv4.IP
	}
	if sshTarget.Port == "" {
		sshTarget.Port = "22"
	}
	if sshTarget.User == "" {
		sshTarget.User = cfg.AppleContainer.User
	}

	remoteCommand := strings.Join(command, " ")
	sshArgs := []string{
		"-p", sshTarget.Port,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=10",
		fmt.Sprintf("%s@%s", sshTarget.User, sshTarget.Host),
		remoteCommand,
	}
	if sshTarget.Key != "" {
		sshArgs = append([]string{"-i", sshTarget.Key}, sshArgs...)
	}

	fmt.Fprintf(a.Stderr, "running on %s@%s:%s...\n", sshTarget.User, sshTarget.Host, sshTarget.Port)
	_, err = rt.Exec.Run(ctx, LocalCommandRequest{
		Name:   "ssh",
		Args:   sshArgs,
		Stdout: a.Stdout,
		Stderr: a.Stderr,
	})
	if err != nil {
		return exit(1, "run: command failed: %v", err)
	}

	if !*keep {
		if err := sshBackend.ReleaseLease(ctx, ReleaseLeaseRequest{LeaseID: lease.LeaseID, DeleteServer: true}); err != nil {
			fmt.Fprintf(a.Stderr, "run: release lease: %v\n", err)
		}
	}

	return nil
}

// ---- compile-test ----

func (a App) compileTest(ctx context.Context, args []string) error {
	return exit(2, "compile-test: not implemented in ciderbox yet; use `ciderbox run` with your test command")
}

// ---- build ----

func (a App) buildCommand(ctx context.Context, args []string) error {
	return exit(2, "build: not implemented in ciderbox yet; use `ciderbox run` with your build command")
}

// ---- chop ----

func (a App) chopCommand(ctx context.Context, args []string) error {
	return a.cleanup(ctx, append(args, "--provider", "apple-container"))
}

