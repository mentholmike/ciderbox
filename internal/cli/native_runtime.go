package cli

import "fmt"

// NativeContainerRuntimeBackend is implemented by providers that expose a
// native container lifecycle runtime.
//
// Keep this interface in cli to avoid importing applecontainer from cli, which
// would create an import cycle because applecontainer already imports cli.
type NativeContainerRuntimeBackend interface {
	ContainerRuntime() (ContainerRuntime, error)
}

func (a App) nativeContainerRuntime(providerName string, cfg Config, rt Runtime) (ContainerRuntime, error) {
	provider, err := ProviderFor(providerName)
	if err != nil {
		return nil, err
	}

	backend, err := provider.Configure(cfg, rt)
	if err != nil {
		return nil, err
	}

	native, ok := backend.(NativeContainerRuntimeBackend)
	if !ok {
		return nil, fmt.Errorf("provider %q does not expose native ContainerRuntime", providerName)
	}

	return native.ContainerRuntime()
}
