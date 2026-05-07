package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"nhooyr.io/websocket"
)

func (a App) webvnc(ctx context.Context, args []string) error {
	defaults := defaultConfig()
	fs := newFlagSet("webvnc", a.Stderr)
	provider := fs.String("provider", defaults.Provider, "provider: hetzner or aws")
	id := fs.String("id", "", "lease id or slug")
	reclaim := fs.Bool("reclaim", false, "claim this lease for the current repo")
	localPort := fs.String("local-port", "", "local VNC tunnel port")
	openPortal := fs.Bool("open", false, "open the web portal VNC page")
	daemon := fs.Bool("daemon", false, "start the WebVNC bridge in the background")
	background := fs.Bool("background", false, "alias for --daemon")
	daemonStatus := fs.Bool("status", false, "show WebVNC background bridge pid/log paths")
	stopDaemon := fs.Bool("stop", false, "stop the WebVNC background bridge for this lease")
	networkFlags := registerNetworkModeFlag(fs, defaults)
	targetFlags := registerTargetFlags(fs, defaults)
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	setIDFromFirstArg(fs, id)
	if *id == "" {
		return exit(2, "usage: crabbox webvnc --id <lease-id-or-slug>")
	}
	if *daemonStatus {
		return a.webVNCDaemonStatus(*id)
	}
	if *stopDaemon {
		return a.stopWebVNCDaemon(*id)
	}
	if *daemon || *background {
		return a.startWebVNCDaemon(args, *id)
	}
	cfg, err := loadLeaseTargetConfig(fs, *provider, targetFlags, networkFlags, leaseTargetConfigOptions{Desktop: true})
	if err != nil {
		return err
	}
	if isBlacksmithProvider(cfg.Provider) || isStaticProvider(cfg.Provider) {
		return exit(2, "webvnc currently supports coordinator-backed hetzner/aws desktop leases")
	}
	coord, useCoordinator, err := newTargetCoordinatorClient(cfg)
	if err != nil {
		return err
	}
	if !useCoordinator || coord == nil || coord.Token == "" {
		return exit(2, "webvnc requires a configured coordinator login; run crabbox login first")
	}
	server, target, leaseID, err := a.resolveNetworkLeaseTarget(ctx, cfg, *id, false)
	if err != nil {
		return err
	}
	if err := enforceManagedLeaseCapabilities(cfg, server, leaseID); err != nil {
		return err
	}
	if err := a.claimAndTouchLeaseTarget(ctx, cfg, server, leaseID, *reclaim); err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "lease: %s slug=%s provider=%s target=%s\n", leaseID, blank(serverSlug(server), "-"), blank(server.Provider, cfg.Provider), blank(target.TargetOS, cfg.TargetOS))
	fmt.Fprintln(a.Stdout, "bridge: probing VNC on target loopback 127.0.0.1:5900 over SSH")
	endpoint, err := resolveVNCEndpoint(ctx, cfg, &target)
	if err != nil {
		return err
	}
	if *localPort == "" {
		*localPort = availableLocalVNCPort()
	}
	password := ""
	if endpoint.Managed {
		password, _ = runSSHOutput(ctx, target, vncPasswordCommand(target))
	}
	username := ""
	if endpoint.Managed && target.TargetOS == targetMacOS {
		username = target.User
	}

	connHost := endpoint.Host
	connPort := endpoint.Port
	var tunnel *exec.Cmd
	if !endpoint.Direct {
		fmt.Fprintf(a.Stdout, "bridge: starting SSH tunnel localhost:%s -> %s:%s\n", *localPort, endpoint.Host, endpoint.Port)
		tunnel, err = startVNCForegroundTunnel(ctx, target, *localPort, endpoint.Host, endpoint.Port)
		if err != nil {
			return err
		}
		defer stopProcess(tunnel)
		connHost = "127.0.0.1"
		connPort = *localPort
	}

	portal := webVNCPortalURL(coord.BaseURL, leaseID, username, password)
	opened := false
	connectedOnce := false
	attempt := 0
	for {
		bridge, err := connectWebVNCBridge(ctx, coord, leaseID, connHost, connPort)
		if err != nil {
			if !connectedOnce {
				return err
			}
			attempt++
			delay := webVNCReconnectDelay(attempt)
			fmt.Fprintf(a.Stdout, "bridge: reconnect failed: %v; retrying in %s\n", err, delay)
			if err := waitWebVNCReconnect(ctx, delay); err != nil {
				return err
			}
			continue
		}
		connectedOnce = true
		if attempt == 0 {
			fmt.Fprintln(a.Stdout, "bridge: connected; keep this process running while using WebVNC")
			fmt.Fprintf(a.Stdout, "webvnc: %s\n", portal)
			if strings.TrimSpace(password) != "" {
				fmt.Fprintf(a.Stdout, "password: %s\n", strings.TrimSpace(password))
				if strings.TrimSpace(username) != "" {
					fmt.Fprintf(a.Stdout, "username: %s\n", strings.TrimSpace(username))
				}
			}
		} else {
			fmt.Fprintf(a.Stdout, "bridge: reconnected after viewer reset (attempt %d)\n", attempt+1)
		}
		if *openPortal && !opened {
			if err := openLocalURL(portal); err != nil {
				bridge.Close()
				return err
			}
			opened = true
			fmt.Fprintf(a.Stdout, "opened: %s\n", portal)
		}
		err = bridge.Serve(ctx)
		if !retryableWebVNCBridgeError(err) {
			return err
		}
		attempt++
		delay := webVNCReconnectDelay(attempt)
		if err != nil {
			fmt.Fprintf(a.Stdout, "bridge: viewer reset: %v; reconnecting in %s\n", err, delay)
		} else {
			fmt.Fprintf(a.Stdout, "bridge: viewer closed; reconnecting in %s\n", delay)
		}
		if err := waitWebVNCReconnect(ctx, delay); err != nil {
			return err
		}
	}
}

