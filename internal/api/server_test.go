package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maruarchive/pgwarden/internal/config"
)

func newTestServer(secret string, insecure bool) *Server {
	return NewServer(&config.Config{API: config.APIConfig{
		Port:     "0",
		Secret:   secret,
		Insecure: insecure,
	}}, nil)
}

func TestStartRefusesWithoutSecret(t *testing.T) {
	err := newTestServer("", false).Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "API_SECRET is required") {
		t.Fatalf("Start() error = %v, want API_SECRET is required", err)
	}
}

func TestAuthMiddleware(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name     string
		secret   string
		path     string
		header   string
		wantCode int
	}{
		{"no header", "s3cret", "/backups", "", http.StatusUnauthorized},
		{"wrong token", "s3cret", "/backups", "Bearer nope", http.StatusUnauthorized},
		{"token prefix only", "s3cret", "/backups", "Bearer s3cre", http.StatusUnauthorized},
		{"right token", "s3cret", "/backups", "Bearer s3cret", http.StatusOK},
		{"health needs no token", "s3cret", "/health", "", http.StatusOK},
		{"insecure mode", "", "/backups", "", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestServer(tt.secret, tt.secret == "").authMiddleware(ok)
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}
		})
	}
}
