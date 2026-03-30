package managementasset

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func resetManagementAssetTestState() {
	lastUpdateCheckMu.Lock()
	lastUpdateCheckTime = time.Time{}
	lastUpdateCheckMu.Unlock()
}

func TestResolveManagementReleaseSource(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		repo         string
		wantURL      string
		wantFallback bool
		wantErr      bool
	}{
		{
			name:         "empty uses default repo and fallback",
			repo:         "",
			wantURL:      defaultManagementReleaseURL,
			wantFallback: true,
		},
		{
			name:         "official github repo keeps fallback",
			repo:         "https://github.com/router-for-me/Cli-Proxy-API-Management-Center",
			wantURL:      defaultManagementReleaseURL,
			wantFallback: true,
		},
		{
			name:         "official api repo keeps fallback",
			repo:         "https://api.github.com/repos/router-for-me/Cli-Proxy-API-Management-Center/releases/latest",
			wantURL:      defaultManagementReleaseURL,
			wantFallback: true,
		},
		{
			name:         "custom github repo disables fallback",
			repo:         "https://github.com/920293630/Cli-Proxy-API-Management-Center",
			wantURL:      "https://api.github.com/repos/920293630/Cli-Proxy-API-Management-Center/releases/latest",
			wantFallback: false,
		},
		{
			name:         "custom api repo disables fallback",
			repo:         "https://api.github.com/repos/920293630/Cli-Proxy-API-Management-Center/releases/latest",
			wantURL:      "https://api.github.com/repos/920293630/Cli-Proxy-API-Management-Center/releases/latest",
			wantFallback: false,
		},
		{
			name:    "invalid custom repo returns error",
			repo:    "not-a-url",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := resolveManagementReleaseSource(tc.repo)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.releaseURL != tc.wantURL {
				t.Fatalf("releaseURL = %q, want %q", got.releaseURL, tc.wantURL)
			}
			if got.allowFallback != tc.wantFallback {
				t.Fatalf("allowFallback = %v, want %v", got.allowFallback, tc.wantFallback)
			}
		})
	}
}

func TestEnsureLatestManagementHTML_CustomRepoSkipsFallbackWhenReleaseFetchFails(t *testing.T) {
	tempDir := t.TempDir()
	resetManagementAssetTestState()

	prevFetch := fetchLatestAssetFunc
	prevDownload := downloadAssetFunc
	t.Cleanup(func() {
		fetchLatestAssetFunc = prevFetch
		downloadAssetFunc = prevDownload
	})

	fetchLatestAssetFunc = func(context.Context, *http.Client, string) (*releaseAsset, string, error) {
		return nil, "", errors.New("release lookup failed")
	}

	fallbackCalls := 0
	downloadAssetFunc = func(context.Context, *http.Client, string) ([]byte, string, error) {
		fallbackCalls++
		return []byte("<html>fallback</html>"), "fallback-hash", nil
	}

	ok := EnsureLatestManagementHTML(context.Background(), tempDir, "", "https://github.com/920293630/Cli-Proxy-API-Management-Center")
	if ok {
		t.Fatal("expected sync to fail when custom repo has no release and no local file")
	}
	if fallbackCalls != 0 {
		t.Fatalf("expected no fallback download, got %d", fallbackCalls)
	}
	if _, err := os.Stat(filepath.Join(tempDir, ManagementFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no local management file, got err=%v", err)
	}
}

func TestEnsureLatestManagementHTML_CustomRepoKeepsExistingLocalFileWhenReleaseFetchFails(t *testing.T) {
	tempDir := t.TempDir()
	localPath := filepath.Join(tempDir, ManagementFileName)
	resetManagementAssetTestState()
	if err := os.WriteFile(localPath, []byte("<html>existing</html>"), 0o644); err != nil {
		t.Fatalf("seed local management file: %v", err)
	}

	prevFetch := fetchLatestAssetFunc
	prevDownload := downloadAssetFunc
	t.Cleanup(func() {
		fetchLatestAssetFunc = prevFetch
		downloadAssetFunc = prevDownload
	})

	fetchLatestAssetFunc = func(context.Context, *http.Client, string) (*releaseAsset, string, error) {
		return nil, "", errors.New("release lookup failed")
	}

	fallbackCalls := 0
	downloadAssetFunc = func(context.Context, *http.Client, string) ([]byte, string, error) {
		fallbackCalls++
		return []byte("<html>fallback</html>"), "fallback-hash", nil
	}

	ok := EnsureLatestManagementHTML(context.Background(), tempDir, "", "https://github.com/920293630/Cli-Proxy-API-Management-Center")
	if !ok {
		t.Fatal("expected sync to keep existing local file when custom repo release lookup fails")
	}
	if fallbackCalls != 0 {
		t.Fatalf("expected no fallback download, got %d", fallbackCalls)
	}
	body, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read local management file: %v", err)
	}
	if string(body) != "<html>existing</html>" {
		t.Fatalf("expected existing local file to remain unchanged, got %q", string(body))
	}
}

func TestEnsureLatestManagementHTML_DefaultRepoUsesFallbackWhenReleaseFetchFails(t *testing.T) {
	tempDir := t.TempDir()
	resetManagementAssetTestState()

	prevFetch := fetchLatestAssetFunc
	prevDownload := downloadAssetFunc
	t.Cleanup(func() {
		fetchLatestAssetFunc = prevFetch
		downloadAssetFunc = prevDownload
	})

	fetchLatestAssetFunc = func(context.Context, *http.Client, string) (*releaseAsset, string, error) {
		return nil, "", errors.New("release lookup failed")
	}

	fallbackCalls := 0
	downloadAssetFunc = func(context.Context, *http.Client, string) ([]byte, string, error) {
		fallbackCalls++
		return []byte("<html>fallback</html>"), "fallback-hash", nil
	}

	ok := EnsureLatestManagementHTML(context.Background(), tempDir, "", "")
	if !ok {
		t.Fatal("expected default repo to allow fallback download")
	}
	if fallbackCalls != 1 {
		t.Fatalf("expected one fallback download, got %d", fallbackCalls)
	}
	body, err := os.ReadFile(filepath.Join(tempDir, ManagementFileName))
	if err != nil {
		t.Fatalf("read local management file: %v", err)
	}
	if string(body) != "<html>fallback</html>" {
		t.Fatalf("unexpected local management file contents: %q", string(body))
	}
}
