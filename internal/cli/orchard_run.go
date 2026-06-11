package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"encoding/base64"
)

// orchardRun executes one task on one tree or all trees.
// Supports --sync (sync workspace into trees) and --tree (single tree).
// Generates a task ID, writes structured results to ~/.ciderbox/orchards/<name>/tasks/<task-id>/
func (a App) orchardRun(ctx context.Context, args []string) error {
	fs := newFlagSet("orchard run", a.Stderr)
	configFile := fs.String("config", orchardDefaultFile, "path to orchard manifest")
	treeID := fs.String("tree", "", "tree ID to run on; defaults to all trees")
	task := fs.String("task", "", "task prompt/instruction to execute")
	syncFlag := fs.Bool("sync", false, "sync current project workspace into trees before running")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *task == "" {
		return exit(2, "--task required")
	}

	config, err := readOrchardConfig(*configFile)
	if err != nil {
		return exit(2, "orchard config: %v", err)
	}

	trees, runtimeConfig, containerRuntime, err := a.orchardContainers(ctx, config.Name)
	if err != nil {
		return exit(2, "orchard run: %v", err)
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
		if *treeID != "" {
			return exit(2, "tree %q not found", *treeID)
		}
		return exit(2, "no trees running")
	}

	// Generate task ID and local state paths
	startedAt := time.Now().UTC()
	taskID := fmt.Sprintf("task-%s-%09d", startedAt.Format("20060102-150405"), startedAt.Nanosecond())
	home, _ := os.UserHomeDir()
	taskDir := filepath.Join(home, ".ciderbox", "orchards", config.Name, "tasks", taskID)
	if err := os.MkdirAll(taskDir, 0700); err != nil {
		return exit(2, "create task dir: %v", err)
	}
	_ = os.Chmod(taskDir, 0700)

	// Inside each tree, results go to /tmp/orchid/tasks/<task-id>/
	taskResultDir := fmt.Sprintf("/tmp/orchid/tasks/%s", taskID)
	taskJSONPath := fmt.Sprintf("%s/result.json", taskResultDir)

	// Resolve workspace sync dir
	var workDir string
	syncEnabled := *syncFlag || config.Workspace.Sync
	if syncEnabled {
		if config.Workspace.Path != "" {
			workDir = config.Workspace.Path
		} else {
			cwd, err := os.Getwd()
			if err != nil {
				return exit(2, "getwd: %v", err)
			}
			projectName := filepath.Base(cwd)
			if projectName == "." || projectName == "" {
				projectName = "project"
			}
			workDir = fmt.Sprintf("/work/ciderbox/%s", projectName)
		}
	}

	command := strings.TrimSpace(config.Agent.Command)
	if command == "" {
		command = defaultOrchardAgentCommand
	}

	encodedTask := base64.StdEncoding.EncodeToString([]byte(*task))
	encodedCommand := base64.StdEncoding.EncodeToString([]byte(command))

	fmt.Fprintf(a.Stdout, "=== Orchard Run ===\n")
	fmt.Fprintf(a.Stdout, "Orchard: %s\n", config.Name)
	fmt.Fprintf(a.Stdout, "Task ID: %s\n", taskID)
	fmt.Fprintf(a.Stdout, "Trees: %d\n", len(trees))
	if syncEnabled {
		fmt.Fprintf(a.Stdout, "Sync: %s\n", workDir)
	}
	fmt.Fprintf(a.Stdout, "Result path: %s\n\n", taskJSONPath)

	// Write initial task state
	treesStatus := make(map[string]interface{})
	for _, tree := range trees {
		name := blank(tree.Labels["tree.id"], tree.Name)
		treesStatus[name] = map[string]string{"status": "pending"}
	}
	taskFile := filepath.Join(taskDir, "task.json")
	taskMeta := map[string]interface{}{
		"id":           taskID,
		"orchard":      config.Name,
		"task":         *task,
		"status":       "running",
		"trees":        treesStatus,
		"created_at":   startedAt.Format(time.RFC3339Nano),
		"completed_at": nil,
	}
	taskData, _ := json.MarshalIndent(taskMeta, "", "  ")
	os.WriteFile(taskFile, taskData, 0600)

	// Setup script that each tree runs before the real command
	setupScript := fmt.Sprintf("mkdir -p %s", taskResultDir)

	// Sync workspace into each tree (early, before tasks start)
	if syncEnabled {
		fmt.Fprintf(a.Stdout, "Syncing workspace into %d tree(s)...\n", len(trees))
		for _, tree := range trees {
			name := blank(tree.Labels["tree.id"], tree.Name)
			fmt.Fprintf(a.Stdout, "[%s] syncing %s...\n", name, workDir)
			if err := a.orchardSyncTree(ctx, containerRuntime, containerCLI, tree.ID, workDir); err != nil {
				fmt.Fprintf(a.Stderr, "[%s] sync failed: %v\n", name, err)
				treesStatus[name] = map[string]string{"status": "failed", "error": fmt.Sprintf("sync: %v", err)}
				continue
			}
			_ = a.treeExec(ctx, containerRuntime, tree.ID, []string{"mkdir", "-p", taskResultDir})
		}
	}

	// Run task on each tree
	failures := 0
	for _, tree := range trees {
		name := blank(tree.Labels["tree.id"], tree.Name)

		// Skip if tree already failed during sync
		if ts, ok := treesStatus[name].(map[string]string); ok && ts["status"] == "failed" {
			if errMsg := ts["error"]; errMsg != "" {
				writeLocalTaskError(taskDir, name, errMsg)
			}
			failures++
			continue
		}

		fmt.Fprintf(a.Stdout, "[%s] running task...\n", name)

		if err := a.ensureTreeRuntimeDeps(ctx, containerRuntime, tree.ID); err != nil {
			errMsg := fmt.Sprintf("runtime deps: %v", err)
			fmt.Fprintf(a.Stderr, "[%s] runtime dependency check failed: %v\n", name, err)
			treesStatus[name] = map[string]string{"status": "failed", "error": errMsg}
			writeLocalTaskError(taskDir, name, errMsg)
			failures++
			continue
		}

		// Build workspace env
		workEnv := ""
		if syncEnabled && workDir != "" {
			workEnv = fmt.Sprintf("export ORCHARD_WORKSPACE=%s", shQuote(workDir))
		}

		_ /* unused */ = setupScript
		_ /* unused */ = taskJSONPath

		// Build the execution script
		stdoutPath := fmt.Sprintf("%s/stdout", taskResultDir)
		script := fmt.Sprintf(`set -e
%s
mkdir -p %s
export ORCHARD_TASK="$(printf %%s %q | base64 -d)"
export ORCHARD_WORKSPACE=%s
ORCHARD_AGENT_COMMAND="$(printf %%s %q | base64 -d)"
set +e
/bin/sh -lc "$ORCHARD_AGENT_COMMAND" > %s 2>&1
status=$?
echo "$status" > %s.exit
`, workEnv, shQuote(taskResultDir), encodedTask, shQuote(workDir), encodedCommand, stdoutPath, stdoutPath)

		resultScript := fmt.Sprintf(`python3 - <<'PY'
import json, os
p = %s
ep = %s
try:
    with open(ep) as f: ec=int(f.read().strip())
except Exception: ec=-1
try:
    with open(p) as f: stdout=f.read()
except Exception: stdout=''
r={'task_id':%s,'tree':%s,'status':'ok' if ec==0 else 'error','output':stdout,'exit_code':ec}
os.makedirs(os.path.dirname(%s),exist_ok=True)
with open(%s,'w') as f: json.dump(r,f,indent=2)
with open(%s,'w') as f: json.dump(r,f,indent=2)
PY
writer_status=$?
if [ "$writer_status" -ne 0 ]; then
  exit "$writer_status"
fi
exit "$status"
`, jsonEncode(stdoutPath), jsonEncode(stdoutPath+".exit"), jsonEncode(taskID), jsonEncode(name), jsonEncode(taskJSONPath), jsonEncode(taskJSONPath), jsonEncode(orchardResultPath))

		script += resultScript

		if err := a.treeExec(ctx, containerRuntime, tree.ID, []string{"/bin/sh", "-lc", script}); err != nil {
			fmt.Fprintf(a.Stderr, "[%s] task failed: %v\n", name, err)
			treesStatus[name] = map[string]string{"status": "failed", "error": err.Error()}
			failures++

			// Try to collect partial result
			if out, readErr := a.treeExecCapture(ctx, containerRuntime, tree.ID, []string{"cat", taskJSONPath}); readErr == nil {
				outPath := filepath.Join(taskDir, fmt.Sprintf("%s.json", name))
				os.WriteFile(outPath, []byte(out), 0600)
			}
			continue
		}

		fmt.Fprintf(a.Stdout, "[%s] complete\n", name)
		treesStatus[name] = map[string]string{"status": "complete"}

		// Collect result from tree
		if out, err := a.treeExecCapture(ctx, containerRuntime, tree.ID, []string{"cat", taskJSONPath}); err == nil {
			outPath := filepath.Join(taskDir, fmt.Sprintf("%s.json", name))
			os.WriteFile(outPath, []byte(out), 0600)
		}
	}

	// Update task state
	now := time.Now().UTC().Format(time.RFC3339)
	taskOverall := "complete"
	if failures > 0 {
		taskOverall = "partial"
	}
	if failures == len(trees) {
		taskOverall = "failed"
	}
	taskMeta["status"] = taskOverall
	taskMeta["trees"] = treesStatus
	taskMeta["completed_at"] = now
	taskData, _ = json.MarshalIndent(taskMeta, "", "  ")
	os.WriteFile(taskFile, taskData, 0600)

	fmt.Fprintf(a.Stdout, "\nTask %s: %s/%d trees complete\n", taskID, strconv.Itoa(len(trees)-failures), len(trees))
	fmt.Fprintf(a.Stdout, "Results: %s\n", taskDir)

	if failures > 0 {
		return exit(1, "%d tree task(s) failed", failures)
	}
	return nil
}

