// Package stanza is an AI-native, batteries-included Go framework for rapid
// application development. It provides HTTP routing, SQLite storage, structured
// logging, configuration, lifecycle management, cron scheduling, job queues,
// and authentication — all built from scratch on Go's standard library with
// zero external dependencies.
//
// A Stanza application runs as a single process with a single SQLite file and
// a single data directory. The framework handles startup ordering, dependency
// injection, and graceful shutdown.
//
// All framework packages live under pkg/. Application code imports them
// directly:
//
//	import "github.com/stanza-go/framework/pkg/http"
//	import "github.com/stanza-go/framework/pkg/sqlite"
//	import "github.com/stanza-go/framework/pkg/log"
package stanza
