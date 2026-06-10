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

	switch args[0] {
	case "run":
		return a.runCommand(ctx, args[1:])
	case "compile-test":
		return a.compileTest(ctx, args[1:])
	case "build":
		return a.buildCommand(ctx, args[1:])
	case "chop":
		return a.chopCommand(ctx, args[1:])
	case "doctor":
		return a.doctor(ctx, args[1:])
	case "list":
		return a.list(ctx, args[1:])
	case "cleanup":
		return a.cleanup(ctx, args[1:])
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
		return nil, false
	case "run":
		return a.runCommand(ctx, helpArgs), true
	case "compile-test":
		return a.compileTest(ctx, helpArgs), true
	case "build":
		return a.buildCommand(ctx, helpArgs), true
	case "chop":
		return a.chopCommand(ctx, helpArgs), true
	case "doctor":
		return a.doctor(ctx, helpArgs), true
	case "list":
		return a.list(ctx, helpArgs), true
	case "cleanup":
		return a.cleanup(ctx, helpArgs), true
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

Core:
  doctor             Check local tools and provider readiness
  init               Scaffold .ciderbox.yaml in the current repo
  run -- <cmd>       Run a command in a fresh Apple Container
  compile-test       Run test command across configured distros
  build              Build project inside an Apple Container
  chop               Remove active ciderbox containers
  version            Print version

Orchid:
  orchard init       Scaffold .orchard.yaml
  orchard plant      Spin up an AI-agent swarm
  orchard tend       Show live tree health
  orchard graft      Install OpenClaw on a tree
  orchard run        Execute a task across trees
  orchard harvest    Collect tree results
  orchard chop       Tear down the swarm

Examples:
  ciderbox init
  ciderbox run -- go test ./...
  ciderbox compile-test
  ciderbox build
  ciderbox chop --yes
  ciderbox orchard plant

Environment:
  CIDERBOX_CONFIG    Config file path override

Global:
  -h, --help         Show help
  --version          Print version`)
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func (a App) providerConfigRuntime(providerName string) (Config, Runtime, error) {
	cfg, err := loadConfig()
	if err != nil {
		return Config{}, Runtime{}, err
	}
	cfg.Provider = providerName
	canonicalizeConfigProvider(&cfg)
	rt := Runtime{
		Stdout: a.Stdout,
		Stderr: a.Stderr,
		Clock:  realClock{},
		HTTP:   http.DefaultClient,
		Exec:   osCommandRunner{},
	}
	return cfg, rt, nil
}

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
	return LocalCommandResult{ExitCode: exitCode, Stdout: stdoutBuf.String(), Stderr: stderrBuf.String()}, err
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