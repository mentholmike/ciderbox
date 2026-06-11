package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/alecthomas/kong"
)

type ciderboxKongCLI struct {
	Version kong.VersionFlag `name:"version" short:"v" help:"Print version."`

	VersionCmd versionKongCmd `cmd:"" name:"version" help:"Print version."`
	Init       initKongCmd    `cmd:"" passthrough:"" help:"Onboard the current repo for Ciderbox."`
	Config     configKongCmd  `cmd:"" help:"Show or update user config."`
}

type kongExit struct {
	code int
}

func (a App) runKong(ctx context.Context, args []string) (err error) {
	args = normalizeKongHelpArgs(args)
	var cli ciderboxKongCLI
	parser, err := kong.New(&cli,
		kong.Name("ciderbox"),
		kong.Description("Ciderbox \u2014 Apple-container dev/test runner and local OpenClaw swarm launcher."),
		kong.Vars{"version": currentVersion()},
		kong.Writers(a.Stdout, a.Stderr),
		kong.Exit(func(code int) {
			panic(kongExit{code: code})
		}),
	)
	if err != nil {
		return err
	}
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		if exit, ok := recovered.(kongExit); ok {
			if exit.code == 0 {
				err = nil
			} else {
				err = ExitError{Code: exit.code}
			}
			return
		}
		panic(recovered)
	}()
	kctx, err := parser.Parse(args)
	if err != nil {
		var parseErr *kong.ParseError
		if errors.As(err, &parseErr) {
			return exit(2, "%v", parseErr)
		}
		return err
	}
	kctx.BindTo(ctx, (*context.Context)(nil))
	return kctx.Run(a)
}

func normalizeKongHelpArgs(args []string) []string {
	if len(args) > 1 && args[0] == "help" {
		next := append([]string{}, args[1:]...)
		next = append(next, "--help")
		return next
	}
	if isKongCommandGroup(args[0]) && (len(args) == 1 || args[1] == "help") {
		return []string{args[0], "--help"}
	}
	return args
}

func isKongCommandGroup(command string) bool {
	return command == "config"
}

type initKongCmd struct {
	Args []string `arg:"" optional:""`
}

type configKongCmd struct {
	Path      configPathKongCmd      `cmd:"" help:"Print the user config path."`
	Show      configShowKongCmd      `cmd:"" passthrough:"" help:"Print merged config without secret values."`
	SetBroker configSetBrokerKongCmd `cmd:"" name:"set-broker" passthrough:"" help:"Store broker URL and optional tokens in user config."`
}

type configPathKongCmd struct{}
type configShowKongCmd struct {
	Args []string `arg:"" optional:""`
}
type configSetBrokerKongCmd struct {
	Args []string `arg:"" optional:""`
}

type versionKongCmd struct{}

func (c *initKongCmd) Run(ctx context.Context, app App) error { return app.initProject(ctx, c.Args) }

func (c *configPathKongCmd) Run(ctx context.Context, app App) error {
	path := userConfigPath()
	if path == "" {
		return exit(2, "user config directory is unavailable")
	}
	fmt.Fprintln(app.Stdout, path)
	return nil
}
func (c *configShowKongCmd) Run(app App) error {
	return app.configShow(c.Args)
}
func (c *configSetBrokerKongCmd) Run(app App) error {
	return app.configSetBroker(c.Args)
}

func (c *versionKongCmd) Run(app App) error {
	fmt.Fprintln(app.Stdout, currentVersion())
	return nil
}
