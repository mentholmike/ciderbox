package cli

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SecretsState holds loaded secret values (never serialized to disk).
type SecretsState struct {
	EnvFile string
	Secrets map[string]string
	Sources map[string]string // key -> source description (".orchid.env" or "host env")
}

// loadSecrets loads .orchid.env and merges pass-through host env vars.
func loadSecrets(cfg *OrchardConfig) (*SecretsState, error) {
	state := &SecretsState{
		EnvFile: blank(cfg.Secrets.EnvFile, ".orchid.env"),
		Secrets: make(map[string]string),
		Sources: make(map[string]string),
	}

	// 1. Load env file if present
	envPath := state.EnvFile
	if data, err := os.ReadFile(envPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			if key == "" {
				continue
			}
			state.Secrets[key] = val
			state.Sources[key] = state.EnvFile
		}
	}

	// 2. Merge allowed host env vars (higher priority)
	hostKeys := append([]string{}, cfg.Secrets.PassThrough...)
	for _, key := range cfg.Secrets.Required {
		if !contains(hostKeys, key) {
			hostKeys = append(hostKeys, key)
		}
	}
	for _, key := range hostKeys {
		if val, ok := os.LookupEnv(key); ok {
			state.Secrets[key] = val
			state.Sources[key] = "host env"
		}
	}

	return state, nil
}

// validateRequired checks that required secrets are non-empty.
// Returns only names, never values.
func (s *SecretsState) validateRequired(required []string) []string {
	var missing []string
	for _, key := range required {
		if val, ok := s.Secrets[key]; !ok || val == "" {
			missing = append(missing, key)
		}
	}
	return missing
}

// redactValue returns "***" for any key that looks like a secret.
func redactValue(key string) string {
	return "***"
}

