package helps

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	internallogging "github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestParseOpenAIUsageChatCompletions(t *testing.T) {
	data := []byte(`{"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens_details":{"reasoning_tokens":5}}}`)
	detail, observed := ParseOpenAIUsage(data)
	if !observed {
		t.Fatal("ParseOpenAIUsage() observed = false, want true")
	}
	if detail.InputTokens != 1 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 1)
	}
	if detail.OutputTokens != 2 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 2)
	}
	if detail.TotalTokens != 3 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 3)
	}
	if detail.CachedTokens != 4 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 4)
	}
	if detail.CacheReadTokens != 4 {
		t.Fatalf("cache read tokens = %d, want %d", detail.CacheReadTokens, 4)
	}
	if detail.ReasoningTokens != 5 {
		t.Fatalf("reasoning tokens = %d, want %d", detail.ReasoningTokens, 5)
	}
}

func TestParseOpenAIUsageResponses(t *testing.T) {
	data := []byte(`{"service_tier":"default","usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30,"input_tokens_details":{"cached_tokens":7},"output_tokens_details":{"reasoning_tokens":9}}}`)
	detail, observed := ParseOpenAIUsage(data)
	if !observed {
		t.Fatal("ParseOpenAIUsage() observed = false, want true")
	}
	if detail.InputTokens != 10 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 10)
	}
	if detail.OutputTokens != 20 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 20)
	}
	if detail.TotalTokens != 30 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 30)
	}
	if detail.CachedTokens != 7 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 7)
	}
	if detail.CacheReadTokens != 7 {
		t.Fatalf("cache read tokens = %d, want %d", detail.CacheReadTokens, 7)
	}
	if detail.ReasoningTokens != 9 {
		t.Fatalf("reasoning tokens = %d, want %d", detail.ReasoningTokens, 9)
	}
	if detail.ResponseServiceTier != "default" {
		t.Fatalf("response service tier = %q, want default", detail.ResponseServiceTier)
	}
}

func TestParseCodexUsageIncludesCacheWriteTokens(t *testing.T) {
	data := []byte(`{"response":{"service_tier":"priority","usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120,"input_tokens_details":{"cached_tokens":30,"cache_write_tokens":40}}}}`)
	detail, ok := ParseCodexUsage(data)
	if !ok {
		t.Fatal("ParseCodexUsage() ok = false, want true")
	}
	if detail.InputTokens != 100 {
		t.Fatalf("input tokens = %d, want 100", detail.InputTokens)
	}
	if detail.OutputTokens != 20 {
		t.Fatalf("output tokens = %d, want 20", detail.OutputTokens)
	}
	if detail.CachedTokens != 30 {
		t.Fatalf("cached tokens = %d, want 30", detail.CachedTokens)
	}
	if detail.CacheReadTokens != 30 {
		t.Fatalf("cache read tokens = %d, want 30", detail.CacheReadTokens)
	}
	if detail.CacheCreationTokens != 40 {
		t.Fatalf("cache creation tokens = %d, want 40", detail.CacheCreationTokens)
	}
	if detail.TotalTokens != 120 {
		t.Fatalf("total tokens = %d, want 120", detail.TotalTokens)
	}
	if detail.ResponseServiceTier != "priority" {
		t.Fatalf("response service tier = %q, want priority", detail.ResponseServiceTier)
	}
}

func TestParseOpenAIUsageNormalizesCacheCreationAlias(t *testing.T) {
	data := []byte(`{"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12,"input_tokens_details":{"cache_creation_tokens":4}}}`)
	detail, observed := ParseOpenAIUsage(data)
	if !observed {
		t.Fatal("ParseOpenAIUsage() observed = false, want true")
	}
	if detail.CacheCreationTokens != 4 {
		t.Fatalf("cache creation tokens = %d, want 4", detail.CacheCreationTokens)
	}
}

func TestParseOpenAIUsageIgnoresNullUsage(t *testing.T) {
	data := []byte(`{"usage":null}`)
	detail, observed := ParseOpenAIUsage(data)
	if observed || detail != (usage.Detail{}) {
		t.Fatalf("ParseOpenAIUsage() = (%+v, %t), want zero detail and false", detail, observed)
	}
}

func TestProviderParsersDistinguishExplicitZeroFromMissingUsage(t *testing.T) {
	tests := []struct {
		name    string
		parse   func([]byte) (usage.Detail, bool)
		payload []byte
		null    []byte
		empty   []byte
		invalid []byte
	}{
		{name: "openai", parse: ParseOpenAIUsage, payload: []byte(`{"usage":{"prompt_tokens":0}}`), null: []byte(`{"usage":null}`), empty: []byte(`{"usage":{}}`), invalid: []byte(`{"usage":{"prompt_tokens":null}}`)},
		{name: "claude", parse: ParseClaudeUsage, payload: []byte(`{"usage":{"input_tokens":0}}`), null: []byte(`{"usage":null}`), empty: []byte(`{"usage":{}}`), invalid: []byte(`{"usage":{"input_tokens":"zero"}}`)},
		{name: "gemini", parse: ParseGeminiUsage, payload: []byte(`{"usageMetadata":{"promptTokenCount":0}}`), null: []byte(`{"usageMetadata":null}`), empty: []byte(`{"usageMetadata":{}}`), invalid: []byte(`{"usageMetadata":{"promptTokenCount":null}}`)},
		{name: "interactions", parse: ParseInteractionsUsage, payload: []byte(`{"usage":{"input_tokens":0}}`), null: []byte(`{"usage":null}`), empty: []byte(`{"usage":{}}`), invalid: []byte(`{"usage":{"input_tokens":"zero"}}`)},
		{name: "antigravity", parse: ParseAntigravityUsage, payload: []byte(`{"response":{"usageMetadata":{"promptTokenCount":0}}}`), null: []byte(`{"response":{"usageMetadata":null}}`), empty: []byte(`{"response":{"usageMetadata":{}}}`), invalid: []byte(`{"response":{"usageMetadata":{"promptTokenCount":null}}}`)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			detail, observed := test.parse(test.payload)
			if !observed || detail != (usage.Detail{}) {
				t.Fatalf("explicit zero = (%+v, %t), want zero detail and true", detail, observed)
			}
			missing, missingObserved := test.parse([]byte(`{}`))
			if missingObserved || missing != (usage.Detail{}) {
				t.Fatalf("missing usage = (%+v, %t), want zero detail and false", missing, missingObserved)
			}
			for label, payload := range map[string][]byte{"null": test.null, "empty": test.empty, "invalid": test.invalid} {
				detail, observed := test.parse(payload)
				if observed || detail != (usage.Detail{}) {
					t.Fatalf("%s usage = (%+v, %t), want zero detail and false", label, detail, observed)
				}
			}
		})
	}
}

