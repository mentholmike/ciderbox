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
		{manager: "apt", want: "golang python3 rsync"},
		{manager: "dnf", want: "golang python3 rsync"},
		{manager: "apk", want: "go python3 rsync"},
		{manager: "pacman", want: "go python rsync"},
		{manager: "zypper", want: "go python3 rsync"},
	}

	for _, tt := range tests {
		got := packageList(tt.manager, []string{"golang", "python3", "rsync", "rsync"})
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
