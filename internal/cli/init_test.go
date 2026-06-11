package cli

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestProjectConfigTemplateUsesSingleDistroAndDependencies(t *testing.T) {
	data := projectConfigTemplate(initProjectDetection{})
	if strings.Contains(data, "\n  deps:") {
		t.Fatalf("generated config uses deprecated deps key:\n%s", data)
	}

	var cfg Config
	if err := yaml.Unmarshal([]byte(data), &cfg); err != nil {
		t.Fatalf("generated config is not valid YAML: %v\n%s", err, data)
	}
	if got := len(cfg.CompileTest.Distros); got != 1 {
		t.Fatalf("active distro count = %d, want 1\n%s", got, data)
	}
	if cfg.CompileTest.Distros[0].Image != "debian:bookworm" {
		t.Fatalf("default distro image = %q", cfg.CompileTest.Distros[0].Image)
	}
	if got := strings.Join(cfg.CompileTest.Dependencies, ","); got != "rsync" {
		t.Fatalf("dependencies = %q, want rsync", got)
	}
}