func TestInteractionsUsageSkipsInvalidEarlierFallback(t *testing.T) {
	payload := []byte(`{"usage":null,"metadata":{"total_usage":{"total_input_tokens":2,"total_output_tokens":3,"total_tokens":5}}}`)
	detail, observed := ParseInteractionsUsage(payload)
	if !observed || detail.TotalTokens != 5 {
		t.Fatalf("ParseInteractionsUsage() = (%+v, %t), want valid later fallback", detail, observed)
	}
	detail, observed = ParseInteractionsStreamUsage(append([]byte("data: "), payload...))
	if !observed || detail.TotalTokens != 5 {
		t.Fatalf("ParseInteractionsStreamUsage() = (%+v, %t), want valid later fallback", detail, observed)
	}
}

func TestProviderParsersIgnoreNumericStringsBesideValidNumbers(t *testing.T) {
	tests := []struct {
		name    string
		parse   func([]byte) (usage.Detail, bool)
		payload []byte
	}{
		{name: "openai", parse: ParseOpenAIUsage, payload: []byte(`{"usage":{"prompt_tokens":1,"completion_tokens":"999","total_tokens":"1000"}}`)},
		{name: "claude", parse: ParseClaudeUsage, payload: []byte(`{"usage":{"input_tokens":1,"output_tokens":"999"}}`)},
		{name: "gemini", parse: ParseGeminiUsage, payload: []byte(`{"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":"999"}}`)},
		{name: "interactions", parse: ParseInteractionsUsage, payload: []byte(`{"usage":{"input_tokens":1,"output_tokens":"999"}}`)},
		{name: "antigravity", parse: ParseAntigravityUsage, payload: []byte(`{"response":{"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":"999"}}}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			detail, observed := test.parse(test.payload)
			if !observed || detail.InputTokens != 1 || detail.OutputTokens != 0 || detail.TotalTokens != 0 {
				t.Fatalf("parsed usage = (%+v, %t), want only numeric input token", detail, observed)
			}
		})
	}
}

func TestProviderStreamParsersRejectNullAndEmptyUsage(t *testing.T) {
	tests := []struct {
		name  string
		parse func([]byte) (usage.Detail, bool)
		null  []byte
		empty []byte
		zero  []byte
	}{
		{name: "openai", parse: ParseOpenAIStreamUsage, null: []byte(`data: {"usage":null}`), empty: []byte(`data: {"usage":{}}`), zero: []byte(`data: {"usage":{"prompt_tokens":0}}`)},
		{name: "claude", parse: ParseClaudeStreamUsage, null: []byte(`data: {"usage":null}`), empty: []byte(`data: {"usage":{}}`), zero: []byte(`data: {"usage":{"input_tokens":0}}`)},
		{name: "gemini", parse: ParseGeminiStreamUsage, null: []byte(`data: {"usageMetadata":null}`), empty: []byte(`data: {"usageMetadata":{}}`), zero: []byte(`data: {"usageMetadata":{"promptTokenCount":0}}`)},
		{name: "interactions", parse: ParseInteractionsStreamUsage, null: []byte(`data: {"usage":null}`), empty: []byte(`data: {"usage":{}}`), zero: []byte(`data: {"usage":{"input_tokens":0}}`)},
		{name: "antigravity", parse: ParseAntigravityStreamUsage, null: []byte(`data: {"response":{"usageMetadata":null}}`), empty: []byte(`data: {"response":{"usageMetadata":{}}}`), zero: []byte(`data: {"response":{"usageMetadata":{"promptTokenCount":0}}}`)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for label, payload := range map[string][]byte{"null": test.null, "empty": test.empty} {
				if detail, observed := test.parse(payload); observed {
					t.Fatalf("%s usage = (%+v, true), want false", label, detail)
				}
			}
			if detail, observed := test.parse(test.zero); !observed || detail != (usage.Detail{}) {
				t.Fatalf("explicit zero = (%+v, %t), want zero detail and true", detail, observed)
			}
		})
	}
}

func TestParseOpenAIUsagePreservesResponseTierWithoutUsage(t *testing.T) {
	t.Parallel()

	detail, observed := ParseOpenAIUsage([]byte(`{"service_tier":"default"}`))
	if observed {
		t.Fatal("ParseOpenAIUsage() observed = true, want false for tier-only metadata")
	}
	if detail.ResponseServiceTier != "default" {
		t.Fatalf("response service tier = %q, want default", detail.ResponseServiceTier)
	}
}

func TestParseCodexUsagePreservesResponseTierWithoutUsage(t *testing.T) {
	t.Parallel()

	detail, observed := ParseCodexUsage([]byte(`{"response":{"service_tier":"default"}}`))
	if observed || detail.ResponseServiceTier != "default" {
		t.Fatalf("ParseCodexUsage() = (%+v, %v), want tier default and observed false", detail, observed)
	}
}

func TestParseOpenAIStreamUsageIgnoresNullUsage(t *testing.T) {
	line := []byte(`data: {"id":"chunk_1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}],"usage":null}`)
	if detail, ok := ParseOpenAIStreamUsage(line); ok {
		t.Fatalf("ParseOpenAIStreamUsage() = (%+v, true), want false for null usage", detail)
	}
}

func TestParseOpenAIStreamUsageResponsesFields(t *testing.T) {
	line := []byte(`data: {"id":"chunk_1","object":"chat.completion.chunk","service_tier":"flex","choices":[],"usage":{"input_tokens":8,"output_tokens":5,"total_tokens":13,"input_tokens_details":{"cached_tokens":3},"output_tokens_details":{"reasoning_tokens":2}}}`)
	detail, ok := ParseOpenAIStreamUsage(line)
	if !ok {
		t.Fatal("ParseOpenAIStreamUsage() ok = false, want true")
	}
	if detail.InputTokens != 8 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 8)
	}
	if detail.OutputTokens != 5 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 5)
	}
	if detail.TotalTokens != 13 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 13)
	}
	if detail.CachedTokens != 3 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 3)
	}
	if detail.CacheReadTokens != 3 {
		t.Fatalf("cache read tokens = %d, want %d", detail.CacheReadTokens, 3)
	}
	if detail.ReasoningTokens != 2 {
		t.Fatalf("reasoning tokens = %d, want %d", detail.ReasoningTokens, 2)
	}
	if detail.ResponseServiceTier != "flex" {
		t.Fatalf("response service tier = %q, want flex", detail.ResponseServiceTier)
	}
}

