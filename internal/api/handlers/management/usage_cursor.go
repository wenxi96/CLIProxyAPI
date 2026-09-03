package management

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	usageTokenVersionV1        = "v1"
	usageEventsCursorTokenKind = "usage_events_cursor"
	usageAuthCursorTokenKind   = "usage_auth_cursor"
	usageWindowAnchorTokenKind = "usage_window_anchor"
	usageEventsSortV1          = "timestamp_desc,sequence_desc,batch_ordinal_desc,stable_event_id_desc"
	usageTokenMaxPayloadBytes  = 16 << 10
	usageWindowAnchorTTL       = 5 * time.Minute
)

var (
	errUsageTokenInvalid        = errors.New("invalid usage token")
	errUsageWindowAnchorExpired = errors.New("usage window anchor expired")
)

type usageTokenCodec struct {
	key   []byte
	keyID string
}

type usageWindowAnchorManager struct {
	mu                  sync.Mutex
	codec               *usageTokenCodec
	now                 func() time.Time
	nonce               func() (string, error)
	lastObservationTime time.Time
}

type usageTokenEnvelopeV1 struct {
	Version string          `json:"version"`
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

type usageEventsCursorV1 struct {
	DatasetEpoch         uint64 `json:"dataset_epoch"`
	QueryHash            string `json:"query_hash"`
	SnapshotMaxSequence  uint64 `json:"snapshot_max_sequence"`
	RewriteRevision      uint64 `json:"rewrite_revision"`
	PostingListID        string `json:"posting_list_id"`
	PhysicalScanPosition uint64 `json:"physical_scan_position"`
}

type usageWindowAnchorV1 struct {
	DatasetEpoch uint64    `json:"dataset_epoch"`
	Window       string    `json:"window"`
	From         time.Time `json:"from"`
	To           time.Time `json:"to"`
	Timezone     string    `json:"timezone"`
	AnchorNonce  string    `json:"anchor_nonce"`
	MintedAt     time.Time `json:"minted_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	KeyID        string    `json:"key_id"`
}

type usageEventsQueryIdentity struct {
	From           time.Time
	To             time.Time
	Window         string
	WindowAnchorID string
	Timezone       string
	Limit          int
	APIs           []string
	Models         []string
	Providers      []string
	Sources        []string
	Auths          []string
	Failed         string
}

type usageEventsCanonicalQueryV1 struct {
	From           string   `json:"from"`
	To             string   `json:"to"`
	Window         string   `json:"window"`
	WindowAnchorID string   `json:"window_anchor_id"`
	Timezone       string   `json:"timezone"`
	Limit          int      `json:"limit"`
	Sort           string   `json:"sort"`
	APIs           []string `json:"apis"`
	Models         []string `json:"models"`
	Providers      []string `json:"providers"`
	Sources        []string `json:"sources"`
	Auths          []string `json:"auths"`
	Failed         string   `json:"failed"`
}

func newUsageTokenCodecWithKey(key []byte) (*usageTokenCodec, error) {
	if len(key) < sha256.Size {
		return nil, fmt.Errorf("usage token key must contain at least %d bytes", sha256.Size)
	}
	digest := sha256.Sum256(key)
	return &usageTokenCodec{
		key:   append([]byte(nil), key...),
		keyID: hex.EncodeToString(digest[:8]),
	}, nil
}

func newUsageTokenCodec() (*usageTokenCodec, error) {
	var key [sha256.Size]byte
	if _, err := rand.Read(key[:]); err != nil {
		return nil, fmt.Errorf("generate usage token key: %w", err)
	}
	return newUsageTokenCodecWithKey(key[:])
}

func newUsageTokenCodecFromKeyMaterial(material string) (*usageTokenCodec, error) {
	if strings.TrimSpace(material) == "" {
		return newUsageTokenCodec()
	}
	digest := sha256.Sum256(append([]byte("usage-token-v1-key\x00"), []byte(material)...))
	return newUsageTokenCodecWithKey(digest[:])
}

func (h *Handler) ensureUsageTokenState() error {
	if h == nil {
		return errUsageTokenInvalid
	}
	h.usageTokenMu.Lock()
	defer h.usageTokenMu.Unlock()
	if h.usageTokenCodec != nil && h.usageWindowAnchors != nil {
		return nil
	}
	if h.usageTokenInitErr != nil {
		return h.usageTokenInitErr
	}
	if h.usageTokenCodec == nil {
		codec, err := newUsageTokenCodecFromKeyMaterial(h.usageTokenKeyMaterial())
		if err != nil {
			h.usageTokenInitErr = err
			return err
		}
		h.usageTokenCodec = codec
	}
	if h.usageWindowAnchors == nil {
		h.usageWindowAnchors = newUsageWindowAnchorManager(h.usageTokenCodec, h.usageCurrentTime, nil)
	}
	return nil
}

func (h *Handler) usageTokenKeyMaterial() string {
	if h == nil {
		return ""
	}
	if secret := strings.TrimSpace(h.envSecret); secret != "" {
		return "env:" + secret
	}
	h.mu.Lock()
	cfg := h.cfg
	localPassword := h.localPassword
	h.mu.Unlock()
	if cfg != nil {
		if secret := strings.TrimSpace(cfg.RemoteManagement.SecretKey); secret != "" {
			return "config:" + secret
		}
	}
	if password := strings.TrimSpace(localPassword); password != "" {
		return "local:" + password
	}
	return ""
}

func newUsageWindowAnchorManager(codec *usageTokenCodec, now func() time.Time, nonce func() (string, error)) *usageWindowAnchorManager {
	if now == nil {
		now = time.Now
	}
	if nonce == nil {
		nonce = randomUsageAnchorNonce
	}
	return &usageWindowAnchorManager{codec: codec, now: now, nonce: nonce}
}

func (codec *usageTokenCodec) encodeEventsCursor(cursor usageEventsCursorV1) (string, error) {
	if cursor.DatasetEpoch == 0 || strings.TrimSpace(cursor.QueryHash) == "" || strings.TrimSpace(cursor.PostingListID) == "" {
		return "", errUsageTokenInvalid
	}
	return codec.encode(usageEventsCursorTokenKind, cursor)
}

func (codec *usageTokenCodec) decodeEventsCursor(token string) (usageEventsCursorV1, error) {
	var cursor usageEventsCursorV1
	if err := codec.decode(token, usageEventsCursorTokenKind, &cursor); err != nil {
		return usageEventsCursorV1{}, err
	}
	if cursor.DatasetEpoch == 0 || strings.TrimSpace(cursor.QueryHash) == "" || strings.TrimSpace(cursor.PostingListID) == "" {
		return usageEventsCursorV1{}, errUsageTokenInvalid
	}
	return cursor, nil
}

func (codec *usageTokenCodec) encodeAuthCursor(cursor usageEventsCursorV1) (string, error) {
	if cursor.DatasetEpoch == 0 || strings.TrimSpace(cursor.QueryHash) == "" || strings.TrimSpace(cursor.PostingListID) == "" {
		return "", errUsageTokenInvalid
	}
	return codec.encode(usageAuthCursorTokenKind, cursor)
}

func (codec *usageTokenCodec) decodeAuthCursor(token string) (usageEventsCursorV1, error) {
	var cursor usageEventsCursorV1
	if err := codec.decode(token, usageAuthCursorTokenKind, &cursor); err != nil {
		return usageEventsCursorV1{}, err
	}
	if cursor.DatasetEpoch == 0 || strings.TrimSpace(cursor.QueryHash) == "" || strings.TrimSpace(cursor.PostingListID) == "" {
		return usageEventsCursorV1{}, errUsageTokenInvalid
	}
	return cursor, nil
}

func (manager *usageWindowAnchorManager) mint(datasetEpoch uint64, window, timezone string) (string, usageWindowAnchorV1, error) {
	if manager == nil || manager.codec == nil || datasetEpoch == 0 {
		return "", usageWindowAnchorV1{}, errUsageTokenInvalid
	}
	window, duration, err := normalizedUsageWindow(window)
	if err != nil {
		return "", usageWindowAnchorV1{}, err
	}
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		timezone = "UTC"
	}

	manager.mu.Lock()
	observationTime := manager.now().UTC()
	if observationTime.Before(manager.lastObservationTime) {
		observationTime = manager.lastObservationTime
	}
	manager.lastObservationTime = observationTime
	nonce, err := manager.nonce()
	manager.mu.Unlock()
	if err != nil || strings.TrimSpace(nonce) == "" {
		return "", usageWindowAnchorV1{}, fmt.Errorf("mint usage anchor nonce: %w", errUsageTokenInvalid)
	}

	from := time.Time{}
	if duration > 0 {
		from = observationTime.Add(-duration)
	}
	anchor := usageWindowAnchorV1{
		DatasetEpoch: datasetEpoch,
		Window:       window,
		From:         from,
		To:           observationTime,
		Timezone:     timezone,
		AnchorNonce:  strings.TrimSpace(nonce),
		MintedAt:     observationTime,
		ExpiresAt:    observationTime.Add(usageWindowAnchorTTL),
		KeyID:        manager.codec.keyID,
	}
	token, err := manager.codec.encode(usageWindowAnchorTokenKind, anchor)
	if err != nil {
		return "", usageWindowAnchorV1{}, err
	}
	return token, anchor, nil
}

func (manager *usageWindowAnchorManager) resolve(token string, datasetEpoch uint64, now time.Time) (usageWindowAnchorV1, error) {
	if manager == nil || manager.codec == nil || datasetEpoch == 0 {
		return usageWindowAnchorV1{}, errUsageTokenInvalid
	}
	var anchor usageWindowAnchorV1
	if err := manager.codec.decode(token, usageWindowAnchorTokenKind, &anchor); err != nil {
		return usageWindowAnchorV1{}, err
	}
	if err := validateUsageWindowAnchorV1(anchor, manager.codec.keyID); err != nil {
		return usageWindowAnchorV1{}, err
	}
	if anchor.DatasetEpoch != datasetEpoch || !now.UTC().Before(anchor.ExpiresAt) {
		return usageWindowAnchorV1{}, errUsageWindowAnchorExpired
	}
	return anchor, nil
}

func validateUsageWindowAnchorV1(anchor usageWindowAnchorV1, keyID string) error {
	window, duration, err := normalizedUsageWindow(anchor.Window)
	if err != nil || window != anchor.Window || anchor.DatasetEpoch == 0 || strings.TrimSpace(anchor.Timezone) == "" ||
		strings.TrimSpace(anchor.AnchorNonce) == "" || anchor.KeyID == "" || anchor.KeyID != keyID || anchor.To.IsZero() ||
		anchor.MintedAt.IsZero() || anchor.ExpiresAt.IsZero() || !anchor.MintedAt.Equal(anchor.To) ||
		!anchor.ExpiresAt.Equal(anchor.MintedAt.Add(usageWindowAnchorTTL)) {
		return errUsageTokenInvalid
	}
	if duration == 0 {
		if !anchor.From.IsZero() {
			return errUsageTokenInvalid
		}
	} else if !anchor.From.Equal(anchor.To.Add(-duration)) {
		return errUsageTokenInvalid
	}
	return nil
}

func normalizedUsageWindow(window string) (string, time.Duration, error) {
	window = strings.ToLower(strings.TrimSpace(window))
	switch window {
	case "7h":
		return window, 7 * time.Hour, nil
	case "24h":
		return window, 24 * time.Hour, nil
	case "7d":
		return window, 7 * 24 * time.Hour, nil
	case "all":
		return window, 0, nil
	default:
		return "", 0, errUsageTokenInvalid
	}
}

func randomUsageAnchorNonce() (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(nonce[:]), nil
}

func (codec *usageTokenCodec) encode(kind string, payload any) (string, error) {
	if codec == nil || len(codec.key) < sha256.Size || strings.TrimSpace(kind) == "" {
		return "", errUsageTokenInvalid
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal usage token payload: %w", err)
	}
	envelopeJSON, err := json.Marshal(usageTokenEnvelopeV1{
		Version: usageTokenVersionV1,
		Kind:    kind,
		Payload: payloadJSON,
	})
	if err != nil {
		return "", fmt.Errorf("marshal usage token envelope: %w", err)
	}
	if len(envelopeJSON) > usageTokenMaxPayloadBytes {
		return "", errUsageTokenInvalid
	}
	signature := codec.sign(envelopeJSON)
	return base64.RawURLEncoding.EncodeToString(envelopeJSON) + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (codec *usageTokenCodec) decode(token, kind string, target any) error {
	if codec == nil || len(codec.key) < sha256.Size || strings.TrimSpace(token) == "" || strings.TrimSpace(kind) == "" || target == nil {
		return errUsageTokenInvalid
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return errUsageTokenInvalid
	}
	envelopeJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(envelopeJSON) == 0 || len(envelopeJSON) > usageTokenMaxPayloadBytes {
		return errUsageTokenInvalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(signature, codec.sign(envelopeJSON)) {
		return errUsageTokenInvalid
	}
	var envelope usageTokenEnvelopeV1
	if err := decodeUsageTokenJSON(envelopeJSON, &envelope); err != nil ||
		envelope.Version != usageTokenVersionV1 || envelope.Kind != kind || len(envelope.Payload) == 0 {
		return errUsageTokenInvalid
	}
	if err := decodeUsageTokenJSON(envelope.Payload, target); err != nil {
		return errUsageTokenInvalid
	}
	return nil
}

func (codec *usageTokenCodec) sign(payload []byte) []byte {
	mac := hmac.New(sha256.New, codec.key)
	_, _ = mac.Write([]byte("usage-token-v1\x00"))
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func decodeUsageTokenJSON(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errUsageTokenInvalid
	}
	return nil
}

func usageEventsQueryHashV1(query usageEventsQueryIdentity) (string, error) {
	canonical, err := canonicalUsageEventsQueryV1(query)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("marshal usage events query: %w", err)
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func canonicalUsageEventsQueryV1(query usageEventsQueryIdentity) (usageEventsCanonicalQueryV1, error) {
	if query.Limit < 1 || query.Limit > 500 {
		return usageEventsCanonicalQueryV1{}, errUsageTokenInvalid
	}
	if !query.From.IsZero() {
		query.From = query.From.UTC()
	}
	if !query.To.IsZero() {
		query.To = query.To.UTC()
	}
	if !query.From.IsZero() && !query.To.IsZero() && !query.From.Before(query.To) {
		return usageEventsCanonicalQueryV1{}, errUsageTokenInvalid
	}
	failed := strings.TrimSpace(query.Failed)
	if failed != "" {
		parsed, err := strconv.ParseBool(failed)
		if err != nil {
			return usageEventsCanonicalQueryV1{}, errUsageTokenInvalid
		}
		failed = strconv.FormatBool(parsed)
	}
	return usageEventsCanonicalQueryV1{
		From:           canonicalUsageQueryTime(query.From),
		To:             canonicalUsageQueryTime(query.To),
		Window:         strings.ToLower(strings.TrimSpace(query.Window)),
		WindowAnchorID: strings.TrimSpace(query.WindowAnchorID),
		Timezone:       strings.TrimSpace(query.Timezone),
		Limit:          query.Limit,
		Sort:           usageEventsSortV1,
		APIs:           normalizedUsageQueryValues(query.APIs),
		Models:         normalizedUsageQueryValues(query.Models),
		Providers:      normalizedUsageQueryValues(query.Providers),
		Sources:        normalizedUsageQueryValues(query.Sources),
		Auths:          normalizedUsageQueryValues(query.Auths),
		Failed:         failed,
	}, nil
}

func canonicalUsageQueryTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func normalizedUsageQueryValues(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, rawValue := range values {
		value := strings.TrimSpace(rawValue)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
