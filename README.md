# 🍎 Ciderbox

**Apple-native throwaway dev environments.** A fork of [crabbox](https://github.com/openclaw/crabbox) that replaces Docker with Apple's native `container` CLI for sub-second, hypervisor-isolated Linux VMs on Apple Silicon Macs.

```sh
# Test your code across multiple Linux distros
ciderbox compile-test

# Or run a one-off command in a fresh Ubuntu VM
ciderbox run -- uname -a

# Clean up everything when done
ciderbox chop
```

## Requirements

- Apple Silicon Mac (M1+)
- macOS 26+ (for container IP reachability)
- Apple `container` CLI installed
- Go 1.26+ (for building from source)

## Install

### Homebrew (recommended)

```sh
brew tap mentholmike/ciderbox
brew install ciderbox
```

Or in one line:

```sh
brew install mentholmike/ciderbox/ciderbox
```

### From source

```sh
git clone https://github.com/mentholmike/ciderbox.git
cd ciderbox
go build ./cmd/ciderbox
mv ciderbox /usr/local/bin/
```

## Quick Start

### 1. Initialize a project

```sh
cd my-project
ciderbox init
```

This creates `.ciderbox.yaml` with your project config.

### 2. Configure your test matrix

Edit `.ciderbox.yaml`:

```yaml
project: my-project
compileTest:
  distros:
    - name: ubuntu
      image: ubuntu:26.04
    - name: debian
      image: debian:bookworm
  command: "make test"
  parallel: true
build:
  image: ubuntu:26.04
  command: "make build"
  dependencies: [build-essential, git]
run:
  provider: apple-container
  image: ubuntu:26.04
```

### 3. Run compile tests

```sh
ciderbox compile-test
```

Runs your test command in parallel across all configured distros. Results show as a pass/fail grid with timing.

### 4. Clean up

```sh
ciderbox chop
```

Terminates all active ciderbox containers.

## Commands

| Command | Description |
|---------|-------------|
| `ciderbox init` | Scaffold `.ciderbox.yaml` |
| `ciderbox compile-test` | Run tests across configured distros |
| `ciderbox build` | Single-distro build |
| `ciderbox run -- <cmd>` | One-off command in fresh VM |
| `ciderbox warmup` | Create a warm/persistent VM |
| `ciderbox ssh --id <slug>` | SSH into a warm VM |
| `ciderbox doctor` | Check environment |
| `ciderbox chop` | Kill all ciderbox VMs |
| `ciderbox list` | Show active leases |
| `ciderbox stop <id>` | Stop a specific lease |

### Orchard (AI Agent Swarm)

Orchid is a swarm management feature for running distributed OpenClaw agents:

```sh
# Initialize an orchard manifest
ciderbox orchard init

# Spin up the swarm
ciderbox orchard plant

# Check tree health
ciderbox orchard tend

# Install OpenClaw on a tree
ciderbox orchard graft --tree tree-0

# Collect results from all trees
ciderbox orchard harvest --output results.json

# Aggregate into unified report
ciderbox orchard press

# Tear down the swarm
ciderbox orchard chop --yes
```

See [ORCHID.md](ORCHID.md) for full documentation.

## How It Works

```text
your Mac (Apple Silicon)
    |
    |  ciderbox CLI
    v
Apple container CLI  -->  Linux VM (ubuntu, debian, alpine...)
    |                           |
    |  SSH                      |  your code synced in
    v                           v
run tests, collect      /work/ciderbox/my-project
results, teardown
```

## Differences from crabbox

| Feature | crabbox | ciderbox |
|---------|---------|----------|
| Runtime | Docker/OrbStack/Colima | Apple `container` CLI |
| Networking | Port publishing | Direct container IPs |
| Target | Cloud + local | Apple Silicon Macs only |
| Compile test | Not built-in | First-class feature |
| Repo pattern | Manual | `.ciderbox.yaml` config |
| Cleanup | Per-lease | `chop` all at once |

## Why Apple container?

Apple's `container` CLI (from [apple/container](https://github.com/apple/container)) provides:

- **Sub-second boots** after first image pull
- **Dedicated IPs** per VM — no port collision
- **Hypervisor isolation** per lease
- **Persistent machines** via `container machine`
- **Native home mount** — your Mac `$HOME` visible inside VMs

No Docker Desktop, no daemon socket, no port mapping headaches.

## Configuration

### `.ciderbox.yaml`

```yaml
project: my-project
compileTest:
  distros:
    - name: ubuntu
      image: ubuntu:26.04
    - name: debian
      image: debian:bookworm
    - name: alpine
      image: alpine:latest
  command: "make test"
  parallel: true
build:
  image: ubuntu:26.04
  command: "make build"
  dependencies: [build-essential, git, nodejs]
  cachePaths:
    - /root/.cache/go-build
    - /root/.npm
```

### Runtime Dependencies

Both `compile-test` and `build` support installing packages at runtime:

```yaml
compileTest:
  distros:
    - name: ubuntu
      image: ubuntu:26.04
  command: "make test"
  dependencies: [build-essential, libssl-dev, python3]

build:
  image: ubuntu:26.04
  command: "make build"
  dependencies: [build-essential, git]
```

This translates to:
```bash
apt-get update && apt-get install -y --no-install-recommends build-essential libssl-dev python3 && make test
```

Use this when your project needs system packages that aren't in the base image.

### `.orchard.yaml`

For AI agent swarms, see [ORCHID.md](ORCHID.md).

```yaml
name: my-orchard
trees: 3
template:
  image: ubuntu:26.04
  cpus: 2
  memory: 4G
agent:
  identity: archimedes-clone
  skills: [web-search, github, discord]
  model: gpt-5.3-codex-spark
mesh:
  mode: gossip
  broadcast: true
```

## Troubleshooting

### "apple-container provider not found"

Install Apple container CLI:
```sh
# Download from https://github.com/apple/container/releases
sudo installer -pkg container-*.pkg -target /
container system start
```

### "container stopped before network address assigned"

Image lacks SSH server. Use Ubuntu/Debian base images, or install `openssh-server` in your Dockerfile.

### SSH connection refused

Container needs time to boot. The provider waits up to 20 minutes for SSH — but usually connects in 1-2 seconds with pre-pulled images.

### "No active ciderbox containers found"

If `chop` reports no containers but VMs are running, they may be protected:

```sh
# Force chop protected containers
ciderbox chop --force
```

## Changelog

### v0.2.0 — Orchid (AI Agent Swarm)
- Added `orchard` command suite for distributed AI agent workloads
- Uses `container exec` (no SSH required for tree management)
- Supports swarm manifests via `.orchard.yaml`
- See [ORCHID.md](ORCHID.md) for details

### v0.1.0 — Initial Fork
- Forked from crabbox, stripped to apple-container provider only
- Added `compile-test` for multi-distro testing
- Added `build` for single-distro builds
- Added `chop` with `--force` for protected leases
- Fixed `strings.Fields` command parsing bug
- Added macOS 26 version gate

## License

MIT — forked from [crabbox](https://github.com/openclaw/crabbox) by OpenClaw.
