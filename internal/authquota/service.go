package authquota

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/runtime/geminicli"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/proxyutil"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const defaultAPICallTimeout = 60 * time.Second

const (
	ClassificationOK             = coreauth.ClassificationOK
	ClassificationNoQuota        = coreauth.ClassificationNoQuota
	ClassificationInvalidated401 = coreauth.ClassificationInvalidated401
	ClassificationAPIError       = coreauth.ClassificationAPIError
	ClassificationRequestFailed  = coreauth.ClassificationRequestFailed
	ClassificationUnsupported    = coreauth.ClassificationUnsupported
	ClassificationUnknown        = coreauth.ClassificationUnknown
)

const (
	antigravityDefaultProjectID = "bamboo-precept-lgxtn"
	antigravityQuotaURLPrimary  = "https://daily-cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels"
	antigravityQuotaURLSandbox  = "https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:fetchAvailableModels"
	antigravityQuotaURLDefault  = "https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels"
	geminiCLIQuotaURL           = "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota"
	geminiCLICodeAssistURL      = "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist"
	claudeUsageURL              = "https://api.anthropic.com/api/oauth/usage"
	codexUsageURL               = "https://chatgpt.com/backend-api/wham/usage"
	kimiUsageURL                = "https://api.kimi.com/coding/v1/usages"
)

const (
	geminiOAuthClientID     = "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
	geminiOAuthClientSecret = "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"

	antigravityOAuthClientID     = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"
	antigravityOAuthClientSecret = "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"
)

var geminiOAuthScopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/userinfo.profile",
}

var antigravityOAuthTokenURL = "https://oauth2.googleapis.com/token"

var antigravityQuotaURLs = []string{
	antigravityQuotaURLPrimary,
	antigravityQuotaURLSandbox,
	antigravityQuotaURLDefault,
}

var claudeWindows = []struct {
	Key   string
	ID    string
	Label string
}{
	{Key: "five_hour", ID: "five-hour", Label: "five_hour"},
	{Key: "seven_day", ID: "seven-day", Label: "seven_day"},
	{Key: "seven_day_oauth_apps", ID: "seven-day-oauth-apps", Label: "seven_day_oauth_apps"},
	{Key: "seven_day_opus", ID: "seven-day-opus", Label: "seven_day_opus"},
	{Key: "seven_day_sonnet", ID: "seven-day-sonnet", Label: "seven_day_sonnet"},
	{Key: "seven_day_cowork", ID: "seven-day-cowork", Label: "seven_day_cowork"},
	{Key: "iguana_necktie", ID: "iguana-necktie", Label: "iguana_necktie"},
}

var antigravityGroups = []struct {
	ID          string
	Label       string
	Identifiers []string
}{
	{ID: "claude-gpt", Label: "Claude/GPT", Identifiers: []string{"claude-sonnet-4-6", "claude-opus-4-6-thinking", "gpt-oss-120b-medium"}},
	{ID: "gemini-3-pro", Label: "Gemini 3 Pro", Identifiers: []string{"gemini-3-pro-high", "gemini-3-pro-low"}},
	{ID: "gemini-3-1-pro-series", Label: "Gemini 3.1 Pro Series", Identifiers: []string{"gemini-3.1-pro-high", "gemini-3.1-pro-low"}},
	{ID: "gemini-2-5-flash", Label: "Gemini 2.5 Flash", Identifiers: []string{"gemini-2.5-flash", "gemini-2.5-flash-thinking"}},
	{ID: "gemini-2-5-flash-lite", Label: "Gemini 2.5 Flash Lite", Identifiers: []string{"gemini-2.5-flash-lite"}},
	{ID: "gemini-2-5-cu", Label: "Gemini 2.5 CU", Identifiers: []string{"rev19-uic3-1p"}},
	{ID: "gemini-3-flash", Label: "Gemini 3 Flash", Identifiers: []string{"gemini-3-flash"}},
	{ID: "gemini-image", Label: "gemini-3.1-flash-image", Identifiers: []string{"gemini-3.1-flash-image"}},
}

type Options struct {
	ConfigProvider    func() *config.Config
	TransportProvider func(auth *coreauth.Auth, cfg *config.Config) http.RoundTripper
}

type Service struct {
	configProvider    func() *config.Config
	transportProvider func(auth *coreauth.Auth, cfg *config.Config) http.RoundTripper
}

type apiCallRequest struct {
	Method string
	URL    string
	Header map[string]string
	Data   string
}

type apiCallResponse struct {
	StatusCode int
	Header     map[string][]string
	Body       string
}

type window struct {
	ID               string
	Label            string
	UsedPercent      *int
	RemainingPercent *int
	RemainingAmount  *int
	ResetAt          *int64
	ResetAfter       *int
	ResetTime        string
	Limit            *int
	Used             *int
	ResetHint        string
	ModelIDs         []string
}