func (a App) startWebVNCDaemon(args []string, leaseID string) error {
	exe, err := os.Executable()
	if err != nil {
		return exit(2, "resolve crabbox executable: %v", err)
	}
	logPath, pidPath, err := webVNCDaemonPaths(leaseID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return exit(2, "create WebVNC daemon directory: %v", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return exit(2, "open WebVNC daemon log: %v", err)
	}
	defer logFile.Close()
	childArgs := append([]string{"webvnc"}, stripWebVNCDaemonFlags(args)...)
	cmd := exec.Command("sh", "-c", webVNCDaemonSupervisorScript(exe, childArgs))
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	configureDaemonCommand(cmd)
	if err := cmd.Start(); err != nil {
		return exit(5, "start WebVNC daemon: %v", err)
	}
	pid := cmd.Process.Pid
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", pid)), 0o600); err != nil {
		_ = cmd.Process.Kill()
		return exit(2, "write WebVNC daemon pid: %v", err)
	}
	if err := cmd.Process.Release(); err != nil {
		return exit(5, "release WebVNC daemon process: %v", err)
	}
	fmt.Fprintf(a.Stdout, "webvnc daemon: pid=%d log=%s\n", pid, logPath)
	fmt.Fprintln(a.Stdout, "webvnc daemon: stop with crabbox webvnc --id <lease-id-or-slug> --stop")
	return nil
}

func webVNCDaemonSupervisorScript(exe string, args []string) string {
	firstArgs := make([]string, 0, len(args)+1)
	firstArgs = append(firstArgs, shellQuote(exe))
	for _, arg := range args {
		firstArgs = append(firstArgs, shellQuote(arg))
	}
	restartArgs := make([]string, 0, len(args)+1)
	restartArgs = append(restartArgs, shellQuote(exe))
	for _, arg := range stripWebVNCOpenFlags(args) {
		restartArgs = append(restartArgs, shellQuote(arg))
	}
	return "set -u\n" +
		"echo 'webvnc daemon supervisor: starting'\n" +
		"first=1\n" +
		"while :; do\n" +
		"  if [ \"$first\" = 1 ]; then\n" +
		"    " + strings.Join(firstArgs, " ") + "\n" +
		"    first=0\n" +
		"  else\n" +
		"    " + strings.Join(restartArgs, " ") + "\n" +
		"  fi\n" +
		"  code=$?\n" +
		"  echo \"webvnc daemon supervisor: child exited code=$code; restarting in 1s\"\n" +
		"  sleep 1\n" +
		"done\n"
}

