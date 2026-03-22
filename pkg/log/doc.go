// Package log provides structured JSON logging with level filtering, child
// loggers, and file rotation. It is built entirely on Go's standard library
// with zero external dependencies.
//
// Basic usage:
//
//	logger := log.New(log.WithLevel(log.LevelDebug))
//	logger.Info("server started", log.String("addr", ":8080"), log.Int("port", 8080))
//
// Output:
//
//	{"time":"2024-01-15T10:30:00.123Z","level":"info","msg":"server started","addr":":8080","port":8080}
//
// Child loggers carry pre-set fields:
//
//	httpLog := logger.With(log.String("component", "http"))
//	httpLog.Info("request", log.String("method", "GET"), log.String("path", "/api/v1"))
//
// File output with rotation:
//
//	fw, err := log.NewFileWriter("/var/log/myapp")
//	logger := log.New(log.WithWriter(io.MultiWriter(os.Stdout, fw)))
package log