// NewService constructs a runtime quota inspector for providers with real quota APIs.
func NewService(opts Options) *Service {
	configProvider := opts.ConfigProvider
	if configProvider == nil {
		configProvider = func() *config.Config { return nil }
	}
	return &Service{
		configProvider:    configProvider,
		transportProvider: opts.TransportProvider,
	}
}

// Supports reports whether the auth can be checked using a real quota endpoint.
func (s *Service) Supports(auth *coreauth.Auth) bool {
	if auth == nil || isRuntimeOnlyAuth(auth) {
		return false
	}
	switch normalizeProvider(auth.Provider) {
	case "antigravity", "claude", "codex", "gemini-cli", "kimi":
		return true
	default:
		return false
	}
}

// Check inspects the auth against its provider's real quota endpoint.
func (s *Service) Check(ctx context.Context, auth *coreauth.Auth) (coreauth.QuotaCheckResult, error) {
	if !s.Supports(auth) {
		return coreauth.QuotaCheckResult{Classification: ClassificationUnsupported}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	switch normalizeProvider(auth.Provider) {
	case "codex":
		return s.checkCodex(ctx, auth)
	case "claude":
		return s.checkClaude(ctx, auth)
	case "gemini-cli":
		return s.checkGeminiCLI(ctx, auth)
	case "kimi":
		return s.checkKimi(ctx, auth)
	case "antigravity":
		return s.checkAntigravity(ctx, auth)
	default:
		return coreauth.QuotaCheckResult{Classification: ClassificationUnsupported}, nil
	}
}

func (s *Service) checkCodex(ctx context.Context, auth *coreauth.Auth) (coreauth.QuotaCheckResult, error) {
	accountID := resolveCodexAccountID(auth)
	if accountID == "" {
		return finalizeResult(ClassificationRequestFailed, nil, "missing chatgpt account id", 0), nil
	}

	resp, err := s.executeAPICall(ctx, auth, apiCallRequest{
		Method: "GET",
		URL:    codexUsageURL,
		Header: map[string]string{
			"Authorization":      "Bearer $TOKEN$",
			"Content-Type":       "application/json",
			"User-Agent":         "codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal",
			"Chatgpt-Account-Id": accountID,
		},
	})
	if err != nil {
		return coreauth.QuotaCheckResult{}, err
	}
	classification, message, statusCode := classifyAPIResponse(resp)
	payload := gjson.Parse(resp.Body)
	windows := extractCodexWindows(payload)
	remaining := minRemaining(windows)
	if classification == "" && len(windows) == 0 {
		classification = ClassificationAPIError
		message = "empty codex quota payload"
	}
	if classification == "" {
		classification = classificationFromRemainingPercent(remaining)
	}
	return finalizeResult(classification, remaining, message, statusCode), nil
}

func (s *Service) checkClaude(ctx context.Context, auth *coreauth.Auth) (coreauth.QuotaCheckResult, error) {
	resp, err := s.executeAPICall(ctx, auth, apiCallRequest{
		Method: "GET",
		URL:    claudeUsageURL,
		Header: map[string]string{
			"Authorization":  "Bearer $TOKEN$",
			"Content-Type":   "application/json",
			"anthropic-beta": "oauth-2025-04-20",
		},
	})
	if err != nil {
		return coreauth.QuotaCheckResult{}, err
	}
	classification, message, statusCode := classifyAPIResponse(resp)
	payload := gjson.Parse(resp.Body)
	windows := extractClaudeWindows(payload)
	remaining := minRemaining(windows)
	if classification == "" && len(windows) == 0 {
		classification = ClassificationAPIError
		message = "empty claude quota payload"
	}
	if classification == "" {
		classification = classificationFromRemainingPercent(remaining)
	}
	return finalizeResult(classification, remaining, message, statusCode), nil
}

func (s *Service) checkGeminiCLI(ctx context.Context, auth *coreauth.Auth) (coreauth.QuotaCheckResult, error) {
	projectID := resolveGeminiCLIProjectID(auth)
	if projectID == "" {
		return finalizeResult(ClassificationRequestFailed, nil, "missing project id", 0), nil
	}

	resp, err := s.executeAPICall(ctx, auth, apiCallRequest{
		Method: "POST",
		URL:    geminiCLIQuotaURL,
		Header: map[string]string{
			"Authorization": "Bearer $TOKEN$",
			"Content-Type":  "application/json",
		},
		Data: fmt.Sprintf(`{"project":%q}`, projectID),
	})
	if err != nil {
		return coreauth.QuotaCheckResult{}, err
	}
	classification, message, statusCode := classifyAPIResponse(resp)
	payload := gjson.Parse(resp.Body)
	buckets := extractGeminiBuckets(payload)
	remaining := minRemaining(buckets)
	if classification == "" && len(buckets) == 0 {
		classification = ClassificationAPIError
		message = "empty gemini cli quota payload"
	}
	if classification == "" {
		classification = classificationFromRemainingPercent(remaining)
	}
	return finalizeResult(classification, remaining, message, statusCode), nil
}

func (s *Service) checkKimi(ctx context.Context, auth *coreauth.Auth) (coreauth.QuotaCheckResult, error) {
	resp, err := s.executeAPICall(ctx, auth, apiCallRequest{
		Method: "GET",
		URL:    kimiUsageURL,
		Header: map[string]string{
			"Authorization": "Bearer $TOKEN$",
			"Content-Type":  "application/json",
		},
	})
	if err != nil {
		return coreauth.QuotaCheckResult{}, err
	}
	classification, message, statusCode := classifyAPIResponse(resp)
	payload := gjson.Parse(resp.Body)
	rows := extractKimiRows(payload)
	remaining := minRemaining(rows)
	if classification == "" && len(rows) == 0 {
		classification = ClassificationAPIError
		message = "empty kimi quota payload"
	}
	if classification == "" {
		classification = classificationFromRemainingPercent(remaining)
	}
	return finalizeResult(classification, remaining, message, statusCode), nil
}

func (s *Service) checkAntigravity(ctx context.Context, auth *coreauth.Auth) (coreauth.QuotaCheckResult, error) {
	projectID := resolveAntigravityProjectID(auth)
	var lastResp apiCallResponse
	for _, urlStr := range antigravityQuotaURLs {
		resp, err := s.executeAPICall(ctx, auth, apiCallRequest{
			Method: "POST",
			URL:    urlStr,
			Header: map[string]string{
				"Authorization": "Bearer $TOKEN$",
				"Content-Type":  "application/json",
			},
			Data: fmt.Sprintf(`{"project":%q}`, projectID),
		})
		if err != nil {
			return coreauth.QuotaCheckResult{}, err
		}
		lastResp = resp
		classification, message, statusCode := classifyAPIResponse(resp)
		if classification == ClassificationInvalidated401 || classification == ClassificationRequestFailed {
			return finalizeResult(classification, nil, message, statusCode), nil
		}
		payload := gjson.Parse(resp.Body)
		groups := extractAntigravityGroups(payload)
		remaining := minRemaining(groups)
		if len(groups) > 0 {
			return finalizeResult(classificationFromRemainingPercent(remaining), remaining, "", resp.StatusCode), nil
		}
	}
	classification, message, statusCode := classifyAPIResponse(lastResp)
	if classification == "" {
		classification = ClassificationAPIError
		message = "empty antigravity quota payload"
	}
	return finalizeResult(classification, nil, message, statusCode), nil
}

func (s *Service) executeAPICall(ctx context.Context, auth *coreauth.Auth, body apiCallRequest) (apiCallResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	method := strings.ToUpper(strings.TrimSpace(body.Method))
	if method == "" {
		return apiCallResponse{}, fmt.Errorf("missing method")
	}
	urlStr := strings.TrimSpace(body.URL)
	if urlStr == "" {
		return apiCallResponse{}, fmt.Errorf("missing url")
	}
	parsedURL, err := url.Parse(urlStr)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return apiCallResponse{}, fmt.Errorf("invalid url")
	}

	headers := make(map[string]string, len(body.Header))
	for key, value := range body.Header {
		headers[key] = value
	}

	var token string
	var tokenResolved bool
	for key, value := range headers {
		if !strings.Contains(value, "$TOKEN$") {
			continue
		}
		if !tokenResolved {
			token, err = s.resolveTokenForAuth(ctx, auth)
			if err != nil {
				return apiCallResponse{}, fmt.Errorf("auth token refresh failed: %w", err)
			}
			tokenResolved = true
		}
		if token == "" {
			return apiCallResponse{}, fmt.Errorf("auth token not found")
		}
		headers[key] = strings.ReplaceAll(value, "$TOKEN$", token)
	}

	var requestBody io.Reader
	if body.Data != "" {
		requestBody = strings.NewReader(body.Data)
	}
	req, err := http.NewRequestWithContext(ctx, method, urlStr, requestBody)
	if err != nil {
		return apiCallResponse{}, fmt.Errorf("failed to build request: %w", err)
	}
	for key, value := range headers {
		if strings.EqualFold(key, "host") {
			req.Host = strings.TrimSpace(value)
			continue
		}
		req.Header.Set(key, value)
	}

	httpClient := &http.Client{
		Timeout:   defaultAPICallTimeout,
		Transport: s.transportFor(auth),
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return apiCallResponse{}, fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.WithError(closeErr).Warn("failed to close quota check response body")
		}
	}()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return apiCallResponse{}, fmt.Errorf("failed to read response: %w", err)
	}
	return apiCallResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		Body:       string(bodyBytes),
	}, nil
}

