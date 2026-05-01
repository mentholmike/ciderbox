# Performance

Read when:

- making remote runs faster;
- choosing machine classes;
- changing sync behavior;
- tuning Actions hydration.

Crabbox performance comes from avoiding repeated setup, keeping the sync small, choosing available capacity, and reusing project-defined hydration when it matters.

## Warm Leases

Use `warmup` for repeated agent loops:

```sh
bin/crabbox warmup --class beast
bin/crabbox run --id blue-lobster -- pnpm test:changed:max
```

Warm leases avoid waiting for a fresh VM and preserve package caches outside the synced source tree. They release after the idle timeout, default `30m`, if untouched. Use `crabbox stop blue-lobster` when the loop is done.

## Sync Size

The CLI syncs a Git-derived manifest: tracked files plus nonignored untracked files. Ignored build output, dependency folders, `.git`, and common local caches are excluded before rsync sees the tree. Each run prints the candidate file count and byte estimate, then warns or fails if the manifest crosses configured guardrails.

Good habits:

- keep generated artifacts and dependency folders out of the synced tree;
- tune repo-local excludes in `.crabbox.yaml`;
- keep `.gitignore` current so local build junk never enters the manifest;
- raise `sync.failFiles` or `sync.failBytes` only for projects that intentionally sync very large source trees.

## Sync Fingerprints

The CLI records a local/remote fingerprint after sync. If nothing changed, hot runs skip the expensive rsync pass. The fingerprint includes the commit, dirty metadata, sync config, and manifest, so adding a nonignored untracked file invalidates the skip while ignored cache churn does not.

Good habits:

- avoid broad local deletes unless they are intentional;
- use `inspect` when diagnosing stale remote state.

## Git Hydration

Crabbox seeds remote Git when possible, then overlays the dirty local checkout with rsync. It also hydrates configured base-ref history so changed-file commands can compare against the expected base.

This matters for commands such as:

```sh
pnpm test:changed:max
pnpm check:changed
git diff --name-only origin/main...
```

## Package And Tool Caches

Runner bootstrap prepares shared cache directories, but does not install project runtimes. Package-manager and Docker caches are best-effort speedups once the repository setup installs those tools; they must not be treated as source of truth.

Use explicit cache commands on kept leases:

```sh
bin/crabbox cache stats --id blue-lobster
bin/crabbox cache warm --id blue-lobster -- pnpm install --frozen-lockfile
bin/crabbox cache purge --id blue-lobster --kind pnpm --force
```

For repeatable setup that needs repository secrets, use Actions hydration:

```sh
bin/crabbox actions hydrate --id blue-lobster
bin/crabbox run --id blue-lobster -- pnpm test:changed:max
```

The workflow owns dependency installation, cache/service setup, and secret-backed environment hydration. Crabbox attaches later commands to the hydrated workspace.

## Machine Classes

Use the smallest class that keeps the target command CPU-bound without creating queue or quota failures.

Typical choices:

- `standard`: cheap smoke checks and small repos.
- `fast`: general maintainer testing.
- `large`: broad test shards or heavy builds.
- `beast`: high-core changed-test runs.

Hetzner dedicated classes can hit account quota. AWS Spot classes can hit regional capacity. For AWS, `CRABBOX_CAPACITY_STRATEGY=most-available` and multiple `CRABBOX_CAPACITY_REGIONS` give the coordinator more room to find capacity.

## Measure The Loop

Use wall-clock timing around the whole command, not just the remote test process:

```sh
/usr/bin/time -p bin/crabbox run --id cbx_... -- pnpm test:changed:max
```

The useful number includes lease wait, SSH readiness, sync, Git hydration, command execution, and release. For warm leases, sync fingerprints and package caches should make repeated runs much faster than cold runs.

Coordinator-backed runs also retain structured metrics in `history --json`:

```sh
bin/crabbox history --lease cbx_... --json
```

Use `syncMs`, `commandMs`, `durationMs`, `syncFiles`, `syncBytes`, `syncDeleted`, `syncManifestBytes`, and `syncSkipped` to separate source sync overhead from the remote command itself. If memory looks high, compare those values against `free -h`, `ps aux --sort=-rss | head`, `docker system df`, and `bin/crabbox cache stats --id cbx_...` on the same lease before blaming the coordinator path.
