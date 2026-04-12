//go:build windows

package management

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestDownloadAuthFile_PreventsWindowsSlashTraversal(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	authDir := filepath.Join(tempDir, "auth")
	externalDir := filepath.Join(tempDir, "external")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		t.Fatalf("failed to create auth dir: %v", err)
	}
	if err := os.MkdirAll(externalDir, 0o700); err != nil {
		t.Fatalf("failed to create external dir: %v", err)
	}

	secretName := "secret.json"
	secretPath := filepath.Join(externalDir, secretName)
	if err := os.WriteFile(secretPath, []byte(`{"secret":true}`), 0o600); err != nil {
		t.Fatalf("failed to write external file: %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, nil)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/v0/management/auth-files/download?name="+url.QueryEscape("../external/"+secretName),
		nil,
	)
	h.DownloadAuthFile(ctx)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestDownloadAuthFilesArchive_PreventsWindowsSlashTraversal(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	authDir := filepath.Join(tempDir, "auth")
	externalDir := filepath.Join(tempDir, "external")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		t.Fatalf("failed to create auth dir: %v", err)
	}
	if err := os.MkdirAll(externalDir, 0o700); err != nil {
		t.Fatalf("failed to create external dir: %v", err)
	}

	secretName := "secret.json"
	secretPath := filepath.Join(externalDir, secretName)
	if err := os.WriteFile(secretPath, []byte(`{"secret":true}`), 0o600); err != nil {
		t.Fatalf("failed to write external file: %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, nil)

	body, err := json.Marshal(map[string]any{
		"names": []string{"../external/" + secretName},
	})
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(
		http.MethodPost,
		"/v0/management/auth-files/download-archive",
		bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	h.DownloadAuthFilesArchive(ctx)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}
