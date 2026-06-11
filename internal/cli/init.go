package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (a App) initProject(_ context.Context, args []string) error {
	fs := newFlagSet("init", a.Stderr)
	force := fs.Bool("force", false, "overwrite generated files")
	detect := fs.Bool("detect", false, "detect repo test commands and write a jobs.detected entry")
	skill := fs.String("skill", ".agents/skills/ciderbox/SKILL.md", "agent skill path")
	config := fs.String("config", ".ciderbox.yaml", "repo config path")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	repo, err := findRepo()
	if err != nil {
		return err
	}
	detected := initProjectDetection{}
	if *detect {
		detected = detectInitProject(repo.Root)
	}
	files := map[string]string{
		filepath.Join(repo.Root, *config): projectConfigTemplate(detected),
		filepath.Join(repo.Root, *skill):  skillTemplate(detected),
	}
	for path, content := range files {
		if err := writeInitFile(path, content, *force); err != nil {
			return err
		}
		fmt.Fprintf(a.Stdout, "wrote %s\n", path)
	}
	if *detect {
		if len(detected.Commands) == 0 {
			fmt.Fprintln(a.Stdout, "detected no runnable project commands; edit .ciderbox.yaml compileTest manually")
		} else {
			fmt.Fprintf(a.Stdout, "detected compileTest command: %s\n", strings.Join(detected.Commands, " && "))
		}
	}
	return nil
}

func writeInitFile(path, content string, force bool) error {
	if _, err := os.Stat(path); err == nil && !force {
		return exit(2, "%s already exists; use --force to overwrite", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return exit(2, "create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return exit(2, "write %s: %v", path, err)
	}
	return nil
}

type initProjectDetection struct {
	Commands       []string
	PreflightTools []string
	SyncExcludes   []string
	EnvAllow       []string
}

func projectConfigTemplate(detected initProjectDetection) string {
	deps := append([]string{"rsync"}, detected.PreflightTools...)

	var b strings.Builder
	b.WriteString(`compileTest:
  distros:
    - name: debian
      image: debian:bookworm
    # - name: ubuntu
    #   image: ubuntu:26.04
    # - name: alpine
    #   image: alpine:latest
    # - name: fedora
    #   image: fedora:latest
    # - name: rocky
    #   image: rockylinux:9
  parallel: false
  dependencies:
`)
	writeYAMLList(&b, deps, 4)
	if len(detected.Commands) > 0 {
		b.WriteString("  command: >\n")
		for i, command := range detected.Commands {
			line := command
			if i < len(detected.Commands)-1 {
				line += " &&"
			}
			fmt.Fprintf(&b, "      %s\n", line)
		}
	} else {
		b.WriteString(`  command: "echo edit .ciderbox.yaml compileTest.command"
`)
	}
	return b.String()
}

func writeYAMLList(b *strings.Builder, values []string, indent int) {
	prefix := strings.Repeat(" ", indent)
	for _, value := range values {
		fmt.Fprintf(b, "%s- %s\n", prefix, yamlScalar(value))
	}
}

func yamlScalar(value string) string {
	if strings.TrimSpace(value) == "" {
		return `""`
	}
	if strings.ContainsAny(value, ":#[]{}&,*!?|>'\"%@\t`") {
		return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
	}
	return value
}

func skillTemplate(detected initProjectDetection) string {
	var b strings.Builder
	b.WriteString(`# Ciderbox

Use Ciderbox for cross-distro compile testing and verification on Apple Silicon Macs.

Workflow:
- Test compilation across configured Linux distributions: ciderbox compile-test
- Run a single command in a fresh container: ciderbox run -- <cmd>
- Build the project: ciderbox build
- Clean up all containers: ciderbox chop

Ciderbox tests that your code compiles and basic commands run on:
- Debian Bookworm (default)

Additional distros can be uncommented in .ciderbox.yaml:
- Ubuntu 26.04
- Alpine Latest
- Fedora Latest
- Rocky Linux 9

Dependencies are automatically installed in test containers via the package manager.
`)
	if len(detected.Commands) > 0 {
		b.WriteString("\nDetected compileTest command:\n```sh\nciderbox compile-test\n```\n")
	}
	return b.String()
}
