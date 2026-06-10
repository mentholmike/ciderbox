package applecontainer

import (
	"context"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	core "github.com/mentholmike/ciderbox/internal/cli"
)

// containerRuntime implements core.ContainerRuntime using the Apple
// `container` CLI directly. It is intentionally decoupled from the lease
// abstraction (SSH bootstrap, claims, testbox keys) so Orchard and other
// consumers can manage VM lifecycle without the Crabbox overhead.
type containerRuntime struct {
	cfg core.Config
	rt  core.Runtime
}

// NewContainerRuntime returns a native ContainerRuntime backed by Apple's
// container CLI. cfg should have at least AppleContainer.Image and
// AppleContainer.CLIPath set; the rest use sensible defaults.
func NewContainerRuntime(cfg core.Config, rt core.Runtime) (core.ContainerRuntime, error) {
	applyDefaults(&cfg)
	return &containerRuntime{cfg: cfg, rt: rt}, nil
}

func (r *containerRuntime) container(ctx context.Context, args []string, stdout, stderr io.Writer) (core.LocalCommandResult, error) {
	return r.rt.Exec.Run(ctx, core.LocalCommandRequest{
		Name:   r.cfg.AppleContainer.CLIPath,
		Args:   args,
		Stdout: stdout,
		Stderr: stderr,
	})
}

// Run creates a container with the given spec and returns populated info.
func (r *containerRuntime) Run(ctx context.Context, spec core.ContainerSpec) (core.ContainerInfo, error) {
	if err := requireMacOS(); err != nil {
		return core.ContainerInfo{}, err
	}

	args := []string{"run", "-d"}
	if spec.Name != "" {
		args = append(args, "--name", spec.Name)
	}
	if spec.User != "" {
		args = append(args, "--user", spec.User)
	}
	for k, v := range spec.Labels {
		args = append(args, "--label", k+"="+v)
	}
	for k, v := range spec.Env {
		args = append(args, "-e", k+"="+v)
	}
	if spec.CPUs > 0 {
		args = append(args, "--cpus", strconv.Itoa(spec.CPUs))
	}
	if spec.Memory != "" {
		args = append(args, "--memory", spec.Memory)
	}
	for _, v := range spec.Volumes {
		args = append(args, "--volume", v)
	}
	if len(spec.DNS) == 0 && !appleContainerHasDNSArg(spec.ExtraArgs) {
		servers := r.hostDNSServers(ctx)
		for _, s := range servers {
			args = append(args, "--dns", s)
		}
	} else {
		for _, s := range spec.DNS {
			args = append(args, "--dns", s)
		}
	}
	args = append(args, spec.ExtraArgs...)
	args = append(args, spec.Image)
	if len(spec.Command) > 0 {
		args = append(args, spec.Command...)
	}

	result, err := r.container(ctx, args, nil, r.rt.Stderr)
	if err != nil {
		return core.ContainerInfo{}, commandError("container run", result, err)
	}
	id := strings.TrimSpace(result.Stdout)
	if id == "" && spec.Name != "" {
		id = spec.Name
	}
	if id == "" {
		return core.ContainerInfo{}, exit(2, "container run succeeded but no container id returned")
	}

	c, err := r.inspectInspect(ctx, id)
	if err != nil {
		_ = r.Remove(ctx, id, true)
		return core.ContainerInfo{}, err
	}
	return r.infoFromInspect(c), nil
}

// List returns containers matching the given label filters.
func (r *containerRuntime) List(ctx context.Context, filters map[string]string) ([]core.ContainerInfo, error) {
	if err := requireMacOS(); err != nil {
		return nil, err
	}
	containers, err := r.listContainers(ctx)
	if err != nil {
		return nil, err
	}
	var out []core.ContainerInfo
	for _, c := range containers {
		labels := c.labels()
		match := true
		for k, v := range filters {
			if labels[k] != v {
				match = false
				break
			}
		}
		if match {
			out = append(out, r.infoFromInspect(c))
		}
	}
	return out, nil
}

// Inspect returns detailed info for one container.
func (r *containerRuntime) Inspect(ctx context.Context, id string) (core.ContainerInfo, error) {
	if err := requireMacOS(); err != nil {
		return core.ContainerInfo{}, err
	}
	c, err := r.inspectInspect(ctx, id)
	if err != nil {
		return core.ContainerInfo{}, err
	}
	return r.infoFromInspect(c), nil
}

