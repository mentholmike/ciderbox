package cli

import (
	"strings"
	"testing"
)

func TestRemoteImageScrubRemovesCommonSecretStores(t *testing.T) {
	script := remoteImageScrub()
	for _, want := range []string{
		"/root/.aws",
		"/home/*/.aws",
		"/root/.docker",
		"/.crabbox/actions/*.env.sh",
		"cloud-init clean --logs",
		"journalctl --vacuum-time=1s",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("scrub script missing %q:\n%s", want, script)
		}
	}
}
