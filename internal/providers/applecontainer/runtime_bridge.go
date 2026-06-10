package applecontainer

import core "github.com/mentholmike/ciderbox/internal/cli"

// ContainerRuntime exposes the native Apple container lifecycle runtime from
// the provider backend without forcing internal/cli to import this package.
//
// This keeps the existing lease-compatible provider path alive for run/build/
// compile-test while allowing Orchard to use Apple's container runtime directly.
func (b *backend) ContainerRuntime() (core.ContainerRuntime, error) {
	cfg := b.configForRun()
	return NewContainerRuntime(cfg, b.rt)
}
