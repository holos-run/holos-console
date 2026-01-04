package cli

import "testing"

func TestDeriveIssuer(t *testing.T) {
	tests := []struct {
		name       string
		listenAddr string
		issuer     string
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveIssuer(tt.listenAddr, tt.issuer)
			if got != tt.want {
				t.Errorf("deriveIssuer() = %v, want %v", got, tt.want)
			}
		})
	}
}
