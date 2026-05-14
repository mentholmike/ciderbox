# CF Containers Provider

Use `provider: cf-containers` when Crabbox should run commands through a
Cloudflare Worker backed by a custom Cloudflare Containers image. The provider
also accepts the aliases `cloudflare-containers`, `cloudflare-container`, and
`cf-container`.

CF Containers is a delegated run provider. Crabbox owns local repo archive
creation, local lease claims, timing output, command rendering, and friendly
slugs. A small Worker runner owns container creation, file upload, command
execution, and teardown.

## Requirements

- A Cloudflare account with Workers, Durable Objects, and Containers access.
- Wrangler authenticated for deploys.
- Docker or a Docker-compatible CLI/daemon available to Wrangler for container
  image builds.
- A deployed Crabbox CF Containers runner with `CRABBOX_RUNNER_TOKEN` set
  as a Worker secret.

The Worker coordinator lives in `worker/src/cloudflare-container-runner.ts`. The
container image is built from `worker/cloudflare-container.Dockerfile` and starts
the HTTP runner in `worker/cloudflare-container-runner`. The deploy config is
`worker/wrangler.cloudflare-container.jsonc`.

## Configuration

```yaml
provider: cf-containers
cfContainers:
  apiUrl: https://crabbox-cloudflare-container-runner.example.workers.dev
  workdir: /workspace/crabbox
```

Keep the bearer token in `CRABBOX_CF_CONTAINERS_TOKEN` or user-level config,
not in repo YAML. `CRABBOX_CF_CONTAINERS_URL` or
`CRABBOX_CF_CONTAINERS_API_URL` can also provide the runner URL.

With the token already available from `CRABBOX_CF_CONTAINERS_TOKEN` or user
config, the runner URL can also be supplied as a flag:

```sh
crabbox run \
  --provider cf-containers \
  --cf-containers-url https://runner.example.workers.dev \
  -- pnpm test
```

## Runner Deploy

Install Worker dependencies and verify the runner:

```sh
npm ci --prefix worker
npm run check:cf-containers --prefix worker
npm run build:cf-containers --prefix worker
```

Deploy with:

```sh
npm run deploy:cf-containers --prefix worker
```

The deploy script uses Wrangler's immediate container rollout mode so a Worker
deploy updates the backing container image and `instance_type` in the same
operation. When deploying manually, include the same flag:

```sh
npx wrangler deploy \
  --config worker/wrangler.cloudflare-container.jsonc \
  --containers-rollout=immediate
```

Then set the bearer token:

```sh
printf '%s' "$CRABBOX_CF_CONTAINERS_TOKEN" \
  | npx wrangler secret put CRABBOX_RUNNER_TOKEN \
      --config worker/wrangler.cloudflare-container.jsonc
```

The checked-in runner config uses the `standard-4` container instance type so
agent test workloads have the largest predefined disk budget. Cloudflare
Containers tie disk to the instance type; reduce `instance_type` only for small
smoke-test runners, or use a custom instance type when the account allows it.

Check the active container app after deploy:

```sh
npx wrangler containers list \
  --config worker/wrangler.cloudflare-container.jsonc
npx wrangler containers info <container-application-id> \
  --config worker/wrangler.cloudflare-container.jsonc
```

## Live Smoke

With `CRABBOX_CF_CONTAINERS_TOKEN` available and `cfContainers.apiUrl` set,
start with a no-sync smoke so the runner, token, image, disk, and package cache
settings are exercised before uploading a repository archive:

```sh
crabbox run \
  --provider cf-containers \
  --no-sync \
  --timing-json \
  --shell \
  -- 'df -h / /tmp /workspace; printf "npm cache=%s\n" "${NPM_CONFIG_CACHE:-}"; printf "pnpm store="; pnpm config get store-dir'
```

Stop the printed lease ID when the smoke is complete:

```sh
crabbox stop --provider cf-containers <lease-id>
```

## Behavior

- `run` creates or reuses a Container Durable Object, uploads a gzipped archive
  of the local checkout, extracts it into `workdir`, and relays command output
  and exit status back to the CLI.
- Before uploading an archive, the provider checks remote disk headroom for the
  compressed archive plus extracted checkout and fails early with a sizing hint
  when the selected instance type is too small.
- `warmup` creates the container and prepares the workdir. Warmed containers
  remain alive until `crabbox stop` or the configured TTL/idle deadline
  expires.
- `status` and `stop` resolve Crabbox's local claim and call the runner.
- `list` reports local CF Containers claims. Cloudflare does not expose a
  global container listing API through the runner.
- `worker/cloudflare-container.Dockerfile` is the default Crabbox runner image.
  Operators can replace it in Wrangler config when they need a different
  language or toolchain baseline.
- The default image includes common repo-test tools such as Git, GitHub CLI,
  `jq`, `ripgrep`, Go, Node, and `pnpm`; project-specific dependencies still
  belong to the repo's own setup commands.
- npm and pnpm package caches live under `/var/cache/crabbox` so a warmed
  container can reuse package downloads across repeated commands.
- Warmed containers keep their container filesystem between commands while the
  lease is active. Use that as the provider's cache layer for cloned
  repositories, package stores, and generated setup state.
- The runner stores lease metadata in the Container Durable Object and schedules
  cleanup at the earlier of `--ttl` or `--idle-timeout`. Activity on file upload
  or command execution extends the idle deadline. When the deadline passes, the
  runner destroys the container and marks the lease expired.
- `crabbox cleanup --provider cf-containers` cannot discover every remote
  container, but it checks local CF Containers claims and removes entries
  whose runner state is expired, stopped, or missing.

## Limitations

- SSH, VNC, browser desktop, code-server, Actions hydration, and `--download`
  are not supported.
- `--fresh-pr` is not supported for delegated archive sync.
- `--checksum` is not supported because the provider uses archive upload and
  extraction instead of Crabbox rsync.
- Container size and concurrency are controlled by
  `worker/wrangler.cloudflare-container.jsonc`. Choose an `instance_type` and
  `max_instances` that match the account's Cloudflare Containers limits.
- Cloudflare can roll container changes separately from Worker script changes.
  Use `npm run deploy:cf-containers --prefix worker` or pass
  `--containers-rollout=immediate` when running Wrangler directly.