func (s *Service) transportFor(auth *coreauth.Auth) http.RoundTripper {
	cfg := s.configProvider()
	if s.transportProvider != nil {
		if rt := s.transportProvider(auth, cfg); rt != nil {
			return rt
		}
	}
	if auth != nil {
		if rt := buildProxyTransport(strings.TrimSpace(auth.ProxyURL)); rt != nil {
			return rt
		}
	}
	if cfg != nil {
		if rt := buildProxyTransport(strings.TrimSpace(cfg.ProxyURL)); rt != nil {
			return rt
		}
	}
	return proxyutil.NewDirectTransport()
}

func (s *Service) resolveTokenForAuth(ctx context.Context, auth *coreauth.Auth) (string, error) {
	if auth == nil {
		return "", nil
	}
	switch normalizeProvider(auth.Provider) {
	case "gemini-cli":
		return s.refreshGeminiOAuthAccessToken(ctx, auth)
	case "antigravity":
		return s.refreshAntigravityOAuthAccessToken(ctx, auth)
	default:
		return tokenValueForAuth(auth), nil
	}
}

func (s *Service) refreshGeminiOAuthAccessToken(ctx context.Context, auth *coreauth.Auth) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	metadata, updater := geminiOAuthMetadata(auth)
	if len(metadata) == 0 {
		return "", fmt.Errorf("gemini oauth metadata missing")
	}

	base := make(map[string]any)
	if tokenRaw, ok := metadata["token"].(map[string]any); ok && tokenRaw != nil {
		base = cloneMap(tokenRaw)
	}

	var token oauth2.Token
	if len(base) > 0 {
		if raw, err := json.Marshal(base); err == nil {
			_ = json.Unmarshal(raw, &token)
		}
	}

	if token.AccessToken == "" {
		token.AccessToken = stringValue(metadata, "access_token")
	}
	if token.RefreshToken == "" {
		token.RefreshToken = stringValue(metadata, "refresh_token")
	}
	if token.TokenType == "" {
		token.TokenType = stringValue(metadata, "token_type")
	}
	if token.Expiry.IsZero() {
		if expiry := stringValue(metadata, "expiry"); expiry != "" {
			if ts, err := time.Parse(time.RFC3339, expiry); err == nil {
				token.Expiry = ts
			}
		}
	}

	conf := &oauth2.Config{
		ClientID:     geminiOAuthClientID,
		ClientSecret: geminiOAuthClientSecret,
		Scopes:       geminiOAuthScopes,
		Endpoint:     google.Endpoint,
	}

	httpClient := &http.Client{
		Timeout:   defaultAPICallTimeout,
		Transport: s.transportFor(auth),
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, httpClient)
	currentToken, err := conf.TokenSource(ctx, &token).Token()
	if err != nil {
		return "", err
	}

	merged := buildOAuthTokenMap(base, currentToken)
	if updater != nil {
		updater(buildOAuthTokenFields(currentToken, merged))
	}
	return strings.TrimSpace(currentToken.AccessToken), nil
}

