package railway

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func NewRailwayBackend(spec ProviderSpec, cfg Config, rt Runtime) Backend {
	cfg.Provider = providerName
	return &railwayBackend{spec: spec, cfg: cfg, rt: rt}
}

type railwayBackend struct {
	spec   ProviderSpec
	cfg    Config
	rt     Runtime
	client railwayAPI
}

func (b *railwayBackend) Spec() ProviderSpec { return b.spec }

func (b *railwayBackend) Warmup(ctx context.Context, req WarmupRequest) error {
	_ = ctx
	_ = req
	// Warmup is rejected because Railway services and projects must be created
	// out-of-band (the provider would otherwise leak billable resources if a
	// warmup were triggered accidentally). Use the Railway dashboard or CLI to
	// create the service, then point crabbox at it with --id <serviceId>.
	return exit(2, "provider=%s does not support warmup; create the Railway service out-of-band", providerName)
}

func (b *railwayBackend) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	if err := rejectRailwayRunOptions(req); err != nil {
		return RunResult{}, err
	}
	if req.ID == "" {
		return RunResult{}, exit(2, "provider=%s requires --id <railway-service-id>", providerName)
	}
	projectID := strings.TrimSpace(b.cfg.Railway.ProjectID)
	environmentID := strings.TrimSpace(b.cfg.Railway.EnvironmentID)
	if projectID == "" {
		return RunResult{}, exit(2, "provider=%s requires --railway-project or RAILWAY_PROJECT_ID", providerName)
	}
	if environmentID == "" {
		return RunResult{}, exit(2, "provider=%s requires --railway-environment or RAILWAY_ENVIRONMENT_ID", providerName)
	}
	if len(req.Command) == 0 {
		return RunResult{}, exit(2, "missing command")
	}
	client, err := b.api()
	if err != nil {
		return RunResult{}, err
	}
	started := b.now()
	fmt.Fprintf(b.rt.Stderr, "running on %s service=%s command=%s (start command is owned by the Railway service)\n", providerName, req.ID, strings.Join(req.Command, " "))
	if _, err := client.TriggerDeploy(ctx, projectID, environmentID, req.ID); err != nil {
		return RunResult{}, ExitError{Code: 1, Message: fmt.Sprintf("%s trigger deploy failed: %v", providerName, err)}
	}
	deployment, err := client.LatestDeployment(ctx, projectID, environmentID, req.ID)
	if err != nil {
		return RunResult{}, ExitError{Code: 1, Message: fmt.Sprintf("%s fetch deployment failed: %v", providerName, err)}
	}
	if deployment.ID == "" {
		return RunResult{}, ExitError{Code: 1, Message: fmt.Sprintf("%s deployment id missing", providerName)}
	}
	logs, err := client.DeploymentLogs(ctx, deployment.ID, 0)
	if err != nil {
		return RunResult{}, ExitError{Code: 1, Message: fmt.Sprintf("%s fetch logs failed: %v", providerName, err)}
	}
	for _, line := range logs {
		fmt.Fprintln(b.rt.Stdout, line)
	}
	commandDuration := b.now().Sub(started)
	result := RunResult{
		ExitCode: railwayExitCode(deployment.Status),
		Command:  commandDuration,
		Total:    commandDuration,
	}
	fmt.Fprintf(b.rt.Stderr, "%s run summary command=%s total=%s exit=%d status=%s\n", providerName, result.Command.Round(time.Millisecond), result.Total.Round(time.Millisecond), result.ExitCode, deployment.Status)
	if req.TimingJSON {
		if err := writeTimingJSON(b.rt.Stderr, timingReport{
			Provider:  providerName,
			CommandMs: commandDuration.Milliseconds(),
			TotalMs:   result.Total.Milliseconds(),
			ExitCode:  result.ExitCode,
		}); err != nil {
			return result, err
		}
	}
	if result.ExitCode != 0 {
		return result, ExitError{Code: result.ExitCode, Message: fmt.Sprintf("%s deployment status=%s", providerName, deployment.Status)}
	}
	return result, nil
}

