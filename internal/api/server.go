package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/maruarchive/pgwarden/internal/backup"
	"github.com/maruarchive/pgwarden/internal/config"
	"github.com/maruarchive/pgwarden/internal/restore"
	"github.com/maruarchive/pgwarden/internal/storage"
)

type Server struct {
	cfg   *config.Config
	store *storage.S3Storage
}

func NewServer(cfg *config.Config, store *storage.S3Storage) *Server {
	return &Server{cfg: cfg, store: store}
}

func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /backup", s.handleBackup)
	mux.HandleFunc("POST /restore", s.handleRestore)
	mux.HandleFunc("GET /backups", s.handleList)
	mux.HandleFunc("DELETE /backups/{id}", s.handleDelete)
	mux.HandleFunc("GET /health", s.handleHealth)

	handler := s.authMiddleware(mux)

	addr := ":" + s.cfg.API.Port
	fmt.Printf("[pgwarden] API server listening on %s\n", addr)

	server := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Minute,
	}

	go func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	}()

	return server.ListenAndServe()
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.API.Secret != "" {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer "+s.cfg.API.Secret {
				s.jsonError(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	result, err := backup.Run(r.Context(), s.cfg, s.store)
	if err != nil {
		s.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.jsonResponse(w, map[string]interface{}{
		"id":         result.ID,
		"size":       result.Size,
		"duration":   result.Duration.String(),
		"checksum":   result.Checksum,
		"created_at": result.CreatedAt,
	}, http.StatusOK)
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.ID == "" {
		s.jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	if err := restore.Run(r.Context(), s.cfg, s.store, req.ID); err != nil {
		s.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.jsonResponse(w, map[string]interface{}{
		"status": "completed",
		"id":     req.ID,
	}, http.StatusOK)
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	objects, err := s.store.List(r.Context())
	if err != nil {
		s.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	backups := make([]map[string]interface{}, 0)
	for _, obj := range objects {
		backups = append(backups, map[string]interface{}{
			"key":        *obj.Key,
			"size":       obj.Size,
			"modified":   obj.LastModified,
		})
	}

	s.jsonResponse(w, backups, http.StatusOK)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		s.jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	key := fmt.Sprintf("backups/%s/%s.dump.gpg",
		id[:4]+"/"+id[4:6], id)

	if err := s.store.Delete(r.Context(), key); err != nil {
		s.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.jsonResponse(w, map[string]interface{}{
		"deleted": true,
		"id":      id,
	}, http.StatusOK)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.jsonResponse(w, map[string]string{"status": "ok"}, http.StatusOK)
}

func (s *Server) jsonResponse(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (s *Server) jsonError(w http.ResponseWriter, message string, status int) {
	s.jsonResponse(w, map[string]interface{}{
		"error": message,
	}, status)
}
