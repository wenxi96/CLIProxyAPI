package executor

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/wsrelay"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

func TestAIStudioExecutorExecuteStartsTTFTBeforeRelayWait(t *testing.T) {
	const authID = "aistudio-ttft-auth"
	delay := 40 * time.Millisecond
	connected := make(chan struct{})
	var connectedOnce sync.Once
	relay := wsrelay.NewManager(wsrelay.Options{
		ProviderFactory: func(*http.Request) (string, error) {
			return authID, nil
		},
		OnConnected: func(provider string) {
			if provider == authID {
				connectedOnce.Do(func() {
					close(connected)
				})
			}
		},
	})
	server := httptest.NewServer(relay.Handler())
	defer server.Close()
	defer func() {
		if errStop := relay.Stop(context.Background()); errStop != nil {
			t.Errorf("relay stop error = %v", errStop)
		}
	}()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + relay.Path()
	conn, _, errDial := websocket.DefaultDialer.Dial(wsURL, nil)
	if errDial != nil {
		t.Fatalf("dial websocket: %v", errDial)
	}
	defer func() {
		if errClose := conn.Close(); errClose != nil {
			t.Errorf("websocket close error = %v", errClose)
		}
	}()
	select {
	case <-connected:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for relay connection")
	}

	clientDone := make(chan error, 1)
	go func() {
		var msg wsrelay.Message
		if errReadJSON := conn.ReadJSON(&msg); errReadJSON != nil {
			clientDone <- fmt.Errorf("read relay request: %w", errReadJSON)
			return
		}
		if msg.Type != wsrelay.MessageTypeHTTPReq {
			clientDone <- fmt.Errorf("relay message type = %q, want %q", msg.Type, wsrelay.MessageTypeHTTPReq)
			return
		}
		time.Sleep(delay)
		response := wsrelay.Message{
			ID:   msg.ID,
			Type: wsrelay.MessageTypeHTTPResp,
			Payload: map[string]any{
				"status":  float64(http.StatusOK),
				"headers": map[string]any{"Content-Type": "application/json"},
				"body":    `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`,
			},
		}
		if errWriteJSON := conn.WriteJSON(response); errWriteJSON != nil {
			clientDone <- fmt.Errorf("write relay response: %w", errWriteJSON)
			return
		}
		clientDone <- nil
	}()

	plugin := &captureAIStudioUsagePlugin{records: make(chan usage.Record, 16)}
	usage.RegisterPlugin(plugin)
	exec := NewAIStudioExecutor(&config.Config{}, "aistudio", relay)
	_, errExecute := exec.Execute(context.Background(), &cliproxyauth.Auth{ID: authID, Provider: "aistudio"}, cliproxyexecutor.Request{
		Model:   "gemini-3.1-pro-preview",
		Payload: []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatGemini})
	if errExecute != nil {
		t.Fatalf("Execute() error = %v", errExecute)
	}
	if errClient := <-clientDone; errClient != nil {
		t.Fatal(errClient)
	}

	record := waitForAIStudioUsageRecord(t, plugin.records, "gemini-3.1-pro-preview")
	if record.TTFT < delay {
		t.Fatalf("ttft = %v, want >= %v", record.TTFT, delay)
	}
}

