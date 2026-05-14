# checkpoint

`crabbox checkpoint` saves a useful remote state and starts fresh leases from it
later.

Use it when setup is expensive or a bug is already staged: install
dependencies once, pause a reproducer, keep generated fixtures, then fork that
state for repeated test runs.

The current native checkpoint backends are brokered AWS, Azure, and GCP Linux
leases. The default `auto` mode creates the provider's boot-disk snapshot when
the source lease supports it:

- AWS creates an EBS snapshot.
- Azure creates a managed OS disk snapshot in the configured resource group.
- GCP creates a Compute Engine persistent disk snapshot.

Forking the checkpoint launches a new VM from that provider snapshot. Use
`--strategy image` on AWS or GCP when you specifically want the older provider
image primitive: AWS AMI or GCP machine image. Azure managed images require a
stopped/generalized source VM, so active Azure leases use disk snapshots.

On providers without a real VM snapshot primitive, Crabbox falls back to
`workspace-archive`: a local tar archive of the POSIX SSH lease workdir. This
preserves files in the workspace, not the whole machine.

`crabbox checkpoint` is different from `crabbox image promote`. A checkpoint is
explicit: you fork it by ID when you want that prepared scenario. A promoted
image changes the default AWS runner image for future brokered AWS leases.

## Create

```sh
crabbox checkpoint create --id <lease-id-or-slug> --name after-install
crabbox checkpoint create --id <lease-id-or-slug> --mode native --wait
crabbox checkpoint create --id <lease-id-or-slug> --strategy image
crabbox checkpoint create --id <lease-id-or-slug> --mode archive
crabbox checkpoint create --id <lease-id-or-slug> --workdir /work/cbx_123/my-app
```

Useful flags:

```text
--id <lease-id-or-slug>   lease to snapshot
--name <name>             human-readable name
--mode auto|native|archive default auto
--strategy auto|disk-snapshot|image default auto
--wait                    wait for native snapshot availability, default true
--wait-timeout <duration> default 45m
--no-reboot               AWS AMI CreateImage NoReboot when using --strategy image, default true
--workdir <path>          remote workdir, default is the repo workdir for the lease
--recipe-only             record metadata without archiving files
--reclaim                 claim the lease for the current repo
```

Native Linux checkpoints clean cloud-init state before imaging so forked VMs
can run fresh user-data and install their own per-lease SSH key.

Native provider checkpoints keep machine-level state: installed packages,
toolchains, system caches, services, and files on the root volume. They may
also keep secrets or one-off debugging state if those were written to disk, so
create them from intentional leases and delete them when they are no longer
needed.

Workspace archives intentionally skip `.crabbox/env` and `.crabbox/scripts` so
profile-backed secret helpers are not silently persisted.

`--recipe-only` records metadata only. It is useful for future workflow design,
but current `restore` and `fork` commands require either `workspace-archive` or
a native provider checkpoint.

## List And Inspect

```sh
crabbox checkpoint list
crabbox checkpoint inspect chk_123
crabbox checkpoint inspect chk_123 --json
```

Checkpoints live in the local Crabbox state directory under `checkpoints/`.
For native checkpoints, that local record points at provider-side snapshot/image
resources. Keep the local metadata and the provider resources together; if
either side is deleted, the checkpoint cannot be forked normally.

## Restore

```sh
crabbox checkpoint restore chk_123 --id <lease-id-or-slug>
crabbox checkpoint restore chk_123 --id <lease-id-or-slug> --clear=false
```

Restore uploads the local archive to the target and extracts it into the target
lease's normal repo workdir by default. Use `--workdir` to restore somewhere
else. VM-level checkpoints cannot restore onto an already-running lease; fork
them into a new lease.

## Fork

```sh
crabbox checkpoint fork chk_123 --class beast
```

Fork leases a new SSH-backed box, restores the checkpoint, prints the lease id
and slug, and keeps the lease by default. For native provider checkpoints, fork
launches a fresh lease from the checkpoint snapshot or image, then moves the
captured source workdir to the fork's normal per-lease workdir so
`crabbox run --id <fork>` starts in the snapshotted scenario.

Fast test loop:

```sh
crabbox warmup --provider aws --class beast
crabbox run --id blue-lobster --shell 'npm ci && npm test'
crabbox checkpoint create --id blue-lobster --name after-npm-ci
crabbox checkpoint fork chk_123 --class beast
crabbox run --id <forked-slug> -- npm test
```

Use `--mode archive` when you only need the workdir and want a portable local
artifact. Use the default `auto` mode on AWS, Azure, or GCP when the machine
setup itself is the expensive part.

## Delete

```sh
crabbox checkpoint delete chk_123
crabbox checkpoint delete chk_123 --local-only
```

Deleting a native checkpoint removes the provider snapshot/image before
removing the local checkpoint record. AWS snapshot checkpoints delete the EBS
snapshot; AWS image checkpoints deregister the AMI and delete its EBS
snapshots. Azure deletion removes the managed disk snapshot or managed image.
GCP deletion removes the disk snapshot or machine image. Use `--local-only`
only when the provider resource was already removed outside Crabbox.

AWS EBS snapshots, Azure managed disk snapshots, Azure managed images, GCP disk
snapshots, and GCP machine images can incur storage cost while they exist.
Prefer naming checkpoints after the scenario they preserve, inspect old
checkpoints periodically, and delete stale ones.

## Boundary

Native checkpoints are provider-specific. The default native backends are AWS
EBS snapshots, Azure managed OS disk snapshots, and GCP persistent disk
snapshots for brokered Linux leases. AWS AMI and GCP machine-image checkpoints
remain available with `--strategy image`. Windows native checkpoints are not
supported yet. Proxmox VM snapshots/clones and sandbox-provider snapshots fit
the same command contract, but plain SSH targets still use workspace archives
unless the target exposes a real snapshot API that Crabbox owns.
