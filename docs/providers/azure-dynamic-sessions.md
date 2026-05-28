# Azure Dynamic Sessions Provider

Use `provider: azure-dynamic-sessions` to run Linux commands inside Microsoft
Azure Container Apps custom container dynamic sessions. Azure owns the
Hyper-V-isolated session pool and lifecycle; Crabbox owns the runner image,
local claims, archive sync, command streaming, and timing output.

Microsoft docs:

- https://learn.microsoft.com/en-us/azure/container-apps/session-pool
- https://learn.microsoft.com/en-us/azure/container-apps/sessions-custom-container

## Requirements

- An Azure Container Apps custom container dynamic session pool.
- The pool image exposes the Crabbox runner HTTP API on port `8787`; build it
  from `worker/azure-dynamic-sessions.Dockerfile`.
- The caller has the `Azure ContainerApps Session Executor` role on that pool.
- `az login` is available locally, or `CRABBOX_AZURE_DYNAMIC_SESSIONS_TOKEN`
  contains a bearer token for `https://dynamicsessions.io`.

## Pool Setup

Build and publish the runner image to a registry Azure can pull. For private
Azure Container Registry images, prefer a managed identity with `AcrPull` on the
registry and pass that identity through `--registry-identity` instead of putting
registry passwords on the command line:

```sh
az acr login --name <registry>

docker buildx build \
  --platform linux/amd64 \
  --push \
  --tag <registry>.azurecr.io/crabbox-runner:<tag> \
  --file worker/azure-dynamic-sessions.Dockerfile \
  worker

identity_id="$(az identity show \
  --name <pull-identity> \
  --resource-group crabbox-sandboxes-rg \
  --query id \
  --output tsv)"

identity_principal_id="$(az identity show \
  --name <pull-identity> \
  --resource-group crabbox-sandboxes-rg \
  --query principalId \
  --output tsv)"

registry_id="$(az acr show \
  --name <registry> \
  --query id \
  --output tsv)"

az role assignment create \
  --assignee "$identity_principal_id" \
  --role AcrPull \
  --scope "$registry_id"

az containerapp sessionpool create \
  --name crabboxpool \
  --resource-group crabbox-sandboxes-rg \
  --environment crabbox-env \
  --registry-server <registry>.azurecr.io \
  --registry-identity "$identity_id" \
  --container-type CustomContainer \
  --image <registry>.azurecr.io/crabbox-runner:<tag> \
  --target-port 8787 \
  --cpu 0.25 \
  --memory 0.5Gi \
  --cooldown-period 300 \
  --max-sessions 20 \
  --ready-sessions 1 \
  --network-status EgressEnabled \
  --location francecentral
```

Fetch the pool endpoint:

```sh
az containerapp sessionpool show \
  --name crabboxpool \
  --resource-group crabbox-sandboxes-rg \
  --query "properties.poolManagementEndpoint" \
  --output tsv
```

The bundled image is intentionally small: it contains the Crabbox runner plus
basic shell, Git, tar, curl, jq, and ripgrep. Extend the Dockerfile or use your
own image when a pool needs Node, Go, Python, browsers, or other test runtimes.

## Configuration

Set the full custom container pool management endpoint:

```yaml
provider: azure-dynamic-sessions
target: linux
azureDynamicSessions:
  endpoint: https://<pool>.<environment-id>.eastus.azurecontainerapps.io
  workdir: /workspace/crabbox
```

Provider flags:

```text
--azure-dynamic-sessions-endpoint
--azure-dynamic-sessions-pool
--azure-dynamic-sessions-api-version
--azure-dynamic-sessions-workdir
--azure-dynamic-sessions-timeout-secs
```

Environment overrides use the same names with the `CRABBOX_` prefix, for
example `CRABBOX_AZURE_DYNAMIC_SESSIONS_ENDPOINT` and
`CRABBOX_AZURE_DYNAMIC_SESSIONS_POOL`.

## Behavior

- `warmup` allocates a random `azds-...` session by calling the runner
  `/health` endpoint through the pool management endpoint.
- `run` syncs the checkout by uploading a `.tgz` archive to the runner,
  extracts it under `azureDynamicSessions.workdir`, then streams the command
  through the runner.
- `--keep` retains the session for reuse. Without `--keep`, Crabbox calls the
  custom container `/.management/stopSession` endpoint after the run.
- `run`, `status`, and `stop` accept kept Crabbox lease IDs or slugs from local
  claims; raw Dynamic Sessions identifiers are not accepted.
- `status` and `list` use `/.management/getSession` and
  `/.management/listSessions` plus local Crabbox claims.

## Limitations

- Linux delegated `run`, `warmup`, `status`, `list`, `stop`, and `doctor` are
  supported.
- SSH, VNC, browser, web code, Actions hydration, downloads, and artifacts are
  not supported because the provider has no Crabbox SSH target.
- `--class` and `--type` are rejected; size and egress belong to the Azure
  session pool configuration.
- `--checksum` is rejected because sync uses archive upload/extract instead of
  rsync.