func (a App) webVNCDaemonStatus(leaseID string) error {
	logPath, pidPath, err := webVNCDaemonPaths(leaseID)
	if err != nil {
		return err
	}
	pid, err := readWebVNCDaemonPID(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(a.Stdout, "webvnc daemon: no pid file for %s\n", leaseID)
			fmt.Fprintf(a.Stdout, "webvnc daemon: expected log=%s\n", logPath)
			return nil
		}
		return err
	}
	command, alive := webVNCDaemonProcessCommand(pid)
	if !alive {
		fmt.Fprintf(a.Stdout, "webvnc daemon: stale pid=%d log=%s\n", pid, logPath)
		return nil
	}
	fmt.Fprintf(a.Stdout, "webvnc daemon: pid=%d log=%s\n", pid, logPath)
	if strings.TrimSpace(command) != "" {
		fmt.Fprintf(a.Stdout, "webvnc daemon: command=%s\n", strings.TrimSpace(command))
	}
	return nil
}

func (a App) stopWebVNCDaemon(leaseID string) error {
	_, pidPath, err := webVNCDaemonPaths(leaseID)
	if err != nil {
		return err
	}
	pid, err := readWebVNCDaemonPID(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(a.Stdout, "webvnc daemon: no pid file for %s\n", leaseID)
			return nil
		}
		return err
	}
	command, alive := webVNCDaemonProcessCommand(pid)
	if !alive {
		_ = os.Remove(pidPath)
		fmt.Fprintf(a.Stdout, "webvnc daemon: removed stale pid=%d\n", pid)
		return nil
	}
	if !isWebVNCDaemonCommand(command) {
		return exit(5, "refusing to stop pid %d; command does not look like crabbox webvnc: %s", pid, strings.TrimSpace(command))
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return exit(5, "find WebVNC daemon pid %d: %v", pid, err)
	}
	if err := stopDaemonProcess(process, pid); err != nil {
		return exit(5, "stop WebVNC daemon pid %d: %v", pid, err)
	}
	_ = os.Remove(pidPath)
	fmt.Fprintf(a.Stdout, "webvnc daemon: stopped pid=%d\n", pid)
	return nil
}

func webVNCDaemonProcessCommand(pid int) (string, bool) {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil {
		return "", false
	}
	command := strings.TrimSpace(string(out))
	return command, command != ""
}

func isWebVNCDaemonCommand(command string) bool {
	command = strings.ToLower(command)
	return strings.Contains(command, "crabbox") && strings.Contains(command, "webvnc")
}

func readWebVNCDaemonPID(pidPath string) (int, error) {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, exit(2, "invalid WebVNC daemon pid file %s", pidPath)
	}
	return pid, nil
}

func stripWebVNCDaemonFlags(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--daemon" || arg == "--background" ||
			strings.HasPrefix(arg, "--daemon=") || strings.HasPrefix(arg, "--background=") {
			continue
		}
		out = append(out, arg)
	}
	return out
}

func stripWebVNCOpenFlags(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--open" || strings.HasPrefix(arg, "--open=") {
			continue
		}
		out = append(out, arg)
	}
	return out
}

func webVNCDaemonPaths(leaseID string) (string, string, error) {
	dir, err := crabboxStateDir()
	if err != nil {
		return "", "", err
	}
	name := safeWebVNCDaemonName(leaseID)
	bridgeDir := filepath.Join(dir, "webvnc")
	return filepath.Join(bridgeDir, name+".log"), filepath.Join(bridgeDir, name+".pid"), nil
}

func safeWebVNCDaemonName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "bridge"
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func startVNCForegroundTunnel(ctx context.Context, target SSHTarget, localPort, remoteHost, remotePort string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, "ssh", vncTunnelArgs(target, localPort, remoteHost, remotePort)...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		_ = cmd.Wait()
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			stopProcess(cmd)
			return nil, context.Cause(ctx)
		}
		if tcpReachable(ctx, "127.0.0.1", localPort, 200*time.Millisecond) {
			return cmd, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	stopProcess(cmd)
	return nil, exit(5, "timed out starting VNC SSH tunnel on localhost:%s", localPort)
}

type webVNCBridge struct {
	tcp net.Conn
	ws  *websocket.Conn
}

