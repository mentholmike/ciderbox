package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
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

	// Direct-dispatch commands that don't go through kong
	switch args[0] {
	case "orchard":
		return a.orchardCommand(ctx, args[1:])
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
	case "config":
		return nil, false // handled by kong
	case "orchard":
		return a.orchardCommand(ctx, helpArgs), true
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
  ciderbox doctor
      Check local tools and provider readiness.
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

Global:
  -h, --help     Show help
  --version      Print version
`)
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

// providerConfigRuntime loads the ciderbox user config and returns a Runtime
// wired to the app's stdout/stderr streams and the host os/exec runner.
func (a App) providerConfigRuntime(providerName string) (Config, Runtime, error) {
	cfg, err := loadConfig()
	if err != nil {
		return Config{}, Runtime{}, err
	}
	cfg.Provider = providerName
	rt := Runtime{
		Stdout: a.Stdout,
		Stderr: a.Stderr,
		Clock:  realClock{},
		HTTP:   http.DefaultClient,
		Exec:   osCommandRunner{},
	}
	return cfg, rt, nil
}

// osCommandRunner wraps os/exec to satisfy the CommandRunner interface.
type osCommandRunner struct{}

func (osCommandRunner) Run(ctx context.Context, req LocalCommandRequest) (LocalCommandResult, error) {
	cmd := exec.CommandContext(ctx, req.Name, req.Args...)
	cmd.Dir = req.Dir
	cmd.Env = req.Env
	if req.Stdin != nil {
		cmd.Stdin = req.Stdin
	}
	if req.Stdout != nil {
		cmd.Stdout = req.Stdout
	}
	if req.Stderr != nil {
		cmd.Stderr = req.Stderr
	}
	var stdoutBuf, stderrBuf strings.Builder
	if req.Stdout == nil {
		cmd.Stdout = &stdoutBuf
	}
	if req.Stderr == nil {
		cmd.Stderr = &stderrBuf
	}
	err := cmd.Run()
	var exitCode int
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	return LocalCommandResult{
		ExitCode: exitCode,
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
	}, err
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
