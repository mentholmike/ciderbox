# Orchid - AI Agent Swarm on Apple Container VMs

**A Ciderbox Feature**

---

## What is Ciderbox?

Ciderbox is a fork of [Crabbox](https://github.com/openclaw/crabbox) stripped down to a single provider: **Apple Container**, Apple's native container runtime for macOS.

**Ciderbox exists to:**
- Lease Apple Silicon Linux VMs on your local Mac
- Sync a repo into them
- Run commands, tests, and builds
- Clean up the local VMs afterward

**Key differences from upstream Crabbox:**
- Single provider: `apple-container`
- Local-only by default
- Cider-themed paths and labels such as `/work/ciderbox` and `ciderbox-protected`

---

## What is Orchid?

Orchid is Ciderbox's local OpenClaw swarm layer. It plants Apple Container VMs, called trees, and grafts an OpenClaw runtime/config into each one so tasks can run across the orchard.

**The metaphor:**
- **Orchard** = the swarm
- **Trees** = individual container VMs
- **Plant** = create tree containers
- **Graft** = install/configure OpenClaw on a tree
- **Tend** = inspect live tree health
- **Run** = execute a task on one tree or all trees
- **Harvest** = collect task output
- **Press** = aggregate harvested output
- **Chop** = tear down the orchard

---

## Current Status

### Commands

| Command | Status | Description |
|---------|--------|-------------|
| `orchard init` | Working | Scaffolds `.orchard.yaml` |
| `orchard plant` | Working | Starts N trees through the native Apple Container runtime |
| `orchard tend` | Working | Reads live tree status, IPs, and age from container labels/state |
| `orchard list` | Working | Lists active trees across orchards |
| `orchard graft --tree <id>` | Working | Installs Node/OpenClaw, writes identity/config, and validates OpenClaw config |
| `orchard graft --all` | Working | Grafts every running tree in the orchard |
| `orchard run --task "..."` | Working | Runs an agent command on one tree or all trees and creates a task ID |
| `orchard run --sync --task "..."` | Working | Syncs the current workspace before running the task |
| `orchard harvest` | Working | Collects legacy `/tmp/orchard-result.json` from trees |
| `orchard harvest --task <id>` | Working | Collects structured task results from `/tmp/orchid/tasks/<id>/result.json` |
| `orchard press --input results.json` | Working | Prints a readable summary from harvested results |
| `orchard press --task <id>` | Working | Reads local task result files and prints a summary |
| `orchard secrets init` | Working | Creates `.orchid.env` and updates `.gitignore` |
| `orchard secrets check` | Working | Validates required secrets without printing values |
| `orchard secrets push --all` | Working | Writes `/root/.openclaw/.env` into running trees |
| `orchard login <provider>` | Working | Prints provider-specific auth setup guidance |
| `orchard doctor` | Working | Checks runtime, model config, OpenClaw install, generated files, and config validation |
| `orchard logs --tree <id>` | Working | Shows tree logs |
| `orchard chop --yes` | Working | Removes orchard trees and clears local orchard state |

### Generated Files

On the host:

```text
.orchard.yaml
.orchid.env
~/.ciderbox/orchards/<orchard>/state.json
~/.ciderbox/orchards/<orchard>/tasks/<task-id>/task.json
~/.ciderbox/orchards/<orchard>/tasks/<task-id>/<tree>.json
```

Inside each tree after `orchard graft`:

```text
/root/.openclaw/openclaw.json
/root/.openclaw/.env
/root/.openclaw/workspace/IDENTITY.md
```

Inside each tree after `orchard run`:

```text
/tmp/orchid/tasks/<task-id>/result.json
/tmp/orchard-result.json
```

### Architecture

```text
Host Mac
  ciderbox orchard
      |
      v
Apple Container runtime
      |
      +-- tree-0
      |     /root/.openclaw/openclaw.json
      |     /root/.openclaw/.env
      |     /root/.openclaw/workspace/IDENTITY.md
      |     /tmp/orchid/tasks/<task-id>/result.json
      |
      +-- tree-1
      |
      +-- tree-N
```

Orchid uses `container exec` and the native Apple Container runtime. It does not require SSH for tree management.

---

## Manifest Format

Current `orchard init` output:

```yaml
name: my-orchard
trees: 3
template:
    image: debian:bookworm
    cpus: 2
    memory: 4G
    distro: debian
agent:
    identity: tree-agent
    skills: []
    model: CHANGE_ME
    command: cd "${ORCHARD_WORKSPACE:-/root/.openclaw/workspace}" && openclaw --log-level silent agent --local --agent main --session-key "orchard:${HOSTNAME:-tree}" --message "$ORCHARD_TASK" --timeout 180 --verbose off
secrets:
    envFile: .orchid.env
    required: []
workspace:
    sync: true
    path: /work/ciderbox
```

`agent.command` runs inside each tree. The task text is available as `ORCHARD_TASK`. When workspace sync is enabled, the workspace path is available as `ORCHARD_WORKSPACE`; the generated command starts there before launching OpenClaw.

---

## Secrets and Auth

Use `.orchid.env` for local API keys. Do not put secrets directly in `.orchard.yaml`.

```sh
ciderbox orchard secrets init
# edit .orchid.env
ciderbox orchard secrets check
ciderbox orchard secrets push --all
```

`orchard secrets check` reports whether keys are present and where they came from (`.orchid.env` or host env), but it does not print secret values.

`orchard login` is a helper for provider setup:

```sh
ciderbox orchard login openclaw
ciderbox orchard login openrouter
ciderbox orchard login anthropic
ciderbox orchard login openai
```

For API-key providers, set the matching environment variable in `.orchid.env` or the host environment, then run `orchard secrets check` and `orchard secrets push --all`.

---

## Task Flow

```sh
ciderbox orchard plant
ciderbox orchard graft --all
ciderbox orchard run --sync --task "review this repo and write findings"
ciderbox orchard run --sync -- "review this repo and write findings"
ciderbox orchard harvest --task <task-id> --output results.json
ciderbox orchard press --task <task-id>
ciderbox orchard chop --yes
```

Each `orchard run` creates a task ID like:

```text
task-20260610-235100-123456789
```

Local task state is stored under:

```text
~/.ciderbox/orchards/<orchard>/tasks/<task-id>/
```

Each tree writes structured JSON containing the task ID, tree name, status, output, and exit code.

---

## Linux Support

Orchid graft and runtime dependency installation currently support Debian/Ubuntu-style trees only.

The current graft path expects:

```text
apt-get
curl
ca-certificates
git
gnupg
python3
npm / nodejs
```

The current runtime dependency helper installs missing Python with `apt-get`. Images based on Alpine, Fedora, Arch, or other distributions may plant successfully, but `orchard graft`, `orchard run`, and dependency installation are not considered supported until package-manager detection is implemented.

Future package-manager support should map:

```text
apt-get -> Ubuntu/Debian
apk     -> Alpine
dnf     -> Fedora
pacman  -> Arch
```

---

## Current Gaps

- `orchard init` still emits a smaller manifest than the newer config structs support.
- `harvest --task latest` and `press --task latest` are not implemented yet; pass an explicit task ID.
- Automated Go tests for secrets, generated env content, redaction, task state, and harvest/press behavior still need to be added.
- Tree-to-tree mesh communication is not implemented.
- A prebuilt Orchid image would remove repeated package installation during graft.

---

## Testing

Current local proof:

```sh
go test ./...
go build -o /tmp/ciderbox ./cmd/ciderbox
```

On-device smoke path:

```sh
ciderbox orchard init --force
ciderbox orchard plant
ciderbox orchard graft --all
ciderbox orchard run --sync --task "inspect this tree"
ciderbox orchard harvest --task <task-id> --output results.json
ciderbox orchard press --task <task-id>
ciderbox orchard chop --yes
```

---

## License

Same as Ciderbox/Crabbox: MIT.
