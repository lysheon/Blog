//go:build integration

package jobs

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/lsy/blog/internal/config"
	"github.com/lsy/blog/internal/domain"
	"github.com/lsy/blog/internal/platform/database"
	"github.com/lsy/blog/internal/platform/ids"
	"github.com/lsy/blog/internal/platform/migrations"
)

func TestMySQLConcurrentClaimAndLifecycle(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Fatal("TEST_MYSQL_DSN is required for integration tests")
	}
	if err := migrations.RunUp(dsn); err != nil {
		t.Fatalf("RunUp(first): %v", err)
	}
	if err := migrations.RunUp(dsn); err != nil {
		t.Fatalf("RunUp(second): %v", err)
	}
	version, dirty, err := migrations.Version(dsn)
	if err != nil {
		t.Fatalf("Version(): %v", err)
	}
	versions, err := migrations.ListVersions()
	if err != nil {
		t.Fatalf("ListVersions(): %v", err)
	}
	if len(versions) == 0 || version != versions[len(versions)-1] || dirty {
		t.Fatalf("migration state = version %d dirty %v, want latest %v clean", version, dirty, versions)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := database.New(ctx, config.MySQLConfig{
		DSN: dsn, MaxOpenConns: 10, MaxIdleConns: 5,
		ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute,
	}, "dev")
	if err != nil {
		t.Fatalf("database.New(): %v", err)
	}
	defer database.Close(db)

	if err := db.Exec("DELETE FROM background_jobs WHERE job_type = ?", "integration_claim").Error; err != nil {
		t.Fatalf("clean jobs: %v", err)
	}
	producer := NewProducer(db, 3)
	const jobCount = 12
	runAfter := time.Now().UTC().Add(-time.Second)
	for i := 0; i < jobCount; i++ {
		key := fmt.Sprintf("integration-claim-%02d", i)
		if _, err := producer.Enqueue(ctx, "integration_claim", map[string]int{"index": i}, WithDedupKey(key), WithRunAfter(runAfter)); err != nil {
			t.Fatalf("Enqueue(%d): %v", i, err)
		}
	}

	cfg := config.JobsConfig{PollInterval: time.Millisecond, LockSeconds: 30, BatchSize: jobCount / 2}
	first := NewConsumer(db, cfg)
	second := NewConsumer(db, cfg)
	start := make(chan struct{})
	type result struct {
		consumer *Consumer
		jobs     []uint64
		err      error
	}
	results := make(chan result, 2)
	claim := func(consumer *Consumer) {
		<-start
		claimed, err := consumer.Claim(ctx)
		ids := make([]uint64, 0, len(claimed))
		for _, job := range claimed {
			ids = append(ids, job.ID)
		}
		results <- result{consumer: consumer, jobs: ids, err: err}
	}
	go claim(first)
	go claim(second)
	close(start)

	seen := make(map[uint64]struct{}, jobCount)
	for range 2 {
		claimed := <-results
		if claimed.err != nil {
			t.Fatalf("Claim(): %v", claimed.err)
		}
		for _, id := range claimed.jobs {
			if _, duplicate := seen[id]; duplicate {
				t.Fatalf("job %d claimed by more than one worker", id)
			}
			seen[id] = struct{}{}
			if err := claimed.consumer.Complete(ctx, id); err != nil {
				t.Fatalf("Complete(%d): %v", id, err)
			}
		}
	}
	if len(seen) != jobCount {
		t.Fatalf("claimed %d unique jobs, want %d", len(seen), jobCount)
	}

	var completed int64
	if err := db.Table("background_jobs").Where("job_type = ? AND status = 'completed'", "integration_claim").Count(&completed).Error; err != nil {
		t.Fatalf("count completed: %v", err)
	}
	if completed != jobCount {
		t.Fatalf("completed jobs = %d, want %d", completed, jobCount)
	}
	stats, err := first.QueueStats(ctx)
	if err != nil {
		t.Fatalf("QueueStats(): %v", err)
	}
	if stats.Completed != jobCount || stats.Pending != 0 || stats.Running != 0 || stats.Dead != 0 {
		t.Fatalf("QueueStats() = %+v, want completed=%d and no unfinished jobs", stats, jobCount)
	}
}

// The retry, dead-letter and stale-lock transitions are the recovery contract
// the Worker depends on, so they are asserted against real MySQL rows.