// Exec runs a command inside a container.
func (r *containerRuntime) Exec(ctx context.Context, id string, cmd []string, stdout, stderr io.Writer) error {
	if err := requireMacOS(); err != nil {
		return err
	}
	args := append([]string{"exec", id}, cmd...)
	result, err := r.container(ctx, args, stdout, stderr)
	if err != nil {
		return commandError("container exec", result, err)
	}
	return nil
}

// Copy copies files between host and container (src -> dst).
func (r *containerRuntime) Copy(ctx context.Context, src, dst string) error {
	if err := requireMacOS(); err != nil {
		return err
	}
	result, err := r.container(ctx, []string{"cp", src, dst}, nil, r.rt.Stderr)
	if err != nil {
		return commandError("container cp", result, err)
	}
	return nil
}

// Stop stops a running container.
func (r *containerRuntime) Stop(ctx context.Context, id string) error {
	if err := requireMacOS(); err != nil {
		return err
	}
	result, err := r.container(ctx, []string{"stop", id}, nil, r.rt.Stderr)
	if err != nil {
		return commandError("container stop", result, err)
	}
	return nil
}

// Remove deletes a container (force=true kills running ones).
func (r *containerRuntime) Remove(ctx context.Context, id string, force bool) error {
	if err := requireMacOS(); err != nil {
		return err
	}
	args := []string{"delete"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, id)
	result, err := r.container(ctx, args, nil, r.rt.Stderr)
	if err != nil {
		return commandError("container delete", result, err)
	}
	return nil
}

// Logs returns the last N lines of a container's logs.
func (r *containerRuntime) Logs(ctx context.Context, id string, tailLines int) (string, error) {
	if err := requireMacOS(); err != nil {
		return "", err
	}
	args := []string{"logs", id}
	if tailLines > 0 {
		args = append(args, "--tail", strconv.Itoa(tailLines))
	}
	result, err := r.container(ctx, args, nil, nil)
	if err != nil {
		return "", commandError("container logs", result, err)
	}
	return strings.TrimSpace(result.Stdout + result.Stderr), nil
}

// --- helpers ---

func (r *containerRuntime) inspectInspect(ctx context.Context, id string) (inspectContainer, error) {
	result, err := r.container(ctx, []string{"inspect", id}, nil, nil)
	if err != nil {
		return inspectContainer{}, commandError("container inspect", result, err)
	}
	containers, err := decodeInspect([]byte(result.Stdout))
	if err != nil {
		return inspectContainer{}, exit(2, "parse container inspect for %s: %v", id, err)
	}
	if len(containers) == 0 {
		return inspectContainer{}, exit(4, "container not found: %s", id)
	}
	return containers[0], nil
}

func (r *containerRuntime) listContainers(ctx context.Context) ([]inspectContainer, error) {
	result, err := r.container(ctx, []string{"ls", "--all", "--format", "json"}, nil, nil)
	if err != nil {
		return nil, commandError("container ls", result, err)
	}
	return decodeInspect([]byte(result.Stdout))
}

func (r *containerRuntime) infoFromInspect(c inspectContainer) core.ContainerInfo {
	labels := c.labels()
	var started time.Time
	if t, err := time.Parse(time.RFC3339, c.Status.StartedAt); err == nil {
		started = t
	}
	return core.ContainerInfo{
		ID:        c.id(),
		Name:      c.id(),
		Image:     c.image(),
		Status:    c.status(),
		IP:        c.ip(),
		Labels:    labels,
		Pid:       c.Status.PID,
		StartedAt: started,
	}
}

func (r *containerRuntime) hostDNSServers(ctx context.Context) []string {
	servers := []string{}
	if result, err := r.rt.Exec.Run(ctx, core.LocalCommandRequest{Name: "scutil", Args: []string{"--dns"}}); err == nil {
		servers = append(servers, parseAppleContainerDNSServers(result.Stdout+result.Stderr)...)
	}
	if len(servers) == 0 {
		if data, err := os.ReadFile("/etc/resolv.conf"); err == nil {
			servers = append(servers, parseAppleContainerDNSServers(string(data))...)
		}
	}
	return uniqueAppleContainerDNSServers(servers, 3)
}
