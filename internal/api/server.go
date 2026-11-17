package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/maruarchive/pgwarden/internal/backup"
	"github.com/maruarchive/pgwarden/internal/config"
	"github.com/maruarchive/pgwarden/internal/restore"
	"github.com/maruarchive/pgwarden/internal/storage"
)

// Limits for the background job queue.
const (
	maxQueuedJobs = 16  // POST /backup and /restore answer 503 beyond this
	keptJobs      = 100 // finished jobs still readable via GET /jobs/{id}
)

type Server struct {
	cfg   *config.Config
	store *storage.S3Storage
	jobs  *jobQueue

	// The work behind the endpoints; replaced in tests.
	runBackup    func(ctx context.Context) (*backup.Result, error)
	runRestore   func(ctx context.Context, backupID string) error
	backupExists func(ctx context.Context, backupID string) (bool, error)
}

func NewServer(cfg *config.Config, store *storage.S3Storage) *Server {
	s := &Server{
		cfg:   cfg,
		store: store,
		jobs: newJobQueue(maxQueuedJobs, keptJobs, func(format string, args ...any) {
			fmt.Printf(format+"\n", args...)
		}),
	}
	s.runBackup = func(ctx context.Context) (*backup.Result, error) {
		return backup.Run(ctx, s.cfg, s.store)
	}
	s.runRestore = func(ctx context.Context, id string) error {
		return restore.Run(ctx, s.cfg, s.store, id)
	}
	s.backupExists = func(ctx context.Context, id string) (bool, error) {
		key, err := storage.BackupKey(id)
		if err != nil {
			return false, err
		}
		return s.store.Exists(ctx, key)
	}
	return s
}

func (s *Server) Start(ctx context.Context) error {
	if s.cfg.API.Secret == "" {
		if !s.cfg.API.Insecure {
			return fmt.Errorf("API_SECRET is required: without it anyone who can reach the API can restore over or delete your backups (set API_INSECURE=true to run without auth, development only)")
		}
		fmt.Println("[pgwarden] WARNING: API_INSECURE=true, the API is running WITHOUT authentication")
	}

	mux := http.NewServeMux()

	mux.HandleFunc("POST /backup", s.handleBackup)
	mux.HandleFunc("POST /restore", s.handleRestore)
	mux.HandleFunc("GET /backups", s.handleList)
	mux.HandleFunc("DELETE /backups/{id}", s.handleDelete)
	mux.HandleFunc("GET /jobs/{id}", s.handleJob)
	mux.HandleFunc("GET /health", s.handleHealth)

	// Backups and restores run here, outside any request. SIGINT/SIGTERM
	// cancels a running job, which is then recorded as failed.
	go s.jobs.run(ctx)

	handler := s.authMiddleware(mux)

	addr := ":" + s.cfg.API.Port
	fmt.Printf("[pgwarden] API server listening on %s\n", addr)

	server := &http.Server{
		Addr:        addr,
		Handler:     handler,
		ReadTimeout: 30 * time.Second,
		// Every response is quick now that backup and restore run as jobs.
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	}()

	return server.ListenAndServe()
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /health stays open so Docker and load balancers can probe it.
		if s.cfg.API.Secret != "" && r.URL.Path != "/health" {
			auth := r.Header.Get("Authorization")
			want := "Bearer " + s.cfg.API.Secret
			if subtle.ConstantTimeCompare([]byte(auth), []byte(want)) != 1 {
				s.jsonError(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// handleBackup queues a backup and answers 202 with the job to poll.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	job, err := s.jobs.submit("backup", "", func(ctx context.Context) (map[string]any, error) {
		result, err := s.runBackup(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"id":         result.ID,
			"size":       result.Size,
			"duration":   result.Duration.String(),
			"checksum":   result.Checksum,
			"created_at": result.CreatedAt,
		}, nil
	})
	s.jobAccepted(w, job, err)
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
	if _, err := storage.BackupKey(req.ID); err != nil {
		s.jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Reject an unknown backup now rather than queueing a job that fails.
	exists, err := s.backupExists(r.Context(), req.ID)
	if err != nil {
		s.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !exists {
		s.jsonError(w, fmt.Sprintf("backup %s not found", req.ID), http.StatusNotFound)
		return
	}

	id := req.ID
	job, err := s.jobs.submit("restore", id, func(ctx context.Context) (map[string]any, error) {
		return nil, s.runRestore(ctx, id)
	})
	s.jobAccepted(w, job, err)
}

// jobAccepted answers a queued job with 202 and where to poll it.
func (s *Server) jobAccepted(w http.ResponseWriter, job Job, err error) {
	if err != nil {
		s.jsonError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Location", "/jobs/"+job.ID)
	s.jsonResponse(w, job, http.StatusAccepted)
}

// handleJob reports a job's status, and its result or error once finished.
func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	job, ok := s.jobs.get(r.PathValue("id"))
	if !ok {
		s.jsonError(w, "job not found (unknown, expired, or pgwarden restarted)", http.StatusNotFound)
		return
	}
	s.jsonResponse(w, job, http.StatusOK)
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
			"key":      *obj.Key,
			"size":     obj.Size,
			"modified": obj.LastModified,
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

	key, err := storage.BackupKey(id)
	if err != nil {
		s.jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

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