func (s *Service) refreshAntigravityOAuthAccessToken(ctx context.Context, auth *coreauth.Auth) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if auth == nil || len(auth.Metadata) == 0 {
		return "", fmt.Errorf("antigravity oauth metadata missing")
	}
	current := strings.TrimSpace(tokenValueFromMetadata(auth.Metadata))
	if current != "" && !antigravityTokenNeedsRefresh(auth.Metadata) {
		return current, nil
	}
	refreshToken := stringValue(auth.Metadata, "refresh_token")
	if refreshToken == "" {
		return "", fmt.Errorf("antigravity refresh token missing")
	}

	form := url.Values{}
	form.Set("client_id", antigravityOAuthClientID)
	form.Set("client_secret", antigravityOAuthClientSecret)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, antigravityOAuthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	httpClient := &http.Client{
		Timeout:   defaultAPICallTimeout,
		Transport: s.transportFor(auth),
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.WithError(closeErr).Warn("failed to close antigravity token response body")
		}
	}()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("antigravity oauth token refresh failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err = json.Unmarshal(bodyBytes, &tokenResp); err != nil {
		return "", err
	}
	if strings.TrimSpace(tokenResp.AccessToken) == "" {
		return "", fmt.Errorf("antigravity oauth token refresh returned empty access_token")
	}

	if auth.Metadata == nil {
		auth.Metadata = make(map[string]any)
	}
	now := time.Now()
	auth.Metadata["access_token"] = strings.TrimSpace(tokenResp.AccessToken)
	if strings.TrimSpace(tokenResp.RefreshToken) != "" {
		auth.Metadata["refresh_token"] = strings.TrimSpace(tokenResp.RefreshToken)
	}
	if tokenResp.ExpiresIn > 0 {
		auth.Metadata["expires_in"] = tokenResp.ExpiresIn
		auth.Metadata["timestamp"] = now.UnixMilli()
		auth.Metadata["expired"] = now.Add(time.Duration(tokenResp.ExpiresIn) * time.Second).Format(time.RFC3339)
	}
	auth.Metadata["type"] = "antigravity"
	return strings.TrimSpace(tokenResp.AccessToken), nil
}

