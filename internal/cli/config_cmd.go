package cli

import (
	"fmt"
)

func (a App) configShow(args []string) error {
	fs := newFlagSet("config show", a.Stderr)
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "provider=%s target=%s image=%s cli=%s user=%s work_root=%s\n",
		cfg.Provider,
		cfg.TargetOS,
		cfg.AppleContainer.Image,
		cfg.AppleContainer.CLIPath,
		cfg.AppleContainer.User,
		cfg.AppleContainer.WorkRoot,
	)
	return nil
}

func (a App) configSetBroker(args []string) error {
	return fmt.Errorf("broker config not available in ciderbox")
}

func Blank(value, fallback string) string {
	return blank(value, fallback)
}

func blank(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func configShowView(cfg Config) map[string]any {
	return map[string]any{
		"provider": cfg.Provider,
		"target":   cfg.TargetOS,
	}
}