func TestStreamUsageBufferKeepsLastUsage(t *testing.T) {
	var buffer StreamUsageBuffer
	buffer.Observe(usage.Detail{}, true)
	buffer.Observe(usage.Detail{InputTokens: 1, OutputTokens: 1, TotalTokens: 2}, false)
	buffer.Observe(usage.Detail{InputTokens: 39320, OutputTokens: 26, TotalTokens: 39346, CachedTokens: 33280}, true)

	detail, ok := buffer.Detail()
	if !ok {
		t.Fatal("buffer detail ok = false, want true")
	}
	if detail.InputTokens != 39320 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 39320)
	}
	if detail.OutputTokens != 26 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 26)
	}
	if detail.TotalTokens != 39346 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 39346)
	}
	if detail.CachedTokens != 33280 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 33280)
	}
}

func TestStreamUsageBufferPreservesTierAcrossChunks(t *testing.T) {
	t.Parallel()

	var buffer StreamUsageBuffer
	buffer.ObserveOpenAIStream([]byte(`data: {"service_tier":"default"}`))
	buffer.ObserveOpenAIStream([]byte(`data: {"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
	detail, ok := buffer.Detail()
	if !ok {
		t.Fatal("Detail() ok = false, want true")
	}
	if detail.InputTokens != 1 || detail.OutputTokens != 1 || detail.ResponseServiceTier != "default" {
		t.Fatalf("detail = %+v, want usage with response tier default", detail)
	}
}

func TestStreamUsageBufferObserveOpenAIStreamStateTransitions(t *testing.T) {
	t.Parallel()

	t.Run("same chunk", func(t *testing.T) {
		var buffer StreamUsageBuffer
		buffer.ObserveOpenAIStream([]byte(`data: {"service_tier":"flex","usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}`))
		detail, ok := buffer.Detail()
		if !ok || detail.InputTokens != 2 || detail.ResponseServiceTier != "flex" {
			t.Fatalf("detail = %+v ok=%v", detail, ok)
		}
	})

	t.Run("usage before tier", func(t *testing.T) {
		var buffer StreamUsageBuffer
		buffer.ObserveOpenAIStream([]byte(`data: {"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}`))
		buffer.ObserveOpenAIStream([]byte(`data: {"service_tier":"default"}`))
		detail, ok := buffer.Detail()
		if !ok || detail.InputTokens != 2 || detail.ResponseServiceTier != "default" {
			t.Fatalf("detail = %+v ok=%v", detail, ok)
		}
	})

	t.Run("final usage tier overrides early tier", func(t *testing.T) {
		var buffer StreamUsageBuffer
		buffer.ObserveOpenAIStream([]byte(`data: {"service_tier":"default"}`))
		buffer.ObserveOpenAIStream([]byte(`data: {"service_tier":"priority","usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}`))
		detail, ok := buffer.Detail()
		if !ok || detail.ResponseServiceTier != "priority" {
			t.Fatalf("detail = %+v ok=%v", detail, ok)
		}
	})

	t.Run("irrelevant and invalid chunks do not change state", func(t *testing.T) {
		var buffer StreamUsageBuffer
		buffer.ObserveOpenAIStream([]byte(`data: {"content":"the word \"usage\" appears here"}`))
		buffer.ObserveOpenAIStream([]byte(`data: {"usage":`))
		buffer.ObserveOpenAIStream([]byte(`data: {"usage":null}`))
		if detail, ok := buffer.Detail(); ok {
			t.Fatalf("detail = %+v ok=true, want empty buffer", detail)
		}
	})

	t.Run("zero token usage is retained", func(t *testing.T) {
		var buffer StreamUsageBuffer
		buffer.ObserveOpenAIStream([]byte(`data: {"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}`))
		if _, ok := buffer.Detail(); !ok {
			t.Fatal("Detail() ok = false, want true")
		}
	})
}

func TestStreamUsageBufferPreservesOnlyZeroUsage(t *testing.T) {
	var buffer StreamUsageBuffer
	buffer.Observe(usage.Detail{}, true)

	detail, ok := buffer.Detail()
	if !ok {
		t.Fatal("buffer detail ok = false, want true")
	}
	if detail != (usage.Detail{}) {
		t.Fatalf("detail = %+v, want zero detail", detail)
	}
}

func TestGeminiStreamUsageAccumulatorHandlesCombinedAndSplitChunks(t *testing.T) {
	tests := []struct {
		name   string
		chunks [][]byte
	}{
		{
			name: "combined SSE frames",
			chunks: [][]byte{
				[]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]}}],\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":3,\"totalTokenCount\":10}}\n\ndata: {\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n"),
			},
		},
		{
			name: "split usage frame",
			chunks: [][]byte{
				[]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]}}],\"usageMeta"),
				[]byte("data\":{\"promptTokenCount\":7,\"candidatesTokenCount\":3,\"totalTokenCount\":10}}\n\ndata: {\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var accumulator GeminiStreamUsageAccumulator
			var buffer StreamUsageBuffer
			for _, chunk := range tt.chunks {
				accumulator.Observe(chunk, &buffer)
			}
			accumulator.Flush(&buffer)

			detail, ok := buffer.Detail()
			if !ok {
				t.Fatal("usage detail ok = false, want true")
			}
			if detail.InputTokens != 7 || detail.OutputTokens != 3 || detail.TotalTokens != 10 {
				t.Fatalf("usage detail = %+v, want input=7 output=3 total=10", detail)
			}
		})
	}
}

