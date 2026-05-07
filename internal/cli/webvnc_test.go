package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

func TestWebVNCURLs(t *testing.T) {
	if got := webVNCAgentURL("https://crabbox.openclaw.ai", "cbx_abcdef123456"); got != "wss://crabbox.openclaw.ai/v1/leases/cbx_abcdef123456/webvnc/agent" {
		t.Fatalf("agent URL=%q", got)
	}
	if got := webVNCAgentURLWithTicket("https://crabbox.openclaw.ai", "cbx_abcdef123456", "wvnc_abc"); got != "wss://crabbox.openclaw.ai/v1/leases/cbx_abcdef123456/webvnc/agent?ticket=wvnc_abc" {
		t.Fatalf("agent fallback URL=%q", got)
	}
	if got := webVNCPortalURL("https://crabbox.openclaw.ai/", "cbx_abcdef123456", "", "secret value"); got != "https://crabbox.openclaw.ai/portal/leases/cbx_abcdef123456/vnc#password=secret+value" {
		t.Fatalf("portal URL=%q", got)
	}
	if got := webVNCPortalURL("https://crabbox.openclaw.ai/", "cbx_abcdef123456", "ec2-user", "secret value"); got != "https://crabbox.openclaw.ai/portal/leases/cbx_abcdef123456/vnc#password=secret+value&username=ec2-user" {
		t.Fatalf("portal URL=%q", got)
	}
	if got := webVNCPortalURL("https://crabbox.openclaw.ai/", "cbx_abcdef123456", "", "Cb1!abc"); got != "https://crabbox.openclaw.ai/portal/leases/cbx_abcdef123456/vnc#password=Cb1%21abc" {
		t.Fatalf("portal URL=%q", got)
	}
	if got := webVNCPortalURL("https://crabbox.openclaw.ai/#stale", "cbx_abcdef123456", "", ""); got != "https://crabbox.openclaw.ai/portal/leases/cbx_abcdef123456/vnc" {
		t.Fatalf("portal URL=%q", got)
	}
	got := webVNCPortalURL("https://crabbox.openclaw.ai/", "cbx_abcdef123456", "", "JVS/yMb%2B")
	if got != "https://crabbox.openclaw.ai/portal/leases/cbx_abcdef123456/vnc#password=JVS%2FyMb%252B" {
		t.Fatalf("portal URL with escaped password=%q", got)
	}
	fragment, ok := strings.CutPrefix(got, "https://crabbox.openclaw.ai/portal/leases/cbx_abcdef123456/vnc#")
	if !ok {
		t.Fatalf("portal URL missing expected fragment: %q", got)
	}
	values, err := url.ParseQuery(fragment)
	if err != nil {
		t.Fatal(err)
	}
	if values.Get("password") != "JVS/yMb%2B" {
		t.Fatalf("decoded portal password=%q", values.Get("password"))
	}
}

