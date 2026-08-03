package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/home"
)

type homeRequestLogClient interface {
	HeartbeatOK() bool
	RPushRequestLog(ctx context.Context, payload []byte) error
}

var currentHomeRequestLogClient = func() homeRequestLogClient {
	return home.Current()
}

type homeRequestLogPayload struct {
	Headers    map[string][]string `json:"headers,omitempty"`
	RequestID  string              `json:"request_id,omitempty"`
	RequestLog string              `json:"request_log,omitempty"`
}

const externalLogRedactedValue = "[REDACTED]"

var (
	externalLogJSONSecretPattern = regexp.MustCompile(`(?i)("(?:(?:api[_-]?key)|(?:auth[_-]?token)|(?:access[_-]?token)|(?:refresh[_-]?token)|(?:id[_-]?token)|password|secret|authorization|cookie)"\s*:\s*")[^"]*(")`)
	externalLogFormSecretPattern = regexp.MustCompile(`(?i)(\b(?:key|api[_-]?key|auth[_-]?token|access[_-]?token|refresh[_-]?token|id[_-]?token|password|secret)=)[^&\s]+`)
)

func cloneHeaders(headers map[string][]string) map[string][]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string][]string, len(headers))
	for key, values := range headers {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if values == nil {
			out[key] = nil
			continue
		}
		copied := make([]string, len(values))
		copy(copied, values)
		out[key] = copied
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isSensitiveExternalLogKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return false
	}
	key = strings.TrimSuffix(key, "[]")
	switch key {
	case "authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "x-goog-api-key", "openai-api-key", "key", "password", "secret":
		return true
	}
	return strings.Contains(key, "api-key") ||
		strings.Contains(key, "apikey") ||
		strings.Contains(key, "api_key") ||
		strings.Contains(key, "token") ||
		strings.Contains(key, "secret") ||
		strings.Contains(key, "cookie") ||
		strings.Contains(key, "password")
}

func maskExternalLogHeaderValue(key, value string) string {
	if !isSensitiveExternalLogKey(key) {
		return value
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(key)), "authorization") {
		parts := strings.SplitN(strings.TrimSpace(value), " ", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) != "" {
			return parts[0] + " " + externalLogRedactedValue
		}
	}
	return externalLogRedactedValue
}

func sanitizeHeadersForExternalLog(headers map[string][]string) map[string][]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string][]string, len(headers))
	for key, values := range headers {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if values == nil {
			out[key] = nil
			continue
		}
		copied := make([]string, len(values))
		for i, value := range values {
			copied[i] = maskExternalLogHeaderValue(key, value)
		}
		out[key] = copied
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sanitizeExternalLogURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	parsed, errParse := url.Parse(raw)
	if errParse != nil || parsed.RawQuery == "" {
		return raw
	}
	values := parsed.Query()
	changed := false
	for key := range values {
		if !isSensitiveExternalLogKey(key) {
			continue
		}
		for i := range values[key] {
			values[key][i] = externalLogRedactedValue
		}
		changed = true
	}
	if !changed {
		return raw
	}
	parsed.RawQuery = values.Encode()
	return parsed.String()
}

func sanitizeLogTextForExternalLog(logText string) string {
	if logText == "" {
		return ""
	}
	lines := strings.Split(logText, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "URL: ") {
			lines[i] = "URL: " + sanitizeExternalLogURL(strings.TrimSpace(strings.TrimPrefix(line, "URL: ")))
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if ok && isSensitiveExternalLogKey(key) {
			lines[i] = key + ": " + maskExternalLogHeaderValue(key, strings.TrimSpace(value))
		}
	}
	sanitized := strings.Join(lines, "\n")
	sanitized = externalLogJSONSecretPattern.ReplaceAllString(sanitized, `${1}`+externalLogRedactedValue+`${2}`)
	sanitized = externalLogFormSecretPattern.ReplaceAllString(sanitized, `${1}`+externalLogRedactedValue)
	return sanitized
}

