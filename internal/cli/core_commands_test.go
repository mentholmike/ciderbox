package cli

import (
	"strings"
	"testing"
)

func TestPackageListMapsCommonPackagesByManager(t *testing.T) {
	tests := []struct {
		manager string
		want    string
	}{
		{manager: "apt", want: "curl golang python3 rsync xz-utils"},
		{manager: "dnf", want: "curl-minimal golang python3 rsync xz"},
		{manager: "microdnf", want: "curl-minimal golang python3 rsync xz"},
		{manager: "yum", want: "curl golang python3 rsync xz"},
		{manager: "apk", want: "curl go python3 rsync xz"},
		{manager: "pacman", want: "curl go python rsync xz"},
		{manager: "zypper", want: "curl go python3 rsync xz"},
	}

	for _, tt := range tests {
		got := packageList(tt.manager, []string{"curl", "golang", "python3", "rsync", "rsync", "xz"})
		if got != tt.want {
			t.Fatalf("%s package list = %q, want %q", tt.manager, got, tt.want)
		}
	}
}

func TestPackageInstallScriptIncludesMajorManagers(t *testing.T) {
	script := packageInstallScript([]string{"rsync"})
	for _, cmd := range []string{"apt-get", "apk", "dnf", "microdnf", "yum", "pacman", "zypper"} {
		if !strings.Contains(script, cmd) {
			t.Fatalf("install script missing %s:\n%s", cmd, script)
		}
	}
}

func TestDistroRunFlagsPutImageBeforeBaseFlags(t *testing.T) {
	flags := distroRunFlags([]string{"--provider", "apple-container", "--", "sh", "-c", "true"}, "alpine:latest")
	opts, command, err := parseRunFlags(flags)
	if err != nil {
		t.Fatal(err)
	}
	if opts.image != "alpine:latest" {
		t.Fatalf("image = %q, want alpine:latest", opts.image)
	}
	if strings.Join(command, " ") != "sh -c true" {
		t.Fatalf("command = %q", command)
	}
}

func TestCompileTestDependenciesAcceptCanonicalAndDeprecatedKeys(t *testing.T) {
	cfg := CompileTestConfig{
		Deps:         []string{"rsync"},
		Dependencies: []string{"build-essential", "git"},
	}
	got := strings.Join(compileTestDependencies(cfg), ",")
	want := "rsync,build-essential,git"
	if got != want {
		t.Fatalf("compile test dependencies = %q, want %q", got, want)
	}
}
