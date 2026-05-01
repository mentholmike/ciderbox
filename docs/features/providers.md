# Providers

Read when:

- changing Hetzner, AWS, or Blacksmith Testbox provisioning;
- adding a backend;
- adjusting machine classes, fallback order, regions, or images.

Crabbox currently supports two brokered providers:

```text
hetzner     Hetzner Cloud servers
aws         AWS EC2 one-time Spot instances
```

Hetzner behavior:

- imports or reuses the lease SSH key;
- creates a server with Crabbox labels;
- uses configured image and location;
- falls back across class server types when capacity or quota rejects a request;
- fetches server-type hourly prices when cost estimates need provider pricing.

AWS behavior:

- signs EC2 Query API calls inside the Worker;
- imports or reuses an EC2 key pair;
- creates or reuses the `crabbox-runners` security group with SSH ingress limited to configured CIDRs or the request source IP;
- launches one-time Spot instances;
- tags instances, volumes, and Spot requests;
- falls back across broad C/M/R instance families;
- uses Spot placement score across configured regions in direct AWS mode;
- can fall back to On-Demand after Spot capacity/quota failures when configured;
- fetches Spot price history when cost estimates need provider pricing.
- can inspect the configured runner AMI, list Crabbox-created AMIs, and create a tagged AMI from an existing AWS lease after best-effort scrub.

Machine classes map to provider-specific types:

```text
Hetzner
standard  ccx33, cpx62, cx53
fast      ccx43, cpx62, cx53
large     ccx53, ccx43, cpx62, cx53
beast     ccx63, ccx53, ccx43, cpx62, cx53

AWS
standard  c7a.8xlarge, c7i.8xlarge, m7a.8xlarge, m7i.8xlarge, c7a.4xlarge
fast      c7a.16xlarge, c7i.16xlarge, m7a.16xlarge, m7i.16xlarge, c7a.12xlarge, c7a.8xlarge
large     c7a.24xlarge, c7i.24xlarge, m7a.24xlarge, m7i.24xlarge, r7a.24xlarge, c7a.16xlarge, c7a.12xlarge
beast     c7a.48xlarge, c7i.48xlarge, m7a.48xlarge, m7i.48xlarge, r7a.48xlarge, c7a.32xlarge, c7i.32xlarge, m7a.32xlarge, c7a.24xlarge, c7a.16xlarge
```

Direct provider mode still exists when no coordinator is configured. It uses local AWS credentials or `HCLOUD_TOKEN`/`HETZNER_TOKEN` and should stay secondary to the brokered path.

AWS images are an operator acceleration layer, not a secret store. Bake Docker, buildx, language runtimes, package caches, and heavy base layers. Keep runtime secrets in the coordinator, GitHub Actions, AWS instance profiles, SSM, or a secrets manager.

Crabbox can also wrap Blacksmith Testboxes with `provider: blacksmith-testbox`. That backend does not use the Crabbox broker or direct cloud credentials. It shells out to the authenticated Blacksmith CLI for `testbox warmup`, `run`, `status`, `list`, and `stop`, while Crabbox keeps local slugs, repo claims, config, and timing summaries. See [Blacksmith Testbox](blacksmith-testbox.md).

Related docs:

- [Infrastructure](../infrastructure.md)
- [Blacksmith Testbox](blacksmith-testbox.md)
- [Runner bootstrap](runner-bootstrap.md)
- [Cost and usage](cost-usage.md)
