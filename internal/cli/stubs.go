package cli

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ---- Core types that were in deleted files ----

type NetworkMode string

const (
	NetworkAuto      NetworkMode = "auto"
	NetworkTailscale NetworkMode = "tailscale"
	NetworkPublic    NetworkMode = "public"
)

type TailscaleConfig struct {
	Enabled                bool
	Tags                   []string
	HostnameTemplate       string
	Hostname               string
	AuthKeyEnv             string
	AuthKey                string
	ExitNode               string
	ExitNodeAllowLANAccess bool
}

type TailscaleMetadata struct {
	Enabled                bool
	Hostname               string
	FQDN                   string
	IPv4                   string
	Tags                   []string
	State                  string
	Error                  string
	ExitNode               string
	ExitNodeAllowLANAccess bool
}

type SSHTarget struct {
	Host         string
	Port         string
	User         string
	SSHKey       string
	TargetOS     string
	WindowsMode  string
	DesktopEnv   string
	JumpTarget   *SSHTarget
	SSHOptions   []string
}

type Repo struct {
	Root   string
	Name   string
	Commit string
	Ref    string
	Dirty  bool
}

type RunScriptSpec struct {
	Commands []string
	Env      []string
	WorkDir  string
}

// Server describes a provider server/VM backing a lease.
type Server struct {
	ID         int64
	Name       string
	CloudID    string
	Provider   string
	Labels     map[string]string
	ServerType ServerTypeInfo
	PublicNet  PublicNetInfo
	HostID     string
	Status     string
}

type ServerTypeInfo struct {
	Name string
}

type PublicNetInfo struct {
	IPv4 IPv4Info
}

type IPv4Info struct {
	IP string
}

func (s Server) DisplayID() string {
	if s.ID != 0 {
		return fmt.Sprintf("%d", s.ID)
	}
	return fmt.Sprintf("%s (%s)", s.Name, s.CloudID)
}

// stringListFlag implements flag.Value for repeatable -flag key=value args.
type stringListFlag []string

