package cli

import (
	"os"
	"path/filepath"
	"time"
)

// Config is the ciderbox user configuration.
type Config struct {
	Profile            string        `yaml:"profile"`
	Provider           string        `yaml:"provider"`
	Coordinator        string        `yaml:"coordinator,omitempty"`
	CoordToken         string        `yaml:"-"`
	CoordAdminToken    string        `yaml:"-"`
	TargetOS           string        `yaml:"target"`
	WindowsMode        string        `yaml:"windowsMode,omitempty"`
	Architecture       string        `yaml:"architecture,omitempty"`
	OSImage            string        `yaml:"osImage,omitempty"`
	ServerType         string        `yaml:"type,omitempty"`
	Class              string        `yaml:"class,omitempty"`
	WorkRoot           string        `yaml:"workRoot,omitempty"`
	SSHUser            string        `yaml:"sshUser,omitempty"`
	SSHPort            string        `yaml:"sshPort,omitempty"`
	SSHKey             string        `yaml:"sshKey,omitempty"`
	SSHFallbackPorts   []string      `yaml:"sshFallbackPorts,omitempty"`
	DesktopEnv         string        `yaml:"desktopEnv,omitempty"`
	Network            NetworkMode   `yaml:"network,omitempty"`
	Tailscale          TailscaleConfig `yaml:"tailscale,omitempty"`
	Pond               string        `yaml:"pond,omitempty"`
	IdleTimeout        time.Duration `yaml:"-"`
	TTL                time.Duration `yaml:"-"`
	ProfileStore       string        `yaml:"-"`
	Slug               string        `yaml:"slug,omitempty"`
	ExposedPorts       []string      `yaml:"expose,omitempty"`

	AppleContainer AppleContainerConfig `yaml:"appleContainer,omitempty"`

	Cache CacheConfig `yaml:"cache,omitempty"`

	// Internal fields populated during config loading
	GCPProject     string `yaml:"-"`
	Static         StaticConfig `yaml:"-"`
}

type CacheConfig struct {
	Volumes []string `yaml:"volumes,omitempty"`
}

type AppleContainerConfig struct {
	CLIPath      string   `yaml:"cliPath,omitempty"`
	Image        string   `yaml:"image,omitempty"`
	User         string   `yaml:"user,omitempty"`
	WorkRoot     string   `yaml:"workRoot,omitempty"`
	CPUs         int      `yaml:"cpus,omitempty"`
	Memory       string   `yaml:"memory,omitempty"`
	ExtraRunArgs []string `yaml:"extraRunArgs,omitempty"`
}

type StaticConfig struct {
	Host string `yaml:"host,omitempty"`
	User string `yaml:"user,omitempty"`
	Port string `yaml:"port,omitempty"`
}

func baseConfig() Config {
	return Config{}
}

func loadConfig() (Config, error) {
	var cfg Config
	cfg.Provider = "apple-container"
	cfg.TargetOS = "linux"
	cfg.SSHUser = "crabbox"
	cfg.SSHPort = "22"
	cfg.AppleContainer.CLIPath = "container"
	cfg.AppleContainer.Image = "debian:bookworm"
	cfg.AppleContainer.User = "crabbox"
	cfg.AppleContainer.WorkRoot = "/work/crabbox"
	cfg.WorkRoot = "/work/crabbox"
	cfg.IdleTimeout = 30 * time.Minute
	cfg.TTL = 90 * time.Minute
	return cfg, nil
}

func configPaths() []string {
	return []string{
		filepath.Join(os.Getenv("HOME"), ".config", "ciderbox", "config.yaml"),
	}
}

func userConfigPath() string {
	return filepath.Join(os.Getenv("HOME"), ".config", "ciderbox")
}

func expandUserPath(path string) string {
	if len(path) > 1 && path[0] == '~' {
		return filepath.Join(os.Getenv("HOME"), path[1:])
	}
	return path
}

func normalizeNetworkConfig(cfg *Config) {}
func validateNetworkConfig(cfg Config) error { return nil }

func normalizeOSImage(value string) (string, error) { return value, nil }
func normalizeArchitecture(value string) (string, error) { return value, nil }
func effectiveArchitectureForConfig(cfg Config) string { return "amd64" }
func normalizeTargetConfig(cfg *Config) {}
func validateTargetConfig(cfg Config) error { return nil }
func normalizePreflightToolNames(names []string) []string { return names }
func normalizeTailscaleTags(tags []string) []string { return tags }
func routeConfiguredProvider(cfg *Config) error { return nil }
func canonicalizeConfigProvider(cfg *Config) {}
func applyProviderConfigDefaults(cfg *Config) error { return nil }

const (
	TargetLinux   = "linux"
	TargetMacOS   = "macos"
	TargetWindows = "windows"
	targetLinux   = TargetLinux
	targetMacOS   = TargetMacOS
	targetWindows = TargetWindows
)
const ArchitectureAMD64 = "amd64"
const defaultPOSIXWorkRoot = "/work/crabbox"
const AzureOSDiskManaged = ""

func configFilePermissionProblem(path string) string { return "" }

func isDefaultWorkRoot(value string) bool { return value == "" || value == defaultPOSIXWorkRoot }

var defaultWorkRootForTarget = func(targetOS, windowsMode string) string { return defaultPOSIXWorkRoot }