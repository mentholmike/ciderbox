package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCFContainersProviderSpec(t *testing.T) {
	spec := Provider{}.Spec()
	if spec.Name != providerName {
		t.Fatalf("spec.Name = %q, want %q", spec.Name, providerName)
	}
	if spec.Kind != "delegated-run" {
		t.Fatalf("spec.Kind = %q, want delegated-run", spec.Kind)
	}
	if len(spec.Features) != 1 || spec.Features[0] != "archive-sync" {
		t.Fatalf("spec.Features = %#v, want archive-sync", spec.Features)
	}
	aliases := Provider{}.Aliases()
	for _, want := range []string{"cloudflare-containers", "cloudflare-container", "cf-container"} {
		if !containsString(aliases, want) {
			t.Fatalf("aliases = %#v, missing %q", aliases, want)
		}
	}
}

func TestCFContainersWorkdirRejectsBroadPaths(t *testing.T) {
	cfg := Config{}
	cfg.CFContainers.Workdir = "/workspace"
	if _, err := cfContainersWorkdir(cfg); err == nil {
		t.Fatal("cfContainersWorkdir accepted broad /workspace path")
	}
}

func TestBuildCFContainersCommandQuotesArgv(t *testing.T) {
	got, err := buildCFContainersCommand([]string{"node", "-e", "console.log('ok')"}, false)
	if err != nil {
		t.Fatal(err)
	}
	want := "'node' '-e' 'console.log('\\''ok'\\'')'"
	if got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
}

func TestCFContainersHealthyStateIsReady(t *testing.T) {
	if !cfContainersReady("healthy") {
		t.Fatal("healthy state should be ready")
	}
}

func TestCFContainersTokenFlagDoesNotDefaultToConfiguredSecret(t *testing.T) {
	cfg := Config{}
	cfg.CFContainers.Token = "secret-token"
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	values := RegisterCFContainersProviderFlags(fs, cfg).(cfContainersFlagValues)
	if got := *values.Token; got != "" {
		t.Fatalf("token flag default = %q, want empty", got)
	}
}

func TestCFContainersFlagsApply(t *testing.T) {
	cfg := Config{Provider: providerName}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	values := RegisterCFContainersProviderFlags(fs, cfg)
	err := fs.Parse([]string{
		"--cf-containers-url", "https://current.example",
		"--cf-containers-token", "token",
		"--cf-containers-workdir", "/workspace/current",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyCFContainersProviderFlags(&cfg, fs, values); err != nil {
		t.Fatal(err)
	}
	if cfg.CFContainers.APIURL != "https://current.example" || cfg.CFContainers.Token != "token" || cfg.CFContainers.Workdir != "/workspace/current" {
		t.Fatalf("cf containers flags not applied: %#v", cfg.CFContainers)
	}
}

func TestCFContainersPrepareWorkspacePreservesWhenRequested(t *testing.T) {
	for _, tc := range []struct {
		name           string
		deleteContents bool
		wantDelete     bool
	}{
		{name: "preserve", deleteContents: false, wantDelete: false},
		{name: "delete", deleteContents: true, wantDelete: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got execStreamRequest
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/sandboxes/cbx_test/exec-stream" {
					http.NotFound(w, r)
					return
				}
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatalf("decode exec request: %v", err)
				}
				w.Header().Set("Content-Type", "application/x-ndjson")
				_, _ = io.WriteString(w, `{"type":"complete","exitCode":0}`+"\n")
			}))
			defer server.Close()

			cfg := Config{}
			cfg.CFContainers.APIURL = server.URL
			cfg.CFContainers.Token = "token"
			backend := cfContainersBackend{cfg: cfg, rt: Runtime{HTTP: server.Client(), Stderr: io.Discard}}
			client, err := newCFContainersClient(cfg, backend.rt)
			if err != nil {
				t.Fatal(err)
			}
			if err := backend.prepareWorkspace(context.Background(), client, "cbx_test", "/workspace/repo", tc.deleteContents); err != nil {
				t.Fatal(err)
			}
			hasDelete := strings.Contains(got.Command, "rm -rf")
			if hasDelete != tc.wantDelete {
				t.Fatalf("prepare command = %q, rm -rf presence = %t, want %t", got.Command, hasDelete, tc.wantDelete)
			}
		})
	}
}

func TestCFContainersRemoteDiskCheckRejectsSmallContainer(t *testing.T) {
	var got execStreamRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sandboxes/cbx_test/exec-stream" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode exec request: %v", err)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = io.WriteString(w, `{"type":"stdout","data":"1048576 /workspace/repo\n"}`+"\n")
		_, _ = io.WriteString(w, `{"type":"complete","exitCode":0}`+"\n")
	}))
	defer server.Close()

	cfg := Config{}
	cfg.CFContainers.APIURL = server.URL
	cfg.CFContainers.Token = "token"
	backend := cfContainersBackend{cfg: cfg, rt: Runtime{HTTP: server.Client(), Stderr: io.Discard}}
	client, err := newCFContainersClient(cfg, backend.rt)
	if err != nil {
		t.Fatal(err)
	}
	err = backend.checkRemoteDiskForSync(context.Background(), client, "cbx_test", "/workspace/repo", 2<<20, 1<<20)
	if err == nil {
		t.Fatal("expected disk check to reject sync")
	}
	if !strings.Contains(err.Error(), "remote disk too small for sync") {
		t.Fatalf("error = %v, want remote disk message", err)
	}
	if !strings.Contains(got.Command, "df -B1") {
		t.Fatalf("disk check command = %q, want df probe", got.Command)
	}
}

