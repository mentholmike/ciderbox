# image

`crabbox image` manages AWS AMIs for faster runner startup.

```sh
crabbox image current
crabbox image list
crabbox image list --name 'openclaw-crabbox-*'
crabbox image create --id blue-lobster --name openclaw-crabbox-20260501
crabbox image create --id cbx_abcdef123456 --name openclaw-hot --wait --json
```

The brokered path uses the coordinator's AWS credentials and requires admin auth. Direct-provider mode uses local AWS credentials. `image current` shows the AMI that AWS leases will use: configured `aws.ami` / `CRABBOX_AWS_AMI` when set, otherwise the latest Ubuntu 24.04 AMI Crabbox resolves in the configured region.

`image list` returns self-owned AMIs tagged by Crabbox image creation. Pass `--name` to search self-owned images by AMI name glob.

`image create` captures an AMI from an existing AWS lease. By default it runs a best-effort scrub over SSH before calling EC2 `CreateImage`: it removes common AWS/Docker credential stores, Actions env handoff files, shell history, cloud-init logs, and old journal entries. Use `--skip-scrub` only for deliberately disposable boxes that never received secrets.

By default AWS may reboot the instance to capture a more consistent image. `--no-reboot` is faster but can capture inconsistent filesystem state. Use `--wait` when scripts need the AMI to be available before exiting.

Do not bake long-lived credentials into AMIs. Use the AMI for tools, packages, Docker/buildx cache, and base layers; keep runtime secrets in GitHub Actions, instance profiles, SSM, or the coordinator.
