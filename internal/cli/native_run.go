package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type nativeRunOptions struct {
	Provider     string
	Image        string
	CPUs         int
	Memory       string
	User         string
	WorkRoot     string
	Keep         bool
	NoSync       bool
	ConfigFile   string
	Dependencies []string
	Labels       map[string]string
	Env          map[string]string
	Command      []string
}

func (a App) runCommand(ctx context.Context, args []string) error {
	fs := newFlagSet("run", a.Stderr)

	provider := fs.String("provider", "apple-container", "provider to use")
	image := fs.String("apple-container-image", "", "Apple container image")
	cpus := fs.Int("apple-container-cpus", 0, "CPU limit; 0 leaves runtime default")
	memory := fs.String("apple-container-memory", "", "memory limit, e.g. 4G")
	user := fs.String("apple-container-user", "", "container user; defaults to root for native exec")
	workRoot := fs.String("apple-container-work-root", "", "container workspace root")
	keep := fs.Bool("keep", false, "keep container after command exits")
	noSync := fs.Bool("no-sync", false, "do not copy current directory into container")
	configFile := fs.String("config", ".ciderbox.yaml", "path to ciderbox config")

	var labelFlags stringListFlag
	var envFlags stringListFlag
	var depFlags stringListFlag
	fs.Var(&labelFlags, "label", "extra container label key=value; repeatable")
	fs.Var(&envFlags, "env", "environment variable key=value for command; repeatable")
	fs.Var(&depFlags, "dependency", "system package to install before command; repeatable")

	if err := parseFlags(fs, args); err != nil {
		return err
	}

	command := fs.Args()
	if len(command) > 0 && command[0] == "--" {
		command = command[1:]
	}
	if len(command) == 0 {
		return exit(2, "run requires a command after --, e.g. ciderbox run -- go test ./...")
	}

	projectCfg, _ := readCiderboxConfig(*configFile)

	if projectCfg != nil {
		if projectCfg.Run.Provider != "" && *provider == "apple-container" {
			*provider = projectCfg.Run.Provider
		}
		if projectCfg.Run.Image != "" && *image == "" {
			*image = projectCfg.Run.Image
		}
	}

	if *provider == "" {
		*provider = "apple-container"
	}
	if *provider != "apple-container" {
		return exit(2, "native run currently supports provider=apple-container only; got %q", *provider)
	}

	labels, err := parseKeyValueFlags(labelFlags, "label")
	if err != nil {
		return err
	}
	env, err := parseKeyValueFlags(envFlags, "env")
	if err != nil {
		return err
	}

	dependencies := append([]string(nil), depFlags...)

	opts := nativeRunOptions{
		Provider:     *provider,
		Image:        *image,
		CPUs:         *cpus,
		Memory:       *memory,
		User:         *user,
		WorkRoot:     *workRoot,
		Keep:         *keep,
		NoSync:       *noSync,
		ConfigFile:   *configFile,
		Dependencies: dependencies,
		Labels:       labels,
		Env:          env,
		Command:      command,
	}

	return a.runNativeContainerCommand(ctx, opts)
}

