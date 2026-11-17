package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Job states.
const (
	jobQueued    = "queued"
	jobRunning   = "running"
	jobSucceeded = "succeeded"
	jobFailed    = "failed"
)

// Job is a backup or restore that runs in the background. Its fields are
// what GET /jobs/{id} returns.
type Job struct {
	ID         string         `json:"job_id"`
	Type       string         `json:"type"`                // "backup" or "restore"
	Status     string         `json:"status"`              // queued, running, succeeded, failed
	BackupID   string         `json:"backup_id,omitempty"` // restore: the backup restored
	Result     map[string]any `json:"result,omitempty"`    // backup: id, size, duration, checksum, created_at
	Error      string         `json:"error,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	StartedAt  *time.Time     `json:"started_at,omitempty"`
	FinishedAt *time.Time     `json:"finished_at,omitempty"`

	run func(ctx context.Context) (map[string]any, error)
}

var errQueueFull = errors.New("too many queued jobs, try again later")

// jobQueue runs jobs one at a time, in the order they were submitted, and
// keeps the most recent ones so their status can be read back. Jobs run on
// the queue's context, not the request's: a client that disconnects or
// times out does not stop its backup or restore.
type jobQueue struct {
	mu      sync.Mutex
	jobs    map[string]*Job
	order   []string // job IDs, oldest first
	pending chan *Job
	keep    int // finished jobs kept for GET /jobs/{id}
	logf    func(format string, args ...any)
}

func newJobQueue(maxQueued, keep int, logf func(string, ...any)) *jobQueue {
	return &jobQueue{
		jobs:    make(map[string]*Job),
		pending: make(chan *Job, maxQueued),
		keep:    keep,
		logf:    logf,
	}
}

// submit queues a job and returns a snapshot of it.
func (q *jobQueue) submit(typ, backupID string, run func(context.Context) (map[string]any, error)) (Job, error) {
	j := &Job{
		ID:        newJobID(),
		Type:      typ,
		Status:    jobQueued,
		BackupID:  backupID,
		CreatedAt: time.Now().UTC(),
		run:       run,
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	select {
	case q.pending <- j:
	default:
		return Job{}, errQueueFull
	}
	q.jobs[j.ID] = j
	q.order = append(q.order, j.ID)
	q.evictLocked()
	q.logf("[pgwarden] job %s %s queued", j.ID, j.Type)
	return *j, nil
}

// get returns a snapshot of the job, safe to encode while the job runs.
func (q *jobQueue) get(id string) (Job, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.jobs[id]
	if !ok {
		return Job{}, false
	}
	return *j, true
}

// run processes jobs until ctx is done. A job running at that point sees
// its context cancelled and is recorded as failed.
func (q *jobQueue) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-q.pending:
			q.runOne(ctx, j)
		}
	}
}

func (q *jobQueue) runOne(ctx context.Context, j *Job) {
	q.mu.Lock()
	started := time.Now().UTC()
	j.Status = jobRunning
	j.StartedAt = &started
	q.mu.Unlock()
	q.logf("[pgwarden] job %s %s running", j.ID, j.Type)

	result, err := j.run(ctx)

	q.mu.Lock()
	finished := time.Now().UTC()
	j.FinishedAt = &finished
	if err != nil {
		j.Status = jobFailed
		j.Error = err.Error()
	} else {
		j.Status = jobSucceeded
		j.Result = result
	}
	q.evictLocked()
	q.mu.Unlock()

	if err != nil {
		q.logf("[pgwarden] job %s %s failed after %s: %v", j.ID, j.Type, finished.Sub(started).Round(time.Millisecond), err)
	} else {
		q.logf("[pgwarden] job %s %s succeeded after %s", j.ID, j.Type, finished.Sub(started).Round(time.Millisecond))
	}
}

// evictLocked drops the oldest finished jobs beyond q.keep. Queued and
// running jobs are never dropped. Callers hold q.mu.
func (q *jobQueue) evictLocked() {
	finished := 0
	for _, id := range q.order {
		if s := q.jobs[id].Status; s == jobSucceeded || s == jobFailed {
			finished++
		}
	}
	if finished <= q.keep {
		return
	}
	drop := finished - q.keep
	kept := q.order[:0]
	for _, id := range q.order {
		if s := q.jobs[id].Status; drop > 0 && (s == jobSucceeded || s == jobFailed) {
			delete(q.jobs, id)
			drop--
			continue
		}
		kept = append(kept, id)
	}
	q.order = kept
}

func newJobID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand: %v", err))
	}
	return hex.EncodeToString(b)
}
