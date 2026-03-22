// Package queue provides a SQLite-backed job queue with in-process workers.
// Jobs are enqueued with a type and JSON payload, then processed by registered
// handler functions. Failed jobs are retried with configurable backoff, and
// permanently failed jobs are moved to a dead-letter state.
//
// Basic usage:
//
//	q := queue.New(db)
//	q.Register("send_email", func(ctx context.Context, payload []byte) error {
//	    // process the job
//	    return nil
//	})
//
// Integration with lifecycle:
//
//	lc.Append(lifecycle.Hook{
//	    OnStart: q.Start,
//	    OnStop:  q.Stop,
//	})
//
// Enqueuing jobs:
//
//	id, err := q.Enqueue(ctx, "send_email", []byte(`{"to":"user@example.com"}`))
//	id, err := q.Enqueue(ctx, "report", payload, queue.Delay(time.Hour))
package queue
