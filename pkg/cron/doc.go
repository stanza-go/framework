// Package cron provides an in-process cron scheduler for periodic task
// execution. It supports standard 5-field cron expressions, named jobs,
// graceful shutdown, and status introspection.
//
// Basic usage:
//
//	s := cron.NewScheduler()
//	s.Add("cleanup", "0 */6 * * *", func(ctx context.Context) error {
//	    // runs every 6 hours
//	    return nil
//	})
//
// Integration with lifecycle:
//
//	lc.Append(lifecycle.Hook{
//	    OnStart: s.Start,
//	    OnStop:  s.Stop,
//	})
//
// Querying job status:
//
//	for _, e := range s.Entries() {
//	    fmt.Printf("%s next=%s running=%v\n", e.Name, e.NextRun, e.Running)
//	}
package cron