func TestGeminiStreamUsageAccumulatorRecoversAfterOversizedLine(t *testing.T) {
	var accumulator GeminiStreamUsageAccumulator
	accumulator.maxPendingBytes = 512
	var buffer StreamUsageBuffer

	accumulator.Observe([]byte(strings.Repeat("x", 300)), &buffer)
	accumulator.Observe([]byte(strings.Repeat("x", 213)), &buffer)
	accumulator.Observe([]byte("discarded\ndata: {\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":3,\"totalTokenCount\":10}}\n\n"), &buffer)
	accumulator.Flush(&buffer)

	detail, ok := buffer.Detail()
	if !ok {
		t.Fatal("usage detail ok = false, want true after oversized-line recovery")
	}
	if detail.InputTokens != 7 || detail.OutputTokens != 3 || detail.TotalTokens != 10 {
		t.Fatalf("usage detail = %+v, want input=7 output=3 total=10", detail)
	}
}

func TestParseClaudeUsageIncludesCacheTokensInTotal(t *testing.T) {
	data := []byte(`{"usage":{"input_tokens":3085,"output_tokens":253,"cache_read_input_tokens":7,"cache_creation_input_tokens":19514}}`)
	detail, observed := ParseClaudeUsage(data)
	if !observed {
		t.Fatal("ParseClaudeUsage() observed = false, want true")
	}
	if detail.InputTokens != 3085 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 3085)
	}
	if detail.OutputTokens != 253 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 253)
	}
	if detail.CacheReadTokens != 7 {
		t.Fatalf("cache read tokens = %d, want %d", detail.CacheReadTokens, 7)
	}
	if detail.CacheCreationTokens != 19514 {
		t.Fatalf("cache creation tokens = %d, want %d", detail.CacheCreationTokens, 19514)
	}
	if detail.CachedTokens != 7 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 7)
	}
	if detail.TotalTokens != 0 {
		t.Fatalf("total tokens = %d, want zero when provider omitted total", detail.TotalTokens)
	}
}

func TestParseClaudeUsageFallsBackCachedTokensToCacheCreation(t *testing.T) {
	data := []byte(`{"usage":{"input_tokens":3085,"output_tokens":253,"cache_creation_input_tokens":19514}}`)
	detail, observed := ParseClaudeUsage(data)
	if !observed {
		t.Fatal("ParseClaudeUsage() observed = false, want true")
	}
	if detail.CachedTokens != 19514 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 19514)
	}
	if detail.TotalTokens != 0 {
		t.Fatalf("total tokens = %d, want zero when provider omitted total", detail.TotalTokens)
	}
}

func TestClaudeStreamUsageBufferMergesMessageStartAndMessageDelta(t *testing.T) {
	var buffer ClaudeStreamUsageBuffer
	buffer.Observe([]byte(`data: {"type":"message_start","message":{"usage":{"input_tokens":100,"cache_read_input_tokens":30,"cache_creation_input_tokens":20,"output_tokens":1}}}`))
	buffer.Observe([]byte(`data: {"type":"message_delta","usage":{"output_tokens":25}}`))

	detail, ok := buffer.Detail()
	if !ok {
		t.Fatal("buffer detail missing")
	}
	if detail.InputTokens != 100 || detail.OutputTokens != 25 {
		t.Fatalf("detail input/output = %d/%d, want 100/25", detail.InputTokens, detail.OutputTokens)
	}
	if detail.CacheReadTokens != 30 || detail.CacheCreationTokens != 20 || detail.CachedTokens != 30 {
		t.Fatalf("detail cache facts = %+v, want read=30 creation=20 cached=30", detail)
	}
	if detail.TotalTokens != 0 {
		t.Fatalf("detail total_tokens = %d, want zero when provider omitted total", detail.TotalTokens)
	}
}

func TestProviderParsersDoNotSynthesizeReportedTotals(t *testing.T) {
	gemini, geminiObserved := ParseGeminiUsage([]byte(`{"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":4,"thoughtsTokenCount":5}}`))
	if !geminiObserved {
		t.Fatal("ParseGeminiUsage() observed = false, want true")
	}
	if gemini.TotalTokens != 0 {
		t.Fatalf("Gemini total_tokens = %d, want zero when totalTokenCount is absent", gemini.TotalTokens)
	}
	interactions, interactionsObserved := ParseInteractionsUsage([]byte(`{"usage":{"input_tokens":3,"output_tokens":4,"reasoning_tokens":5}}`))
	if !interactionsObserved {
		t.Fatal("ParseInteractionsUsage() observed = false, want true")
	}
	if interactions.TotalTokens != 0 {
		t.Fatalf("Interactions total_tokens = %d, want zero when total_tokens is absent", interactions.TotalTokens)
	}
}