func TestFailRetriesUntilAttemptsAreExhausted(t *testing.T) {
	db := newJobsTestDB(t)
	const jobType = "integration_retry"
	cleanJobs(t, db, jobType)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	producer := NewProducer(db, 5)
	enqueued, err := producer.Enqueue(ctx, jobType, map[string]string{"case": "retry"},
		WithMaxAttempts(2), WithRunAfter(time.Now().UTC().Add(-time.Second)))
	if err != nil {
		t.Fatalf("Enqueue(): %v", err)
	}

	consumer := NewConsumer(db, config.JobsConfig{PollInterval: time.Millisecond, LockSeconds: 60, BatchSize: 5})

	// First failure still has budget, so the job returns to pending and is
	// rescheduled instead of being lost.
	claimOne(t, ctx, consumer, enqueued.ID)
	if err := consumer.Fail(ctx, enqueued.ID, "first failure"); err != nil {
		t.Fatalf("Fail(first): %v", err)
	}
	job := loadJob(t, db, enqueued.ID)
	if job.Status != "pending" || job.Attempts != 1 {
		t.Fatalf("after first failure status=%q attempts=%d, want pending/1", job.Status, job.Attempts)
	}
	if job.LockedBy != nil || job.LockedAt != nil {
		t.Fatalf("a retried job must release its lock, got locked_by=%v locked_at=%v", job.LockedBy, job.LockedAt)
	}
	if job.LastError == nil || *job.LastError != "first failure" {
		t.Fatalf("last_error = %v, want the recorded failure", job.LastError)
	}
	if !job.RunAfter.After(time.Now().UTC()) {
		t.Fatalf("run_after = %s, want a future retry schedule", job.RunAfter)
	}

	// The final failure exhausts the budget, so the job becomes dead instead of
	// cycling forever.
	if err := db.Model(&domain.Job{}).Where("id = ?", enqueued.ID).Update("run_after", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatalf("reschedule for final attempt: %v", err)
	}
	claimOne(t, ctx, consumer, enqueued.ID)
	if err := consumer.Fail(ctx, enqueued.ID, "final failure"); err != nil {
		t.Fatalf("Fail(final): %v", err)
	}
	job = loadJob(t, db, enqueued.ID)
	if job.Status != "dead" || job.Attempts != 2 {
		t.Fatalf("after final failure status=%q attempts=%d, want dead/2", job.Status, job.Attempts)
	}
	if job.FinishedAt == nil {
		t.Fatal("a dead job must record finished_at")
	}

	stats, err := consumer.QueueStats(ctx)
	if err != nil {
		t.Fatalf("QueueStats(): %v", err)
	}
	if stats.Dead < 1 {
		t.Fatalf("QueueStats() = %+v, want the dead job counted", stats)
	}
}

func TestFailTruncatesOversizedErrors(t *testing.T) {
	db := newJobsTestDB(t)
	const jobType = "integration_truncate"
	cleanJobs(t, db, jobType)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	producer := NewProducer(db, 5)
	enqueued, err := producer.Enqueue(ctx, jobType, map[string]string{"case": "truncate"},
		WithMaxAttempts(3), WithRunAfter(time.Now().UTC().Add(-time.Second)))
	if err != nil {
		t.Fatalf("Enqueue(): %v", err)
	}
	consumer := NewConsumer(db, config.JobsConfig{PollInterval: time.Millisecond, LockSeconds: 60, BatchSize: 5})
	claimOne(t, ctx, consumer, enqueued.ID)

	if err := consumer.Fail(ctx, enqueued.ID, strings.Repeat("e", 5000)); err != nil {
		t.Fatalf("Fail(): %v", err)
	}

	job := loadJob(t, db, enqueued.ID)
	if job.LastError == nil {
		t.Fatal("last_error must be recorded")
	}
	if len(*job.LastError) != 1000 {
		t.Fatalf("last_error length = %d, want it bounded to 1000", len(*job.LastError))
	}
}

func TestReapStaleJobsSplitsRetryableAndExhausted(t *testing.T) {
	db := newJobsTestDB(t)
	const jobType = "integration_stale"
	cleanJobs(t, db, jobType)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// A crashed worker leaves rows in running with an old lock. Rows with budget
	// left go back to pending; rows without it must not be reclaimed forever.
	retryable := insertStaleRunningJob(t, db, jobType, 1, 3)
	exhausted := insertStaleRunningJob(t, db, jobType, 3, 3)

	consumer := NewConsumer(db, config.JobsConfig{PollInterval: time.Millisecond, LockSeconds: 60, BatchSize: 5})
	reclaimed, err := consumer.ReapStaleJobsCount(ctx)
	if err != nil {
		t.Fatalf("ReapStaleJobsCount(): %v", err)
	}
	if reclaimed < 2 {
		t.Fatalf("reclaimed = %d, want both stale jobs handled", reclaimed)
	}

	if job := loadJob(t, db, retryable); job.Status != "pending" || job.LockedBy != nil {
		t.Fatalf("retryable stale job status=%q locked_by=%v, want pending and unlocked", job.Status, job.LockedBy)
	}
	dead := loadJob(t, db, exhausted)
	if dead.Status != "dead" || dead.FinishedAt == nil {
		t.Fatalf("exhausted stale job status=%q finished_at=%v, want dead and finished", dead.Status, dead.FinishedAt)
	}
	if dead.LastError == nil || !strings.Contains(*dead.LastError, "lock expired") {
		t.Fatalf("last_error = %v, want the lock-expiry reason", dead.LastError)
	}
}

