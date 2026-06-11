package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	Enabled  bool
	Hostname string
	FQDN     string
	IPv4     string
	Tags     []string
	State    string
	Error    string
}

type PublicNetInfo struct {
	IPv4 PublicIPv4Info
}

type PublicIPv4Info struct {
	IP string
}

type SSHTarget struct {
	Host          string
	Port          string
	User          string
	Key           string
	Password      string
	FallbackPorts []string
	Provider      string
	Slug          string
	Labels        map[string]string
	ReadyCheck    string
}

// Server is a concrete type representing a lease/container server.
// It is intentionally concrete, not an interface, so that orchard and
// other consumers can inspect its fields directly without type assertions.
type Server struct {
	ID         string
	CloudID    string
	Name       string
	Provider   string
	State      string
	Status     string
	Labels     map[string]string
	PublicNet  PublicNetInfo
	ServerType ServerTypeInfo
	Image      string
	LeaseID    string
	Region     string
}

type ServerTypeInfo struct {
	Name string
}

func (s Server) DisplayID() string {
	if s.Name != "" {
		return s.Name
	}
	return s.ID
}

// stringListFlag implements flag.Value for repeatable -flag key=value args.
type stringListFlag []string

func (s *stringListFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func (s *stringListFlag) String() string {
	return strings.Join(*s, ",")
}

type SSHCache struct {
	Hostname string
	Port     string
}

// ---- Coordinator (stub) ----

type CoordinatorClient struct{}

type CoordinatorCapability struct {
	Name    string
	Version string
}

func NewCoordinatorClient(url, adminToken, runtimeToken string) *CoordinatorClient {
	return &CoordinatorClient{}
}
func (c *CoordinatorClient) Close()                         {}
func (c *CoordinatorClient) Ping(ctx context.Context) error { return nil }

// ---- Repo ----

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
}

type RunScriptResult struct {
	Output   string
	ExitCode int
}

// ---- Claim stubs for init (no-op) ----

// ---- Init helpers ----

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
	return Repo{Root: ".", Name: "project", Commit: "HEAD", Ref: "main"}, nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func firstNonBlank(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// ---- Apple SSHTarget helpers ----

func SSHTargetFromConfig(cfg Config, host string) SSHTarget {
	keyPath := cfg.SSHKey
	if keyPath == "" {
		keyPath = testboxKeyPath(cfg.Slug)
	}
	return SSHTarget{
		Host: host,
		Port: "22",
		User: cfg.AppleContainer.User,
		Key:  keyPath,
	}
}

func NewLeaseID() string { return fmt.Sprintf("cbx_%x", time.Now().UnixNano()) }

func LeaseProviderName(leaseID, slug string) string { return slug }

func AllocateDirectLeaseSlug(leaseID, requested string, servers []Server) (string, error) {
	if requested != "" {
		return requested, nil
	}
	return leaseID, nil
}

func NormalizeLeaseSlug(slug string) string { return slug }

func BootstrapWaitTimeout(cfg Config) time.Duration { return 90 * time.Second }

// ---- SSH key management ----

var testboxKeyDir string

func testboxKeyPath(leaseID string) string {
	if testboxKeyDir == "" {
		testboxKeyDir = os.TempDir()
	}
	return filepath.Join(testboxKeyDir, "crabbox-ssh-"+leaseID)
}

func TestboxKeyPath(leaseID string) (string, error) { return testboxKeyPath(leaseID), nil }

func EnsureTestboxKeyForConfig(cfg Config, leaseID string) (string, string, error) {
	keyPath := testboxKeyPath(leaseID)
	if _, err := os.Stat(keyPath); err == nil {
		data, err := os.ReadFile(keyPath + ".pub")
		if err == nil {
			return keyPath, strings.TrimSpace(string(data)), nil
		}
	}
	// Generate a real ED25519 key pair via ssh-keygen
	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-f", keyPath, "-N", "", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		return keyPath, "", fmt.Errorf("generate key at %s: %w\n%s", keyPath, err, string(out))
	}
	pubData, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		return keyPath, "", fmt.Errorf("read public key: %w", err)
	}
	return keyPath, strings.TrimSpace(string(pubData)), nil
}