func (l *FileRequestLogger) forwardRequestLogToHome(ctx context.Context, headers map[string][]string, requestID string, logText string) error {
	if l == nil || !l.homeEnabled {
		return nil
	}
	client := currentHomeRequestLogClient()
	if client == nil || !client.HeartbeatOK() {
		return nil
	}
	payload := homeRequestLogPayload{
		Headers:    sanitizeHeadersForExternalLog(headers),
		RequestID:  strings.TrimSpace(requestID),
		RequestLog: sanitizeLogTextForExternalLog(logText),
	}
	raw, errMarshal := json.Marshal(&payload)
	if errMarshal != nil {
		return errMarshal
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return client.RPushRequestLog(ctx, raw)
}

// SetHomeEnabled toggles home request-log forwarding.
// When enabled, request logs are not written to disk and are instead forwarded to home via Redis RESP.
func (l *FileRequestLogger) SetHomeEnabled(enabled bool) {
	if l == nil {
		return
	}
	l.homeEnabled = enabled
}

type homeStreamingLogWriter struct {
	url       string
	method    string
	timestamp time.Time

	requestHeaders map[string][]string
	requestBody    []byte

	chunkChan chan []byte
	doneChan  chan struct{}

	responseStatus   int
	statusWritten    bool
	responseHeaders  map[string][]string
	responseBody     bytes.Buffer
	apiRequest       []byte
	apiResponse      []byte
	apiWebsocketTime []byte
	requestID        string
	apiResponseTS    time.Time
	firstChunkTS     time.Time
}

func newHomeStreamingLogWriter(url, method string, headers map[string][]string, body []byte, requestID string) *homeStreamingLogWriter {
	requestHeaders := make(map[string][]string, len(headers))
	for key, values := range headers {
		headerValues := make([]string, len(values))
		copy(headerValues, values)
		requestHeaders[key] = headerValues
	}

	writer := &homeStreamingLogWriter{
		url:            url,
		method:         method,
		timestamp:      time.Now(),
		requestHeaders: requestHeaders,
		requestBody:    append([]byte(nil), body...),
		requestID:      strings.TrimSpace(requestID),
		chunkChan:      make(chan []byte, 100),
		doneChan:       make(chan struct{}),
	}

	go writer.asyncWriter()
	return writer
}

func (w *homeStreamingLogWriter) asyncWriter() {
	defer close(w.doneChan)
	for chunk := range w.chunkChan {
		if len(chunk) == 0 {
			continue
		}
		_, _ = w.responseBody.Write(chunk)
	}
}

func (w *homeStreamingLogWriter) WriteChunkAsync(chunk []byte) {
	if w == nil || w.chunkChan == nil || len(chunk) == 0 {
		return
	}
	select {
	case w.chunkChan <- append([]byte(nil), chunk...):
	default:
	}
}

func (w *homeStreamingLogWriter) WriteStatus(status int, headers map[string][]string) error {
	if w == nil || status == 0 {
		return nil
	}
	w.responseStatus = status
	w.statusWritten = true
	if headers != nil {
		w.responseHeaders = make(map[string][]string, len(headers))
		for key, values := range headers {
			copied := make([]string, len(values))
			copy(copied, values)
			w.responseHeaders[key] = copied
		}
	}
	return nil
}

func (w *homeStreamingLogWriter) WriteAPIRequest(apiRequest []byte) error {
	if w == nil || len(apiRequest) == 0 {
		return nil
	}
	w.apiRequest = bytes.Clone(apiRequest)
	return nil
}

func (w *homeStreamingLogWriter) WriteAPIResponse(apiResponse []byte) error {
	if w == nil || len(apiResponse) == 0 {
		return nil
	}
	w.apiResponse = bytes.Clone(apiResponse)
	return nil
}

func (w *homeStreamingLogWriter) WriteAPIWebsocketTimeline(apiWebsocketTimeline []byte) error {
	if w == nil || len(apiWebsocketTimeline) == 0 {
		return nil
	}
	w.apiWebsocketTime = bytes.Clone(apiWebsocketTimeline)
	return nil
}

func (w *homeStreamingLogWriter) SetFirstChunkTimestamp(timestamp time.Time) {
	if w == nil {
		return
	}
	if !timestamp.IsZero() {
		w.firstChunkTS = timestamp
		w.apiResponseTS = timestamp
	}
}

func (w *homeStreamingLogWriter) Close() error {
	if w == nil {
		return nil
	}

	if w.chunkChan != nil {
		close(w.chunkChan)
		<-w.doneChan
		w.chunkChan = nil
	}

	client := currentHomeRequestLogClient()
	if client == nil || !client.HeartbeatOK() {
		return nil
	}

	responsePayload := w.responseBody.Bytes()

	var buf bytes.Buffer
	upstreamTransport := inferUpstreamTransport(w.apiRequest, nil, w.apiResponse, nil, w.apiWebsocketTime, nil, nil)
	if errWrite := writeRequestInfoWithBody(&buf, w.url, w.method, w.requestHeaders, w.requestBody, "", w.timestamp, "http", upstreamTransport, true); errWrite != nil {
		return errWrite
	}
	if errWrite := writeAPISection(&buf, "=== API WEBSOCKET TIMELINE ===\n", "=== API WEBSOCKET TIMELINE", w.apiWebsocketTime, time.Time{}); errWrite != nil {
		return errWrite
	}
	if errWrite := writeAPISection(&buf, "=== API REQUEST ===\n", "=== API REQUEST", w.apiRequest, time.Time{}); errWrite != nil {
		return errWrite
	}
	if errWrite := writeAPISection(&buf, "=== API RESPONSE ===\n", "=== API RESPONSE", w.apiResponse, w.apiResponseTS); errWrite != nil {
		return errWrite
	}
	if errWrite := writeResponseSection(&buf, w.responseStatus, w.statusWritten, w.responseHeaders, bytes.NewReader(responsePayload), nil, false); errWrite != nil {
		return errWrite
	}

	payload := homeRequestLogPayload{
		Headers:    sanitizeHeadersForExternalLog(w.requestHeaders),
		RequestID:  w.requestID,
		RequestLog: sanitizeLogTextForExternalLog(buf.String()),
	}
	raw, errMarshal := json.Marshal(&payload)
	if errMarshal != nil {
		return errMarshal
	}
	return client.RPushRequestLog(context.Background(), raw)
}