func TestCFContainersAliasRejectsResourceFlags(t *testing.T) {
	cfg := Config{Provider: cloudflareContainerName}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	_ = fs.String("class", "", "")
	values := RegisterCFContainersProviderFlags(fs, cfg)
	if err := fs.Parse([]string{"--class", "standard"}); err != nil {
		t.Fatal(err)
	}
	if err := ApplyCFContainersProviderFlags(&cfg, fs, values); err == nil {
		t.Fatal("expected legacy provider alias to reject --class")
	}
}

func TestCFContainersClientExecStream(t *testing.T) {
	var token string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"type":"heartbeat"}` + "\n"))
		_, _ = w.Write([]byte(`{"type":"stdout","data":"hello\n"}` + "\n"))
		_, _ = w.Write([]byte(`{"type":"stderr","data":"warn\n"}` + "\n"))
		_, _ = w.Write([]byte(`{"type":"complete","exitCode":7}` + "\n"))
	}))
	defer server.Close()

	token = "test-token"
	cfg := Config{}
	cfg.CFContainers.APIURL = server.URL
	cfg.CFContainers.Token = token
	client, err := newCFContainersClient(cfg, Runtime{HTTP: server.Client()})
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code, err := client.execStream(context.Background(), "cbx_test", execStreamRequest{Command: "true"}, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if code != 7 {
		t.Fatalf("exit code = %d, want 7", code)
	}
	if stdout.String() != "hello\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.String() != "warn\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestCFContainersClientRejectsPlainHTTPExceptLoopback(t *testing.T) {
	for _, tc := range []struct {
		name    string
		apiURL  string
		wantErr bool
	}{
		{name: "https", apiURL: "https://runner.example.test", wantErr: false},
		{name: "loopback", apiURL: "http://127.0.0.1:8787", wantErr: false},
		{name: "localhost", apiURL: "http://localhost:8787", wantErr: false},
		{name: "remote http", apiURL: "http://runner.example.test", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{}
			cfg.CFContainers.APIURL = tc.apiURL
			cfg.CFContainers.Token = "token"
			_, err := newCFContainersClient(cfg, Runtime{})
			if tc.wantErr && err == nil {
				t.Fatal("expected URL validation error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected URL validation error: %v", err)
			}
		})
	}
}

func TestDurationCeil(t *testing.T) {
	if got := durationMillisecondsCeil(1500 * time.Microsecond); got != 2 {
		t.Fatalf("durationMillisecondsCeil = %d, want 2", got)
	}
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func TestCFContainersStatusPrunesExpiredClaim(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sandboxes/cbx_expired" {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprint(w, `{"id":"cbx_expired","state":"expired","workdir":"/workspace/repo"}`)
	}))
	defer server.Close()

	if err := claimLeaseForRepoProvider("cbx_expired", "blue-lobster", cloudflareContainerName, t.TempDir(), time.Hour, false); err != nil {
		t.Fatal(err)
	}
	backend := cfContainersBackend{
		cfg: Config{
			Provider: providerName,
			CFContainers: CFContainersConfig{
				APIURL: server.URL,
				Token:  "token",
			},
		},
		rt: Runtime{HTTP: server.Client()},
	}
	view, err := backend.Status(context.Background(), StatusRequest{ID: "blue-lobster"})
	if err != nil {
		t.Fatal(err)
	}
	if view.State != "expired" {
		t.Fatalf("state = %q, want expired", view.State)
	}
	if _, ok, err := resolveLeaseClaimForProvider("blue-lobster", cloudflareContainerName); err != nil || ok {
		t.Fatalf("claim resolved after expired status ok=%t err=%v", ok, err)
	}
}

func TestCFContainersCleanupPrunesTerminalClaims(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sandboxes/cbx_expired":
			_, _ = fmt.Fprint(w, `{"id":"cbx_expired","state":"expired","workdir":"/workspace/repo"}`)
		case "/v1/sandboxes/cbx_running":
			_, _ = fmt.Fprint(w, `{"id":"cbx_running","state":"running","workdir":"/workspace/repo"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	repo := t.TempDir()
	if err := claimLeaseForRepoProvider("cbx_expired", "blue-lobster", cloudflareContainerName, repo, time.Hour, false); err != nil {
		t.Fatal(err)
	}
	if err := claimLeaseForRepoProvider("cbx_running", "green-lobster", providerName, repo, time.Hour, false); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	backend := cfContainersBackend{
		cfg: Config{
			Provider: providerName,
			CFContainers: CFContainersConfig{
				APIURL: server.URL,
				Token:  "token",
			},
		},
		rt: Runtime{HTTP: server.Client(), Stdout: &stdout},
	}
	if err := backend.Cleanup(context.Background(), CleanupRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := resolveLeaseClaimForProvider("blue-lobster", cloudflareContainerName); err != nil || ok {
		t.Fatalf("expired claim resolved after cleanup ok=%t err=%v", ok, err)
	}
	if _, ok, err := resolveLeaseClaimForProvider("green-lobster", providerName); err != nil || !ok {
		t.Fatalf("running claim missing after cleanup ok=%t err=%v", ok, err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("removed=1 checked=2")) {
		t.Fatalf("cleanup output = %q, want removed summary", stdout.String())
	}
}
