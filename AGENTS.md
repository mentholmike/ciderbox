# Repository Guidelines

## Project Structure & Module Organization

Ciderbox is a Go CLI for Apple-native container dev environments. The CLI entrypoint is `cmd/ciderbox`, with implementation and Go tests in `internal/cli`. Documentation lives in `docs/`; command docs are under `docs/commands`, and feature notes under `docs/features`. Release configuration is in `.goreleaser.yaml`; GitHub Actions live in `.github/workflows`. Generated outputs such as `bin/`, `dist/` should not be edited by hand.

## Product Positioning

Ciderbox is an Apple-native dev/test container tool. New code, docs, tests, and examples should not mention OpenClaw, Peter, or other project/person-specific workflows unless the file is explicitly about legacy compatibility or release history. Prefer neutral examples such as `example-org`, `alice@example.com`, `my-app`, and generic repository workflows.

## Architecture Boundaries

Keep the apple-container provider as the sole provider. Core logic handles container lifecycle (run, exec, rm, cp) through the Apple `container` CLI. No multi-provider abstraction needed — this is intentionally a single-purpose tool.

## Build, Test, and Development Commands

- `go build -trimpath -o bin/ciderbox ./cmd/ciderbox`: build the local CLI.
- `go vet ./...`: run Go static checks.
- `go test -race ./...`: run the Go test suite with the race detector.
- `gofmt -w $(git ls-files '*.go')`: format Go files.

## Coding Style & Naming Conventions

Use standard Go formatting and keep package names short and lowercase. Prefer table-driven Go tests where behavior has multiple cases, and keep command behavior close to the matching file in `internal/cli`.

## Testing Guidelines

Name Go tests `*_test.go` beside the code they cover. Add regression tests for bug fixes when practical. Before handoff, run the relevant subset; before release or broad changes, run the full CI-equivalent gate.

## Commit & Pull Request Guidelines

History uses Conventional Commit prefixes such as `feat:`, `fix:`, `docs:`, and `ci:`. Keep commits focused and mention user-visible behavior changes. Pull requests should include a clear summary, verification commands, and config implications.

## Security & Configuration Tips

Keep API tokens out of the repository. Do not pass secrets as command-line arguments. Local config belongs in `~/.config/ciderbox/config.yaml`, `~/Library/Application Support/ciderbox/config.yaml`, `ciderbox.yaml`, or `.ciderbox.yaml` as documented.
