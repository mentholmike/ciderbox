package cli

import (
	"strings"
	"testing"
	"time"
)

func TestFormatRunSummary(t *testing.T) {
	got := formatRunSummary(runTimings{
		sync:    1200 * time.Millisecond,
		command: 3400 * time.Millisecond,
		syncSteps: syncStepTimings{
			manifest: 20 * time.Millisecond,
			rsync:    900 * time.Millisecond,
		},
		syncStats: syncStats{
			files:         10,
			bytes:         1536,
			deleted:       1,
			manifestBytes: 120,
		},
		syncSkipped: true,
	}, 5*time.Second, 7)
	for _, want := range []string{
		"run summary",
		"sync=1.2s",
		"command=3.4s",
		"total=5s",
		"sync_skipped=true",
		"exit=7",
		"sync_steps=manifest:20ms,rsync:900ms",
		"sync_files=10",
		"sync_bytes=1.5 KiB",
		"sync_deleted=1",
		"sync_manifest=120 B",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q in %q", want, got)
		}
	}
}