func TestParseGeminiUsageNormalizesCachedContent(t *testing.T) {
	detail, observed := ParseGeminiUsage([]byte(`{"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"cachedContentTokenCount":4,"totalTokenCount":12}}`))
	if !observed {
		t.Fatal("ParseGeminiUsage() observed = false, want true")
	}
	if detail.CachedTokens != 4 {
		t.Fatalf("cached tokens = %d, want 4", detail.CachedTokens)
	}
	if detail.CacheReadTokens != 4 {
		t.Fatalf("cache read tokens = %d, want 4", detail.CacheReadTokens)
	}
}

func TestParseInteractionsUsage(t *testing.T) {
	detail, observed := ParseInteractionsUsage([]byte(`{"usage":{"input_tokens":3,"output_tokens":4,"reasoning_tokens":5,"total_tokens":12,"cached_tokens":2}}`))
	if !observed {
		t.Fatal("ParseInteractionsUsage() observed = false, want true")
	}
	if detail.InputTokens != 3 {
		t.Fatalf("input tokens = %d, want 3", detail.InputTokens)
	}
	if detail.OutputTokens != 4 {
		t.Fatalf("output tokens = %d, want 4", detail.OutputTokens)
	}
	if detail.ReasoningTokens != 5 {
		t.Fatalf("reasoning tokens = %d, want 5", detail.ReasoningTokens)
	}
	if detail.TotalTokens != 12 {
		t.Fatalf("total tokens = %d, want 12", detail.TotalTokens)
	}
	if detail.CachedTokens != 2 {
		t.Fatalf("cached tokens = %d, want 2", detail.CachedTokens)
	}
	if detail.CacheReadTokens != 2 {
		t.Fatalf("cache read tokens = %d, want 2", detail.CacheReadTokens)
	}
}

func TestParseInteractionsUsageNormalizesCacheWriteAlias(t *testing.T) {
	detail, observed := ParseInteractionsUsage([]byte(`{"usage":{"input_tokens":3,"cache_write_tokens":2}}`))
	if !observed {
		t.Fatal("ParseInteractionsUsage() observed = false, want true")
	}
	if detail.CacheCreationTokens != 2 {
		t.Fatalf("cache creation tokens = %d, want 2", detail.CacheCreationTokens)
	}
}

func TestParseInteractionsStreamUsage(t *testing.T) {
	detail, ok := ParseInteractionsStreamUsage([]byte(`{"type":"interaction.completed","interaction":{"usage":{"input_tokens":2,"output_tokens":6,"total_tokens":8}}}`))
	if !ok {
		t.Fatal("ParseInteractionsStreamUsage() ok = false, want true")
	}
	if detail.TotalTokens != 8 {
		t.Fatalf("total tokens = %d, want 8", detail.TotalTokens)
	}
}

func TestParseInteractionsStreamUsageOfficialMetadata(t *testing.T) {
	detail, ok := ParseInteractionsStreamUsage([]byte(`data: {"event_type":"finish","metadata":{"total_usage":{"total_input_tokens":2,"total_output_tokens":6,"total_thought_tokens":3,"total_cached_tokens":1,"total_tokens":11}}}`))
	if !ok {
		t.Fatal("ParseInteractionsStreamUsage() ok = false, want true")
	}
	if detail.InputTokens != 2 {
		t.Fatalf("input tokens = %d, want 2", detail.InputTokens)
	}
	if detail.OutputTokens != 6 {
		t.Fatalf("output tokens = %d, want 6", detail.OutputTokens)
	}
	if detail.ReasoningTokens != 3 {
		t.Fatalf("reasoning tokens = %d, want 3", detail.ReasoningTokens)
	}
	if detail.CachedTokens != 1 {
		t.Fatalf("cached tokens = %d, want 1", detail.CachedTokens)
	}
	if detail.CacheReadTokens != 1 {
		t.Fatalf("cache read tokens = %d, want 1", detail.CacheReadTokens)
	}
	if detail.TotalTokens != 11 {
		t.Fatalf("total tokens = %d, want 11", detail.TotalTokens)
	}
}

func TestUsageReporterBuildRecordIncludesLatency(t *testing.T) {
	reporter := &UsageReporter{
		provider:    "openai",
		model:       "gpt-5.4",
		requestedAt: time.Now().Add(-1500 * time.Millisecond),
	}

	record := reporter.buildRecord(usage.Detail{TotalTokens: 3}, false)
	if record.Latency < time.Second {
		t.Fatalf("latency = %v, want >= 1s", record.Latency)
	}
	if record.Latency > 3*time.Second {
		t.Fatalf("latency = %v, want <= 3s", record.Latency)
	}
}

func TestSummarizeErrorBodyRedactsSensitiveValues(t *testing.T) {
	body := []byte(`{"error":{"message":"bad key sk-raw-summary-key"},"authorization":"Basic raw-basic-token","token":"raw-json-token","x-api-token":"raw-x-token","cookie":"session=raw-cookie","total_tokens":12}`)
	summary := SummarizeErrorBody("application/json", body)
	if !strings.Contains(summary, "[redacted]") {
		t.Fatalf("summary = %q, want redacted marker", summary)
	}
	for _, secret := range []string{"sk-raw-summary-key", "raw-basic-token", "raw-json-token", "raw-x-token", "raw-cookie"} {
		if strings.Contains(summary, secret) {
			t.Fatalf("summary leaked %q: %s", secret, summary)
		}
	}
}