func (b *railwayBackend) List(ctx context.Context, req ListRequest) ([]LeaseView, error) {
	_ = req
	client, err := b.api()
	if err != nil {
		return nil, err
	}
	services, err := client.ListServices(ctx)
	if err != nil {
		return nil, err
	}
	servers := make([]Server, 0, len(services))
	for _, s := range services {
		servers = append(servers, Server{
			CloudID:  s.ID,
			Provider: providerName,
			Name:     s.Name,
			Labels:   map[string]string{"projectId": s.ProjectID},
		})
	}
	return servers, nil
}

func (b *railwayBackend) Status(ctx context.Context, req StatusRequest) (StatusView, error) {
	if req.ID == "" {
		return StatusView{}, exit(2, "provider=%s status requires --id <railway-service-id>", providerName)
	}
	projectID := strings.TrimSpace(b.cfg.Railway.ProjectID)
	environmentID := strings.TrimSpace(b.cfg.Railway.EnvironmentID)
	if projectID == "" || environmentID == "" {
		return StatusView{}, exit(2, "provider=%s status requires --railway-project and --railway-environment", providerName)
	}
	client, err := b.api()
	if err != nil {
		return StatusView{}, err
	}
	service, err := client.GetService(ctx, req.ID)
	if err != nil {
		return StatusView{}, err
	}
	deployment, err := client.LatestDeployment(ctx, projectID, environmentID, req.ID)
	if err != nil {
		return StatusView{}, err
	}
	view := StatusView{
		ID:         service.ID,
		Slug:       service.Name,
		Provider:   providerName,
		TargetOS:   targetLinux,
		State:      strings.ToLower(deployment.Status),
		ServerID:   service.ID,
		ServerType: "railway-service",
		Network:    networkPublic,
		Ready:      railwayReady(deployment.Status),
		Labels:     map[string]string{"projectId": service.ProjectID},
	}
	return view, nil
}

func (b *railwayBackend) Stop(ctx context.Context, req StopRequest) error {
	if req.ID == "" {
		return exit(2, "provider=%s stop requires --id <railway-service-id>", providerName)
	}
	projectID := strings.TrimSpace(b.cfg.Railway.ProjectID)
	environmentID := strings.TrimSpace(b.cfg.Railway.EnvironmentID)
	if projectID == "" || environmentID == "" {
		return exit(2, "provider=%s stop requires --railway-project and --railway-environment", providerName)
	}
	client, err := b.api()
	if err != nil {
		return err
	}
	deployment, err := client.LatestDeployment(ctx, projectID, environmentID, req.ID)
	if err != nil {
		return err
	}
	if deployment.ID == "" {
		return exit(5, "provider=%s service=%s has no deployment to stop", providerName, req.ID)
	}
	return client.StopDeployment(ctx, deployment.ID)
}

func (b *railwayBackend) api() (railwayAPI, error) {
	if b.client != nil {
		return b.client, nil
	}
	return newRailwayClient(b.cfg, b.rt)
}

func rejectRailwayRunOptions(req RunRequest) error {
	if req.Keep {
		return exit(2, "provider=%s lifecycle is owned by Railway; --keep is not supported", providerName)
	}
	if req.Reclaim {
		return exit(2, "provider=%s lifecycle is owned by Railway; --reclaim is not supported", providerName)
	}
	if !req.NoSync {
		// Railway does not expose a workspace-sync surface; mirror other
		// delegated-only providers and require --no-sync explicitly so callers
		// understand the deploy runs whatever the service is already configured
		// to run.
		return exit(2, "provider=%s does not support workspace sync; pass --no-sync", providerName)
	}
	if req.SyncOnly {
		return exit(2, "provider=%s does not support sync; --sync-only is rejected", providerName)
	}
	if req.ChecksumSync {
		return exit(2, "provider=%s does not support sync; --checksum is rejected", providerName)
	}
	if req.ForceSyncLarge {
		return exit(2, "provider=%s does not support sync; --force-sync-large is rejected", providerName)
	}
	if req.FullResync {
		return exit(2, "provider=%s does not support sync; --full-resync is rejected", providerName)
	}
	return nil
}

func railwayExitCode(status string) int {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "SUCCESS", "DEPLOYED":
		return 0
	case "":
		return 0
	}
	return 1
}

func railwayReady(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "SUCCESS", "DEPLOYED":
		return true
	}
	return false
}

func (b *railwayBackend) now() time.Time {
	if b.rt.Clock != nil {
		return b.rt.Clock.Now()
	}
	return time.Now()
}