func TestAIStudioExecutorStreamPreservesUsageFromFilteredNonTerminalChunk(t *testing.T) {
	const (
		authID = "aistudio-filtered-stream-usage-auth"
		model  = "aistudio-filtered-stream-usage-test"
	)
	connected := make(chan struct{})
	var connectedOnce sync.Once
	relay := wsrelay.NewManager(wsrelay.Options{
		ProviderFactory: func(*http.Request) (string, error) {
			return authID, nil
		},
		OnConnected: func(provider string) {
			if provider == authID {
				connectedOnce.Do(func() {
					close(connected)
				})
			}
		},
	})
	server := httptest.NewServer(relay.Handler())
	defer server.Close()
	defer func() {
		if errStop := relay.Stop(context.Background()); errStop != nil {
			t.Errorf("relay stop error = %v", errStop)
		}
	}()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + relay.Path()
	conn, _, errDial := websocket.DefaultDialer.Dial(wsURL, nil)
	if errDial != nil {
		t.Fatalf("dial websocket: %v", errDial)
	}
	defer func() {
		if errClose := conn.Close(); errClose != nil {
			t.Errorf("websocket close error = %v", errClose)
		}
	}()
	select {
	case <-connected:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for relay connection")
	}

	clientDone := make(chan error, 1)
	go func() {
		var msg wsrelay.Message
		if errReadJSON := conn.ReadJSON(&msg); errReadJSON != nil {
			clientDone <- fmt.Errorf("read relay request: %w", errReadJSON)
			return
		}
		messages := []wsrelay.Message{
			{
				ID:   msg.ID,
				Type: wsrelay.MessageTypeStreamStart,
				Payload: map[string]any{
					"status":  float64(http.StatusOK),
					"headers": map[string]any{"Content-Type": "text/event-stream"},
				},
			},
			{
				ID:   msg.ID,
				Type: wsrelay.MessageTypeStreamChunk,
				Payload: map[string]any{
					"data": "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"ok\"}]}}],\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":3,\"totalTokenCount\":10}}\n\n",
				},
			},
			{
				ID:   msg.ID,
				Type: wsrelay.MessageTypeStreamChunk,
				Payload: map[string]any{
					"data": "data: {\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n",
				},
			},
			{ID: msg.ID, Type: wsrelay.MessageTypeStreamEnd},
		}
		for _, response := range messages {
			if errWriteJSON := conn.WriteJSON(response); errWriteJSON != nil {
				clientDone <- fmt.Errorf("write relay response: %w", errWriteJSON)
				return
			}
		}
		clientDone <- nil
	}()

	plugin := &captureAIStudioUsagePlugin{records: make(chan usage.Record, 4)}
	usage.RegisterNamedPlugin("aistudio-filtered-stream-usage-test", plugin)
	exec := NewAIStudioExecutor(&config.Config{}, "aistudio", relay)
	payload := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	result, errExecute := exec.ExecuteStream(context.Background(), &cliproxyauth.Auth{ID: authID, Provider: "aistudio"}, cliproxyexecutor.Request{
		Model:   model,
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat:    sdktranslator.FormatGemini,
		ResponseFormat:  sdktranslator.FormatGemini,
		OriginalRequest: payload,
	})
	if errExecute != nil {
		t.Fatalf("ExecuteStream() error = %v", errExecute)
	}
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream chunk error: %v", chunk.Err)
		}
	}
	if errClient := <-clientDone; errClient != nil {
		t.Fatal(errClient)
	}

	record := waitForAIStudioUsageRecord(t, plugin.records, model)
	if record.Failed || !record.UsageObserved {
		t.Fatalf("usage outcome = failed:%v observed:%v, want successful observed usage", record.Failed, record.UsageObserved)
	}
	if record.Detail.InputTokens != 7 || record.Detail.OutputTokens != 3 || record.Detail.TotalTokens != 10 {
		t.Fatalf("usage detail = %+v, want input=7 output=3 total=10", record.Detail)
	}
}