func (s *stringListFlag) String() string { return strings.Join(*s, ",") }
func (s *stringListFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

// ---- Stubs for App methods ----
func (a App) doctor(ctx context.Context, args []string) error       { return fmt.Errorf("doctor: not available") }
func (a App) list(ctx context.Context, args []string) error         { return fmt.Errorf("list: not available") }
func (a App) cleanup(ctx context.Context, args []string) error      { return fmt.Errorf("cleanup: not available") }
func (a App) actionsHydrate(ctx context.Context, args []string) error { return fmt.Errorf("not available") }

// ---- Stubs for utility functions ----
func allServersForProvider(providerName string) []Server { return nil }
func cloneStringMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
func firstNonBlank(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
const staticProvider = ""

func applyCapabilityFlags(cfg *Config, desktop, browser, code bool)       {}
func validateRequestedCapabilities(cfg Config) error                     { return nil }
func labelBool(value string) bool                                         { return value == "true" }
func normalizeCapabilities(value string) string                            { return value }

const (
	pondLabelKey             = ""
	pondExposedPortsLabelKey = ""
)
func renderExposedPortsLabel(ports []string) string                     { return "" }
func parseExposedPorts(values []string) (map[int]string, error)         { return nil, nil }
func requestedExposedPorts(values []string) ([]string, error)           { return values, nil }

func azureVMSizeCandidatesForConfig(cfg Config) []string               { return nil }
func azureVMSizeCandidatesForClass(class string) []string               { return nil }
func gcpMachineTypeCandidatesForClass(class string) []string            { return nil }

type HetznerClient struct{}
func NewHetznerClient(token string) *HetznerClient                     { return &HetznerClient{} }
func (c *HetznerClient) DeleteServer(ctx context.Context, id int64) error { return nil }
func (c *HetznerClient) Name(ctx context.Context) (string, error)      { return "", nil }
func (c *HetznerClient) IsServerOn(ctx context.Context, id int64) (bool, error) { return false, nil }

func normalizeCheckpointStrategy(value string) string { return value }
const (
	checkpointKindAWSAMI            = ""
	checkpointKindAWSEBS            = ""
	checkpointKindAzure             = ""
	checkpointKindAzureOS           = ""
	checkpointKindGCP               = ""
	checkpointKindGCPDisk           = ""
	checkpointKindParallels         = ""
	checkpointStrategyImage         = ""
	checkpointStrategyDiskSnapshot  = ""
)

type providerBackendSpec struct{}
func providerBackendFor(cfg Config) (Provider, Backend, providerBackendSpec, error) {
	return nil, nil, providerBackendSpec{}, fmt.Errorf("no provider")
}
func providerBackendForTarget(cfg Config, targetName string) (Backend, error) {
	return nil, fmt.Errorf("no backend")
}

func serverSSHTarget(server Server, cfg Config) SSHTarget { return SSHTarget{} }
func serverSSHUser(server Server, cfg Config) string      { return "root" }
func tailscaleSummary(tail *TailscaleMetadata) string     { return "" }
func applyTailscaleLabelToServer(server *Server, labels map[string]string) {}
func cleanupServersForProvider(ctx context.Context, a App, providerName string, servers []Server) error { return nil }

func isCoordinatorUnauthorized(err error) bool { return false }
const desktopEnvXFCE = "xfce"
func normalizedDesktopEnv(value string) string { return value }

// Coordinator stubs
type CoordinatorClient struct{}
func newCoordinatorClient(cfg Config) (*CoordinatorClient, bool, error) { return nil, false, nil }
type CoordinatorLease struct {
	ID, Owner, Org, Provider, ServerType string
	ServerID                             int64
	ServerName, Slug, CloudID, Host, SSHPort, SSHUser, State string
	Keep                                 bool
	IdleTimeoutSeconds                   int
	TargetOS, WindowsMode, DesktopEnv    string
	Tailscale                            *TailscaleMetadata
	ExposedPorts                         []string
	Pond                                 string
	SSHFallbackPorts                     []string
}
type CoordinatorMachine           struct{}
type CoordinatorReadyPoolEntry    struct{}
type CoordinatorReadyPoolResponse struct{}
type CoordinatorAuditEntry        struct{}
type CoordinatorImageTask         struct{}
type CoordinatorMacHost           struct{}
type CoordinatorWhoami            struct{ Login, Owner string }
type CoordinatorRun               struct{ Provider, LeaseID, ID, Phase, TargetOS, Class, ServerType string }
type CoordinatorRunEventInput     struct{}
type CoordinatorEventInput        struct{}
type CoordinatorProvider          struct{}
type LeaseTelemetry               struct{}
type TestResultSummary            struct{}

func (c *CoordinatorClient) CreateLease(ctx context.Context, cfg Config, publicKey string, keep bool, leaseID, slug string) (CoordinatorLease, error) { return CoordinatorLease{}, nil }
func (c *CoordinatorClient) TouchLease(ctx context.Context, id string) (CoordinatorLease, error) { return CoordinatorLease{}, nil }
func (c *CoordinatorClient) GetLease(ctx context.Context, id string) (CoordinatorLease, error) { return CoordinatorLease{}, nil }
func (c *CoordinatorClient) ReleaseLease(ctx context.Context, id string, deleteServer bool) (CoordinatorLease, error) { return CoordinatorLease{}, nil }
func (c *CoordinatorClient) ReuseLease(ctx context.Context, id string) (CoordinatorLease, error) { return CoordinatorLease{}, nil }
func (c *CoordinatorClient) Leases(ctx context.Context, state string, limit int) ([]CoordinatorLease, error) { return nil, nil }
func (c *CoordinatorClient) Machines(ctx context.Context) ([]CoordinatorMachine, error) { return nil, nil }
func (c *CoordinatorClient) DeleteServer(ctx context.Context, id int64) error { return nil }
func (c *CoordinatorClient) TouchLeaseWithTelemetry(ctx context.Context, id string, telemetry *LeaseTelemetry) (CoordinatorLease, error) { return CoordinatorLease{}, nil }
func (c *CoordinatorClient) UpdateLeaseIdleTimeout(ctx context.Context, id string, idleTimeout time.Duration) (CoordinatorLease, error) { return CoordinatorLease{}, nil }
func (c *CoordinatorClient) Pool(ctx context.Context, cfg Config) ([]CoordinatorMachine, error) { return nil, nil }
func (c *CoordinatorClient) ReadyPools(ctx context.Context) ([]CoordinatorReadyPoolEntry, error) { return nil, nil }
func (c *CoordinatorClient) ReadyPool(ctx context.Context, key string) ([]CoordinatorReadyPoolEntry, error) { return nil, nil }
func (c *CoordinatorClient) RegisterReadyPoolLease(ctx context.Context, key string, input map[string]any) (CoordinatorReadyPoolResponse, error) { return CoordinatorReadyPoolResponse{}, nil }
func (c *CoordinatorClient) BorrowReadyPoolLease(ctx context.Context, key string) (CoordinatorReadyPoolEntry, error) { return CoordinatorReadyPoolEntry{}, nil }
func (c *CoordinatorClient) ReturnReadyPoolLease(ctx context.Context, key, token string) (CoordinatorReadyPoolEntry, error) { return CoordinatorReadyPoolEntry{}, nil }
func (c *CoordinatorClient) Whoami(ctx context.Context) (CoordinatorWhoami, error) { return CoordinatorWhoami{}, nil }
func (c *CoordinatorClient) Runs(ctx context.Context, filter map[string]string, limit int) ([]CoordinatorRun, error) { return nil, nil }
func (c *CoordinatorClient) CreateRun(ctx context.Context, input CoordinatorRunEventInput) (*CoordinatorRun, error) { return nil, nil }
func (c *CoordinatorClient) CreateRunEvent(ctx context.Context, runID string, event CoordinatorEventInput) error { return nil }
func (c *CoordinatorClient) GetRun(ctx context.Context, runID string) (*CoordinatorRun, error) { return nil, nil }
func (c *CoordinatorClient) Heartbeat(ctx context.Context) error { return nil }
func (c *CoordinatorClient) GetProvider(ctx context.Context, provider string) (CoordinatorProvider, error) { return CoordinatorProvider{}, nil }
func (c *CoordinatorClient) Logout(ctx context.Context) error { return nil }
func (c *CoordinatorClient) UpdateLeaseTailscale(ctx context.Context, id string, meta TailscaleMetadata) (CoordinatorLease, error) { return CoordinatorLease{}, nil }
func (c *CoordinatorClient) UpdateLeaseIdleTimeoutWithTelemetry(ctx context.Context, id string, idleTimeout time.Duration, telemetry *LeaseTelemetry) (CoordinatorLease, error) { return CoordinatorLease{}, nil }
func (c *CoordinatorClient) ReadyPoolOwner(ctx context.Context, key string) (CoordinatorReadyPoolEntry, error) { return CoordinatorReadyPoolEntry{}, nil }
func (c *CoordinatorClient) WaitReadyPool(ctx context.Context, key, leaseID string) (CoordinatorReadyPoolEntry, error) { return CoordinatorReadyPoolEntry{}, nil }
func (c *CoordinatorClient) LeaseAudit(ctx context.Context, state string) ([]CoordinatorAuditEntry, error) { return nil, nil }
func (c *CoordinatorClient) ProviderIdentity(ctx context.Context) (string, error) { return "", nil }
func (c *CoordinatorClient) ProviderImageCreate(ctx context.Context, leaseID string, tags []string) (CoordinatorImageTask, error) { return CoordinatorImageTask{}, nil }
func (c *CoordinatorClient) ProviderImagePromote(ctx context.Context, imageID string) error { return nil }
func (c *CoordinatorClient) ProviderImageFSRStatus(ctx context.Context, imageID, region string) (string, error) { return "", nil }
func (c *CoordinatorClient) ProviderImageDelete(ctx context.Context, imageID string) error { return nil }
func (c *CoordinatorClient) AWSIdentity(ctx context.Context) (string, error) { return "", nil }
func (c *CoordinatorClient) AWSPolicy(ctx context.Context) (string, error) { return "", nil }
func (c *CoordinatorClient) MacHosts(ctx context.Context, region string) ([]CoordinatorMacHost, error) { return nil, nil }
func (c *CoordinatorClient) AllocateMacHost(ctx context.Context, region string) (string, error) { return "", nil }
func (c *CoordinatorClient) ReleaseMacHost(ctx context.Context, hostID, region string) error { return nil }
func (c *CoordinatorClient) Health(ctx context.Context) (string, error) { return "ok", nil }
func (c *CoordinatorClient) Run(ctx context.Context, args []string) (*CoordinatorRun, error) { return nil, nil }
func (c *CoordinatorClient) ProviderReadiness(ctx context.Context) (*CoordinatorProvider, error) { return nil, nil }

func leaseToServerTarget(lease CoordinatorLease, cfg Config) (Server, bool, string) { return Server{}, false, "" }
func localCoordinatorOwner() string { return "" }
func supportsGitHubActionsRunnerTarget(target SSHTarget) bool { return false }
type coordinatorLeaseBackend struct {
	spec   ProviderSpec
	cfg    Config
	direct Backend
	coord  *CoordinatorClient
	rt     Runtime
}
func (b *coordinatorLeaseBackend) Spec() ProviderSpec { return b.spec }
func newCoordinatorLeaseBackend(cfg Config, rt Runtime) (Backend, error) { return nil, fmt.Errorf("coordinator not available") }

func directLeaseExpiresAt(now time.Time, cfg Config) string {
	return now.Add(cfg.TTL).Format(time.RFC3339)
}

func directLeaseLabels(cfg Config, leaseID, slug, provider, market string, keep bool, now time.Time) map[string]string {
	return map[string]string{}
}
func touchDirectLeaseLabels(labels map[string]string, cfg Config, state string, now time.Time) map[string]string {
	return labels
}

func leaseLabelTime(t time.Time) string                { return t.Format(time.RFC3339) }
func leaseLabelTimeDisplay(value string) string        { return value }
func leaseLabelDurationDisplay(secondsValue, fallbackValue string) string { return fallbackValue }

func updateLeaseClaimCacheVolumes(leaseID string, specs []string) error { return nil }
func powershellCommand(script string) string { return script }
func validCrabboxProviderKey(value string) bool { return false }

func UpdateLeaseClaimEndpoint(leaseID string, server Server, target SSHTarget) error { return nil }
func ClaimLeaseForRepoProvider(leaseID, slug, provider, repoRoot string, idleTimeout time.Duration, reclaim bool) error { return nil }
func ReadLeaseClaim(leaseID string) (LeaseClaim, error) { return LeaseClaim{}, nil }
type LeaseClaim struct{}

func shellQuote(s string) string { return s }






func appendUniqueStrings(slice []string, values ...string) []string {
	for _, v := range values {
		found := false
		for _, s := range slice {
			if s == v {
				found = true
				break
			}
		}
		if !found {
			slice = append(slice, v)
		}
	}
	return slice
}

func findRepo() (Repo, error) {
	return Repo{Root: ".", Commit: "HEAD", Ref: "main"}, nil
}


func runtimeForApp(a App) Runtime {
	return Runtime{
		Stdout: a.Stdout,
		Stderr: a.Stderr,
		Clock:  realClock{},
		Exec:   osCommandRunner{},
	}
}


type leaseClaim struct {
	LeaseID string
	Slug    string
	Provider string
	RepoRoot string
}

func findLeaseClaim(slug string, match func(leaseClaim) bool) (leaseClaim, bool, error) {
	return leaseClaim{}, false, nil
}


func NewLeaseID() string { return fmt.Sprintf("cbr-%d", time.Now().UnixNano()) }

func EnsureTestboxKeyForConfig(cfg Config, leaseID string) (string, string, error) { return "", "", nil }
func RemoveStoredTestboxKey(leaseID string) {}

type CleanupRequest struct {
	DryRun bool
}

type TouchRequest struct {
	LeaseID string
	Server  Server
	Lease   LeaseTarget
}

type CacheVolumeConfig struct {
	Key  string
	Path string
	Name string
}


func AllocateDirectLeaseSlug(leaseID, requested string, servers []Server) (string, error) {
	if requested != "" {
		return requested, nil
	}
	return leaseID, nil
}

func LeaseProviderName(leaseID, slug string) string { return "apple-container" }
func BootstrapWaitTimeout(cfg Config) time.Duration { return 120 * time.Second }

func ClaimLeaseForRepoProviderScopePond(leaseID, slug, provider, providerScope, pond, repoRoot string, idleTimeout time.Duration, reclaim bool) error {
	return nil
}

func CacheVolumeStickyDiskSpecs(cacheVolumes []string) []string { return cacheVolumes }


func removeLeaseClaim(leaseID string) {}
func resolveLeaseClaim(identifier string) (leaseClaim, bool, error) { return leaseClaim{}, false, nil }
func listLeaseClaims() ([]leaseClaim, error) { return nil, nil }
func readLeaseClaim(leaseID string) (leaseClaim, error) { return leaseClaim{}, nil }

func findTestboxKey(leaseID string) string { return "" }


func UpdateLeaseClaimCacheVolumes(leaseID string, volumes []string) error { return nil }

func RemoveLeaseClaim(leaseID string) {}

func ListLeaseClaims() ([]LeaseView, error) { return nil, nil }

func ResolveLeaseClaimForProvider(identifier, provider string) (LeaseView, error) { return LeaseView{}, nil }

func NormalizeLeaseSlug(slug string) string { return slug }

func SSHTargetFromConfig(cfg Config) SSHTarget { return SSHTarget{} }

func TestboxKeyPath(leaseID string) string { return "" }

func WaitForSSHReady(ctx context.Context, rt Runtime, target SSHTarget, timeout time.Duration) error { return nil }

type DirectLeaseLabels struct{}
