package management

import "testing"

func TestResolveLatestReleaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		repository string
		want       string
	}{
		{
			name:       "empty falls back to default",
			repository: "",
			want:       defaultLatestReleaseURL,
		},
		{
			name:       "github repository url",
			repository: "https://github.com/wenxi96/CLIProxyAPI",
			want:       "https://api.github.com/repos/wenxi96/CLIProxyAPI/releases/latest",
		},
		{
			name:       "github repository url with git suffix",
			repository: "https://github.com/wenxi96/CLIProxyAPI.git",
			want:       "https://api.github.com/repos/wenxi96/CLIProxyAPI/releases/latest",
		},
		{
			name:       "api release url stays as latest",
			repository: "https://api.github.com/repos/wenxi96/CLIProxyAPI/releases/latest",
			want:       "https://api.github.com/repos/wenxi96/CLIProxyAPI/releases/latest",
		},
		{
			name:       "api repo url appends latest",
			repository: "https://api.github.com/repos/wenxi96/CLIProxyAPI/releases",
			want:       "https://api.github.com/repos/wenxi96/CLIProxyAPI/releases/latest",
		},
		{
			name:       "plain owner repo path",
			repository: "wenxi96/CLIProxyAPI",
			want:       "https://api.github.com/repos/wenxi96/CLIProxyAPI/releases/latest",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveLatestReleaseURL(tt.repository); got != tt.want {
				t.Fatalf("resolveLatestReleaseURL(%q) = %q, want %q", tt.repository, got, tt.want)
			}
		})
	}
}

func TestResolveReleaseVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		info releaseInfo
		want string
	}{
		{
			name: "prefer release name",
			info: releaseInfo{
				Name:    "6.9.15-wx-1.0",
				TagName: "v6.9.15-wx-1.0-build.906c5fe6",
			},
			want: "6.9.15-wx-1.0",
		},
		{
			name: "normalize upstream release name",
			info: releaseInfo{
				Name: "v6.9.15",
			},
			want: "6.9.15",
		},
		{
			name: "fallback to tag name",
			info: releaseInfo{
				TagName: "v6.9.15-wx-1.0-build.906c5fe6",
			},
			want: "6.9.15-wx-1.0",
		},
		{
			name: "keep legacy fork version compatible",
			info: releaseInfo{
				TagName: "v6.9.16-wx.1.2-build.88a812ee",
			},
			want: "6.9.16-wx.1.2",
		},
		{
			name: "empty response",
			info: releaseInfo{},
			want: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveReleaseVersion(tt.info); got != tt.want {
				t.Fatalf("resolveReleaseVersion(%+v) = %q, want %q", tt.info, got, tt.want)
			}
		})
	}
}