func (a App) runNativeContainerCommand(ctx context.Context, opts nativeRunOptions) error {
	cfg, rt, err := a.providerConfigRuntime(opts.Provider)
	if err != nil {
		return exit(2, "run: config: %v", err)
	}

	if opts.Image != "" {
		cfg.AppleContainer.Image = opts.Image
	}
	if cfg.AppleContainer.Image == "" {
		cfg.AppleContainer.Image = "ubuntu:26.04"
	}
	if opts.CPUs > 0 {
		cfg.AppleContainer.CPUs = opts.CPUs
	}
	if opts.Memory != "" {
		cfg.AppleContainer.Memory = opts.Memory
	}
	if opts.WorkRoot != "" {
		cfg.AppleContainer.WorkRoot = opts.WorkRoot
	}
	if cfg.AppleContainer.WorkRoot == "" || cfg.AppleContainer.WorkRoot == "/work/crabbox" {
		cfg.AppleContainer.WorkRoot = "/work/ciderbox"
	}

	// The old SSH path used a created "crabbox" user. Native container exec
	// should default to root because stock Ubuntu/Debian/Alpine images do not
	// contain a crabbox user.
	if opts.User != "" {
		cfg.AppleContainer.User = opts.User
	} else {
		cfg.AppleContainer.User = "root"
	}

	containerRuntime, err := a.nativeContainerRuntime(opts.Provider, cfg, rt)
	if err != nil {
		return exit(2, "run: runtime: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return exit(2, "run: get working directory: %v", err)
	}
	projectName := filepath.Base(cwd)
	if projectName == "." || projectName == string(filepath.Separator) || projectName == "" {
		projectName = "workspace"
	}

	now := time.Now().UTC()
	runID := fmt.Sprintf("run-%d", now.UnixNano())
	containerName := sanitizeContainerName("ciderbox-" + projectName + "-" + runID)
	workRoot := strings.TrimRight(cfg.AppleContainer.WorkRoot, "/")
	workDir := workRoot + "/" + projectName

	labels := map[string]string{
		"ciderbox":      "true",
		"provider":      opts.Provider,
		"lease":         runID,
		"ciderbox.kind": "run",
		"ciderbox.run":  runID,
		"project":       projectName,
		"created_at":    now.Format(time.RFC3339),
		"image":         cfg.AppleContainer.Image,
		"work_root":     workRoot,
		"work_dir":      workDir,
	}
	for k, v := range opts.Labels {
		labels[k] = v
	}
	if opts.Keep {
		labels[ciderboxProtectedLabel] = "true"
	}

	spec := ContainerSpec{
		Name:      containerName,
		Image:     cfg.AppleContainer.Image,
		CPUs:      cfg.AppleContainer.CPUs,
		Memory:    cfg.AppleContainer.Memory,
		User:      cfg.AppleContainer.User,
		Labels:    labels,
		ExtraArgs: append([]string(nil), cfg.AppleContainer.ExtraRunArgs...),
		Command:   []string{"sleep", "infinity"},
	}

	fmt.Fprintf(a.Stdout, "=== Ciderbox Run ===\n")
	fmt.Fprintf(a.Stdout, "Image: %s\n", spec.Image)
	fmt.Fprintf(a.Stdout, "Project: %s\n", projectName)
	fmt.Fprintf(a.Stdout, "Container: %s\n", containerName)
	fmt.Fprintf(a.Stdout, "Keep: %v\n\n", opts.Keep)

	info, err := containerRuntime.Run(ctx, spec)
	if err != nil {
		return exit(1, "run: create container: %v", err)
	}

	remove := !opts.Keep
	defer func() {
		if remove {
			fmt.Fprintf(a.Stdout, "\nCleaning up container %s...\n", info.ID)
			if err := containerRuntime.Remove(context.Background(), info.ID, true); err != nil {
				fmt.Fprintf(a.Stderr, "WARN: cleanup failed for %s: %v\n", info.ID, err)
			}
		}
	}()

	if err := containerRuntime.Exec(ctx, info.ID, []string{"/bin/sh", "-lc", "mkdir -p " + shQuoteNative(workRoot)}, a.Stdout, a.Stderr); err != nil {
		return exit(1, "run: prepare work root: %v", err)
	}

	if !opts.NoSync {
		fmt.Fprintf(a.Stdout, "Copying %s -> %s:%s ...\n", cwd, info.ID, workRoot)
		if err := containerRuntime.Copy(ctx, cwd, info.ID+":"+workRoot); err != nil {
			return exit(1, "run: copy workspace: %v", err)
		}
	} else {
		fmt.Fprintln(a.Stdout, "Skipping workspace copy (--no-sync).")
	}

	script := buildNativeRunScript(workDir, opts.Dependencies, opts.Env, opts.Command)
	fmt.Fprintf(a.Stdout, "\nRunning command in %s...\n\n", workDir)

	if err := containerRuntime.Exec(ctx, info.ID, []string{"/bin/sh", "-lc", script}, a.Stdout, a.Stderr); err != nil {
		if opts.Keep {
			fmt.Fprintf(a.Stdout, "\nContainer kept for inspection: %s\n", info.ID)
		}
		return exit(1, "run: command failed: %v", err)
	}

	if opts.Keep {
		fmt.Fprintf(a.Stdout, "\nContainer kept: %s\n", info.ID)
	}

	return nil
}

func buildNativeRunScript(workDir string, dependencies []string, env map[string]string, command []string) string {
	parts := []string{"set -e"}

	if len(env) > 0 {
		keys := sortedMapKeys(env)
		for _, key := range keys {
			parts = append(parts, "export "+key+"="+shQuoteNative(env[key]))
		}
	}

	if len(dependencies) > 0 {
		parts = append(parts, depInstallSnippet(dependencies))
	}

	parts = append(parts, "cd "+shQuoteNative(workDir))
	parts = append(parts, shellJoinNative(command))

	return strings.Join(parts, "\n")
}

func parseKeyValueFlags(values []string, name string) (map[string]string, error) {
	out := map[string]string{}
	for _, value := range values {
		key, val, ok := strings.Cut(value, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, exit(2, "--%s must be key=value, got %q", name, value)
		}
		if name == "env" && !validShellEnvName(key) {
			return nil, exit(2, "--env has invalid variable name %q", key)
		}
		out[key] = val
	}
	return out, nil
}

func sortedMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

var shellEnvNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validShellEnvName(value string) bool {
	return shellEnvNameRE.MatchString(value)
}

func shellJoinNative(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shQuoteNative(arg))
	}
	return strings.Join(quoted, " ")
}

func shQuoteNative(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func sanitizeContainerName(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "ciderbox-run"
	}
	if len(out) > 63 {
		out = strings.Trim(out[:63], "-")
	}
	return out
}


func (a App) compileTest(ctx context.Context, args []string) error {
	return exit(2, "compile-test: not implemented yet")
}

func (a App) buildCommand(ctx context.Context, args []string) error {
	return exit(2, "build: not implemented yet")
}

func (a App) chopCommand(ctx context.Context, args []string) error {
	return exit(2, "chop: not implemented yet")
}


const ciderboxProtectedLabel = "ciderbox-protected"

func readCiderboxConfig(path string) (*Config, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func depInstallSnippet(dependencies []string) string {
	if len(dependencies) == 0 {
		return ""
	}
	return "apt-get update && apt-get install -y --no-install-recommends " + strings.Join(dependencies, " ")
}
