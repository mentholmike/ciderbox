# Runner Bootstrap

Read when:

- changing cloud-init;
- debugging machines that never become SSH-ready;
- changing the minimal runner contract or readiness checks.

Each runner is an Ubuntu machine prepared by cloud-init. It does not need coordinator credentials.

Bootstrap creates:

- the `crabbox` user;
- SSH key-only access;
- SSH on port `2222`;
- `/work/crabbox`;
- shared package caches.

Bootstrap installs:

- curl and CA certificates;
- Git;
- rsync;
- jq;
- OpenSSH server.

Bootstrap intentionally does not install project language runtimes such as Go, Node, pnpm, Docker, databases, or service dependencies. Those belong in GitHub Actions hydration, devcontainers, Nix, mise/asdf, repository setup scripts, or a trusted AWS AMI selected with `aws.ami` / `CRABBOX_AWS_AMI`. A machine should not pass readiness until `crabbox-ready` succeeds over SSH.

The CLI prefers the configured SSH port and can fall back to port 22 during early bootstrap. `crabbox image create` can capture a scrubbed AWS AMI from a warmed lease when cloud-init plus hydration is too slow for repeated work.

Related docs:

- [Providers](providers.md)
- [SSH keys](ssh-keys.md)
- [run command](../commands/run.md)
- [doctor command](../commands/doctor.md)
