package cloudflaresandbox

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

type cloudflareSandboxClient struct {
	baseURL string
	token   string
	http    *http.Client
}

type cloudflareSandbox struct {
	ID        string            `json:"id"`
	State     string            `json:"state"`
	Workdir   string            `json:"workdir"`
	Labels    map[string]string `json:"labels,omitempty"`
	CreatedAt string            `json:"createdAt,omitempty"`
}

type createSandboxRequest struct {
	ID                 string            `json:"id"`
	LeaseID            string            `json:"leaseId"`
	Slug               string            `json:"slug"`
	Repo               string            `json:"repo,omitempty"`
	Workdir            string            `json:"workdir"`
	TTLSeconds         int               `json:"ttlSeconds,omitempty"`
	IdleTimeoutSeconds int               `json:"idleTimeoutSeconds,omitempty"`
	Labels             map[string]string `json:"labels,omitempty"`
}

type execStreamRequest struct {
	Command   string            `json:"command"`
	Cwd       string            `json:"cwd,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	TimeoutMS int64             `json:"timeoutMs,omitempty"`
}

type execStreamEvent struct {
	Type     string `json:"type"`
	Data     string `json:"data,omitempty"`
	Error    string `json:"error,omitempty"`
	ExitCode *int   `json:"exitCode,omitempty"`
}

func newCloudflareSandboxClient(cfg Config, rt Runtime) (*cloudflareSandboxClient, error) {
	apiURL := strings.TrimSpace(cfg.CloudflareSandbox.APIURL)
	if apiURL == "" {
		return nil, exit(2, "cloudflare-sandbox requires --cloudflare-sandbox-url or CRABBOX_CLOUDFLARE_SANDBOX_URL")
	}
	token := strings.TrimSpace(cfg.CloudflareSandbox.Token)
	if token == "" {
		return nil, exit(2, "cloudflare-sandbox requires --cloudflare-sandbox-token or CRABBOX_CLOUDFLARE_SANDBOX_TOKEN")
	}
	parsed, err := url.Parse(apiURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, exit(2, "cloudflare-sandbox url %q is invalid", apiURL)
	}
	httpClient := rt.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &cloudflareSandboxClient{
		baseURL: strings.TrimRight(apiURL, "/"),
		token:   token,
		http:    httpClient,
	}, nil
}

func (c *cloudflareSandboxClient) createSandbox(ctx context.Context, req createSandboxRequest) (cloudflareSandbox, error) {
	var sandbox cloudflareSandbox
	err := c.doJSON(ctx, http.MethodPost, "/v1/sandboxes", req, &sandbox)
	return sandbox, err
}

func (c *cloudflareSandboxClient) getSandbox(ctx context.Context, sandboxID string) (cloudflareSandbox, error) {
	var sandbox cloudflareSandbox
	err := c.doJSON(ctx, http.MethodGet, "/v1/sandboxes/"+url.PathEscape(sandboxID), nil, &sandbox)
	return sandbox, err
}

func (c *cloudflareSandboxClient) destroySandbox(ctx context.Context, sandboxID string) error {
	return c.doJSON(ctx, http.MethodDelete, "/v1/sandboxes/"+url.PathEscape(sandboxID), nil, nil)
}

func (c *cloudflareSandboxClient) uploadFile(ctx context.Context, sandboxID, localPath, remotePath string) error {
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open upload file: %w", err)
	}
	defer file.Close()
	endpoint := "/v1/sandboxes/" + url.PathEscape(sandboxID) + "/files?path=" + url.QueryEscape(remotePath)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+endpoint, file)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	httpReq.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.responseError(resp)
	}
	return nil
}

func (c *cloudflareSandboxClient) execStream(ctx context.Context, sandboxID string, req execStreamRequest, stdout, stderr io.Writer) (int, error) {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(req); err != nil {
		return 0, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/sandboxes/"+url.PathEscape(sandboxID)+"/exec-stream", &body)
	if err != nil {
		return 0, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, c.responseError(resp)
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mediaType != "" && mediaType != "application/x-ndjson" && mediaType != "application/jsonl" {
		return 0, fmt.Errorf("unexpected cloudflare-sandbox stream content-type %q", resp.Header.Get("Content-Type"))
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	exitCode := 0
	completed := false
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event execStreamEvent
		if err := json.Unmarshal(line, &event); err != nil {
			return exitCode, fmt.Errorf("decode cloudflare-sandbox stream event: %w", err)
		}
		switch event.Type {
		case "stdout":
			if stdout != nil {
				_, _ = io.WriteString(stdout, event.Data)
			}
		case "stderr":
			if stderr != nil {
				_, _ = io.WriteString(stderr, event.Data)
			}
		case "complete":
			completed = true
			if event.ExitCode != nil {
				exitCode = *event.ExitCode
			}
			return exitCode, nil
		case "error":
			if event.Error == "" {
				event.Error = "stream error"
			}
			return exitCode, errors.New(event.Error)
		case "start":
		default:
			return exitCode, fmt.Errorf("unknown cloudflare-sandbox stream event %q", event.Type)
		}
	}
	if err := scanner.Err(); err != nil {
		return exitCode, err
	}
	if !completed {
		return exitCode, fmt.Errorf("cloudflare-sandbox stream ended before completion")
	}
	return exitCode, nil
}

func (c *cloudflareSandboxClient) doJSON(ctx context.Context, method, endpoint string, input any, output any) error {
	var body io.Reader
	if input != nil {
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(input); err != nil {
			return err
		}
		body = &buf
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.responseError(resp)
	}
	if output == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(output)
}

func (c *cloudflareSandboxClient) responseError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && strings.TrimSpace(payload.Error) != "" {
		return fmt.Errorf("cloudflare-sandbox API %s: %s", resp.Status, payload.Error)
	}
	text := strings.TrimSpace(string(body))
	if text == "" {
		text = resp.Status
	}
	return fmt.Errorf("cloudflare-sandbox API %s: %s", resp.Status, text)
}

func remoteArchivePath() string {
	return path.Join("/tmp", "crabbox-cloudflare-sync-"+time.Now().UTC().Format("20060102150405.000000000")+".tgz")
}