func TestUsageReporterFailureRedactsSensitiveValues(t *testing.T) {
	reporter := NewUsageReporter(context.Background(), "openai", "gpt-5.4", nil)
	record := reporter.buildRecord(usage.Detail{}, true, failFromErrors(errors.New("upstream failed with sk-raw-failure-key Authorization: Basic raw-basic-token token=raw-form-token Cookie: session=raw-cookie total_tokens=12")))
	for _, secret := range []string{"sk-raw-failure-key", "raw-basic-token", "raw-form-token", "raw-cookie"} {
		if strings.Contains(record.Fail.Body, secret) {
			t.Fatalf("fail body leaked %q: %s", secret, record.Fail.Body)
		}
	}
	if !strings.Contains(record.Fail.Body, "total_tokens=12") {
		t.Fatalf("fail body = %q, want total_tokens counter preserved", record.Fail.Body)
	}
}

func TestUsageReporterTrackHTTPClientStartsTTFTBeforeRoundTrip(t *testing.T) {
	delay := 40 * time.Millisecond
	reporter := NewUsageReporter(context.Background(), "openai", "gpt-5.4", nil)
	client := reporter.TrackHTTPClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			time.Sleep(delay)
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("ok")),
				Request:    req,
			}, nil
		}),
	})

	req, errNewRequest := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.invalid/v1/chat/completions", strings.NewReader("{}"))
	if errNewRequest != nil {
		t.Fatalf("NewRequestWithContext() error = %v", errNewRequest)
	}
	resp, errDo := client.Do(req)
	if errDo != nil {
		t.Fatalf("Do() error = %v", errDo)
	}
	if _, errRead := io.ReadAll(resp.Body); errRead != nil {
		t.Fatalf("ReadAll() error = %v", errRead)
	}
	if errClose := resp.Body.Close(); errClose != nil {
		t.Fatalf("response body close error = %v", errClose)
	}
	if got := reporter.ttftDuration(); got < delay {
		t.Fatalf("ttft = %v, want >= %v", got, delay)
	}
}

func TestUsageReporterBuildRecordIncludesRequestedModelAlias(t *testing.T) {
	ctx := usage.WithRequestedModelAlias(context.Background(), "client-gpt")
	reporter := NewUsageReporter(ctx, "openai", "gpt-5.4", nil)

	record := reporter.buildRecord(usage.Detail{TotalTokens: 3}, false)
	if record.Model != "gpt-5.4" {
		t.Fatalf("model = %q, want %q", record.Model, "gpt-5.4")
	}
	if record.Alias != "client-gpt" {
		t.Fatalf("alias = %q, want %q", record.Alias, "client-gpt")
	}
}

func TestNewExecutorUsageReporterIncludesExecutorType(t *testing.T) {
	reporter := NewExecutorUsageReporter(context.Background(), &TestUsageExecutor{}, "gpt-5.4", nil)

	record := reporter.buildRecord(usage.Detail{TotalTokens: 3}, false)
	if record.Provider != "test-provider" {
		t.Fatalf("provider = %q, want %q", record.Provider, "test-provider")
	}
	if record.ExecutorType != "TestUsageExecutor" {
		t.Fatalf("executor type = %q, want %q", record.ExecutorType, "TestUsageExecutor")
	}
}

func TestUsageReporterBuildRecordIncludesReasoningEffort(t *testing.T) {
	ctx := usage.WithReasoningEffort(context.Background(), "medium")
	reporter := NewUsageReporter(ctx, "openai", "gpt-5.4", nil)

	record := reporter.buildRecord(usage.Detail{TotalTokens: 3}, false)
	if record.ReasoningEffort != "medium" {
		t.Fatalf("reasoning effort = %q, want %q", record.ReasoningEffort, "medium")
	}
}

func TestUsageReporterBuildRecordIncludesServiceTier(t *testing.T) {
	ctx := usage.WithServiceTier(context.Background(), "auto")
	reporter := NewUsageReporter(ctx, "openai", "gpt-5.4", nil)

	record := reporter.buildRecord(usage.Detail{TotalTokens: 3, ResponseServiceTier: "default"}, false)
	if record.ServiceTier != "auto" {
		t.Fatalf("service tier = %q, want %q", record.ServiceTier, "auto")
	}
	if record.ResponseServiceTier != "default" {
		t.Fatalf("response service tier = %q, want default", record.ResponseServiceTier)
	}
}

func TestUsageReporterBuildRecordDefaultsGenerateTrue(t *testing.T) {
	reporter := NewUsageReporter(context.Background(), "openai", "gpt-5.4", nil)

	record := reporter.buildRecord(usage.Detail{TotalTokens: 3}, false)
	if !usage.GenerateEnabled(record.Generate) {
		t.Fatalf("generate = %v, want true", usage.GenerateEnabled(record.Generate))
	}
}

func TestUsageReporterBuildRecordIncludesGenerateFalse(t *testing.T) {
	ctx := usage.WithGenerate(context.Background(), false)
	reporter := NewUsageReporter(ctx, "openai", "gpt-5.4", nil)

	record := reporter.buildRecord(usage.Detail{TotalTokens: 3}, false)
	if usage.GenerateEnabled(record.Generate) {
		t.Fatalf("generate = %v, want false", usage.GenerateEnabled(record.Generate))
	}
}

