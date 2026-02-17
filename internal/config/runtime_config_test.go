package config

import "testing"

func TestValidateEndpointURL(t *testing.T) {
	tests := []struct {
		name    string
		envName string
		url     string
		wantErr bool
	}{
		{name: "https remote", envName: "BACKEND_BASE_URL", url: "https://api.example.com", wantErr: false},
		{name: "http loopback", envName: "AUTH_JWKS_URL", url: "http://localhost:8080/.well-known/jwks.json", wantErr: false},
		{name: "http remote", envName: "BACKEND_BASE_URL", url: "http://api.example.com", wantErr: true},
		{name: "query string", envName: "BACKEND_BASE_URL", url: "https://api.example.com?token=secret", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEndpointURL(tt.envName, tt.url)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for %s", tt.url)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error for %s: %v", tt.url, err)
			}
		})
	}
}
