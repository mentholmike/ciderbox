# Ciderbox Operations

Ciderbox is a local Apple-native container tool. Operations are minimal compared to cloud-based Crabbox.

## Common Tasks

### Build and Test

```bash
go build -o bin/ciderbox ./cmd/ciderbox
go test -race ./...
gofmt -w $(git ls-files '*.go')
```

### Release

See `.goreleaser.yaml` for release configuration. Tags trigger GitHub Actions builds.

### Local Smoke Test

```bash
ciderbox init
ciderbox compile-test
ciderbox chop
```

## Orchard Operations

See [ORCHID.md](../ORCHID.md) for the full Orchard swarm lifecycle.

Quick smoke:

```bash
ciderbox orchard init --force
ciderbox orchard plant
ciderbox orchard graft --all
ciderbox orchard run --sync --task "hello world"
ciderbox orchard harvest --task <task-id>
ciderbox orchard chop --yes
```
