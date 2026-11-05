package storage

import "testing"

func TestEndpointURL(t *testing.T) {
	tests := []struct {
		endpoint string
		useSSL   bool
		want     string
	}{
		{"abc.r2.cloudflarestorage.com", true, "https://abc.r2.cloudflarestorage.com"},
		{"s3:8333", false, "http://s3:8333"},
		{"http://s3:8333/", true, "http://s3:8333"},
		{"https://s3.amazonaws.com", false, "https://s3.amazonaws.com"},
	}
	for _, tt := range tests {
		if got := endpointURL(tt.endpoint, tt.useSSL); got != tt.want {
			t.Errorf("endpointURL(%q, %v) = %q, want %q", tt.endpoint, tt.useSSL, got, tt.want)
		}
	}
}