func connectWebVNCBridge(ctx context.Context, coord *CoordinatorClient, leaseID, host, port string) (*webVNCBridge, error) {
	tcp, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return nil, err
	}
	ticket, err := coord.CreateWebVNCTicket(ctx, leaseID)
	if err != nil {
		_ = tcp.Close()
		return nil, err
	}
	ws, resp, err := websocket.Dial(ctx, webVNCAgentURL(coord.BaseURL, leaseID), &websocket.DialOptions{
		HTTPHeader: bridgeTicketHeaders(coord, ticket.Ticket),
	})
	if retryBridgeTicketInQuery(resp, err) {
		ws, _, err = websocket.Dial(ctx, webVNCAgentURLWithTicket(coord.BaseURL, leaseID, ticket.Ticket), &websocket.DialOptions{
			HTTPHeader: coord.webVNCAccessHeaders(),
		})
	}
	if err != nil {
		_ = tcp.Close()
		return nil, err
	}
	return &webVNCBridge{tcp: tcp, ws: ws}, nil
}

func (b *webVNCBridge) Serve(ctx context.Context) error {
	defer b.Close()
	errc := make(chan error, 2)
	go func() { errc <- copyWebSocketToTCP(ctx, b.ws, b.tcp) }()
	go func() { errc <- copyTCPToWebSocket(ctx, b.ws, b.tcp) }()
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case err := <-errc:
		if err == nil || strings.Contains(err.Error(), "status = StatusNormalClosure") {
			return nil
		}
		return err
	}
}

func retryableWebVNCBridgeError(err error) bool {
	if err == nil {
		return true
	}
	message := err.Error()
	return strings.Contains(message, "WebVNC viewer disconnected") ||
		strings.Contains(message, "replaced by a newer WebVNC viewer") ||
		strings.Contains(message, "WebVNC bridge reset") ||
		strings.Contains(message, "failed to read frame header: EOF") ||
		strings.Contains(message, "status = StatusNormalClosure")
}

func webVNCReconnectDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Duration(attempt) * 500 * time.Millisecond
	if delay > 5*time.Second {
		return 5 * time.Second
	}
	return delay
}

func waitWebVNCReconnect(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-timer.C:
		return nil
	}
}

func (b *webVNCBridge) Close() {
	if b == nil {
		return
	}
	if b.ws != nil {
		_ = b.ws.Close(websocket.StatusNormalClosure, "bridge stopped")
	}
	if b.tcp != nil {
		_ = b.tcp.Close()
	}
}

func copyWebSocketToTCP(ctx context.Context, ws *websocket.Conn, tcp net.Conn) error {
	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			return err
		}
		if _, err := tcp.Write(data); err != nil {
			return err
		}
	}
}

func copyTCPToWebSocket(ctx context.Context, ws *websocket.Conn, tcp net.Conn) error {
	buf := make([]byte, 32*1024)
	for {
		n, err := tcp.Read(buf)
		if n > 0 {
			if writeErr := ws.Write(ctx, websocket.MessageBinary, buf[:n]); writeErr != nil {
				return writeErr
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func (c *CoordinatorClient) webVNCAccessHeaders() http.Header {
	header := http.Header{}
	c.addAccessHeaders(header)
	return header
}

func bridgeTicketHeaders(coord *CoordinatorClient, ticket string) http.Header {
	headers := coord.webVNCAccessHeaders()
	headers.Set("Authorization", "Bearer "+ticket)
	return headers
}

func retryBridgeTicketInQuery(resp *http.Response, err error) bool {
	if err == nil || resp == nil {
		return false
	}
	if resp.Body != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	return resp.StatusCode == http.StatusUnauthorized
}

func webVNCAgentURL(base, leaseID string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/leases/" + url.PathEscape(leaseID) + "/webvnc/agent"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func webVNCAgentURLWithTicket(base, leaseID, ticket string) string {
	u, err := url.Parse(webVNCAgentURL(base, leaseID))
	if err != nil {
		return base
	}
	values := url.Values{}
	values.Set("ticket", ticket)
	u.RawQuery = values.Encode()
	u.Fragment = ""
	return u.String()
}

func webVNCPortalURL(base, leaseID, username, password string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/portal/leases/" + url.PathEscape(leaseID) + "/vnc"
	u.RawQuery = ""
	u.Fragment = ""
	u.RawFragment = ""
	if strings.TrimSpace(username) != "" || strings.TrimSpace(password) != "" {
		values := url.Values{}
		if strings.TrimSpace(username) != "" {
			values.Set("username", strings.TrimSpace(username))
		}
		if strings.TrimSpace(password) != "" {
			values.Set("password", strings.TrimSpace(password))
		}
		u.RawFragment = values.Encode()
		if fragment, err := url.PathUnescape(u.RawFragment); err == nil {
			u.Fragment = fragment
		}
	}
	return u.String()
}

func stopProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
