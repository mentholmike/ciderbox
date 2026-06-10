package cli

import (
	"context"
	"io"
	"time"
)

// ContainerSpec describes a container to create via a native container runtime.
// It deliberately omits SSH-specific concerns (keys, bootstrap, claims) so
// callers that only need VM lifecycle can stay clean.
type ContainerSpec struct {
	Name      string            // --name (optional; container ID is used if empty)
	Image     string            // container image
	CPUs      int               // --cpus
	Memory    string            // --memory
	User      string            // --user
	Labels    map[string]string // --label
	Env       map[string]string // -e
	Volumes   []string          // --volume host:container
	DNS       []string          // --dns (auto-detected from host if empty)
	ExtraArgs []string          // appended before image
	Command   []string          // appended after image
}

// ContainerInfo is a lightweight, provider-agnostic view of a container.
type ContainerInfo struct {
	ID        string
	Name      string
	Image     string
	Status    string // running, stopped, created, etc.
	IP        string
	Labels    map[string]string
	Pid       int
	StartedAt time.Time
}

// ContainerRuntime is the native container lifecycle interface. Orchard and
// other first-class consumers should use this instead of SSHLeaseBackend when
// they only need VM lifecycle (Run, List, Exec, Remove) without the CRABBOX
// lease abstraction (ssh bootstrap, claim management, key storage).
type ContainerRuntime interface {
	Run(ctx context.Context, spec ContainerSpec) (ContainerInfo, error)
	List(ctx context.Context, filters map[string]string) ([]ContainerInfo, error)
	Inspect(ctx context.Context, id string) (ContainerInfo, error)
	Exec(ctx context.Context, id string, cmd []string, stdout, stderr io.Writer) error
	Copy(ctx context.Context, src, dst string) error
	Stop(ctx context.Context, id string) error
	Remove(ctx context.Context, id string, force bool) error
	Logs(ctx context.Context, id string, tailLines int) (string, error)
}
