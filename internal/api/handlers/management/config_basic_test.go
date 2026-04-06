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
