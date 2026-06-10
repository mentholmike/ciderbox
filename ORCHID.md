# Orchid — AI Agent Swarm on Apple Container VMs

**A Ciderbox Feature (Milestone 9)**

---

## What is Ciderbox?

Ciderbox is a fork of [Crabbox](https://github.com/openclaw/crabbox) (by OpenClaw), stripped down to a single provider: **Apple Container** — Apple's native container runtime for macOS (not Docker, not Podman).

**Ciderbox exists to:**
- Lease Apple Silicon VMs on your local Mac (sub-second boot)
- Sync your repo into them
- Run commands, tests, builds
- Clean up automatically

**Key differences from upstream Crabbox:**
- Removed all cloud providers (Hetzner, AWS, Azure, GCP, etc.)
- Single provider: `apple-container`
- Local-only — no broker, no remote infrastructure
- Cider-themed branding (`/work/ciderbox`, `ciderbox-protected` labels)

---

## What is Orchid?

Orchid is **Feature M9** in the Ciderbox roadmap: an **AI agent swarm** running on Apple Container VMs.

Instead of leasing VMs to compile code, Orchid leases VMs to run **autonomous OpenClaw agents** — each tree is an independent AI that can:
- Process tasks
- Communicate with other trees
- Report results back to the host

**The metaphor:**
- **Orchard** = the swarm
- **Trees** = individual container VMs
- **Graft** = install OpenClaw agent on a tree
- **Tend** = check swarm health
- **Harvest** = collect results from all trees
- **Press** = aggregate results into a unified report
- **Chop** = tear down the orchard

---

## What Orchid Does Currently

### Commands

| Command | Status | Description |
|---------|--------|-------------|
| `orchard init` | ✅ Working | Scaffolds `.orchard.yaml` manifest |
| `orchard plant` | ✅ Working | Spins up N trees via apple-container provider |
| `orchard tend` | ⚠️ Simulated | Shows tree status (mocked IPs for now) |
| `orchard graft` | ✅ Working | Installs OpenClaw via `container exec` (no SSH) |
| `orchard harvest` | ⚠️ Simulated | Collects results (structure in place) |
| `orchard press` | ⚠️ Simulated | Aggregates outputs (structure in place) |
| `orchard chop` | ✅ Working | Destroys all trees via provider `ReleaseLease` |
| `orchard list` | ⚠️ Simulated | Lists active trees (placeholder) |

### Architecture

```
Host Mac (OpenClaw with Ciderbox)
├── .openclaw.json          # Main agent config
└── orchard/
    ├── .orchard.yaml       # Swarm manifest
    └── (commands via CLI)

Apple Container VMs (Trees)
├── Tree 0: crabbox-my-orchard-0-xxx
│   ├── Ubuntu 26.04
│   ├── OpenClaw agent (grafted)
│   └── Independent memory/skills
├── Tree 1: crabbox-my-orchard-1-xxx
│   └── ...
└── Tree N...
```

### Key Design Decisions

1. **No SSH** — Uses `container exec` and `container copy` directly
2. **No Lethe** — Each tree uses standard OpenClaw memory (not persistent Lethe)
3. **Provider-native** — Reuses Ciderbox's `SSHLeaseBackend` for lifecycle
4. **Container ID tracking** — Each `TreeState` stores `container_id` for direct `exec` access

### Manifest Format

```yaml
# .orchard.yaml
name: my-orchard
trees: 3
template:
  image: ubuntu:26.04
  cpus: 2
  memory: 4G
  distro: ubuntu
agent:
  identity: archimedes-clone
  skills: [web-search, github, discord]
  memory_provider: builtin
  model: gpt-5.3-codex-spark
mesh:
  mode: gossip
  broadcast: true
  port: 18790
```

---

## What Orchid Will Do

### Immediate Next Steps

| Feature | Priority | Description |
|---------|----------|-------------|
| **Real `tend`** | High | Query `container ls` to show actual tree status, IPs, container IDs |
| **Real `harvest`** | High | Use `container exec` to collect JSON results from each tree |
| **Real `press`** | High | Aggregate harvest results into unified report |
| **Real `list`** | Medium | Filter `container ls` for orchard labels |

### Future Work

| Feature | Description |
|---------|-------------|
| **Mesh communication** | Tree-to-tree messaging via gossip/broker/star topology |
| **Graft OpenClaw** | Actually download and install OpenClaw binary (not just apt) |
| **Tree identity** | Each tree gets unique `.openclaw.json` with skills/model config |
| **Pre-baked images** | Build `orchid-agent` image with OpenClaw pre-installed |
| **Workload distribution** | Distribute tasks across trees, collect parallel results |
| **Auto-scaling** | Add/remove trees based on queue depth |

---

## Code Changes Made

### Files Modified

| File | Change |
|------|--------|
| `internal/cli/orchard.go` | New file — full orchard command suite |
| `internal/cli/app.go` | Registered `orchard` command |
| `internal/cli/cli_kong.go` | Added `orchardKongCmd` struct and wiring |
| `internal/cli/config.go` | Changed default provider from `hetzner` → `apple-container` |
| `internal/cli/compiletest.go` | Fixed `--keep` leak, `strings.Fields` bug, added `ciderbox-protected` |
| `internal/providers/applecontainer/backend.go` | Added macOS 26 version gate, 48h protected ceiling |
| `internal/providers/applecontainer/backend_test.go` | Updated tests for `inspectStatus` struct |

### Bugs Fixed (Code Review)

| # | Issue | Severity | Fix |
|---|-------|----------|-----|
| 1 | `chop` bypassed provider layer | Blocker | Rewrote to use `SSHLeaseBackend` |
| 2 | `--keep` caused VM leak | Blocker | Auto-release on PASS, 48h ceiling |
| 3 | `strings.Fields` broke commands | Major | Use `/bin/sh -lc` instead |
| 4 | No macOS 26 gate | Major | Added `sw_vers` check |
| 5 | Parallel output interleaved | Minor | Added per-distro buffered writers |
| 6 | Identity drift | Minor | `/work/crabbox` → `/work/ciderbox` |

---

## Testing

**Verified on-device:**
```bash
$ ciderbox orchard init --force
$ ciderbox orchard plant
  → Container crabbox-my-orchard-0-xxx ready (13s)
$ ciderbox orchard graft --tree tree-0
  → apt-get update/install via container exec
$ ciderbox orchard chop --yes
  → Container destroyed via ReleaseLease
```

---

## License

Same as Ciderbox/Crabbox — MIT.

*Built by [MentholMike](https://github.com/mentholmike).*
