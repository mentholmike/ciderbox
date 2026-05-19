# Prebaked Runner Images

Read when:

- creating or promoting Crabbox runner images;
- speeding up desktop/browser QA leases;
- deciding whether state belongs in a provider image, a warm lease, or a repo cache.

Prebaked images store machine capabilities, not scenario state.

## Where Images Live

Provider-owned image storage is the source of truth:

- AWS: AMIs plus their EBS snapshots live in the AWS account. `crabbox image
  promote` stores the selected AMI id in coordinator metadata so future AWS
  brokered leases can use it.
- Hetzner: snapshots/images live in the Hetzner project. Crabbox can already
  boot a configured image through `image`/`CRABBOX_HETZNER_IMAGE`, but
  create/promote lifecycle commands are not implemented for Hetzner yet.
- Blacksmith Testbox: images are owned by Blacksmith/GitHub runner
  infrastructure, not Crabbox.

Do not store image bytes in git, release artifacts, or coordinator durable
state. The coordinator should hold only the current provider image identifier,
promotion metadata, and enough tags to explain provenance.

## Bake Into Images

Good prebake contents:

- OS patches and base packages;
- SSH, Git, rsync, curl, jq, and readiness helpers;
- desktop/browser capabilities for `--desktop --browser` leases;
- screenshot and recording tools such as `scrot`, `ffmpeg`, `xdotool`, and VNC;
- Node 24, corepack/pnpm, build-essential, Python, and common native-addon
  headers when the image targets browser/channel QA;
- Docker Engine and common container plugin support when the target platform
  supports headless Docker;
- empty shared cache directories such as `/var/cache/crabbox/pnpm`.

Bad prebake contents:

- personal or CI secrets;
- browser profiles, Slack/Discord/WhatsApp login state, cookies, or OAuth
  tokens;
- repository checkouts, `node_modules`, built product `dist/`, or PR artifacts;
- one-off debugging files.

## Runtime Caches

Runtime caches belong outside the image:

- warm leases can keep `/var/cache/crabbox/pnpm` and browser profiles for
  short-lived operator sessions;
- GitHub Actions should cache candidate pnpm stores by lockfile and platform;
- product-specific runtime bundles and evidence artifacts belong in the repo
  workflow workspace, for example under `.artifacts/qa-e2e/...`;
- long-lived reusable volumes should be keyed by repo, lockfile, Node version,
  platform, and image id before Crabbox mounts them into leases.

This split keeps images reusable across repositories while still letting slow QA
systems skip repeated dependency work when they deliberately reuse a warm lease
or a keyed external cache.

## Operator Flow

Use the [Image bake runbook](image-bake-runbook.md) for the exact AWS bake,
candidate smoke, promotion, rollback, and cleanup commands. At a high level:

1. Warm a fresh `--desktop --browser` AWS lease.
2. Verify the machine capability contract on that lease.
3. Create an AMI with `crabbox image create --wait`.
4. Boot the AMI explicitly through an image override and smoke it.
5. Promote the AMI with `crabbox image promote`.
6. Run a normal brokered lease and the relevant QA lane.
7. Keep the previous known-good AMI until the new image has real QA proof.

Image bake success is not just "Chrome exists." A useful image must reduce
`crabbox.warmup` or `crabbox.remote_run` time in timing evidence while keeping
project credentials, browser login state, and repository artifacts outside the
image.

Related docs:

- [Image bake runbook](image-bake-runbook.md)
- [image command](../commands/image.md)
- [Runner bootstrap](runner-bootstrap.md)
- [Interactive desktop and VNC](interactive-desktop-vnc.md)
