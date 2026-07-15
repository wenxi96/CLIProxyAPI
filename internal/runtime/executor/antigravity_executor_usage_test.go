package executor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

func TestAntigravityClaudeNonStreamPreservesFilteredStreamUsage(t *testing.T) {
	const model = "claude-antigravity-buffered-usage-test"
	records := make(chan usage.Record, 2)
	usage.RegisterNamedPlugin("antigravity-buffered-usage-test", antigravityUsageCapturePlugin{records: records})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"ok\"}]}}]},\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":3,\"totalTokenCount\":10}}\n\n"))
		_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"finishReason\":\"STOP\"}]}}\n\n"))
	}))
	defer server.Close()

	exec := NewAntigravityExecutor(&config.Config{RequestRetry: 1})
	auth := &cliproxyauth.Auth{
		ID:       "antigravity-buffered-usage-auth",
		Provider: "antigravity",
		Attributes: map[string]string{
			"base_url": server.URL,
		},
		Metadata: map[string]any{
			"access_token": "token",
			"project_id":   "project-1",
			"expired":      time.Now().Add(time.Hour).Format(time.RFC3339),
		},
	}
	payload := []byte(`{"model":"` + model + `","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
	_, errExecute := exec.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   model,
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat:    sdktranslator.FormatClaude,
		ResponseFormat:  sdktranslator.FormatClaude,
		OriginalRequest: payload,
	})
	if errExecute != nil {
		t.Fatalf("Execute() error = %v", errExecute)
	}

	record := waitForAntigravityUsageRecord(t, records, model)
	if record.Failed || !record.UsageObserved {
		t.Fatalf("usage outcome = failed:%v observed:%v, want successful observed usage", record.Failed, record.UsageObserved)
	}
	if record.Detail.InputTokens != 7 || record.Detail.OutputTokens != 3 || record.Detail.TotalTokens != 10 {
		t.Fatalf("usage detail = %+v, want input=7 output=3 total=10", record.Detail)
	}
	select {
	case duplicate := <-records:
		if duplicate.Provider == "antigravity" && duplicate.Model == model {
			t.Fatalf("unexpected duplicate terminal usage record: %+v", duplicate)
		}
	case <-time.After(50 * time.Millisecond):
	}
}

type antigravityUsageCapturePlugin struct {
	records chan<- usage.Record
}

func (p antigravityUsageCapturePlugin) HandleUsage(_ context.Context, record usage.Record) {
	select {
	case p.records <- record:
	default:
	}
}

func waitForAntigravityUsageRecord(t *testing.T, records <-chan usage.Record, model string) usage.Record {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case record := <-records:
			if record.Provider == "antigravity" && record.Model == model {
				return record
			}
		case <-timeout:
			t.Fatal("timed out waiting for Antigravity usage record")
		}
	}
}
