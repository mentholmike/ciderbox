package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// --- Provider interfaces ---

type Provider interface {
	Name() string
	Aliases() []string
	Spec() ProviderSpec
	RegisterFlags(fs *flag.FlagSet, defaults Config) any
	ApplyFlags(cfg *Config, fs *flag.FlagSet, values any) error
	Configure(cfg Config, rt Runtime) (Backend, error)
}

type ProviderSpec struct {
	Name        string
	Description string
	Family      string
	Kind        ProviderKind
	Features    FeatureSet
	Targets     interface{}
	Coordinator string
}

type ProviderKind string

const (
	ProviderKindSSH          ProviderKind = "ssh"
	ProviderKindDirect       ProviderKind = "direct"
	ProviderKindContainer    ProviderKind = "container"
	ProviderKindDelegatedRun ProviderKind = "delegated-run"
)

const CoordinatorSupported = "supported"

const ProviderKindSSHLease ProviderKind = "ssh-lease"
const CoordinatorNever = "never"
const FeatureSSH Feature = "ssh"
const FeatureCrabboxSync Feature = "crabbox-sync"
const FeatureCleanup Feature = "cleanup"

type TargetSpec struct {
	OS string
}

type Feature string

type FeatureSet []Feature

func (fs FeatureSet) Has(f Feature) bool {
	for _, v := range fs {
		if v == f {
			return true
		}
	}
	return false
}

const (
	FeatureDesktop     Feature = "desktop"
	FeatureBrowser     Feature = "browser"
	FeatureCode        Feature = "code"
	FeatureCacheVolume Feature = "cache-volume"
)

type Backend interface {
	Spec() ProviderSpec
}

type SSHLeaseBackend interface {
	Backend
	Acquire(ctx context.Context, req AcquireRequest) (LeaseTarget, error)
	Resolve(ctx context.Context, req ResolveRequest) (LeaseTarget, error)
	List(ctx context.Context, req ListRequest) ([]LeaseView, error)
	ReleaseLease(ctx context.Context, req ReleaseLeaseRequest) error
}

type LeaseTarget struct {
	Server  Server
	Target  SSHTarget
	LeaseID string
	SSH     SSHTarget
}

type AcquireRequest struct {
	Config        Config
	LeaseID       string
	Slug          string
	PublicKey     string
	Options       LeaseOptions
	Keep          bool
	RequestedSlug string
	Repo          Repo
	Reclaim       bool
}

type LeaseOptions struct {
	TargetOS string
}

type ResolveRequest struct {
	Config      Config
	LeaseID     string
	Slug        string
	ID          string
	ReleaseOnly bool
	Reclaim     bool
	Repo        Repo
}

type ListRequest struct{}

type ReleaseLeaseRequest struct {
	LeaseID      string
	DeleteServer bool
	Lease        LeaseTarget
}

type LeaseView struct {
	ID        string
	Name      string
	Provider  string
	Server    Server
	Target    SSHTarget
	Slug      string
	State     string
	Status    string
	Labels    map[string]string
	PublicNet PublicNetInfo
}

type DoctorBackend interface {
	Backend
	Doctor(ctx context.Context, req DoctorRequest) (DoctorResult, error)
}

type DoctorRequest struct {
	ProbeSSH bool
}

type DoctorResult struct {
	Provider string
	Message  string
	Status   string
	Checks   []DoctorCheck
}

type DoctorCheck struct {
	Status  string
	Check   string
	Message string
}

// --- Runtime ---

type Runtime struct {
	Stdout io.Writer
	Stderr io.Writer
	Clock  Clock
	HTTP   *http.Client
	Exec   CommandRunner
}

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type CommandRunner interface {
	Run(ctx context.Context, req LocalCommandRequest) (LocalCommandResult, error)
}

type LocalCommandRequest struct {
	Name   string
	Args   []string
	Env    []string
	Dir    string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

type LocalCommandResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// --- Provider registry ---

var registeredProviders = map[string]Provider{}

func RegisterProvider(p Provider) {
	registeredProviders[p.Name()] = p
	for _, alias := range p.Aliases() {
		registeredProviders[alias] = p
	}
}

func ProviderFor(name string) (Provider, error) {
	p, ok := registeredProviders[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", name)
	}
	return p, nil
}

func registeredProvidersList() []Provider {
	out := make([]Provider, 0, len(registeredProviders))
	for _, p := range registeredProviders {
		out = append(out, p)
	}
	return out
}

// --- Direct backend helpers ---

type DirectSSHBackend struct {
	SpecValue ProviderSpec
	Cfg       Config
	RT        Runtime
	Delete    func(context.Context, Config, Server) error
}

func (b *DirectSSHBackend) Spec() ProviderSpec { return b.SpecValue }

func (b *DirectSSHBackend) DeleteServer(ctx context.Context, server Server) error {
	if b.Delete != nil {
		return b.Delete(ctx, b.Cfg, server)
	}
	return nil
}

func (b *DirectSSHBackend) GetServers(ctx context.Context) ([]Server, error) {
	return nil, nil
}

// --- Backend loader ---

func loadBackend(cfg Config) (Backend, error) {
	provider, err := ProviderFor(cfg.Provider)
	if err != nil {
		return nil, err
	}
	rt := Runtime{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Clock:  realClock{},
		HTTP:   http.DefaultClient,
	}
	return provider.Configure(cfg, rt)
}

func leaseOptionsFromConfig(cfg Config, fs *flag.FlagSet) (Config, error) {
	return cfg, nil
}

// --- Provider flag values ---

type providerFlagValues struct {
	Provider   *string
	Profile    *string
	Class      *string
	OSTarget   *string
	OSImage    *string
	ServerType *string
	Market     *string
}

func registerProviderFlags(fs *flag.FlagSet, defaults Config) providerFlagValues {
	return providerFlagValues{
		Provider: fs.String("provider", defaults.Provider, "Provider name"),
		Profile:  fs.String("profile", defaults.Profile, "Config profile name"),
		Class:    fs.String("class", defaults.Class, "Provider class"),
	}
}

// --- Provider-specific stub functions ---

func ProviderServerTypeProvider(cfg Config) string { return cfg.Provider }

const (
	azureProvider      = "azure"
	awsProvider        = "aws"
	gcpProvider        = "gcp"
	hetznerProvider    = "hetzner"
	parallelsProvider  = "parallels"
	proxmoxProvider    = "proxmox"
	incusProvider      = "incus"
	blacksmithProvider = "blacksmith"
)

func isBlacksmithProvider(cfg Config) bool  { return false }
func isStaticProvider(provider string) bool { return false }
