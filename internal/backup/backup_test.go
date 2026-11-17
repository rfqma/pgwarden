package backup

import (
	"testing"
	"time"
)

func TestNextIDNeverRepeats(t *testing.T) {
	lastID = ""
	clock := time.Date(2026, 11, 17, 12, 0, 0, 300_000_000, time.UTC)
	now := func() time.Time { return clock }
	var slept time.Duration
	sleep := func(d time.Duration) { slept = d; clock = clock.Add(d) }

	_, first := nextID(now, sleep)
	if first != "20261117_120000" {
		t.Fatalf("first ID = %s", first)
	}

	// Same second: must wait until the next second and move on.
	clock = clock.Add(200 * time.Millisecond)
	_, second := nextID(now, sleep)
	if second != "20261117_120001" {
		t.Fatalf("second ID = %s, want 20261117_120001", second)
	}
	if slept != 500*time.Millisecond {
		t.Fatalf("slept %s, want 500ms", slept)
	}

	// A later second: no wait.
	slept = 0
	clock = clock.Add(3 * time.Second)
	if _, third := nextID(now, sleep); third != "20261117_120004" || slept != 0 {
		t.Fatalf("third ID = %s, slept %s", third, slept)
	}
}
