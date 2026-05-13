package cloudflaresandbox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

func (b *cloudflareSandboxBackend) syncWorkspace(ctx context.Context, client *cloudflareSandboxClient, sandboxID string, req RunRequest, workdir string) ([]timingPhase, time.Duration, error) {
	start := b.now()
	excludes, err := syncExcludes(req.Repo.Root, b.cfg)
	if err != nil {
		return nil, 0, err
	}
	manifestStarted := b.now()
	manifest, err := syncManifest(req.Repo.Root, excludes)
	if err != nil {
		return nil, 0, exit(6, "build sync file list: %v", err)
	}
	manifestDuration := b.now().Sub(manifestStarted)
	preflightStarted := b.now()
	if err := checkSyncPreflight(manifest, b.cfg, req.ForceSyncLarge, b.rt.Stderr); err != nil {
		return nil, 0, err
	}
	preflightDuration := b.now().Sub(preflightStarted)
	prepareStarted := b.now()
	if err := b.prepareWorkspace(ctx, client, sandboxID, workdir); err != nil {
		return nil, 0, err
	}
	prepareDuration := b.now().Sub(prepareStarted)
	archiveStarted := b.now()
	archive, err := createCloudflareSandboxSyncArchive(ctx, req.Repo, manifest, b.rt.Stderr)
	if err != nil {
		return nil, 0, err
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	archiveDuration := b.now().Sub(archiveStarted)
	uploadStarted := b.now()
	remoteArchive := remoteArchivePath()
	if err := client.uploadFile(ctx, sandboxID, archive.Name(), remoteArchive); err != nil {
		return nil, 0, fmt.Errorf("upload archive: %w", err)
	}
	extract := strings.Join([]string{
		"tar -xzf " + shellQuote(remoteArchive) + " -C " + shellQuote(workdir),
		"rm -f " + shellQuote(remoteArchive),
	}, " && ")
	if err := b.execShell(ctx, client, sandboxID, extract, io.Discard); err != nil {
		return nil, 0, err
	}
	uploadDuration := b.now().Sub(uploadStarted)
	total := b.now().Sub(start)
	return []timingPhase{
		{Name: "manifest", Ms: manifestDuration.Milliseconds()},
		{Name: "preflight", Ms: preflightDuration.Milliseconds()},
		{Name: "prepare", Ms: prepareDuration.Milliseconds()},
		{Name: "archive", Ms: archiveDuration.Milliseconds()},
		{Name: "upload", Ms: uploadDuration.Milliseconds()},
		{Name: "cloudflare_sandbox_sync", Ms: total.Milliseconds()},
	}, total, nil
}

func (b *cloudflareSandboxBackend) prepareWorkspace(ctx context.Context, client *cloudflareSandboxClient, sandboxID, workdir string) error {
	command := "mkdir -p " + shellQuote(workdir)
	if b.cfg.Sync.Delete {
		command = "rm -rf " + shellQuote(workdir) + " && " + command
	}
	return b.execShell(ctx, client, sandboxID, command, io.Discard)
}

func (b *cloudflareSandboxBackend) execShell(ctx context.Context, client *cloudflareSandboxClient, sandboxID, command string, stdout io.Writer) error {
	code, err := client.execStream(ctx, sandboxID, execStreamRequest{
		Command:   command,
		Cwd:       "/",
		TimeoutMS: durationMillisecondsCeil(b.cfg.TTL),
	}, stdout, b.rt.Stderr)
	if err != nil {
		return fmt.Errorf("cloudflare-sandbox exec %q: %w", command, err)
	}
	if code != 0 {
		return exit(code, "cloudflare-sandbox exec %q exited %d", command, code)
	}
	return nil
}

func createCloudflareSandboxSyncArchive(ctx context.Context, repo Repo, manifest SyncManifest, stderr io.Writer) (*os.File, error) {
	var input bytes.Buffer
	input.Write(manifest.NUL())
	archive, err := os.CreateTemp("", "crabbox-cloudflare-sandbox-sync-*.tgz")
	if err != nil {
		return nil, fmt.Errorf("create sync archive temp file: %w", err)
	}
	keep := false
	defer func() {
		if !keep {
			name := archive.Name()
			_ = archive.Close()
			_ = os.Remove(name)
		}
	}()
	cmd := exec.CommandContext(ctx, "tar", "--no-xattrs", "-czf", "-", "-C", repo.Root, "--null", "-T", "-")
	cmd.Stdin = &input
	cmd.Env = append(os.Environ(), "COPYFILE_DISABLE=1")
	cmd.Stdout = archive
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return nil, exit(6, "create sync archive: %v", err)
	}
	keep = true
	return archive, nil
}
