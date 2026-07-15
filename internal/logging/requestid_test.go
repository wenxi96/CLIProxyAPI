package logging

import "testing"

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