// orchardSyncTree tars the current project directory and pipes it into a tree container.
func (a App) orchardSyncTree(ctx context.Context, runtime ContainerRuntime, containerCLI, containerID, dstDir string) error {
	dstDir = filepath.Clean(strings.TrimSpace(dstDir))
	if dstDir == "" || dstDir == "." || dstDir == "/" {
		return fmt.Errorf("refusing to sync into unsafe destination %q", dstDir)
	}
	if dstDir != "/work/ciderbox" && !strings.HasPrefix(dstDir, "/work/ciderbox/") {
		return fmt.Errorf("refusing to sync outside /work/ciderbox: %q", dstDir)
	}
	resetScript := fmt.Sprintf("rm -rf -- %s && mkdir -p %s", shQuote(dstDir), shQuote(dstDir))
	if err := a.treeExec(ctx, runtime, containerID, []string{"/bin/sh", "-lc", resetScript}); err != nil {
		return fmt.Errorf("reset %s: %w", dstDir, err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	tarArgs := []string{
		"czf", "-",
		"--no-xattrs",
		"--exclude", ".git",
		"--exclude", ".crabbox",
		"--exclude", ".agents",
		"--exclude", ".orchid.env",
		"--exclude", ".openclaw.env",
		"--exclude", ".env",
		"--exclude", ".env.*",
		"--exclude", "*.env",
		"--exclude", "node_modules",
		"--exclude", "vendor",
		"--exclude", "target",
		"--exclude", "*.tar.gz",
		"--exclude", "*.tar",
		"--exclude", ".DS_Store",
		"-C", cwd, ".",
	}
	tarCmd := exec.CommandContext(ctx, "tar", tarArgs...)

	untarArgs := []string{"exec", "-i", containerID, "tar", "xzf", "-", "-C", dstDir}
	untarCmd := exec.CommandContext(ctx, containerCLI, untarArgs...)

	untarCmd.Stdin, _ = tarCmd.StdoutPipe()
	untarCmd.Stderr = a.Stderr

	if err := untarCmd.Start(); err != nil {
		return fmt.Errorf("start untar: %w", err)
	}
	if err := tarCmd.Run(); err != nil {
		return fmt.Errorf("tar: %w", err)
	}
	if err := untarCmd.Wait(); err != nil {
		return fmt.Errorf("untar: %w", err)
	}
	return nil
}

// jsonEncode returns a JSON-encoded string safe for embedding in shell scripts.
func jsonEncode(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}

// escapeForPython escapes a string for embedding in a Python heredoc.
func escapeForPython(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}

func writeLocalTaskError(taskDir, treeName, message string) {
	result := HarvestResult{
		Tree:   treeName,
		Status: "failed",
		Error:  message,
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return
	}
	outName := strings.NewReplacer("/", "_", "\\", "_").Replace(treeName)
	_ = os.WriteFile(filepath.Join(taskDir, fmt.Sprintf("%s.json", outName)), data, 0600)
}