func RemoveStoredTestboxKey(leaseID string) {
	os.Remove(testboxKeyPath(leaseID))
	os.Remove(testboxKeyPath(leaseID) + ".pub")
}

// ---- Lease claim management ----

type LeaseClaim struct {
	LeaseID            string `json:"lease_id"`
	Slug               string `json:"slug"`
	Provider           string `json:"provider"`
	ProviderScope      string `json:"provider_scope"`
	RepoRoot           string `json:"repo_root"`
	IdleTimeoutSeconds int    `json:"idle_timeout_seconds"`
	LastUsedAt         string `json:"last_used_at"`
}

type CacheVolumeConfig struct {
	Key  string
	Path string
	Name string
}

type CleanupRequest struct {
	DryRun bool
}

type TouchRequest struct {
	Lease LeaseTarget
	State string
}

type LeaseClaimStore struct {
	claims map[string]LeaseClaim
}

var globalClaimStore = &LeaseClaimStore{claims: map[string]LeaseClaim{}}

func ClaimLeaseForRepoProviderScopePond(leaseID, slug, provider, providerScope, pond, repoRoot string, idleTimeout time.Duration, reclaim bool) error {
	globalClaimStore.claims[leaseID] = LeaseClaim{
		LeaseID:            leaseID,
		Slug:               slug,
		Provider:           provider,
		RepoRoot:           repoRoot,
		IdleTimeoutSeconds: int(idleTimeout.Seconds()),
		LastUsedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	return nil
}

func RemoveLeaseClaim(leaseID string) {
	delete(globalClaimStore.claims, leaseID)
}

func ListLeaseClaims() ([]LeaseClaim, error) {
	out := make([]LeaseClaim, 0, len(globalClaimStore.claims))
	for _, c := range globalClaimStore.claims {
		out = append(out, c)
	}
	return out, nil
}

func ResolveLeaseClaimForProvider(identifier, provider string) (LeaseClaim, bool, error) {
	for _, c := range globalClaimStore.claims {
		if (c.LeaseID == identifier || c.Slug == identifier) && c.Provider == provider {
			return c, true, nil
		}
	}
	return LeaseClaim{}, false, nil
}

func UpdateLeaseClaimCacheVolumes(leaseID string, volumes []string) error { return nil }
func CacheVolumeStickyDiskSpecs(cacheVolumes []CacheVolumeConfig) []string {
	out := make([]string, len(cacheVolumes))
	for i, v := range cacheVolumes {
		out[i] = v.Key + "=" + v.Path
		_ = v.Name
	}
	return out
}

// ---- Lease labels ----

func DirectLeaseLabels(cfg Config, leaseID, slug, provider, providerScope string, keep bool, createdAt time.Time) map[string]string {
	return map[string]string{
		"crabbox":    "true",
		"provider":   provider,
		"lease":      leaseID,
		"slug":       slug,
		"keep":       fmt.Sprintf("%v", keep),
		"created_at": createdAt.Format(time.RFC3339),
		"ciderbox":   "true",
	}
}

func TouchDirectLeaseLabels(original map[string]string, cfg Config, state string, now time.Time) map[string]string {
	labels := map[string]string{}
	for k, v := range original {
		labels[k] = v
	}
	labels["state"] = state
	labels["last_touched_at"] = now.Format(time.RFC3339)
	return labels
}

// ---- SSH readiness ----

func WaitForSSHReady(ctx context.Context, target *SSHTarget, stderr io.Writer, label string, timeout time.Duration) error {
	if target == nil {
		return nil
	}
	host := target.Host
	port := target.Port
	if port == "" {
		port = "22"
	}
	fmt.Fprintf(stderr, "waiting for SSH at %s:%s...\n", host, port)
	deadline := time.Now().Add(timeout)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("SSH wait timeout after %v at %s:%s", timeout, host, port)
			}
			cmd := exec.CommandContext(ctx, "sh", "-c",
				fmt.Sprintf("nc -z -w3 %s %s", host, port))
			if cmd.Run() == nil {
				fmt.Fprintf(stderr, "SSH ready at %s:%s\n", host, port)
				return nil
			}
		}
	}
}

func MarkAppleContainerImageExplicit(cfg *Config) { cfg.OSImage = cfg.AppleContainer.Image }