func TestUsageReporterSetTranslatedReasoningEffortPreservesClientServiceTier(t *testing.T) {
	ctx := usage.WithServiceTier(context.Background(), "auto")
	reporter := NewUsageReporter(ctx, "openai", "gpt-5.4", nil)

	reporter.SetTranslatedReasoningEffort([]byte(`{"service_tier":"priority"}`), "openai")

	record := reporter.buildRecord(usage.Detail{TotalTokens: 3}, false)
	if record.ServiceTier != "auto" {
		t.Fatalf("service tier = %q, want %q", record.ServiceTier, "auto")
	}
}

func TestUsageReporterBuildAdditionalModelRecordSkipsZeroTokens(t *testing.T) {
	reporter := &UsageReporter{
		provider:    "codex",
		model:       "gpt-5.4",
		requestedAt: time.Now(),
	}

	if _, ok := reporter.buildAdditionalModelRecord("gpt-image-2", usage.Detail{}); ok {
		t.Fatalf("expected all-zero token usage to be skipped")
	}
	if _, ok := reporter.buildAdditionalModelRecord("gpt-image-2", usage.Detail{InputTokens: 2}); !ok {
		t.Fatalf("expected non-zero input token usage to be recorded")
	}
	if _, ok := reporter.buildAdditionalModelRecord("gpt-image-2", usage.Detail{CachedTokens: 2}); !ok {
		t.Fatalf("expected non-zero cached token usage to be recorded")
	}
}

