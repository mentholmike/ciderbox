# admin

`crabbox admin` contains trusted operator controls for coordinator-backed leases.

```sh
crabbox admin leases
crabbox admin leases --state active --json
crabbox admin lease-audit --state expired --provider aws
crabbox admin lease-audit --fail-on-live
crabbox admin mac-hosts list --region eu-west-1
crabbox admin mac-hosts allocate --region eu-west-1 --availability-zone eu-west-1a --type mac2.metal --force
crabbox admin mac-hosts release h-0123456789abcdef0 --region eu-west-1 --force
crabbox admin release blue-lobster
crabbox admin release blue-lobster --delete
crabbox admin delete cbx_... --force
```

Release/delete accept a canonical `cbx_...` ID or an active slug; use the canonical ID when an admin slug lookup is ambiguous. Add `--json` to print the updated lease record.

Admin commands require a configured coordinator and a separate admin bearer token
stored as `broker.adminToken` or `CRABBOX_COORDINATOR_ADMIN_TOKEN`. The shared
operator token is not enough for admin routes.

## leases

List coordinator lease records.

Flags:

```text
--state <state>     filter by active, released, expired, or failed
--owner <email>     filter by owner
--org <name>        filter by org
--limit <n>         default 100, maximum 500
--json              print JSON
```

## lease-audit

Check expired coordinator lease records against the backing cloud provider.
The audit currently supports AWS leases and reports whether each expired
`cloudID` is still present, missing, or could not be checked.

Flags:

```text
--state <state>     default expired
--provider <name>   default aws
--owner <email>     filter by owner
--org <name>        filter by org
--limit <n>         default 100, maximum 500
--fail-on-live      exit non-zero for live cloud instances or audit errors
--json              print JSON
```

## mac-hosts

List, allocate, or release AWS EC2 Mac Dedicated Hosts through the coordinator.
`list` is read-only. `allocate` and `release` require `--force` because EC2 Mac
Dedicated Hosts are billed separately from Crabbox leases and have AWS lifecycle
constraints.

Flags:

```text
list:
  --region <region>     AWS region
  --type <type>         filter by mac1.metal, mac2.metal, or another Mac type
  --state <state>       filter by host state
  --json                print JSON

allocate:
  --region <region>             AWS region
  --availability-zone <az>      required, for example eu-west-1a
  --type <type>                 default mac2.metal
  --force                       confirm host allocation
  --json                        print JSON

release:
  --id <host-id> or positional host id
  --region <region>
  --force                       confirm host release
  --json                        print JSON
```

## release

Mark a lease released. Add `--delete` to delete the backing server while releasing.

Flags:

```text
--id <lease-id-or-slug>
--delete
--json
```

## delete

Delete the backing server for an active lease and mark it released. Requires `--force`.

Flags:

```text
--id <lease-id-or-slug>
--force
--json
```

Related docs:

- [Operations](../operations.md)
- [Auth and admin](../features/auth-admin.md)
