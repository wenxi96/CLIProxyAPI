package management

import (
	"errors"
	"testing"
	"time"
)

func TestUsageEventsCursorV1RoundTripAndTamperRejected(t *testing.T) {
	codec, err := newUsageTokenCodecWithKey([]byte("unit-test-usage-token-key-32-bytes"))
	if err != nil {
		t.Fatalf("newUsageTokenCodecWithKey() error = %v", err)
	}
	want := usageEventsCursorV1{
		DatasetEpoch:         7,
		QueryHash:            "query-hash-v1",
		SnapshotMaxSequence:  123,
		RewriteRevision:      9,
		PostingListID:        "events:v1:source:abc",
		PhysicalScanPosition: 50,
	}
	token, err := codec.encodeEventsCursor(want)
	if err != nil {
		t.Fatalf("encodeEventsCursor() error = %v", err)
	}
	got, err := codec.decodeEventsCursor(token)
	if err != nil {
		t.Fatalf("decodeEventsCursor() error = %v", err)
	}
	if got != want {
		t.Fatalf("decoded cursor = %+v, want %+v", got, want)
	}

	tampered := token[:len(token)-1] + "A"
	if tampered == token {
		tampered = token[:len(token)-1] + "B"
	}
	if _, err := codec.decodeEventsCursor(tampered); !errors.Is(err, errUsageTokenInvalid) {
		t.Fatalf("tampered cursor error = %v, want %v", err, errUsageTokenInvalid)
	}
}

func TestUsageEventsQueryHashV1NormalizesRepeatedSources(t *testing.T) {
	from := time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	first := usageEventsQueryIdentity{
		From:      from,
		To:        to,
		Timezone:  "UTC",
		Limit:     50,
		APIs:      []string{"POST /v1/responses"},
		Models:    []string{"gpt-5"},
		Providers: []string{"openai"},
		Sources:   []string{"source-b", "source-a", "source-b"},
		Auths:     []string{"auth-1"},
		Failed:    "false",
	}
	second := first
	second.Sources = []string{"source-a", "source-b"}

	firstHash, err := usageEventsQueryHashV1(first)
	if err != nil {
		t.Fatalf("usageEventsQueryHashV1(first) error = %v", err)
	}
	secondHash, err := usageEventsQueryHashV1(second)
	if err != nil {
		t.Fatalf("usageEventsQueryHashV1(second) error = %v", err)
	}
	if firstHash != secondHash {
		t.Fatalf("normalized hashes differ: %q != %q", firstHash, secondHash)
	}

	changed := second
	changed.Limit = 51
	changedHash, err := usageEventsQueryHashV1(changed)
	if err != nil {
		t.Fatalf("usageEventsQueryHashV1(changed) error = %v", err)
	}
	if changedHash == firstHash {
		t.Fatalf("query hash ignored limit change: %q", changedHash)
	}
}

func TestUsageWindowAnchorV1MintsPinnedRangeAndExpires(t *testing.T) {
	now := time.Date(2026, 8, 10, 9, 30, 0, 123, time.UTC)
	codec, err := newUsageTokenCodecWithKey([]byte("unit-test-usage-token-key-32-bytes"))
	if err != nil {
		t.Fatalf("newUsageTokenCodecWithKey() error = %v", err)
	}
	nonces := []string{"nonce-a", "nonce-b"}
	manager := newUsageWindowAnchorManager(codec, func() time.Time { return now }, func() (string, error) {
		nonce := nonces[0]
		nonces = nonces[1:]
		return nonce, nil
	})

	firstToken, first, err := manager.mint(7, "24h", "UTC")
	if err != nil {
		t.Fatalf("mint(first) error = %v", err)
	}
	secondToken, second, err := manager.mint(7, "24h", "UTC")
	if err != nil {
		t.Fatalf("mint(second) error = %v", err)
	}
	if firstToken == secondToken || first.AnchorNonce == second.AnchorNonce {
		t.Fatalf("same-time anchors were not unique: first=%q second=%q", firstToken, secondToken)
	}
	if first.To.Before(now) || second.To.Before(first.To) {
		t.Fatalf("anchor observation times regressed: first=%s second=%s now=%s", first.To, second.To, now)
	}
	if wantFrom := first.To.Add(-24 * time.Hour); !first.From.Equal(wantFrom) {
		t.Fatalf("anchor from = %s, want %s", first.From, wantFrom)
	}
	if !first.MintedAt.Equal(first.To) || !first.ExpiresAt.Equal(first.MintedAt.Add(5*time.Minute)) {
		t.Fatalf("anchor times = minted:%s expires:%s to:%s", first.MintedAt, first.ExpiresAt, first.To)
	}

	resolved, err := manager.resolve(firstToken, 7, first.MintedAt.Add(4*time.Minute))
	if err != nil {
		t.Fatalf("resolve(valid) error = %v", err)
	}
	if resolved.AnchorNonce != first.AnchorNonce || !resolved.From.Equal(first.From) || !resolved.To.Equal(first.To) {
		t.Fatalf("resolved anchor = %+v, want %+v", resolved, first)
	}
	if _, err := manager.resolve(firstToken, 7, first.ExpiresAt); !errors.Is(err, errUsageWindowAnchorExpired) {
		t.Fatalf("resolve(expired) error = %v, want %v", err, errUsageWindowAnchorExpired)
	}
	if _, err := manager.resolve(firstToken, 8, first.MintedAt); !errors.Is(err, errUsageWindowAnchorExpired) {
		t.Fatalf("resolve(epoch mismatch) error = %v, want %v", err, errUsageWindowAnchorExpired)
	}
}

func TestUsageWindowAnchorUsesStableManagementKeyAcrossHandlerRestart(t *testing.T) {
	first := &Handler{envSecret: "test-management-anchor-signing-secret"}
	if err := first.ensureUsageTokenState(); err != nil {
		t.Fatalf("first ensureUsageTokenState() error = %v", err)
	}

	token, anchor, err := first.usageWindowAnchors.mint(7, "24h", "UTC")
	if err != nil {
		t.Fatalf("first mint() error = %v", err)
	}

	second := &Handler{envSecret: "test-management-anchor-signing-secret"}
	if err := second.ensureUsageTokenState(); err != nil {
		t.Fatalf("second ensureUsageTokenState() error = %v", err)
	}
	if _, err := second.usageWindowAnchors.resolve(token, 7, anchor.MintedAt.Add(time.Minute)); err != nil {
		t.Fatalf("restart resolve() error = %v", err)
	}
	if _, err := second.usageWindowAnchors.resolve(token, 8, anchor.MintedAt.Add(time.Minute)); !errors.Is(err, errUsageWindowAnchorExpired) {
		t.Fatalf("restart epoch mismatch error = %v, want %v", err, errUsageWindowAnchorExpired)
	}
}
