package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

type App struct {
	Stdout io.Writer
	Stderr io.Writer
	Stdin  io.Reader
}

func Run(ctx context.Context, args []string) error {
	app := App{Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin}
	return app.Run(ctx, args)
}

func (a App) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		a.printHelp()
		return exit(2, "missing command")
	}

	switch args[0] {
	case "-h", "--help":
		a.printHelp()
		return nil
	case "help":
		if len(args) > 1 {
			return a.runKong(ctx, args)
		}
		a.printHelp()
		return nil
	}
	if help, ok := a.directCommandHelp(ctx, args); ok {
		return help
	}

	return a.runKong(ctx, args)
}

func (a App) directCommandHelp(ctx context.Context, args []string) (error, bool) {
	if len(args) < 2 || !isHelpArg(args[1]) || isKongCommandGroup(args[0]) {
		return nil, false
	}
	helpArgs := []string{"--help"}
	switch args[0] {
	case "init":
		return a.initProject(ctx, helpArgs), true
	case "login":
		return a.login(ctx, helpArgs), true
	case "logout":
		return a.logout(ctx, helpArgs), true
	case "whoami":
		return a.whoami(ctx, helpArgs), true
	case "doctor":
		return a.doctor(ctx, helpArgs), true
	case "warmup":
		return a.warmup(ctx, helpArgs), true
	case "prewarm":
		return a.prewarm(ctx, helpArgs), true
	case "compile-test":
		return a.compileTest(ctx, helpArgs), true
	case "chop":
		return a.chopCommand(ctx, helpArgs), true
	case "build":
		return a.buildCommand(ctx, helpArgs), true
	case "orchard":
		return a.orchardCommand(ctx, helpArgs), true
	case "run":
		return a.runCommand(ctx, helpArgs), true
	case "job":
		return nil, false
	case "sync-plan":
		return a.syncPlan(ctx, helpArgs), true
	case "providers":
		return a.providers(ctx, helpArgs), true
	case "history":
		return a.history(ctx, helpArgs), true
	case "logs":
		return a.logs(ctx, helpArgs), true
	case "events":
		return a.events(ctx, helpArgs), true
	case "attach":
		return a.attach(ctx, helpArgs), true
	case "results":
		return a.results(ctx, helpArgs), true
	case "status":
		return a.status(ctx, helpArgs), true
	case "list":
		return a.list(ctx, helpArgs), true
	case "usage":
		return a.usage(ctx, helpArgs), true
	case "ssh":
		return a.ssh(ctx, helpArgs), true
	case "ports":
		return a.ports(ctx, helpArgs), true
	case "cp":
		return a.copyCommand(ctx, helpArgs), true
	case "vnc":
		return a.vnc(ctx, helpArgs), true
	case "webvnc":
		return a.webvnc(ctx, helpArgs), true
	case "code":
		return a.webCode(ctx, helpArgs), true
	case "egress":
		return a.egress(ctx, helpArgs), true
	case "screenshot":
		return a.screenshot(ctx, helpArgs), true
	case "artifacts":
		return nil, false
	case "capsule":
		return nil, false
	case "checkpoint":
		return nil, false
	case "inspect":
		return a.inspect(ctx, helpArgs), true
	case "stop", "release":
		return a.stop(ctx, helpArgs), true
	case "cleanup":
		return a.cleanup(ctx, helpArgs), true
	default:
		return nil, false
	}
}

func isHelpArg(arg string) bool {
	return arg == "-h" || arg == "--help" || arg == "help"
}

func (a App) printHelp() {
	fmt.Fprintln(a.Stdout, `Ciderbox — Apple-container dev/test runner and local OpenClaw swarm launcher.

Usage:
  ciderbox <command> [flags]
  ciderbox run [flags] -- <command...>

Start Here:
  ciderbox init
      Scaffold .ciderbox.yaml in the current repo.
  ciderbox compile-test
      Run your test command across multiple Apple-container distros.
  ciderbox build
      Build the project inside an Apple-container VM.
  ciderbox chop
      Terminate all active ciderbox leases.
  ciderbox orchard plant
      Spin up an AI-agent swarm on local Apple-container VMs.

Commands:
  init          Scaffold repo .ciderbox.yaml config
  compile-test  Run tests across multiple distros in parallel
  build         Build project in an Apple-container VM
  chop          Terminate active leases (respects ciderbox-protected)
  run           Sync repo and run a command in a container
  warmup        Lease a box and wait until ready
  ssh           Print SSH command for a lease
  cp            Copy files to/from a lease
  status        Show lease state; --wait blocks until ready
  list          List active ciderbox machines
  stop          Release a lease
  cleanup       Sweep expired direct-provider machines
  inspect       Print lease/provider details (--json for scripts)
  doctor        Check local tools and provider readiness
  config        Show or update user config
  orchard       Manage an AI-agent swarm on Apple-container VMs

Common Flows:
  ciderbox init
  ciderbox compile-test
  ciderbox chop

  ciderbox run -- go test ./...
  ciderbox warmup
  ciderbox ssh --id blue-lobster
  ciderbox cp --id blue-lobster ./out SANDBOX:/tmp/out
  ciderbox status --id blue-lobster --wait
  ciderbox stop blue-lobster

  ciderbox orchard init
  ciderbox orchard plant
  ciderbox orchard tend
  ciderbox orchard graft --tree tree-0
  ciderbox orchard harvest --output results.json
  ciderbox orchard chop

Environment:
  CIDERBOX_CONFIG              Config file path override
  CRABBOX_CONFIG               Legacy config file path override
  CRABBOX_PROVIDER             Provider override (apple-container, ssh, ...)
  CRABBOX_IDLE_TIMEOUT         Default idle expiry, e.g. 30m
  CRABBOX_TTL                  Maximum lease lifetime, e.g. 90m
  CRABBOX_SSH_FALLBACK_PORTS   Comma-separated SSH fallback ports, or none

Global:
  -h, --help     Show help
  --version      Print version

Docs:
  docs/commands/README.md`)
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitError{Code: 0}
		}
		return exit(2, "%v", err)
	}
	return nil
}