func TestConnectWebVNCBridgeRegistersAgentBeforeServe(t *testing.T) {
	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcpListener.Close()
	go func() {
		conn, err := tcpListener.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	agentConnected := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/leases/cbx_abcdef123456/webvnc/ticket":
			if r.Method != http.MethodPost {
				t.Errorf("ticket method=%s", r.Method)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Errorf("authorization=%q", got)
			}
			_ = json.NewEncoder(w).Encode(CoordinatorWebVNCTicket{
				Ticket:  "wvnc_abcdef1234567890abcdef1234567890",
				LeaseID: "cbx_abcdef123456",
			})
		case "/v1/leases/cbx_abcdef123456/webvnc/agent":
			if got := r.URL.Query().Get("ticket"); got != "" {
				t.Errorf("query ticket=%q", got)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer wvnc_abcdef1234567890abcdef1234567890" {
				t.Errorf("bridge authorization=%q", got)
			}
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				t.Errorf("websocket accept: %v", err)
				return
			}
			close(agentConnected)
			_, _, _ = conn.Read(context.Background())
			_ = conn.Close(websocket.StatusNormalClosure, "test done")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, port, err := net.SplitHostPort(tcpListener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	coord := &CoordinatorClient{BaseURL: server.URL, Token: "test-token", Client: server.Client()}
	bridge, err := connectWebVNCBridge(ctx, coord, "cbx_abcdef123456", "127.0.0.1", port)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()

	select {
	case <-agentConnected:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestRetryableWebVNCBridgeErrors(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		retryable bool
	}{
		{
			name:      "viewer disconnected",
			err:       errors.New(`failed to get reader: received close frame: status = StatusInternalError and reason = "WebVNC viewer disconnected"`),
			retryable: true,
		},
		{
			name:      "newer viewer",
			err:       errors.New(`received close frame: status = StatusServiceRestart and reason = "replaced by a newer WebVNC viewer"`),
			retryable: true,
		},
		{
			name:      "websocket eof",
			err:       errors.New(`failed to get reader: failed to read frame header: EOF`),
			retryable: true,
		},
		{
			name:      "normal close",
			err:       errors.New(`received close frame: status = StatusNormalClosure and reason = "test done"`),
			retryable: true,
		},
		{
			name:      "nil",
			err:       nil,
			retryable: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := retryableWebVNCBridgeError(tc.err); got != tc.retryable {
				t.Fatalf("retryable=%v, want %v", got, tc.retryable)
			}
		})
	}
}

func TestRetryBridgeTicketInQuery(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusUnauthorized,
		Body:       io.NopCloser(strings.NewReader("old broker needs query ticket")),
	}
	if !retryBridgeTicketInQuery(resp, errors.New("websocket rejected")) {
		t.Fatal("expected unauthorized websocket response to retry with query ticket")
	}
	if retryBridgeTicketInQuery(&http.Response{StatusCode: http.StatusForbidden}, errors.New("forbidden")) {
		t.Fatal("forbidden response should not retry with query ticket")
	}
	if retryBridgeTicketInQuery(resp, nil) {
		t.Fatal("successful dial should not retry with query ticket")
	}
}

func TestWebVNCDaemonArgsStripBackgroundFlags(t *testing.T) {
	got := strings.Join(stripWebVNCDaemonFlags([]string{
		"--provider",
		"aws",
		"--daemon",
		"--target",
		"linux",
		"--background=true",
		"--id",
		"pearl-krill",
		"--open",
	}), " ")
	if got != "--provider aws --target linux --id pearl-krill --open" {
		t.Fatalf("stripped args=%q", got)
	}
}

func TestWebVNCDaemonSupervisorRestartsWithoutReopeningPortal(t *testing.T) {
	got := webVNCDaemonSupervisorScript("/tmp/crabbox", []string{
		"webvnc",
		"--provider",
		"hetzner",
		"--id",
		"pearl-krill",
		"--open",
	})
	if !strings.Contains(got, "/tmp/crabbox' 'webvnc' '--provider' 'hetzner' '--id' 'pearl-krill' '--open'") {
		t.Fatalf("first daemon command missing --open: %s", got)
	}
	if !strings.Contains(got, "/tmp/crabbox' 'webvnc' '--provider' 'hetzner' '--id' 'pearl-krill'\n") {
		t.Fatalf("restart daemon command should strip --open: %s", got)
	}
	if strings.Count(got, "--open") != 1 {
		t.Fatalf("daemon supervisor should only open portal once: %s", got)
	}
	if !strings.Contains(got, "webvnc daemon supervisor: child exited code=$code; restarting in 1s") {
		t.Fatalf("daemon supervisor missing restart log: %s", got)
	}
}

func TestSafeWebVNCDaemonName(t *testing.T) {
	if got := safeWebVNCDaemonName("pearl/krill :99"); got != "pearl_krill__99" {
		t.Fatalf("safe daemon name=%q", got)
	}
}

func TestReadWebVNCDaemonPID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bridge.pid")
	if err := os.WriteFile(path, []byte("12345\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readWebVNCDaemonPID(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != 12345 {
		t.Fatalf("pid=%d", got)
	}
}

func TestIsWebVNCDaemonCommand(t *testing.T) {
	if !isWebVNCDaemonCommand("/usr/local/bin/crabbox webvnc --id pearl-krill") {
		t.Fatal("expected crabbox webvnc command")
	}
	if isWebVNCDaemonCommand("/bin/sleep 999") {
		t.Fatal("sleep must not be treated as WebVNC daemon")
	}
}
