package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func (a App) adminLeases(ctx context.Context, args []string) error {
	fs := newFlagSet("admin leases", a.Stderr)
	state := fs.String("state", "", "filter by state")
	owner := fs.String("owner", "", "filter by owner")
	org := fs.String("org", "", "filter by org")
	limit := fs.Int("limit", 100, "maximum leases")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	coord, err := configuredAdminCoordinator()
	if err != nil {
		return err
	}
	leases, err := coord.AdminLeases(ctx, *state, *owner, *org, *limit)
	if err != nil {
		return err
	}
	if *jsonOut {
		return json.NewEncoder(a.Stdout).Encode(leases)
	}
	for _, lease := range leases {
		fmt.Fprintf(a.Stdout, "%-16s %-16s %-8s %-10s %-14s %-24s owner=%s org=%s idle=%s expires=%s\n",
			lease.ID, blank(lease.Slug, "-"), lease.Provider, lease.State, lease.ServerType, lease.Host, lease.Owner, lease.Org, formatSecondsDuration(lease.IdleTimeoutSeconds), blank(lease.ExpiresAt, "-"))
	}
	return nil
}

func (a App) adminLeaseAudit(ctx context.Context, args []string) error {
	fs := newFlagSet("admin lease-audit", a.Stderr)
	state := fs.String("state", "expired", "filter by state")
	provider := fs.String("provider", "aws", "filter by provider")
	owner := fs.String("owner", "", "filter by owner")
	org := fs.String("org", "", "filter by org")
	limit := fs.Int("limit", 100, "maximum leases")
	failOnLive := fs.Bool("fail-on-live", false, "exit non-zero when expired leases still have live cloud instances or audit errors")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	coord, err := configuredAdminCoordinator()
	if err != nil {
		return err
	}
	audits, err := coord.AdminLeaseAudit(ctx, *state, *provider, *owner, *org, *limit)
	if err != nil {
		return err
	}
	if *jsonOut {
		if err := json.NewEncoder(a.Stdout).Encode(audits); err != nil {
			return err
		}
	} else {
		for _, audit := range audits {
			fmt.Fprintf(a.Stdout, "%-16s %-16s %-8s %-8s %-14s cloud=%-7s cloud_state=%s host=%s expires=%s cleanup=%s\n",
				audit.LeaseID, blank(audit.Slug, "-"), audit.Provider, audit.State, audit.ServerType, audit.CloudStatus, blank(audit.CloudState, "-"), blank(audit.CloudHost, "-"), blank(audit.ExpiresAt, "-"), leaseAuditCleanupSummary(audit))
		}
	}
	if *failOnLive {
		for _, audit := range audits {
			if audit.CloudStatus == "found" || audit.CloudStatus == "error" {
				return exit(1, "lease audit found unreconciled cloud instances or audit errors")
			}
		}
	}
	return nil
}

func leaseAuditCleanupSummary(audit CoordinatorLeaseCloudAudit) string {
	if audit.CleanupAttempts == 0 && audit.CleanupError == "" {
		return "-"
	}
	if audit.CleanupError == "" {
		return fmt.Sprintf("attempts=%d", audit.CleanupAttempts)
	}
	return fmt.Sprintf("attempts=%d error=%s", audit.CleanupAttempts, audit.CleanupError)
}

func (a App) adminMacHosts(ctx context.Context, args []string) error {
	args = stripKongCommandPath(args, "admin", "mac-hosts")
	if len(args) == 0 || isHelpArg(args[0]) {
		return exit(2, "usage: crabbox admin mac-hosts <list|allocate|release> [flags]")
	}
	switch args[0] {
	case "list":
		return a.adminMacHostsList(ctx, args[1:])
	case "allocate":
		return a.adminMacHostsAllocate(ctx, args[1:])
	case "release":
		return a.adminMacHostsRelease(ctx, args[1:])
	default:
		return exit(2, "usage: crabbox admin mac-hosts <list|allocate|release> [flags]")
	}
}

func (a App) adminMacHostsList(ctx context.Context, args []string) error {
	fs := newFlagSet("admin mac-hosts list", a.Stderr)
	region := fs.String("region", "", "AWS region")
	serverType := fs.String("type", "", "filter by EC2 Mac instance type")
	state := fs.String("state", "", "filter by host state")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	coord, err := configuredAdminCoordinator()
	if err != nil {
		return err
	}
	hosts, err := coord.AdminMacHosts(ctx, *region, *serverType, *state)
	if err != nil {
		return err
	}
	if *jsonOut {
		return json.NewEncoder(a.Stdout).Encode(hosts)
	}
	for _, host := range hosts {
		fmt.Fprintf(a.Stdout, "%-18s %-12s %-14s %-12s %-10s auto=%s allocated=%s\n",
			host.ID, host.Region, host.AvailabilityZone, host.InstanceType, host.State,
			blank(host.AutoPlacement, "-"), blank(host.AllocationTime, "-"))
	}
	return nil
}