func finalizeResult(classification string, remaining *int, message string, statusCode int) coreauth.QuotaCheckResult {
	if classification == "" {
		classification = ClassificationUnknown
	}
	return coreauth.QuotaCheckResult{
		Classification:   classification,
		RemainingPercent: remaining,
		ErrorMessage:     strings.TrimSpace(message),
		StatusCode:       statusCode,
		Exhausted:        remaining != nil && *remaining <= 0 || classification == ClassificationNoQuota,
	}
}

func classifyAPIResponse(resp apiCallResponse) (string, string, int) {
	body := gjson.Parse(resp.Body)
	statusCode := resp.StatusCode
	if statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices {
		return "", "", statusCode
	}
	errorMessage := extractAPIErrorMessage(body, resp.Body)
	switch {
	case statusCode == http.StatusUnauthorized:
		return ClassificationInvalidated401, errorMessage, statusCode
	case looksLikeNoQuotaError(body, errorMessage, statusCode):
		return ClassificationNoQuota, errorMessage, statusCode
	case statusCode >= http.StatusBadRequest:
		return ClassificationAPIError, errorMessage, statusCode
	default:
		return "", errorMessage, statusCode
	}
}

func extractAPIErrorMessage(body gjson.Result, raw string) string {
	for _, candidate := range []string{
		body.Get("error.message").String(),
		body.Get("message").String(),
		body.Get("error").String(),
		raw,
	} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func looksLikeNoQuotaError(body gjson.Result, message string, statusCode int) bool {
	if statusCode == http.StatusTooManyRequests {
		return true
	}
	joined := strings.ToLower(strings.Join([]string{
		body.Get("error.code").String(),
		body.Get("error.type").String(),
		message,
	}, " "))
	return strings.Contains(joined, "usage_limit_reached") ||
		strings.Contains(joined, "usage limit has been reached") ||
		(statusCode >= http.StatusBadRequest && strings.Contains(joined, "quota"))
}

func classificationFromRemainingPercent(remaining *int) string {
	if remaining != nil && *remaining <= 0 {
		return ClassificationNoQuota
	}
	return ClassificationOK
}

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func resolveCodexAccountID(auth *coreauth.Auth) string {
	if auth == nil {
		return ""
	}
	if auth.Metadata != nil {
		for _, key := range []string{"chatgpt_account_id", "chatgptAccountId"} {
			if value := strings.TrimSpace(stringValueAny(auth.Metadata[key])); value != "" {
				return value
			}
		}
	}
	if auth.Attributes != nil {
		for _, key := range []string{"chatgpt_account_id", "chatgptAccountId"} {
			if value := strings.TrimSpace(auth.Attributes[key]); value != "" {
				return value
			}
		}
	}
	return ""
}

func resolveGeminiCLIProjectID(auth *coreauth.Auth) string {
	if auth == nil {
		return ""
	}
	if auth.Metadata != nil {
		for _, key := range []string{"project_id", "projectId"} {
			if value := strings.TrimSpace(stringValueAny(auth.Metadata[key])); value != "" {
				return value
			}
		}
	}
	_, account := auth.AccountInfo()
	start := strings.LastIndex(account, "(")
	end := strings.LastIndex(account, ")")
	if start >= 0 && end > start {
		return strings.TrimSpace(account[start+1 : end])
	}
	return ""
}

func resolveAntigravityProjectID(auth *coreauth.Auth) string {
	if auth != nil && auth.Metadata != nil {
		for _, key := range []string{"project_id", "projectId"} {
			if value := strings.TrimSpace(stringValueAny(auth.Metadata[key])); value != "" {
				return value
			}
		}
	}
	return antigravityDefaultProjectID
}

func extractCodexWindows(payload gjson.Result) []window {
	definitions := []struct {
		ID    string
		Label string
		Path  string
	}{
		{ID: "five-hour", Label: "five_hour", Path: "rate_limit.primary_window"},
		{ID: "weekly", Label: "weekly", Path: "rate_limit.secondary_window"},
		{ID: "code-review-five-hour", Label: "code_review_five_hour", Path: "code_review_rate_limit.primary_window"},
		{ID: "code-review-weekly", Label: "code_review_weekly", Path: "code_review_rate_limit.secondary_window"},
	}

	windows := make([]window, 0, len(definitions))
	for _, def := range definitions {
		item := payload.Get(def.Path)
		if !item.Exists() {
			continue
		}
		usedPercent := intPtrFromGJSON(item, "used_percent", "usedPercent")
		windows = append(windows, window{
			ID:               def.ID,
			Label:            def.Label,
			UsedPercent:      usedPercent,
			RemainingPercent: remainingPercentFromUsedPercent(usedPercent),
			ResetAfter:       intPtrFromGJSON(item, "reset_after_seconds", "resetAfterSeconds"),
			ResetAt:          int64PtrFromGJSON(item, "reset_at", "resetAt"),
		})
	}
	return windows
}

func extractClaudeWindows(payload gjson.Result) []window {
	windows := make([]window, 0, len(claudeWindows))
	for _, def := range claudeWindows {
		item := payload.Get(def.Key)
		if !item.Exists() {
			continue
		}
		usedPercent := intPtrFromGJSON(item, "utilization")
		windows = append(windows, window{
			ID:               def.ID,
			Label:            def.Label,
			UsedPercent:      usedPercent,
			RemainingPercent: remainingPercentFromUsedPercent(usedPercent),
			ResetTime:        strings.TrimSpace(item.Get("resets_at").String()),
		})
	}
	return windows
}

func extractGeminiBuckets(payload gjson.Result) []window {
	buckets := make([]window, 0)
	for _, bucket := range payload.Get("buckets").Array() {
		modelID := strings.TrimSpace(firstNonEmptyGJSON(bucket, "modelId", "model_id"))
		if modelID == "" {
			continue
		}
		remainingPercent := percentageFromFraction(float64PtrFromGJSON(bucket, "remainingFraction", "remaining_fraction"))
		remainingAmount := intPtrFromGJSON(bucket, "remainingAmount", "remaining_amount")
		if remainingPercent == nil && remainingAmount != nil && *remainingAmount <= 0 {
			zero := 0
			remainingPercent = &zero
		}
		buckets = append(buckets, window{
			ID:               modelID,
			Label:            modelID,
			RemainingPercent: remainingPercent,
			RemainingAmount:  remainingAmount,
			ResetTime:        strings.TrimSpace(firstNonEmptyGJSON(bucket, "resetTime", "reset_time")),
			ModelIDs:         []string{modelID},
		})
	}
	return buckets
}

func extractKimiRows(payload gjson.Result) []window {
	rows := make([]window, 0)
	if usage := payload.Get("usage"); usage.Exists() {
		if row := buildKimiRow("summary", "weekly_limit", usage); row != nil {
			rows = append(rows, *row)
		}
	}
	for index, item := range payload.Get("limits").Array() {
		detail := item.Get("detail")
		if !detail.Exists() {
			detail = item
		}
		label := strings.TrimSpace(firstNonEmptyGJSON(detail, "name", "title"))
		if label == "" {
			label = fmt.Sprintf("limit_%d", index+1)
		}
		row := buildKimiRow(fmt.Sprintf("limit-%d", index), label, detail)
		if row == nil {
			continue
		}
		if row.ResetHint == "" {
			row.ResetHint = kimiResetHint(item.Get("window"))
		}
		rows = append(rows, *row)
	}
	return rows
}

func buildKimiRow(id, label string, payload gjson.Result) *window {
	limit := intPtrFromGJSON(payload, "limit")
	used := intPtrFromGJSON(payload, "used")
	if used == nil {
		remaining := intPtrFromGJSON(payload, "remaining")
		if limit != nil && remaining != nil {
			value := *limit - *remaining
			used = &value
		}
	}
	if limit == nil && used == nil {
		return nil
	}

	var remainingPercent *int
	if limit != nil && *limit > 0 {
		usedValue := 0
		if used != nil {
			usedValue = *used
		}
		value := maxInt(0, minInt(100, int(float64((*limit-usedValue)*100)/float64(*limit)+0.5)))
		remainingPercent = &value
	} else if used != nil && *used > 0 {
		zero := 0
		remainingPercent = &zero
	}

	return &window{
		ID:               id,
		Label:            label,
		Limit:            limit,
		Used:             used,
		RemainingPercent: remainingPercent,
		ResetHint:        kimiResetHint(payload),
	}
}

func kimiResetHint(payload gjson.Result) string {
	if !payload.Exists() {
		return ""
	}
	for _, key := range []string{"reset_at", "resetAt", "reset_time", "resetTime"} {
		value := strings.TrimSpace(payload.Get(key).String())
		if value == "" {
			continue
		}
		if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return durationHint(time.Until(ts))
		}
		if ts, err := time.Parse(time.RFC3339, value); err == nil {
			return durationHint(time.Until(ts))
		}
	}
	for _, key := range []string{"reset_in", "resetIn", "ttl"} {
		value := payload.Get(key).Int()
		if value > 0 {
			return durationHint(time.Duration(value) * time.Second)
		}
	}
	return ""
}

