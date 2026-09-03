package helps

import "testing"

func TestParsePluginExecutorResponseUsageAdaptsObservedParsers(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		payload  string
		want     int64
	}{
		{
			name:     "claude",
			protocol: "claude",
			payload:  `{"usage":{"input_tokens":2,"output_tokens":3}}`,
			want:     2,
		},
		{
			name:     "gemini",
			protocol: "gemini",
			payload:  `{"usageMetadata":{"promptTokenCount":3,"totalTokenCount":3}}`,
			want:     3,
		},
		{
			name:     "interactions",
			protocol: "interactions",
			payload:  `{"usage":{"input_tokens":4,"total_tokens":4}}`,
			want:     4,
		},
		{
			name:     "antigravity",
			protocol: "antigravity",
			payload:  `{"response":{"usageMetadata":{"promptTokenCount":5,"totalTokenCount":5}}}`,
			want:     5,
		},
		{
			name:     "openai fallback",
			protocol: "unknown",
			payload:  `{"usage":{"prompt_tokens":6,"completion_tokens":1,"total_tokens":7}}`,
			want:     6,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			detail := ParsePluginExecutorResponseUsage(tc.protocol, []byte(tc.payload))
			if detail.InputTokens != tc.want {
				t.Fatalf("input tokens = %d, want %d; detail=%+v", detail.InputTokens, tc.want, detail)
			}
		})
	}
}
