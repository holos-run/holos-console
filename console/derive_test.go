package console

import "testing"

func TestDeriveRedirectURI(t *testing.T) {
	tests := []struct {
		name   string
		origin string
		want   string
	}{
		{
			name:   "standard origin",
			origin: "https://holos-console.home.jeffmccune.com",
			want:   "https://holos-console.home.jeffmccune.com/ui/callback",
		},
		{
			name:   "localhost origin",
			origin: "https://localhost:8443",
			want:   "https://localhost:8443/ui/callback",
		},
		{
			name:   "trailing slash stripped",
			origin: "https://holos-console.example.com/",
			want:   "https://holos-console.example.com/ui/callback",
		},
		{
			name:   "plain http origin",
			origin: "http://localhost:8080",
			want:   "http://localhost:8080/ui/callback",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveRedirectURI(tt.origin)
			if got != tt.want {
				t.Errorf("deriveRedirectURI() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDerivePostLogoutRedirectURI(t *testing.T) {
	tests := []struct {
		name   string
		origin string
		want   string
	}{
		{
			name:   "standard origin",
			origin: "https://holos-console.home.jeffmccune.com",
			want:   "https://holos-console.home.jeffmccune.com/ui",
		},
		{
			name:   "localhost origin",
			origin: "https://localhost:8443",
			want:   "https://localhost:8443/ui",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := derivePostLogoutRedirectURI(tt.origin)
			if got != tt.want {
				t.Errorf("derivePostLogoutRedirectURI() = %v, want %v", got, tt.want)
			}
		})
	}
}

