package cli

import (
	"testing"
	"time"
)

func TestDeriveOrigin(t *testing.T) {
	tests := []struct {
		name       string
		listenAddr string
		origin     string
		plainHTTP  bool
		want       string
	}{
		{
			name:       "explicit origin takes precedence",
			listenAddr: ":8443",
			origin:     "https://holos-console.home.jeffmccune.com",
			want:       "https://holos-console.home.jeffmccune.com",
		},
		{
			name:       "derive from port-only listen",
			listenAddr: ":4443",
			origin:     "",
			want:       "https://localhost:4443",
		},
		{
			name:       "derive from full listen address",
			listenAddr: "localhost:9000",
			origin:     "",
			want:       "https://localhost:9000",
		},
		{
			name:       "0.0.0.0 becomes localhost",
			listenAddr: "0.0.0.0:8443",
			origin:     "",
			want:       "https://localhost:8443",
		},
		{
			name:       "plain http derive",
			listenAddr: ":8080",
			origin:     "",
			plainHTTP:  true,
			want:       "http://localhost:8080",
		},
		{
			name:       "plain http explicit origin unchanged",
			listenAddr: ":8080",
			origin:     "https://holos-console.example.com",
			plainHTTP:  true,
			want:       "https://holos-console.example.com",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveOrigin(tt.listenAddr, tt.origin, tt.plainHTTP)
			if got != tt.want {
				t.Errorf("deriveOrigin() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeriveIssuer(t *testing.T) {
	tests := []struct {
		name       string
		listenAddr string
		issuer     string
		plainHTTP  bool
		want       string
	}{
		{
			name:       "explicit issuer takes precedence",
			listenAddr: ":8443",
			issuer:     "https://console.example.com/dex",
			want:       "https://console.example.com/dex",
		},
		{
			name:       "derive from port-only listen",
			listenAddr: ":4443",
			issuer:     "",
			want:       "https://localhost:4443/dex",
		},
		{
			name:       "derive from full listen address",
			listenAddr: "localhost:9000",
			issuer:     "",
			want:       "https://localhost:9000/dex",
		},
		{
			name:       "0.0.0.0 becomes localhost",
			listenAddr: "0.0.0.0:8443",
			issuer:     "",
			want:       "https://localhost:8443/dex",
		},
		{
			name:       "plain http derive",
			listenAddr: ":8080",
			issuer:     "",
			plainHTTP:  true,
			want:       "http://localhost:8080/dex",
		},
		{
			name:       "plain http explicit issuer unchanged",
			listenAddr: ":8080",
			issuer:     "https://holos.example.com/dex",
			plainHTTP:  true,
			want:       "https://holos.example.com/dex",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveIssuer(tt.listenAddr, tt.issuer, tt.plainHTTP)
			if got != tt.want {
				t.Errorf("deriveIssuer() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTTLParsing(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"15 minutes", "15m", 15 * time.Minute, false},
		{"1 hour", "1h", time.Hour, false},
		{"30 seconds", "30s", 30 * time.Second, false},
		{"12 hours", "12h", 12 * time.Hour, false},
		{"1 hour 30 minutes", "1h30m", 90 * time.Minute, false},
		{"500 milliseconds", "500ms", 500 * time.Millisecond, false},
		{"invalid", "invalid", 0, true},
		{"empty string", "", 0, true},
		{"negative", "-15m", -15 * time.Minute, false}, // ParseDuration allows negative
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := time.ParseDuration(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("time.ParseDuration(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("time.ParseDuration(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