func extractAntigravityGroups(payload gjson.Result) []window {
	models := payload.Get("models")
	if !models.Exists() {
		return nil
	}
	findModel := func(identifier string) *window {
		direct := models.Get(identifier)
		if direct.Exists() {
			return antigravityWindowFromResult(identifier, direct)
		}
		var found *window
		models.ForEach(func(key, value gjson.Result) bool {
			displayName := strings.TrimSpace(firstNonEmptyGJSON(value, "displayName", "display_name"))
			if strings.EqualFold(displayName, identifier) {
				found = antigravityWindowFromResult(key.String(), value)
				return false
			}
			return true
		})
		return found
	}

	groups := make([]window, 0, len(antigravityGroups))
	for _, group := range antigravityGroups {
		matches := make([]window, 0, len(group.Identifiers))
		for _, identifier := range group.Identifiers {
			if match := findModel(identifier); match != nil {
				matches = append(matches, *match)
			}
		}
		if len(matches) == 0 {
			continue
		}

		modelIDs := make([]string, 0, len(matches))
		var remaining *int
		resetTime := ""
		for _, match := range matches {
			modelIDs = append(modelIDs, match.ID)
			if match.RemainingPercent != nil {
				if remaining == nil || *match.RemainingPercent < *remaining {
					value := *match.RemainingPercent
					remaining = &value
				}
			}
			if resetTime == "" && match.ResetTime != "" {
				resetTime = match.ResetTime
			}
		}

		groups = append(groups, window{
			ID:               group.ID,
			Label:            group.Label,
			RemainingPercent: remaining,
			ResetTime:        resetTime,
			ModelIDs:         modelIDs,
		})
	}
	return groups
}