// envContent generates the .env file content from secrets (pass-through + required).
func (s *SecretsState) envContent(cfg *OrchardConfig) string {
	seen := make(map[string]bool)
	var keys []string
	for _, k := range cfg.Secrets.PassThrough {
		if !seen[k] {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	for _, k := range cfg.Secrets.Required {
		if !seen[k] {
			keys = append(keys, k)
			seen[k] = true
		}
	}

	var b strings.Builder
	for _, k := range keys {
		v := s.Secrets[k]
		if v != "" {
			b.WriteString(fmt.Sprintf("%s=%s\n", k, v))
		}
	}
	return b.String()
}

// generateOpenClawJSON builds the content for /root/.openclaw/openclaw.json.
func generateOpenClawJSON(cfg *OrchardConfig, treeID string, workspacePath string) string {
	modelProvider, modelName := parseModel(cfg.Agent.Model)
	modelID := modelProvider + "/" + modelName
	workspace := blank(workspacePath, "/work/ciderbox")

	return fmt.Sprintf(`{
	 "agents": {
	  "defaults": {
	   "model": {
	    "primary": %q
	   },
	   "workspace": %q,
	   "repoRoot": %q
	  }
	 },
	 "tools": {
	  "profile": "coding",
	  "fs": {
	   "workspaceOnly": true
	  },
	  "exec": {
	   "mode": "ask"
	  },
	  "web": {
	   "fetch": {
	    "enabled": true
	   },
	   "search": {
	    "enabled": false
	   }
	  }
	 },
	 "approvals": {
	  "exec": {
	   "enabled": false
	  }
	 }
	}
	`,
		modelID,
		workspace,
		workspace,
	)
}

// parseModel splits "provider/model" into ("provider", "model").
func parseModel(model string) (string, string) {
	if model == "" || model == "CHANGE_ME" {
		return "openrouter", "anthropic/claude-sonnet-4.5"
	}
	parts := strings.SplitN(model, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "openrouter", parts[0]
}

// orchardSecretsCommand routes secrets subcommands.
func (a App) orchardSecretsCommand(ctx context.Context, args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(a.Stdout, `Usage: ciderbox orchard secrets <subcommand>

Subcommands:
  init       Create .orchid.env and update .gitignore
  check      Validate required secrets (never prints values)
  push       Push secrets into running trees

Examples:
  ciderbox orchard secrets init
  ciderbox orchard secrets check
  ciderbox orchard secrets push --all`)
		return nil
	}

	switch args[0] {
	case "init":
		return a.orchardSecretsInit(ctx, args[1:])
	case "check":
		return a.orchardSecretsCheck(ctx, args[1:])
	case "push":
		return a.orchardSecretsPush(ctx, args[1:])
	default:
		return exit(2, "unknown secrets subcommand: %q (try init, check, push)", args[0])
	}
}

// orchardSecretsInit scaffolds .orchid.env and updates .gitignore.
func (a App) orchardSecretsInit(ctx context.Context, args []string) error {
	fs := newFlagSet("orchard secrets init", a.Stderr)
	configFile := fs.String("config", orchardDefaultFile, "path to orchard manifest")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	cfg, err := readOrchardConfig(*configFile)
	if err != nil {
		return exit(2, "orchard config: %v", err)
	}

	envPath := blank(cfg.Secrets.EnvFile, ".orchid.env")

	if _, err := os.Stat(envPath); err == nil {
		return exit(2, "%s already exists", envPath)
	}

	var b strings.Builder
	b.WriteString("# .orchid.env\n")
	b.WriteString("# Local Orchid secrets. Do not commit.\n")
	b.WriteString("# Generated by `ciderbox orchard secrets init`\n\n")

	seen := make(map[string]bool)
	for _, k := range cfg.Secrets.PassThrough {
		if !seen[k] {
			b.WriteString(fmt.Sprintf("%s=\n", k))
			seen[k] = true
		}
	}
	for _, k := range cfg.Secrets.Required {
		if !seen[k] {
			b.WriteString(fmt.Sprintf("%s=\n", k))
			seen[k] = true
		}
	}
	if len(cfg.Secrets.Required) == 0 && len(cfg.Secrets.PassThrough) == 0 {
		b.WriteString("# No secrets configured in .orchard.yaml\n")
		b.WriteString("# Add a 'secrets' section to configure keys:\n")
		b.WriteString("#   OPENROUTER_API_KEY=\n")
	}

	if err := os.WriteFile(envPath, []byte(b.String()), 0600); err != nil {
		return exit(2, "write %s: %v", envPath, err)
	}

	fmt.Fprintf(a.Stdout, "Created %s (chmod 600)\n", envPath)

	gPath := ".gitignore"
	gData, _ := os.ReadFile(gPath)
	gContent := string(gData)
	changed := false
	existingIgnores := make(map[string]bool)
	for _, line := range strings.Split(gContent, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line != "" {
			existingIgnores[line] = true
		}
	}
	ignoreEnvPath := filepath.Clean(envPath)
	if filepath.IsAbs(ignoreEnvPath) {
		if cwd, err := os.Getwd(); err == nil {
			if rel, err := filepath.Rel(cwd, ignoreEnvPath); err == nil && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".." {
				ignoreEnvPath = rel
			}
		}
	}
	patterns := []string{ignoreEnvPath, ".orchid.env", ".openclaw.env"}
	for _, p := range patterns {
		if filepath.IsAbs(p) {
			continue
		}
		if p != "" && !existingIgnores[p] {
			if gContent != "" && !strings.HasSuffix(gContent, "\n") {
				gContent += "\n"
			}
			gContent += p + "\n"
			existingIgnores[p] = true
			changed = true
		}
	}
	if changed {
		if err := os.WriteFile(gPath, []byte(gContent), 0644); err != nil {
			return exit(2, "write %s: %v", gPath, err)
		}
		fmt.Fprintln(a.Stdout, "Updated .gitignore")
	}

	fmt.Fprintln(a.Stdout)
	fmt.Fprintln(a.Stdout, "Next: fill in your API keys in .orchid.env or export them as env vars.")
	return nil
}

// orchardSecretsCheck validates required secrets without printing values.
func (a App) orchardSecretsCheck(ctx context.Context, args []string) error {
	fs := newFlagSet("orchard secrets check", a.Stderr)
	configFile := fs.String("config", orchardDefaultFile, "path to orchard manifest")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	cfg, err := readOrchardConfig(*configFile)
	if err != nil {
		return exit(2, "orchard config: %v", err)
	}

	state, err := loadSecrets(cfg)
	if err != nil {
		return exit(2, "load secrets: %v", err)
	}

	missing := state.validateRequired(cfg.Secrets.Required)

	fmt.Fprintln(a.Stdout, "Secrets:")

	for _, key := range cfg.Secrets.PassThrough {
		val := state.Secrets[key]
		source := state.Sources[key]
		if val != "" {
			fmt.Fprintf(a.Stdout, "  ✓ %s (from %s)\n", key, source)
		} else {
			fmt.Fprintf(a.Stdout, "  ! %s missing (pass-through)\n", key)
		}
	}

	for _, key := range cfg.Secrets.Required {
		if contains(cfg.Secrets.PassThrough, key) {
			continue
		}
		val := state.Secrets[key]
		source := state.Sources[key]
		if val != "" {
			fmt.Fprintf(a.Stdout, "  ✓ %s (from %s)\n", key, source)
		} else {
			fmt.Fprintf(a.Stdout, "  ! %s missing (required)\n", key)
		}
	}

	if len(missing) > 0 {
		fmt.Fprintf(a.Stdout, "\nMissing required: %s\n", strings.Join(missing, ", "))
		return exit(1, "required secrets missing")
	}

	fmt.Fprintln(a.Stdout, "\nAll required secrets present.")
	return nil
}

// orchardSecretsPush pushes sanitized env into running trees.
func (a App) orchardSecretsPush(ctx context.Context, args []string) error {
	fs := newFlagSet("orchard secrets push", a.Stderr)
	configFile := fs.String("config", orchardDefaultFile, "path to orchard manifest")
	treeID := fs.String("tree", "", "tree ID to push secrets to; defaults to all trees")
	pushAll := fs.Bool("all", false, "push secrets to all trees")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *pushAll && *treeID != "" {
		return exit(2, "orchard secrets push: specify --tree or --all, not both")
	}

	cfg, err := readOrchardConfig(*configFile)
	if err != nil {
		return exit(2, "orchard config: %v", err)
	}

	state, err := loadSecrets(cfg)
	if err != nil {
		return exit(2, "load secrets: %v", err)
	}

	missing := state.validateRequired(cfg.Secrets.Required)
	if len(missing) > 0 {
		return exit(2, "required secrets missing: %s (run 'orchard secrets check')", strings.Join(missing, ", "))
	}

	trees, runtimeConfig, _, err := a.orchardContainers(ctx, cfg.Name)
	if err != nil {
		return exit(2, "orchard push: %v", err)
	}
	containerCLI := blank(runtimeConfig.AppleContainer.CLIPath, "container")

	if *treeID != "" {
		filtered := trees[:0]
		for _, tree := range trees {
			if tree.Labels["tree.id"] == *treeID || tree.Name == *treeID || strings.Contains(tree.Name, *treeID) {
				filtered = append(filtered, tree)
			}
		}
		trees = filtered
	}

	if len(trees) == 0 {
		return exit(2, "no trees running")
	}

	envContent := state.envContent(cfg)

	fmt.Fprintf(a.Stdout, "Pushing secrets to %d tree(s)...\n", len(trees))
	success, failed := 0, 0
	for _, tree := range trees {
		name := blank(tree.Labels["tree.id"], tree.Name)
		fmt.Fprintf(a.Stdout, "  [%s] writing /root/.openclaw/.env...\n", name)

		if err := a.writeTreeFile(ctx, containerCLI, tree.ID, "/root/.openclaw/.env", envContent, "600"); err != nil {
			fmt.Fprintf(a.Stderr, "  [%s] failed: %v\n", name, err)
			failed++
			continue
		}
		success++
	}

	fmt.Fprintf(a.Stdout, "\npushed=%d failed=%d\n", success, failed)
	if failed > 0 {
		return exit(1, "%d tree(s) failed", failed)
	}
	return nil
}

// orchardLogin provides provider auth helpers.
func (a App) orchardLogin(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintln(a.Stdout, `Usage: ciderbox orchard login <provider>

Providers:
  openclaw     Login to OpenClaw (no separate login needed — env vars suffice)
  openrouter   Set OPENROUTER_API_KEY in .orchid.env or host env
  anthropic    Set ANTHROPIC_API_KEY in .orchid.env or host env
  openai       Set OPENAI_API_KEY in .orchid.env or host env

For API-key providers: fill key in .orchid.env or export it.
Then run: ciderbox orchard secrets push --all`)
		return nil
	}

	switch args[0] {
	case "openclaw":
		fmt.Fprintln(a.Stdout, `OpenClaw doesn't need a separate login.
Provider keys in /root/.openclaw/.env are sufficient.

  1. ciderbox orchard secrets init
  2. Fill API keys in .orchid.env
  3. ciderbox orchard secrets push --all`)
	case "openrouter", "anthropic", "openai", "xai":
		envKey := strings.ToUpper(args[0]) + "_API_KEY"
		fmt.Fprintf(a.Stdout, `To configure %s:

  1. ciderbox orchard secrets init
  2. Edit .orchid.env and set %s=<your key>
  3. Or export: export %s=<your key>
  4. ciderbox orchard secrets check
  5. ciderbox orchard secrets push --all
`, args[0], envKey, envKey)
	default:
		return exit(2, "unknown provider: %q (try openclaw, openrouter, anthropic)", args[0])
	}

	return nil
}

// ensureOpenClawConfig generates openclaw.json and .env inside a tree.
func (a App) ensureOpenClawConfig(ctx context.Context, runtime ContainerRuntime, containerCLI, containerID string, cfg *OrchardConfig, treeID string, workspacePath string, secrets *SecretsState) error {
	// 1. Generate and write openclaw.json
	ocJSON := generateOpenClawJSON(cfg, treeID, workspacePath)
	encodedJSON := base64.StdEncoding.EncodeToString([]byte(ocJSON))

	setupScript := fmt.Sprintf(`mkdir -p /root/.openclaw /root/.openclaw/workspace
printf %%s %q | base64 -d > /root/.openclaw/openclaw.json
chmod 600 /root/.openclaw/openclaw.json`, encodedJSON)

	if err := a.treeExec(ctx, runtime, containerID, []string{"/bin/sh", "-lc", setupScript}); err != nil {
		return fmt.Errorf("write openclaw.json: %w", err)
	}

	// 2. Write .env from secrets
	if secrets != nil {
		envContent := secrets.envContent(cfg)
		if err := a.writeTreeFile(ctx, containerCLI, containerID, "/root/.openclaw/.env", envContent, "600"); err != nil {
			return fmt.Errorf("write .env: %w", err)
		}
	}

	return nil
}

func (a App) writeTreeFile(ctx context.Context, containerCLI, containerID, path, content, mode string) error {
	script := fmt.Sprintf("mkdir -p %s && cat > %s && chmod %s %s", shQuote(filepath.Dir(path)), shQuote(path), shQuote(mode), shQuote(path))
	cmd := exec.CommandContext(ctx, containerCLI, "exec", "-i", containerID, "/bin/sh", "-lc", script)
	cmd.Stdin = strings.NewReader(content)
	cmd.Stderr = a.Stderr
	return cmd.Run()
}

// contains checks if a string is in a slice.
func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