func TestUsageReporterFinalFailurePublishesUsageFactsOnce(t *testing.T) {
	records := make(chan usage.Record, 2)
	usage.RegisterNamedPlugin("helps-test-usage-wins-over-later-failure", usagePluginFunc(func(_ context.Context, record usage.Record) {
		if record.Model == "gpt-usage-wins" {
			records <- record
		}
	}))

	reporter := NewUsageReporter(context.Background(), "openai", "gpt-5.4", nil)
	reporter.model = "gpt-usage-wins"
	reporter.PublishFailureWithUsage(
		context.Background(),
		usage.Detail{InputTokens: 2, OutputTokens: 3, TotalTokens: 5},
		errors.New("stream read error"),
	)

	select {
	case record := <-records:
		if !record.Failed {
			t.Fatalf("record failed = false, want true")
		}
		if record.Detail.TotalTokens != 5 {
			t.Fatalf("total tokens = %d, want 5", record.Detail.TotalTokens)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for usage record")
	}

	select {
	case record := <-records:
		t.Fatalf("unexpected duplicate usage record: %+v", record)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestUsageReporterMissingUsageIsTerminal(t *testing.T) {
	records := make(chan usage.Record, 2)
	usage.RegisterNamedPlugin("helps-test-terminal-missing", usagePluginFunc(func(_ context.Context, record usage.Record) {
		if record.Model == "gpt-terminal-missing" {
			records <- record
		}
	}))

	ctx := internallogging.WithRequestID(context.Background(), "req-terminal-missing")
	reporter := NewUsageReporter(ctx, "openai", "gpt-5.4", nil)
	reporter.model = "gpt-terminal-missing"
	reporter.EnsurePublished(ctx)
	reporter.Publish(ctx, usage.Detail{InputTokens: 2, OutputTokens: 3, TotalTokens: 5})

	select {
	case record := <-records:
		if record.Detail.TotalTokens != 0 {
			t.Fatalf("record total_tokens = %d, want missing usage", record.Detail.TotalTokens)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for missing usage record")
	}

	select {
	case record := <-records:
		t.Fatalf("unexpected usage revision after terminal missing record: %+v", record)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestUsageReporterPublishParsedMissingEmitsExactlyOnce(t *testing.T) {
	records := make(chan usage.Record, 2)
	usage.RegisterNamedPlugin("helps-test-publish-parsed-missing", usagePluginFunc(func(_ context.Context, record usage.Record) {
		if record.Model == "gpt-publish-parsed-missing" {
			records <- record
		}
	}))
	reporter := NewUsageReporter(context.Background(), "codex", "gpt-publish-parsed-missing", nil)
	reporter.PublishParsed(context.Background(), usage.Detail{}, false)
	reporter.EnsurePublished(context.Background())

	select {
	case record := <-records:
		if record.UsageObserved || record.Detail != (usage.Detail{}) {
			t.Fatalf("record = %+v, want one missing-usage record", record)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for missing usage record")
	}
	select {
	case record := <-records:
		t.Fatalf("unexpected duplicate missing usage record: %+v", record)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestStreamUsageBufferPublishesFailureWithFactsOnce(t *testing.T) {
	records := make(chan usage.Record, 2)
	usage.RegisterNamedPlugin("helps-test-stream-failure-with-facts", usagePluginFunc(func(_ context.Context, record usage.Record) {
		if record.Model == "gpt-stream-failure-with-facts" {
			records <- record
		}
	}))

	ctx := internallogging.WithRequestID(context.Background(), "req-stream-failure-with-facts")
	reporter := NewUsageReporter(ctx, "openai", "gpt-5.4", nil)
	reporter.model = "gpt-stream-failure-with-facts"
	var buffer StreamUsageBuffer
	buffer.Observe(usage.Detail{InputTokens: 2, OutputTokens: 3, TotalTokens: 5}, true)
	if !buffer.PublishFailure(ctx, reporter, errors.New("stream read failed")) {
		t.Fatal("PublishFailure() = false, want true")
	}
	buffer.Publish(ctx, reporter)

	select {
	case record := <-records:
		if !record.Failed || record.Detail.TotalTokens != 5 {
			t.Fatalf("record = %+v, want failed usage with preserved facts", record)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for failed usage record")
	}

	select {
	case record := <-records:
		t.Fatalf("unexpected duplicate stream usage record: %+v", record)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestStreamUsageBufferFinalizeMarksContextCancellationFailed(t *testing.T) {
	records := make(chan usage.Record, 1)
	usage.RegisterNamedPlugin("helps-test-stream-context-cancel", usagePluginFunc(func(_ context.Context, record usage.Record) {
		if record.Model == "gpt-stream-context-cancel" {
			records <- record
		}
	}))

	ctx, cancel := context.WithCancel(context.Background())
	reporter := NewUsageReporter(ctx, "openai", "gpt-5.4", nil)
	reporter.model = "gpt-stream-context-cancel"
	var buffer StreamUsageBuffer
	buffer.Observe(usage.Detail{InputTokens: 2, TotalTokens: 2}, true)
	cancel()
	buffer.Finalize(ctx, reporter, nil)

	select {
	case record := <-records:
		if !record.Failed || record.Detail.TotalTokens != 2 {
			t.Fatalf("record = %+v, want canceled failure with preserved usage", record)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for canceled usage record")
	}
}

func TestStreamUsageBufferPublishesExplicitZeroAsObserved(t *testing.T) {
	records := make(chan usage.Record, 1)
	usage.RegisterNamedPlugin("helps-test-stream-observed-zero", usagePluginFunc(func(_ context.Context, record usage.Record) {
		if record.Model == "gpt-stream-observed-zero" {
			records <- record
		}
	}))

	ctx := context.Background()
	reporter := NewUsageReporter(ctx, "openai", "gpt-5.4", nil)
	reporter.model = "gpt-stream-observed-zero"
	var buffer StreamUsageBuffer
	buffer.Observe(usage.Detail{}, true)
	buffer.Finalize(ctx, reporter, nil)

	select {
	case record := <-records:
		if !record.UsageObserved || record.Failed {
			t.Fatalf("record = %+v, want observed zero success", record)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for observed zero usage record")
	}
}

func TestStreamUsageBufferPublishesTierOnlyAsUnobservedMetadata(t *testing.T) {
	records := make(chan usage.Record, 1)
	usage.RegisterNamedPlugin("helps-test-stream-tier-only", usagePluginFunc(func(_ context.Context, record usage.Record) {
		if record.Model == "gpt-stream-tier-only" {
			records <- record
		}
	}))

	ctx := context.Background()
	reporter := NewUsageReporter(ctx, "openai", "gpt-5.4", nil)
	reporter.model = "gpt-stream-tier-only"
	var buffer StreamUsageBuffer
	buffer.Observe(usage.Detail{ResponseServiceTier: "default"}, false)
	buffer.Finalize(ctx, reporter, nil)

	select {
	case record := <-records:
		if record.UsageObserved || record.Failed || record.ResponseServiceTier != "default" {
			t.Fatalf("record = %+v, want unobserved tier-only success", record)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tier-only usage metadata")
	}
}

func TestUsageReporterDoesNotSynthesizeTotalTokens(t *testing.T) {
	records := make(chan usage.Record, 1)
	usage.RegisterNamedPlugin("helps-test-no-synthetic-total", usagePluginFunc(func(_ context.Context, record usage.Record) {
		if record.Model == "gpt-no-synthetic-total" {
			records <- record
		}
	}))

	reporter := NewUsageReporter(context.Background(), "openai", "gpt-5.4", nil)
	reporter.model = "gpt-no-synthetic-total"
	reporter.Publish(context.Background(), usage.Detail{InputTokens: 2, OutputTokens: 3, ReasoningTokens: 4})

	select {
	case record := <-records:
		if record.Detail.TotalTokens != 0 {
			t.Fatalf("reported total_tokens = %d, want zero so canonical usage normalization computes totals", record.Detail.TotalTokens)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for usage record")
	}
}

func TestUsageReporterPublishAdditionalModelMarksDetailRole(t *testing.T) {
	roles := make(chan string, 1)
	usage.RegisterNamedPlugin("helps-test-additional-model-role", usagePluginFunc(func(ctx context.Context, record usage.Record) {
		if record.Model == "gpt-image-2" {
			roles <- internallogging.GetUsageDetailRole(ctx)
		}
	}))

	reporter := NewUsageReporter(context.Background(), "codex", "gpt-5.4", nil)
	reporter.PublishAdditionalModel(context.Background(), "gpt-image-2", usage.Detail{InputTokens: 1, TotalTokens: 1})

	select {
	case role := <-roles:
		if role != "additional" {
			t.Fatalf("detail role = %q, want additional", role)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for additional model record")
	}
}

func TestUsageReporterPublishAdditionalModelAddsSequence(t *testing.T) {
	sequences := make(chan string, 2)
	usage.RegisterNamedPlugin("helps-test-additional-model-sequence", usagePluginFunc(func(ctx context.Context, record usage.Record) {
		if record.Model == "gpt-image-2-sequence" {
			sequences <- internallogging.GetUsageDetailSequence(ctx)
		}
	}))

	reporter := NewUsageReporter(context.Background(), "codex", "gpt-5.4", nil)
	reporter.PublishAdditionalModel(context.Background(), "gpt-image-2-sequence", usage.Detail{InputTokens: 1, TotalTokens: 1})
	reporter.PublishAdditionalModel(context.Background(), "gpt-image-2-sequence", usage.Detail{InputTokens: 2, TotalTokens: 2})

	var got []string
	deadline := time.After(time.Second)
	for len(got) < 2 {
		select {
		case sequence := <-sequences:
			got = append(got, sequence)
		case <-deadline:
			t.Fatalf("timed out waiting for additional model sequences, got %v", got)
		}
	}
	if got[0] != "1" || got[1] != "2" {
		t.Fatalf("sequences = %v, want [1 2]", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type TestUsageExecutor struct{}

func (TestUsageExecutor) Identifier() string {
	return "test-provider"
}

type usagePluginFunc func(context.Context, usage.Record)

func (f usagePluginFunc) HandleUsage(ctx context.Context, record usage.Record) {
	f(ctx, record)
}
