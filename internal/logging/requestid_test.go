package logging

import (
	"context"
	"testing"
)

func TestGenerateRequestIDUses128Bits(t *testing.T) {
	requestID := GenerateRequestID()
	if len(requestID) != 32 {
		t.Fatalf("request ID length = %d, want 32 hex characters", len(requestID))
	}
	for _, char := range requestID {
		if !(char >= '0' && char <= '9') && !(char >= 'a' && char <= 'f') {
			t.Fatalf("request ID contains non-hex character %q", char)
		}
	}
}

func TestClientRequestMetadataIsAvailableFromCarrier(t *testing.T) {
	ctx := WithClientRequestMetadata(context.Background(), ClientRequestMetadata{
		ClientIP:      "203.0.113.8",
		XForwardedFor: "198.51.100.4",
		UserAgent:     "test-client/1.0",
	})
	metadata := GetClientRequestMetadata(ctx)
	if metadata.ClientIP != "203.0.113.8" || metadata.XForwardedFor != "198.51.100.4" || metadata.UserAgent != "test-client/1.0" {
		t.Fatalf("metadata = %+v, want all carrier fields", metadata)
	}
}