func (a App) adminMacHostsAllocate(ctx context.Context, args []string) error {
	args, forceAnywhere := extractBoolFlag(args, "force")
	args, jsonAnywhere := extractBoolFlag(args, "json")
	fs := newFlagSet("admin mac-hosts allocate", a.Stderr)
	region := fs.String("region", "", "AWS region")
	serverType := fs.String("type", "mac2.metal", "EC2 Mac instance type")
	availabilityZone := fs.String("availability-zone", "", "AWS availability zone")
	force := fs.Bool("force", false, "confirm host allocation")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if forceAnywhere {
		*force = true
	}
	if jsonAnywhere {
		*jsonOut = true
	}
	if !*force {
		return exit(2, "admin mac-hosts allocate requires --force")
	}
	if strings.TrimSpace(*availabilityZone) == "" {
		return exit(2, "usage: crabbox admin mac-hosts allocate --availability-zone <az> [--region <region>] [--type mac2.metal] --force")
	}
	coord, err := configuredAdminCoordinator()
	if err != nil {
		return err
	}
	hosts, err := coord.AdminAllocateMacHost(ctx, *region, *serverType, *availabilityZone)
	if err != nil {
		return err
	}
	if *jsonOut {
		return json.NewEncoder(a.Stdout).Encode(hosts)
	}
	for _, host := range hosts {
		fmt.Fprintf(a.Stdout, "allocated host=%s region=%s az=%s type=%s state=%s\n",
			host.ID, host.Region, host.AvailabilityZone, host.InstanceType, host.State)
	}
	return nil
}

func (a App) adminMacHostsRelease(ctx context.Context, args []string) error {
	args, forceAnywhere := extractBoolFlag(args, "force")
	args, jsonAnywhere := extractBoolFlag(args, "json")
	fs := newFlagSet("admin mac-hosts release", a.Stderr)
	id := fs.String("id", "", "EC2 Mac Dedicated Host id")
	region := fs.String("region", "", "AWS region")
	force := fs.Bool("force", false, "confirm host release")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *id == "" && fs.NArg() > 0 {
		*id = fs.Arg(0)
	}
	if forceAnywhere {
		*force = true
	}
	if jsonAnywhere {
		*jsonOut = true
	}
	if *id == "" {
		return exit(2, "usage: crabbox admin mac-hosts release <host-id> [--region <region>] --force")
	}
	if !*force {
		return exit(2, "admin mac-hosts release requires --force")
	}
	coord, err := configuredAdminCoordinator()
	if err != nil {
		return err
	}
	released, err := coord.AdminReleaseMacHost(ctx, *region, *id)
	if err != nil {
		return err
	}
	if *jsonOut {
		return json.NewEncoder(a.Stdout).Encode(released)
	}
	fmt.Fprintf(a.Stdout, "released host=%s region=%s released=%s\n", *id, blank(*region, "-"), strings.Join(released, ","))
	return nil
}

func (a App) adminRelease(ctx context.Context, args []string) error {
	args, deleteAnywhere := extractBoolFlag(args, "delete")
	args, jsonAnywhere := extractBoolFlag(args, "json")
	fs := newFlagSet("admin release", a.Stderr)
	id := fs.String("id", "", "lease id or slug")
	deleteServer := fs.Bool("delete", false, "delete server while releasing")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *id == "" && fs.NArg() > 0 {
		*id = fs.Arg(0)
	}
	if *id == "" {
		return exit(2, "usage: crabbox admin release --id <lease-id-or-slug>")
	}
	if deleteAnywhere {
		*deleteServer = true
	}
	if jsonAnywhere {
		*jsonOut = true
	}
	coord, err := configuredAdminCoordinator()
	if err != nil {
		return err
	}
	lease, err := coord.AdminReleaseLease(ctx, *id, *deleteServer)
	if err != nil {
		return err
	}
	if *jsonOut {
		return json.NewEncoder(a.Stdout).Encode(lease)
	}
	fmt.Fprintf(a.Stdout, "released %s slug=%s state=%s delete=%t\n", lease.ID, blank(lease.Slug, "-"), lease.State, *deleteServer)
	return nil
}

func (a App) adminDelete(ctx context.Context, args []string) error {
	args, forceAnywhere := extractBoolFlag(args, "force")
	args, jsonAnywhere := extractBoolFlag(args, "json")
	fs := newFlagSet("admin delete", a.Stderr)
	id := fs.String("id", "", "lease id or slug")
	force := fs.Bool("force", false, "confirm deletion")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *id == "" && fs.NArg() > 0 {
		*id = fs.Arg(0)
	}
	if *id == "" {
		return exit(2, "usage: crabbox admin delete --id <lease-id-or-slug> --force")
	}
	if forceAnywhere {
		*force = true
	}
	if jsonAnywhere {
		*jsonOut = true
	}
	if !*force {
		return exit(2, "admin delete requires --force")
	}
	coord, err := configuredAdminCoordinator()
	if err != nil {
		return err
	}
	lease, err := coord.AdminDeleteLease(ctx, *id)
	if err != nil {
		return err
	}
	if *jsonOut {
		return json.NewEncoder(a.Stdout).Encode(lease)
	}
	fmt.Fprintf(a.Stdout, "deleted %s slug=%s state=%s\n", lease.ID, blank(lease.Slug, "-"), lease.State)
	return nil
}

func configuredCoordinator() (*CoordinatorClient, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	coord, ok, err := newCoordinatorClient(cfg)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, exit(2, "command requires a configured coordinator")
	}
	return coord, nil
}

func configuredAdminCoordinator() (*CoordinatorClient, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	if cfg.CoordAdminToken == "" {
		return nil, exit(2, "admin command requires broker.adminToken or CRABBOX_COORDINATOR_ADMIN_TOKEN")
	}
	cfg.CoordToken = cfg.CoordAdminToken
	coord, ok, err := newCoordinatorClient(cfg)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, exit(2, "admin command requires a configured coordinator")
	}
	return coord, nil
}