func antigravityWindowFromResult(modelID string, payload gjson.Result) *window {
	quotaInfo := payload.Get("quotaInfo")
	if !quotaInfo.Exists() {
		quotaInfo = payload.Get("quota_info")
	}
	remainingPercent := percentageFromFraction(float64PtrFromGJSON(quotaInfo, "remainingFraction", "remaining_fraction", "remaining"))
	resetTime := strings.TrimSpace(firstNonEmptyGJSON(quotaInfo, "resetTime", "reset_time"))
	if remainingPercent == nil && resetTime != "" {
		zero := 0
		remainingPercent = &zero
	}
	if remainingPercent == nil {
		return nil
	}
	return &window{
		ID:               modelID,
		Label:            modelID,
		RemainingPercent: remainingPercent,
		ResetTime:        resetTime,
	}
}

func minRemaining(items []window) *int {
	var remaining *int
	for _, item := range items {
		if item.RemainingPercent == nil {
			continue
		}
		if remaining == nil || *item.RemainingPercent < *remaining {
			value := *item.RemainingPercent
			remaining = &value
		}
	}
	return remaining
}

func remainingPercentFromUsedPercent(usedPercent *int) *int {
	if usedPercent == nil {
		return nil
	}
	value := maxInt(0, minInt(100, 100-*usedPercent))
	return &value
}

func percentageFromFraction(value *float64) *int {
	if value == nil {
		return nil
	}
	normalized := *value
	if normalized < 0 {
		normalized = 0
	}
	if normalized > 1 {
		normalized = 1
	}
	percentage := int(normalized*100 + 0.5)
	return &percentage
}

func durationHint(duration time.Duration) string {
	if duration <= 0 {
		return ""
	}
	totalMinutes := int(duration / time.Minute)
	hours := totalMinutes / 60
	minutes := totalMinutes % 60
	switch {
	case hours > 0 && minutes > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	case minutes > 0:
		return fmt.Sprintf("%dm", minutes)
	default:
		return "<1m"
	}
}

func firstNonEmptyGJSON(result gjson.Result, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(result.Get(key).String()); value != "" {
			return value
		}
	}
	return ""
}

func intPtrFromGJSON(result gjson.Result, keys ...string) *int {
	for _, key := range keys {
		value := result.Get(key)
		if value.Exists() {
			converted := int(value.Int())
			return &converted
		}
	}
	return nil
}

func int64PtrFromGJSON(result gjson.Result, keys ...string) *int64 {
	for _, key := range keys {
		value := result.Get(key)
		if value.Exists() {
			converted := value.Int()
			return &converted
		}
	}
	return nil
}

func float64PtrFromGJSON(result gjson.Result, keys ...string) *float64 {
	for _, key := range keys {
		value := result.Get(key)
		if value.Exists() {
			converted := value.Float()
			return &converted
		}
	}
	return nil
}

