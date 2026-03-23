// Package task provides a bounded worker pool for fire-and-forget background
// tasks. It fills the gap between synchronous inline execution and the
// persistent, SQLite-backed job queue: tasks run concurrently in memory with
// panic recovery and graceful shutdown, but are not persisted or retried.
//
// Use task for lightweight work that should not block the caller — sending
// an email, dispatching a webhook, updating a cache — where losing the task
// on a crash is acceptable.
//
// Basic usage:
//
//	p := task.New(task.WithWorkers(4))
//
// Integration with lifecycle:
//
//	lc.Append(lifecycle.Hook{
//	    OnStart: p.Start,
//	    OnStop:  p.Stop,
//	})
//
// Submitting tasks:
//
//	ok := p.Submit(func() {
//	    _ = emailClient.Send(ctx, msg)
//	})
//	if !ok {
//	    logger.Warn("task pool full, sending synchronously")
//	    _ = emailClient.Send(ctx, msg)
//	}
package task