func TestAIStudioExecutorHTTPRespObservesUsageBeforeCanceledDelivery(t *testing.T) {
	const (
		authID = "aistudio-http-response-cancel-auth"
		model  = "aistudio-http-response-cancel-usage-test"
	)
	relay, conn := newAIStudioTestRelay(t, authID)
	clientDone := make(chan error, 1)
	go func() {
		var msg wsrelay.Message
		if errReadJSON := conn.ReadJSON(&msg); errReadJSON != nil {
			clientDone <- fmt.Errorf("read relay request: %w", errReadJSON)
			return
		}
		response := wsrelay.Message{
			ID:   msg.ID,
			Type: wsrelay.MessageTypeHTTPResp,
			Payload: map[string]any{
				"status":  float64(http.StatusOK),
				"headers": map[string]any{"Content-Type": "application/json"},
				"body":    `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":3,"totalTokenCount":10}}`,
			},
		}
		if errWriteJSON := conn.WriteJSON(response); errWriteJSON != nil {
			clientDone <- fmt.Errorf("write relay response: %w", errWriteJSON)
			return
		}
		clientDone <- nil
	}()

	plugin := &captureAIStudioUsagePlugin{records: make(chan usage.Record, 4)}
	usage.RegisterNamedPlugin("aistudio-http-response-cancel-usage-test", plugin)
	exec := NewAIStudioExecutor(&config.Config{}, "aistudio", relay)
	payload := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	ctx, cancel := context.WithCancel(context.Background())
	result, errExecute := exec.ExecuteStream(ctx, &cliproxyauth.Auth{ID: authID, Provider: "aistudio"}, cliproxyexecutor.Request{
		Model:   model,
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat:    sdktranslator.FormatGemini,
		ResponseFormat:  sdktranslator.FormatGemini,
		OriginalRequest: payload,
	})
	if errExecute != nil {
		cancel()
		t.Fatalf("ExecuteStream() error = %v", errExecute)
	}
	time.Sleep(20 * time.Millisecond)
	cancel()
	for range result.Chunks {
	}
	if errClient := <-clientDone; errClient != nil {
		t.Fatal(errClient)
	}

	record := waitForAIStudioUsageRecord(t, plugin.records, model)
	if !record.Failed || !record.UsageObserved {
		t.Fatalf("usage outcome = failed:%v observed:%v, want failed usage with observed facts", record.Failed, record.UsageObserved)
	}
	if record.Detail.InputTokens != 7 || record.Detail.OutputTokens != 3 || record.Detail.TotalTokens != 10 {
		t.Fatalf("usage detail = %+v, want input=7 output=3 total=10", record.Detail)
	}
}

func newAIStudioTestRelay(t *testing.T, authID string) (*wsrelay.Manager, *websocket.Conn) {
	t.Helper()
	connected := make(chan struct{})
	var connectedOnce sync.Once
	relay := wsrelay.NewManager(wsrelay.Options{
		ProviderFactory: func(*http.Request) (string, error) {
			return authID, nil
		},
		OnConnected: func(provider string) {
			if provider == authID {
				connectedOnce.Do(func() {
					close(connected)
				})
			}
		},
	})
	server := httptest.NewServer(relay.Handler())
	t.Cleanup(func() {
		if errStop := relay.Stop(context.Background()); errStop != nil {
			t.Errorf("relay stop error = %v", errStop)
		}
		server.Close()
	})

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + relay.Path()
	conn, _, errDial := websocket.DefaultDialer.Dial(wsURL, nil)
	if errDial != nil {
		t.Fatalf("dial websocket: %v", errDial)
	}
	t.Cleanup(func() {
		if errClose := conn.Close(); errClose != nil {
			t.Errorf("websocket close error = %v", errClose)
		}
	})
	select {
	case <-connected:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for relay connection")
	}
	return relay, conn
}

type captureAIStudioUsagePlugin struct {
	records chan usage.Record
}

func (p *captureAIStudioUsagePlugin) HandleUsage(_ context.Context, record usage.Record) {
	if p == nil {
		return
	}
	select {
	case p.records <- record:
	default:
	}
}

func waitForAIStudioUsageRecord(t *testing.T, records <-chan usage.Record, model string) usage.Record {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case record := <-records:
			if record.Provider == "aistudio" && record.Model == model {
				return record
			}
		case <-timeout:
			t.Fatalf("timed out waiting for AI Studio usage record")
		}
	}
}