func stringValueAny(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

func tokenValueForAuth(auth *coreauth.Auth) string {
	if auth == nil {
		return ""
	}
	if v := tokenValueFromMetadata(auth.Metadata); v != "" {
		return v
	}
	if auth.Attributes != nil {
		if v := strings.TrimSpace(auth.Attributes["api_key"]); v != "" {
			return v
		}
	}
	if shared := geminicli.ResolveSharedCredential(auth.Runtime); shared != nil {
		if v := tokenValueFromMetadata(shared.MetadataSnapshot()); v != "" {
			return v
		}
	}
	return ""
}

func geminiOAuthMetadata(auth *coreauth.Auth) (map[string]any, func(map[string]any)) {
	if auth == nil {
		return nil, nil
	}
	if shared := geminicli.ResolveSharedCredential(auth.Runtime); shared != nil {
		snapshot := shared.MetadataSnapshot()
		return snapshot, func(fields map[string]any) { shared.MergeMetadata(fields) }
	}
	return auth.Metadata, func(fields map[string]any) {
		if auth.Metadata == nil {
			auth.Metadata = make(map[string]any)
		}
		for k, v := range fields {
			auth.Metadata[k] = v
		}
	}
}

func stringValue(metadata map[string]any, key string) string {
	if len(metadata) == 0 || key == "" {
		return ""
	}
	if v, ok := metadata[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func buildOAuthTokenMap(base map[string]any, tok *oauth2.Token) map[string]any {
	merged := cloneMap(base)
	if merged == nil {
		merged = make(map[string]any)
	}
	if tok == nil {
		return merged
	}
	if raw, err := json.Marshal(tok); err == nil {
		var tokenMap map[string]any
		if err = json.Unmarshal(raw, &tokenMap); err == nil {
			for k, v := range tokenMap {
				merged[k] = v
			}
		}
	}
	return merged
}

func buildOAuthTokenFields(tok *oauth2.Token, merged map[string]any) map[string]any {
	fields := make(map[string]any, 5)
	if tok != nil && tok.AccessToken != "" {
		fields["access_token"] = tok.AccessToken
	}
	if tok != nil && tok.TokenType != "" {
		fields["token_type"] = tok.TokenType
	}
	if tok != nil && tok.RefreshToken != "" {
		fields["refresh_token"] = tok.RefreshToken
	}
	if tok != nil && !tok.Expiry.IsZero() {
		fields["expiry"] = tok.Expiry.Format(time.RFC3339)
	}
	if len(merged) > 0 {
		fields["token"] = cloneMap(merged)
	}
	return fields
}

func tokenValueFromMetadata(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	if v, ok := metadata["accessToken"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if v, ok := metadata["access_token"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if tokenRaw, ok := metadata["token"]; ok && tokenRaw != nil {
		switch typed := tokenRaw.(type) {
		case string:
			if v := strings.TrimSpace(typed); v != "" {
				return v
			}
		case map[string]any:
			if v, ok := typed["access_token"].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
			if v, ok := typed["accessToken"].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		case map[string]string:
			if v := strings.TrimSpace(typed["access_token"]); v != "" {
				return v
			}
			if v := strings.TrimSpace(typed["accessToken"]); v != "" {
				return v
			}
		}
	}
	if v, ok := metadata["token"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if v, ok := metadata["id_token"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if v, ok := metadata["cookie"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return ""
}

func antigravityTokenNeedsRefresh(metadata map[string]any) bool {
	const skew = 30 * time.Second
	if metadata == nil {
		return true
	}
	if expStr, ok := metadata["expired"].(string); ok {
		if ts, err := time.Parse(time.RFC3339, strings.TrimSpace(expStr)); err == nil {
			return !ts.After(time.Now().Add(skew))
		}
	}
	expiresIn := int64Value(metadata["expires_in"])
	timestampMs := int64Value(metadata["timestamp"])
	if expiresIn > 0 && timestampMs > 0 {
		exp := time.UnixMilli(timestampMs).Add(time.Duration(expiresIn) * time.Second)
		return !exp.After(time.Now().Add(skew))
	}
	return true
}

func int64Value(raw any) int64 {
	switch typed := raw.(type) {
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case uint:
		return int64(typed)
	case uint32:
		return int64(typed)
	case uint64:
		return int64(typed)
	case float32:
		return int64(typed)
	case float64:
		return int64(typed)
	case json.Number:
		if i, err := typed.Int64(); err == nil {
			return i
		}
	case string:
		if s := strings.TrimSpace(typed); s != "" {
			if i, err := json.Number(s).Int64(); err == nil {
				return i
			}
		}
	}
	return 0
}

func buildProxyTransport(proxyStr string) http.RoundTripper {
	transport, _, err := proxyutil.BuildHTTPTransport(proxyStr)
	if err != nil {
		log.WithError(err).Debug("build proxy transport failed")
		return nil
	}
	return transport
}

func isRuntimeOnlyAuth(auth *coreauth.Auth) bool {
	if auth == nil || auth.Attributes == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(auth.Attributes["runtime_only"]), "true")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
