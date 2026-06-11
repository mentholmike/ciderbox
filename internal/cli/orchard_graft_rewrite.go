package cli

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
)

// orchardGraft installs OpenClaw + generates configs on one or all trees.
func (a App) orchardGraft(ctx context.Context, args []string) error {
	fs := newFlagSet("orchard graft", a.Stderr)
	treeID := fs.String("tree", "", "tree ID to graft onto")
	graftAll := fs.Bool("all", false, "graft OpenClaw onto all trees")
	upgrade := fs.Bool("upgrade", false, "force reinstall even if already grafted")
	configFile := fs.String("config", orchardDefaultFile, "path to orchard manifest")
	noValidate := fs.Bool("no-validate", false, "skip openclaw config validate after graft")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *graftAll && *treeID != "" {
		return exit(2, "orchard graft: specify --tree or --all, not both")
	}

	config, err := readOrchardConfig(*configFile)
	if err != nil {
		return exit(2, "orchard graft: config: %v", err)
	}

	trees, runtimeConfig, containerRuntime, err := a.orchardContainers(ctx, config.Name)
	if err != nil {
		return exit(2, "orchard graft: %v", err)
	}
	containerCLI := blank(runtimeConfig.AppleContainer.CLIPath, "container")

	targetTrees := make([]ContainerInfo, 0)
	if *graftAll {
		targetTrees = trees
	} else if *treeID != "" {
		target, err := a.findTreeInSlice(trees, *treeID)
		if err != nil {
			return exit(2, "orchard graft: %v", err)
		}
		targetTrees = append(targetTrees, target)
	} else {
		return exit(2, "orchard graft: specify --tree <id> or --all")
	}

	if len(targetTrees) == 0 {
		return exit(2, "orchard graft: no matching trees found")
	}

	// Load secrets
	secrets, err := loadSecrets(config)
	if err != nil {
		return exit(2, "orchard graft: load secrets: %v", err)
	}

	// Validate required secrets
	if missing := secrets.validateRequired(config.Secrets.Required); len(missing) > 0 {
		return exit(2, "orchard graft: required secrets missing: %s (run `orchard secrets check` or fill .orchid.env)", strings.Join(missing, ", "))
	}

	// Resolve workspace path
	wsPath := ""
	if config.Workspace.Path != "" {
		wsPath = config.Workspace.Path
	} else {
		wsPath = "/work/ciderbox"
	}

	fmt.Fprintf(a.Stdout, "=== Orchard Graft ===\n")
	fmt.Fprintf(a.Stdout, "Orchard: %s\n", config.Name)
	fmt.Fprintf(a.Stdout, "Trees: %d\n", len(targetTrees))
	if *upgrade {
		fmt.Fprintln(a.Stdout, "Mode: upgrade (force reinstall)")
	}
	fmt.Fprintf(a.Stdout, "Secrets: %d pass-through, %d required\n", len(config.Secrets.PassThrough), len(config.Secrets.Required))
	fmt.Fprintln(a.Stdout)

	installed, configured, skipped, failed := 0, 0, 0, 0
	for _, tree := range targetTrees {
		name := blank(tree.Labels["tree.id"], tree.Name)
		fmt.Fprintf(a.Stdout, "[%s] checking...\n", name)

		alreadyInstalled := false
		if !*upgrade {
			if out, err := a.treeExecCapture(ctx, containerRuntime, tree.ID, []string{"openclaw", "--version"}); err == nil && out != "" {
				fmt.Fprintf(a.Stdout, "[%s] OpenClaw already installed (%s); refreshing config\n", name, strings.TrimSpace(out))
				alreadyInstalled = true
				skipped++
			}
		}

		if !alreadyInstalled {
			fmt.Fprintf(a.Stdout, "[%s] grafting (Node 22 + OpenClaw)...\n", name)
			if err := a.graftTree(ctx, containerRuntime, config, tree, name, *upgrade); err != nil {
				fmt.Fprintf(a.Stderr, "[%s] graft failed: %v\n", name, err)
				failed++
				continue
			}
			installed++
		}

		if err := a.ensureTreeRuntimeDeps(ctx, containerRuntime, tree.ID); err != nil {
			fmt.Fprintf(a.Stderr, "[%s] runtime dependency check failed: %v\n", name, err)
			failed++
			continue
		}

		fmt.Fprintf(a.Stdout, "[%s] generating openclaw.json + .env...\n", name)
		if err := a.ensureOpenClawConfig(ctx, containerRuntime, containerCLI, tree.ID, config, name, wsPath, secrets); err != nil {
			fmt.Fprintf(a.Stderr, "[%s] config generation failed: %v\n", name, err)
			failed++
			continue
		}

		// Write identity doc
		identity := blank(config.Agent.Identity, name)
		identityDoc := fmt.Sprintf("# IDENTITY\n\nTree: %s\nIdentity: %s\nModel: %s\n", name, identity, config.Agent.Model)
		encodedID := base64.StdEncoding.EncodeToString([]byte(identityDoc))
		idScript := fmt.Sprintf("printf %%s %q | base64 -d > /root/.openclaw/workspace/IDENTITY.md && chmod 644 /root/.openclaw/workspace/IDENTITY.md", encodedID)
		_ = a.treeExec(ctx, containerRuntime, tree.ID, []string{"/bin/sh", "-lc", idScript})

		// Validate OpenClaw config
		if !*noValidate {
			fmt.Fprintf(a.Stdout, "[%s] validating OpenClaw config...\n", name)
			if err := a.treeExec(ctx, containerRuntime, tree.ID, []string{"openclaw", "config", "validate"}); err != nil {
				fmt.Fprintf(a.Stderr, "[%s] openclaw config validate: %v\n", name, err)
				failed++
				continue
			}
		}

		configured++
	}

	fmt.Fprintf(a.Stdout, "\ninstalled=%d configured=%d skipped_install=%d failed=%d\n", installed, configured, skipped, failed)
	if failed > 0 {
		return exit(1, "%d tree(s) failed to graft", failed)
	}
	return nil
}

func (a App) ensureTreeRuntimeDeps(ctx context.Context, containerRuntime ContainerRuntime, containerID string) error {
	script := strings.Join([]string{
		"set -e",
		"if command -v python3 >/dev/null 2>&1; then exit 0; fi",
		"export DEBIAN_FRONTEND=noninteractive",
		"if ! command -v apt-get >/dev/null 2>&1; then echo 'python3 missing and apt-get unavailable' >&2; exit 1; fi",
		"apt-get update -qq",
		"apt-get install -y -qq --no-install-recommends python3",
	}, " && ")
	return a.treeExec(ctx, containerRuntime, containerID, []string{"/bin/sh", "-lc", script})
}
