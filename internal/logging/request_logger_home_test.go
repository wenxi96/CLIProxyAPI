package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"
)

type stubHomeRequestLogClient struct {
	heartbeatOK bool
	pushed      [][]byte
}

func (c *stubHomeRequestLogClient) HeartbeatOK() bool { return c.heartbeatOK }

func (c *stubHomeRequestLogClient) RPushRequestLog(_ context.Context, payload []byte) error {
	c.pushed = append(c.pushed, bytes.Clone(payload))
	return nil
}

func TestFileRequestLogger_HomeEnabled_ForwardsWhenRequestLogEnabled(t *testing.T) {
	original := currentHomeRequestLogClient
	defer func() {
		currentHomeRequestLogClient = original
	}()

	stub := &stubHomeRequestLogClient{heartbeatOK: true}
	currentHomeRequestLogClient = func() homeRequestLogClient {
		return stub
	}

	logsDir := t.TempDir()
	logger := NewFileRequestLogger(true, logsDir, "", 0)
	logger.SetHomeEnabled(true)

	requestHeaders := map[string][]string{
		"Content-Type":  {"application/json"},
		"Authorization": {"Bearer secret"},
		"Cookie":        {"session=super-secret-cookie"},
		"X-Api-Key":     {"header-api-secret"},
	}

	errLog := logger.LogRequest(
		"/v1/chat/completions?key=query-secret&foo=bar",
		http.MethodPost,
		requestHeaders,
		[]byte(`{"input":"hello","api_key":"body-secret","password":"body-password"}`),
		http.StatusOK,
		map[string][]string{"Content-Type": {"application/json"}},
		[]byte(`{"ok":true,"access_token":"response-secret"}`),
		nil,
		nil,
		nil,
		nil,
		nil,
		"req-1",
		time.Now(),
		time.Now(),
	)
	if errLog != nil {
		t.Fatalf("LogRequest error: %v", errLog)
	}

	entries, errRead := os.ReadDir(logsDir)
	if errRead != nil {
		t.Fatalf("failed to read logs dir: %v", errRead)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no local request log files, got entries: %+v", entries)
	}

	if len(stub.pushed) != 1 {
		t.Fatalf("home pushed records = %d, want 1", len(stub.pushed))
	}

	var got struct {
		Headers    map[string][]string `json:"headers"`
		RequestLog string              `json:"request_log"`
	}
	if errUnmarshal := json.Unmarshal(stub.pushed[0], &got); errUnmarshal != nil {
		t.Fatalf("unmarshal payload: %v payload=%s", errUnmarshal, string(stub.pushed[0]))
	}
	if got.Headers == nil || got.Headers["Content-Type"][0] != "application/json" {
		t.Fatalf("headers.content-type = %+v, want application/json", got.Headers["Content-Type"])
	}
	if got.Headers == nil || got.Headers["Authorization"][0] != "Bearer [REDACTED]" {
		t.Fatalf("headers.authorization = %+v, want Bearer [REDACTED]", got.Headers["Authorization"])
	}
	if got.Headers == nil || got.Headers["Cookie"][0] != "[REDACTED]" {
		t.Fatalf("headers.cookie = %+v, want [REDACTED]", got.Headers["Cookie"])
	}
	if got.Headers == nil || got.Headers["X-Api-Key"][0] != "[REDACTED]" {
		t.Fatalf("headers.x-api-key = %+v, want [REDACTED]", got.Headers["X-Api-Key"])
	}
	if got.RequestLog == "" {
		t.Fatalf("request_log empty, want non-empty")
	}
	for _, secret := range []string{"Bearer secret", "super-secret-cookie", "header-api-secret", "query-secret", "body-secret", "body-password", "response-secret"} {
		if bytes.Contains([]byte(got.RequestLog), []byte(secret)) {
			t.Fatalf("request_log leaked secret %q: %s", secret, got.RequestLog)
		}
	}
	if !bytes.Contains([]byte(got.RequestLog), []byte("[REDACTED]")) {
		t.Fatalf("request_log does not contain redaction marker: %s", got.RequestLog)
	}
}

func TestFileRequestLogger_HomeEnabled_DoesNotForwardForcedErrorLogsWhenRequestLogDisabled(t *testing.T) {
	original := currentHomeRequestLogClient
	defer func() {
		currentHomeRequestLogClient = original
	}()

	stub := &stubHomeRequestLogClient{heartbeatOK: true}
	currentHomeRequestLogClient = func() homeRequestLogClient {
		return stub
	}

	logsDir := t.TempDir()
	logger := NewFileRequestLogger(false, logsDir, "", 0)
	logger.SetHomeEnabled(true)

	errLog := logger.LogRequestWithOptions(
		"/v1/chat/completions",
		http.MethodPost,
		map[string][]string{"Content-Type": {"application/json"}},
		[]byte(`{"input":"hello"}`),
		http.StatusBadGateway,
		map[string][]string{"Content-Type": {"application/json"}},
		[]byte(`{"error":"upstream failure"}`),
		nil,
		nil,
		nil,
		nil,
		nil,
		true,
		"req-2",
		time.Now(),
		time.Now(),
	)
	if errLog != nil {
		t.Fatalf("LogRequestWithOptions error: %v", errLog)
	}

	if len(stub.pushed) != 0 {
		t.Fatalf("home pushed records = %d, want 0", len(stub.pushed))
	}

	entries, errRead := os.ReadDir(logsDir)
	if errRead != nil {
		t.Fatalf("failed to read logs dir: %v", errRead)
	}
	found := false
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if entry.Name() != "" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected local forced error log file when request-log disabled")
	}
}

func TestHomeStreamingLogWriterCloseReleasesResourcesWhenHomeUnavailable(t *testing.T) {
	original := currentHomeRequestLogClient
	defer func() {
		currentHomeRequestLogClient = original
	}()

	currentHomeRequestLogClient = func() homeRequestLogClient {
		return &stubHomeRequestLogClient{heartbeatOK: false}
	}

	writer := newHomeStreamingLogWriter(
		"/v1/responses?auth_token=query-secret",
		http.MethodPost,
		map[string][]string{"Authorization": {"Bearer stream-secret"}},
		[]byte(`{"api_key":"body-secret"}`),
		"req-stream",
	)
	writer.WriteChunkAsync([]byte(`{"access_token":"response-secret"}`))

	done := make(chan error, 1)
	go func() {
		done <- writer.Close()
	}()

	select {
	case errClose := <-done:
		if errClose != nil {
			t.Fatalf("Close error: %v", errClose)
		}
	case <-time.After(time.Second):
		t.Fatal("Close timed out")
	}

	if writer.chunkChan != nil {
		t.Fatal("chunkChan not cleared after Close")
	}
}
