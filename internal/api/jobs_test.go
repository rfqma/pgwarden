package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maruarchive/pgwarden/internal/backup"
)

func quietQueue(maxQueued, keep int) *jobQueue {
	return newJobQueue(maxQueued, keep, func(string, ...any) {})
}

// waitFor polls the queue until the job reaches a finished state.
func waitFor(t *testing.T, q *jobQueue, id string) Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if j, ok := q.get(id); ok && (j.Status == jobSucceeded || j.Status == jobFailed) {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s did not finish", id)
	return Job{}
}

func TestJobQueueRunsJobsOneAtATimeInOrder(t *testing.T) {
	q := quietQueue(8, 100)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go q.run(ctx)

	var running, maxRunning atomic.Int32
	var order []int
	var ids []string
	for i := range 4 {
		j, err := q.submit("backup", "", func(context.Context) (map[string]any, error) {
			n := running.Add(1)
			if n > maxRunning.Load() {
				maxRunning.Store(n)
			}
			time.Sleep(10 * time.Millisecond)
			order = append(order, i)
			running.Add(-1)
			return map[string]any{"n": i}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if j.Status != jobQueued {
			t.Fatalf("submitted job status = %s", j.Status)
		}
		ids = append(ids, j.ID)
	}
	for _, id := range ids {
		if j := waitFor(t, q, id); j.Status != jobSucceeded {
			t.Fatalf("job %s = %s", id, j.Status)
		}
	}
	if maxRunning.Load() != 1 {
		t.Fatalf("%d jobs ran at once, want 1", maxRunning.Load())
	}
	if fmt.Sprint(order) != "[0 1 2 3]" {
		t.Fatalf("ran in order %v", order)
	}
}

func TestJobQueueRecordsFailure(t *testing.T) {
	q := quietQueue(8, 100)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go q.run(ctx)

	j, _ := q.submit("restore", "20260726_071210", func(context.Context) (map[string]any, error) {
		return nil, errors.New("pg_restore failed: boom")
	})
	got := waitFor(t, q, j.ID)
	if got.Status != jobFailed || got.Error != "pg_restore failed: boom" || got.Result != nil {
		t.Fatalf("job = %+v", got)
	}
	if got.StartedAt == nil || got.FinishedAt == nil {
		t.Fatal("started_at/finished_at not set")
	}
}

// The job's context is the queue's, so it outlives the request that
// submitted it; only stopping the queue cancels it.
func TestJobQueueCancelsRunningJobOnShutdown(t *testing.T) {
	q := quietQueue(8, 100)
	ctx, cancel := context.WithCancel(context.Background())
	go q.run(ctx)

	started := make(chan struct{})
	j, _ := q.submit("backup", "", func(ctx context.Context) (map[string]any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	<-started
	cancel()
	if got := waitFor(t, q, j.ID); got.Status != jobFailed || !strings.Contains(got.Error, "canceled") {
		t.Fatalf("job = %+v", got)
	}
}

func TestJobQueueFull(t *testing.T) {
	q := quietQueue(2, 100) // no worker: jobs stay queued
	noop := func(context.Context) (map[string]any, error) { return nil, nil }
	for range 2 {
		if _, err := q.submit("backup", "", noop); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := q.submit("backup", "", noop); !errors.Is(err, errQueueFull) {
		t.Fatalf("err = %v, want errQueueFull", err)
	}
}

func TestJobQueueKeepsOnlyRecentFinishedJobs(t *testing.T) {
	q := quietQueue(16, 3)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go q.run(ctx)

	var ids []string
	for range 6 {
		j, _ := q.submit("backup", "", func(context.Context) (map[string]any, error) { return nil, nil })
		ids = append(ids, j.ID)
		waitFor(t, q, j.ID)
	}
	for i, id := range ids {
		_, ok := q.get(id)
		if want := i >= 3; ok != want {
			t.Errorf("job %d kept = %v, want %v", i, ok, want)
		}
	}
}

// --- HTTP ---

func testServerWithFakes(t *testing.T) (*Server, http.Handler, context.CancelFunc) {
	t.Helper()
	s := newTestServer("s3cret", false)
	s.jobs = quietQueue(8, 100)
	s.runBackup = func(context.Context) (*backup.Result, error) {
		return &backup.Result{ID: "20261117_120000", Size: 42, Duration: time.Second, Checksum: "abc",
			CreatedAt: time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)}, nil
	}
	s.runRestore = func(context.Context, string) error { return nil }
	s.backupExists = func(_ context.Context, id string) (bool, error) { return id == "20261117_120000", nil }

	ctx, cancel := context.WithCancel(context.Background())
	go s.jobs.run(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /backup", s.handleBackup)
	mux.HandleFunc("POST /restore", s.handleRestore)
	mux.HandleFunc("GET /jobs/{id}", s.handleJob)
	return s, s.authMiddleware(mux), cancel
}

func do(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer s3cret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func pollJob(t *testing.T, h http.Handler, id string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rec := do(h, http.MethodGet, "/jobs/"+id, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /jobs/%s = %d %s", id, rec.Code, rec.Body)
		}
		var j map[string]any
		json.Unmarshal(rec.Body.Bytes(), &j)
		if j["status"] == jobSucceeded || j["status"] == jobFailed {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return nil
}

func TestBackupEndpointQueuesJob(t *testing.T) {
	_, h, cancel := testServerWithFakes(t)
	defer cancel()

	rec := do(h, http.MethodPost, "/backup", "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /backup = %d %s", rec.Code, rec.Body)
	}
	var accepted map[string]any
	json.Unmarshal(rec.Body.Bytes(), &accepted)
	id, _ := accepted["job_id"].(string)
	if id == "" || accepted["status"] != jobQueued || accepted["type"] != "backup" {
		t.Fatalf("202 body = %s", rec.Body)
	}
	if loc := rec.Header().Get("Location"); loc != "/jobs/"+id {
		t.Fatalf("Location = %q", loc)
	}

	j := pollJob(t, h, id)
	result, _ := j["result"].(map[string]any)
	if j["status"] != jobSucceeded || result["id"] != "20261117_120000" || result["size"] != float64(42) {
		t.Fatalf("finished job = %v", j)
	}
}

func TestRestoreEndpoint(t *testing.T) {
	_, h, cancel := testServerWithFakes(t)
	defer cancel()

	if rec := do(h, http.MethodPost, "/restore", `{"id":"20250101_000000"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("restore of a missing backup = %d, want 404", rec.Code)
	}

	rec := do(h, http.MethodPost, "/restore", `{"id":"20261117_120000"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /restore = %d %s", rec.Code, rec.Body)
	}
	var accepted map[string]any
	json.Unmarshal(rec.Body.Bytes(), &accepted)
	j := pollJob(t, h, accepted["job_id"].(string))
	if j["status"] != jobSucceeded || j["type"] != "restore" || j["backup_id"] != "20261117_120000" {
		t.Fatalf("finished job = %v", j)
	}
}

func TestJobEndpointUnknownAndAuth(t *testing.T) {
	_, h, cancel := testServerWithFakes(t)
	defer cancel()

	if rec := do(h, http.MethodGet, "/jobs/nope", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown job = %d, want 404", rec.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/jobs/nope", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /jobs without token = %d, want 401", rec.Code)
	}
}
