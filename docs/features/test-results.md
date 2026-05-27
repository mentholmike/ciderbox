# Test Results

Read when:

- adding result formats;
- changing how failed tests are summarized;
- debugging why `crabbox results` has no data.

Crabbox can attach JUnit XML summaries to coordinator run history. The agent uses this so a failed run can answer "which tests failed?" without scraping a large raw log.

Configure per run:

```sh
crabbox run --id cbx_... --junit junit.xml -- go test ./...
crabbox run --id cbx_... --results-auto -- go test ./...
```

Or per repo:

```yaml
results:
  auto: true
  junit:
    - junit.xml
    - reports/junit.xml
```

After the command exits, the CLI reads configured remote files from the workdir, or scans common JUnit XML names when auto discovery is enabled. Auto discovery only considers reports written after the command starts, using Crabbox metadata when the workdir is a Git checkout so clean-worktree checks are not dirtied, and `.crabbox` metadata otherwise. It skips dependency and Git directories, sniffs for JUnit XML before counting a file, prioritizes reports with failures or errors, and caps remote reads before parsing. It parses JUnit and sends only the summary to the coordinator. Raw XML is not stored. The coordinator caps stored file lists, failed-case entries, and long strings so a huge report cannot exceed Durable Object storage or response limits.

Use:

```sh
crabbox history --lease cbx_...
crabbox results run_...
```

Current format support:

- JUnit XML.

Future useful additions:

- Vitest JSON;
- Go `test2json`;
- flaky history across runs;
- changed-file correlation.
