package cli

import (
	"context"
	"strings"
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
}

type PublicNetInfo struct {
	IPv4 PublicIPv4Info
}

type PublicIPv4Info struct {
	IP string
}

type SSHTarget struct {
	Host      string
	Port      string
	User      string
	Key       string
	Password  string
	FallbackPorts []string
	Provider  string
	Slug      string
	Labels    map[string]string
}

// Server is a concrete type representing a lease/container server.
// It is intentionally concrete, not an interface, so that orchard and
// other consumers can inspect its fields directly without type assertions.
type Server struct {
	ID          string
	Name        string
	Provider    string
	State       string
	Labels      map[string]string
	PublicNet   PublicNetInfo
	Region      string
	ServerType  string
	Image       string
	LeaseID     string
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
func (c *CoordinatorClient) Close() {}
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

type LeaseClaim struct{}

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
