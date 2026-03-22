package queue

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stanza-go/framework/pkg/sqlite"
)

func benchDB(b *testing.B) *sqlite.DB {
	b.Helper()
	db := sqlite.New(":memory:")
	if err := db.Start(context.Background()); err != nil {
		b.Fatalf("open db: %v", err)
	}
	b.Cleanup(func() { db.Stop(context.Background()) })
	return db
}

func benchQueue(b *testing.B, db *sqlite.DB) *Queue {
	b.Helper()
	q := New(db,
		WithWorkers(1),
		WithPollInterval(time.Hour), // long poll — we're benchmarking enqueue, not processing
	)
	q.Register("bench_job", func(_ context.Context, _ []byte) error {
		return nil
	})
	if err := q.Start(context.Background()); err != nil {
		b.Fatalf("start queue: %v", err)
	}
	b.Cleanup(func() { q.Stop(context.Background()) })
	return q
}

// --- Enqueue throughput ---

func BenchmarkQueue_Enqueue(b *testing.B) {
	db := benchDB(b)
	q := benchQueue(b, db)

	payload := []byte(`{"to":"user@example.com","subject":"Hello"}`)
	ctx := context.Background()

	b.ResetTimer()
	for range b.N {
		if _, err := q.Enqueue(ctx, "bench_job", payload); err != nil {
			b.Fatalf("enqueue: %v", err)
		}
	}
}

func BenchmarkQueue_Enqueue_WithOptions(b *testing.B) {
	db := benchDB(b)
	q := benchQueue(b, db)

	payload := []byte(`{"data":"test"}`)
	ctx := context.Background()

	b.ResetTimer()
	for range b.N {
		if _, err := q.Enqueue(ctx, "bench_job", payload,
			Delay(5*time.Minute),
			MaxAttempts(5),
			OnQueue("high"),
		); err != nil {
			b.Fatalf("enqueue: %v", err)
		}
	}
}

// --- Stats query ---

func BenchmarkQueue_Stats(b *testing.B) {
	db := benchDB(b)
	q := benchQueue(b, db)

	// Seed some jobs
	ctx := context.Background()
	for i := range 500 {
		q.Enqueue(ctx, "bench_job", []byte(fmt.Sprintf(`{"i":%d}`, i)))
	}

	b.ResetTimer()
	for range b.N {
		if _, err := q.Stats(); err != nil {
			b.Fatalf("stats: %v", err)
		}
	}
}

// --- Jobs listing ---

func BenchmarkQueue_Jobs(b *testing.B) {
	db := benchDB(b)
	q := benchQueue(b, db)

	ctx := context.Background()
	for i := range 1000 {
		q.Enqueue(ctx, "bench_job", []byte(fmt.Sprintf(`{"i":%d}`, i)))
	}

	b.ResetTimer()
	for range b.N {
		if _, err := q.Jobs(Filter{Status: StatusPending, Limit: 20}); err != nil {
			b.Fatalf("jobs: %v", err)
		}
	}
}

// --- Job processing throughput ---

func BenchmarkQueue_ProcessJobs(b *testing.B) {
	db := benchDB(b)
	q := New(db,
		WithWorkers(4),
		WithPollInterval(time.Millisecond),
	)

	processed := make(chan struct{}, b.N+100)
	q.Register("bench_job", func(_ context.Context, _ []byte) error {
		processed <- struct{}{}
		return nil
	})
	if err := q.Start(context.Background()); err != nil {
		b.Fatalf("start: %v", err)
	}
	b.Cleanup(func() { q.Stop(context.Background()) })

	ctx := context.Background()
	payload := []byte(`{"data":"bench"}`)

	b.ResetTimer()
	for range b.N {
		if _, err := q.Enqueue(ctx, "bench_job", payload); err != nil {
			b.Fatalf("enqueue: %v", err)
		}
	}
	// Wait for all to be processed
	for range b.N {
		select {
		case <-processed:
		case <-time.After(30 * time.Second):
			b.Fatal("timeout waiting for jobs to process")
		}
	}
}