func TestCompleteAndFailRequireOwnership(t *testing.T) {
	db := newJobsTestDB(t)
	const jobType = "integration_ownership"
	cleanJobs(t, db, jobType)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	producer := NewProducer(db, 5)
	enqueued, err := producer.Enqueue(ctx, jobType, map[string]string{"case": "ownership"},
		WithMaxAttempts(3), WithRunAfter(time.Now().UTC().Add(-time.Second)))
	if err != nil {
		t.Fatalf("Enqueue(): %v", err)
	}

	cfg := config.JobsConfig{PollInterval: time.Millisecond, LockSeconds: 60, BatchSize: 5}
	owner := NewConsumer(db, cfg)
	stranger := NewConsumer(db, cfg)
	claimOne(t, ctx, owner, enqueued.ID)

	if err := stranger.Complete(ctx, enqueued.ID); err == nil {
		t.Fatal("a worker that does not hold the lock must not complete the job")
	}
	// Fail is idempotent for a job this worker never claimed, so it reports no
	// error but must leave the row untouched.
	if err := stranger.Fail(ctx, enqueued.ID, "not mine"); err != nil {
		t.Fatalf("Fail(stranger): %v", err)
	}
	if job := loadJob(t, db, enqueued.ID); job.Status != "running" || job.LastError != nil {
		t.Fatalf("job status=%q last_error=%v, want it still owned by the original worker", job.Status, job.LastError)
	}

	if err := owner.Complete(ctx, enqueued.ID); err != nil {
		t.Fatalf("Complete(owner): %v", err)
	}
	if job := loadJob(t, db, enqueued.ID); job.Status != "completed" || job.FinishedAt == nil {
		t.Fatalf("job status=%q finished_at=%v, want completed", job.Status, job.FinishedAt)
	}
}

func newJobsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Fatal("TEST_MYSQL_DSN is required for integration tests")
	}
	if err := migrations.RunUp(dsn); err != nil {
		t.Fatalf("RunUp(): %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := database.New(ctx, config.MySQLConfig{
		DSN: dsn, MaxOpenConns: 10, MaxIdleConns: 5,
		ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute,
	}, "dev")
	if err != nil {
		t.Fatalf("database.New(): %v", err)
	}
	t.Cleanup(func() { database.Close(db) })
	return db
}

func cleanJobs(t *testing.T, db *gorm.DB, jobType string) {
	t.Helper()
	if err := db.Exec("DELETE FROM background_jobs WHERE job_type = ?", jobType).Error; err != nil {
		t.Fatalf("clean jobs: %v", err)
	}
}

func claimOne(t *testing.T, ctx context.Context, consumer *Consumer, jobID uint64) {
	t.Helper()
	claimed, err := consumer.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim(): %v", err)
	}
	for _, job := range claimed {
		if job.ID == jobID {
			return
		}
	}
	t.Fatalf("Claim() returned %d jobs, none of them job %d", len(claimed), jobID)
}

func loadJob(t *testing.T, db *gorm.DB, jobID uint64) domain.Job {
	t.Helper()
	var job domain.Job
	if err := db.Where("id = ?", jobID).First(&job).Error; err != nil {
		t.Fatalf("load job %d: %v", jobID, err)
	}
	return job
}

func insertStaleRunningJob(t *testing.T, db *gorm.DB, jobType string, attempts, maxAttempts int) uint64 {
	t.Helper()
	stale := time.Now().UTC().Add(-10 * time.Minute)
	lockedBy := "crashed-worker"
	job := domain.Job{
		PublicID: ids.MustNewULID(), JobType: jobType, PayloadJSON: []byte(`{"case":"stale"}`),
		Status: "running", Attempts: attempts, MaxAttempts: maxAttempts,
		RunAfter: stale, LockedBy: &lockedBy, LockedAt: &stale,
		CreatedAt: stale, UpdatedAt: stale,
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatalf("insert stale job: %v", err)
	}
	return job.ID
}
