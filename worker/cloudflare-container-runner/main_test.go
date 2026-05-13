package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanAbsolutePath(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "absolute", input: "/workspace/../workspace/repo", want: "/workspace/repo"},
		{name: "empty", input: "", want: ""},
		{name: "relative", input: "workspace/repo", want: ""},
		{name: "nul", input: "/workspace/repo\x00bad", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := cleanAbsolutePath(tc.input); got != tc.want {
				t.Fatalf("cleanAbsolutePath(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestCommandEnvFiltersInvalidNames(t *testing.T) {
	env := commandEnv(map[string]string{
		"GOOD_NAME": "kept",
		"1BAD":      "dropped",
		"BAD-NAME":  "dropped",
	})

	if !containsEnv(env, "GOOD_NAME=kept") {
		t.Fatalf("commandEnv missing allowed variable: %#v", env)
	}
	if containsEnv(env, "1BAD=dropped") || containsEnv(env, "BAD-NAME=dropped") {
		t.Fatalf("commandEnv kept invalid variable name: %#v", env)
	}
}

func TestHandleFileUploadWritesDestination(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "nested", "archive.tgz")
	req := httptest.NewRequest(http.MethodPost, "/v1/files?path="+dst, strings.NewReader("payload"))
	rec := httptest.NewRecorder()

	handleFileUpload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, []byte("payload")) {
		t.Fatalf("uploaded data = %q, want payload", data)
	}
}

func containsEnv(env []string, value string) bool {
	for _, entry := range env {
		if entry == value {
			return true
		}
	}
	return false
}
